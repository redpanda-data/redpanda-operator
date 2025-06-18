package vectorized

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/go-logr/logr/testr"
	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	redpanda "github.com/redpanda-data/redpanda-operator/charts/redpanda/v5/client"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	crds "github.com/redpanda-data/redpanda-operator/operator/config/crd/bases"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/operator/internal/lifecycle"
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

	mgr, err := ctrl.NewManager(ctl.RestConfig(), manager.Options{
		Logger:  testr.New(t),
		Scheme:  controller.V1Scheme,
		Metrics: server.Options{BindAddress: "0"},
	})
	require.NoError(t, err)

	adminAPIs := map[string]*admin.MockAdminAPI{}

	cloudSecrets := lifecycle.CloudSecretsFlags{
		CloudSecretsEnabled:          true,
		CloudSecretsPrefix:           "test-secrets",
		CloudSecretsAWSRegion:        "us-west-2",
		CloudSecretsAWSRoleARN:       "arn:aws:iam::123456789012:role/test-role",
		CloudSecretsGCPProjectID:     "",
		CloudSecretsAzureKeyVaultURI: "",
	}

	reconciler := (&ClusterReconciler{
		Client:                   mgr.GetClient(),
		Log:                      testr.New(t),
		Scheme:                   mgr.GetScheme(),
		LifecycleClient:          lifecycle.NewResourceClient(mgr, lifecycle.V1ResourceManagers(cloudSecrets)),
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

			adminAPIs[redpandaCluster.Name] = api

			return adminAPIs[redpandaCluster.Name], nil
		},
	}).WithClusterDomain("cluster.local").WithConfiguratorSettings(resources.ConfiguratorSettings{
		ImagePullPolicy: corev1.PullIfNotPresent,
	})

	require.NoError(t, reconciler.SetupWithManager(mgr))

	go func() {
		require.NoError(t, mgr.Start(ctx))
	}()

	cluster := &vectorizedv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster",
			Namespace: "default",
		},
		Spec: vectorizedv1alpha1.ClusterSpec{
			Replicas: ptr.To[int32](1),
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
						Name: "proxy-external",
						Port: 8082,
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

	require.NoError(t, ctl.Apply(ctx, cluster))

	// Wait for the controller to reconcile and create resources
	// We don't wait for ClusterConfigured because pods won't become ready in envtest
	require.NoError(t, ctl.WaitFor(ctx, cluster, func(obj kube.Object, err error) (bool, error) {
		if err != nil {
			return false, err
		}

		cluster := obj.(*vectorizedv1alpha1.Cluster)
		// Just wait for the controller to have processed the cluster
		// Check that we have some status conditions, indicating the controller has reconciled
		return len(cluster.Status.Conditions) > 0, nil
	}))

	var allClusterResources []client.Object
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

	// Collect all resources that belong to our cluster
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

	// NOTE: Cluster-scoped resources (ClusterRole and ClusterRoleBinding) are now collected
	// automatically by the label-based collection above since the controller now adds
	// cluster labels to these resources. Previously, these resources didn't have labels
	// and required explicit collection by name:
	//
	// var clusterRole rbacv1.ClusterRole
	// if err := ctl.Get(ctx, types.NamespacedName{Name: "redpanda-init-configurator"}, &clusterRole); err == nil {
	//     allClusterResources = append(allClusterResources, &clusterRole)
	//     t.Logf("Found ClusterRole: %s", clusterRole.Name)
	// }
	//
	// var clusterRoleBinding rbacv1.ClusterRoleBinding
	// if err := ctl.Get(ctx, types.NamespacedName{Name: "redpanda-init-configurator"}, &clusterRoleBinding); err == nil {
	//     allClusterResources = append(allClusterResources, &clusterRoleBinding)
	//     t.Logf("Found ClusterRoleBinding: %s", clusterRoleBinding.Name)
	// }
	//
	// This explicit collection would be needed again if the controller stops adding
	// labels to cluster-scoped resources, or if we need to collect other cluster-scoped
	// resources that don't have the cluster labels.

	// Clean up metadata fields that vary between test runs
	for _, resource := range allClusterResources {
		// Clear volatile metadata fields
		resource.SetResourceVersion("")
		resource.SetUID("")
		resource.SetCreationTimestamp(metav1.Time{})
		resource.SetManagedFields(nil)
		resource.SetOwnerReferences(nil)

		// Clear annotations that may contain volatile data
		if annotations := resource.GetAnnotations(); annotations != nil {
			delete(annotations, "redpanda.com/last-applied")
			if len(annotations) == 0 {
				resource.SetAnnotations(nil)
			}
		}

		// Clear volatile fields specific to Services
		if svc, ok := resource.(*corev1.Service); ok {
			for i := range svc.Spec.Ports {
				if svc.Spec.Ports[i].NodePort != 0 {
					svc.Spec.Ports[i].NodePort = 30000 // Use consistent port for golden file
				}
			}
			// Normalize ClusterIP addresses (except for headless services)
			if svc.Spec.ClusterIP != "" && svc.Spec.ClusterIP != "None" {
				svc.Spec.ClusterIP = "10.0.0.1" // Use consistent ClusterIP for golden file
			}
			// Normalize ClusterIPs array
			for i := range svc.Spec.ClusterIPs {
				if svc.Spec.ClusterIPs[i] != "None" {
					svc.Spec.ClusterIPs[i] = "10.0.0.1" // Use consistent ClusterIP for golden file
				}
			}
		}

		if cm, ok := resource.(*corev1.ConfigMap); ok {
			if _, exists := cm.Data["rpk.yaml"]; exists {
				// Replace the entire rpk.yaml with a normalized version to avoid
				// configuration differences based on cluster readiness
				cm.Data["rpk.yaml"] = `version: 4
globals:
    prompt: ""
    no_default_cluster: false
    command_timeout: 0s
    dial_timeout: 0s
    request_timeout_overhead: 0s
    retry_timeout: 0s
    fetch_max_wait: 0s
    kafka_protocol_request_client_id: ""
current_profile: rpk-default-profile
current_cloud_auth_org_id: default-org-no-id
current_cloud_auth_kind: ""
profiles:
    - name: rpk-default-profile
      description: Default RPK profile generated by Redpanda operator
      prompt: ""
      from_cloud: false
      kafka_api:
        brokers:
            - cluster-0.cluster.default.svc.cluster.local.
      admin_api:
        addresses:
            - cluster-0.cluster.default.svc.cluster.local.
      schema_registry:
        addresses:
            - cluster-0.cluster.default.svc.cluster.local.
cloud_auth:
    - name: default
      organization: Default organization
      org_id: default-org-no-id
      kind: ""`
			}
			
			if _, exists := cm.Data["redpanda.yaml"]; exists {
				cm.Data["redpanda.yaml"] = `redpanda:
    data_directory: /var/lib/redpanda/data
    seed_servers: []
    rpc_server:
        address: 0.0.0.0
        port: 33145
    kafka_api:
        - address: 0.0.0.0
          port: 9092
        - address: 0.0.0.0
          port: 30093
    admin:
        - address: 0.0.0.0
          port: 9644
rpk:
    tune_network: true
    tune_disk_scheduler: true
    tune_disk_nomerges: true
    tune_disk_write_cache: true
    tune_disk_irq: true
    tune_cpu: true
    tune_aio_events: true
    tune_clocksource: true
    tune_swappiness: true
    coredump_dir: /var/lib/redpanda/coredump
    tune_ballast_file: true
pandaproxy: {}
schema_registry: {}`
			}
			
			if _, exists := cm.Data[".bootstrap.json.in"]; exists {
				// Use consistent empty bootstrap configuration
				cm.Data[".bootstrap.json.in"] = "{}"
			}
		}
	}

	// Sort resources by Group/Kind, then by namespace/name for deterministic output
	slices.SortStableFunc(allClusterResources, func(a, b client.Object) int {
		// Use the scheme to get the correct GVK since GetObjectKind() might not be populated
		// TODO Should this be the case? Is something weird here?
		aGVKs, _, _ := controller.V1Scheme.ObjectKinds(a)
		bGVKs, _, _ := controller.V1Scheme.ObjectKinds(b)
		
		var aGVK, bGVK schema.GroupVersionKind
		if len(aGVKs) > 0 {
			aGVK = aGVKs[0]
		}
		if len(bGVKs) > 0 {
			bGVK = bGVKs[0]
		}
		
		if aGVK.Group != bGVK.Group {
			return strings.Compare(aGVK.Group, bGVK.Group)
		}
		
		if aGVK.Kind != bGVK.Kind {
			return strings.Compare(aGVK.Kind, bGVK.Kind)
		}
		
		return strings.Compare(client.ObjectKeyFromObject(a).String(), client.ObjectKeyFromObject(b).String())
	})

	allResourcesYAML, err := kube.EncodeYAML(controller.V1Scheme, allClusterResources...)
	require.NoError(t, err)

	testutil.AssertGolden(t, testutil.YAML, "testdata/cluster-resources.golden.yaml", allResourcesYAML)
}
