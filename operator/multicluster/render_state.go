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
type RenderState struct {
	cluster     *redpandav1alpha2.StretchCluster
	pools       []*redpandav1alpha2.NodePool
	clusterName string
	releaseName string
	namespace   string

	client *kube.Ctl

	bootstrapUserSecret  *corev1.Secret
	statefulSetPodLabels map[string]string
	statefulSetSelector  map[string]string
}

// NewRenderState constructs a RenderState from a StretchCluster, its NodePools,
// and a cluster name. It uses the provided config for K8s lookups.
// The cluster and pools are deep-copied so that merging defaults does not
// mutate the caller's objects.
func NewRenderState(
	config *kube.RESTConfig,
	cluster *redpandav1alpha2.StretchCluster,
	pools []*redpandav1alpha2.NodePool,
	clusterName string,
) (*RenderState, error) {
	// Deep-copy to avoid mutating the caller's CRD objects.
	cluster = cluster.DeepCopy()
	copiedPools := make([]*redpandav1alpha2.NodePool, len(pools))
	for i, p := range pools {
		copiedPools[i] = p.DeepCopy()
	}

	// Sort pools by name for deterministic rendering order.
	sort.Slice(copiedPools, func(i, j int) bool {
		return copiedPools[i].Name < copiedPools[j].Name
	})

	// Apply Helm-equivalent defaults to nil fields.
	cluster.Spec.MergeDefaults()

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
		cluster:     cluster,
		pools:       copiedPools,
		clusterName: clusterName,
		releaseName: releaseName,
		namespace:   cluster.Namespace,
		client:      ctl,
	}

	if err := state.fetchBootstrapUser(); err != nil {
		return nil, err
	}
	if err := state.fetchStatefulSetPodSelector(); err != nil {
		return nil, err
	}

	return state, nil
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

// Pools returns the list of NodePools. Exported for test/debugging access.
func (r *RenderState) Pools() []*redpandav1alpha2.NodePool {
	return r.pools
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
func (r *RenderState) BrokerList(port int32) []string {
	var brokers []string
	for _, pool := range r.pools {
		for i := int32(0); i < pool.GetReplicas(); i++ {
			brokers = append(brokers, fmt.Sprintf("%s%s-%d.%s:%d",
				r.fullname(), pool.Suffix(), i, r.Spec().InternalDomain(r.fullname(), r.namespace), port))
		}
	}
	return brokers
}

func (r *RenderState) allPodNames() []string {
	var names []string
	for _, pool := range r.pools {
		for i := int32(0); i < pool.GetReplicas(); i++ {
			names = append(names, fmt.Sprintf("%s-%d", r.poolFullname(pool), i))
		}
	}
	return names
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

	secretName := fmt.Sprintf("%s-bootstrap-user", r.fullname())

	var existing corev1.Secret
	if err := r.client.Get(context.Background(), kube.ObjectKey{Namespace: r.namespace, Name: secretName}, &existing); err != nil {
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
