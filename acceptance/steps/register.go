// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package steps

import framework "github.com/redpanda-data/redpanda-operator/harpoon"

func init() {
	// General scenario steps
	framework.RegisterStep(`^(vectorized )?cluster "([^"]*)" is available$`, checkClusterAvailability)
	framework.RegisterStep(`^I apply Kubernetes manifest:$`, iApplyKubernetesManifest)
	framework.RegisterStep(`^I exec "([^"]+)" in a Pod matching "([^"]+)", it will output:$`, execInPodMatchingEventuallyMatches)
	framework.RegisterStep(`^kubectl exec -it "([^"]+)" "([^"]+)" will eventually output:$`, execInPodEventuallyMatches)
	framework.RegisterStep(`Pod "([^"]+)" (?:will|is) eventually(?: be)? (Running|Pending)`, podWillEventuallyBeInPhase)

	framework.RegisterStep(`^I store "([^"]*)" of Kubernetes object with type "([^"]*)" and name "([^"]*)" as "([^"]*)"$`, recordVariable)
	framework.RegisterStep(`^the recorded value "([^"]*)" has the same value as "([^"]*)" of the Kubernetes object with type "([^"]*)" and name "([^"]*)"$`, assertVariableValue)
	framework.RegisterStep(`^the recorded value "([^"]*)" is one less than "([^"]*)" of the Kubernetes object with type "([^"]*)" and name "([^"]*)"$`, assertVariableValueIncremented)
	framework.RegisterStep(`^I enable feature "([^"]*)" on( vectorized)? cluster "([^"]*)"`, enableDevelopmentFeatureOn)
	framework.RegisterStep(`^I enable "([^"]*)" logging for the "([^"]*)" logger on( vectorized)? cluster "([^"]*)"`, setLogLevelOn)

	// Schema scenario steps
	framework.RegisterStep(`^there is a schema "([^"]*)" in( vectorized)? cluster "([^"]*)"$`, thereIsASchema)
	framework.RegisterStep(`^there is no schema "([^"]*)" in( vectorized)? cluster "([^"]*)"$`, thereIsNoSchema)
	framework.RegisterStep(`^schema "([^"]*)" is successfully synced$`, schemaIsSuccessfullySynced)
	framework.RegisterStep(`^I should be able to check compatibility against "([^"]*)" in( vectorized)? cluster "([^"]*)"$`, iShouldBeAbleToCheckCompatibilityAgainst)

	// Topic scenario steps
	framework.RegisterStep(`^there is no topic "([^"]*)" in( vectorized)? cluster "([^"]*)"$`, thereIsNoTopic)
	framework.RegisterStep(`^topic "([^"]*)" is successfully synced$`, topicIsSuccessfullySynced)
	framework.RegisterStep(`^I should be able to produce and consume from "([^"]*)" in( vectorized)? cluster "([^"]*)"$`, iShouldBeAbleToProduceAndConsumeFrom)
	framework.RegisterStep(`I create topic "([^"]*)" in( vectorized)? cluster "([^"]*)"`, iCreateTopicInCluster)
	framework.RegisterStep(`I create topic "([^"]*)" with (\d+) partitions and replication factor (\d+) in( vectorized)? cluster "([^"]*)"`, iCreateTopicWithShapeInCluster)
	framework.RegisterStep(`I should find topic "([^"]*)" in( vectorized)? cluster "([^"]*)"`, iShouldFindTopicIn)

	// ShadowLink scenario steps
	framework.RegisterStep(`^shadow link "([^"]*)" is successfully synced$`, shadowLinkIsSuccessfullySynced)

	// User scenario steps
	framework.RegisterStep(`^user "([^"]*)" is successfully synced$`, userIsSuccessfullySynced)
	framework.RegisterStep(`^"([^"]*)" should be able to read from topic "([^"]*)" in( vectorized)? cluster "([^"]*)"$`, userShouldBeAbleToReadFromTopicInCluster)
	framework.RegisterStep(`^there is no user "([^"]*)" in( vectorized)? cluster "([^"]*)"$`, thereIsNoUser)
	framework.RegisterStep(`^there are already the following ACLs in( vectorized)? cluster "([^"]*)":$`, thereAreAlreadyTheFollowingACLsInCluster)
	framework.RegisterStep(`^there are the following pre-existing users in( vectorized)? cluster "([^"]*)"$`, thereAreTheFollowingPreexistingUsersInCluster)
	framework.RegisterStep(`^I create CRD-based users for( vectorized)? cluster "([^"]*)":$`, iCreateCRDbasedUsers)
	framework.RegisterStep(`^I delete the CRD user "([^"]*)"$`, iDeleteTheCRDUser)
	framework.RegisterStep(`^there should be ACLs in the( vectorized)? cluster "([^"]*)" for user "([^"]*)"$`, thereShouldBeACLsInTheClusterForUser)
	framework.RegisterStep(`^"([^"]*)" should exist and be able to authenticate to the( vectorized)? "([^"]*)" cluster$`, shouldExistAndBeAbleToAuthenticateToTheCluster)
	framework.RegisterStep(`^"([^"]*)" should be able to authenticate to the( vectorized)? "([^"]*)" cluster with password "([^"]*)" and mechanism "([^"]*)"$`, shouldBeAbleToAuthenticateToTheClusterWithPasswordAndMechanism)

	// Group scenario steps
	framework.RegisterStep(`^group "([^"]*)" is successfully synced$`, groupIsSuccessfullySynced)
	framework.RegisterStep(`^I delete the CRD group "([^"]*)"$`, iDeleteTheCRDGroup)
	framework.RegisterStep(`^group "([^"]*)" should have (\d+) ACLs for topic pattern "([^"]*)" in( vectorized)? cluster "([^"]*)"$`, groupShouldHaveNACLsForTopicPatternInCluster)
	framework.RegisterStep(`^group "([^"]*)" should have ACLs in( vectorized)? cluster "([^"]*)"$`, groupShouldHaveACLsInCluster)
	framework.RegisterStep(`^there should be no ACLs for group "([^"]*)" in( vectorized)? cluster "([^"]*)"$`, thereShouldBeNoACLsForGroupInCluster)

	// Role scenario steps
	framework.RegisterStep(`^role "([^"]*)" is successfully synced$`, roleIsSuccessfullySynced)
	framework.RegisterStep(`^I delete the CRD role "([^"]*)"$`, iDeleteTheCRDRole)
	framework.RegisterStep(`^there is no role "([^"]*)" in( vectorized)? cluster "([^"]*)"$`, thereIsNoRole)
	framework.RegisterStep(`^role "([^"]*)" should exist in( vectorized)? cluster "([^"]*)"$`, roleShouldExistInCluster)
	framework.RegisterStep(`^there should be no role "([^"]*)" in( vectorized)? cluster "([^"]*)"$`, thereShouldBeNoRoleInCluster)
	framework.RegisterStep(`^role "([^"]*)" should not have member "([^"]*)" in( vectorized)? cluster "([^"]*)"$`, roleShouldNotHaveMemberInCluster)
	framework.RegisterStep(`^role "([^"]*)" should have ACLs for topic pattern "([^"]*)" in( vectorized)? cluster "([^"]*)"$`, roleShouldHaveACLsForTopicPatternInCluster)
	framework.RegisterStep(`^role "([^"]*)" should have no managed ACLs in( vectorized)? cluster "([^"]*)"$`, roleShouldHaveNoManagedACLsInCluster)
	framework.RegisterStep(`^there should be no ACLs for role "([^"]*)" in( vectorized)? cluster "([^"]*)"$`, thereShouldBeNoACLsForRoleInCluster)
	framework.RegisterStep(`^role "([^"]*)" should have members "([^"]*)" in( vectorized)? cluster "([^"]*)"$`, roleShouldHaveMembersAndInCluster)
	framework.RegisterStep(`^there is a pre-existing role "([^"]*)" in( vectorized)? cluster "([^"]*)"$`, thereIsAPreExistingRole)
	framework.RegisterStep(`^there should still be role "([^"]*)" in( vectorized)? cluster "([^"]*)"$`, thereShouldStillBeRole)
	framework.RegisterStep(`^RedpandaRole "([^"]*)" should have no members in( vectorized)? cluster "([^"]*)"$`, roleShouldHaveNoMembersInCluster)
	framework.RegisterStep(`^RedpandaRole "([^"]*)" should have status field "([^"]*)" set to "([^"]*)"$`, redpandaRoleShouldHaveStatusFieldSetTo)

	// Direct testing with effective role names (using the role name as it appears in Redpanda)
	framework.RegisterStep(`^role "([^"]*)" should exist in( vectorized)? cluster "([^"]*)" with effective name "([^"]*)"$`, roleShouldExistInClusterWithEffectiveName)
	framework.RegisterStep(`^there should be no role "([^"]*)" in( vectorized)? cluster "([^"]*)" with effective name "([^"]*)"$`, thereShouldBeNoRoleInClusterWithEffectiveName)
	framework.RegisterStep(`^role "([^"]*)" should have members "([^"]*)" in( vectorized)? cluster "([^"]*)" with effective name "([^"]*)"$`, roleShouldHaveMembersWithEffectiveName)

	// Metrics scenario steps
	framework.RegisterStep(`^the operator is running$`, operatorIsRunning)
	framework.RegisterStep(`^its metrics endpoint should reject http request with status code "([^"]*)"$`, requestMetricsEndpointPlainHTTP)
	framework.RegisterStep(`^its metrics endpoint should reject authorization random token request with status code "([^"]*)"$`, requestMetricsEndpointWithTLSAndRandomToken)
	framework.RegisterStep(`^"([^"]*)" service account has bounded "([^"]*)" regexp cluster role name$`, createClusterRoleBinding)
	framework.RegisterStep(`^its metrics endpoint should accept https request with "([^"]*)" service account token$`, acceptServiceAccountMetricsRequest)

	// Helm steps
	// I helm install "release-name" "chart/path" with values:
	// I can helm install "release-name" "chart/path" with values:
	// I helm install "release-name" "chart/path" --version v1.2.3 with values:
	framework.RegisterStep(`I(?: can)? helm install "([^"]+)" "([^"]+)"(?: --version (\S+))? with values:`, iHelmInstall)
	// I helm upgrade "release-name" "chart/path" with values:
	// I can helm upgrade "release-name" "chart/path" with values:
	// I helm upgrade "release-name" "chart/path" --version v1.2.3 with values:
	framework.RegisterStep(`I(?: can)? helm upgrade "([^"]+)" "([^"]+)"(?: --version (\S+))? with values:`, iHelmUpgrade)
	// the stored manifest for release "release-name" re-renders Secret "secret-name" without server-set metadata
	framework.RegisterStep(`^the stored manifest for release "([^"]+)" re-renders Secret "([^"]+)" without server-set metadata$`, helmRerendersSecretWithoutServerSetMetadata)
	// the stored manifest for release "release-name" re-renders Secret "secret-name" with the password in use
	framework.RegisterStep(`^the stored manifest for release "([^"]+)" re-renders Secret "([^"]+)" with the password in use$`, helmRerendersSecretWithLivePassword)

	// Helm migration scenario steps
	framework.RegisterStep(`^the Kubernetes object of type "([^"]*)" with name "([^"]*)" has an OwnerReference pointing to the cluster "([^"]*)"$`, kubernetesObjectHasClusterOwner)
	framework.RegisterStep(`^the helm release for "([^"]*)" can be deleted by removing its stored secret$`, iDeleteHelmReleaseSecret)
	framework.RegisterStep(`^the cluster "([^"]*)" is healthy$`, redpandaClusterIsHealthy)

	// Rolling restart scenario steps
	framework.RegisterStep(`^I create a sentinel topic in the stretch cluster of "([^"]*)"$`, createSentinelTopicInStretchCluster)
	framework.RegisterStep(`^I upgrade the RedpandaBrokerPools in "([^"]*)" to use image "([^"]*)"$`, upgradeBrokerPoolsToImage)
	framework.RegisterStep(`^the upgrade of "([^"]*)" completes with at most 1 pod unavailable at a time$`, upgradeCompletesWithAtMostOneUnavailable)
	framework.RegisterStep(`^the sentinel data is still readable in "([^"]*)"$`, sentinelDataIsReadable)

	// Multicluster scenario steps
	framework.RegisterStep(`^I create a multicluster operator named "([^"]*)" with (\d+) nodes$`, createNetworkedVClusterOperators)
	framework.RegisterStep(`^I apply a multicluster Kubernetes manifest to "([^"]*)":$`, iApplyKuberneteMulticlusterManifest)
	framework.RegisterStep(`^in "([^"]*)" the Kubernetes object "([^"]*)" in namespace "([^"]*)" of type "([^"]*)" should have finalizer "([^"]*)"$`, checkMulticlusterFinalizers)
	framework.RegisterStep(`^in "([^"]*)" the Kubernetes object "([^"]*)" in namespace "([^"]*)" of type "([^"]*)" should have condition "([^"]*)" with status "([^"]*)"$`, checkMulticlusterCondition)
	framework.RegisterStep(`^I apply a RedpandaBrokerPool Kubernetes manifest to "([^"]*)":$`, applyBrokerPoolWithStretchCluster)
	framework.RegisterStep(`^I expect (\d+) statefulsets in (\d+) kubernetes cluster to be created and eventually ready$`, expectStatefulsetsReady)
	framework.RegisterStep(`^I expect all (\d+) RedpandaBrokerPools in "([^"]*)" to be eventually bound and deployed$`, expectBrokerPoolsBoundAndDeployed)
	framework.RegisterStep(`^I execute "([^"]*)" command in the statefulset container in each cluster$`, executeCommandInStatefulsetContainers)
	framework.RegisterStep(`^I expect them to return the same Redpanda broker list$`, expectSameBrokerList)
	framework.RegisterStep(`^I apply a RedpandaBrokerPool Kubernetes manifest to "([^"]*)" with extra spec for region "([^"]*)":$`, applyBrokerPoolsWithRegionOverride)
	framework.RegisterStep(`^in "([^"]*)" the StatefulSet for region "([^"]*)" of StretchCluster "([^"]*)" should have data-dir PVC annotations:$`, checkDataDirPVCAnnotations)

	// Regional outage scenario steps
	framework.RegisterStep(`^I take the "([^"]*)" region of "([^"]*)" offline$`, takeRegionOffline)
	framework.RegisterStep(`^I bring the "([^"]*)" region of "([^"]*)" back online$`, bringRegionOnline)
	framework.RegisterStep(`^the remaining regions of "([^"]*)" should eventually report SpecSynced as "([^"]*)"$`, remainingRegionsReportSpecSynced)
	framework.RegisterStep(`^all regions of "([^"]*)" should eventually report SpecSynced as "([^"]*)"$`, allRegionsReportSpecSynced)
	framework.RegisterStep(`^the remaining regions of "([^"]*)" should eventually report the "([^"]*)" broker as unavailable$`, remainingRegionsReportBrokerUnavailable)
	framework.RegisterStep(`^the reachable regions of "([^"]*)" should eventually reflect the updated StretchCluster spec$`, reachableRegionsReflectUpdatedSpec)
	framework.RegisterStep(`^the "([^"]*)" region of "([^"]*)" should reflect the updated StretchCluster spec$`, regionReflectsUpdatedSpec)
	framework.RegisterStep(`^the operator in the "([^"]*)" region of "([^"]*)" should eventually be running and reconciling$`, operatorInRegionRecovering)

	// Ghost node ejection scenario steps
	framework.RegisterStep(`^I take a non-controller region of "([^"]*)" offline$`, takeNonControllerRegionOffline)
	framework.RegisterStep(`^the cluster health output should show (\d+) nodes across all clusters in "([^"]*)"$`, expectClusterHealthNodeCount)
	framework.RegisterStep(`^the cluster health output should eventually show (\d+) nodes in the remaining clusters of "([^"]*)"$`, expectEventualNodeCountInRemainingClusters)

	// Scaling scenario steps
	framework.RegisterStep(`^cluster "([^"]*)" should be stable with (\d+) nodes$`, checkClusterStableWithCount)
	framework.RegisterStep(`^cluster "([^"]*)" is stable with (\d+) nodes$`, checkClusterStableWithCount)
	framework.RegisterStep(`^I create a basic cluster "([^"]*)" with (\d+) nodes$`, iCreateABasicClusterWithNodes)
	framework.RegisterStep(`^I scale "([^"]*)" to (\d+) nodes$`, iScaleToNodes)
	framework.RegisterStep(`^the in-pod rpk broker list for every broker in "([^"]*)" matches the current pods without a restart$`, theInPodRPKSeedListMatchesCurrentPods)

	// General cluster scenario steps
	framework.RegisterStep(`^service "([^"]*)" has named port "([^"]*)" with value (\d+)$`, checkServiceWithPort)
	framework.RegisterStep(`^service "([^"]*)" should have named port "([^"]*)" with value (\d+)$`, checkServiceWithPort)
	framework.RegisterStep(`^rpk is configured correctly in "([^"]*)" cluster$`, checkRPKCommands)
	framework.RegisterStep("running `(.*)` will output:$", runScriptInClusterCheckOutput)

	// Decommissioning scenario steps
	framework.RegisterStep(`^cluster "([^"]*)" is unhealthy$`, checkClusterUnhealthy)
	framework.RegisterStep(`^cluster "([^"]*)" should recover$`, checkClusterHealthy)
	framework.RegisterStep(`^I physically shutdown a kubernetes node for cluster "([^"]*)"$`, shutdownRandomClusterNode)
	framework.RegisterStep(`^I prune any kubernetes node that is now in a NotReady status$`, deleteNotReadyKubernetesNodes)
	framework.RegisterStep(`I stop the Node running Pod "([^"]+)"`, shutdownNodeOfPod)
	framework.RegisterStep(`^cluster "([^"]*)" has only (\d+) remaining nodes$`, checkClusterNodeCount)
	framework.RegisterStep(`^I prune kubernetes node that was removed in previous step$`, deleteKubernetesNodesFromContext)

	// Operator upgrade scenario steps
	framework.RegisterStep(`^I install local CRDs from "([^"]*)"`, iInstallLocalCRDs)

	// Stretch-cluster operator upgrade scenario steps
	framework.RegisterStep(`^I create a multicluster operator named "([^"]*)" with (\d+) nodes using helm chart "([^"]*)" version "([^"]*)"$`, createNetworkedVClusterOperatorsWithChart)
	framework.RegisterStep(`^the multicluster operator raft quorum in "([^"]*)" is healthy$`, multiclusterOperatorRaftQuorumIsHealthy)
	framework.RegisterStep(`^I upgrade the multicluster operator in "([^"]*)" to the local dev chart, one vcluster at a time$`, upgradeMulticlusterOperatorOneAtATime)

	// Console scenario steps
	framework.RegisterStep(`^Console "([^"]+)" will be healthy`, consoleIsHealthy)
	framework.RegisterStep(`^the migrated console cluster "([^"]+)" should have (\d+) warning(s)?$`, consoleHasWarnings)

	// Regression steps
	framework.RegisterStep(`^service "([^"]*)" should have field managers:$`, checkResourceFieldManagers)
	framework.RegisterStep(`^service "([^"]*)" should not have field managers:$`, checkResourceNoFieldManagers)
	framework.RegisterStep(`^cluster "([^"]*)" should have sync error:$`, checkClusterHasSyncError)

	// Broker CRD scenario steps
	framework.RegisterStep(`^I pause reconciliation on cluster "([^"]*)"$`, pauseReconciliation)
	framework.RegisterStep(`^I orphan-delete the StatefulSet for cluster "([^"]*)"$`, orphanDeleteStatefulSet)
	framework.RegisterStep(`^I create Broker CRs for cluster "([^"]*)"$`, createBrokerCRsForCluster)
	framework.RegisterStep(`^I grant a roll-grant to Broker "([^"]*)"$`, grantRollGrantToBroker)
	framework.RegisterStep(`^I add additional configuration "([^"]*)" with value "([^"]*)" to V1 cluster "([^"]*)"$`, addAdditionalConfigurationToV1Cluster)
	framework.RegisterStep(`^I set nodePool "([^"]*)" replicas to (\d+) on V1 cluster "([^"]*)"$`, setNodePoolReplicasOnV1Cluster)
	framework.RegisterStep(`^I scale nodePool "([^"]*)" to (\d+) and nodePool "([^"]*)" to (\d+) replicas on V1 cluster "([^"]*)" in a single update$`, scaleTwoNodePoolsOnV1Cluster)
	framework.RegisterStep(`^at most one Broker of cluster "([^"]*)" should be decommissioning at any time until it has (\d+) Broker CRs$`, atMostOneBrokerDecommissioningUntil)
	framework.RegisterStep(`^I add nodePool "([^"]*)" with (\d+) replicas? to V1 cluster "([^"]*)"$`, addNodePoolToV1Cluster)
	framework.RegisterStep(`^I remove nodePool "([^"]*)" from V1 cluster "([^"]*)"$`, removeNodePoolFromV1Cluster)
	framework.RegisterStep(`^pods for cluster "([^"]*)" should roll one at a time$`, podsShouldRollOneAtATime)
	framework.RegisterStep(`^all Broker CRs for cluster "([^"]*)" should be Running$`, allBrokerCRsRunning)
	framework.RegisterStep(`^all Broker CRs for cluster "([^"]*)" should be Stable$`, allBrokerCRsStable)
	framework.RegisterStep(`^cluster "([^"]*)" should have (\d+) Broker CRs$`, clusterShouldHaveNBrokerCRs)
	framework.RegisterStep(`^cluster "([^"]*)" should eventually have (\d+) Broker CRs$`, clusterShouldEventuallyHaveNBrokerCRs)
	framework.RegisterStep(`^no StatefulSet should exist for cluster "([^"]*)"$`, noStatefulSetShouldExistForCluster)
	framework.RegisterStep(`^no StatefulSet should eventually exist for cluster "([^"]*)"$`, noStatefulSetShouldEventuallyExistForCluster)
	framework.RegisterStep(`^a StatefulSet should exist for cluster "([^"]*)"$`, statefulSetShouldExistForCluster)
	framework.RegisterStep(`^a StatefulSet should eventually exist for cluster "([^"]*)"$`, statefulSetShouldEventuallyExistForCluster)
	framework.RegisterStep(`^I set annotation "([^"]*)" to "([^"]*)" on V1 cluster "([^"]*)"$`, setAnnotationOnV1Cluster)
	framework.RegisterStep(`^I remove annotation "([^"]*)" from V1 cluster "([^"]*)"$`, removeAnnotationFromV1Cluster)
	framework.RegisterStep(`^I set annotation "([^"]*)" to "([^"]*)" on Redpanda "([^"]*)"$`, setAnnotationOnRedpanda)
	framework.RegisterStep(`^I remove annotation "([^"]*)" from Redpanda "([^"]*)"$`, removeAnnotationFromRedpanda)
	framework.RegisterStep(`^I set decommission on Broker "([^"]*)"$`, setDecommissionOnBroker)
	framework.RegisterStep(`^I set decommission on the Broker with index (\d+) of cluster "([^"]*)"$`, setDecommissionOnBrokerWithIndex)
	framework.RegisterStep(`^the Broker with index (\d+) of cluster "([^"]*)" should be replaced$`, brokerWithIndexShouldBeReplaced)
	framework.RegisterStep(`^Broker "([^"]*)" should reach phase "([^"]*)"$`, brokerShouldReachPhase)
	framework.RegisterStep(`^cluster "([^"]*)" admin API should show (\d+) brokers$`, clusterAdminAPIShouldShowBrokers)
	framework.RegisterStep(`^I update Broker "([^"]*)" pod template with env "([^"]*)" for cluster "([^"]*)"$`, updateBrokerPodTemplateEnv)
	framework.RegisterStep(`^Broker "([^"]*)" pod should have env "([^"]*)" = "([^"]*)"$`, brokerPodShouldHaveEnv)
	framework.RegisterStep(`^Broker "([^"]*)" pod should not be rotated$`, brokerPodShouldNotBeRotated)
	framework.RegisterStep(`^Broker "([^"]*)" should not be in maintenance mode in cluster "([^"]*)"$`, brokerShouldNotBeInMaintenanceMode)
	framework.RegisterStep(`^Broker "([^"]*)" PVCs should be owned by its Broker CR in cluster "([^"]*)"$`, brokerPVCsShouldBeOwnedByBrokerCR)
	framework.RegisterStep(`^all Broker CRs for cluster "([^"]*)" should have conditions Ready, PodScheduled, BrokerRegistered, StorageBound as True$`, allBrokerCRsShouldHaveConditions)
	framework.RegisterStep(`^Broker "([^"]*)" should have condition "([^"]*)" as "([^"]*)"$`, brokerShouldHaveCondition)
	framework.RegisterStep(`^I snapshot pod UIDs for cluster "([^"]*)"$`, snapshotPodUIDs)
	framework.RegisterStep(`^pods for cluster "([^"]*)" should have the same UIDs as the snapshot$`, podUIDsShouldBeUnchanged)

	// Debug steps
	framework.RegisterStep(`^I become debuggable$`, sleepALongTime)
	// Pipeline scenario steps
	framework.RegisterStep(`^pipeline "([^"]*)" is successfully running$`, pipelineIsSuccessfullyRunning)
	framework.RegisterStep(`^pipeline "([^"]*)" is stopped$`, pipelineIsStopped)
	framework.RegisterStep(`^I delete the CRD pipeline "([^"]*)"$`, iDeleteTheCRDPipeline)
	framework.RegisterStep(`^pipeline "([^"]*)" does not exist$`, pipelineDoesNotExist)
	framework.RegisterStep(`^pipeline "([^"]*)" has invalid config$`, pipelineHasInvalidConfig)
	framework.RegisterStep(`^topic "([^"]*)" has messages in cluster "([^"]*)"$`, topicHasMessagesInCluster)
	framework.RegisterStep(`^I produce messages to "([^"]*)" in cluster "([^"]*)"$`, iProduceMessagesToTopicInCluster)
}
