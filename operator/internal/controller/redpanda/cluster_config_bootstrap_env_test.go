// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/redpanda-data/redpanda-operator/operator/api/apiutil"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
)

// stubCluster implements the only two [cluster.Cluster] accessors
// clusterConfigFor uses.
//
// GetConfig returns nil so the chart renders "offline": GoChart.Dot treats a nil
// RESTConfig as empty capabilities without erroring, whereas a bogus non-nil
// config fails apiserver discovery and short-circuits before the code under test.
type stubCluster struct {
	cluster.Cluster
	c client.Client
}

func (s *stubCluster) GetConfig() *rest.Config  { return nil }
func (s *stubCluster) GetClient() client.Client { return s.c }

// TestClusterConfigForPostInstallJobDisabled asserts that cluster configuration
// is derived independently of the optional post-install job.
//
// clusterConfigFor used to read the bootstrap init container's environment off
// redpanda.PostInstallUpgradeJob, which returns nil when
// `post_install_job.enabled=false`. Dereferencing it panicked, and the recovered
// panic left ConfigurationApplied in Error and Quiesced/Stable False forever
// while Ready and Healthy still reported True — cluster configuration was
// silently never applied.
//
// See https://github.com/redpanda-data/redpanda-operator/issues/1021
func TestClusterConfigForPostInstallJobDisabled(t *testing.T) {
	ctx := ctrllog.IntoContext(context.Background(), logr.Discard())

	configFor := func(t *testing.T, spec *redpandav1alpha2.RedpandaClusterSpec) map[string]any {
		t.Helper()

		rp := &redpandav1alpha2.Redpanda{
			ObjectMeta: metav1.ObjectMeta{Name: "redpanda-example", Namespace: "redpanda"},
			Spec:       redpandav1alpha2.RedpandaSpec{ChartRef: redpandav1alpha2.ChartRef{}, ClusterSpec: spec},
		}

		r := &RedpandaReconciler{}
		cl := &stubCluster{c: fake.NewClientBuilder().WithScheme(controller.V2Scheme).Build()}

		config, warnings, err := r.clusterConfigFor(ctx, rp, nil, cl)
		require.NoError(t, err)
		require.Empty(t, warnings)
		require.NotEmpty(t, config, "cluster config must not be empty")
		return config
	}

	// The reference: chart defaults, where the post-install job is enabled.
	baseline := configFor(t, &redpandav1alpha2.RedpandaClusterSpec{})

	for name, spec := range map[string]*redpandav1alpha2.RedpandaClusterSpec{
		// The reporter's configuration.
		"both jobs disabled": {
			PostInstallJob: &redpandav1alpha2.PostInstallJob{Enabled: ptr.To(false)},
			PostUpgradeJob: &redpandav1alpha2.PostUpgradeJob{Enabled: ptr.To(false)},
		},
		"post_install_job disabled": {
			PostInstallJob: &redpandav1alpha2.PostInstallJob{Enabled: ptr.To(false)},
		},
		"post_upgrade_job disabled": {
			PostUpgradeJob: &redpandav1alpha2.PostUpgradeJob{Enabled: ptr.To(false)},
		},
		"post_install_job explicitly enabled": {
			PostInstallJob: &redpandav1alpha2.PostInstallJob{Enabled: ptr.To(true)},
		},
	} {
		t.Run(name, func(t *testing.T) {
			// Whether or not the job is rendered is irrelevant to the cluster
			// config: the operator performs that job's work itself and never
			// creates it.
			require.Equal(t, baseline, configFor(t, spec))
		})
	}
}

// TestClusterConfigForBootstrapEnvIsMirrored asserts that the environment the
// bootstrap init container would have used is still mirrored onto the pod
// context, so `${VAR}`-style references in the cluster config template resolve.
// This is the behavior the (now removed) post-install job lookup existed for,
// and it must survive with the job disabled.
func TestClusterConfigForBootstrapEnvIsMirrored(t *testing.T) {
	ctx := ctrllog.IntoContext(context.Background(), logr.Discard())

	for name, postInstallJob := range map[string]*redpandav1alpha2.PostInstallJob{
		"post_install_job enabled":  {Enabled: ptr.To(true)},
		"post_install_job disabled": {Enabled: ptr.To(false)},
	} {
		t.Run(name, func(t *testing.T) {
			// Tiered storage credentials are injected into the bootstrap
			// container as env vars and referenced from the cluster config
			// template, so they exercise the env mirroring end to end.
			rp := &redpandav1alpha2.Redpanda{
				ObjectMeta: metav1.ObjectMeta{Name: "redpanda-example", Namespace: "redpanda"},
				Spec: redpandav1alpha2.RedpandaSpec{
					ChartRef: redpandav1alpha2.ChartRef{},
					ClusterSpec: &redpandav1alpha2.RedpandaClusterSpec{
						PostInstallJob: postInstallJob,
						Storage: &redpandav1alpha2.Storage{
							Tiered: &redpandav1alpha2.Tiered{
								Config: &redpandav1alpha2.TieredConfig{
									CloudStorageEnabled: &apiutil.JSONBoolean{Raw: []byte("true")},
									CloudStorageBucket:  ptr.To("test-bucket"),
									CloudStorageRegion:  ptr.To("us-east-1"),
								},
							},
						},
					},
				},
			}

			r := &RedpandaReconciler{}
			cl := &stubCluster{c: fake.NewClientBuilder().WithScheme(controller.V2Scheme).Build()}

			config, _, err := r.clusterConfigFor(ctx, rp, nil, cl)
			require.NoError(t, err)

			// A nil ConfigSchema is passed, so values are not coerced to
			// their Redpanda types and stay as the reified strings.
			require.Equal(t, "true", config["cloud_storage_enabled"])
			require.Equal(t, "test-bucket", config["cloud_storage_bucket"])
			require.Equal(t, "us-east-1", config["cloud_storage_region"])
		})
	}
}
