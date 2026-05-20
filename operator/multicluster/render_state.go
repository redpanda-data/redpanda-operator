// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package multicluster

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/redpanda-data/common-go/kube"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8sapierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/utils/ptr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/tplutil"
)

// RenderState contains contextual information about the current rendering of
// stretch cluster resources. Exported methods are limited to those useful for
// establishing client connections to the cluster.
// PodEndpoint holds the information needed to create Endpoints/EndpointSlices
// for a pod in a multicluster stretch cluster.
type PodEndpoint struct {
	// Name is the pod name (e.g. "factory-test-pool-0-0").
	Name string
	// IP is the pod's IP address.
	IP string
	// Cluster is the Kubernetes cluster the pod runs on.
	Cluster string
	// Ready indicates whether the pod's readiness probe is passing.
	Ready bool
}

type RenderState struct {
	cluster        *redpandav1alpha2.StretchCluster
	inClusterPools []*redpandav1alpha2.NodePool
	pools          []*redpandav1alpha2.NodePool
	podEndpoints   []PodEndpoint
	clusterName    string
	releaseName    string
	namespace      string

	client *kube.Ctl

	// ctx carries reconciliation-scoped values (logger, trace span) so that
	// render helpers can emit structured logs bound to the reconcile that
	// invoked them. Set via WithContext(); nil is fine for unit tests and
	// yields a background-context logger.
	ctx context.Context

	seedServers          []string
	bootstrapUserSecret  *corev1.Secret
	statefulSetPodLabels map[string]string
	statefulSetSelector  map[string]string

	// externalNodePortConflicts is populated by nodePortService during
	// rendering: each entry describes a local NodePool whose external
	// Service was skipped because its requested NodePort numbers already
	// belong to another local pool's Service. The reconciler reads this to
	// surface ExternalAccessReady=False on each affected pool.
	externalNodePortConflicts []ExternalNodePortConflict
}

// ExternalNodePortConflict records that one local NodePool's external
// NodePort Service could not be rendered because another local pool in the
// same Kubernetes cluster already claims the same nodePort numbers.
type ExternalNodePortConflict struct {
	// Pool is the name of the NodePool whose external Service was skipped.
	Pool string
	// ConflictsWith is the name of the NodePool that owns the conflicting
	// nodePorts (the lexically-first pool wins).
	ConflictsWith string
	// Ports lists the colliding nodePort numbers.
	Ports []int32
}

// WithContext stores the reconciliation context so render helpers can emit
// contextual logs. Safe to call after NewRenderState.
func (r *RenderState) WithContext(ctx context.Context) *RenderState {
	r.ctx = ctx
	return r
}

// Context returns the stored reconciliation context, falling back to a
// background context if WithContext was never called (e.g. in tests).
func (r *RenderState) Context() context.Context {
	if r.ctx == nil {
		return context.Background()
	}
	return r.ctx
}

func seedServersFromNodePools(cluster *redpandav1alpha2.StretchCluster, pools []*redpandav1alpha2.NodePool) []string {
	// In MCS mode, use the clusterset.local domain so DNS resolves via the
	// MCS controller across cluster boundaries.
	addressFmt := "%s.%s:%d"
	if cluster.Spec.Networking.IsMCS() {
		addressFmt = "%s.%s.svc.clusterset.local:%d"
	}

	var seedServers []string
	for _, pool := range pools {
		for i := int32(0); i < pool.GetReplicas(); i++ {
			poolFullname := tplutil.CleanForK8s(cluster.Name) + pool.Suffix()
			name := PerPodServiceName(poolFullname, i)
			seedServers = append(seedServers, fmt.Sprintf(addressFmt, name, pool.GetNamespace(), pool.Spec.RPCPort()))
		}
	}
	return seedServers
}

