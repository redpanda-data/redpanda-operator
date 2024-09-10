package redpanda

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	cmapiv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"github.com/fluxcd/pkg/apis/meta"
	"github.com/go-logr/logr/testr"
	"github.com/redpanda-data/helm-charts/charts/redpanda"
	"github.com/redpanda-data/helm-charts/pkg/gotohelm/helmette"
	"github.com/redpanda-data/helm-charts/pkg/helm/helmtest"
	"github.com/redpanda-data/helm-charts/pkg/testutil"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/src/go/k8s/internal/testutils"
	"github.com/redpanda-data/redpanda-operator/src/go/k8s/pkg/k3d"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

func TestRedpandaReconciler(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := testr.New(t).V(0)
	log.SetLogger(logger)
	ctx = log.IntoContext(ctx, logger)

	var cluster *k3d.Cluster
	var err error
	if val, ok := os.LookupEnv("EXISTING_K3D_CLUSTER"); ok && val != "" {
		cluster, err = k3d.ExistingCluster(val)
		require.NoError(t, err)
		t.Logf("imported cluster %T %q", cluster, cluster.Name)
	} else {
		cluster, err = k3d.NewCluster(t.Name())
		require.NoError(t, err)
		t.Logf("created cluster %T %q", cluster, cluster.Name)
	}

	t.Cleanup(func() {
		if testutil.Retain() {
			t.Logf("retain flag is set; not deleting cluster %q", cluster.Name)
			return
		}
		t.Logf("Deleting cluster %q", cluster.Name)
		require.NoError(t, cluster.Cleanup())
	})

	h := helmtest.Setup(t)

	n := h.Namespaced(t)

	require.NoError(t, redpandav1alpha2.AddToScheme(scheme.Scheme))
	require.NoError(t, cmapiv1.AddToScheme(scheme.Scheme))

	c, err := client.New(cluster.RESTConfig(), client.Options{})
	require.NoError(t, err)

	t.Cleanup(func() {
		if testutil.Retain() {
			t.Logf("retain flag is set; not deleting namespace %q", n.Namespace())
			return
		}
		t.Logf("Deleting namespace %q", n.Namespace())
		require.NoError(t, c.Delete(context.Background(), &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: n.Namespace()},
		}))
	})

	opt := envtest.CRDInstallOptions{
		Scheme: scheme.Scheme,
		Paths:  []string{filepath.Join("../", "../", "../", "config", "crd", "bases")},
	}

	_, err = envtest.InstallCRDs(cluster.RESTConfig(), opt)
	require.NoError(t, err)

	rpName := "smoke-test"

	rp := &redpandav1alpha2.Redpanda{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rpName,
			Namespace: n.Namespace(),
		},
		Spec: redpandav1alpha2.RedpandaSpec{
			ChartRef: redpandav1alpha2.ChartRef{},
			ClusterSpec: &redpandav1alpha2.RedpandaClusterSpec{
				Image:       &redpandav1alpha2.RedpandaImage{Tag: ptr.To("v24.2.4")},
				Console:     &redpandav1alpha2.RedpandaConsole{Enabled: ptr.To(false)},
				Statefulset: &redpandav1alpha2.Statefulset{Replicas: ptr.To(1)},
			},
		},
	}
	require.NoError(t, c.Create(ctx, rp))

	t.Cleanup(func() {
		t.Log("Deleting Redpanda CR smoke-test")
		err = c.Delete(context.Background(), &redpandav1alpha2.Redpanda{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rpName,
				Namespace: n.Namespace(),
			},
		})

		require.NoError(t, client.IgnoreNotFound(err))
	})

	// Start up our manager
	mgr, err := manager.New(cluster.RESTConfig(), manager.Options{
		Metrics: metricsserver.Options{BindAddress: "0"},
		BaseContext: func() context.Context {
			return log.IntoContext(ctx, logger)
		},
	})
	require.NoError(t, err)

	r := RedpandaReconciler{Client: c}
	require.NoError(t, r.SetupWithManager(ctx, mgr))

	testutils.TGo(t, ctx, func(ctx context.Context) error {
		return mgr.Start(log.IntoContext(ctx, logger))
	})

	release := helmette.Release{
		Name:      rpName,
		Namespace: n.Namespace(),
		// TODO What this Service is used for?
		//Service:   "",
		// TODO Should those fields matter in Redpanda, Console and Connectors?
		//IsUpgrade: false,
		//IsInstall: false,
	}

	dot, err := redpanda.Dot(release, redpanda.PartialValues{})
	require.NoError(t, err)
	sel := redpanda.StatefulSetPodLabelsSelector(dot)

	// Wait until all our replicas are up
	require.Eventually(t, func() bool {
		require.NoError(t, c.Get(ctx, types.NamespacedName{Name: rp.Name, Namespace: n.Namespace()}, rp))
		con := apimeta.FindStatusCondition(rp.Status.Conditions, meta.ReadyCondition)

		return con != nil && con.Status == metav1.ConditionTrue
	}, 2*time.Minute, 5*time.Second)

	var pods corev1.PodList
	require.NoError(t, c.List(ctx, &pods, &client.ListOptions{
		Namespace:     n.Namespace(),
		LabelSelector: labels.SelectorFromValidatedSet(sel),
		FieldSelector: fields.SelectorFromSet(fields.Set{
			"status.phase": string(corev1.PodRunning),
		}),
	}))

	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: rp.Name, Namespace: rp.Namespace}, rp))

	rp.Spec.ClusterSpec.Listeners = &redpandav1alpha2.Listeners{
		Admin: &redpandav1alpha2.Admin{Port: ptr.To(2444)},
	}
	rp.SetGroupVersionKind(redpandav1alpha2.GroupVersion.WithKind("Redpanda"))
	rp.SetManagedFields(nil)
	require.NoError(t, c.Patch(ctx, rp, client.Apply, client.ForceOwnership, client.FieldOwner("redpanda-operator")))

	require.Eventually(t, func() bool {
		require.NoError(t, c.Get(ctx, types.NamespacedName{Name: rp.Name, Namespace: n.Namespace()}, rp))
		con := apimeta.FindStatusCondition(rp.Status.Conditions, meta.ReadyCondition)

		return con != nil && con.Status == metav1.ConditionTrue && rp.Status.ObservedGeneration == 2
	}, 2*time.Minute, 5*time.Second)

	var svc corev1.Service
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: rpName, Namespace: n.Namespace()}, &svc))

	require.Contains(t, svc.Spec.Ports, corev1.ServicePort{
		Name:       "admin",
		Port:       2444,
		TargetPort: intstr.FromInt32(2444),
		Protocol:   corev1.ProtocolTCP,
	})

	require.NoError(t, c.Delete(ctx, rp))

	require.Eventually(t, func() bool {
		var pods corev1.PodList
		err = c.List(ctx, &pods, &client.ListOptions{
			Namespace:     n.Namespace(),
			LabelSelector: labels.SelectorFromValidatedSet(sel),
			FieldSelector: fields.SelectorFromSet(fields.Set{
				"status.phase": string(corev1.PodRunning),
			}),
		})
		require.NoError(t, err)

		return len(pods.Items) == 0
	}, 2*time.Minute, 5*time.Second)

	require.Eventually(t, func() bool {
		var svcList corev1.ServiceList
		err = c.List(ctx, &svcList, &client.ListOptions{
			Namespace: n.Namespace(),
		})
		require.NoError(t, err)

		return len(svcList.Items) == 0
	}, 2*time.Minute, 5*time.Second)

	require.Eventually(t, func() bool {
		var pdbs policyv1.PodDisruptionBudgetList
		err = c.List(ctx, &pdbs, &client.ListOptions{
			Namespace: n.Namespace(),
		})
		require.NoError(t, err)

		return len(pdbs.Items) == 0
	}, 2*time.Minute, 5*time.Second)
}
