package vectorized

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/go-logr/logr/testr"
	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	redpanda "github.com/redpanda-data/redpanda-operator/charts/redpanda/v5/client"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	crds "github.com/redpanda-data/redpanda-operator/operator/config/crd/bases"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/admin"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/labels"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources/types"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/secrets"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
	"github.com/redpanda-data/redpanda-operator/pkg/kube/kubetest"
	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/log"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

func TestClusterControllerGolden(t *testing.T) {
	ctx := log.IntoContext(t.Context(), testr.New(t))

	ctl := kubetest.NewEnv(t, kube.Options{
		Options: client.Options{
			Scheme: controller.V1Scheme,
		},
	})

	require.NoError(t, kube.ApplyAllAndWait(ctx, ctl, func(crd *apiextensionsv1.CustomResourceDefinition, err error) (bool, error) {
		if err != nil {
			return false, err
		}

		for _, cond := range crd.Status.Conditions {
			if cond.Type == apiextensionsv1.Established {
				return cond.Status == apiextensionsv1.ConditionTrue, nil
			}
		}

		return false, nil
	}, crds.All()...))

	manager, err := ctrl.NewManager(ctl.RestConfig(), manager.Options{
		Logger:  testr.New(t),
		Scheme:  controller.V1Scheme,
		Metrics: server.Options{BindAddress: "0"},
	})
	require.NoError(t, err)

	adminAPIs := map[string]*admin.MockAdminAPI{}

	ctrl := &ClusterReconciler{
		Log:    testr.New(t),
		Client: manager.GetClient(),
		Scheme: manager.GetClient().Scheme(),
		configuratorSettings: resources.ConfiguratorSettings{
			ImagePullPolicy: corev1.PullIfNotPresent,
		},
		CloudSecretsExpander: &secrets.CloudExpander{},
		AdminAPIClientFactory: func(ctx context.Context, k8sClient client.Reader, redpandaCluster *vectorizedv1alpha1.Cluster, fqdn string, adminTLSProvider types.AdminTLSConfigProvider, dialer redpanda.DialContextFunc, timeout time.Duration, pods ...string) (admin.AdminAPIClient, error) {
			if api, ok := adminAPIs[redpandaCluster.Name]; ok {
				return api, nil
			}

			api := &admin.MockAdminAPI{Log: testr.New(t)}

			api.RegisterPropertySchema("auto_create_topics_enabled", rpadmin.ConfigPropertyMetadata{NeedsRestart: false, Type: "boolean"})
			api.RegisterPropertySchema("cloud_storage_segment_max_upload_interval_sec", rpadmin.ConfigPropertyMetadata{NeedsRestart: true, Type: "integer"})
			api.RegisterPropertySchema("log_segment_size", rpadmin.ConfigPropertyMetadata{NeedsRestart: true, Type: "integer"})
			api.RegisterPropertySchema("enable_rack_awareness", rpadmin.ConfigPropertyMetadata{NeedsRestart: false, Type: "boolean"})

			adminAPIs[redpandaCluster.Name] = api

			return adminAPIs[redpandaCluster.Name], nil
		},
	}

	require.NoError(t, ctrl.SetupWithManager(manager))

	go func() {
		require.NoError(t, manager.Start(ctx))
	}()

	cluster := &vectorizedv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster",
			Namespace: "default",
		},
		Spec: vectorizedv1alpha1.ClusterSpec{
			Replicas: ptr.To[int32](1),
			Configuration: vectorizedv1alpha1.RedpandaConfig{
				AdminAPI: []vectorizedv1alpha1.AdminAPI{
					{Port: 1234},
				},
				KafkaAPI: []vectorizedv1alpha1.KafkaAPI{
					{Port: 4321},
				},
			},
		},
	}

	require.NoError(t, kube.ApplyAndWait(ctx, ctl, cluster, func(cluster *vectorizedv1alpha1.Cluster, err error) (bool, error) {
		if err != nil {
			return false, err
		}

		for _, cond := range cluster.Status.Conditions {
			if cond.Type == vectorizedv1alpha1.ClusterConfiguredConditionType {
				return cond.Status == corev1.ConditionTrue, nil
			}
		}
		return false, nil
	}))

	var lists []client.ObjectList
	for gvk := range controller.V1Scheme.AllKnownTypes() {
		if strings.HasSuffix(gvk.Kind, "List") {
			obj, err := controller.V1Scheme.New(gvk)
			require.NoError(t, err)

			if l, ok := obj.(client.ObjectList); ok {
				lists = append(lists, l)
			}
		}
	}

	for _, l := range lists {
		err := ctl.List(ctx, l)
		if errors.Is(err, &meta.NoKindMatchError{}) {
			continue
		}
		require.NoError(t, err)

		objs, err := meta.ExtractList(l)
		require.NoError(t, err)

		for _, obj := range objs {
			o := obj.(client.Object)

			if !labels.ForCluster(cluster).AsClientSelector().Matches(k8slabels.Set(o.GetLabels())) {
				continue
			}

			t.Logf("%T: %s/%s", o, o.GetNamespace(), o.GetName())
		}
	}

	// unstructured.Unstructured
	// unstructured.SetNestedSlice

	// kube.EncodeYAML()
	// yaml.Marshal
	// testutil.AssertGolden()
}
