// Copyright 2024 Redpanda Data, Inc.
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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/redpanda-data/common-go/rpadmin"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources/featuregates"
)

var _ = Describe("Redpanda cluster scale resource", func() {
	const (
		timeout  = time.Second * 30
		interval = time.Millisecond * 100

		timeoutShort  = time.Millisecond * 100
		intervalShort = time.Millisecond * 20
	)

	Context("When scaling up a cluster", func() {
		It("Should just update the StatefulSet", func() {
			By("Allowing creation of a new cluster with 2 replicas")
			key, redpandaCluster := getClusterWithReplicas("scaling", 2)
			Expect(k8sClient.Create(context.Background(), redpandaCluster)).Should(Succeed())

			By("Scaling to 2 replicas when at least one broker appears")
			testAdminAPI.AddBroker(rpadmin.Broker{NodeID: 0, MembershipStatus: rpadmin.MembershipStatusActive})
			var sts appsv1.StatefulSet
			Eventually(resourceDataGetter(key, &sts, func() interface{} {
				return *sts.Spec.Replicas
			}), timeout, interval).Should(Equal(int32(2)))
			Eventually(resourceDataGetter(key, redpandaCluster, func() interface{} {
				return redpandaCluster.Status.CurrentReplicas
			}), timeout, interval).Should(Equal(int32(2)), "CurrentReplicas should be 2, got %d", redpandaCluster.Status.CurrentReplicas)

			By("Scaling up when replicas increase")
			Eventually(clusterUpdater(key, func(cluster *vectorizedv1alpha1.Cluster) {
				cluster.Spec.Replicas = ptr.To(int32(5))
			}), timeout, interval).Should(Succeed())
			Eventually(resourceDataGetter(key, &sts, func() interface{} {
				return *sts.Spec.Replicas
			}), timeout, interval).Should(Equal(int32(5)))

			By("Deleting the cluster")
			Expect(k8sClient.Delete(context.Background(), redpandaCluster)).Should(Succeed())
		})
	})

	Context("When scaling down a cluster", func() {
		It("Should always decommission the last pods of a cluster one at time", func() {
			By("Allowing creation of a new cluster with 3 replicas")
			key, redpandaCluster := getClusterWithReplicas("decommission", 3)
			Expect(k8sClient.Create(context.Background(), redpandaCluster)).Should(Succeed())

			By("Scaling to 3 replicas when the brokers start to appear")
			testAdminAPI.AddBroker(rpadmin.Broker{NodeID: 0, MembershipStatus: rpadmin.MembershipStatusActive})
			testAdminAPI.AddBroker(rpadmin.Broker{NodeID: 1, MembershipStatus: rpadmin.MembershipStatusActive})
			testAdminAPI.AddBroker(rpadmin.Broker{NodeID: 2, MembershipStatus: rpadmin.MembershipStatusActive})
			var sts appsv1.StatefulSet
			Eventually(resourceDataGetter(key, &sts, func() interface{} {
				return *sts.Spec.Replicas
			}), timeout, interval).Should(Equal(int32(3)))
			Eventually(resourceDataGetter(key, redpandaCluster, func() interface{} {
				return redpandaCluster.Status.CurrentReplicas
			}), timeout, interval).Should(Equal(int32(3)), "CurrentReplicas should be 3, got %d", redpandaCluster.Status.CurrentReplicas)
			Eventually(statefulSetReplicasReconciler(ctrl.Log.WithName("statefulSetReplicasReconciler"), key, redpandaCluster), timeout, interval).Should(Succeed())

			By("Decommissioning the last node when scaling down by 2")
			Eventually(clusterUpdater(key, func(cluster *vectorizedv1alpha1.Cluster) {
				cluster.Spec.Replicas = ptr.To(int32(1))
			}), timeout, interval).Should(Succeed())

			By("Start decommissioning node with ordinal 2")
			Eventually(resourceDataGetter(key, redpandaCluster, func() interface{} {
				if redpandaCluster.GetDecommissionBrokerID() == nil {
					return nil
				}
				return *redpandaCluster.GetDecommissionBrokerID()
			}), timeout, interval).Should(Equal(int32(2)), "node 2 is not decommissioning:\n%s", func() string { y, _ := yaml.Marshal(redpandaCluster); return string(y) }())
			Eventually(testAdminAPI.BrokerStatusGetter(2), timeout, interval).Should(Equal(rpadmin.MembershipStatusDraining))
			Consistently(testAdminAPI.BrokerStatusGetter(1), timeoutShort, intervalShort).Should(Equal(rpadmin.MembershipStatusActive))
			Eventually(resourceDataGetter(key, &sts, func() interface{} {
				return *sts.Spec.Replicas
			}), timeout, interval).Should(Equal(int32(3)))

			By("Scaling down only when decommissioning is done")
			Expect(testAdminAPI.RemoveBroker(2)).To(BeTrue())
			testAdminAPI.AddGhostBroker(rpadmin.Broker{NodeID: 2, MembershipStatus: rpadmin.MembershipStatusDraining})
			Eventually(resourceDataGetter(key, &sts, func() interface{} {
				return *sts.Spec.Replicas
			}), timeout, interval).Should(Equal(int32(2)))
			Eventually(statefulSetReplicasReconciler(ctrl.Log.WithName("statefulSetReplicasReconciler"), key, redpandaCluster), timeout, interval).Should(Succeed())

			By("Start decommissioning the other node")
			Eventually(testAdminAPI.BrokerStatusGetter(1), timeout, interval).Should(Equal(rpadmin.MembershipStatusDraining))

			By("Removing the other node as well when done")
			Expect(testAdminAPI.RemoveBroker(1)).To(BeTrue())
			testAdminAPI.AddGhostBroker(rpadmin.Broker{NodeID: 1, MembershipStatus: rpadmin.MembershipStatusDraining})
			Eventually(resourceDataGetter(key, &sts, func() interface{} {
				return *sts.Spec.Replicas
			}), timeout, interval).Should(Equal(int32(1)))

			By("Deleting the cluster")
			Expect(k8sClient.Delete(context.Background(), redpandaCluster)).Should(Succeed())
		})

		It("Can recommission a node while decommission is in progress", func() {
			By("Allowing creation of a new cluster with 3 replicas")
			key, redpandaCluster := getClusterWithReplicas("recommission", 3)
			Expect(k8sClient.Create(context.Background(), redpandaCluster)).Should(Succeed())

			By("Scaling to 3 replicas when the brokers start to appear")
			testAdminAPI.AddBroker(rpadmin.Broker{NodeID: 0, MembershipStatus: rpadmin.MembershipStatusActive})
			testAdminAPI.AddBroker(rpadmin.Broker{NodeID: 1, MembershipStatus: rpadmin.MembershipStatusActive})
			testAdminAPI.AddBroker(rpadmin.Broker{NodeID: 2, MembershipStatus: rpadmin.MembershipStatusActive})
			var sts appsv1.StatefulSet
			Eventually(resourceDataGetter(key, &sts, func() interface{} {
				return *sts.Spec.Replicas
			}), timeout, interval).Should(Equal(int32(3)))
			Eventually(resourceDataGetter(key, redpandaCluster, func() interface{} {
				return redpandaCluster.Status.CurrentReplicas
			}), timeout, interval).Should(Equal(int32(3)), "CurrentReplicas should be 3, got %d", redpandaCluster.Status.CurrentReplicas)
			Eventually(statefulSetReplicasReconciler(ctrl.Log.WithName("statefulSetReplicasReconciler"), key, redpandaCluster), timeout, interval).Should(Succeed())

			By("Start decommissioning node 2")
			Eventually(clusterUpdater(key, func(cluster *vectorizedv1alpha1.Cluster) {
				cluster.Spec.Replicas = ptr.To(int32(2))
			}), timeout, interval).Should(Succeed())
			Eventually(resourceDataGetter(key, redpandaCluster, func() interface{} {
				if redpandaCluster.GetDecommissionBrokerID() == nil {
					return nil
				}
				return *redpandaCluster.GetDecommissionBrokerID()
			}), timeout, interval).Should(Equal(int32(2)), "node 2 is not decommissioning:\n%s", func() string { y, _ := yaml.Marshal(redpandaCluster); return string(y) }())
			Eventually(testAdminAPI.BrokerStatusGetter(2), timeout, interval).Should(Equal(rpadmin.MembershipStatusDraining))

			By("Recommissioning the node by restoring replicas")
			Eventually(clusterUpdater(key, func(cluster *vectorizedv1alpha1.Cluster) {
				cluster.Spec.Replicas = ptr.To(int32(3))
			}), timeout, interval).Should(Succeed())
			Eventually(testAdminAPI.BrokerStatusGetter(2), timeout, interval).Should(Equal(rpadmin.MembershipStatusActive))

			By("Start decommissioning node 2 again")
			Eventually(clusterUpdater(key, func(cluster *vectorizedv1alpha1.Cluster) {
				cluster.Spec.Replicas = ptr.To(int32(2))
			}), timeout, interval).Should(Succeed())
			Eventually(resourceDataGetter(key, redpandaCluster, func() interface{} {
				if redpandaCluster.GetDecommissionBrokerID() == nil {
					return nil
				}
				return *redpandaCluster.GetDecommissionBrokerID()
			}), timeout, interval).Should(Equal(int32(2)))
			Eventually(testAdminAPI.BrokerStatusGetter(2), timeout, interval).Should(Equal(rpadmin.MembershipStatusDraining))

			By("Recommissioning the node also when scaling to more replicas")
			Eventually(clusterUpdater(key, func(cluster *vectorizedv1alpha1.Cluster) {
				cluster.Spec.Replicas = ptr.To(int32(4))
			}), timeout, interval).Should(Succeed())
			Eventually(testAdminAPI.BrokerStatusGetter(2), timeout, interval).Should(Equal(rpadmin.MembershipStatusActive))
			Eventually(resourceDataGetter(key, &sts, func() interface{} {
				return *sts.Spec.Replicas
			}), timeout, interval).Should(Equal(int32(4)))

			By("Deleting the cluster")
			Expect(k8sClient.Delete(context.Background(), redpandaCluster)).Should(Succeed())
		})
	})
})

