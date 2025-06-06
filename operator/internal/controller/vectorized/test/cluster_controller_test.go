// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// nolint:testpackage // this name is ok
package test

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	types2 "github.com/onsi/gomega/types"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller/vectorized"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/labels"
	res "github.com/redpanda-data/redpanda-operator/operator/pkg/resources"
)

const (
	redpandaContainerTag = "24.2.5"
)

var _ = Describe("RedPandaCluster controller", func() {
	const (
		timeout  = time.Second * 30
		interval = time.Second * 1

		adminPort                 = 9644
		kafkaPort                 = 9092
		pandaProxyPort            = 8082
		schemaRegistryPort        = 8081
		redpandaConfigurationFile = "redpanda.yaml"
		replicas                  = 1
		redpandaContainerTag      = "x"
		redpandaContainerImage    = "vectorized/redpanda"

		clusterNameWithLicense = "test-cluster-with-license"
		licenseName            = "test-cluster-with-license"
		licenseNamespace       = "default"

		externalSuffix = "-external"
	)

	Context("When creating RedpandaCluster", func() {
		It("Should create Redpanda cluster with corresponding resources", func() {
			resourceRedpanda := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			}

			resourceRequests := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("3Gi"),
			}

			resourceLimits := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("3"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
			}

			key := types.NamespacedName{
				Name:      "redpanda-test",
				Namespace: "default",
			}
			baseKey := types.NamespacedName{
				Name:      key.Name + baseSuffix,
				Namespace: "default",
			}
			clusterRoleKey := types.NamespacedName{
				Name:      "redpanda-init-configurator",
				Namespace: "",
			}
			redpandaCluster := &vectorizedv1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
					Labels: map[string]string{
						"app": "redpanda",
					},
				},
				Spec: vectorizedv1alpha1.ClusterSpec{
					Image:    redpandaContainerImage,
					Version:  redpandaContainerTag,
					Replicas: ptr.To(int32(replicas)),
					Configuration: vectorizedv1alpha1.RedpandaConfig{
						KafkaAPI: []vectorizedv1alpha1.KafkaAPI{
							{Port: kafkaPort},
							{External: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true}},
						},
						AdminAPI: []vectorizedv1alpha1.AdminAPI{
							{Port: adminPort},
							{External: vectorizedv1alpha1.ExternalConnectivityConfig{
								Enabled: true,
							}},
						},
						PandaproxyAPI: []vectorizedv1alpha1.PandaproxyAPI{
							{Port: pandaProxyPort},
							{External: vectorizedv1alpha1.PandaproxyExternalConnectivityConfig{ExternalConnectivityConfig: vectorizedv1alpha1.ExternalConnectivityConfig{
								Enabled: true,
							}}},
						},
						SchemaRegistry: &vectorizedv1alpha1.SchemaRegistryAPI{
							Port: schemaRegistryPort,
							External: &vectorizedv1alpha1.SchemaRegistryExternalConnectivityConfig{
								ExternalConnectivityConfig: vectorizedv1alpha1.ExternalConnectivityConfig{
									Enabled: true,
								},
							},
						},
					},
					Resources: vectorizedv1alpha1.RedpandaResourceRequirements{
						ResourceRequirements: corev1.ResourceRequirements{
							Limits:   resourceLimits,
							Requests: resourceRequests,
						},
						Redpanda: resourceRedpanda,
					},
				},
			}
			Expect(k8sClient.Create(context.Background(), redpandaCluster)).Should(Succeed())

			redpandaPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
					Labels: map[string]string{
						"app.kubernetes.io/component": "redpanda",
						"app.kubernetes.io/instance":  "redpanda-test",
						"app.kubernetes.io/name":      "redpanda",
						labels.NodePoolKey:            "default",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: "test-node",
					Containers: []corev1.Container{{
						Name:  "test",
						Image: "test",
					}},
				},
				Status: corev1.PodStatus{},
			}
			Expect(k8sClient.Create(context.Background(), redpandaPod)).Should(Succeed())

			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
				Spec: corev1.NodeSpec{},
				Status: corev1.NodeStatus{
					Addresses: []corev1.NodeAddress{
						{
							Type:    corev1.NodeExternalIP,
							Address: "9.8.7.6",
						},
						{
							Type:    corev1.NodeInternalIP,
							Address: "11.11.11.11",
						},
					},
				},
			}
			Expect(k8sClient.Create(context.Background(), node)).Should(Succeed())

			By("Creating headless Service")
			var svc corev1.Service
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), key, &svc)
				return err == nil &&
					svc.Spec.ClusterIP == corev1.ClusterIPNone &&
					findPort(svc.Spec.Ports, res.InternalListenerName) == kafkaPort &&
					findPort(svc.Spec.Ports, res.AdminPortName) == adminPort &&
					validOwner(redpandaCluster, svc.OwnerReferences)
			}, timeout, interval).Should(BeTrue())

			By("Creating NodePort Service")
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), types.NamespacedName{
					Name:      key.Name + externalSuffix,
					Namespace: key.Namespace,
				}, &svc)
				return err == nil &&
					svc.Spec.Type == corev1.ServiceTypeNodePort &&
					findPort(svc.Spec.Ports, res.ExternalListenerName) == kafkaPort+1 &&
					findPort(svc.Spec.Ports, res.AdminPortExternalName) == adminPort+1 &&
					validOwner(redpandaCluster, svc.OwnerReferences)
			}, timeout, interval).Should(BeTrue())

			By("Creating Configmap with the redpanda configuration")
			var cm corev1.ConfigMap
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), baseKey, &cm)
				if err != nil {
					return false
				}
				_, exist := cm.Data[redpandaConfigurationFile]
				return exist &&
					validOwner(redpandaCluster, cm.OwnerReferences)
			}, timeout, interval).Should(BeTrue())

			By("Creating ServiceAccount")
			var sa corev1.ServiceAccount
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), key, &sa)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			By("Creating ClusterRole")
			var cr rbacv1.ClusterRole
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), clusterRoleKey, &cr)
				return err == nil &&
					cr.Rules[0].Verbs[0] == "get" &&
					cr.Rules[0].Resources[0] == "nodes"
			}, timeout, interval).Should(BeTrue())

			By("Creating ClusterRoleBinding")
			var crb rbacv1.ClusterRoleBinding
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), clusterRoleKey, &crb)
				found := false
				for _, s := range crb.Subjects {
					if key.Name == s.Name && s.Kind == "ServiceAccount" {
						found = true
					}
				}
				return err == nil &&
					crb.RoleRef.Name == clusterRoleKey.Name &&
					crb.RoleRef.Kind == "ClusterRole" &&
					found
			}, timeout, interval).Should(BeTrue())

			By("Creating StatefulSet")
			var sts appsv1.StatefulSet
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), key, &sts)
				return err == nil &&
					*sts.Spec.Replicas == replicas &&
					sts.Spec.Template.Spec.Containers[0].Image == "vectorized/redpanda:"+redpandaContainerTag &&
					validOwner(redpandaCluster, sts.OwnerReferences)
			}, timeout, interval).Should(BeTrue())

			Expect(sts.Spec.Template.Spec.Containers[0].Resources.Requests).Should(Equal(resourceRequests))
			Expect(sts.Spec.Template.Spec.Containers[0].Resources.Limits).Should(Equal(resourceLimits))
			Expect(sts.Spec.Template.Spec.Containers[0].Args).Should(ContainElement(fmt.Sprintf("--memory=%d", resourceRedpanda.Memory().Value())))
			Expect(sts.Spec.Template.Spec.Containers[0].Args).Should(ContainElement(fmt.Sprintf("--smp=%d", resourceRedpanda.Cpu().Value())))
			Expect(sts.Spec.Template.Spec.Containers[0].Env).Should(ContainElement(corev1.EnvVar{Name: "REDPANDA_ENVIRONMENT", Value: "kubernetes"}))

			By("Reporting nodes internal and external")
			var rc vectorizedv1alpha1.Cluster
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), key, &rc)
				return err == nil &&
					len(rc.Status.Nodes.Internal) == 1 &&
					len(rc.Status.Nodes.External) == 1 &&
					len(rc.Status.Nodes.ExternalAdmin) == 1 &&
					len(rc.Status.Nodes.ExternalPandaproxy) == 1 &&
					len(rc.Status.Nodes.SchemaRegistry.ExternalNodeIPs) == 1 &&
					rc.Status.Nodes.SchemaRegistry.Internal != "" &&
					rc.Status.Nodes.SchemaRegistry.External == "" // Without subdomain the external address is empty
			}, timeout, interval).Should(BeTrue())
		})
		It("creates redpanda cluster with tls enabled", func() {
			resources := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			}

			key := types.NamespacedName{
				Name:      "redpanda-test-tls",
				Namespace: "default",
			}
			redpandaCluster := &vectorizedv1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: vectorizedv1alpha1.ClusterSpec{
					Image:    redpandaContainerImage,
					Version:  redpandaContainerTag,
					Replicas: ptr.To(int32(replicas)),
					Configuration: vectorizedv1alpha1.RedpandaConfig{
						KafkaAPI: []vectorizedv1alpha1.KafkaAPI{
							{
								Port: kafkaPort,
								TLS:  vectorizedv1alpha1.KafkaAPITLS{Enabled: true, RequireClientAuth: true},
							},
						},
						AdminAPI: []vectorizedv1alpha1.AdminAPI{{Port: adminPort}},
					},
					Resources: vectorizedv1alpha1.RedpandaResourceRequirements{
						ResourceRequirements: corev1.ResourceRequirements{
							Limits:   resources,
							Requests: resources,
						},
						Redpanda: nil,
					},
				},
			}
			Expect(k8sClient.Create(context.Background(), redpandaCluster)).Should(Succeed())

			By("Creating StatefulSet")
			var sts appsv1.StatefulSet
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), key, &sts)
				return err == nil &&
					*sts.Spec.Replicas == replicas
			}, timeout, interval).Should(BeTrue())

			var defaultMode int32 = 420
			Expect(sts.Spec.Template.Spec.Containers[0].VolumeMounts).Should(
				ContainElements(
					corev1.VolumeMount{Name: "tlscert", MountPath: "/etc/tls/certs"},
					corev1.VolumeMount{Name: "tlsca", MountPath: "/etc/tls/certs/ca"},
				))
			Expect(sts.Spec.Template.Spec.Volumes).Should(
				ContainElements(
					corev1.Volume{
						Name: "tlscert",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: "redpanda-test-tls-redpanda",
								Items: []corev1.KeyToPath{
									{
										Key:  "tls.key",
										Path: "tls.key",
									},
									{
										Key:  "tls.crt",
										Path: "tls.crt",
									},
									{
										Key:  "ca.crt",
										Path: "ca.crt",
									},
								},
								DefaultMode: &defaultMode,
							},
						},
					},
					corev1.Volume{
						Name: "tlsca",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: "redpanda-test-tls-operator-client",
								Items: []corev1.KeyToPath{
									{
										Key:  "ca.crt",
										Path: "ca.crt",
									},
									{
										Key:  "tls.key",
										Path: "tls.key",
									},
									{
										Key:  "tls.crt",
										Path: "tls.crt",
									},
								},
								DefaultMode: &defaultMode,
							},
						},
					}))
		})
		It("creates redpanda cluster without external connectivity", func() {
			resources := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			}

			key := types.NamespacedName{
				Name:      "internal-redpanda",
				Namespace: "default",
			}
			redpandaCluster := &vectorizedv1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: vectorizedv1alpha1.ClusterSpec{
					Image:    redpandaContainerImage,
					Version:  redpandaContainerTag,
					Replicas: ptr.To(int32(replicas)),
					Configuration: vectorizedv1alpha1.RedpandaConfig{
						KafkaAPI: []vectorizedv1alpha1.KafkaAPI{
							{
								Port: kafkaPort,
							},
						},
						AdminAPI: []vectorizedv1alpha1.AdminAPI{{Port: adminPort}},
					},
					Resources: vectorizedv1alpha1.RedpandaResourceRequirements{
						ResourceRequirements: corev1.ResourceRequirements{
							Limits:   resources,
							Requests: resources,
						},
						Redpanda: nil,
					},
				},
			}
			Expect(k8sClient.Create(context.Background(), redpandaCluster)).Should(Succeed())

			redpandaPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
					Labels: map[string]string{
						"app.kubernetes.io/component": "redpanda",
						"app.kubernetes.io/instance":  "internal-redpanda",
						"app.kubernetes.io/name":      "redpanda",
						labels.NodePoolKey:            "default",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: "test-node",
					Containers: []corev1.Container{{
						Name:  "test",
						Image: "test",
					}},
				},
				Status: corev1.PodStatus{},
			}
			Expect(k8sClient.Create(context.Background(), redpandaPod)).Should(Succeed())

			By("Creating StatefulSet")
			var sts appsv1.StatefulSet
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), key, &sts)
				return err == nil &&
					*sts.Spec.Replicas == replicas
			}, timeout, interval).Should(BeTrue())

			By("report only internal address")
			var cluster vectorizedv1alpha1.Cluster
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), key, &cluster)
				return err == nil &&
					cluster.Status.Nodes.SchemaRegistry != nil &&
					cluster.Status.Nodes.SchemaRegistry.Internal != "" &&
					len(cluster.Status.Nodes.Internal) > 0
			}, timeout, interval).Should(BeTrue())
		})
		It("creates redpanda cluster with fixed nodeport", func() {
			resources := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			}

			key := types.NamespacedName{
				Name:      "external-fixed-redpanda",
				Namespace: "default",
			}
			redpandaCluster := &vectorizedv1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: vectorizedv1alpha1.ClusterSpec{
					Image:    redpandaContainerImage,
					Version:  redpandaContainerTag,
					Replicas: ptr.To(int32(replicas)),
					Configuration: vectorizedv1alpha1.RedpandaConfig{
						KafkaAPI: []vectorizedv1alpha1.KafkaAPI{
							{
								Port: kafkaPort,
							},
							{
								Port: 31111,
								External: vectorizedv1alpha1.ExternalConnectivityConfig{
									Enabled:   true,
									Subdomain: "vectorized.io",
								},
							},
						},
						AdminAPI: []vectorizedv1alpha1.AdminAPI{{Port: adminPort}},
					},
					Resources: vectorizedv1alpha1.RedpandaResourceRequirements{
						ResourceRequirements: corev1.ResourceRequirements{
							Limits:   resources,
							Requests: resources,
						},
						Redpanda: nil,
					},
				},
			}
			Expect(k8sClient.Create(context.Background(), redpandaCluster)).Should(Succeed())

			redpandaPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
					Labels: map[string]string{
						"app.kubernetes.io/component": "redpanda",
						"app.kubernetes.io/instance":  "internal-redpanda",
						"app.kubernetes.io/name":      "redpanda",
						labels.NodePoolKey:            "default",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: "test-node",
					Containers: []corev1.Container{{
						Name:  "test",
						Image: "test",
					}},
				},
				Status: corev1.PodStatus{},
			}
			Expect(k8sClient.Create(context.Background(), redpandaPod)).Should(Succeed())

			By("Creating StatefulSet")
			var sts appsv1.StatefulSet
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), key, &sts)
				return err == nil &&
					*sts.Spec.Replicas == replicas
			}, timeout, interval).Should(BeTrue())
		})
		It("creates redpanda cluster with preferred address type", func() {
			resources := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			}
			key := types.NamespacedName{
				Name:      "preferred-address-redpanda",
				Namespace: "default",
			}
			redpandaCluster := &vectorizedv1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: vectorizedv1alpha1.ClusterSpec{
					Image:    redpandaContainerImage,
					Version:  redpandaContainerTag,
					Replicas: ptr.To(int32(replicas)),
					Configuration: vectorizedv1alpha1.RedpandaConfig{
						KafkaAPI: []vectorizedv1alpha1.KafkaAPI{
							{
								Port: kafkaPort,
							},
							{
								External: vectorizedv1alpha1.ExternalConnectivityConfig{
									Enabled:              true,
									PreferredAddressType: "InternalIP",
								},
							},
						},
						AdminAPI: []vectorizedv1alpha1.AdminAPI{{Port: adminPort}},
					},
					Resources: vectorizedv1alpha1.RedpandaResourceRequirements{
						ResourceRequirements: corev1.ResourceRequirements{
							Limits:   resources,
							Requests: resources,
						},
						Redpanda: nil,
					},
				},
			}
			Expect(k8sClient.Create(context.Background(), redpandaCluster)).Should(Succeed())

			redpandaPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
					Labels: map[string]string{
						"app.kubernetes.io/component": "redpanda",
						"app.kubernetes.io/instance":  "preferred-address-redpanda",
						"app.kubernetes.io/name":      "redpanda",
						labels.NodePoolKey:            "default",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: "test-node",
					Containers: []corev1.Container{{
						Name:  "test",
						Image: "test",
					}},
				},
				Status: corev1.PodStatus{},
			}
			Expect(k8sClient.Create(context.Background(), redpandaPod)).Should(Succeed())

			By("Creating StatefulSet")
			var sts appsv1.StatefulSet
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), key, &sts)
				return err == nil &&
					*sts.Spec.Replicas == replicas
			}, timeout, interval).Should(BeTrue())

			By("Creating external service")
			var svc corev1.Service
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), types.NamespacedName{
					Name:      key.Name + externalSuffix,
					Namespace: key.Namespace,
				}, &svc)
				return err == nil &&
					svc.Spec.Type == corev1.ServiceTypeNodePort &&
					len(svc.Spec.Ports) == 1
			}, timeout, interval).Should(BeTrue())

			By("Reporting external connectivity through InternalIP")
			var rc vectorizedv1alpha1.Cluster
			nodeport := svc.Spec.Ports[0].NodePort
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), key, &rc)
				return err == nil &&
					len(rc.Status.Nodes.External) == 1 &&
					rc.Status.Nodes.External[0] == fmt.Sprintf("%s:%d", "11.11.11.11", nodeport)
			}, timeout, interval).Should(BeTrue())
		})
		It("creates redpanda cluster with bootstrap load balancer", func() {
			resources := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			}
			key := types.NamespacedName{
				Name:      "bootstrap-redpanda",
				Namespace: "default",
			}
			redpandaCluster := &vectorizedv1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: vectorizedv1alpha1.ClusterSpec{
					Image:    redpandaContainerImage,
					Version:  redpandaContainerTag,
					Replicas: ptr.To(int32(replicas)),
					Configuration: vectorizedv1alpha1.RedpandaConfig{
						KafkaAPI: []vectorizedv1alpha1.KafkaAPI{
							{
								Port: kafkaPort,
							},
							{
								External: vectorizedv1alpha1.ExternalConnectivityConfig{
									Enabled: true,
									Bootstrap: &vectorizedv1alpha1.LoadBalancerConfig{
										Annotations: map[string]string{
											"key": "val",
										},
										Port: 1234,
									},
								},
							},
						},
						AdminAPI: []vectorizedv1alpha1.AdminAPI{{Port: adminPort}},
					},
					Resources: vectorizedv1alpha1.RedpandaResourceRequirements{
						ResourceRequirements: corev1.ResourceRequirements{
							Limits:   resources,
							Requests: resources,
						},
						Redpanda: nil,
					},
				},
			}
			Expect(k8sClient.Create(context.Background(), redpandaCluster)).Should(Succeed())

			redpandaPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
					Labels: map[string]string{
						"app.kubernetes.io/component": "redpanda",
						"app.kubernetes.io/instance":  "bootstrap-redpanda",
						"app.kubernetes.io/name":      "redpanda",
						labels.NodePoolKey:            "default",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: "test-node",
					Containers: []corev1.Container{{
						Name:  "test",
						Image: "test",
					}},
				},
				Status: corev1.PodStatus{},
			}
			Expect(k8sClient.Create(context.Background(), redpandaPod)).Should(Succeed())

			By("Creating StatefulSet")
			var sts appsv1.StatefulSet
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), key, &sts)
				return err == nil &&
					*sts.Spec.Replicas == replicas
			}, timeout, interval).Should(BeTrue())

			By("Creating LB bootstrap service")
			var svc corev1.Service
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), types.NamespacedName{
					Name:      key.Name + "-lb-bootstrap",
					Namespace: key.Namespace,
				}, &svc)
				return err == nil &&
					svc.Spec.Type == corev1.ServiceTypeLoadBalancer &&
					len(svc.Spec.Ports) == 1
			}, timeout, interval).Should(BeTrue())

			By("Reporting external connectivity bootstrap load balancer")
			var rc vectorizedv1alpha1.Cluster
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), key, &rc)
				return err == nil &&
					rc.Status.Nodes.ExternalBootstrap != nil
			}, timeout, interval).Should(BeTrue())
		})

		It("scaling out does not trigger a rolling restart", func() {
			const replicas = 3
			resources := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			}
			key := types.NamespacedName{
				Name:      "redpanda-no-restart",
				Namespace: "default",
			}
			redpandaCluster := &vectorizedv1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: vectorizedv1alpha1.ClusterSpec{
					Image:    redpandaContainerImage,
					Version:  redpandaContainerTag,
					Replicas: ptr.To(int32(replicas)),
					Configuration: vectorizedv1alpha1.RedpandaConfig{
						KafkaAPI: []vectorizedv1alpha1.KafkaAPI{
							{
								Port: kafkaPort,
							},
						},
						AdminAPI: []vectorizedv1alpha1.AdminAPI{{Port: adminPort}},
					},
					Resources: vectorizedv1alpha1.RedpandaResourceRequirements{
						ResourceRequirements: corev1.ResourceRequirements{
							Limits:   resources,
							Requests: resources,
						},
						Redpanda: nil,
					},
				},
			}
			Expect(k8sClient.Create(context.Background(), redpandaCluster)).Should(Succeed())

			By("Creating StatefulSet")
			var sts appsv1.StatefulSet
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), key, &sts)
				return err == nil &&
					*sts.Spec.Replicas == replicas
			}, timeout, interval).Should(BeTrue())

			// configmap annotation should not change when scaling out cluster replicas
			// to avoid an unnecessary rolling restart
			configMapHash := sts.Annotations[res.ConfigMapHashAnnotationKey]
			var existingCluster vectorizedv1alpha1.Cluster
			Expect(k8sClient.Get(context.Background(), key, &existingCluster)).Should(Succeed())
			latest := existingCluster.DeepCopy()
			var newReplicas int32 = replicas + 2
			existingCluster.Spec.Replicas = &newReplicas
			Expect(k8sClient.Patch(context.Background(), &existingCluster, client.MergeFrom(latest))).Should(Succeed())

			// verify scale-out completed and the configmap hash did not change
			var newSts appsv1.StatefulSet
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), key, &newSts)
				return err == nil &&
					*newSts.Spec.Replicas == newReplicas
			}, timeout, interval).Should(BeTrue())
			newConfigMapHash := newSts.Annotations[res.ConfigMapHashAnnotationKey]
			Expect(newConfigMapHash).Should(Equal(configMapHash))
		})

		It("Should load license", func() {
			By("Creating the license Secret")
			licenseSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: licenseNamespace,
					Name:      licenseName,
				},
				StringData: map[string]string{vectorizedv1alpha1.DefaultLicenseSecretKey: "fake-license"},
			}
			Expect(k8sClient.Create(context.Background(), licenseSecret)).Should(Succeed())

			By("Creating a Cluster")
			key, _, redpandaCluster, namespace, _ := getInitialTestCluster(clusterNameWithLicense)
			redpandaCluster.Spec.LicenseRef = &vectorizedv1alpha1.SecretKeyRef{Namespace: licenseNamespace, Name: licenseName}
			Expect(k8sClient.Create(context.Background(), namespace)).Should(Succeed())
			Expect(k8sClient.Create(context.Background(), redpandaCluster)).Should(Succeed())
			Eventually(clusterConfiguredConditionStatusGetter(key), timeout, interval).Should(BeTrue())
		})
	})

	Context("Calling reconcile", func() {
		It("Should not throw error on non-existing CRB and cluster", func() {
			// this test is started with fake client that was not initialized,
			// so neither redpanda Cluster object or CRB or any other object
			// exists. This verifies that these situations are handled
			// gracefully and without error
			r := &vectorized.ClusterReconciler{
				Client:                   fake.NewClientBuilder().WithScheme(controller.UnifiedScheme).Build(),
				Log:                      ctrl.Log,
				Scheme:                   controller.UnifiedScheme,
				AdminAPIClientFactory:    testAdminAPIFactory,
				DecommissionWaitInterval: 100 * time.Millisecond,
			}
			_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{
				Namespace: "default",
				Name:      "nonexisting",
			}})
			Expect(err).To(Succeed())
		})
	})

	Context("Calling reconcile with restricted version", func() {
		const allowedVersion = "v23.2.1"
		It("Should throw error due to restricted redpanda version", func() {
			restrictedVersion := "v23.2.2"
			key, redpandaCluster := getVersionedRedpanda("restricted-redpanda-negative", restrictedVersion)
			fc := fake.NewClientBuilder().WithObjects(redpandaCluster).WithStatusSubresource(redpandaCluster).WithScheme(controller.UnifiedScheme).Build()
			r := &vectorized.ClusterReconciler{
				Client:                    fc,
				Log:                       ctrl.Log,
				Scheme:                    controller.UnifiedScheme,
				AdminAPIClientFactory:     testAdminAPIFactory,
				DecommissionWaitInterval:  100 * time.Millisecond,
				RestrictToRedpandaVersion: allowedVersion,
			}
			_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: key})
			Expect(err).To(Succeed())
			By("Reporting the existence of cluster")
			var rc vectorizedv1alpha1.Cluster
			Eventually(func() bool {
				err := fc.Get(context.Background(), key, &rc)
				return err == nil
			}, time.Second*5, interval).Should(BeTrue())
			By("Not changing status version to a valid one")
			Consistently(func() bool {
				return rc.Status.Version == ""
			}, time.Second, 100*time.Millisecond).Should(BeTrue())
		})
		It("Should not throw error; redpanda version allowed", func() {
			key, redpandaCluster := getVersionedRedpanda("restricted-redpanda-positive", allowedVersion)
			pods := readyPodsForCluster(redpandaCluster)
			objects := []client.Object{redpandaCluster}
			for i := range pods {
				objects = append(objects, pods[i])
			}
			fc := fake.NewClientBuilder().WithObjects(objects...).WithScheme(controller.UnifiedScheme).WithStatusSubresource(objects...).Build()
			r := &vectorized.ClusterReconciler{
				Client:                    fc,
				Log:                       ctrl.Log,
				Scheme:                    controller.UnifiedScheme,
				AdminAPIClientFactory:     testAdminAPIFactory,
				DecommissionWaitInterval:  100 * time.Millisecond,
				RestrictToRedpandaVersion: allowedVersion,
			}
			_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: key})
			Expect(err).To(Succeed())
			By("Reporting the existence of cluster and allowed version status")
			var rc vectorizedv1alpha1.Cluster
			Eventually(func() bool {
				err := fc.Get(context.Background(), key, &rc)
				return err == nil && rc.Status.Version == allowedVersion
			}, time.Second*15, interval).Should(BeTrue())
		})
	})

	DescribeTable("Image pull policy tests table", func(imagePullPolicy string, matcher types2.GomegaMatcher) {
		k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme: controller.UnifiedScheme,
			Metrics: metricsserver.Options{
				BindAddress: "0",
			},
		})
		Expect(err).NotTo(HaveOccurred())

		r := &vectorized.ClusterReconciler{
			Client:                   fake.NewClientBuilder().WithScheme(controller.UnifiedScheme).Build(),
			Log:                      ctrl.Log,
			Scheme:                   controller.UnifiedScheme,
			AdminAPIClientFactory:    testAdminAPIFactory,
			DecommissionWaitInterval: 100 * time.Millisecond,
		}

		Expect(r.WithConfiguratorSettings(res.ConfiguratorSettings{
			ImagePullPolicy: corev1.PullPolicy(imagePullPolicy),
		}).WithSkipNameValidation(true).SetupWithManager(k8sManager)).To(matcher)
	},
		Entry("Always image pull policy", "Always", Succeed()),
		Entry("IfNotPresent image pull policy", "IfNotPresent", Succeed()),
		Entry("Never image pull policy", "Never", Succeed()),
		Entry("Empty image pull policy", "", Not(Succeed())),
		Entry("Random image pull policy", "asdvasd", Not(Succeed())))
})

