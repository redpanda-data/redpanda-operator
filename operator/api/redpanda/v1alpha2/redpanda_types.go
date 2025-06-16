// Copyright 2025 Redpanda Data, Inc.
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
	"fmt"
	"strconv"
	"strings"

	"github.com/cockroachdb/errors"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"

	redpandav5 "github.com/redpanda-data/redpanda-operator/charts/redpanda/v5"
	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
)

const (
	// ClusterConfigSynced is a condition indicating whether or not the
	// redpanda cluster's configuration is up to date with the desired config.
	ClusterConfigSynced = "ClusterConfigSynced"
	// ClusterLicenseValid is a condition indicating whether or not the
	// redpanda cluster has a valid license.
	ClusterLicenseValid = "ClusterLicenseValid"
)

type ChartRef struct {
	// Specifies the name of the chart to deploy.
	ChartName string `json:"chartName,omitempty"`
	// Defines the version of the Redpanda Helm chart to deploy.
	ChartVersion string `json:"chartVersion,omitempty"`
	// Defines the chart repository to use. Defaults to `redpanda` if not defined.
	HelmRepositoryName string `json:"helmRepositoryName,omitempty"`
	// Specifies the time to wait for any individual Kubernetes operation (like Jobs
	// for hooks) during Helm actions. Defaults to `15m0s`.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern="^([0-9]+(\\.[0-9]+)?(ms|s|m|h))+$"
	// +optional
	Timeout *metav1.Duration `json:"timeout,omitempty"`
	// +kubebuilder:pruning:PreserveUnknownFields
	// Defines how to handle upgrades, including failures.
	Upgrade *runtime.RawExtension `json:"upgrade,omitempty"`
	// Setting the `useFlux` flag to `false` disables the Helm controller's reconciliation of the Helm chart.
	// This ties the operator to a specific version of the Go-based Redpanda Helm chart, causing all other
	// ChartRef fields to be ignored.
	//
	// Before disabling `useFlux`, ensure that your `chartVersion` is aligned with `5.9.21` or the corresponding
	// version of the Redpanda chart.
	//
	// Note: When `useFlux` is set to `false`, `RedpandaStatus` may become inaccurate if the HelmRelease is
	// manually deleted.
	//
	// To dynamically switch Flux controllers (HelmRelease and HelmRepository), setting `useFlux` to `false`
	// will suspend these resources instead of removing them.
	//
	// References:
	// - https://fluxcd.io/flux/components/helm/helmreleases/#suspend
	// - https://fluxcd.io/flux/components/source/helmrepositories/#suspend
	//
	// +optional
	UseFlux *bool `json:"useFlux,omitempty"`
}

// RedpandaSpec defines the desired state of the Redpanda cluster.
type RedpandaSpec struct {
	// Defines chart details, including the version and repository.
	ChartRef ChartRef `json:"chartRef,omitempty"`
	// Defines the Helm values to use to deploy the cluster.
	ClusterSpec *RedpandaClusterSpec `json:"clusterSpec,omitempty"`
	// Deprecated and Removed in v2.2.3-24.2.X. Downgrade to v2.2.2-24.2.4 perform the migration
	Migration *Migration `json:"migration,omitempty"`
}

// Migration can configure old Cluster and Console custom resource that will be disabled.
// With Migration the ChartRef and ClusterSpec still need to be correctly configured.
type Migration struct {
	Enabled bool `json:"enabled"`
	// ClusterRef by default will not be able to reach different namespaces, but it can be
	// overwritten by adding ClusterRole and ClusterRoleBinding to operator ServiceAccount.
	ClusterRef vectorizedv1alpha1.NamespaceNameRef `json:"clusterRef"`

	// ConsoleRef by default will not be able to reach different namespaces, but it can be
	// overwritten by adding ClusterRole and ClusterRoleBinding to operator ServiceAccount.
	ConsoleRef vectorizedv1alpha1.NamespaceNameRef `json:"consoleRef"`
}

