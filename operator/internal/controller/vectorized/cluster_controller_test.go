package vectorized

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"github.com/cockroachdb/errors"
	"github.com/go-logr/logr/testr"
	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	redpanda "github.com/redpanda-data/redpanda-operator/charts/redpanda/v5/client"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	crds "github.com/redpanda-data/redpanda-operator/operator/config/crd/bases"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/admin"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/labels"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources"
	resourcetypes "github.com/redpanda-data/redpanda-operator/operator/pkg/resources/types"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/secrets"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
	"github.com/redpanda-data/redpanda-operator/pkg/kube/kubetest"
	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/log"
)

func TestClusterControllerGolden(t *testing.T) {
	tests := []struct {
	name           string
	clusterDef     func() *vectorizedv1alpha1.Cluster
	reconcilerFunc func(t *testing.T, mgr manager.Manager) *ClusterReconciler
	goldenFile     string
	normalizerFunc func(ctx context.Context, testScheme *runtime.Scheme, cluster *vectorizedv1alpha1.Cluster, t *testing.T, ctl *kube.Ctl) []client.Object
}{
		{
			name:           "minimal_cluster",
			clusterDef:     minimalClusterDef,
			reconcilerFunc: minimalReconciler,
			normalizerFunc: minimalClusterNormalizer,
			goldenFile:     "testdata/cluster-resources.golden.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Unified scheme gives issues so we're using a home bake
			testScheme := runtime.NewScheme()
			for _, fn := range []func(s *runtime.Scheme) error{
				clientgoscheme.AddToScheme,
				certmanagerv1.AddToScheme,
				vectorizedv1alpha1.AddToScheme,
				apiextensionsv1.AddToScheme,
			} {
				require.NoError(t, fn(testScheme))
			}
			ctx := log.IntoContext(t.Context(), testr.New(t))

			ctl := kubetest.NewEnv(t, kube.Options{
				Options: client.Options{
					Scheme: testScheme,
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

			mgr, err := ctrl.NewManager(ctl.RestConfig(), manager.Options{
				Logger:  testr.New(t),
				Scheme:  testScheme,
				Metrics: server.Options{BindAddress: "0"},
			})
			require.NoError(t, err)

			reconciler := tt.reconcilerFunc(t, mgr)
			err = ctrl.NewControllerManagedBy(mgr).
				For(&vectorizedv1alpha1.Cluster{}).
				Owns(&appsv1.StatefulSet{}).
				Owns(&corev1.Service{}).
				Named("cluster-" + fmt.Sprintf("%d", time.Now().UnixNano())).
				WithOptions(controller.Options{
					SkipNameValidation: ptr.To(true),
					NeedLeaderElection: ptr.To(false),
				}).
				Complete(reconciler)
			require.NoError(t, err)

			mgrCtx, mgrCancel := context.WithCancel(ctx)
			go func() {
				require.NoError(t, mgr.Start(mgrCtx))
			}()


			cluster := tt.clusterDef()
			require.NoError(t, ctl.Apply(ctx, cluster))
			require.NoError(t, ctl.WaitFor(ctx, cluster, func(obj kube.Object, err error) (bool, error) {
				if err != nil {
					return false, err
				}

				cl := obj.(*vectorizedv1alpha1.Cluster)
				fmt.Printf("conditions: %s", cl.Status.Conditions)
				return len(cl.Status.Conditions) > 0, nil
			}))

			defer cleanupTest(t, ctx, ctl, cluster, mgrCancel)

			allClusterResources := tt.normalizerFunc(ctx, testScheme, cluster, t, ctl)

			slices.SortStableFunc(allClusterResources, func(a, b client.Object) int {
				return strings.Compare(fmt.Sprintf("%T/%s/%s", a, a.GetNamespace(), a.GetName()),
					fmt.Sprintf("%T/%s/%s", b, b.GetNamespace(), b.GetName()))
			})

			allResourcesYAML, err := kube.EncodeYAML(testScheme, allClusterResources...)
			require.NoError(t, err)
			testutil.AssertGolden(t, testutil.YAML, tt.goldenFile, allResourcesYAML)
		})
	}
}

func minimalReconciler(t *testing.T, mgr manager.Manager) *ClusterReconciler {
	adminAPIs := map[string]*admin.MockAdminAPI{}

	return (&ClusterReconciler{
		Client:                   mgr.GetClient(),
		Log:                      testr.New(t),
		Scheme:                   mgr.GetScheme(),
		DecommissionWaitInterval: 8 * time.Second,
		MetricsTimeout:           8 * time.Second,
		Timeout:                  10 * time.Second,
		CloudSecretsExpander:     &secrets.CloudExpander{},
		AdminAPIClientFactory: func(ctx context.Context, k8sClient client.Reader, redpandaCluster *vectorizedv1alpha1.Cluster, fqdn string, adminTLSProvider resourcetypes.AdminTLSConfigProvider, dialer redpanda.DialContextFunc, timeout time.Duration, pods ...string) (admin.AdminAPIClient, error) {
			if api, ok := adminAPIs[redpandaCluster.Name]; ok {
				return api, nil
			}

			api := &admin.MockAdminAPI{Log: testr.New(t)}

			api.RegisterPropertySchema("auto_create_topics_enabled", rpadmin.ConfigPropertyMetadata{NeedsRestart: false, Type: "boolean"})
			api.RegisterPropertySchema("cloud_storage_segment_max_upload_interval_sec", rpadmin.ConfigPropertyMetadata{NeedsRestart: true, Type: "integer"})
			api.RegisterPropertySchema("log_segment_size", rpadmin.ConfigPropertyMetadata{NeedsRestart: true, Type: "integer"})
			api.RegisterPropertySchema("enable_rack_awareness", rpadmin.ConfigPropertyMetadata{NeedsRestart: false, Type: "boolean"})
			api.SetProperty("auto_create_topics_enabled", false)
			api.SetProperty("cloud_storage_segment_max_upload_interval_sec", 1800)
			api.SetProperty("log_segment_size", 536870912)
			api.SetProperty("enable_rack_awareness", true)

			adminAPIs[redpandaCluster.Name] = api

			return adminAPIs[redpandaCluster.Name], nil
		},
	}).WithClusterDomain("cluster.local").WithConfiguratorSettings(resources.ConfiguratorSettings{
		ImagePullPolicy: corev1.PullIfNotPresent,
	})
}

func minimalClusterDef() *vectorizedv1alpha1.Cluster {
	return &vectorizedv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster",
			Namespace: "default",
		},
		Spec: vectorizedv1alpha1.ClusterSpec{
			Replicas: ptr.To[int32](1),
			AdditionalConfiguration: map[string]string{
				"pandaproxy_client.produce_batch_size_bytes":    "2097152",
				"pandaproxy_client.consumer_session_timeout_ms": "10000",
			},
			ClusterConfiguration: map[string]vectorizedv1alpha1.ClusterConfigValue{
				"auto_create_topics_enabled":                    {Repr: ptr.To(vectorizedv1alpha1.YAMLRepresentation("true"))},
				"cloud_storage_segment_max_upload_interval_sec": {Repr: ptr.To(vectorizedv1alpha1.YAMLRepresentation("3600"))},
				"log_segment_size":                              {Repr: ptr.To(vectorizedv1alpha1.YAMLRepresentation("1073741824"))},
				"enable_rack_awareness":                         {Repr: ptr.To(vectorizedv1alpha1.YAMLRepresentation("false"))},
			},
			Configuration: vectorizedv1alpha1.RedpandaConfig{
				KafkaAPI: []vectorizedv1alpha1.KafkaAPI{
					{
						Name: "kafka",
						Port: 9092,
						External: vectorizedv1alpha1.ExternalConnectivityConfig{
							Enabled: false,
						},
					},
					{
						Name: "kafka-external",
						Port: 30093,
						External: vectorizedv1alpha1.ExternalConnectivityConfig{
							Enabled: true,
							Bootstrap: &vectorizedv1alpha1.LoadBalancerConfig{
								Port: 9094,
							},
						},
					},
				},
				AdminAPI: []vectorizedv1alpha1.AdminAPI{
					{
						Port: 9644,
						External: vectorizedv1alpha1.ExternalConnectivityConfig{
							Enabled: false,
						},
					},
				},
				RPCServer: vectorizedv1alpha1.SocketAddress{
					Port: 33145,
				},
				PandaproxyAPI: []vectorizedv1alpha1.PandaproxyAPI{
					{
						Name: "proxy-internal",
						Port: 8082,
						External: vectorizedv1alpha1.PandaproxyExternalConnectivityConfig{
							ExternalConnectivityConfig: vectorizedv1alpha1.ExternalConnectivityConfig{
								Enabled: false,
							},
						},
					},
					{
						Name: "proxy-external",
						External: vectorizedv1alpha1.PandaproxyExternalConnectivityConfig{
							ExternalConnectivityConfig: vectorizedv1alpha1.ExternalConnectivityConfig{
								Enabled:   true,
								Subdomain: "test.example.com",
							},
							Ingress: &vectorizedv1alpha1.IngressConfig{
								Enabled:  ptr.To(true),
								Endpoint: "proxy",
								Annotations: map[string]string{
									"nginx.ingress.kubernetes.io/ssl-passthrough": "true",
								},
							},
						},
					},
				},
				SchemaRegistry: &vectorizedv1alpha1.SchemaRegistryAPI{
					Port: 8081,
				},
			},
			Resources: vectorizedv1alpha1.RedpandaResourceRequirements{
				ResourceRequirements: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("2Gi"),
					},
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("2Gi"),
					},
				},
			},
		},
	}
}

