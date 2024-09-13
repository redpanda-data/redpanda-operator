// Copyright 2024 Redpanda Data, Inc.
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
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cucumber/godog"
	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/redpanda-data/helm-charts/pkg/kube"
	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/src/go/k8s/pkg/client"
	"github.com/redpanda-data/redpanda-operator/src/go/k8s/pkg/client/acls"
	"github.com/redpanda-data/redpanda-operator/src/go/k8s/pkg/client/users"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type clusterClients struct {
	Kafka         *kgo.Client
	RedpandaAdmin *rpadmin.AdminAPI
	Users         *users.Client
	ACLs          *acls.Syncer

	resourceTarget *redpandav1alpha2.User
	cluster        string
	factory        *client.Factory
	t              framework.TestingT
}

func (c *clusterClients) AsUser(ctx context.Context, user, password, mechanism string) *clusterClients {
	t := framework.T(ctx)

	referencer := c.resourceTarget
	factory := c.factory.WithUserAuth(&client.UserAuth{
		Username:  user,
		Password:  password,
		Mechanism: mechanism,
	})

	kafka, err := factory.KafkaClient(ctx, referencer)
	require.NoError(t, err)

	redpanda, err := factory.RedpandaAdminClient(ctx, referencer)
	require.NoError(t, err)

	return &clusterClients{
		Kafka:          kafka,
		RedpandaAdmin:  redpanda,
		resourceTarget: referencer,
		factory:        factory,
		t:              t,
	}
}

func (c *clusterClients) ExpectUser(ctx context.Context, user string) {
	c.t.Logf("Checking for user %q in cluster %q", user, c.cluster)
	c.checkUser(ctx, user, true, fmt.Sprintf("User %q does not exist in cluster %q", user, c.cluster))
	c.t.Logf("Found user %q in cluster %q", user, c.cluster)
}

func (c *clusterClients) ExpectNoUser(ctx context.Context, user string) {
	c.t.Logf("Checking that user %q does not exist in cluster %q", user, c.cluster)
	c.checkUser(ctx, user, false, fmt.Sprintf("User %q still exists in cluster %q", user, c.cluster))
	c.t.Logf("Found no user %q in cluster %q", user, c.cluster)
}

func (c *clusterClients) checkUser(ctx context.Context, user string, exists bool, message string) {
	var users []string
	var err error

	if !assert.Eventually(c.t, func() bool {
		c.t.Logf("Pulling list of users from cluster")
		users, err = c.RedpandaAdmin.ListUsers(ctx)
		require.NoError(c.t, err)

		return exists == slices.Contains(users, user)
	}, 10*time.Second, 1*time.Second, message) {
		c.t.Errorf("Final list of users: %v", users)
	}
}

func clientsForCluster(t framework.TestingT, ctx context.Context, cluster string, user *redpandav1alpha2.User) *clusterClients {
	// we construct a fake user to grab all of the clients for the cluster
	referencer := &redpandav1alpha2.User{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: t.Namespace(),
		},
		Spec: redpandav1alpha2.UserSpec{
			ClusterSource: &redpandav1alpha2.ClusterSource{
				ClusterRef: &redpandav1alpha2.ClusterRef{
					Name: cluster,
				},
			},
		},
	}

	factory := client.NewFactory(t.RestConfig(), t).WithDialer(kube.NewPodDialer(t.RestConfig()).DialContext)

	if user != nil && user.Spec.Authentication != nil {
		username := user.Name
		password, err := user.Spec.Authentication.Password.Fetch(ctx, t, user.Namespace)
		require.NoError(t, err)
		mechanism := string(*user.Spec.Authentication.Type)

		factory = factory.WithUserAuth(&client.UserAuth{
			Username:  username,
			Password:  password,
			Mechanism: mechanism,
		})
	}

	kafka, err := factory.KafkaClient(ctx, referencer)
	require.NoError(t, err)

	redpanda, err := factory.RedpandaAdminClient(ctx, referencer)
	require.NoError(t, err)

	users, err := factory.Users(ctx, referencer)
	require.NoError(t, err)

	syncer, err := factory.ACLs(ctx, referencer)
	require.NoError(t, err)

	return &clusterClients{
		Kafka:          kafka,
		RedpandaAdmin:  redpanda,
		Users:          users,
		ACLs:           syncer,
		resourceTarget: referencer,
		cluster:        cluster,
		factory:        factory,
		t:              t,
	}
}

