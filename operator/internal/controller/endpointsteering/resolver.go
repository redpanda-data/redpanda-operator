// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package endpointsteering

import (
	"context"

	"github.com/cockroachdb/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/schemaregistry"
)

// ErrUnknownCluster reports that no cluster named by a pod group exists in
// the namespace. Pods aligned to such a Service are excluded rather than
// held: with no cluster there is nothing to probe and nothing to preserve.
var ErrUnknownCluster = errors.New("no cluster found for pod group")

// Cluster is the view of a Redpanda cluster a membership decision needs: how
// to tell its broker pods apart from everything else in the namespace, and
// the Schema Registry listener they serve.
//
// client.Factory.ClusterBrokers produces it. It is named here as an
// interface, rather than imported from that package, because pkg/client sits
// above the v1 and v2 renderers and the renderers import this package for
// [Steer] -- so importing it back would be a cycle.
type Cluster interface {
	// PodSelector is carried by every broker pod of the cluster, across node
	// pools; it is the cluster's own Service selector.
	PodSelector() map[string]string
	// ClusterDomain completes a broker pod's DNS record, or is empty when
	// the cluster kind records none and the operator's own applies.
	ClusterDomain() string
	// SchemaRegistry is nil when the cluster's listener is disabled.
	SchemaRegistry() *schemaregistry.Listener
}

// Resolver maps a pod group -- the value of [ServiceAnnotation] on a
// Service, which names a cluster -- to that cluster's brokers.
type Resolver interface {
	// Resolve returns ErrUnknownCluster when no cluster named group exists
	// in namespace. A nil error means a usable Cluster: neither a nil
	// interface nor one holding a nil pointer (see [BrokersOf]).
	Resolve(ctx context.Context, namespace, group string) (Cluster, error)
}

// ResolverFunc adapts a function into a [Resolver].
type ResolverFunc func(ctx context.Context, namespace, group string) (Cluster, error)

// Resolve implements [Resolver].
func (f ResolverFunc) Resolve(ctx context.Context, namespace, group string) (Cluster, error) {
	return f(ctx, namespace, group)
}

// Resolvers consults each resolver in turn and answers with the first that
// knows the group, so one controller serves whichever cluster kinds an
// operator runs.
type Resolvers []Resolver

// Resolve implements [Resolver].
func (rs Resolvers) Resolve(ctx context.Context, namespace, group string) (Cluster, error) {
	for _, r := range rs {
		brokers, err := r.Resolve(ctx, namespace, group)
		if errors.Is(err, ErrUnknownCluster) {
			continue
		}
		return brokers, err
	}
	return nil, ErrUnknownCluster
}

// BrokersFunc resolves a cluster object to its brokers.
// client.Factory.ClusterBrokers is the implementation; it is passed in rather
// than imported for the reason [Cluster] gives. It must return a nil Cluster
// on failure -- a typed-nil pointer in a non-nil interface would read as a
// resolved cluster.
type BrokersFunc func(ctx context.Context, cluster any, clusterName string) (Cluster, error)

// BrokersOf adapts a resolver of concrete brokers -- Factory.ClusterBrokers
// -- to a [BrokersFunc]. It exists so the nil is written once: returning a
// failed lookup's typed-nil pointer straight into the interface would make a
// non-nil Cluster that panics on first use.
func BrokersOf[T Cluster](resolve func(ctx context.Context, cluster any, clusterName string) (T, error)) BrokersFunc {
	return func(ctx context.Context, cluster any, clusterName string) (Cluster, error) {
		brokers, err := resolve(ctx, cluster, clusterName)
		if err != nil {
			return nil, err
		}
		return brokers, nil
	}
}

// V2Resolver resolves groups as v2 Redpanda clusters read through reader,
// which must be backed by an informer for the type (the manager's client in
// an operator running the Redpanda controllers).
func V2Resolver(reader client.Reader, brokers BrokersFunc) Resolver {
	return clusterResolver[redpandav1alpha2.Redpanda](reader, brokers)
}

// V1Resolver resolves groups as v1 (vectorized) Clusters.
func V1Resolver(reader client.Reader, brokers BrokersFunc) Resolver {
	return clusterResolver[vectorizedv1alpha1.Cluster](reader, brokers)
}

// StretchResolver resolves groups as StretchClusters, reading their broker
// pools from the local Kubernetes cluster -- the one whose pods this
// operator instance publishes.
func StretchResolver(reader client.Reader, brokers BrokersFunc) Resolver {
	return clusterResolver[redpandav1alpha2.StretchCluster](reader, brokers)
}

// clusterResolver reads the cluster of type T named by the pod group and asks
// brokers for its brokers. A group naming no such cluster is not this
// resolver's business, which is what lets [Resolvers] try the next one.
func clusterResolver[T any, PT interface {
	*T
	client.Object
}](reader client.Reader, brokers BrokersFunc) Resolver {
	return ResolverFunc(func(ctx context.Context, namespace, group string) (Cluster, error) {
		cluster := PT(new(T))
		if err := reader.Get(ctx, types.NamespacedName{Namespace: namespace, Name: group}, cluster); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, ErrUnknownCluster
			}
			return nil, errors.WithStack(err)
		}
		return brokers(ctx, cluster, mcmanager.LocalCluster)
	})
}
