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
	applycorev1 "k8s.io/client-go/applyconfigurations/core/v1"
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
	require.NoError(t, corev1.AddToScheme(scheme))
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
			state, err := NewRenderState(nil, cluster, pools, pools, "test")
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

func TestPerPodServiceOverrides_LocalVsRemote(t *testing.T) {
	localPool := &redpandav1alpha2.NodePool{
		ObjectMeta: metav1.ObjectMeta{Name: "local-pool"},
		Spec: redpandav1alpha2.NodePoolSpec{
			EmbeddedNodePoolSpec: redpandav1alpha2.EmbeddedNodePoolSpec{
				Replicas: ptr.To(int32(1)),
				Services: &redpandav1alpha2.NodePoolServices{
					PerPod: &redpandav1alpha2.PerPodServices{
						Local: &redpandav1alpha2.PerPodServiceOverride{
							Annotations: map[string]string{"scope": "local"},
						},
						Remote: &redpandav1alpha2.PerPodServiceOverride{
							Annotations: map[string]string{"scope": "remote"},
							Spec:        selectorNilOverride(),
						},
					},
				},
			},
		},
	}
	remotePool := &redpandav1alpha2.NodePool{
		ObjectMeta: metav1.ObjectMeta{Name: "remote-pool"},
		Spec: redpandav1alpha2.NodePoolSpec{
			EmbeddedNodePoolSpec: redpandav1alpha2.EmbeddedNodePoolSpec{
				Replicas: ptr.To(int32(1)),
				Services: &redpandav1alpha2.NodePoolServices{
					PerPod: &redpandav1alpha2.PerPodServices{
						Local: &redpandav1alpha2.PerPodServiceOverride{
							Annotations: map[string]string{"scope": "local"},
						},
						Remote: &redpandav1alpha2.PerPodServiceOverride{
							Annotations: map[string]string{"scope": "remote"},
							Spec:        selectorNilOverride(),
						},
					},
				},
			},
		},
	}

	cluster := &redpandav1alpha2.StretchCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
	}

	// localPool is in-cluster, remotePool is not.
	inClusterPools := []*redpandav1alpha2.NodePool{localPool}
	allPools := []*redpandav1alpha2.NodePool{localPool, remotePool}

	state, err := NewRenderState(nil, cluster, inClusterPools, allPools, "test-cluster")
	require.NoError(t, err)

	resources, err := RenderResources(state)
	require.NoError(t, err)

	var localSvc, remoteSvc *corev1.Service
	for _, obj := range resources {
		svc, ok := obj.(*corev1.Service)
		if !ok {
			continue
		}
		switch svc.Name {
		case "local-pool-0":
			localSvc = svc
		case "remote-pool-0":
			remoteSvc = svc
		}
	}

	require.NotNil(t, localSvc, "local-pool-0 service not found")
	require.NotNil(t, remoteSvc, "remote-pool-0 service not found")

	// Local service should have "local" annotation and a selector.
	require.Equal(t, "local", localSvc.Annotations["scope"])
	require.NotEmpty(t, localSvc.Spec.Selector, "local service should have a selector")

	// Remote service should have "remote" annotation and NO selector.
	require.Equal(t, "remote", remoteSvc.Annotations["scope"])
	require.Empty(t, remoteSvc.Spec.Selector, "remote service should have nil selector")
}

func TestPerPodServiceOverrides_RemoteDisabled(t *testing.T) {
	localPool := &redpandav1alpha2.NodePool{
		ObjectMeta: metav1.ObjectMeta{Name: "local-pool"},
		Spec: redpandav1alpha2.NodePoolSpec{
			EmbeddedNodePoolSpec: redpandav1alpha2.EmbeddedNodePoolSpec{
				Replicas: ptr.To(int32(1)),
				Services: &redpandav1alpha2.NodePoolServices{
					PerPod: &redpandav1alpha2.PerPodServices{
						Remote: &redpandav1alpha2.PerPodServiceOverride{
							Enabled: ptr.To(false),
						},
					},
				},
			},
		},
	}
	remotePool := &redpandav1alpha2.NodePool{
		ObjectMeta: metav1.ObjectMeta{Name: "remote-pool"},
		Spec: redpandav1alpha2.NodePoolSpec{
			EmbeddedNodePoolSpec: redpandav1alpha2.EmbeddedNodePoolSpec{
				Replicas: ptr.To(int32(1)),
				Services: &redpandav1alpha2.NodePoolServices{
					PerPod: &redpandav1alpha2.PerPodServices{
						Remote: &redpandav1alpha2.PerPodServiceOverride{
							Enabled: ptr.To(false),
						},
					},
				},
			},
		},
	}

	cluster := &redpandav1alpha2.StretchCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
	}

	inClusterPools := []*redpandav1alpha2.NodePool{localPool}
	allPools := []*redpandav1alpha2.NodePool{localPool, remotePool}

	state, err := NewRenderState(nil, cluster, inClusterPools, allPools, "test-cluster")
	require.NoError(t, err)

	resources, err := RenderResources(state)
	require.NoError(t, err)

	var serviceNames []string
	for _, obj := range resources {
		svc, ok := obj.(*corev1.Service)
		if !ok {
			continue
		}
		serviceNames = append(serviceNames, svc.Name)
	}

	// local-pool-0 should exist (local service, remote disabled doesn't affect local).
	require.Contains(t, serviceNames, "local-pool-0", "local per-pod service should be created")
	// remote-pool-0 should NOT exist (remote is disabled).
	require.NotContains(t, serviceNames, "remote-pool-0", "remote per-pod service should not be created when disabled")
}

func selectorNilOverride() *applycorev1.ServiceSpecApplyConfiguration {
	// Setting Selector to an empty map in apply-configuration signals
	// "I want to own this field" — when merged with MergeTo, the override
	// (empty map) takes precedence, clearing the original selector.
	return &applycorev1.ServiceSpecApplyConfiguration{
		Selector: map[string]string{},
	}
}
