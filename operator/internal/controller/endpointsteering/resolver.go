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
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
)

// ErrUnknownCluster reports that no cluster named by a pod group exists in
// the namespace. Pods aligned to such a Service are excluded rather than
// held: with no cluster there is nothing to probe and nothing to preserve.
var ErrUnknownCluster = errors.New("no cluster found for pod group")

// Resolver maps a pod group -- the value of [ServiceAnnotation] on a
// Service, which names a cluster -- to that cluster's brokers.
type Resolver interface {
	// Resolve returns ErrUnknownCluster when no cluster named group exists
	// in namespace.
	Resolve(ctx context.Context, namespace, group string) (*internalclient.ClusterBrokers, error)
}

// ResolverFunc adapts a function into a [Resolver].
type ResolverFunc func(ctx context.Context, namespace, group string) (*internalclient.ClusterBrokers, error)

// Resolve implements [Resolver].
func (f ResolverFunc) Resolve(ctx context.Context, namespace, group string) (*internalclient.ClusterBrokers, error) {
	return f(ctx, namespace, group)
}

// Resolvers consults each resolver in turn and answers with the first that
// knows the group, so one controller serves whichever cluster kinds an
// operator runs.
type Resolvers []Resolver

// Resolve implements [Resolver].
func (rs Resolvers) Resolve(ctx context.Context, namespace, group string) (*internalclient.ClusterBrokers, error) {
	for _, r := range rs {
		brokers, err := r.Resolve(ctx, namespace, group)
		if errors.Is(err, ErrUnknownCluster) {
			continue
		}
		return brokers, err
	}
	return nil, ErrUnknownCluster
}

// clusterBrokers is the slice of [internalclient.Factory] the resolvers use.
type clusterBrokers interface {
	ClusterBrokers(ctx context.Context, obj any, clusterName string) (*internalclient.ClusterBrokers, error)
}

// V2Resolver resolves groups as v2 Redpanda clusters read through reader,
// which must be backed by an informer for the type (the manager's client in
// an operator running the Redpanda controllers).
func V2Resolver(reader client.Reader, factory clusterBrokers) Resolver {
	return clusterResolver[redpandav1alpha2.Redpanda](reader, factory)
}

// V1Resolver resolves groups as v1 (vectorized) Clusters.
func V1Resolver(reader client.Reader, factory clusterBrokers) Resolver {
	return clusterResolver[vectorizedv1alpha1.Cluster](reader, factory)
}

// StretchResolver resolves groups as StretchClusters, reading their broker
// pools from the local Kubernetes cluster -- the one whose pods this
// operator instance publishes.
func StretchResolver(reader client.Reader, factory clusterBrokers) Resolver {
	return clusterResolver[redpandav1alpha2.StretchCluster](reader, factory)
}

// clusterResolver reads the cluster of type T named by the pod group and asks
// factory for its brokers. A group naming no such cluster is not this
// resolver's business, which is what lets [Resolvers] try the next one.
func clusterResolver[T any, PT interface {
	*T
	client.Object
}](reader client.Reader, factory clusterBrokers) Resolver {
	return ResolverFunc(func(ctx context.Context, namespace, group string) (*internalclient.ClusterBrokers, error) {
		cluster := PT(new(T))
		if err := reader.Get(ctx, types.NamespacedName{Namespace: namespace, Name: group}, cluster); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, ErrUnknownCluster
			}
			return nil, errors.WithStack(err)
		}
		return factory.ClusterBrokers(ctx, cluster, mcmanager.LocalCluster)
	})
}
