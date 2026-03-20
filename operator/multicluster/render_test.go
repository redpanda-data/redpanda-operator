// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package multicluster

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/txtar"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/yaml"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

func TestRender(t *testing.T) {
	// Pin image tags for deterministic output.
	origRedpandaTag := redpandav1alpha2.DefaultRedpandaImageTag
	origOperatorTag := redpandav1alpha2.DefaultOperatorImageTag
	redpandav1alpha2.DefaultRedpandaImageTag = "v99.9.9"
	redpandav1alpha2.DefaultOperatorImageTag = "v99.9.9"
	t.Cleanup(func() {
		redpandav1alpha2.DefaultRedpandaImageTag = origRedpandaTag
		redpandav1alpha2.DefaultOperatorImageTag = origOperatorTag
	})

	scheme := runtime.NewScheme()
	require.NoError(t, redpandav1alpha2.Install(scheme))
	decoder := serializer.NewCodecFactory(scheme).UniversalDecoder(redpandav1alpha2.SchemeGroupVersion)

	decode := func(t *testing.T, data []byte) (*redpandav1alpha2.StretchCluster, []*redpandav1alpha2.NodePool) {
		t.Helper()
		var cluster *redpandav1alpha2.StretchCluster
		var pools []*redpandav1alpha2.NodePool
		for _, doc := range strings.Split(string(data), "---") {
			doc = strings.TrimSpace(doc)
			if doc == "" {
				continue
			}
			decoded, gvk, err := decoder.Decode([]byte(doc), nil, nil)
			require.NoError(t, err)
			switch o := decoded.(type) {
			case *redpandav1alpha2.StretchCluster:
				require.Nil(t, cluster, "multiple StretchClusters in test case")
				cluster = o
			case *redpandav1alpha2.NodePool:
				pools = append(pools, o)
			default:
				t.Fatalf("unexpected type in test case: %s", gvk)
			}
		}
		if cluster == nil {
			cluster = &redpandav1alpha2.StretchCluster{}
		}
		return cluster, pools
	}

	casesArchive, err := txtar.ParseFile("testdata/render-cases.txtar")
	require.NoError(t, err)

	goldenPools := testutil.NewTxTar(t, "testdata/render-cases.pools.golden.txtar")
	goldenResources := testutil.NewTxTar(t, "testdata/render-cases.resources.golden.txtar")

	for _, file := range casesArchive.Files {
		t.Run(file.Name, func(t *testing.T) {
			t.Parallel()

			cluster, pools := decode(t, file.Data)

			// Use the test case name as the cluster name/namespace.
			cluster.Name = file.Name
			cluster.Namespace = file.Name

			// Construct RenderState with nil config (no K8s client).
			state, err := NewRenderState(nil, cluster, pools, "test")
			require.NoError(t, err)

			// If SASL is enabled, set a deterministic bootstrap user secret
			// to avoid nondeterminism from random password generation.
			if state.Spec().Auth.IsSASLEnabled() {
				sasl := state.Spec().Auth.SASL
				if sasl.BootstrapUser == nil || sasl.BootstrapUser.SecretKeyRef == nil {
					state.bootstrapUserSecret = &corev1.Secret{
						TypeMeta: metav1.TypeMeta{
							APIVersion: "v1",
							Kind:       "Secret",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name:      fmt.Sprintf("%s-bootstrap-user", state.fullname()),
							Namespace: state.namespace,
							Labels:    state.commonLabels(),
						},
						Immutable: ptr.To(true),
						Type:      corev1.SecretTypeOpaque,
						StringData: map[string]string{
							"password": "deterministic-test-password",
						},
					}
				}
			}

			// Render node pools (StatefulSets).
			sets, err := RenderNodePools(state)
			require.NoError(t, err)

			poolBytes, err := yaml.Marshal(sets)
			require.NoError(t, err)
			goldenPools.AssertGolden(t, testutil.YAML, file.Name, poolBytes)

			// Render other resources.
			resources, err := RenderResources(state)
			require.NoError(t, err)

			resourceBytes, err := yaml.Marshal(resources)
			require.NoError(t, err)
			goldenResources.AssertGolden(t, testutil.YAML, file.Name, resourceBytes)
		})
	}
}
