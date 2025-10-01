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
	"strings"
	"time"

	"github.com/moby/moby/pkg/namesgenerator"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redpanda-data/common-go/rpadmin"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/internal/testutils"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/admin"
	"github.com/redpanda-data/redpanda-operator/pkg/clusterconfiguration"
)

const (
	versionWithCentralizedConfiguration = "v24.2.5"

	baseSuffix = "-base"
)

var _ = Describe("RedpandaCluster configuration controller", func() {
	const (
		timeout  = time.Second * 30
		interval = time.Millisecond * 100

		timeoutShort  = time.Millisecond * 100
		intervalShort = time.Millisecond * 20

		nodeConfigHashKey = "redpanda.vectorized.io/configmap-hash"
	)

	gracePeriod := int64(0)
	deleteOptions := &client.DeleteOptions{
		GracePeriodSeconds: &gracePeriod,
	}

	Context("When managing a RedpandaCluster with centralized config", func() {
		It("Can initialize a cluster with centralized configuration", func() {
			By("Allowing creation of a new cluster")
			key, baseKey, redpandaCluster, namespace, _ := getInitialTestCluster("central-initialize")
			Expect(k8sClient.Create(context.Background(), namespace)).Should(Succeed())
			Expect(k8sClient.Create(context.Background(), redpandaCluster)).Should(Succeed())

			By("Creating a Configmap with the bootstrap configuration")
			var cm corev1.ConfigMap
			Eventually(resourceGetter(baseKey, &cm), timeout, interval).Should(Succeed())

			By("Putting a redpanda.yaml template in the configmap")
			Expect(cm.Data[clusterconfiguration.RedpandaYamlTemplateFile]).ToNot(BeEmpty())
			Expect(cm.Data[clusterconfiguration.RedpandaYamlFixupFile]).ToNot(BeEmpty())

			By("Putting a .bootstrap.yaml template in the configmap")
			Expect(cm.Data[clusterconfiguration.BootstrapTemplateFile]).ToNot(BeEmpty())
			Expect(cm.Data[clusterconfiguration.BootstrapFixupFile]).ToNot(BeEmpty())

			By("Creating the statefulset")
			var sts appsv1.StatefulSet
			Eventually(resourceGetter(key, &appsv1.StatefulSet{})).Should(Succeed())

			By("Setting the configmap-hash annotation on the statefulset")
			Eventually(annotationGetter(key, &sts, nodeConfigHashKey), timeout, interval).ShouldNot(BeEmpty())

			By("Synchronizing the condition")
			Eventually(clusterConfiguredConditionStatusGetter(key), timeout, interval).Should(BeTrue())

			By("Deleting the cluster")
			Expect(k8sClient.Delete(context.Background(), redpandaCluster, deleteOptions)).Should(Succeed())
			testutils.DeleteAllInNamespace(testEnv, k8sClient, namespace)
		})

		It("Should interact with the admin API when doing changes", func() {
			By("Allowing creation of a new cluster")
			key, baseKey, redpandaCluster, namespace, adminAPI := getInitialTestCluster("central-changes")
			Expect(k8sClient.Create(context.Background(), namespace)).Should(Succeed())
			Expect(k8sClient.Create(context.Background(), redpandaCluster)).Should(Succeed())

			By("Creating the Configmap and the statefulset")
			Eventually(resourceGetter(baseKey, &corev1.ConfigMap{}), timeout, interval).Should(Succeed())
			Eventually(resourceGetter(key, &appsv1.StatefulSet{}), timeout, interval).Should(Succeed())

			By("Setting a configmap-hash in the statefulset")
			var sts appsv1.StatefulSet
			Expect(resourceGetter(key, &sts)()).To(Succeed())
			nodeConfigHash := sts.Spec.Template.Annotations[nodeConfigHashKey]
			Expect(nodeConfigHash).NotTo(BeEmpty())

			By("Synchronizing the configuration")
			Eventually(clusterConfiguredConditionStatusGetter(key), timeout, interval).Should(BeTrue())

			By("Accepting a change")
			adminAPI.RegisterPropertySchema("non-restarting", rpadmin.ConfigPropertyMetadata{NeedsRestart: false, Type: "string"})
			var cluster vectorizedv1alpha1.Cluster
			Expect(k8sClient.Get(context.Background(), key, &cluster)).To(Succeed())
			latest := cluster.DeepCopy()
			if cluster.Spec.AdditionalConfiguration == nil {
				cluster.Spec.AdditionalConfiguration = make(map[string]string)
			}
			cluster.Spec.AdditionalConfiguration["redpanda.non-restarting"] = "the-val"
			Expect(k8sClient.Patch(context.Background(), &cluster, client.MergeFrom(latest))).To(Succeed())

			By("Sending the new property to the admin API")
			Eventually(adminAPI.PropertyGetter("non-restarting"), timeout, interval).Should(Equal("the-val"))

			By("Synchronizing the condition")
			Eventually(clusterConfiguredConditionStatusGetter(key), timeout, interval).Should(BeTrue())

			By("Not changing the configmap-hash in the statefulset")
			Consistently(annotationGetter(key, &appsv1.StatefulSet{}, nodeConfigHashKey), timeoutShort, intervalShort).Should(Equal(nodeConfigHash))

			By("Never restarting the cluster")
			Consistently(annotationGetter(key, &appsv1.StatefulSet{}, nodeConfigHashKey), timeoutShort, intervalShort).Should(Equal(nodeConfigHash))
			// TODO: detecting this really needs pods to be reflected in this test

			By("Deleting the cluster")
			Expect(k8sClient.Delete(context.Background(), redpandaCluster, deleteOptions)).Should(Succeed())
			testutils.DeleteAllInNamespace(testEnv, k8sClient, namespace)
		})

		It("Should remove properties from the admin API when needed", func() {
			By("Allowing creation of a new cluster")
			key, baseKey, redpandaCluster, namespace, adminAPI := getInitialTestCluster("central-removal")
			Expect(k8sClient.Create(context.Background(), namespace)).Should(Succeed())
			Expect(k8sClient.Create(context.Background(), redpandaCluster)).Should(Succeed())

			By("Creating the Configmap and the statefulset")
			Eventually(resourceGetter(baseKey, &corev1.ConfigMap{}), timeout, interval).Should(Succeed())
			var sts appsv1.StatefulSet
			Eventually(resourceGetter(key, &sts), timeout, interval).Should(Succeed())
			hash := sts.Spec.Template.Annotations[nodeConfigHashKey]
			Expect(hash).NotTo(BeEmpty())

			By("Synchronizing the configuration")
			Eventually(clusterConfiguredConditionStatusGetter(key), timeout, interval).Should(BeTrue())

			By("Accepting an initial change")
			adminAPI.RegisterPropertySchema("p0", rpadmin.ConfigPropertyMetadata{NeedsRestart: false, Type: "string"})
			var cluster vectorizedv1alpha1.Cluster
			Expect(k8sClient.Get(context.Background(), key, &cluster)).To(Succeed())
			latest := cluster.DeepCopy()
			if cluster.Spec.AdditionalConfiguration == nil {
				cluster.Spec.AdditionalConfiguration = make(map[string]string)
			}
			cluster.Spec.AdditionalConfiguration["redpanda.p0"] = "v0"
			Expect(k8sClient.Patch(context.Background(), &cluster, client.MergeFrom(latest))).To(Succeed())

			By("Synchronizing the field with the admin API")
			Eventually(adminAPI.PropertyGetter("p0"), timeout, interval).Should(Equal("v0"))

			By("Synchronizing the condition")
			Eventually(clusterConfiguredConditionStatusGetter(key), timeout, interval).Should(BeTrue())

			By("Accepting two new properties")
			adminAPI.RegisterPropertySchema("p1", rpadmin.ConfigPropertyMetadata{NeedsRestart: false, Type: "string"})
			adminAPI.RegisterPropertySchema("p2", rpadmin.ConfigPropertyMetadata{NeedsRestart: false, Type: "string"})
			Expect(k8sClient.Get(context.Background(), key, &cluster)).To(Succeed())
			latest = cluster.DeepCopy()
			if cluster.Spec.AdditionalConfiguration == nil {
				cluster.Spec.AdditionalConfiguration = make(map[string]string)
			}
			cluster.Spec.AdditionalConfiguration["redpanda.p1"] = "v1"
			cluster.Spec.AdditionalConfiguration["redpanda.p2"] = "v2"
			Expect(k8sClient.Patch(context.Background(), &cluster, client.MergeFrom(latest))).To(Succeed())

			By("Adding two fields to the config at once")
			Eventually(adminAPI.PropertyGetter("p1")).Should(Equal("v1"))
			Eventually(adminAPI.PropertyGetter("p2")).Should(Equal("v2"))

			By("Synchronizing the condition")
			Eventually(clusterConfiguredConditionStatusGetter(key), timeout, interval).Should(BeTrue())

			By("Accepting a deletion and a change")
			Expect(k8sClient.Get(context.Background(), key, &cluster)).To(Succeed())
			latest = cluster.DeepCopy()
			if cluster.Spec.AdditionalConfiguration == nil {
				cluster.Spec.AdditionalConfiguration = make(map[string]string)
			}
			cluster.Spec.AdditionalConfiguration["redpanda.p1"] = "v1x"
			delete(cluster.Spec.AdditionalConfiguration, "redpanda.p2")
			Expect(k8sClient.Patch(context.Background(), &cluster, client.MergeFrom(latest))).To(Succeed())

			By("Producing the right patch")
			Eventually(adminAPI.PropertyGetter("p1")).Should(Equal("v1x"))

			By("Synchronizing the condition")
			Eventually(clusterConfiguredConditionStatusGetter(key), timeout, interval).Should(BeTrue())

			By("Never restarting the cluster")
			Consistently(annotationGetter(key, &appsv1.StatefulSet{}, nodeConfigHashKey), timeoutShort, intervalShort).Should(Equal(hash))
			// TODO: detecting this really needs pods to be reflected in this test

			By("Adding configuration of array type")
			Expect(k8sClient.Get(context.Background(), key, &cluster)).To(Succeed())
			latest = cluster.DeepCopy()
			adminAPI.RegisterPropertySchema("kafka_nodelete_topics", rpadmin.ConfigPropertyMetadata{NeedsRestart: false, Type: "array", Items: rpadmin.ConfigPropertyItems{Type: "string"}})
			cluster.Spec.AdditionalConfiguration["redpanda.kafka_nodelete_topics"] = "[_internal_connectors_configs _internal_connectors_offsets _internal_connectors_status _audit __consumer_offsets _redpanda_e2e_probe _schemas]"
			Expect(k8sClient.Patch(context.Background(), &cluster, client.MergeFrom(latest))).To(Succeed())

			Eventually(adminAPI.PropertyGetter("kafka_nodelete_topics")).Should(Equal([]string{
				"__consumer_offsets",
				"_audit",
				"_internal_connectors_configs",
				"_internal_connectors_offsets",
				"_internal_connectors_status",
				"_redpanda_e2e_probe",
				"_schemas",
			}))

			Eventually(clusterConfiguredConditionStatusGetter(key), timeout, interval).Should(BeTrue())

			By("Deleting the cluster")
			Expect(k8sClient.Delete(context.Background(), redpandaCluster, deleteOptions)).Should(Succeed())
			testutils.DeleteAllInNamespace(testEnv, k8sClient, namespace)
		})

		It("Should restart the cluster only when strictly required", func() {
			By("Allowing creation of a new cluster")
			key, baseKey, redpandaCluster, namespace, adminAPI := getInitialTestCluster("central-restart")
			Expect(k8sClient.Create(context.Background(), namespace)).Should(Succeed())
			Expect(k8sClient.Create(context.Background(), redpandaCluster)).Should(Succeed())

			By("Creating the Configmap and the statefulset")
			Eventually(resourceGetter(baseKey, &corev1.ConfigMap{}), timeout, interval).Should(Succeed())
			Eventually(resourceGetter(key, &appsv1.StatefulSet{}), timeout, interval).Should(Succeed())

			By("Synchronizing the condition")
			Eventually(clusterConfiguredConditionStatusGetter(key), timeout, interval).Should(BeTrue())

			By("Starting with an stable statefulset")
			var sts appsv1.StatefulSet
			Expect(resourceGetter(key, &sts)()).To(Succeed())
			initialNodeConfigHash := sts.Spec.Template.Annotations[nodeConfigHashKey]
			Expect(initialNodeConfigHash).NotTo(BeEmpty())

			By("Accepting a change that would require restart")
			adminAPI.RegisterPropertySchema("prop-restart", rpadmin.ConfigPropertyMetadata{NeedsRestart: true, Type: "string"})
			var cluster vectorizedv1alpha1.Cluster
			Expect(k8sClient.Get(context.Background(), key, &cluster)).To(Succeed())
			latest := cluster.DeepCopy()
			if cluster.Spec.AdditionalConfiguration == nil {
				cluster.Spec.AdditionalConfiguration = make(map[string]string)
			}
			const propValue = "the-value"
			cluster.Spec.AdditionalConfiguration["redpanda.prop-restart"] = propValue
			Expect(k8sClient.Patch(context.Background(), &cluster, client.MergeFrom(latest))).To(Succeed())

			By("Synchronizing the field with the admin API")
			Eventually(adminAPI.PropertyGetter("prop-restart"), timeout, interval).Should(Equal(propValue))

			By("Synchronizing the condition")
			Eventually(clusterConfiguredConditionStatusGetter(key), timeout, interval).Should(BeTrue())

			By("Restarting pods with the same node-config hash")
			Expect(annotationGetter(key, &appsv1.StatefulSet{}, nodeConfigHashKey)()).To(Equal(initialNodeConfigHash))
			// TODO: detecting a cluster restart really needs pods to be reflected in this test

			By("Accepting another change that would not require restart")
			adminAPI.RegisterPropertySchema("prop-no-restart", rpadmin.ConfigPropertyMetadata{NeedsRestart: false, Type: "string"})
			Expect(k8sClient.Get(context.Background(), key, &cluster)).To(Succeed())
			latest = cluster.DeepCopy()
			const propValue2 = "the-value2"
			cluster.Spec.AdditionalConfiguration["redpanda.prop-no-restart"] = propValue2
			Expect(k8sClient.Patch(context.Background(), &cluster, client.MergeFrom(latest))).To(Succeed())

			By("Synchronizing the new field with the admin API")
			Eventually(adminAPI.PropertyGetter("prop-no-restart"), timeout, interval).Should(Equal(propValue2))

			By("Synchronizing the condition")
			Eventually(clusterConfiguredConditionStatusGetter(key), timeout, interval).Should(BeTrue())

			By("Not changing the node hash")
			Expect(annotationGetter(key, &appsv1.StatefulSet{}, nodeConfigHashKey)()).To(Equal(initialNodeConfigHash))

			By("Accepting a change in a node property to trigger restart")
			Expect(k8sClient.Get(context.Background(), key, &cluster)).To(Succeed())
			latest = cluster.DeepCopy()
			cluster.Spec.Configuration.DeveloperMode = !cluster.Spec.Configuration.DeveloperMode
			Expect(k8sClient.Patch(context.Background(), &cluster, client.MergeFrom(latest))).To(Succeed())

			By("Changing the hash because of the node property change")
			Eventually(annotationGetter(key, &appsv1.StatefulSet{}, nodeConfigHashKey), timeout, interval).ShouldNot(Equal(initialNodeConfigHash))
			nodeConfigHash2 := annotationGetter(key, &appsv1.StatefulSet{}, nodeConfigHashKey)()
			Consistently(annotationGetter(key, &appsv1.StatefulSet{}, nodeConfigHashKey), timeoutShort, intervalShort).Should(Equal(nodeConfigHash2))

			By("Synchronizing the condition")
			Eventually(clusterConfiguredConditionStatusGetter(key), timeout, interval).Should(BeTrue())

			By("Accepting another change to a redpanda node property to trigger restart")
			Expect(k8sClient.Get(context.Background(), key, &cluster)).To(Succeed())
			latest = cluster.DeepCopy()
			if cluster.Spec.AdditionalConfiguration == nil {
				cluster.Spec.AdditionalConfiguration = make(map[string]string)
			}
			cluster.Spec.AdditionalConfiguration["redpanda.cloud_storage_cache_directory"] = "/tmp" // node property
			Expect(k8sClient.Patch(context.Background(), &cluster, client.MergeFrom(latest))).To(Succeed())

			By("Changing the hash because of the node property change")
			Eventually(annotationGetter(key, &appsv1.StatefulSet{}, nodeConfigHashKey), timeout, interval).ShouldNot(Equal(nodeConfigHash2))
			configMapHash3 := annotationGetter(key, &appsv1.StatefulSet{}, nodeConfigHashKey)()
			Consistently(annotationGetter(key, &appsv1.StatefulSet{}, nodeConfigHashKey), timeoutShort, intervalShort).Should(Equal(configMapHash3))

			By("Synchronizing the condition")
			Eventually(clusterConfiguredConditionStatusGetter(key), timeout, interval).Should(BeTrue())

			By("Deleting the cluster")
			Expect(k8sClient.Delete(context.Background(), redpandaCluster, deleteOptions)).Should(Succeed())
			testutils.DeleteAllInNamespace(testEnv, k8sClient, namespace)
		})

		It("Should defer updating the centralized configuration when admin API is unavailable", func() {
			By("Allowing creation of a new cluster")
			key, baseKey, redpandaCluster, namespace, adminAPI := getInitialTestCluster("admin-unavailable")
			adminAPI.SetUnavailable(true)
			Expect(k8sClient.Create(context.Background(), namespace)).Should(Succeed())
			Expect(k8sClient.Create(context.Background(), redpandaCluster)).Should(Succeed())

			By("Creating the StatefulSet and the ConfigMap")
			Eventually(resourceGetter(baseKey, &corev1.ConfigMap{}), timeout, interval).Should(Succeed())
			Eventually(resourceGetter(key, &appsv1.StatefulSet{}), timeout, interval).Should(Succeed())

			By("Setting the configured condition to false until verification")
			Eventually(clusterConfiguredConditionGetter(key), timeout, interval).ShouldNot(BeNil())
			Consistently(clusterConfiguredConditionStatusGetter(key), timeoutShort, intervalShort).Should(BeFalse())

			By("Accepting a change to the properties")
			adminAPI.RegisterPropertySchema("prop", rpadmin.ConfigPropertyMetadata{NeedsRestart: false, Type: "string"})
			const propValue = "the-value"
			Eventually(clusterUpdater(key, func(cluster *vectorizedv1alpha1.Cluster) {
				if cluster.Spec.AdditionalConfiguration == nil {
					cluster.Spec.AdditionalConfiguration = make(map[string]string)
				}
				cluster.Spec.AdditionalConfiguration["redpanda.prop"] = propValue
			}), timeout, interval).Should(Succeed())

			By("Maintaining the configured condition to false")
			Eventually(clusterConfiguredConditionStatusGetter(key), timeout, interval).Should(BeFalse())
			Consistently(clusterConfiguredConditionStatusGetter(key), timeoutShort, intervalShort).Should(BeFalse())

			By("Recovering when the API becomes available again")
			adminAPI.SetUnavailable(false)
			Eventually(clusterConfiguredConditionStatusGetter(key), timeout, interval).Should(BeTrue())
			Expect(adminAPI.PropertyGetter("prop")()).To(Equal(propValue))

			By("Deleting the cluster")
			Expect(k8sClient.Delete(context.Background(), redpandaCluster, deleteOptions)).Should(Succeed())
			testutils.DeleteAllInNamespace(testEnv, k8sClient, namespace)
		})
	})

	Context("When setting invalid configuration on the cluster - unknown properties", func() {
		It("Should reflect any invalid status in the condition", func() {
			By("Allowing creation of a new cluster")
			key, baseKey, redpandaCluster, namespace, _ := getInitialTestCluster("condition-check")
			Expect(k8sClient.Create(context.Background(), namespace)).Should(Succeed())
			Expect(k8sClient.Create(context.Background(), redpandaCluster)).Should(Succeed())

			By("Creating the Configmap and the statefulset")
			Eventually(resourceGetter(baseKey, &corev1.ConfigMap{}), timeout, interval).Should(Succeed())
			Eventually(resourceGetter(key, &appsv1.StatefulSet{}), timeout, interval).Should(Succeed())

			By("Configuring the cluster")
			Eventually(clusterConfiguredConditionStatusGetter(key), timeout, interval).Should(BeTrue())

			By("Accepting an unknown property")
			Eventually(clusterUpdater(key, func(cluster *vectorizedv1alpha1.Cluster) {
				if cluster.Spec.AdditionalConfiguration == nil {
					cluster.Spec.AdditionalConfiguration = make(map[string]string)
				}
				cluster.Spec.AdditionalConfiguration["redpanda.unk"] = "nown"
			}), timeout, interval).Should(Succeed())

			By("Reflecting the issue in the condition")
			Eventually(resourceDataGetter(key, redpandaCluster, func() interface{} {
				cond := redpandaCluster.Status.GetCondition(vectorizedv1alpha1.ClusterConfiguredConditionType)
				if cond == nil {
					return nil
				}
				return fmt.Sprintf("%s/%s", cond.Status, cond.Reason)
			}), timeout, interval).Should(Equal("False/Error"))
			Eventually(resourceDataGetter(key, redpandaCluster, func() interface{} {
				cond := redpandaCluster.Status.GetCondition(vectorizedv1alpha1.ClusterConfiguredConditionType)
				if cond == nil {
					return nil
				}
				return cond.Message
			}), timeout, interval).Should(ContainSubstring("Unknown"))

			By("Restoring the state when fixing the property")
			Eventually(clusterUpdater(key, func(cluster *vectorizedv1alpha1.Cluster) {
				delete(cluster.Spec.AdditionalConfiguration, "redpanda.unk")
			}), timeout, interval).Should(Succeed())
			Eventually(clusterConfiguredConditionStatusGetter(key), timeout, interval).Should(BeTrue())

			By("Deleting the cluster")
			Expect(k8sClient.Delete(context.Background(), redpandaCluster, deleteOptions)).Should(Succeed())
			testutils.DeleteAllInNamespace(testEnv, k8sClient, namespace)
		})

		It("Should report direct validation errors in the condition", func() {
			By("Allowing creation of a new cluster")
			key, baseKey, redpandaCluster, namespace, adminAPI := getInitialTestCluster("condition-validation")

			// admin API will return 400 bad request on invalid configuration (default behavior)
			adminAPI.SetDirectValidationEnabled(true)

			Expect(k8sClient.Create(context.Background(), namespace)).Should(Succeed())
			Expect(k8sClient.Create(context.Background(), redpandaCluster)).Should(Succeed())

			By("Creating the Configmap and the statefulset")
			Eventually(resourceGetter(baseKey, &corev1.ConfigMap{}), timeout, interval).Should(Succeed())
			Eventually(resourceGetter(key, &appsv1.StatefulSet{}), timeout, interval).Should(Succeed())

			By("Configuring the cluster")
			Eventually(clusterConfiguredConditionStatusGetter(key), timeout, interval).Should(BeTrue())

			By("Accepting an unknown property")
			Eventually(clusterUpdater(key, func(cluster *vectorizedv1alpha1.Cluster) {
				if cluster.Spec.AdditionalConfiguration == nil {
					cluster.Spec.AdditionalConfiguration = make(map[string]string)
				}
				cluster.Spec.AdditionalConfiguration["redpanda.unk"] = "nown-value"
			}), timeout, interval).Should(Succeed())

			By("Reflecting the issue in the condition")
			Eventually(resourceDataGetter(key, redpandaCluster, func() interface{} {
				cond := redpandaCluster.Status.GetCondition(vectorizedv1alpha1.ClusterConfiguredConditionType)
				if cond == nil {
					return nil
				}
				return fmt.Sprintf("%s/%s/%s", cond.Status, cond.Reason, cond.Message)
			}), timeout, interval).Should(Equal("False/Error/Mock bad request message"))

			By("Restoring the state when fixing the property")
			Eventually(clusterUpdater(key, func(cluster *vectorizedv1alpha1.Cluster) {
				delete(cluster.Spec.AdditionalConfiguration, "redpanda.unk")
			}), timeout, interval).Should(Succeed())
			Eventually(clusterConfiguredConditionStatusGetter(key), timeout, interval).Should(BeTrue())

			By("Deleting the cluster")
			Expect(k8sClient.Delete(context.Background(), redpandaCluster, deleteOptions)).Should(Succeed())
			testutils.DeleteAllInNamespace(testEnv, k8sClient, namespace)
		})

		It("Should report configuration errors present in the .bootstrap.yaml file", func() {
			By("Allowing creation of a new cluster")
			key, baseKey, redpandaCluster, namespace, adminAPI := getInitialTestCluster("condition-bootstrap-failure")

			// Inject property before creating the cluster, simulating .bootstrap.yaml
			const val = "nown"
			_, err := adminAPI.PatchClusterConfig(context.Background(), map[string]interface{}{
				"unk": val,
			}, nil)
			Expect(err).To(BeNil())

			if redpandaCluster.Spec.AdditionalConfiguration == nil {
				redpandaCluster.Spec.AdditionalConfiguration = make(map[string]string)
			}
			redpandaCluster.Spec.AdditionalConfiguration["redpanda.unk"] = val
			Expect(k8sClient.Create(context.Background(), namespace)).Should(Succeed())
			Expect(k8sClient.Create(context.Background(), redpandaCluster)).Should(Succeed())

			By("Creating the Configmap and the statefulset")
			Eventually(resourceGetter(baseKey, &corev1.ConfigMap{}), timeout, interval).Should(Succeed())
			Eventually(resourceGetter(key, &appsv1.StatefulSet{}), timeout, interval).Should(Succeed())

			By("Marking the cluster as not properly configured")
			Eventually(clusterConfiguredConditionStatusGetter(key), timeout, interval).Should(BeFalse())
			Consistently(clusterConfiguredConditionStatusGetter(key), timeoutShort, intervalShort).Should(BeFalse())

			By("Restoring the state when fixing the property")
			Eventually(clusterUpdater(key, func(cluster *vectorizedv1alpha1.Cluster) {
				delete(cluster.Spec.AdditionalConfiguration, "redpanda.unk")
			}), timeout, interval).Should(Succeed())
			Eventually(clusterConfiguredConditionStatusGetter(key), timeout, interval).Should(BeTrue())

			By("Deleting the cluster")
			Expect(k8sClient.Delete(context.Background(), redpandaCluster, deleteOptions)).Should(Succeed())
			testutils.DeleteAllInNamespace(testEnv, k8sClient, namespace)
		})
	})

	Context("When setting invalid configuration on the cluster - invalid properties", func() {
		It("Should reflect any invalid status in the condition", func() {
			By("Allowing creation of a new cluster")
			key, baseKey, redpandaCluster, namespace, adminAPI := getInitialTestCluster("condition-check")
			Expect(k8sClient.Create(context.Background(), namespace)).Should(Succeed())
			Expect(k8sClient.Create(context.Background(), redpandaCluster)).Should(Succeed())

			By("Creating the Configmap and the statefulset")
			Eventually(resourceGetter(baseKey, &corev1.ConfigMap{}), timeout, interval).Should(Succeed())
			Eventually(resourceGetter(key, &appsv1.StatefulSet{}), timeout, interval).Should(Succeed())

			By("Configuring the cluster")
			Eventually(clusterConfiguredConditionStatusGetter(key), timeout, interval).Should(BeTrue())

			By("Accepting an invalid property")
			adminAPI.RegisterPropertySchema("inv", rpadmin.ConfigPropertyMetadata{Description: "invalid", Type: "string"}) // triggers mock validation
			Eventually(clusterUpdater(key, func(cluster *vectorizedv1alpha1.Cluster) {
				if cluster.Spec.AdditionalConfiguration == nil {
					cluster.Spec.AdditionalConfiguration = make(map[string]string)
				}
				cluster.Spec.AdditionalConfiguration["redpanda.inv"] = "alid"
			}), timeout, interval).Should(Succeed())

			By("Reflecting the issue in the condition")
			Eventually(resourceDataGetter(key, redpandaCluster, func() interface{} {
				cond := redpandaCluster.Status.GetCondition(vectorizedv1alpha1.ClusterConfiguredConditionType)
				if cond == nil {
					return nil
				}
				return fmt.Sprintf("%s/%s", cond.Status, cond.Reason)
			}), timeout, interval).Should(Equal("False/Error"))
			Eventually(resourceDataGetter(key, redpandaCluster, func() interface{} {
				cond := redpandaCluster.Status.GetCondition(vectorizedv1alpha1.ClusterConfiguredConditionType)
				if cond == nil {
					return nil
				}
				return cond.Message
			}), timeout, interval).Should(ContainSubstring("Invalid"))

			By("Restoring the state when fixing the property")
			Eventually(clusterUpdater(key, func(cluster *vectorizedv1alpha1.Cluster) {
				delete(cluster.Spec.AdditionalConfiguration, "redpanda.inv")
			}), timeout, interval).Should(Succeed())
			Eventually(clusterConfiguredConditionStatusGetter(key), timeout, interval).Should(BeTrue())

			By("Deleting the cluster")
			Expect(k8sClient.Delete(context.Background(), redpandaCluster, deleteOptions)).Should(Succeed())
			testutils.DeleteAllInNamespace(testEnv, k8sClient, namespace)
		})

		It("Should report direct validation errors in the condition", func() {
			By("Allowing creation of a new cluster")
			key, baseKey, redpandaCluster, namespace, adminAPI := getInitialTestCluster("condition-validation")

			// admin API will return 400 bad request on invalid configuration (default behavior)
			adminAPI.SetDirectValidationEnabled(true)

			Expect(k8sClient.Create(context.Background(), namespace)).Should(Succeed())
			Expect(k8sClient.Create(context.Background(), redpandaCluster)).Should(Succeed())

			By("Creating the Configmap and the statefulset")
			Eventually(resourceGetter(baseKey, &corev1.ConfigMap{}), timeout, interval).Should(Succeed())
			Eventually(resourceGetter(key, &appsv1.StatefulSet{}), timeout, interval).Should(Succeed())

			By("Configuring the cluster")
			Eventually(clusterConfiguredConditionStatusGetter(key), timeout, interval).Should(BeTrue())

			By("Accepting an unknown property")
			Eventually(clusterUpdater(key, func(cluster *vectorizedv1alpha1.Cluster) {
				if cluster.Spec.AdditionalConfiguration == nil {
					cluster.Spec.AdditionalConfiguration = make(map[string]string)
				}
				cluster.Spec.AdditionalConfiguration["redpanda.unk"] = "nown-value"
			}), timeout, interval).Should(Succeed())

			By("Reflecting the issue in the condition")
			Eventually(resourceDataGetter(key, redpandaCluster, func() interface{} {
				cond := redpandaCluster.Status.GetCondition(vectorizedv1alpha1.ClusterConfiguredConditionType)
				if cond == nil {
					return nil
				}
				return fmt.Sprintf("%s/%s/%s", cond.Status, cond.Reason, cond.Message)
			}), timeout, interval).Should(Equal("False/Error/Mock bad request message"))

			By("Restoring the state when fixing the property")
			Eventually(clusterUpdater(key, func(cluster *vectorizedv1alpha1.Cluster) {
				delete(cluster.Spec.AdditionalConfiguration, "redpanda.unk")
			}), timeout, interval).Should(Succeed())
			Eventually(clusterConfiguredConditionStatusGetter(key), timeout, interval).Should(BeTrue())

			By("Deleting the cluster")
			Expect(k8sClient.Delete(context.Background(), redpandaCluster, deleteOptions)).Should(Succeed())
			testutils.DeleteAllInNamespace(testEnv, k8sClient, namespace)
		})

		It("Should report configuration errors present in the .bootstrap.yaml file", func() {
			By("Allowing creation of a new cluster")
			key, baseKey, redpandaCluster, namespace, adminAPI := getInitialTestCluster("condition-bootstrap-failure")

			// Inject property before creating the cluster, simulating .bootstrap.yaml
			const val = "nown"
			_, err := adminAPI.PatchClusterConfig(context.Background(), map[string]interface{}{
				"unk": val,
			}, nil)
			Expect(err).To(BeNil())

			if redpandaCluster.Spec.AdditionalConfiguration == nil {
				redpandaCluster.Spec.AdditionalConfiguration = make(map[string]string)
			}
			redpandaCluster.Spec.AdditionalConfiguration["redpanda.unk"] = val
			Expect(k8sClient.Create(context.Background(), namespace)).Should(Succeed())
			Expect(k8sClient.Create(context.Background(), redpandaCluster)).Should(Succeed())

			By("Creating the Configmap and the statefulset")
			Eventually(resourceGetter(baseKey, &corev1.ConfigMap{}), timeout, interval).Should(Succeed())
			Eventually(resourceGetter(key, &appsv1.StatefulSet{}), timeout, interval).Should(Succeed())

			By("Marking the cluster as not properly configured")
			Eventually(clusterConfiguredConditionStatusGetter(key), timeout, interval).Should(BeFalse())
			Consistently(clusterConfiguredConditionStatusGetter(key), timeoutShort, intervalShort).Should(BeFalse())

			By("Restoring the state when fixing the property")
			Eventually(clusterUpdater(key, func(cluster *vectorizedv1alpha1.Cluster) {
				delete(cluster.Spec.AdditionalConfiguration, "redpanda.unk")
			}), timeout, interval).Should(Succeed())
			Eventually(clusterConfiguredConditionStatusGetter(key), timeout, interval).Should(BeTrue())

			By("Deleting the cluster")
			Expect(k8sClient.Delete(context.Background(), redpandaCluster, deleteOptions)).Should(Succeed())
			testutils.DeleteAllInNamespace(testEnv, k8sClient, namespace)
		})
	})

	Context("When external factors change configuration of a cluster", func() {
		It("The drift detector restore all managed properties", func() {
			// Registering two properties, one of them managed by the operator
			// The unmanaged property will be removed by `Declarative` mode.
			const (
				unmanagedProp = "unmanaged_prop"
				managedProp   = "managed_prop"

				desiredManagedPropValue = "desired-managed-value"
				unmanagedPropValue      = "unmanaged-prop-value"

				externalChangeManagedPropValue   = "external-managed-value"
				externalChangeUnmanagedPropValue = "external-unmanaged-value"
			)

			By("Allowing creation of a new cluster")
			key, baseKey, redpandaCluster, namespace, adminAPI := getInitialTestCluster("central-drift-detector")

			adminAPI.RegisterPropertySchema(managedProp, rpadmin.ConfigPropertyMetadata{NeedsRestart: false, Type: "string"})
			adminAPI.RegisterPropertySchema(unmanagedProp, rpadmin.ConfigPropertyMetadata{NeedsRestart: false, Type: "string"})
			adminAPI.SetProperty(unmanagedProp, unmanagedPropValue)

			Expect(k8sClient.Create(context.Background(), namespace)).Should(Succeed())
			Expect(k8sClient.Create(context.Background(), redpandaCluster)).Should(Succeed())

			By("Creating a Configmap and StatefulSet with the bootstrap configuration")
			Eventually(resourceGetter(baseKey, &corev1.ConfigMap{}), timeout, interval).Should(Succeed())
			Eventually(resourceGetter(key, &appsv1.StatefulSet{}), timeout, interval).Should(Succeed())

			By("Synchronizing the condition")
			Eventually(clusterConfiguredConditionStatusGetter(key), timeout, interval).Should(BeTrue())

			By("Changing the configuration")
			Eventually(clusterUpdater(key, func(cluster *vectorizedv1alpha1.Cluster) {
				if cluster.Spec.AdditionalConfiguration == nil {
					cluster.Spec.AdditionalConfiguration = make(map[string]string)
				}
				cluster.Spec.AdditionalConfiguration["redpanda."+managedProp] = desiredManagedPropValue
			}), timeout, interval).Should(Succeed())

			By("Having it reflected in the configuration")
			Eventually(adminAPI.PropertyGetter(managedProp), timeout, interval).Should(Equal(desiredManagedPropValue))
			// Unmanaged properties are removed by Declarative mode
			Eventually(adminAPI.PropertyGetter(unmanagedProp), timeout, interval).Should(BeNil())

			By("Synchronizing the condition")
			Eventually(clusterConfiguredConditionStatusGetter(key), timeout, interval).Should(BeTrue())

			By("Having an external system change both properties")
			adminAPI.SetProperty(managedProp, externalChangeManagedPropValue)
			adminAPI.SetProperty(unmanagedProp, externalChangeUnmanagedPropValue)

			By("Having the managed property restored to the original value")
			Eventually(adminAPI.PropertyGetter(managedProp), timeout, interval).Should(Equal(desiredManagedPropValue))

			By("Removing the unmanaged property via Declarative mode")
			Expect(adminAPI.PropertyGetter(unmanagedProp)()).Should(BeNil())

			By("Deleting the cluster")
			Expect(k8sClient.Delete(context.Background(), redpandaCluster, deleteOptions)).Should(Succeed())
			testutils.DeleteAllInNamespace(testEnv, k8sClient, namespace)
		})
	})

	Context("When managing a RedpandaCluster with additional command line arguments", func() {
		It("Can initialize a cluster with centralized configuration", func() {
			args := map[string]string{
				"overprovisioned":   "",
				"default-log-level": "info",
			}

			By("Allowing creation of a new cluster")
			key, baseKey, redpandaCluster, namespace, _ := getInitialTestCluster("test-additional-cmdline")
			redpandaCluster.Spec.Configuration.AdditionalCommandlineArguments = args
			Expect(k8sClient.Create(context.Background(), namespace)).Should(Succeed())
			Expect(k8sClient.Create(context.Background(), redpandaCluster)).Should(Succeed())

			By("Creating the Configmap and the statefulset")
			Eventually(resourceGetter(baseKey, &corev1.ConfigMap{}), timeout, interval).Should(Succeed())

			var sts appsv1.StatefulSet
			Eventually(resourceGetter(key, &appsv1.StatefulSet{})).Should(Succeed())
			Consistently(resourceDataGetter(key, &sts, func() interface{} {
				return *sts.Spec.Replicas
			}), timeoutShort, intervalShort).Should(Equal(int32(1)))

			By("Looking for the correct arguments")
			cnt := 0
			finalArgs := make(map[string]int)
			for k, v := range args {
				key := fmt.Sprintf("--%s", k)
				if v != "" {
					key = fmt.Sprintf("%s=%s", key, v)
				}
				finalArgs[key] = 1
			}
			for _, arg := range sts.Spec.Template.Spec.Containers[0].Args {
				if _, ok := finalArgs[arg]; ok {
					cnt++
				}
			}
			Expect(cnt).To(Equal(len(args)))

			By("Setting the configmap-hash annotation on the statefulset")
			Eventually(annotationGetter(key, &sts, nodeConfigHashKey), timeout, interval).ShouldNot(BeEmpty())

			By("Synchronizing the condition")
			Eventually(clusterConfiguredConditionStatusGetter(key), timeout, interval).Should(BeTrue())

			By("Deleting the cluster")
			Expect(k8sClient.Delete(context.Background(), redpandaCluster)).Should(Succeed())
		})
	})
})