var _ = Describe("Redpanda node pool scale", func() {
	const (
		timeout  = time.Second * 30
		interval = time.Millisecond * 100

		timeoutShort  = time.Millisecond * 100
		intervalShort = time.Millisecond * 20
	)

	Context("When starting up a fresh Redpanda cluster", func() {
		It("Launches desired replicas immediately", func() {
			By("Allowing creation of a new cluster with two nodepoools of 3 replicas each")
			_, redpandaCluster := getClusterWithNodePool("np-startup", 3, 3)
			npFirstKey := types.NamespacedName{
				Name:      "np-startup-first",
				Namespace: "default",
			}
			npSecondKey := types.NamespacedName{
				Name:      "np-startup-second",
				Namespace: "default",
			}

			redpandaCluster.Spec.Version = featuregates.V22_3.String()
			Expect(k8sClient.Create(context.Background(), redpandaCluster)).Should(Succeed())
			By("Keeping the StatefulSet at single replica until initialized")
			var sts appsv1.StatefulSet
			Eventually(resourceGetter(npFirstKey, &sts), timeout, interval).Should(Succeed())
			Consistently(resourceDataGetter(npFirstKey, &sts, func() interface{} {
				return *sts.Spec.Replicas
			}), timeoutShort, intervalShort).Should(Equal(int32(3)))
			Eventually(resourceDataGetter(npSecondKey, &sts, func() interface{} {
				return *sts.Spec.Replicas
			}), timeout, interval).Should(Equal(int32(3)))

			By("Scaling to 6 replicas only when broker appears")
			testAdminAPI.AddBroker(rpadmin.Broker{NodeID: 0, MembershipStatus: rpadmin.MembershipStatusActive})
			Eventually(resourceDataGetter(npFirstKey, &sts, func() interface{} {
				return *sts.Spec.Replicas
			}), timeout, interval).Should(Equal(int32(3)))
			Eventually(resourceDataGetter(npSecondKey, &sts, func() interface{} {
				return *sts.Spec.Replicas
			}), timeout, interval).Should(Equal(int32(3)))

			By("Deleting the cluster")
			Expect(k8sClient.Delete(context.Background(), redpandaCluster)).Should(Succeed())
		})
	})
})

