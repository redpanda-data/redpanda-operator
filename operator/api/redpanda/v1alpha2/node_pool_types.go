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

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=nodepools
// +kubebuilder:printcolumn:name="Bound",type="string",JSONPath=".status.conditions[?(@.type==\"Bound\")].status",description=""
// +kubebuilder:printcolumn:name="Deployed",type="string",JSONPath=".status.conditions[?(@.type==\"Deployed\")].status",description=""
type NodePool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec NodePoolSpec `json:"spec,omitempty"`
	// +kubebuilder:default={conditions: {{type: "Bound", status: "Unknown", reason: "NotReconciled", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}, {type: "Deployed", status: "Unknown", reason: "NotReconciled", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}, {type: "Quiesced", status: "Unknown", reason: "NotReconciled", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}, {type: "ExternalAccessReady", status: "Unknown", reason: "NotReconciled", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}, {type: "Stable", status: "Unknown", reason: "NotReconciled", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}}
	Status NodePoolStatus `json:"status,omitempty"`
}

func (n *NodePool) GetClusterSource() *ClusterSource {
	return &ClusterSource{
		ClusterRef: &n.Spec.ClusterRef,
	}
}

// NodePoolStatus defines the observed state of any node pools tied to this cluster
type NodePoolStatus struct {
	EmbeddedNodePoolStatus `json:",inline"`
	// DeployedGeneration represents the generation of the NodePool CRD that is currently
	// deployed as a StatefulSet
	DeployedGeneration int64 `json:"deployedGeneration,omitempty"`
	// Conditions holds the conditions for the Redpanda.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// EmbeddedNodePoolStatus defines the observed state of any node pools tied to this cluster
type EmbeddedNodePoolStatus struct {
	// Name is the name of the pool
	Name string `json:"name,omitempty"`
	// Replicas is the number of actual replicas currently across
	// the node pool. This differs from DesiredReplicas during
	// a scaling operation, but should be the same once the cluster
	// has quiesced.
	Replicas int32 `json:"replicas,omitempty"`
	// DesiredReplicas is the number of replicas that ought to be
	// run for the cluster. It combines the desired replicas across
	// all node pools.
	DesiredReplicas int32 `json:"desiredReplicas,omitempty"`
	// OutOfDateReplicas is the number of replicas that don't currently
	// match their node pool definitions. If OutOfDateReplicas is not 0
	// it should mean that the operator will soon roll this many pods.
	OutOfDateReplicas int32 `json:"outOfDateReplicas,omitempty"`
	// UpToDateReplicas is the number of replicas that currently match
	// their node pool definitions.
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
type NodePoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NodePool `json:"items"`
}

func (s *NodePoolList) GetItems() []*NodePool {
	return functional.MapFn(ptr.To, s.Items)
}

// NodePoolSpec contains the node pool spec for the given node pool.
// Note that the defaulting behavior comes from the underlying Redpanda
// chart renderer, the attributes specified here will get merged in and
// override the defaults.
type NodePoolSpec struct {
	EmbeddedNodePoolSpec `json:",inline"`
	ClusterRef           ClusterRef `json:"clusterRef"`
}

type PoolConfigurator struct {
	// Chart default: []
	AdditionalCLIArgs []string `json:"additionalCLIArgs,omitempty"`
}

type PoolSetDataDirOwnership struct {
	// Chart default: false
	Enabled *bool `json:"enabled,omitempty"`
}

type PoolFSValidator struct {
	// Chart default: false
	Enabled *bool `json:"enabled,omitempty"`
	// Chart default: xfs
	ExpectedFS *string `json:"expectedFS,omitempty"`
}

type PoolInitContainers struct {
	FSValidator         *PoolFSValidator         `json:"fsValidator,omitempty"`
	SetDataDirOwnership *PoolSetDataDirOwnership `json:"setDataDirOwnership,omitempty"`
	Configurator        *PoolConfigurator        `json:"configurator,omitempty"`
}

type EmbeddedNodePoolSpec struct {
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
	Services       *NodePoolServices   `json:"services,omitempty"`
	InitContainers *PoolInitContainers `json:"initContainers,omitempty"`
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

	// Fields below apply when this NodePool is owned by a StretchCluster.
	// They configure per-K8s-cluster infrastructure that varies between
	// clusters and therefore cannot live on the federated StretchCluster spec.

	// ClusterDomain customizes the K8s cluster's DNS suffix used to generate
	// internal pod domains (e.g. "cluster.local"). Defaults to "cluster.local".
	ClusterDomain *string `json:"clusterDomain,omitempty"`

	// TLS configures TLS settings for listeners on this NodePool's pods.
	// Secret references resolve in the K8s cluster where this NodePool runs.
	TLS *TLS `json:"tls,omitempty"`

	// External configures external access (NodePort, LoadBalancer, advertised
	// addresses) for this NodePool's pods.
	External *External `json:"external,omitempty"`

	// Listeners configures listener settings (admin, kafka, http proxy, schema
	// registry, rpc) for this NodePool's pods.
	Listeners *StretchListeners `json:"listeners,omitempty"`

	// RBAC configures Role Based Access Control resources created in the
	// K8s cluster where this NodePool runs.
	RBAC *RBAC `json:"rbac,omitempty"`

	// ServiceAccount configures the K8s ServiceAccount used by pods in this
	// NodePool. Cloud workload-identity annotations belong here.
	ServiceAccount *ServiceAccount `json:"serviceAccount,omitempty"`

	// Monitoring configures the ServiceMonitor created in the K8s cluster
	// where this NodePool runs.
	Monitoring *Monitoring `json:"monitoring,omitempty"`

	// RackAwareness configures rack awareness for the brokers in this
	// NodePool. The node-annotation key that identifies a rack varies by
	// cloud provider (and by on-prem topology), so the setting lives on
	// the NodePool — each member K8s cluster picks the annotation that
	// matches its underlying nodes.
	RackAwareness *RackAwareness `json:"rackAwareness,omitempty"`

	// Fields below override defaults inherited from the parent StretchCluster.
	// When set, the NodePool value replaces the StretchCluster value entirely
	// (no deep merge). When nil, the StretchCluster value applies.

	// Storage overrides StretchCluster.Spec.Storage for this NodePool.
	// Useful when clusters expose different StorageClasses or mount types.
	Storage *StretchStorage `json:"storage,omitempty"`

	// Resources overrides StretchCluster.Spec.Resources for this NodePool.
	// Useful when clusters run on different node SKUs.
	Resources *StretchResources `json:"resources,omitempty"`

	// ImagePullSecrets overrides StretchCluster.Spec.ImagePullSecrets for
	// this NodePool. Useful for per-cluster registry mirrors.
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// Logging overrides StretchCluster.Spec.Logging for this NodePool.
	// Useful during incidents where only a subset of brokers in one
	// region needs an elevated log level for investigation.
	Logging *StretchLogging `json:"logging,omitempty"`
}

// NodePoolServices configures overrides for Services created by the operator
// for this NodePool.
type NodePoolServices struct {
	// PerPod configures overrides for per-pod ClusterIP Services.
	PerPod *PerPodServices `json:"perPod,omitempty"`
}

// PerPodServices configures overrides for per-pod ClusterIP Services.
// Local overrides apply to Services for pods in the same K8s cluster as the
// NodePool; Remote overrides apply to Services created for pods in other clusters.
type PerPodServices struct {
	// Local overrides are applied to per-pod Services for pods in the local cluster.
	Local *PerPodServiceOverride `json:"local,omitempty"`
	// Remote overrides are applied to per-pod Services for pods in remote clusters.
	Remote *PerPodServiceOverride `json:"remote,omitempty"`
}

// PerPodServiceOverride defines overrides for a per-pod Service using
// apply-configuration types. Only fields that are set will be merged
// into the generated Service.
type PerPodServiceOverride struct {
	// Enabled controls whether this per-pod Service is created. Defaults to true.
	Enabled     *bool                                      `json:"enabled,omitempty"`
	Labels      map[string]string                          `json:"labels,omitempty"`
	Annotations map[string]string                          `json:"annotations,omitempty"`
	Spec        *applycorev1.ServiceSpecApplyConfiguration `json:"spec,omitempty"`
}

// IsEnabled returns whether this override allows the Service to be created.
// Defaults to true when Enabled is nil.
func (in *PerPodServiceOverride) IsEnabled() bool {
	if in == nil || in.Enabled == nil {
		return true
	}
	return *in.Enabled
}

func (in *PerPodServiceOverride) DeepCopy() *PerPodServiceOverride {
	if in == nil {
		return nil
	}
	out := new(PerPodServiceOverride)
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

func (in *NodePoolServices) DeepCopy() *NodePoolServices {
	if in == nil {
		return nil
	}
	return &NodePoolServices{
		PerPod: in.PerPod.DeepCopy(),
	}
}

func (in *PerPodServices) DeepCopy() *PerPodServices {
	if in == nil {
		return nil
	}
	return &PerPodServices{
		Local:  in.Local.DeepCopy(),
		Remote: in.Remote.DeepCopy(),
	}
}

func MinimalNodePoolSpec(cluster *Redpanda) NodePoolSpec {
	return NodePoolSpec{
		ClusterRef: ClusterRef{
			Name: cluster.Name,
		},
		EmbeddedNodePoolSpec: EmbeddedNodePoolSpec{
			Replicas: ptr.To(int32(1)),
			Image: &RedpandaImage{
				Repository: ptr.To("redpandadata/redpanda"), // Use docker.io to make caching easier and to not inflate our own metrics.
			},
		},
	}
}
