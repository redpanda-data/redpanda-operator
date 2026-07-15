// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package pipeline

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// These are self-contained render unit tests ported from
// redpanda-operator#1337. They exercise the render path that was brought over
// (ConfigMap + Deployment) without the envtest harness, license plumbing, or
// clusterRef/userRef resolution (all stubbed in the enterprise port). The full
// OSS suite (TestReconcile_*, golden render tests) is deferred until the v2 API
// surface (Redpanda/User types) and an envtest harness are extracted here.

func newRender(p *redpandav1alpha2.Pipeline) *render {
	return &render{pipeline: p, labels: Labels(p)}
}

// TestRender_ExtraInitContainers verifies user-supplied init containers are
// injected, in order, ahead of the built-in lint container.
func TestRender_ExtraInitContainers(t *testing.T) {
	pipeline := &redpandav1alpha2.Pipeline{
		ObjectMeta: metav1.ObjectMeta{Name: "init-test", Namespace: "default"},
		Spec: redpandav1alpha2.PipelineSpec{
			ConfigYAML: "input:\n  stdin: {}\noutput:\n  stdout: {}\n",
			ExtraInitContainers: []corev1.Container{
				{Name: "fetch-certs", Image: "curlimages/curl:8.11.0"},
				{Name: "warm-cache", Image: "busybox:1.36"},
			},
		},
	}

	objs, err := newRender(pipeline).Render(context.Background())
	require.NoError(t, err)

	var dp *appsv1.Deployment
	for _, o := range objs {
		if d, ok := o.(*appsv1.Deployment); ok {
			dp = d
		}
	}
	require.NotNil(t, dp, "expected a Deployment")

	init := dp.Spec.Template.Spec.InitContainers
	require.Len(t, init, 3, "two extra init containers plus the built-in lint container")
	// User containers come first, in declared order; lint runs last so anything
	// they staged is visible to it.
	assert.Equal(t, "fetch-certs", init[0].Name)
	assert.Equal(t, "warm-cache", init[1].Name)
	assert.Equal(t, "lint", init[2].Name)
}

// TestRender_TopLevelRedpandaSharedClient covers fix #1: the operator emits a
// top-level redpanda: shared-client block (seed_brokers + sasl) so
// redpanda_common works without inline credentials.
func TestRender_TopLevelRedpandaSharedClient(t *testing.T) {
	p := &redpandav1alpha2.Pipeline{
		ObjectMeta: metav1.ObjectMeta{Name: "cdc", Namespace: "redpanda"},
		Spec: redpandav1alpha2.PipelineSpec{
			ConfigYAML: "input:\n  generate:\n    mapping: 'root = {}'\noutput:\n  redpanda_common:\n    topic: t\n",
		},
	}
	r := &render{
		pipeline:        p,
		labels:          Labels(p),
		clusterConn:     &clusterConnection{Brokers: []string{"redpanda-0.redpanda:9093"}},
		userCredentials: &userCredentials{Mechanism: "SCRAM-SHA-512"},
	}
	yamlStr, err := r.renderConnectYAML()
	require.NoError(t, err)

	var cfg map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(yamlStr), &cfg))
	rp, ok := cfg["redpanda"].(map[string]any)
	require.True(t, ok, "expected a top-level redpanda: block, got:\n%s", yamlStr)
	assert.Equal(t, []any{"redpanda-0.redpanda:9093"}, rp["seed_brokers"])
	require.NotNil(t, rp["sasl"], "expected sasl in the shared client block")
}