// NewRenderState constructs a RenderState from a StretchCluster, its NodePools,
// and a cluster name. It uses the provided config for K8s lookups.
// The cluster and pools are deep-copied so that merging defaults does not
// mutate the caller's objects.
func NewRenderState(
	config *kube.RESTConfig,
	cluster *redpandav1alpha2.StretchCluster,
	// inClusterPool is a list of NodePools in given cluster
	inClusterPool []*redpandav1alpha2.NodePool,
	// pools is a list of NodePools in all K8S clusters
	pools []*redpandav1alpha2.NodePool,
	clusterName string,
) (*RenderState, error) {
	// Deep-copy to avoid mutating the caller's CRD objects.
	cluster = cluster.DeepCopy()
	copiedInClusterPools := make([]*redpandav1alpha2.NodePool, len(inClusterPool))
	for i, p := range inClusterPool {
		copiedInClusterPools[i] = p.DeepCopy()
	}
	copiedPools := make([]*redpandav1alpha2.NodePool, len(pools))
	for i, p := range pools {
		copiedPools[i] = p.DeepCopy()
	}

	// Sort pools by name for deterministic rendering order.
	sort.Slice(copiedInClusterPools, func(i, j int) bool {
		return copiedInClusterPools[i].Name < copiedInClusterPools[j].Name
	})
	// Sort pools by name for deterministic rendering order.
	sort.Slice(copiedPools, func(i, j int) bool {
		return copiedPools[i].Name < copiedPools[j].Name
	})

	// Apply Helm-equivalent defaults. Cluster-wide defaults run first so that
	// NodePools inheriting defaultable fields pick up the defaulted values.
	// inClusterPools and pools are deep-copied separately above, so each slice
	// needs its own defaults pass.
	cluster.Spec.MergeDefaults()
	for _, pool := range copiedPools {
		pool.Spec.MergeDefaultsFrom(&cluster.Spec)
	}
	for _, pool := range copiedInClusterPools {
		pool.Spec.MergeDefaultsFrom(&cluster.Spec)
	}

	releaseName := cluster.Name

	var ctl *kube.Ctl
	if config != nil {
		var err error
		ctl, err = kube.FromRESTConfig(config, kube.Options{
			FieldManager: string(defaultFieldOwner),
		})
		if err != nil {
			return nil, fmt.Errorf("creating kubernetes client: %w", err)
		}
	}

	state := &RenderState{
		cluster:        cluster,
		pools:          copiedPools,
		inClusterPools: copiedInClusterPools,
		clusterName:    clusterName,
		releaseName:    releaseName,
		namespace:      cluster.Namespace,
		client:         ctl,
		seedServers:    seedServersFromNodePools(cluster, copiedPools),
	}

	if err := state.fetchBootstrapUser(); err != nil {
		return nil, err
	}
	if err := state.fetchStatefulSetPodSelector(); err != nil {
		return nil, err
	}

	return state, nil
}

// WithPodEndpoints sets the pod endpoints on the render state, enabling
// the renderer to produce Endpoints and EndpointSlices for flat network mode.
func (r *RenderState) WithPodEndpoints(endpoints []PodEndpoint) *RenderState {
	r.podEndpoints = endpoints
	return r
}

// ExternalNodePortConflicts returns the list of local NodePools whose
// external NodePort Service could not be rendered because another local
// pool already claimed the same nodePort numbers. Populated during
// rendering; safe to read post-render.
func (r *RenderState) ExternalNodePortConflicts() []ExternalNodePortConflict {
	return r.externalNodePortConflicts
}

// tplData returns the template context data for Go template expansion.
// The shape mirrors Helm's built-in objects so that PodTemplate values
// containing {{ .Release.Name }} etc. continue to work after migration
// from the Helm chart.
func (r *RenderState) tplData() map[string]any {
	return map[string]any{
		"Release": map[string]any{
			"Namespace":   r.namespace,
			"Name":        r.releaseName,
			"Service":     "Helm",
			"IsUpgrade":   true,
			"ClusterName": r.clusterName,
		},
		"Name":      r.fullname(),
		"Namespace": r.namespace,
	}
}

// Spec returns the StretchClusterSpec. Exported for test/debugging access.
func (r *RenderState) Spec() *redpandav1alpha2.StretchClusterSpec {
	return &r.cluster.Spec
}

