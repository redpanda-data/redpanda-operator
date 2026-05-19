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
	"maps"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	applycorev1 "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/operator/pkg/functional"
)

// RedpandaBrokerPool is the per-K8s-cluster pool type used by StretchCluster.
// It is the multicluster successor to NodePool: each pool carries the
// per-K8s-cluster configuration (TLS, listeners, ClusterDomain, external
// access, RBAC, ServiceAccount, monitoring) that previously lived on the
// federated StretchClusterSpec.
//
// NodePool stays in place for the single-cluster Redpanda CR path; the
// two types intentionally do not share an embedded spec so that future
// schema changes to one don't ripple into the other.
//
// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=redpandabrokerpools
// +kubebuilder:printcolumn:name="Bound",type="string",JSONPath=".status.conditions[?(@.type==\"Bound\")].status",description=""
// +kubebuilder:printcolumn:name="Deployed",type="string",JSONPath=".status.conditions[?(@.type==\"Deployed\")].status",description=""
type RedpandaBrokerPool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec RedpandaBrokerPoolSpec `json:"spec,omitempty"`
	// +kubebuilder:default={conditions: {{type: "Bound", status: "Unknown", reason: "NotReconciled", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}, {type: "Deployed", status: "Unknown", reason: "NotReconciled", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}, {type: "Quiesced", status: "Unknown", reason: "NotReconciled", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}, {type: "Stable", status: "Unknown", reason: "NotReconciled", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}}
	Status RedpandaBrokerPoolStatus `json:"status,omitempty"`
}

func (n *RedpandaBrokerPool) GetClusterSource() *ClusterSource {
	return &ClusterSource{
		ClusterRef: &n.Spec.ClusterRef,
	}
}

