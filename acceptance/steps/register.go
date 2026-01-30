// Copyright 2025 Redpanda Data, Inc.
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
	framework.RegisterStep(`^cluster "([^"]*)" is available$`, checkClusterAvailability)
	framework.RegisterStep(`^I apply Kubernetes manifest:$`, iApplyKubernetesManifest)

	framework.RegisterStep(`I kubectl kustomize "([^"]+)" | kubectl apply -f - --server-side`, iKustomizeApply)
	framework.RegisterStep(`^I exec "([^"]+)" in a Pod matching "([^"]+)", it will output:$`, execInPodMatchingEventuallyMatches)
	framework.RegisterStep(`^kubectl exec -it "([^"]+)" "([^"]+)" will eventually output:$`, execInPodEventuallyMatches)
	framework.RegisterStep(`Pod "([^"]+)" (?:will|is) eventually(?: be)? (Running|Pending)`, podWillEventuallyBeInPhase)

	framework.RegisterStep(`^I store "([^"]*)" of Kubernetes object with type "([^"]*)" and name "([^"]*)" as "([^"]*)"$`, recordVariable)
	framework.RegisterStep(`^the recorded value "([^"]*)" has the same value as "([^"]*)" of the Kubernetes object with type "([^"]*)" and name "([^"]*)"$`, assertVariableValue)
	framework.RegisterStep(`^the recorded value "([^"]*)" is one less than "([^"]*)" of the Kubernetes object with type "([^"]*)" and name "([^"]*)"$`, assertVariableValueIncremented)

	// Schema scenario steps
	framework.RegisterStep(`^there is no schema "([^"]*)" in cluster "([^"]*)"$`, thereIsNoSchema)
	framework.RegisterStep(`^schema "([^"]*)" is successfully synced$`, schemaIsSuccessfullySynced)
	framework.RegisterStep(`^I should be able to check compatibility against "([^"]*)" in cluster "([^"]*)"$`, iShouldBeAbleToCheckCompatibilityAgainst)

	// Topic scenario steps
	framework.RegisterStep(`^there is no topic "([^"]*)" in cluster "([^"]*)"$`, thereIsNoTopic)
	framework.RegisterStep(`^topic "([^"]*)" is successfully synced$`, topicIsSuccessfullySynced)
	framework.RegisterStep(`^I should be able to produce and consume from "([^"]*)" in cluster "([^"]*)"$`, iShouldBeAbleToProduceAndConsumeFrom)

	// User scenario steps
	framework.RegisterStep(`^user "([^"]*)" is successfully synced$`, userIsSuccessfullySynced)

	framework.RegisterStep(`^there is no user "([^"]*)" in cluster "([^"]*)"$`, thereIsNoUser)
	framework.RegisterStep(`^there are already the following ACLs in cluster "([^"]*)":$`, thereAreAlreadyTheFollowingACLsInCluster)
	framework.RegisterStep(`^there are the following pre-existing users in cluster "([^"]*)"$`, thereAreTheFollowingPreexistingUsersInCluster)

	framework.RegisterStep(`^I create CRD-based users for cluster "([^"]*)":$`, iCreateCRDbasedUsers)
	framework.RegisterStep(`^I delete the CRD user "([^"]*)"$`, iDeleteTheCRDUser)

	framework.RegisterStep(`^"([^"]*)" should exist and be able to authenticate to the "([^"]*)" cluster$`, shouldExistAndBeAbleToAuthenticateToTheCluster)
	framework.RegisterStep(`^"([^"]*)" should be able to authenticate to the "([^"]*)" cluster with password "([^"]*)" and mechanism "([^"]*)"$`, shouldBeAbleToAuthenticateToTheClusterWithPasswordAndMechanism)
	framework.RegisterStep(`^there should be ACLs in the cluster "([^"]*)" for user "([^"]*)"$`, thereShouldBeACLsInTheClusterForUser)

	// Metrics scenario steps
	framework.RegisterStep(`^the operator is running$`, operatorIsRunning)
	framework.RegisterStep(`^its metrics endpoint should reject http request with status code "([^"]*)"$`, requestMetricsEndpointPlainHTTP)
	framework.RegisterStep(`^its metrics endpoint should reject authorization random token request with status code "([^"]*)"$`, requestMetricsEndpointWithTLSAndRandomToken)
	framework.RegisterStep(`^"([^"]*)" service account has bounded "([^"]*)" cluster role$`, createClusterRoleBinding)
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

	// Helm migration scenario steps
	framework.RegisterStep(`^the Kubernetes object of type "([^"]*)" with name "([^"]*)" has an OwnerReference pointing to the cluster "([^"]*)"$`, kubernetesObjectHasClusterOwner)
	framework.RegisterStep(`^the helm release for "([^"]*)" can be deleted by removing its stored secret$`, iDeleteHelmReleaseSecret)
	framework.RegisterStep(`^the cluster "([^"]*)" is healthy$`, redpandaClusterIsHealthy)

	// Scaling scenario steps
	framework.RegisterStep(`^cluster "([^"]*)" should be stable with (\d+) nodes$`, checkClusterStableWithCount)
	framework.RegisterStep(`^cluster "([^"]*)" is stable with (\d+) nodes$`, checkClusterStableWithCount)
	framework.RegisterStep(`^I create a basic cluster "([^"]*)" with (\d+) nodes$`, iCreateABasicClusterWithNodes)
	framework.RegisterStep(`^I scale "([^"]*)" to (\d+) nodes$`, iScaleToNodes)

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
	framework.RegisterStep(`^I prune kubernetes node that was removed in previous step$`, deleteKubernetesNodesFromContext)
	framework.RegisterStep(`I stop the Node running Pod "([^"]+)"`, shutdownNodeOfPod)
	framework.RegisterStep(`^cluster "([^"]*)" has only (\d+) remaining nodes$`, checkClusterNodeCount)

<<<<<<< HEAD
	// User CRDs with Redpanda 25.2 scenario steps
	framework.RegisterStep(`^I create a sasl cluster named "([^"]*)"$`, iCreateSASLCluster)
	framework.RegisterStep(`^I upgrade to "([^"]*)" cluster to 25.2.1$`, iUpgradeCluster)
	framework.RegisterStep(`^I should be able to modify CRD-based users for cluster "([^"]*)":$`, iUpdateCRDbasedUsers)
=======
	// Operator upgrade scenario steps
	framework.RegisterStep(`^I install local CRDs from "([^"]*)"`, iInstallLocalCRDs)

	// Console scenario steps
	framework.RegisterStep(`^Console "([^"]+)" will be healthy`, consoleIsHealthy)
	framework.RegisterStep(`^the migrated console cluster "([^"]+)" should have (\d+) warning(s)?$`, consoleHasWarnings)

	// Regression steps
	framework.RegisterStep(`^service "([^"]*)" should have field managers:$`, checkResourceFieldManagers)
	framework.RegisterStep(`^service "([^"]*)" should not have field managers:$`, checkResourceNoFieldManagers)
	framework.RegisterStep(`^cluster "([^"]*)" should have sync error:$`, checkClusterHasSyncError)

	// Debug steps
	framework.RegisterStep(`^I become debuggable$`, sleepALongTime)
>>>>>>> f1112cbe (Add migration job to handle mismatched field managers (#1249))
}