func getInitialTestCluster(
	name string,
) (
	key types.NamespacedName,
	baseKey types.NamespacedName,
	cluster *vectorizedv1alpha1.Cluster,
	namespace *corev1.Namespace,
	api *admin.MockAdminAPI,
) {
	ns := strings.Replace(namesgenerator.GetRandomName(0), "_", "-", 1)
	key = types.NamespacedName{
		Name:      name,
		Namespace: ns,
	}
	baseKey = types.NamespacedName{
		Name:      name + baseSuffix,
		Namespace: ns,
	}

	cluster = &vectorizedv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      key.Name,
			Namespace: key.Namespace,
		},
		Spec: vectorizedv1alpha1.ClusterSpec{
			Image:    "vectorized/redpanda",
			Version:  versionWithCentralizedConfiguration,
			Replicas: ptr.To(int32(1)),
			Configuration: vectorizedv1alpha1.RedpandaConfig{
				KafkaAPI: []vectorizedv1alpha1.KafkaAPI{
					{
						Port: 9092,
					},
				},
				AdminAPI: []vectorizedv1alpha1.AdminAPI{{Port: 9644}},
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
	namespace = &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: ns,
		},
	}
	return key, baseKey, cluster, namespace, testAdminAPI(fmt.Sprintf("%s/%s", cluster.Namespace, cluster.Name))
}