// RedpandaBrokerPoolStatus defines the observed state of a broker pool.
type RedpandaBrokerPoolStatus struct {
	EmbeddedBrokerPoolStatus `json:",inline"`
	// DeployedGeneration represents the generation of the RedpandaBrokerPool
	// CRD that is currently deployed as a StatefulSet.
	DeployedGeneration int64 `json:"deployedGeneration,omitempty"`
	// Conditions holds the conditions for the broker pool.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// EmbeddedBrokerPoolStatus is the per-pool status sub-structure also surfaced
// inside StretchClusterStatus.NodePools.
type EmbeddedBrokerPoolStatus struct {
	// Name is the name of the pool.
	Name string `json:"name,omitempty"`
	// Replicas is the number of actual replicas currently across
	// the broker pool. This differs from DesiredReplicas during
	// a scaling operation, but should be the same once the cluster
	// has quiesced.
	Replicas int32 `json:"replicas,omitempty"`
	// DesiredReplicas is the number of replicas that ought to be
	// run for the cluster. It combines the desired replicas across
	// all broker pools.
	DesiredReplicas int32 `json:"desiredReplicas,omitempty"`
	// OutOfDateReplicas is the number of replicas that don't currently
	// match their broker pool definitions. If OutOfDateReplicas is not 0
	// it should mean that the operator will soon roll this many pods.
	OutOfDateReplicas int32 `json:"outOfDateReplicas,omitempty"`
	// UpToDateReplicas is the number of replicas that currently match
	// their broker pool definitions.
	UpToDateReplicas int32 `json:"upToDateReplicas,omitempty"`
	// CondemnedReplicas is the number of replicas that will be decommissioned
	// as part of a scaling down operation.
	CondemnedReplicas int32 `json:"condemnedReplicas,omitempty"`
	// ReadyReplicas is the number of replicas whose readiness probes are
	// currently passing.
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`
	// RunningReplicas is the number of replicas that are actively in a running
	// state.
	RunningReplicas int32 `json:"runningReplicas,omitempty"`
}

// +kubebuilder:object:root=true
type RedpandaBrokerPoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RedpandaBrokerPool `json:"items"`
}

func (l *RedpandaBrokerPoolList) GetItems() []*RedpandaBrokerPool {
	return functional.MapFn(ptr.To, l.Items)
}

// RedpandaBrokerPoolSpec contains the broker pool spec.
type RedpandaBrokerPoolSpec struct {
	EmbeddedBrokerPoolSpec `json:",inline"`
	ClusterRef             ClusterRef `json:"clusterRef"`
}

// EmbeddedBrokerPoolSpec carries the per-pool configuration. It mirrors the
// shape NodePool's EmbeddedNodePoolSpec used to have for the StretchCluster
// path, with the per-K8s-cluster fields (TLS, Listeners, ClusterDomain,
// External, RBAC, ServiceAccount, Monitoring) attached and optional overrides
// for the cluster-wide defaultables (Storage, Resources, ImagePullSecrets).
type EmbeddedBrokerPoolSpec struct {
	// Chart default: {}
	AdditionalSelectorLabels map[string]string `json:"additionalSelectorLabels,omitempty"`
	// Chart default: 3
	Replicas *int32 `json:"replicas,omitempty"`
	// Chart default: []
	AdditionalRedpandaCmdFlags []string `json:"additionalRedpandaCmdFlags,omitempty"`
	// Chart default:
	//     labels: {}
	//     annotations: {}
	//     spec:
	//       securityContext: {}
	//       affinity:
	//         podAntiAffinity:
	//           requiredDuringSchedulingIgnoredDuringExecution:
	//           - topologyKey: kubernetes.io/hostname
	//             labelSelector:
	//               matchLabels:
	//                 "app.kubernetes.io/component": '{{ include "redpanda.name" . }}-{{pool.name}}-statefulset'
	//                 "app.kubernetes.io/instance":  '{{ .Release.Name }}'
	//                 "app.kubernetes.io/name":      '{{ include "redpanda.name" . }}'
	//       terminationGracePeriodSeconds: 90
	//       nodeSelector: {}
	//       priorityClassName: ""
	//       tolerations: []
	//       topologySpreadConstraints:
	//       - maxSkew: 1
	//         topologyKey: topology.kubernetes.io/zone
	//         whenUnsatisfiable: ScheduleAnyway
	//         labelSelector:
	//           matchLabels:
	//             "app.kubernetes.io/component": '{{ include "redpanda.name" . }}-{{pool.name}}-statefulset'
	//             "app.kubernetes.io/instance":  '{{ .Release.Name }}'
	//             "app.kubernetes.io/name":      '{{ include "redpanda.name" . }}'
	PodTemplate *PodTemplate `json:"podTemplate,omitempty"`
	// Services configures overrides for Services created by the operator.
	Services       *BrokerPoolServices       `json:"services,omitempty"`
	InitContainers *BrokerPoolInitContainers `json:"initContainers,omitempty"`
	// Default:
	//     repository: docker.redpanda.com/redpandadata/redpanda
	//     tag: {{.redpandaVersion}}
	Image *RedpandaImage `json:"image,omitempty"`
	// Default:
	//     repository: docker.redpanda.com/redpandadata/redpanda-operator
	//     tag: {{.operatorVersion}}
	SidecarImage *RedpandaImage `json:"sidecarImage,omitempty"`
	// Chart default:
	//     repository: busybox
	//     tag: latest
	InitContainerImage *InitContainerImage `json:"initContainerImage,omitempty"`

	// ClusterDomain customizes the K8s cluster's DNS suffix used to generate
	// internal pod domains (e.g. "cluster.local"). Defaults to "cluster.local".
	ClusterDomain *string `json:"clusterDomain,omitempty"`

	// TLS configures TLS settings for listeners on this broker pool's pods.
	// Secret references resolve in the K8s cluster where this pool runs.
	TLS *TLS `json:"tls,omitempty"`

	// External configures external access (NodePort, LoadBalancer, advertised
	// addresses) for this broker pool's pods.
	External *External `json:"external,omitempty"`

	// Listeners configures listener settings (admin, kafka, http proxy, schema
	// registry, rpc) for this broker pool's pods.
	Listeners *StretchListeners `json:"listeners,omitempty"`

	// RBAC configures Role Based Access Control resources created in the
	// K8s cluster where this broker pool runs.
	RBAC *RBAC `json:"rbac,omitempty"`

	// ServiceAccount configures the K8s ServiceAccount used by pods in this
	// broker pool. Cloud workload-identity annotations belong here.
	ServiceAccount *ServiceAccount `json:"serviceAccount,omitempty"`

	// Monitoring configures the ServiceMonitor created in the K8s cluster
	// where this broker pool runs.
	Monitoring *Monitoring `json:"monitoring,omitempty"`

	// Fields below override defaults inherited from the parent StretchCluster.
	// When set, the broker pool value replaces the StretchCluster value
	// entirely (no deep merge). When nil, the StretchCluster value applies.

	// Storage overrides StretchCluster.Spec.Storage for this broker pool.
	// Useful when clusters expose different StorageClasses or mount types.
	Storage *StretchStorage `json:"storage,omitempty"`

	// Resources overrides StretchCluster.Spec.Resources for this broker pool.
	// Useful when clusters run on different node SKUs.
	Resources *StretchResources `json:"resources,omitempty"`

	// ImagePullSecrets overrides StretchCluster.Spec.ImagePullSecrets for
	// this broker pool. Useful for per-cluster registry mirrors.
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
}

// BrokerPoolConfigurator mirrors PoolConfigurator on NodePool. Duplicated so
// RedpandaBrokerPool can evolve independently from NodePool.
type BrokerPoolConfigurator struct {
	// Chart default: []
	AdditionalCLIArgs []string `json:"additionalCLIArgs,omitempty"`
}

// BrokerPoolSetDataDirOwnership mirrors PoolSetDataDirOwnership on NodePool.
type BrokerPoolSetDataDirOwnership struct {
	// Chart default: false
	Enabled *bool `json:"enabled,omitempty"`
}

// BrokerPoolFSValidator mirrors PoolFSValidator on NodePool.
type BrokerPoolFSValidator struct {
	// Chart default: false
	Enabled *bool `json:"enabled,omitempty"`
	// Chart default: xfs
	ExpectedFS *string `json:"expectedFS,omitempty"`
}

// BrokerPoolInitContainers mirrors PoolInitContainers on NodePool.
type BrokerPoolInitContainers struct {
	FSValidator         *BrokerPoolFSValidator         `json:"fsValidator,omitempty"`
	SetDataDirOwnership *BrokerPoolSetDataDirOwnership `json:"setDataDirOwnership,omitempty"`
	Configurator        *BrokerPoolConfigurator        `json:"configurator,omitempty"`
}

// BrokerPoolServices configures overrides for Services created by the operator
// for this broker pool.
type BrokerPoolServices struct {
	// PerPod configures overrides for per-pod ClusterIP Services.
	PerPod *BrokerPerPodServices `json:"perPod,omitempty"`
}

// BrokerPerPodServices configures overrides for per-pod ClusterIP Services.
// Local overrides apply to Services for pods in the same K8s cluster as the
// broker pool; Remote overrides apply to Services created for pods in other
// clusters.
type BrokerPerPodServices struct {
	// Local overrides are applied to per-pod Services for pods in the local cluster.
	Local *BrokerPerPodServiceOverride `json:"local,omitempty"`
	// Remote overrides are applied to per-pod Services for pods in remote clusters.
	Remote *BrokerPerPodServiceOverride `json:"remote,omitempty"`
}

// BrokerPerPodServiceOverride defines overrides for a per-pod Service using
// apply-configuration types. Only fields that are set will be merged
// into the generated Service.
type BrokerPerPodServiceOverride struct {
	// Enabled controls whether this per-pod Service is created. Defaults to true.
	Enabled     *bool                                      `json:"enabled,omitempty"`
	Labels      map[string]string                          `json:"labels,omitempty"`
	Annotations map[string]string                          `json:"annotations,omitempty"`
	Spec        *applycorev1.ServiceSpecApplyConfiguration `json:"spec,omitempty"`
}

// IsEnabled returns whether this override allows the Service to be created.
// Defaults to true when Enabled is nil.
func (in *BrokerPerPodServiceOverride) IsEnabled() bool {
	if in == nil || in.Enabled == nil {
		return true
	}
	return *in.Enabled
}

func (in *BrokerPerPodServiceOverride) DeepCopy() *BrokerPerPodServiceOverride {
	if in == nil {
		return nil
	}
	out := new(BrokerPerPodServiceOverride)
	if in.Enabled != nil {
		out.Enabled = ptr.To(*in.Enabled)
	}
	if in.Labels != nil {
		out.Labels = make(map[string]string, len(in.Labels))
		maps.Copy(out.Labels, in.Labels)
	}
	if in.Annotations != nil {
		out.Annotations = make(map[string]string, len(in.Annotations))
		maps.Copy(out.Annotations, in.Annotations)
	}
	if in.Spec != nil {
		// JSON round-trip deep copy for apply-configuration types.
		data, _ := json.Marshal(in.Spec)
		out.Spec = &applycorev1.ServiceSpecApplyConfiguration{}
		_ = json.Unmarshal(data, out.Spec)
		// Preserve empty Selector map (signals "clear selector") which
		// json.Marshal drops due to omitempty.
		if in.Spec.Selector != nil && out.Spec.Selector == nil {
			out.Spec.Selector = make(map[string]string)
		}
	}
	return out
}

func (in *BrokerPoolServices) DeepCopy() *BrokerPoolServices {
	if in == nil {
		return nil
	}
	return &BrokerPoolServices{
		PerPod: in.PerPod.DeepCopy(),
	}
}

func (in *BrokerPerPodServices) DeepCopy() *BrokerPerPodServices {
	if in == nil {
		return nil
	}
	return &BrokerPerPodServices{
		Local:  in.Local.DeepCopy(),
		Remote: in.Remote.DeepCopy(),
	}
}
