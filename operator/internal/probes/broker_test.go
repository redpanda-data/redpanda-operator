// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package probes_test

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/suite"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/probes"
	"github.com/redpanda-data/redpanda-operator/operator/internal/testenv"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	"github.com/redpanda-data/redpanda-operator/pkg/helm"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

func TestIntegrationProber(t *testing.T) {
	testutil.SkipIfNotIntegration(t)

	suite.Run(t, new(ProberSuite))
}

type ProberSuite struct {
	suite.Suite

	manager       ctrl.Manager
	ctx           context.Context
	env           *testenv.Env
	client        client.Client
	helm          *helm.Client
	clientFactory *internalclient.Factory
}

var _ suite.SetupAllSuite = (*ProberSuite)(nil)

func (s *ProberSuite) TestProbes() {
	s.T().Skip("Redpanda now does not allow decommissioning that mimic under-replicated partition")
	chart := s.installChart("default", "")
	adminClient := s.adminClientFor(chart)
	defer adminClient.Close()

	brokerURL := fmt.Sprintf("https://default-0.default.%s.svc.cluster.local.:9644", s.env.Namespace())
	// we wrap the first check in a waitFor since there's no guarantee that
	// the broker will be ready when the helm install completes
	prober := chart.prober
	s.waitFor(func(ctx context.Context) (bool, error) {
		healthy, err := prober.IsClusterBrokerHealthy(s.ctx, brokerURL)
		if err != nil {
			s.T().Logf("error checking broker health, retrying: %v", err)
			return false, nil
		}
		return healthy, nil
	})

	ready, err := prober.IsClusterBrokerReady(s.ctx, brokerURL)
	s.Require().NoError(err)
	s.Require().True(ready)

	// now create an under-replicated partition, decommissioning a broker in the process
	kafkaClient := s.kafkaClientFor(chart)
	kafkaAdminClient := kadm.NewClient(kafkaClient)
	defer kafkaAdminClient.Close()

	s.waitFor(func(ctx context.Context) (bool, error) {
		s.T().Log("attempting to create test-topic")
		topicResponse, err := kafkaAdminClient.CreateTopic(s.ctx, 1, 3, nil, "test-topic")
		if err != nil || topicResponse.Err != nil {
			s.T().Logf("error creating test topic, retrying: %v", errors.Join(err, topicResponse.Err))
			return false, nil
		}
		return true, nil
	})

	// decommission a broker to make a topic under-replicated
	err = adminClient.DecommissionBroker(s.ctx, 1)
	s.Require().NoError(err)

	s.waitFor(func(ctx context.Context) (bool, error) {
		healthy, err := prober.IsClusterBrokerHealthy(s.ctx, brokerURL)
		if err != nil {
			s.T().Logf("error checking broker health, retrying: %v", err)
			return false, nil
		}
		s.T().Logf("checking that broker is no longer healthy due to under-replicated partitions, healthy: %v", healthy)
		return healthy == false, nil
	})

	// this should still be true since we don't care about under-replicated partitions here
	ready, err = prober.IsClusterBrokerReady(s.ctx, brokerURL)
	s.Require().NoError(err)
	s.Require().True(ready)

	// now decommission broker 0 to ensure it now fails readiness checks too
	s.waitFor(func(ctx context.Context) (bool, error) {
		s.T().Log("attempting to decommission broker 0")
		err = adminClient.DecommissionBroker(s.ctx, 0)
		if err != nil {
			s.T().Logf("error decommissioning broker, retrying: %v", err)
			return false, nil
		}
		return true, nil
	})

	s.waitFor(func(ctx context.Context) (bool, error) {
		ready, err := prober.IsClusterBrokerReady(s.ctx, brokerURL)
		if err != nil {
			s.T().Logf("error checking broker readiness, retrying: %v", err)
			return false, nil
		}
		s.T().Logf("checking that broker is no longer ready after decommission, ready: %v", ready)
		return ready == false, nil
	})

	s.cleanupChart(chart)
}

func (s *ProberSuite) SetupSuite() {
	t := s.T()

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	log := testr.NewWithOptions(t, testr.Options{
		Verbosity: 10,
	})

	s.ctx = context.Background()
	s.env = testenv.New(t, testenv.Options{
		Name:   "probes",
		Agents: 3,
		Scheme: scheme,
		Logger: log,
	})

	s.client = s.env.Client()

	s.env.SetupManager(s.setupRBAC(), func(mgr ctrl.Manager) error {
		helmClient, err := helm.New(helm.Options{
			KubeConfig: mgr.GetConfig(),
		})
		if err != nil {
			return err
		}
		if err := helmClient.RepoAdd(s.ctx, "redpandadata", "https://charts.redpanda.com"); err != nil {
			return err
		}

		s.manager = mgr
		s.helm = helmClient
		dialer := kube.NewPodDialer(mgr.GetConfig())
		s.clientFactory = internalclient.NewFactory(mgr.GetConfig(), mgr.GetClient()).WithDialer(dialer.DialContext)

		return nil
	})
}

