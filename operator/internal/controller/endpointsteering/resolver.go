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

// ErrUnknownCluster reports that the namespace holds no cluster of that
// name. Pods paired with such a Service are excluded rather than held: with
// no cluster there is nothing to probe.
var ErrUnknownCluster = errors.New("no cluster found for pod group")

// Cluster is what a membership decision needs to know about a Redpanda
// cluster: how to tell its broker pods from everything else in the
// namespace, and the Schema Registry listener they serve.
//
// NB: client.Factory.ClusterBrokers produces it, but is named here as an
// interface rather than imported: pkg/client sits above the v1 and v2
// renderers, and those renderers import this package for [Steer].
type Cluster interface {
	// PodSelector is the cluster's own Service selector, carried by every
	// one of its broker pods across node pools.
	PodSelector() map[string]string
	// ClusterDomain completes a broker pod's DNS record, or is empty when
	// the cluster records none and the operator's own applies.
	ClusterDomain() string
	// SchemaRegistry is the listener to probe, nil when the cluster's
	// internal listener is disabled.
	SchemaRegistry() *schemaregistry.Listener
	// SchemaRegistryPorts is every port a broker serves the registry on,
	// internal and external alike. One registry backs them all, so the one
	// probe decides the lot.
	SchemaRegistryPorts() []int32
}

// Resolver maps a pod group -- the cluster named by [ServiceAnnotation] on a
// Service -- to that cluster's brokers.
type Resolver interface {
	// Resolve returns ErrUnknownCluster when namespace holds no cluster
	// named group. A nil error means a usable Cluster: neither a nil
	// interface nor one holding a nil pointer (see [BrokersOf]).
	Resolve(ctx context.Context, namespace, group string) (Cluster, error)
}

// ResolverFunc adapts a function into a [Resolver].
type ResolverFunc func(ctx context.Context, namespace, group string) (Cluster, error)

// Resolve implements [Resolver].
func (f ResolverFunc) Resolve(ctx context.Context, namespace, group string) (Cluster, error) {
	return f(ctx, namespace, group)
}

// Resolvers answers with the first resolver that knows the group, so one
// controller serves whichever cluster kinds an operator runs.
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
// client.Factory.ClusterBrokers is the implementation, passed in rather than
// imported for the reason [Cluster] gives. It must return a nil Cluster on
// failure.
type BrokersFunc func(ctx context.Context, cluster any, clusterName string) (Cluster, error)

// BrokersOf adapts a resolver of concrete brokers -- Factory.ClusterBrokers
// -- to a [BrokersFunc].
//
// NB: it exists so the nil is written once. A failed lookup's typed-nil
// pointer put straight into the interface reads as a resolved cluster and
// panics on first use.
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
// which needs an informer for the type -- the manager's client in an
// operator running the Redpanda controllers.
func V2Resolver(reader client.Reader, brokers BrokersFunc) Resolver {
	return clusterResolver[redpandav1alpha2.Redpanda](reader, brokers)
}

// V1Resolver resolves groups as v1 (vectorized) Clusters.
func V1Resolver(reader client.Reader, brokers BrokersFunc) Resolver {
	return clusterResolver[vectorizedv1alpha1.Cluster](reader, brokers)
}

// clusterResolver reads the cluster of type T named by the group and asks
// brokers for its brokers. A group naming no such cluster is not this
// resolver's business, which lets [Resolvers] try the next one.
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

// withClusterDomain fills a resolved cluster's empty domain in with the
// operator's own, so a membership decision never has to know there was a
// default. A v1 Cluster records no domain of its own.
func withClusterDomain(resolver Resolver, domain string) Resolver {
	if domain == "" {
		return resolver
	}
	return ResolverFunc(func(ctx context.Context, namespace, group string) (Cluster, error) {
		cluster, err := resolver.Resolve(ctx, namespace, group)
		// A resolver answering with neither a cluster nor an error breaks
		// [Resolver]'s contract; pass it through for the checker to turn into
		// a failed lookup rather than dereferencing it here.
		if err != nil || cluster == nil || cluster.ClusterDomain() != "" {
			return cluster, err
		}
		return defaultedDomain{Cluster: cluster, domain: domain}, nil
	})
}

// defaultedDomain is a [Cluster] answering with the operator's domain.
type defaultedDomain struct {
	Cluster
	domain string
}

func (d defaultedDomain) ClusterDomain() string { return d.domain }
