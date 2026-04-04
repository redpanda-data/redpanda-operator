// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package vectorized

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/go-logr/logr/testr"
	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	redpanda "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/client"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	crds "github.com/redpanda-data/redpanda-operator/operator/config/crd/bases"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/admin"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/labels"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources"
	resourcetypes "github.com/redpanda-data/redpanda-operator/operator/pkg/resources/types"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
	"github.com/redpanda-data/redpanda-operator/pkg/kube/kubetest"
	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/log"
	"github.com/redpanda-data/redpanda-operator/pkg/secrets"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

func TestClusterControllerGolden(t *testing.T) {
	tests := []struct {
		name       string
		clusterDef func() *vectorizedv1alpha1.Cluster
	}{
		{
			name:       "minimal_cluster",
			clusterDef: minimalClusterDef,
		},
	}

	testScheme := controller.UnifiedScheme
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
		Controller: config.Controller{
			SkipNameValidation: ptr.To(true),
		},
	})
	require.NoError(t, err)
	reconciler := createTestReconciler(t, mgr)
	require.NoError(t, reconciler.SetupWithManager(mgr))

	go func() {
		require.NoError(t, mgr.Start(ctx))
	}()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster := tt.clusterDef()
			require.NoError(t, ctl.Apply(ctx, cluster))
			// Wait for cluster reconciliation to complete - we wait for any condition to appear,
			// which indicates the reconciler has run at least once and processed the cluster
			require.NoError(t, ctl.WaitFor(ctx, cluster, func(obj kube.Object, err error) (bool, error) {
				if err != nil {
					return false, err
				}
				cl := obj.(*vectorizedv1alpha1.Cluster)
				return len(cl.Status.Conditions) > 0, nil
			}))

			defer func() {
				if err := ctl.DeleteAndWait(ctx, cluster); err != nil {
					require.NoError(t, err, "failed to delete cluster")
				}
			}()

			allClusterResources := normalizeClusterResources(ctx, testScheme, cluster, t, ctl)
			slices.SortStableFunc(allClusterResources, func(a, b client.Object) int {
				return strings.Compare(fmt.Sprintf("%T/%s/%s", a, a.GetNamespace(), a.GetName()),
					fmt.Sprintf("%T/%s/%s", b, b.GetNamespace(), b.GetName()))
			})

			allResourcesYAML, err := kube.EncodeYAML(testScheme, allClusterResources...)
			require.NoError(t, err)
			goldenFile := fmt.Sprintf("testdata/%s.golden.yaml", tt.name)
			testutil.AssertGolden(t, testutil.YAML, goldenFile, allResourcesYAML)
		})
	}
}

// createTestReconciler creates a standard test reconciler with mock admin API
func createTestReconciler(t *testing.T, mgr manager.Manager) *ClusterReconciler {
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
		skipNameValidation: true,
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

// normalizeClusterResources normalizes cluster resources for golden file comparison
func normalizeClusterResources(ctx context.Context, testScheme *runtime.Scheme, cluster *vectorizedv1alpha1.Cluster, t *testing.T, ctl *kube.Ctl) []client.Object {
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
		err := ctl.List(ctx, cluster.Namespace, l)
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
		rs.SetUID("")
		rs.SetCreationTimestamp(metav1.Unix(0, 0))
		rs.SetManagedFields(nil)
		if ownerRefs := rs.GetOwnerReferences(); len(ownerRefs) > 0 {
			for i := range ownerRefs {
				ownerRefs[i].UID = ""
			}
			rs.SetOwnerReferences(ownerRefs)
		}

		if annotations := rs.GetAnnotations(); annotations != nil {
			annotations["redpanda.com/last-applied"] = "<removed>"
		}

		switch obj := rs.(type) {
		case *corev1.Service:
			for i := range obj.Spec.Ports {
				if obj.Spec.Ports[i].NodePort != 0 {
					obj.Spec.Ports[i].NodePort = 30000
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
						obj.Spec.Template.Spec.Containers[i].Ports[j].HostPort = 30000
					}
				}
			}

			for i := range obj.Spec.Template.Spec.InitContainers {
				for j := range obj.Spec.Template.Spec.InitContainers[i].Env {
					if obj.Spec.Template.Spec.InitContainers[i].Env[j].Name == "PROXY_HOST_PORT" {
						obj.Spec.Template.Spec.InitContainers[i].Env[j].Value = "PLACEHOLDER-PORT-30000"
					}
				}
			}
		}
	}
	return allClusterResources
}