type chart struct {
	name    string
	version string
	release helm.Release
	values  map[string]any
	prober  *probes.Prober
}

func (s *ProberSuite) installChart(name, version string) *chart {
	values := map[string]any{
		"statefulset": map[string]any{
			"replicas": 3,
		},
		"console": map[string]any{
			"enabled": false,
		},
		"external": map[string]any{
			"enabled": false,
		},
		"image": map[string]any{
			"repository": "redpandadata/redpanda",
			"tag":        "v24.3.1",
		},
	}

	release, err := s.helm.Install(s.ctx, "redpandadata/redpanda", helm.InstallOptions{
		Version:         version,
		CreateNamespace: true,
		Name:            name,
		Namespace:       s.env.Namespace(),
		Values:          values,
	})
	s.Require().NoError(err)

	var configMap corev1.ConfigMap
	err = s.client.Get(s.ctx, types.NamespacedName{Namespace: s.env.Namespace(), Name: name}, &configMap)
	s.Require().NoError(err)

	data, ok := configMap.Data["redpanda.yaml"]
	s.Require().True(ok, "redpanda.yaml not found in config map")

	fs := afero.NewMemMapFs()
	err = afero.WriteFile(fs, "/redpanda.yaml", []byte(data), 0o644)
	s.Require().NoError(err)

	var secret corev1.Secret
	err = s.client.Get(s.ctx, types.NamespacedName{Namespace: s.env.Namespace(), Name: fmt.Sprintf("%s-default-cert", name)}, &secret)
	s.Require().NoError(err)

	cert, ok := secret.Data["ca.crt"]
	s.Require().True(ok, "ca.crt not found in secret")

	err = afero.WriteFile(fs, fmt.Sprintf("/etc/tls/certs/%s/ca.crt", name), []byte(cert), 0o644)
	s.Require().NoError(err)

	prober := probes.NewProber(s.clientFactory.WithFS(fs), "/redpanda.yaml", probes.WithFS(fs), probes.WithLogger(s.manager.GetLogger()))

	return &chart{
		name:    name,
		version: version,
		values:  values,
		release: release,
		prober:  prober,
	}
}

func (s *ProberSuite) cleanupChart(chart *chart) {
	s.Require().NoError(s.helm.Uninstall(s.ctx, chart.release))
}

func (s *ProberSuite) adminClientFor(chart *chart) *rpadmin.AdminAPI {
	adminClient, err := s.clientFactory.RedpandaAdminClient(s.ctx, s.clusterFor(chart))
	s.Require().NoError(err)

	return adminClient
}

func (s *ProberSuite) kafkaClientFor(chart *chart) *kgo.Client {
	kafkaClient, err := s.clientFactory.KafkaClient(s.ctx, s.clusterFor(chart))
	s.Require().NoError(err)

	return kafkaClient
}

func (s *ProberSuite) clusterFor(chart *chart) *redpandav1alpha2.Redpanda {
	data, err := json.Marshal(chart.values)
	s.Require().NoError(err)

	cluster := &redpandav1alpha2.Redpanda{
		ObjectMeta: metav1.ObjectMeta{
			Name:      chart.name,
			Namespace: s.env.Namespace(),
		},
		Spec: redpandav1alpha2.RedpandaSpec{ClusterSpec: &redpandav1alpha2.RedpandaClusterSpec{}},
	}

	err = json.Unmarshal(data, cluster)
	s.Require().NoError(err)

	return cluster
}

func (s *ProberSuite) setupRBAC() string {
	objs := []client.Object{
		&rbacv1.Role{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Role",
				APIVersion: "rbac.authorization.k8s.io/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-role",
				Namespace: s.env.Namespace(),
			},
			Rules: []rbacv1.PolicyRule{{
				APIGroups: []string{""},
				Resources: []string{"pods/portforward"},
				Verbs:     []string{"*"},
			}},
		},
		&rbacv1.RoleBinding{
			TypeMeta: metav1.TypeMeta{
				Kind:       "RoleBinding",
				APIVersion: "rbac.authorization.k8s.io/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-role",
				Namespace: s.env.Namespace(),
			},
			Subjects: []rbacv1.Subject{
				{Kind: "ServiceAccount", Namespace: s.env.Namespace(), Name: "user"},
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "Role",
				Name:     "test-role",
			},
		},
	}

	for _, obj := range objs {
		s.Require().NoError(s.client.Patch(s.ctx, obj, client.Apply, client.ForceOwnership, client.FieldOwner("tests")))
	}

	return "user"
}

func (s *ProberSuite) waitFor(cond func(ctx context.Context) (bool, error)) {
	s.NoError(wait.PollUntilContextTimeout(s.ctx, 5*time.Second, 5*time.Minute, false, cond))
}
