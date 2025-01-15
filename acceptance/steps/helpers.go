// Copyright 2025 Redpanda Data, Inc.
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
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cucumber/godog"
	"github.com/prometheus/common/expfmt"
	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sr"
	appsv1 "k8s.io/api/apps/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/acls"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/users"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
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

func (c *clusterClients) SchemaRegistry(ctx context.Context) *sr.Client {
	t := framework.T(ctx)

	client, err := c.factory.SchemaRegistryClient(ctx, c.resourceTarget)
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

func (c *clusterClients) ExpectSchema(ctx context.Context, schema string) {
	t := framework.T(ctx)

	t.Logf("Checking that schema %q exists in cluster %q", schema, c.cluster)
	c.checkSchema(ctx, schema, true, fmt.Sprintf("Schema %q does not exist in cluster %q", schema, c.cluster))
	t.Logf("Found schema %q in cluster %q", schema, c.cluster)
}

func (c *clusterClients) ExpectNoSchema(ctx context.Context, schema string) {
	t := framework.T(ctx)

	t.Logf("Checking that schema %q does not exist in cluster %q", schema, c.cluster)
	c.checkSchema(ctx, schema, false, fmt.Sprintf("Schema %q still exists in cluster %q", schema, c.cluster))
	t.Logf("Found no schema %q in cluster %q", schema, c.cluster)
}

func (c *clusterClients) checkSchema(ctx context.Context, schema string, exists bool, message string) {
	t := framework.T(ctx)

	var subjects []string
	var err error

	if !assert.Eventually(t, func() bool {
		t.Logf("Pulling list of schema subjects from cluster")
		schemaRegistry := c.SchemaRegistry(ctx)
		subjects, err = schemaRegistry.Subjects(ctx)
		require.NoError(t, err)

		return exists == slices.Contains(subjects, schema)
	}, 10*time.Second, 1*time.Second, message) {
		t.Errorf("Final list of schema subjects: %v", subjects)
	}
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
		defer admin.Close()

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
		adminClient := c.RedpandaAdmin(ctx)
		defer adminClient.Close()
		users, err = adminClient.ListUsers(ctx)
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

type operatorClients struct {
	client             http.Client
	operatorPodName    string
	namespace          string
	schema             string
	token              string
	expectedStatusCode int
}

func (c *operatorClients) ExpectRequestRejected(ctx context.Context) {
	t := framework.T(ctx)

	url := fmt.Sprintf("%s://%s.%s:8443/metrics", c.schema, c.operatorPodName, c.namespace)

	t.Logf("Request %s to operator", url)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	require.NoError(t, err)

	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.client.Do(req)
	require.NoError(t, err)

	require.Equal(t, c.expectedStatusCode, resp.StatusCode)

	defer resp.Body.Close()
}

func (c *operatorClients) ExpectCorrectMetricsResponse(ctx context.Context) {
	t := framework.T(ctx)

	url := fmt.Sprintf("%s://%s.%s:8443/metrics", c.schema, c.operatorPodName, c.namespace)

	t.Logf("Request %s to operator", url)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	require.NoError(t, err)

	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.client.Do(req)
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, resp.StatusCode)

	defer resp.Body.Close()

	var parser expfmt.TextParser
	_, err = parser.TextToMetricFamilies(resp.Body)
	require.NoError(t, err)
}

func clientsForOperator(ctx context.Context, includeTLS bool, serviceAccountName, expectedStatusCode string) *operatorClients {
	t := framework.T(ctx)

	var dep appsv1.Deployment
	require.NoError(t, t.Get(ctx, t.ResourceKey("redpanda-operator"), &dep))

	var podList corev1.PodList

	require.NoError(t, t.List(ctx, &podList, &runtimeclient.ListOptions{
		LabelSelector: labels.SelectorFromSet(dep.Spec.Selector.MatchLabels),
	}))

	require.Len(t, podList.Items, 1, "expected 1 pod, got %d", len(podList.Items))

	var tlsCfg tls.Config
	schema := "http"
	if includeTLS {
		tlsCfg = tls.Config{InsecureSkipVerify: includeTLS} // nolint:gosec
		schema = "https"
	}

	token := "non-existing-token"
	if serviceAccountName != "" {
		cs, err := kubernetes.NewForConfig(t.RestConfig())
		require.NoError(t, err)
		tokenResponse, err := cs.CoreV1().ServiceAccounts(t.Namespace()).CreateToken(ctx, serviceAccountName, &authenticationv1.TokenRequest{}, metav1.CreateOptions{})
		require.NoError(t, err)
		token = tokenResponse.Status.Token
	}

	statusCode := http.StatusOK
	if expectedStatusCode != "" {
		var err error
		statusCode, err = strconv.Atoi(expectedStatusCode)
		require.NoError(t, err)
	}

	return &operatorClients{
		expectedStatusCode: statusCode,
		token:              token,
		schema:             schema,
		namespace:          t.Namespace(),
		operatorPodName:    podList.Items[0].Name,
		client: http.Client{Transport: &http.Transport{
			TLSClientConfig: &tlsCfg,
			DialContext:     kube.NewPodDialer(t.RestConfig()).DialContext,
		}},
	}
}