// PoolSpec returns the EmbeddedNodePoolSpec of the supplied NodePool. Every
// moved-from-StretchCluster field (TLS, Listeners, ClusterDomain, External,
// RBAC, ServiceAccount, Monitoring, plus the defaultable overrides Storage,
// Resources, ImagePullSecrets) reads through this method so each rendered
// resource picks up the spec of the pool it belongs to.
//
// Returns an empty (but non-nil) spec when pool is nil — keeps direct field
// access safe for renderers that legitimately have no pool in scope (e.g.
// before any NodePool exists).
func (r *RenderState) PoolSpec(pool *redpandav1alpha2.NodePool) *redpandav1alpha2.EmbeddedNodePoolSpec {
	if pool == nil {
		return &redpandav1alpha2.EmbeddedNodePoolSpec{}
	}
	return &pool.Spec.EmbeddedNodePoolSpec
}

// representativePool returns the first in-cluster NodePool, or nil when no
// local pools exist. Used by the few resources that remain cluster-wide and
// need *some* pool's view to populate (e.g. the headless ClusterIP Service's
// port list, which assumes all in-cluster pools agree on listener port
// numbers — TLS / auth content may still differ per pool).
func (r *RenderState) representativePool() *redpandav1alpha2.NodePool {
	if len(r.inClusterPools) == 0 {
		return nil
	}
	return r.inClusterPools[0]
}

// ServiceName returns the headless ClusterIP Service name. The headless
// service is cluster-wide (one per StretchCluster name in each K8s cluster)
// so this is derived from the cluster fullname, not any individual pool.
func (r *RenderState) ServiceName() string {
	return r.fullname()
}

// InternalDomain returns the fully qualified internal DNS suffix that the
// headless ClusterIP Service exposes for the supplied pool's pods. The
// ClusterDomain comes from the pool spec; the service name is cluster-wide.
func (r *RenderState) InternalDomain(pool *redpandav1alpha2.NodePool) string {
	return r.PoolSpec(pool).InternalDomain(r.ServiceName(), r.namespace)
}

// AdminInternalURL returns the admin API URL template used inside the
// supplied pool's pods (probes, post-install scripts). Wraps
// EmbeddedNodePoolSpec.AdminInternalURL with the cluster-wide service name.
func (r *RenderState) AdminInternalURL(pool *redpandav1alpha2.NodePool) string {
	return r.PoolSpec(pool).AdminInternalURL(r.ServiceName(), r.namespace)
}

// AdminAPIURLs returns the admin API host:port template used in probes for
// the supplied pool. Wraps EmbeddedNodePoolSpec.AdminAPIURLs with the
// cluster-wide service name.
func (r *RenderState) AdminAPIURLs(pool *redpandav1alpha2.NodePool) string {
	return r.PoolSpec(pool).AdminAPIURLs(r.ServiceName(), r.namespace)
}

// Pools returns the list of NodePools across K8S clusters. Exported for test/debugging access.
func (r *RenderState) Pools() []*redpandav1alpha2.NodePool {
	return r.pools
}

// InClusterPools returns the list of NodePools from single K8S cluster. Exported for test/debugging access.
func (r *RenderState) InClusterPools() []*redpandav1alpha2.NodePool {
	return r.inClusterPools
}

// isLocalPool returns true if the given pool is in the local cluster.
func (r *RenderState) isLocalPool(pool *redpandav1alpha2.NodePool) bool {
	for _, p := range r.inClusterPools {
		if p.Name == pool.Name {
			return true
		}
	}
	return false
}

func (r *RenderState) fullname() string {
	return tplutil.CleanForK8s(r.releaseName)
}

func (r *RenderState) commonLabels() map[string]string {
	labels := map[string]string{
		labelNameKey:        labelNameValue,
		labelInstanceKey:    r.releaseName,
		labelManagedByKey:   labelManagedByValue,
		labelComponentKey:   labelNameValue,
		labelClusterNameKey: r.clusterName,
	}
	for k, v := range r.Spec().CommonLabels {
		labels[k] = v
	}
	return labels
}

