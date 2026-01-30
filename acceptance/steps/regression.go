// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package steps

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

func checkClusterHasSyncError(ctx context.Context, t framework.TestingT, clusterName string, errdoc *godog.DocString) {
	errorString := strings.TrimSpace(errdoc.Content)

	var cluster redpandav1alpha2.Redpanda

	key := t.ResourceKey(clusterName)

	t.Logf("Checking cluster %q has sync error", clusterName)
	require.Eventually(t, func() bool {
		require.NoError(t, t.Get(ctx, key, &cluster))
		hasCondition := t.HasCondition(metav1.Condition{
			Type:   "ResourcesSynced",
			Status: metav1.ConditionFalse,
			Reason: "Error",
		}, cluster.Status.Conditions)

		t.Logf(`Checking cluster resource conditions contains an errored "ResourcesSynced"? %v`, hasCondition)
		if !hasCondition {
			return false
		}

		for _, condition := range cluster.Status.Conditions {
			if condition.Type == "ResourcesSynced" && condition.Status == metav1.ConditionFalse && condition.Reason == "Error" {
				t.Logf("Found error message: %q", condition.Message)
				return strings.Contains(condition.Message, errorString)
			}
		}

		return false
	}, 5*time.Minute, 5*time.Second, "%s", delayLog(func() string {
		return fmt.Sprintf(`Cluster %q never contained an error on the condition reason "ResourcesSynced" with a matching error string: %q, final Conditions: %+v`, key.String(), errorString, cluster.Status.Conditions)
	}))
}

func checkResourceNoFieldManagers(ctx context.Context, t framework.TestingT, clusterName string, list *godog.DocString) {
	fieldManagerCheck(ctx, t, false, clusterName, list.Content)
}

func checkResourceFieldManagers(ctx context.Context, t framework.TestingT, clusterName string, list *godog.DocString) {
	fieldManagerCheck(ctx, t, true, clusterName, list.Content)
}

func fieldManagerCheck(ctx context.Context, t framework.TestingT, presence bool, clusterName, managerList string) {
	managers := []string{}
	for _, line := range strings.Split(strings.TrimSpace(managerList), "\n") {
		if manager := strings.TrimSpace(line); manager != "" {
			managers = append(managers, manager)
		}
	}

	var cluster corev1.Service

	key := t.ResourceKey(clusterName)

	t.Logf("Checking resource %q field manager", clusterName)
	require.Eventually(t, func() bool {
		require.NoError(t, t.Get(ctx, key, &cluster))

		fieldManagers := cluster.GetManagedFields()
		for _, manager := range managers {
			if !slices.ContainsFunc(fieldManagers, func(entry metav1.ManagedFieldsEntry) bool {
				return entry.Manager == manager
			}) {
				t.Logf(`Resource %q does not contain the field manager %q`, key.String(), manager)
				// negation because if we are checking for presence, then we want to return that the condition
				// is not met
				return !presence
			}
			t.Logf(`Found field manager %q in resource %q`, manager, key.String())
		}
		return presence
	}, 5*time.Minute, 5*time.Second, "%s", delayLog(func() string {
		finalManagers := []string{}
		for _, entry := range cluster.GetManagedFields() {
			finalManagers = append(finalManagers, entry.Manager)
		}
		if presence {
			return fmt.Sprintf(`Resource %q never contained all of the field managers: %+v, was: %+v`, key.String(), managers, finalManagers)
		}
		return fmt.Sprintf(`Resource %q never lacked all of the field managers: %+v, was: %+v`, key.String(), managers, finalManagers)
	}))
}