func readyPodsForCluster(cluster *vectorizedv1alpha1.Cluster) []*corev1.Pod {
	var result []*corev1.Pod
	for i := 0; i < int(*cluster.Spec.Replicas); i++ {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("pod-%d", i),
				Namespace: cluster.Namespace,
				Labels: map[string]string{
					"app.kubernetes.io/component": "redpanda",
					"app.kubernetes.io/instance":  cluster.Name,
					"app.kubernetes.io/name":      "redpanda",
					labels.NodePoolKey:            "default",
				},
			},
			Spec: corev1.PodSpec{
				NodeName: "test-node",
				Containers: []corev1.Container{{
					Name:  "redpanda",
					Image: fmt.Sprintf("redpanda:%s", cluster.Spec.Version),
				}},
			},
			Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}, {Type: corev1.ContainersReady, Status: corev1.ConditionTrue}}, Phase: corev1.PodRunning},
		}
		result = append(result, pod)
	}
	return result
}

func getVersionedRedpanda(
	name string, version string,
) (key types.NamespacedName, cluster *vectorizedv1alpha1.Cluster) {
	key = types.NamespacedName{
		Name:      name,
		Namespace: "default",
	}
	config := vectorizedv1alpha1.RedpandaConfig{
		KafkaAPI: []vectorizedv1alpha1.KafkaAPI{
			{
				Port: 9644,
			},
		},
		AdminAPI: []vectorizedv1alpha1.AdminAPI{
			{
				Port: 9092,
			},
		},
	}
	resources := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("1"),
		corev1.ResourceMemory: resource.MustParse("2Gi"),
	}
	rpresources := vectorizedv1alpha1.RedpandaResourceRequirements{
		ResourceRequirements: corev1.ResourceRequirements{
			Limits:   resources,
			Requests: resources,
		},
		Redpanda: nil,
	}
	redpandaCluster := &vectorizedv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      key.Name,
			Namespace: key.Namespace,
		},
		Spec: vectorizedv1alpha1.ClusterSpec{
			Image:         "vectorized/redpanda",
			Version:       version,
			Replicas:      ptr.To(int32(1)),
			Configuration: config,
			Resources:     rpresources,
		},
	}
	return key, redpandaCluster
}

func findPort(ports []corev1.ServicePort, name string) int32 {
	for _, port := range ports {
		if port.Name == name {
			return port.Port
		}
	}
	return 0
}

func validOwner(
	cluster *vectorizedv1alpha1.Cluster, owners []metav1.OwnerReference,
) bool {
	if len(owners) != 1 {
		return false
	}

	owner := owners[0]

	return owner.Name == cluster.Name && owner.Controller != nil && *owner.Controller
}
