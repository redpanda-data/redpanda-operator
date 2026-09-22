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
	// ClusterLicenses describes the enterprise licenses configured on the
	// managed clusters. It is also how an empty IDHash is told apart from an
	// unlicensed install: len(Checksums)>1 means the fleet holds several
	// licenses and no single one represents it, so IDHash is left empty and the
	// correlation is done from this list instead.
	ClusterLicenses struct {
		// Checksums are the distinct license checksums found across the managed
		// clusters, sorted. Same value Redpanda core reports as id_hash, so a
		// multi-license fleet still correlates to every account it belongs to
		// rather than to none.
		Checksums []string `json:"checksums,omitempty"`
		// Licensed counts clusters carrying a valid, unexpired enterprise
		// license (Redpanda .spec.clusterSpec.enterprise, legacy
		// Cluster .spec.licenseRef). Not derivable from Checksums, which is
		// deduplicated: several clusters commonly share one license.
		Licensed int `json:"licensed"`
	} `json:"clusterLicenses"`

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
		// HostTuners counts StretchClusters with the chroot-based host tuning
		// init container enabled (spec.tuning.apply_host_tuners); see
		// redpanda.hostTunersEnabled.
		HostTuners int `json:"hostTunersEnabled,omitempty"`
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
		// HostTuners counts clusters with the chroot-based host tuning init
		// container enabled (spec.tuning.apply_host_tuners), which lets
		// `rpk redpanda tune all` apply host-level tuners like disk_irq,
		// disk_scheduler, disk_nomerges and net. The long-standing
		// tune_aio_events tuner is not counted here — only the opt-in
		// host-tuner mode.
		HostTuners int `json:"hostTunersEnabled"`
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

	// Broker reports the experimental Broker CR mode. The fields are
	// independent axes, so enabled=false with count>0 is a valid shape
	// (rollback leftovers, or a collector that never runs the Broker
	// controller — the multicluster command).
	Broker struct {
		// Enabled is the operator's --enable-broker flag: configuration,
		// not usage.
		Enabled bool `json:"enabled"`
		// Count is the number of Broker CRs — broker pods managed in broker
		// mode rather than by a StatefulSet. Only controllers create Broker
		// CRs, so their existence is direct evidence of use. Slightly
		// overcounts live brokers during disk-loss recovery: a disk-lost
		// Broker lingers as its node_id's decommission record while the
		// replacement reuses the pod name.
		Count int `json:"count"`
		// Clusters counts the clusters running in broker mode, which Count
		// cannot: 12 Brokers is one 12-broker cluster or twelve 1-broker
		// clusters, and adoption is decided per cluster.
		Clusters BrokerClusterStats `json:"clusters"`
		// Migration counts clusters by the state of their in-place
		// StatefulSet→Broker migration, read from the cluster-scoped
		// BrokerMigration condition.
		Migration BrokerMigrationStats `json:"migration"`
	} `json:"broker"`

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

	// Console reports how Console CRs expose their UI, to track adoption of
	// Gateway API HTTPRoute vs classic Ingress. Counts are over the Console CR
	// fleet (total is resources.consoles).
	Console struct {
		// HTTPRoute counts Console CRs exposing the UI via a Gateway API
		// HTTPRoute (spec.gateway.enabled).
		HTTPRoute int `json:"httpRouteEnabled"`
		// Ingress counts Console CRs exposing the UI via a classic Ingress
		// (spec.ingress.enabled). Reported alongside HTTPRoute for migration
		// context — both may be set at once during a migration.
		Ingress int `json:"ingressEnabled"`
	} `json:"console"`

	CRDCount int `json:"crdCount"`
	// Features reports enabled operator-shape flags (controllers, webhook, leader
	// election, cloud-secrets provider, PVC Unbinder, etc.). PVC Unbinder usage
	// is reported here under "pvcUnbinder" rather than as a dedicated field.
	Features map[string]bool `json:"features"`

	// Connect aggregates the Redpanda Connect pipelines managed by the operator.
	Connect ConnectStats `json:"connect"`
}

// BrokerClusterStats counts the clusters in broker mode. A cluster is a
// distinct controller owner across the Broker CRs, not a distinct
// spec.clusterRef: a NodePool's Brokers name the pool in clusterRef but are
// owned by the Redpanda, so clusterRef would count a pooled cluster once per
// pool. A Broker without a controller owner is in broker.count but in no
// cluster.
type BrokerClusterStats struct {
	Total int `json:"total"`
	// Vectorized and Redpanda split Total by the owner's kind — the deprecated
	// V1 Cluster reconciler vs the go-forward V2 Redpanda one — to show which
	// path adoption is on. An owner of any other kind is in Total only.
	Vectorized int `json:"vectorized"`
	Redpanda   int `json:"redpanda"`
}

// BrokerMigrationStats counts clusters by the reason of their BrokerMigration
// condition (the brokerset.MigrationReason* vocabulary). Blocked and
// InProgress are the pre-GA health signal: a cluster that stays in either is a
// migration that does not converge in a real install, which support tickets do
// not reliably surface. Clusters born in broker mode never get the condition
// and appear nowhere here; a rolled-back cluster has no Broker CRs left, so it
// is in RolledBack but not in BrokerClusterStats.
type BrokerMigrationStats struct {
	// Blocked is a migration the operator refuses to advance: a precondition
	// (cluster health, pending rollout, owner-specific gates) keeps failing.
	Blocked int `json:"blocked"`
	// InProgress is a migration or rollback the operator is advancing but the
	// world is not converging on: a shadow Broker stuck terminating, pods not
	// re-adopted. A healthy transition spans a few reconcile passes while
	// reports are a day apart by default, so a sampled InProgress is almost
	// always a stall.
	InProgress int `json:"inProgress"`
	Complete   int `json:"complete"`
	RolledBack int `json:"rolledBack"`
}

// ConnectStats summarizes the Redpanda Connect (Pipeline) controller's fleet.
type ConnectStats struct {
	// Enabled reports whether any Pipeline CRs exist (Connect is in use in
	// this install). The authoritative "controller is running" signal is the
	// connectController entry in Payload.Features.
	Enabled bool `json:"enabled"`
	// PipelineCount is the number of Pipeline CRs the operator is managing —
	// the primary Connect adoption/scale metric.
	PipelineCount int `json:"pipelineCount"`
	// RunningPipelines counts pipelines whose status phase is Running.
	RunningPipelines int `json:"runningPipelines,omitempty"`
	// PausedPipelines counts pipelines with spec.paused set.
	PausedPipelines int `json:"pausedPipelines,omitempty"`
	// DesiredReplicas is the sum of each pipeline's effective desired replica
	// count (paused pipelines contribute 0) — the number of Connect pods the
	// operator is trying to run fleet-wide.
	DesiredReplicas int `json:"desiredReplicas,omitempty"`
	// ReadyReplicas is the sum of ready Connect pods across all pipelines.
	ReadyReplicas int `json:"readyReplicas,omitempty"`
	// NodeCount is the number of distinct cluster nodes the Connect pods are
	// scheduled across — a spread signal that distinguishes "many pipelines on
	// one node" from "many pipelines fanned out across the fleet". Best-effort:
	// 0 when pods cannot be listed or none are scheduled yet.
	NodeCount int `json:"nodeCount,omitempty"`
	// Versions are the distinct Connect image versions in use across the
	// fleet (for support and EOL planning). Only the tag (or a shortened
	// digest) is reported — never the repository, which can carry internal
	// registry hostnames or team names. Anonymous.
	Versions []string `json:"versions,omitempty"`
}