// TestRender_StaticConfiguration exercises the staticConfiguration path
// end-to-end through the renderer: brokers, TLS (root_cas_file derived from
// the actual CA key), and env-var-backed SASL all land in the generated
// shared-client block, and the pod projects the matching env vars + CA mount.
func TestRender_StaticConfiguration(t *testing.T) {
	p := &redpandav1alpha2.Pipeline{
		ObjectMeta: metav1.ObjectMeta{Name: "static", Namespace: "default"},
		Spec: redpandav1alpha2.PipelineSpec{
			ConfigYAML: "input:\n  generate:\n    mapping: 'root = {}'\noutput:\n  redpanda_common:\n    topic: t\n",
		},
	}
	conn, err := staticClusterConnection(&redpandav1alpha2.StaticConfigurationSource{
		Kafka: &redpandav1alpha2.KafkaAPISpec{
			Brokers: []string{"broker-0:9093"},
			TLS: &redpandav1alpha2.CommonTLS{
				CaCert: &redpandav1alpha2.ValueSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "ca"},
						Key:                  "corp-ca.pem",
					},
				},
			},
			SASL: &redpandav1alpha2.KafkaSASL{
				Username:  "svc",
				Mechanism: redpandav1alpha2.SASLMechanismScramSHA512,
				Password: &redpandav1alpha2.ValueSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "creds"},
						Key:                  "password",
					},
				},
			},
		},
	})
	require.NoError(t, err)

	r := &render{pipeline: p, labels: Labels(p), clusterConn: conn}

	yamlStr, err := r.renderConnectYAML()
	require.NoError(t, err)
	var cfg map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(yamlStr), &cfg))
	rp, ok := cfg["redpanda"].(map[string]any)
	require.True(t, ok, "expected a top-level redpanda: block, got:\n%s", yamlStr)
	assert.Equal(t, []any{"broker-0:9093"}, rp["seed_brokers"])
	tls, ok := rp["tls"].(map[string]any)
	require.True(t, ok, "expected tls in the shared client block")
	assert.Equal(t, true, tls["enabled"])
	assert.Equal(t, "/etc/tls/certs/ca/corp-ca.pem", tls["root_cas_file"],
		"root_cas_file must use the CA's actual key, not a hardcoded ca.crt")
	sasl, ok := rp["sasl"].([]any)
	require.True(t, ok, "expected sasl in the shared client block without a userRef")
	assert.Equal(t, "SCRAM-SHA-512", sasl[0].(map[string]any)["mechanism"])
	assert.Equal(t, "${REDPANDA_SASL_USERNAME}", sasl[0].(map[string]any)["username"])

	// The pod env must back the ${REDPANDA_SASL_*} references and the CA mount
	// must project the same key the rendered path points at.
	dp := r.deployment()
	env := map[string]corev1.EnvVar{}
	for _, e := range dp.Spec.Template.Spec.Containers[0].Env {
		env[e.Name] = e
	}
	assert.Equal(t, "svc", env["REDPANDA_SASL_USERNAME"].Value)
	assert.Equal(t, "SCRAM-SHA-512", env["REDPANDA_SASL_MECHANISM"].Value)
	require.NotNil(t, env["REDPANDA_SASL_PASSWORD"].ValueFrom)
	assert.Equal(t, "creds", env["REDPANDA_SASL_PASSWORD"].ValueFrom.SecretKeyRef.Name)

	var caVolume *corev1.Volume
	for i := range dp.Spec.Template.Spec.Volumes {
		if dp.Spec.Template.Spec.Volumes[i].Name == clusterTLSVolumeName {
			caVolume = &dp.Spec.Template.Spec.Volumes[i]
		}
	}
	require.NotNil(t, caVolume, "expected a CA volume")
	require.NotNil(t, caVolume.Projected.Sources[0].Secret)
	assert.Equal(t, "corp-ca.pem", caVolume.Projected.Sources[0].Secret.Items[0].Key)
}