func usersFromACLTable(t framework.TestingT, cluster string, table *godog.Table) []*redpandav1alpha2.User {
	var users []*redpandav1alpha2.User

	for i, row := range table.Rows {
		// skip the header row:
		// | name | acls |
		if i == 0 {
			continue
		}
		name, acls := row.Cells[0].Value, row.Cells[1].Value
		name, acls = strings.TrimSpace(name), strings.TrimSpace(acls)

		users = append(users, userFromRow(t, cluster, name, "", "", acls))
	}

	return users
}

func usersFromAuthTable(t framework.TestingT, cluster string, table *godog.Table) []*redpandav1alpha2.User {
	var users []*redpandav1alpha2.User

	for i, row := range table.Rows {
		// skip the header row:
		// | name | password | mechanism |
		if i == 0 {
			continue
		}
		name, password, mechanism := row.Cells[0].Value, row.Cells[1].Value, row.Cells[2].Value
		name, password, mechanism = strings.TrimSpace(name), strings.TrimSpace(password), strings.TrimSpace(mechanism)

		users = append(users, userFromRow(t, cluster, name, password, mechanism, ""))
	}

	return users
}

func usersFromFullTable(t framework.TestingT, cluster string, table *godog.Table) []*redpandav1alpha2.User {
	var users []*redpandav1alpha2.User

	for i, row := range table.Rows {
		// skip the header row:
		// | name | password | mechanism | acls |
		if i == 0 {
			continue
		}
		name, password, mechanism, acls := row.Cells[0].Value, row.Cells[1].Value, row.Cells[2].Value, row.Cells[3].Value
		name, password, mechanism, acls = strings.TrimSpace(name), strings.TrimSpace(password), strings.TrimSpace(mechanism), strings.TrimSpace(acls)

		users = append(users, userFromRow(t, cluster, name, password, mechanism, acls))
	}

	return users
}

func userFromRow(t framework.TestingT, cluster, name, password, mechanism, acls string) *redpandav1alpha2.User {
	user := &redpandav1alpha2.User{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: t.Namespace(),
			Name:      name,
		},
		Spec: redpandav1alpha2.UserSpec{
			ClusterSource: &redpandav1alpha2.ClusterSource{
				ClusterRef: &redpandav1alpha2.ClusterRef{
					Name: cluster,
				},
			},
		},
	}
	if mechanism != "" || password != "" {
		user.Spec.Authentication = &redpandav1alpha2.UserAuthenticationSpec{
			Type: ptr.To(redpandav1alpha2.SASLMechanism(mechanism)),
			Password: redpandav1alpha2.Password{
				Value: password,
				ValueFrom: &redpandav1alpha2.PasswordSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: name + "-password",
						},
					},
				},
			},
		}
	}
	if acls != "" {
		user.Spec.Authorization = &redpandav1alpha2.UserAuthorizationSpec{}
		require.NoError(t, json.Unmarshal([]byte(acls), &user.Spec.Authorization.ACLs))
	}

	return user
}

func checkStableResource(ctx context.Context, t framework.TestingT, o runtimeclient.Object) {
	var previousResourceVersion string
	var equalityChecks int

	key := runtimeclient.ObjectKeyFromObject(o)

	t.Logf("Ensuring that resource %q is stable", key.String())
	require.Eventually(t, func() bool {
		require.NoError(t, t.Get(ctx, key, o))
		if previousResourceVersion == o.GetResourceVersion() {
			equalityChecks++
		} else {
			equalityChecks = 0
		}
		// ensure we're stable for a 5 second window
		if equalityChecks == 5 {
			return true
		}
		previousResourceVersion = o.GetResourceVersion()
		return false
	}, 30*time.Second, 1*time.Second, "Resource never stabilized")
	t.Logf("Resource %q has been stable for 5 seconds", key.String())
}