// NodePoolStatus defines the observed state of any node pools tied to this cluster
type NodePoolStatus struct {
	// Name is the name of the pool
	Name string `json:"name"`
	// Replicas is the number of actual replicas currently across
	// the node pool. This differs from DesiredReplicas during
	// a scaling operation, but should be the same once the cluster
	// has quiesced.
	Replicas int32 `json:"replicas"`
	// DesiredReplicas is the number of replicas that ought to be
	// run for the cluster. It combines the desired replicas across
	// all node pools.
	DesiredReplicas int32 `json:"desiredReplicas"`
	// OutOfDateReplicas is the number of replicas that don't currently
	// match their node pool definitions. If OutOfDateReplicas is not 0
	// it should mean that the operator will soon roll this many pods.
	OutOfDateReplicas int32 `json:"outOfDateReplicas"`
	// UpToDateReplicas is the number of replicas that currently match
	// their node pool definitions.
	UpToDateReplicas int32 `json:"upToDateReplicas"`
	// CondemnedReplicas is the number of replicas that will be decommissioned
	// as part of a scaling down operation.
	CondemnedReplicas int32 `json:"condemnedReplicas"`
	// ReadyReplicas is the number of replicas whose readiness probes are
	// currently passing.
	ReadyReplicas int32 `json:"readyReplicas"`
	// RunningReplicas is the number of replicas that are actively in a running
	// state.
	RunningReplicas int32 `json:"runningReplicas"`
}

// RedpandaStatus defines the observed state of Redpanda
type RedpandaStatus struct {
	// Conditions holds the conditions for the Redpanda.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// LicenseStatus contains information about the current state of any
	// installed license in the Redpanda cluster.
	// +optional
	LicenseStatus *RedpandaLicenseStatus `json:"license,omitempty"`

	// NodePools contains information about the node pools associated
	// with this cluster.
	// +optional
	NodePools []NodePoolStatus `json:"nodePools,omitempty"`

	// ConfigVersion contains the configuration version written in
	// Redpanda used for restarting broker nodes as necessary.
	// +optional
	ConfigVersion string `json:"configVersion,omitempty"`

	// everything below here is deprecated and should be removed

	// Specifies the last observed generation.
	// deprecated
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// LastHandledReconcileAt holds the value of the most recent
	// reconcile request value, so a change of the annotation value
	// can be detected.
	// deprecated
	// +optional
	LastHandledReconcileAt string `json:"lastHandledReconcileAt,omitempty"`

	// LastAppliedRevision is the revision of the last successfully applied source.
	// deprecated
	// +optional
	LastAppliedRevision string `json:"lastAppliedRevision,omitempty"`

	// LastAttemptedRevision is the revision of the last reconciliation attempt.
	// deprecated
	// +optional
	LastAttemptedRevision string `json:"lastAttemptedRevision,omitempty"`

	// deprecated
	// +optional
	HelmRelease string `json:"helmRelease,omitempty"`

	// deprecated
	// +optional
	HelmReleaseReady *bool `json:"helmReleaseReady,omitempty"`

	// deprecated
	// +optional
	HelmRepository string `json:"helmRepository,omitempty"`

	// deprecated
	// +optional
	HelmRepositoryReady *bool `json:"helmRepositoryReady,omitempty"`

	// deprecated
	// +optional
	UpgradeFailures int64 `json:"upgradeFailures,omitempty"`

	// Failures is the reconciliation failure count against the latest desired
	// state. It is reset after a successful reconciliation.
	// deprecated
	// +optional
	Failures int64 `json:"failures,omitempty"`

	// deprecated
	// +optional
	InstallFailures int64 `json:"installFailures,omitempty"`

	// ManagedDecommissioningNode indicates that a node is currently being
	// decommissioned from the cluster and provides its ordinal number.
	// deprecated
	// +optional
	ManagedDecommissioningNode *int32 `json:"decommissioningNode,omitempty"`
}

type RedpandaLicenseStatus struct {
	Violation     bool     `json:"violation"`
	InUseFeatures []string `json:"inUseFeatures"`
	// +optional
	Expired *bool `json:"expired,omitempty"`
	// +optional
	Type *string `json:"type,omitempty"`
	// +optional
	Organization *string `json:"organization,omitempty"`
	// +optional
	Expiration *metav1.Time `json:"expiration,omitempty"`
}

func (s *RedpandaLicenseStatus) String() string {
	expired := "nil"
	expiration := "nil"
	if s.Expired != nil {
		expired = strconv.FormatBool(*s.Expired)
	}
	if s.Expiration != nil {
		expiration = s.Expiration.UTC().Format("Jan 2 2006 MST")
	}

	return fmt.Sprintf("License Status: Expired(%s), Expiration(%s), Features([%s])", expired, expiration, strings.Join(s.InUseFeatures, ", "))
}