// TestRender_ClusterRefSASLFallback verifies a clusterRef pipeline without a
// userRef falls back to the cluster's resolved (bootstrap) SASL credentials in
// the generated sasl block — previously the block was only emitted for
// userRef, so shared-client auth failed on SASL clusters without one.
func TestRender_ClusterRefSASLFallback(t *testing.T) {
	p := &redpandav1alpha2.Pipeline{
		ObjectMeta: metav1.ObjectMeta{Name: "noref", Namespace: "default"},
		Spec: redpandav1alpha2.PipelineSpec{
			ConfigYAML: "input:\n  generate:\n    mapping: 'root = {}'\noutput:\n  redpanda_common:\n    topic: t\n",
		},
	}
	r := &render{
		pipeline: p,
		labels:   Labels(p),
		clusterConn: &clusterConnection{
			Brokers: []string{"rp-0:9093"},
			SASL: &clusterSASL{
				Mechanism: "SCRAM-SHA-256",
				Username:  "kubernetes-controller",
				PasswordRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "rp-superuser"},
					Key:                  "password",
				},
			},
		},
	}

	yamlStr, err := r.renderConnectYAML()
	require.NoError(t, err)
	var cfg map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(yamlStr), &cfg))
	rp := cfg["redpanda"].(map[string]any)
	sasl, ok := rp["sasl"].([]any)
	require.True(t, ok, "expected sasl fallback from the cluster source")
	assert.Equal(t, "SCRAM-SHA-256", sasl[0].(map[string]any)["mechanism"])

	dp := r.deployment()
	env := map[string]corev1.EnvVar{}
	for _, e := range dp.Spec.Template.Spec.Containers[0].Env {
		env[e.Name] = e
	}
	assert.Equal(t, "kubernetes-controller", env["REDPANDA_SASL_USERNAME"].Value)
	require.NotNil(t, env["REDPANDA_SASL_PASSWORD"].ValueFrom)
	assert.Equal(t, "rp-superuser", env["REDPANDA_SASL_PASSWORD"].ValueFrom.SecretKeyRef.Name)
}

// TestRender_ConfigChecksumAnnotation covers fix #2: the Deployment pod template
// carries a config checksum that changes with the rendered config, so a
// configYaml change rolls the Deployment.
func TestRender_ConfigChecksumAnnotation(t *testing.T) {
	mk := func(cfg string) *appsv1.Deployment {
		p := &redpandav1alpha2.Pipeline{
			ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
			Spec:       redpandav1alpha2.PipelineSpec{ConfigYAML: cfg},
		}
		objs, err := newRender(p).Render(context.Background())
		require.NoError(t, err)
		for _, o := range objs {
			if dp, ok := o.(*appsv1.Deployment); ok {
				return dp
			}
		}
		t.Fatal("no Deployment")
		return nil
	}
	a := mk("input:\n  generate:\n    mapping: 'root.v = 1'\noutput:\n  stdout: {}\n")
	b := mk("input:\n  generate:\n    mapping: 'root.v = 2'\noutput:\n  stdout: {}\n")
	ka := "cluster.redpanda.com/config-checksum"
	assert.NotEmpty(t, a.Spec.Template.Annotations[ka], "checksum annotation present")
	assert.NotEqual(t, a.Spec.Template.Annotations[ka], b.Spec.Template.Annotations[ka],
		"checksum must change when configYaml changes")
}

