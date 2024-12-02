// Copyright 2024 Redpanda Data, Inc.
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
	helmv2beta2 "github.com/fluxcd/helm-controller/api/v2beta2"
	"github.com/fluxcd/pkg/apis/meta"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"

	redpandachart "github.com/redpanda-data/helm-charts/charts/redpanda"
	"github.com/redpanda-data/helm-charts/pkg/gotohelm/helmette"
	"github.com/redpanda-data/helm-charts/pkg/kube"

	"github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
)

const (
	RedpandaChartRepository = "https://charts.redpanda.com/"

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
	// Defines how to handle upgrades, including failures.
	Upgrade *HelmUpgrade `json:"upgrade,omitempty"`
	// NOTE! Alpha feature
	// UseFlux flag set to `false` will prevent helm controller from reconciling helm chart. The operator would be
	// tight with `go` based Redpanda helm chart version. The rest of the ChartRef fields would be ignored.
	//
	// Before setting UseFlux flag to `false` please alight your ChartVersion to at least `5.9.11`
	// version of the Redpanda chart.
	//
	// RedpandaStatus might not be accurate if flag is set to `false` and HelmRelease is manually deleted.
	//
	// To achieve dynamic switch for Flux controllers (HelmRelease and HelmRepository) the resources
	// would not be removed, but they will be put in suspended mode (if flag is provided and set to `false`).
	//
	// https://fluxcd.io/flux/components/helm/helmreleases/#suspend
	// https://fluxcd.io/flux/components/source/helmrepositories/#suspend
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
	ClusterRef v1alpha1.NamespaceNameRef `json:"clusterRef"`

	// ConsoleRef by default will not be able to reach different namespaces, but it can be
	// overwritten by adding ClusterRole and ClusterRoleBinding to operator ServiceAccount.
	ConsoleRef v1alpha1.NamespaceNameRef `json:"consoleRef"`
}

// RedpandaStatus defines the observed state of Redpanda
type RedpandaStatus struct {
	// Specifies the last observed generation.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	meta.ReconcileRequestStatus `json:",inline"`

	// Conditions holds the conditions for the Redpanda.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// LastAppliedRevision is the revision of the last successfully applied source.
	// +optional
	LastAppliedRevision string `json:"lastAppliedRevision,omitempty"`

	// LastAttemptedRevision is the revision of the last reconciliation attempt.
	// +optional
	LastAttemptedRevision string `json:"lastAttemptedRevision,omitempty"`

	// +optional
	HelmRelease string `json:"helmRelease,omitempty"`

	// +optional
	HelmReleaseReady *bool `json:"helmReleaseReady,omitempty"`

	// +optional
	HelmRepository string `json:"helmRepository,omitempty"`

	// +optional
	HelmRepositoryReady *bool `json:"helmRepositoryReady,omitempty"`

	// +optional
	UpgradeFailures int64 `json:"upgradeFailures,omitempty"`

	// Failures is the reconciliation failure count against the latest desired
	// state. It is reset after a successful reconciliation.
	// +optional
	Failures int64 `json:"failures,omitempty"`

	// +optional
	InstallFailures int64 `json:"installFailures,omitempty"`

	// ManagedDecommissioningNode indicates that a node is currently being
	// decommissioned from the cluster and provides its ordinal number.
	// +optional
	ManagedDecommissioningNode *int32 `json:"decommissioningNode,omitempty"`

	// LicenseStatus contains information about the current state of any
	// installed license in the Redpanda cluster.
	// +optional
	LicenseStatus *RedpandaLicenseStatus `json:"license,omitempty"`
}

type RedpandaLicenseStatus struct {
	Expired       bool     `json:"expired"`
	Violation     bool     `json:"violation"`
	Type          string   `json:"type"`
	Organization  string   `json:"organization"`
	InUseFeatures []string `json:"inUseFeatures,omitempty"`
	// +optional
	Expiration *metav1.Time `json:"expiration,omitempty"`
}

