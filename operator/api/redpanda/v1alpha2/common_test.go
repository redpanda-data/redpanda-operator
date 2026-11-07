// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha2

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/operator/internal/testutils"
)

// TestCRDs runs every test that needs a real apiserver to exercise CRD
// validation and defaulting. They share one control plane: starting an
// apiserver per test cost ~9s each against bodies that take milliseconds.
//
// Each subtest gets its own namespace. envtest has no namespace controller, so
// nothing reclaims them -- uniqueness is the isolation, not deletion.
func TestCRDs(t *testing.T) {
	testEnv := testutils.RedpandaTestEnv{}
	cfg, err := testEnv.StartRedpandaTestEnv(false)
	require.NoError(t, err)

	t.Cleanup(func() { _ = testEnv.Stop() })

	require.NoError(t, AddToScheme(scheme.Scheme))

	c, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	require.NoError(t, err)

	for name, fn := range map[string]func(*testing.T, client.Client, string){
		"brokerpool/validation": testBrokerPoolValidation,
		"license/printcolumns": func(t *testing.T, c client.Client, ns string) {
			testLicensePrintColumns(t, c, cfg, ns)
		},
		"role/defaults":         testRoleDefaults,
		"role/immutable":        testRoleImmutableFields,
		"role/validation":       testRoleValidation,
		"schema/defaults":       testSchemaDefaults,
		"schema/validation":     testSchemaValidation,
		"shadowlink/defaults":   testShadowLinkDefaults,
		"shadowlink/validation": testShadowLinkValidation,
		"topic/validation":      testTopicValidation,
		"user/defaults":         testUserDefaults,
		"user/immutable":        testUserImmutableFields,
		"user/validation":       testUserValidation,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ns := strings.ReplaceAll(name, "/", "-")
			require.NoError(t, c.Create(t.Context(), &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: ns},
			}))

			fn(t, c, ns)
		})
	}
}

// testLicensePrintColumns checks the license columns as the apiserver renders
// them for kubectl get.
func testLicensePrintColumns(t *testing.T, c client.Client, cfg *rest.Config, ns string) {
	ctx := t.Context()

	cfg = rest.CopyConfig(cfg)
	cfg.APIPath = "/apis"
	cfg.GroupVersion = &SchemeGroupVersion
	cfg.NegotiatedSerializer = scheme.Codecs.WithoutConversion()
	rc, err := rest.RESTClientFor(cfg)
	require.NoError(t, err)

	conditions := []metav1.Condition{{
		Type:               "LicenseValid",
		Status:             metav1.ConditionTrue,
		Reason:             "Valid",
		Message:            "Cluster has a valid license",
		LastTransitionTime: metav1.Now(),
	}}
	// A date column would render this future expiry as <invalid>.
	expiration := metav1.NewTime(time.Date(2099, time.January, 2, 0, 0, 0, 0, time.UTC))
	license := &RedpandaLicenseStatus{
		Violation:     true,
		InUseFeatures: []string{"audit_logging", "cluster_cores_over_limit"},
		Type:          ptr.To("enterprise"),
		Expiration:    &expiration,
	}

	rp := &Redpanda{ObjectMeta: metav1.ObjectMeta{Name: "license", Namespace: ns}}
	sc := &StretchCluster{ObjectMeta: metav1.ObjectMeta{Name: "license", Namespace: ns}}
	for _, tc := range []struct {
		resource  string
		obj       client.Object
		setStatus func()
	}{
		{"redpandas", rp, func() { rp.Status.Conditions, rp.Status.LicenseStatus = conditions, license }},
		{"stretchclusters", sc, func() { sc.Status.Conditions, sc.Status.LicenseStatus = conditions, license }},
	} {
		t.Run(tc.resource, func(t *testing.T) {
			require.NoError(t, c.Create(ctx, tc.obj))
			tc.setStatus()
			require.NoError(t, c.Status().Update(ctx, tc.obj))

			raw, err := rc.Get().Namespace(ns).Resource(tc.resource).Name(tc.obj.GetName()).
				SetHeader("Accept", "application/json;as=Table;v=v1;g=meta.k8s.io").
				DoRaw(ctx)
			require.NoError(t, err)
			var table metav1.Table
			require.NoError(t, json.Unmarshal(raw, &table))
			require.Len(t, table.Rows, 1)

			type column struct {
				priority int32
				cell     any
			}
			got := map[string]column{}
			for i, def := range table.ColumnDefinitions {
				got[def.Name] = column{def.Priority, table.Rows[0].Cells[i]}
			}
			for name, want := range map[string]column{
				"License":             {0, "Cluster has a valid license"},
				"License Type":        {1, "enterprise"},
				"Expires":             {1, "2099-01-02T00:00:00Z"},
				"License Violation":   {1, true},
				"Enterprise Features": {1, `["audit_logging","cluster_cores_over_limit"]`},
			} {
				require.Equal(t, want, got[name], name)
			}
		})
	}
}

func TestClusterRefGetNamespace(t *testing.T) {
	cases := []struct {
		name     string
		ref      ClusterRef
		fallback string
		expected string
	}{
		{
			name:     "unset falls back to referencing object namespace",
			ref:      ClusterRef{Name: "redpanda-source"},
			fallback: "b",
			expected: "b",
		},
		{
			name:     "explicit namespace takes precedence over fallback",
			ref:      ClusterRef{Name: "redpanda-source", Namespace: ptr.To("a")},
			fallback: "b",
			expected: "a",
		},
		{
			name:     "explicit namespace equal to fallback",
			ref:      ClusterRef{Name: "redpanda-source", Namespace: ptr.To("b")},
			fallback: "b",
			expected: "b",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, tc.ref.GetNamespace(tc.fallback))
		})
	}
}
