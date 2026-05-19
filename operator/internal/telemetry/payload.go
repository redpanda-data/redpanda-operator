// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package telemetry

// Payload is the operator telemetry document sent to the /kubernetes
// ingestion endpoint.
type Payload struct {
	ID              string `json:"id"`
	OperatorVersion string `json:"operatorVersion"`
	GoVersion       string `json:"goVersion"`
	KubeVersion     string `json:"kubeVersion,omitempty"`
	// IDHash is the enterprise license checksum (hex SHA-256 of the raw license
	// token), the same value Redpanda core reports as id_hash, so licensed
	// clusters correlate to an account. Absent (omitted) for OSS/unlicensed
	// installs, keeping those reports anonymous.
	IDHash string `json:"id_hash,omitempty"`

	NodePools struct {
		Enabled bool `json:"enabled"`
		Count   int  `json:"count"`
	} `json:"nodePools"`

	StretchCluster struct {
		Enabled bool `json:"enabled"`
		Count   int  `json:"count"`
		// Sizing across RedpandaBrokerPool members (stretch/multicluster mode).
		BrokerCount    int      `json:"brokerCount,omitempty"`
		TotalCPUCores  int      `json:"totalCpuCores,omitempty"`
		TotalMemoryGiB int      `json:"totalMemoryGiB,omitempty"`
		BrokerSizes    []string `json:"brokerSizes,omitempty"`
	} `json:"stretchCluster"`

	// Redpanda aggregates the fleet of v2 (chart-based) Redpanda CRs. The
	// per-cluster feature fields are counts of clusters with the feature enabled
	// (not a fleet-wide bool), so one cluster out of many does not flip the
	// signal and we can gauge adoption across the fleet.
	Redpanda struct {
		// Count is the number of Redpanda CRs.
		Count int `json:"count"`
		// BrokerCount is the total desired broker count across the fleet: the sum
		// of each cluster's rendered statefulset.replicas plus every NodePool's
		// spec.replicas. This is the primary scale metric (cluster count alone
		// hides 3-broker vs 30-broker installs).
		BrokerCount int `json:"brokerCount"`
		// TotalCPUCores / TotalMemoryGiB are the aggregate provisioned capacity
		// across the fleet (Σ per-broker × replicas), computed from rendered chart
		// values so installs relying on chart defaults are not undercounted.
		TotalCPUCores  int `json:"totalCpuCores,omitempty"`
		TotalMemoryGiB int `json:"totalMemoryGiB,omitempty"`
		// BrokerSizes are the distinct per-broker sizes in use, e.g. "4c/16Gi" —
		// like Versions, anonymous and useful for fleet segmentation.
		BrokerSizes []string `json:"brokerSizes,omitempty"`
		// Versions are the distinct spec.image.tag values in use (for support and
		// EOL planning).
		Versions []string `json:"versions"`
		// TLS counts clusters with spec.tls.enabled.
		TLS int `json:"tlsEnabled"`
		// SASL counts clusters with spec.auth.sasl.enabled.
		SASL int `json:"saslEnabled"`
		// TieredStorage counts clusters with tiered (cloud) storage enabled.
		TieredStorage int `json:"tieredStorageEnabled"`
		// Console counts clusters using the deprecated inline console
		// (spec.console.enabled).
		Console int `json:"consoleEnabled"`
		// ManagedConnectors counts clusters using the deprecated managed
		// Connectors deployment (spec.connectors.enabled) — this is NOT Redpanda
		// Connect; the field is named accordingly to avoid misleading dashboards.
		ManagedConnectors int `json:"managedConnectorsEnabled"`
		// GatewayAPIExternalAccess counts clusters using Gateway API TLSRoute-based
		// external access (spec.external.gateway.enabled).
		GatewayAPIExternalAccess int `json:"gatewayAPIExternalAccessEnabled"`
	} `json:"redpanda"`

	// VectorizedClusters counts deprecated v1 (vectorized Cluster CR) installs
	// and their aggregate sizing.
	VectorizedClusters struct {
		Count          int      `json:"count"`
		BrokerCount    int      `json:"brokerCount,omitempty"`
		TotalCPUCores  int      `json:"totalCpuCores,omitempty"`
		TotalMemoryGiB int      `json:"totalMemoryGiB,omitempty"`
		BrokerSizes    []string `json:"brokerSizes,omitempty"`
	} `json:"vectorizedClusters"`

	Storage struct {
		CSIDrivers []string `json:"csiDrivers"`
	} `json:"storage"`

	// Resources counts the supporting Redpanda CR types.
	Resources struct {
		Topics      int `json:"topics"`
		Users       int `json:"users"`
		Schemas     int `json:"schemas"`
		Roles       int `json:"roles"`
		ShadowLinks int `json:"shadowLinks"`
		Consoles    int `json:"consoles"`
	} `json:"resources"`

	CRDCount int `json:"crdCount"`
	// Features reports enabled operator-shape flags (controllers, webhook, leader
	// election, cloud-secrets provider, PVC Unbinder, etc.). PVC Unbinder usage
	// is reported here under "pvcUnbinder" rather than as a dedicated field.
	Features map[string]bool `json:"features"`
}