func getClusterWithReplicas(
	name string, replicas int32,
) (key types.NamespacedName, cluster *vectorizedv1alpha1.Cluster) {
	key = types.NamespacedName{
		Name:      name,
		Namespace: "default",
	}

	cluster = &vectorizedv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      key.Name,
			Namespace: key.Namespace,
		},
		Spec: vectorizedv1alpha1.ClusterSpec{
			Image:    "vectorized/redpanda",
			Version:  redpandaContainerTag,
			Replicas: ptr.To(replicas),
			Configuration: vectorizedv1alpha1.RedpandaConfig{
				KafkaAPI: []vectorizedv1alpha1.KafkaAPI{
					{
						Port: 9092,
					},
				},
				AdminAPI: []vectorizedv1alpha1.AdminAPI{{Port: 9644}},
				RPCServer: vectorizedv1alpha1.SocketAddress{
					Port: 33145,
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
				Redpanda: nil,
			},
		},
	}
	return key, cluster
}

func getClusterWithNodePool(
	name string, firstNodePoolReplicas int32, secondNodePoolReplicas int32,
) (key types.NamespacedName, cluster *vectorizedv1alpha1.Cluster) {
	res := vectorizedv1alpha1.RedpandaResourceRequirements{
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
		Redpanda: nil,
	}

	key = types.NamespacedName{
		Name:      name,
		Namespace: "default",
	}

	cluster = &vectorizedv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      key.Name,
			Namespace: key.Namespace,
		},
		Spec: vectorizedv1alpha1.ClusterSpec{
			Image:   "vectorized/redpanda",
			Version: redpandaContainerTag,
			Configuration: vectorizedv1alpha1.RedpandaConfig{
				KafkaAPI: []vectorizedv1alpha1.KafkaAPI{
					{
						Port: 9092,
					},
				},
				AdminAPI: []vectorizedv1alpha1.AdminAPI{{Port: 9644}},
				RPCServer: vectorizedv1alpha1.SocketAddress{
					Port: 33145,
				},
			},
			NodePools: []vectorizedv1alpha1.NodePoolSpec{
				{
					Name:      "first",
					Replicas:  &firstNodePoolReplicas,
					Resources: res,
				},
				{
					Name:      "second",
					Replicas:  &secondNodePoolReplicas,
					Resources: res,
				},
			},
		},
	}
	return key, cluster
}