func (s *RedpandaLicenseStatus) String() string {
	expired := strconv.FormatBool(s.Expired)
	expiration := "nil"
	if s.Expiration != nil {
		expiration = s.Expiration.UTC().Format("Jan 2 2006 MST")
	}

	return fmt.Sprintf("License Status: Expired(%s), Expiration(%s), Features([%s])", expired, expiration, strings.Join(s.InUseFeatures, ", "))
}

type RemediationStrategy string

// HelmUpgrade configures the behavior and strategy for Helm chart upgrades.
type HelmUpgrade struct {
	// Specifies the actions to take on upgrade failures. See https://pkg.go.dev/github.com/fluxcd/helm-controller/api/v2beta1#UpgradeRemediation.
	Remediation *helmv2beta2.UpgradeRemediation `json:"remediation,omitempty"`
	// Enables forceful updates during an upgrade.
	Force *bool `json:"force,omitempty"`
	// Specifies whether to preserve user-configured values during an upgrade.
	PreserveValues *bool `json:"preserveValues,omitempty"`
	// Specifies whether to perform cleanup in case of failed upgrades.
	CleanupOnFail *bool `json:"cleanupOnFail,omitempty"`
}

// Redpanda defines the CRD for Redpanda clusters.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=redpandas
// +kubebuilder:resource:shortName=rp
// +kubebuilder:printcolumn:name="License",type="string",JSONPath=`.status.conditions[?(@.type=="ClusterLicenseValid")].message`,description=""
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status",description=""
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].message",description=""
// +kubebuilder:storageversion
type Redpanda struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Defines the desired state of the Redpanda cluster.
	Spec RedpandaSpec `json:"spec,omitempty"`
	// Represents the current status of the Redpanda cluster.
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

func (in *Redpanda) GetHelmRepositoryName() string {
	helmRepository := in.Spec.ChartRef.HelmRepositoryName
	if helmRepository == "" {
		helmRepository = "redpanda-repository"
	}
	return helmRepository
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

// RedpandaReady registers a successful reconciliation of the given HelmRelease.
func RedpandaReady(rp *Redpanda) *Redpanda {
	newCondition := metav1.Condition{
		Type:    meta.ReadyCondition,
		Status:  metav1.ConditionTrue,
		Reason:  "RedpandaClusterDeployed",
		Message: "Redpanda reconciliation succeeded",
	}
	apimeta.SetStatusCondition(rp.GetConditions(), newCondition)
	rp.Status.LastAppliedRevision = rp.Status.LastAttemptedRevision
	return rp
}

// RedpandaNotReady registers a failed reconciliation of the given Redpanda.
func RedpandaNotReady(rp *Redpanda, reason, message string) *Redpanda {
	newCondition := metav1.Condition{
		Type:    meta.ReadyCondition,
		Status:  metav1.ConditionFalse,
		Reason:  reason,
		Message: message,
	}
	apimeta.SetStatusCondition(rp.GetConditions(), newCondition)
	return rp
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

func (in *Redpanda) GetValues() (redpandachart.Values, error) {
	values, err := redpandachart.Chart.LoadValues(in.Spec.ClusterSpec)
	if err != nil {
		return redpandachart.Values{}, errors.WithStack(err)
	}

	return helmette.Unwrap[redpandachart.Values](values), nil
}

func (in *Redpanda) GetDot(restConfig *rest.Config) (*helmette.Dot, error) {
	var values []byte
	var partial redpandachart.PartialValues

	values, err := json.Marshal(in.Spec.ClusterSpec)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(values, &partial); err != nil {
		return nil, err
	}

	release := helmette.Release{
		Name:      in.Name,
		Namespace: in.Namespace,
		Service:   "redpanda",
		IsInstall: true,
		IsUpgrade: true,
	}

	return redpandachart.Chart.Dot(kube.RestToConfig(restConfig), release, partial)
}
