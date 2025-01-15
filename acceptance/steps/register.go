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

	// Operator scenario steps
	framework.RegisterStep(`^the operator is running$`, operatorIsRunning)
	framework.RegisterStep(`^its metrics endpoint should reject http request with status code "([^"]*)"$`, requestMetricsEndpointPlainHTTP)
	framework.RegisterStep(`^its metrics endpoint should reject authorization random token request with status code "([^"]*)"$`, requestMetricsEndpointWithTLSAndRandomToken)
	framework.RegisterStep(`^"([^"]*)" service account has bounded "([^"]*)" cluster role$`, createClusterRoleBinding)
	framework.RegisterStep(`^its metrics endpoint should accept https request with "([^"]*)" service account token$`, acceptServiceAccountMetricsRequest)
}
