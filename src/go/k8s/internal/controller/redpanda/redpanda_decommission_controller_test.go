package redpanda_test

import (
	"context"
	"crypto/tls"
	"os"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/redpanda-data/helm-charts/charts/redpanda"
	"github.com/redpanda-data/helm-charts/pkg/gotohelm/helmette"
	"github.com/redpanda-data/helm-charts/pkg/helm"
	"github.com/redpanda-data/helm-charts/pkg/helm/helmtest"
	"github.com/redpanda-data/helm-charts/pkg/testutil"
	controllers "github.com/redpanda-data/redpanda-operator/src/go/k8s/internal/controller/redpanda"
	"github.com/redpanda-data/redpanda-operator/src/go/k8s/pkg/k3d"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zapio"
	"go.uber.org/zap/zaptest"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

func TestDecommission(t *testing.T) {
	if testing.Short() {
		t.Skipf("Skipping log running test...")
	}

	ctx := testutil.Context(t)

	ctx = log.IntoContext(ctx, testr.New(t))
	log.SetLogger(log.FromContext(ctx))

	cluster, err := k3d.NewCluster(t.Name())
	require.NoError(t, err)
	t.Logf("created cluster %T %q", cluster, cluster.Name)

	// TODO It would be good to have this image in registry to speed up setup process
	//require.NoError(t, cluster.ImageImport("redpandadata/redpanda:v24.2.2"))

	t.Cleanup(func() {
		if testutil.Retain() {
			t.Logf("retain flag is set; not deleting cluster %q", cluster.Name)
			return
		}
		t.Logf("Deleting cluster %q", cluster.Name)
		require.NoError(t, cluster.Cleanup())
	})

	c, err := client.New(cluster.RESTConfig(), client.Options{})
	require.NoError(t, err)

	h := helmtest.Setup(t)
	env := h.Namespaced(t)

	partial := redpanda.PartialValues{
		//RBAC: &redpanda.PartialRBAC{Enabled: ptr.To(true)},
		Config: &redpanda.PartialConfig{
			Node: redpanda.PartialNodeConfig{
				"developer_mode": true,
			},
		},
		Statefulset: &redpanda.PartialStatefulset{
			//SideCars:                      enableOperatorController(),
			TerminationGracePeriodSeconds: ptr.To(int64(2)),
		},
		External: &redpanda.PartialExternalConfig{Enabled: ptr.To(false)},
	}

	helmClient, err := helm.New(helm.Options{
		KubeConfig: env.Ctl().RestConfig(),
		ConfigHome: testutil.TempDir(t),
	})
	require.NoError(t, err)

	err = helmClient.RepoAdd(ctx, "redpanda", "https://charts.redpanda.com")
	require.NoError(t, err)

	rpRelease, err := helmClient.Install(ctx, "redpanda/redpanda", helm.InstallOptions{
		Values:    partial,
		Namespace: env.Namespace(),
	})
	require.NoError(t, err)

	dot := &helmette.Dot{
		Values:  *helmette.UnmarshalInto[*helmette.Values](partial),
		Release: helmette.Release{Name: rpRelease.Name, Namespace: rpRelease.Namespace},
		Chart: helmette.Chart{
			Name: "redpanda",
		},
	}

	rpk := Client{Ctl: env.Ctl(), Release: &rpRelease}

	log := zaptest.NewLogger(t)
	w := &zapio.Writer{Log: log, Level: zapcore.InfoLevel}
	wErr := &zapio.Writer{Log: log, Level: zapcore.ErrorLevel}

	cleanup, err := rpk.ExposeRedpandaCluster(ctx, dot, w, wErr)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	require.NoError(t, err)

	// Start up our manager
	mgr, err := manager.New(cluster.RESTConfig(), manager.Options{
		BaseContext: func() context.Context {
			return ctx
		},
	})
	require.NoError(t, err)

	previous, exist := os.LookupEnv(controllers.EnvHelmReleaseNameKey)
	require.NoError(t, os.Setenv(controllers.EnvHelmReleaseNameKey, rpRelease.Name))
	t.Cleanup(func() {
		if exist {
			os.Setenv(controllers.EnvHelmReleaseNameKey, previous)
		}
	})

	r := controllers.DecommissionReconciler{Client: c, OperatorMode: false, DecommissionWaitInterval: time.Second, BuildAdminAPI: func(_, _ string, _ int32, _ *int, _ map[string]interface{}) (*rpadmin.AdminAPI, error) {
		return rpadmin.NewAdminAPI([]string{rpk.GetAdminClientURL()}, &rpadmin.NopAuth{}, &tls.Config{InsecureSkipVerify: true})
	}}
	require.NoError(t, r.SetupWithManager(mgr))

	pvc := controllers.RedpandaNodePVCReconciler{Client: c, OperatorMode: false}
	require.NoError(t, pvc.SetupWithManager(mgr))

	tgo(t, ctx, func(ctx context.Context) error {
		return mgr.Start(ctx)
	})

	iteration := 0
	for {
		if iteration == 10 {
			break
		}
		require.NoError(t, cluster.CreateNode())
		node, err := picNodeToDelete(ctx, c, dot)
		require.NoError(t, err)
		t.Logf("deleting node %q", node.Name)
		require.NoError(t, cluster.DeleteNode(node.Name))
		require.NoError(t, c.Delete(ctx, node))

		health, err := rpk.GetClusterHealth(ctx)
		require.Eventually(t, func() bool {
			health, err = rpk.GetClusterHealth(ctx)
			if err != nil {
				return false
			}

			return health["is_healthy"] == false
		}, time.Minute*5, time.Second*10)

		require.Eventually(t, func() bool {
			health, err = rpk.GetClusterHealth(ctx)
			if err != nil {
				return false
			}

			return health["is_healthy"] == true &&
				health["leaderless_count"] == float64(0) &&
				health["under_replicated_count"] == float64(0) &&
				len(health["unhealthy_reasons"].([]any)) == 0
		}, time.Minute*6, time.Second*10)

		iteration++
	}
}

func picNodeToDelete(ctx context.Context, c client.Client, dot *helmette.Dot) (*corev1.Node, error) {
	sel := redpanda.StatefulSetPodLabelsSelector(dot)

	PodList := &corev1.PodList{}
	err := c.List(ctx, PodList, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(sel),
		Namespace:     dot.Release.Namespace,
	})
	if err != nil {
		return nil, err
	}

	nodeName := ""
	for _, pod := range PodList.Items {
		nodeName = pod.Spec.NodeName
	}

	n := corev1.Node{}
	err = c.Get(ctx, client.ObjectKey{Name: nodeName}, &n)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func enableOperatorController() *struct {
	ConfigWatcher *struct {
		Enabled           *bool                   "json:\"enabled,omitempty\""
		ExtraVolumeMounts *string                 "json:\"extraVolumeMounts,omitempty\""
		Resources         map[string]any          "json:\"resources,omitempty\""
		SecurityContext   *corev1.SecurityContext "json:\"securityContext,omitempty\""
	} "json:\"configWatcher,omitempty\""
	Controllers *struct {
		Image *struct {
			Tag        *redpanda.ImageTag "json:\"tag,omitempty\" jsonschema:\"required,default=Chart.appVersion\""
			Repository *string            "json:\"repository,omitempty\" jsonschema:\"required,default=docker.redpanda.com/redpandadata/redpanda-operator\""
		} "json:\"image,omitempty\""
		Enabled            *bool                   "json:\"enabled,omitempty\""
		CreateRBAC         *bool                   "json:\"createRBAC,omitempty\""
		Resources          any                     "json:\"resources,omitempty\""
		SecurityContext    *corev1.SecurityContext "json:\"securityContext,omitempty\""
		HealthProbeAddress *string                 "json:\"healthProbeAddress,omitempty\""
		MetricsAddress     *string                 "json:\"metricsAddress,omitempty\""
		Run                []string                "json:\"run,omitempty\""
	} "json:\"controllers,omitempty\""
} {
	return &struct {
		ConfigWatcher *struct {
			Enabled           *bool                   "json:\"enabled,omitempty\""
			ExtraVolumeMounts *string                 "json:\"extraVolumeMounts,omitempty\""
			Resources         map[string]any          "json:\"resources,omitempty\""
			SecurityContext   *corev1.SecurityContext "json:\"securityContext,omitempty\""
		} "json:\"configWatcher,omitempty\""
		Controllers *struct {
			Image *struct {
				Tag        *redpanda.ImageTag "json:\"tag,omitempty\" jsonschema:\"required,default=Chart.appVersion\""
				Repository *string            "json:\"repository,omitempty\" jsonschema:\"required,default=docker.redpanda.com/redpandadata/redpanda-operator\""
			} "json:\"image,omitempty\""
			Enabled            *bool                   "json:\"enabled,omitempty\""
			CreateRBAC         *bool                   "json:\"createRBAC,omitempty\""
			Resources          any                     "json:\"resources,omitempty\""
			SecurityContext    *corev1.SecurityContext "json:\"securityContext,omitempty\""
			HealthProbeAddress *string                 "json:\"healthProbeAddress,omitempty\""
			MetricsAddress     *string                 "json:\"metricsAddress,omitempty\""
			Run                []string                "json:\"run,omitempty\""
		} "json:\"controllers,omitempty\""
	}{
		Controllers: &struct {
			Image *struct {
				Tag        *redpanda.ImageTag "json:\"tag,omitempty\" jsonschema:\"required,default=Chart.appVersion\""
				Repository *string            "json:\"repository,omitempty\" jsonschema:\"required,default=docker.redpanda.com/redpandadata/redpanda-operator\""
			} "json:\"image,omitempty\""
			Enabled            *bool                   "json:\"enabled,omitempty\""
			CreateRBAC         *bool                   "json:\"createRBAC,omitempty\""
			Resources          any                     "json:\"resources,omitempty\""
			SecurityContext    *corev1.SecurityContext "json:\"securityContext,omitempty\""
			HealthProbeAddress *string                 "json:\"healthProbeAddress,omitempty\""
			MetricsAddress     *string                 "json:\"metricsAddress,omitempty\""
			Run                []string                "json:\"run,omitempty\""
		}{
			Enabled: ptr.To(true),
		},
	}
}

// tgo is a helper for ensuring that goroutines spawned in test cases are
// appropriately shutdown.
func tgo(t *testing.T, ctx context.Context, fn func(context.Context) error) {
	doneCh := make(chan struct{})
	ctx, cancel := context.WithCancel(ctx)

	t.Cleanup(func() {
		cancel()
		<-doneCh
	})

	go func() {
		assert.NoError(t, fn(ctx))
		close(doneCh)
	}()
}
