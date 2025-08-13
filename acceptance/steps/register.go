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
	framework.RegisterStep(`^(vectorized )?cluster "([^"]*)" is available$`, checkClusterAvailability)
	framework.RegisterStep(`^I apply Kubernetes manifest:$`, iApplyKubernetesManifest)
	framework.RegisterStep(`^I exec "([^"]+)" in a Pod matching "([^"]+)", it will output:$`, iExecInPodMatching)

	framework.RegisterStep(`^I store "([^"]*)" of Kubernetes object with type "([^"]*)" and name "([^"]*)" as "([^"]*)"$`, recordVariable)
	framework.RegisterStep(`^the recorded value "([^"]*)" has the same value as "([^"]*)" of the Kubernetes object with type "([^"]*)" and name "([^"]*)"$`, assertVariableValue)
	framework.RegisterStep(`^the recorded value "([^"]*)" is one less than "([^"]*)" of the Kubernetes object with type "([^"]*)" and name "([^"]*)"$`, assertVariableValueIncremented)
	framework.RegisterStep(`^I enable feature "([^"]*)" on( vectorized)? cluster "([^"]*)"`, enableDevelopmentFeatureOn)
	framework.RegisterStep(`^I enable "([^"]*)" logging for the "([^"]*)" logger on( vectorized)? cluster "([^"]*)"`, setLogLevelOn)

	// Schema scenario steps
	framework.RegisterStep(`^there is no schema "([^"]*)" in( vectorized)? cluster "([^"]*)"$`, thereIsNoSchema)
	framework.RegisterStep(`^schema "([^"]*)" is successfully synced$`, schemaIsSuccessfullySynced)
	framework.RegisterStep(`^I should be able to check compatibility against "([^"]*)" in( vectorized)? cluster "([^"]*)"$`, iShouldBeAbleToCheckCompatibilityAgainst)

	// Topic scenario steps
	framework.RegisterStep(`^there is no topic "([^"]*)" in( vectorized)? cluster "([^"]*)"$`, thereIsNoTopic)
	framework.RegisterStep(`^topic "([^"]*)" is successfully synced$`, topicIsSuccessfullySynced)
	framework.RegisterStep(`^I should be able to produce and consume from "([^"]*)" in( vectorized)? cluster "([^"]*)"$`, iShouldBeAbleToProduceAndConsumeFrom)
	framework.RegisterStep(`I create topic "([^"]*)" in( vectorized)? cluster "([^"]*)"`, iCreateTopicInCluster)
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

	// Metrics scenario steps
	framework.RegisterStep(`^the operator is running$`, operatorIsRunning)
	framework.RegisterStep(`^its metrics endpoint should reject http request with status code "([^"]*)"$`, requestMetricsEndpointPlainHTTP)
	framework.RegisterStep(`^its metrics endpoint should reject authorization random token request with status code "([^"]*)"$`, requestMetricsEndpointWithTLSAndRandomToken)
	framework.RegisterStep(`^"([^"]*)" service account has bounded "([^"]*)" regexp cluster role name$`, createClusterRoleBinding)
	framework.RegisterStep(`^its metrics endpoint should accept https request with "([^"]*)" service account token$`, acceptServiceAccountMetricsRequest)

	// Helm migration scenario steps
	framework.RegisterStep(`^a Helm release named "([^"]*)" of the "([^"]*)" helm chart with the values:$`, iInstallHelmRelease)
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
	framework.RegisterStep(`^cluster "([^"]*)" has only (\d+) remaining nodes$`, checkClusterNodeCount)

	// Operator upgrade scenario steps
	framework.RegisterStep(`^I can upgrade to the latest operator with the values:$`, iCanUpgradeToTheLatestOperatorWithTheValues)
	framework.RegisterStep(`^I install redpanda helm chart version "([^"]*)" with the values:$`, iInstallRedpandaHelmChartVersionWithTheValues)
	framework.RegisterStep(`^I install local CRDs from "([^"]*)"`, iInstallLocalCRDs)

	// Console scenario steps
	framework.RegisterStep(`^Console "([^"]+)" will be healthy`, consoleIsHealthy)
}