func (r *RenderState) clusterPodLabelsSelector() map[string]string {
	return map[string]string{
		labelInstanceKey:    r.releaseName,
		labelNameKey:        labelNameValue,
		labelClusterNameKey: r.clusterName,
	}
}

func (r *RenderState) poolFullname(pool *redpandav1alpha2.NodePool) string {
	return fmt.Sprintf("%s%s", r.fullname(), pool.Suffix())
}

func (r *RenderState) totalReplicas() int32 {
	var total int32
	for _, pool := range r.pools {
		total += pool.GetReplicas()
	}
	return total
}

// BrokerList returns a list of broker addresses for the given port.
// For MCS mode, uses the clusterset.local domain. For mesh/flat modes,
// uses the per-pod service name (<pool>-<ordinal>.<namespace>) which
// resolves across clusters via the synced per-pod Services.
func (r *RenderState) BrokerList(port int32) []string {
	addressFmt := "%s.%s:%d"
	if r.Spec().Networking.IsMCS() {
		addressFmt = "%s.%s.svc.clusterset.local:%d"
	}

	var brokers []string
	for _, pool := range r.pools {
		for i := int32(0); i < pool.GetReplicas(); i++ {
			name := PerPodServiceName(r.poolFullname(pool), i)
			brokers = append(brokers, fmt.Sprintf(addressFmt, name, r.namespace, port))
		}
	}
	return brokers
}

// fetchBootstrapUser looks up an existing bootstrap user secret so that we
// re-emit it with the same password rather than generating a new random one
// on every reconciliation. If the secret doesn't exist yet, secretBootstrapUser()
// will create one with a fresh random password.
func (r *RenderState) fetchBootstrapUser() error {
	if r.client == nil || !r.Spec().Auth.IsSASLEnabled() {
		return nil
	}

	sasl := r.Spec().Auth.SASL
	// If the user explicitly provides a secretKeyRef, they own the secret.
	if sasl.BootstrapUser != nil && sasl.BootstrapUser.SecretKeyRef != nil {
		return nil
	}

	secretName := r.cluster.BootstrapUserSecretName()

	// Bound the Get explicitly. fetchBootstrapUser runs inside NewRenderState
	// per cluster the renderer iterates; without a deadline a partitioned peer's
	// apiserver dial would hang the kernel TCP retry (~30–90s) and serialize
	// across every render call in the reconcile, blowing the partition-detect
	// SLA. Kept in sync with lifecycle.LocalCallTimeout — duplicated rather
	// than imported to avoid an operator/multicluster → operator/internal/lifecycle
	// import cycle (lifecycle already imports this package).
	getCtx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	var existing corev1.Secret
	if err := r.client.Get(getCtx, kube.ObjectKey{Namespace: r.namespace, Name: secretName}, &existing); err != nil {
		if k8sapierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("fetching bootstrap user secret %s/%s: %w", r.namespace, secretName, err)
	}
	existing.Immutable = ptr.To(true)
	r.bootstrapUserSecret = &existing
	return nil
}

// fetchStatefulSetPodSelector preserves the existing StatefulSet's label
// selector. StatefulSet selectors are immutable after creation, so if the
// labels were set incorrectly on first deploy, we must continue using them
// rather than generating new ones that would cause an update rejection.
// This only applies to the default (unnamed) pool for backward compatibility.
func (r *RenderState) fetchStatefulSetPodSelector() error {
	if r.client == nil {
		return nil
	}
	var existing appsv1.StatefulSet
	if err := r.client.Get(context.Background(), kube.ObjectKey{Namespace: r.namespace, Name: r.fullname()}, &existing); err != nil {
		if k8sapierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("fetching statefulset %s/%s: %w", r.namespace, r.fullname(), err)
	}
	if len(existing.Spec.Template.ObjectMeta.Labels) > 0 {
		r.statefulSetPodLabels = existing.Spec.Template.ObjectMeta.Labels
		r.statefulSetSelector = existing.Spec.Selector.MatchLabels
	}
	return nil
}
