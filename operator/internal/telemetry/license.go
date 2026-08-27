// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package telemetry

import (
	"context"

	"github.com/redpanda-data/common-go/license"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
)

// LicenseChecksum returns the hex SHA-256 checksum of a parsed enterprise
// license — the same value Redpanda core reports as id_hash. The
// license.RedpandaLicense interface doesn't expose the checksum, so we type
// switch to the concrete versions. Returns "" for an unrecognized type.
//
// Callers must gate on license.AllowsEnterpriseFeatures() first: the checksum
// is only emitted for licensed clusters so OSS/unlicensed reports stay
// anonymous.
func LicenseChecksum(l license.RedpandaLicense) string {
	switch v := l.(type) {
	case *license.V1RedpandaLicense:
		return v.Checksum
	case *license.V0RedpandaLicense:
		return v.Checksum
	default:
		return ""
	}
}

// clusterLicenses resolves the enterprise licenses configured on the managed
// clusters and returns the number of licensed clusters keyed by license
// checksum.
//
// This is what makes a licensed install identifiable at all in the common case.
// The operator's own --license-file-path (operator chart
// enterprise.licenseSecretRef) is set only by deployers who need the Connect
// controller or multicluster; a cluster license lives on the CR
// (Redpanda .spec.clusterSpec.enterprise, legacy Cluster .spec.licenseRef), and
// before this the collector never looked at it — so every licensed install
// reported an empty id_hash.
//
// Unresolvable licenses are skipped, never fatal: a secret that is missing,
// unreadable (Forbidden) or holds something unparseable is a property of one
// cluster's configuration, and dropping the whole telemetry document over it
// would be strictly worse than reporting the rest. Non-enterprise and expired
// licenses are skipped too, matching LicenseChecksum's contract.
func (c *Collector) clusterLicenses(ctx context.Context, redpandas []redpandav1alpha2.Redpanda, legacy []vectorizedv1alpha1.Cluster) map[string]int {
	counts := map[string]int{}

	parse := c.ParseLicense
	if parse == nil {
		parse = license.ParseLicense
	}

	record := func(raw []byte) {
		parsed, err := parse(raw)
		if err != nil || !parsed.AllowsEnterpriseFeatures() {
			return
		}
		if sum := LicenseChecksum(parsed); sum != "" {
			counts[sum]++
		}
	}

	for i := range redpandas {
		rp := &redpandas[i]
		if rp.Spec.ClusterSpec == nil || rp.Spec.ClusterSpec.Enterprise == nil {
			continue
		}
		enterprise := rp.Spec.ClusterSpec.Enterprise
		if inline := ptr.Deref(enterprise.License, ""); inline != "" {
			record([]byte(inline))
			continue
		}
		if ref := enterprise.LicenseSecretRef; ref != nil {
			// No default key here: the chart hands this SecretKeySelector
			// straight to the kubelet, so an unset key is a broken cluster
			// rather than a shape we should guess at.
			if raw, ok := c.secretValue(ctx, rp.Namespace, ptr.Deref(ref.Name, ""), ptr.Deref(ref.Key, "")); ok {
				record(raw)
			}
		}
	}

	for i := range legacy {
		ref := legacy[i].Spec.LicenseRef
		if ref == nil {
			continue
		}
		// SecretKeyRef documents "license" as the key when none is given.
		key := ref.Key
		if key == "" {
			key = "license"
		}
		if raw, ok := c.secretValue(ctx, ref.Namespace, ref.Name, key); ok {
			record(raw)
		}
	}

	return counts
}

// secretValue reads one key out of one Secret, returning ok=false for every
// reason a telemetry cycle should shrug at rather than fail on.
func (c *Collector) secretValue(ctx context.Context, namespace, name, key string) ([]byte, bool) {
	if namespace == "" || name == "" || key == "" {
		return nil, false
	}

	var secret corev1.Secret
	if err := c.Reader.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &secret); err != nil {
		return nil, false
	}

	raw, ok := secret.Data[key]
	if !ok || len(raw) == 0 {
		return nil, false
	}
	return raw, true
}