// Redpanda defines the CRD for Redpanda clusters.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=redpandas
// +kubebuilder:resource:shortName=rp
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status",description=""
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].message",description=""
// +kubebuilder:printcolumn:name="License",type="string",JSONPath=".status.conditions[?(@.type==\"LicenseValid\")].message",description=""
type Redpanda struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Defines the desired state of the Redpanda cluster.
	Spec RedpandaSpec `json:"spec,omitempty"`
	// Represents the current status of the Redpanda cluster.
	// +kubebuilder:default={conditions: {{type: "Ready", status: "Unknown", reason: "NotReconciled", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}, {type: "Healthy", status: "Unknown", reason: "NotReconciled", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}, {type: "LicenseValid", status: "Unknown", reason: "NotReconciled", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}, {type: "ResourcesSynced", status: "Unknown", reason: "NotReconciled", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}, {type: "ConfigurationApplied", status: "Unknown", reason: "NotReconciled", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}, {type: "Quiesced", status: "Unknown", reason: "NotReconciled", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}, {type: "Stable", status: "Unknown", reason: "NotReconciled", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}}
	Status RedpandaStatus `json:"status,omitempty"`
}

// RedpandaList contains a list of Redpanda objects.
// +kubebuilder:object:root=true
type RedpandaList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// Specifies a list of Redpanda resources.
	Items []Redpanda `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Redpanda{}, &RedpandaList{})
}

func (in *Redpanda) GetHelmReleaseName() string {
	return in.Name
}

func (in *Redpanda) ValuesJSON() (*apiextensionsv1.JSON, error) {
	vyaml, err := json.Marshal(in.Spec.ClusterSpec)
	if err != nil {
		return nil, fmt.Errorf("could not convert spec to yaml: %w", err)
	}
	values := &apiextensionsv1.JSON{Raw: vyaml}

	return values, nil
}

func (in *Redpanda) GenerationObserved() bool {
	return in.Generation != 0 && in.Generation == in.Status.ObservedGeneration
}

// GetConditions returns the status conditions of the object.
func (in *Redpanda) GetConditions() *[]metav1.Condition {
	return &in.Status.Conditions
}

func (in *Redpanda) OwnerShipRefObj() metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: in.APIVersion,
		Kind:       in.Kind,
		Name:       in.Name,
		UID:        in.UID,
		Controller: ptr.To(true),
	}
}

func (in *Redpanda) GetValues() (redpandav5.Values, error) {
	values, err := redpandav5.Chart.LoadValues(in.Spec.ClusterSpec)
	if err != nil {
		return redpandav5.Values{}, errors.WithStack(err)
	}

	return helmette.Unwrap[redpandav5.Values](values), nil
}

func (in *Redpanda) GetDot(restConfig *rest.Config) (*helmette.Dot, error) {
	return redpandav5.Chart.Dot(
		restConfig,
		helmette.Release{
			Name:      in.GetHelmReleaseName(),
			Namespace: in.Namespace,
			Service:   "redpanda",
			IsInstall: true,
			IsUpgrade: true,
		}, in.Spec.ClusterSpec.DeepCopy())
}

// MinimalRedpandaSpec returns a [RedpandaSpec] with the smallest resource
// footprint possible for use in integration and E2E tests.
func MinimalRedpandaSpec() RedpandaSpec {
	return RedpandaSpec{
		// Any empty structs are to make setting them more ergonomic
		// without having to worry about nil pointers.
		ChartRef: ChartRef{},
		ClusterSpec: &RedpandaClusterSpec{
			Config: &Config{},
			External: &External{
				// Disable NodePort creation to stop broken tests from blocking others due to port conflicts.
				Enabled: ptr.To(false),
			},
			Image: &RedpandaImage{
				Repository: ptr.To("redpandadata/redpanda"), // Use docker.io to make caching easier and to not inflate our own metrics.
			},
			Console: &RedpandaConsole{
				Enabled: ptr.To(false), // Speed up most cases by not enabling console to start.
			},
			Statefulset: &Statefulset{
				Replicas: ptr.To(1), // Speed up tests ever so slightly.
				PodAntiAffinity: &PodAntiAffinity{
					// Disable the default "hard" affinity so we can
					// schedule multiple redpanda Pods on a single
					// kubernetes node. Useful for tests that require > 3
					// brokers.
					Type: ptr.To("soft"),
				},
				// Speeds up managed decommission tests. Decommissioned
				// nodes will take the entirety of
				// TerminationGracePeriodSeconds as the pre-stop hook
				// doesn't account for decommissioned nodes.
				TerminationGracePeriodSeconds: ptr.To(10),
			},
			Resources: &Resources{
				CPU: &CPU{
					// Inform redpanda/seastar that it's not going to get
					// all the resources it's promised.
					Overprovisioned: ptr.To(true),
				},
			},
		},
	}
}