// TestBuildAffinity_NodePool covers node-pool scheduling: a user-supplied
// affinity (e.g. pinning a node pool) is passed through, and when Zones is also
// set the zone requirement is AND-ed into each node-affinity term so node-pool
// and zone constraints both apply.
func TestBuildAffinity_NodePool(t *testing.T) {
	poolAffinity := func() *corev1.Affinity {
		return &corev1.Affinity{
			NodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{{
						MatchExpressions: []corev1.NodeSelectorRequirement{{
							Key:      "redpanda.com/node-pool",
							Operator: corev1.NodeSelectorOpIn,
							Values:   []string{"connect"},
						}},
					}},
				},
			},
		}
	}

	t.Run("nil when neither affinity nor zones", func(t *testing.T) {
		assert.Nil(t, buildAffinity(&redpandav1alpha2.Pipeline{}))
	})

	t.Run("user affinity passed through verbatim when no zones", func(t *testing.T) {
		p := &redpandav1alpha2.Pipeline{Spec: redpandav1alpha2.PipelineSpec{Affinity: poolAffinity()}}
		got := buildAffinity(p)
		require.NotNil(t, got.NodeAffinity)
		terms := got.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
		require.Len(t, terms, 1)
		require.Len(t, terms[0].MatchExpressions, 1)
		assert.Equal(t, "redpanda.com/node-pool", terms[0].MatchExpressions[0].Key)
	})

	t.Run("zones only generates zone node-affinity (unchanged behavior)", func(t *testing.T) {
		p := &redpandav1alpha2.Pipeline{Spec: redpandav1alpha2.PipelineSpec{Zones: []string{"us-east-2a", "us-east-2b"}}}
		got := buildAffinity(p)
		terms := got.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
		require.Len(t, terms, 1)
		require.Len(t, terms[0].MatchExpressions, 1)
		assert.Equal(t, zoneTopologyKey, terms[0].MatchExpressions[0].Key)
		assert.Equal(t, []string{"us-east-2a", "us-east-2b"}, terms[0].MatchExpressions[0].Values)
	})

	t.Run("node-pool affinity AND zones: zone req added to the pool term", func(t *testing.T) {
		p := &redpandav1alpha2.Pipeline{Spec: redpandav1alpha2.PipelineSpec{
			Affinity: poolAffinity(),
			Zones:    []string{"us-east-2a"},
		}}
		got := buildAffinity(p)
		terms := got.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
		require.Len(t, terms, 1)
		// the single term now AND-s the node-pool match and the zone match
		keys := []string{}
		for _, e := range terms[0].MatchExpressions {
			keys = append(keys, e.Key)
		}
		assert.ElementsMatch(t, []string{"redpanda.com/node-pool", zoneTopologyKey}, keys)
	})

	t.Run("does not mutate the input pipeline's affinity", func(t *testing.T) {
		p := &redpandav1alpha2.Pipeline{Spec: redpandav1alpha2.PipelineSpec{
			Affinity: poolAffinity(),
			Zones:    []string{"us-east-2a"},
		}}
		_ = buildAffinity(p)
		// the original spec.affinity must still have exactly its one expression
		orig := p.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions
		assert.Len(t, orig, 1, "buildAffinity must deep-copy, not mutate spec.affinity")
	})
}

// TestClusterConnCache covers the per-cluster render cache: a hit only when the
// generation matches, invalidation when the Redpanda spec generation bumps,
// independent entries per cluster, at-most-one entry per cluster, and nil-safety.
func TestClusterConnCache(t *testing.T) {
	c := newClusterConnCache()
	connA1 := &clusterConnection{Brokers: []string{"a:9093"}}
	connA2 := &clusterConnection{Brokers: []string{"a2:9093"}}
	connB := &clusterConnection{Brokers: []string{"b:9093"}}

	// miss on empty cache
	_, ok := c.get("ns", "rp-a", 1)
	assert.False(t, ok, "empty cache must miss")

	// put gen1, hit on gen1, miss on a different generation
	c.put("ns", "rp-a", 1, connA1)
	got, ok := c.get("ns", "rp-a", 1)
	assert.True(t, ok)
	assert.Same(t, connA1, got)
	_, ok = c.get("ns", "rp-a", 2)
	assert.False(t, ok, "generation bump must invalidate (forces a re-render)")

	// put gen2 replaces the stale gen1 entry (at most one entry per cluster)
	c.put("ns", "rp-a", 2, connA2)
	got, ok = c.get("ns", "rp-a", 2)
	assert.True(t, ok)
	assert.Same(t, connA2, got)
	assert.Len(t, c.entries, 1, "a cluster keeps a single entry across generations")

	// distinct clusters are independent
	c.put("ns", "rp-b", 1, connB)
	got, ok = c.get("ns", "rp-b", 1)
	assert.True(t, ok)
	assert.Same(t, connB, got)
	got, ok = c.get("ns", "rp-a", 2)
	assert.True(t, ok, "rp-a entry unaffected by rp-b")
	assert.Same(t, connA2, got)

	// same name, different namespace is a different key
	_, ok = c.get("other-ns", "rp-a", 2)
	assert.False(t, ok)

	// nil cache is safe (no panic; always misses)
	var nilCache *clusterConnCache
	_, ok = nilCache.get("ns", "rp-a", 1)
	assert.False(t, ok)
	nilCache.put("ns", "rp-a", 1, connA1) // must not panic
}
