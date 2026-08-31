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

	// Broker reports the experimental Broker CR mode, in which the operator
	// manages one Broker CR (and its pod/PVC) per broker instead of a
	// StatefulSet. The two fields are independent axes — enabled says the
	// operator *can* run broker mode, count says whether it actually does:
	//
	//	enabled=false            -> the mode is unavailable in this install
	//	enabled=true,  count=0   -> available but unused
	//	enabled=true,  count>0   -> in use, across count brokers
	//	enabled=false, count>0   -> in use but the controller was turned off
	//	                            (a rollback, or the flag dropped from a
	//	                            running install)
	Broker struct {
		// Enabled is the operator's --enable-broker flag: whether the Broker
		// controller is running at all. Configuration, not usage — it says
		// nothing about whether any cluster is managed this way.
		Enabled bool `json:"enabled"`
		// Count is the number of Broker CRs, i.e. the number of broker pods
		// managed in broker mode rather than by a StatefulSet. This is the
		// usage signal: Broker CRs are created by the Redpanda and Cluster
		// controllers, never by hand, so their existence is direct evidence the
		// mode is in use. No opt-in signal is read (the use-broker-cr
		// annotation that selects the mode today goes away once broker mode is
		// the default path).
		Count int `json:"count"`
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
