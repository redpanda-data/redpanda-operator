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
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/acls"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/users"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type clusterClients struct {
	cluster        string
	resourceTarget *redpandav1alpha2.User
	factory        *client.Factory
}

func (c *clusterClients) ACLs(ctx context.Context) *acls.Syncer {
	t := framework.T(ctx)

	syncer, err := c.factory.ACLs(ctx, c.resourceTarget)
	require.NoError(t, err)
	return syncer
}

func (c *clusterClients) Users(ctx context.Context) *users.Client {
	t := framework.T(ctx)

	client, err := c.factory.Users(ctx, c.resourceTarget)
	require.NoError(t, err)
	return client
}

func (c *clusterClients) Kafka(ctx context.Context) *kgo.Client {
	t := framework.T(ctx)

	client, err := c.factory.KafkaClient(ctx, c.resourceTarget)
	require.NoError(t, err)
	return client
}

func (c *clusterClients) RedpandaAdmin(ctx context.Context) *rpadmin.AdminAPI {
	t := framework.T(ctx)

	client, err := c.factory.RedpandaAdminClient(ctx, c.resourceTarget)
	require.NoError(t, err)
	return client
}

func (c *clusterClients) WithAuthentication(auth *client.UserAuth) *clusterClients {
	return &clusterClients{
		cluster:        c.cluster,
		resourceTarget: c.resourceTarget,
		factory:        c.factory.WithUserAuth(auth),
	}
}

func (c *clusterClients) AsUser(ctx context.Context, user *redpandav1alpha2.User) *clusterClients {
	t := framework.T(ctx)

	require.NotNil(t, user.Spec.Authentication)

	username := user.Name
	password, err := user.Spec.Authentication.Password.Fetch(ctx, t, user.Namespace)
	require.NoError(t, err)
	mechanism := string(*user.Spec.Authentication.Type)

	return c.WithAuthentication(&client.UserAuth{
		Username:  username,
		Password:  password,
		Mechanism: mechanism,
	})
}

func (c *clusterClients) ExpectUser(ctx context.Context, user string) {
	t := framework.T(ctx)

	t.Logf("Checking for user %q in cluster %q", user, c.cluster)
	c.checkUser(ctx, user, true, fmt.Sprintf("User %q does not exist in cluster %q", user, c.cluster))
	t.Logf("Found user %q in cluster %q", user, c.cluster)
}

func (c *clusterClients) ExpectNoUser(ctx context.Context, user string) {
	t := framework.T(ctx)

	t.Logf("Checking that user %q does not exist in cluster %q", user, c.cluster)
	c.checkUser(ctx, user, false, fmt.Sprintf("User %q still exists in cluster %q", user, c.cluster))
	t.Logf("Found no user %q in cluster %q", user, c.cluster)
}

func (c *clusterClients) ExpectTopic(ctx context.Context, topic string) {
	t := framework.T(ctx)

	t.Logf("Checking that topic %q exists in cluster %q", topic, c.cluster)
	c.checkTopic(ctx, topic, true, fmt.Sprintf("Topic %q does not exist in cluster %q", topic, c.cluster))
	t.Logf("Found topic %q in cluster %q", topic, c.cluster)
}

func (c *clusterClients) ExpectNoTopic(ctx context.Context, topic string) {
	t := framework.T(ctx)

	t.Logf("Checking that topic %q does not exist in cluster %q", topic, c.cluster)
	c.checkTopic(ctx, topic, false, fmt.Sprintf("Topic %q still exists in cluster %q", topic, c.cluster))
	t.Logf("Found no topic %q in cluster %q", topic, c.cluster)
}

func (c *clusterClients) checkTopic(ctx context.Context, topic string, exists bool, message string) {
	t := framework.T(ctx)

	var topics kadm.TopicDetails
	var err error

	if !assert.Eventually(t, func() bool {
		t.Logf("Pulling list of topics from cluster")
		admin := kadm.NewClient(c.Kafka(ctx))
		topics, err = admin.ListTopics(ctx)
		require.NoError(t, err)
		require.NoError(t, topics.Error())

		return exists == topics.Has(topic)
	}, 10*time.Second, 1*time.Second, message) {
		t.Errorf("Final list of topics: %v", topics.Names())
	}
}

func (c *clusterClients) checkUser(ctx context.Context, user string, exists bool, message string) {
	t := framework.T(ctx)

	var users []string
	var err error

	if !assert.Eventually(t, func() bool {
		t.Logf("Pulling list of users from cluster")
		users, err = c.RedpandaAdmin(ctx).ListUsers(ctx)
		require.NoError(t, err)

		return exists == slices.Contains(users, user)
	}, 10*time.Second, 1*time.Second, message) {
		t.Errorf("Final list of users: %v", users)
	}
}

func clientsForCluster(ctx context.Context, cluster string) *clusterClients {
	t := framework.T(ctx)

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

	return &clusterClients{
		resourceTarget: referencer,
		cluster:        cluster,
		factory:        factory,
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