func minimalClusterNormalizer(ctx context.Context, testScheme *runtime.Scheme, cluster *vectorizedv1alpha1.Cluster, t *testing.T, ctl *kube.Ctl) []client.Object {
	var allClusterResources []client.Object
	var lists []client.ObjectList
	for gvk := range testScheme.AllKnownTypes() {
		if strings.HasSuffix(gvk.Kind, "List") {
			obj, err := testScheme.New(gvk)
			require.NoError(t, err)
			if l, ok := obj.(client.ObjectList); ok {
				lists = append(lists, l)
			}
		}
	}

	clusterSelector := labels.ForCluster(cluster).AsClientSelector()
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

			if !clusterSelector.Matches(k8slabels.Set(o.GetLabels())) {
				continue
			}

			allClusterResources = append(allClusterResources, o)
			t.Logf("%T: %s/%s", o, o.GetNamespace(), o.GetName())
		}
	}

	for _, rs := range allClusterResources {
		rs.SetResourceVersion("1")
		rs.SetUID("1234")
		rs.SetCreationTimestamp(metav1.Unix(0, 0))
		rs.SetManagedFields(nil)
		if ownerRefs := rs.GetOwnerReferences(); len(ownerRefs) > 0 {
			for i := range ownerRefs {
				ownerRefs[i].UID = "normalized-uid-1234"
			}
			rs.SetOwnerReferences(ownerRefs)
		}

		if annotations := rs.GetAnnotations(); annotations != nil {
			annotations["redpanda.com/last-applied"] = "applied"
		}

		switch obj := rs.(type) {
		case *corev1.Service:
			for i := range obj.Spec.Ports {
				if obj.Spec.Ports[i].NodePort != 0 {
					obj.Spec.Ports[i].NodePort = 9999
				}
			}
			if obj.Spec.ClusterIP != "" && obj.Spec.ClusterIP != "None" {
				obj.Spec.ClusterIP = "10.99.99.99"
			}
			for i := range obj.Spec.ClusterIPs {
				if obj.Spec.ClusterIPs[i] != "None" {
					obj.Spec.ClusterIPs[i] = "10.99.99.99"
				}
			}

		case *appsv1.StatefulSet:
			for i := range obj.Spec.Template.Spec.Containers {
				for j := range obj.Spec.Template.Spec.Containers[i].Ports {
					if obj.Spec.Template.Spec.Containers[i].Ports[j].HostPort != 0 {
						obj.Spec.Template.Spec.Containers[i].Ports[j].HostPort = 9999
					}
				}
			}

			for i := range obj.Spec.Template.Spec.InitContainers {
				for j := range obj.Spec.Template.Spec.InitContainers[i].Env {
					if obj.Spec.Template.Spec.InitContainers[i].Env[j].Name == "PROXY_HOST_PORT" {
						obj.Spec.Template.Spec.InitContainers[i].Env[j].Value = "9999"
					}
				}
			}
		}
	}
	return allClusterResources
}

func cleanupTest(t *testing.T, ctx context.Context, ctl *kube.Ctl, cluster *vectorizedv1alpha1.Cluster, mgrCancel context.CancelFunc) {
	if err := ctl.Delete(ctx, cluster); err != nil {
		t.Logf("Failed to delete cluster: %v", err)
	}
	if err := ctl.WaitFor(ctx, cluster, func(obj kube.Object, err error) (bool, error) {
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	}); err != nil {
		t.Logf("Failed to wait for cluster deletion: %v", err)
	}
	mgrCancel()
}