// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda

import (
	"context"
	"fmt"
	"reflect"

	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

type clientList[T client.Object] interface {
	client.ObjectList
	GetItems() []T
}

func clusterReferenceIndexName(name string) string {
	return fmt.Sprintf("__%s_referencing_cluster", name)
}

func registerClusterSourceIndex[T client.Object, U clientList[T]](ctx context.Context, mgr ctrl.Manager, name string, o T, l U) (handler.EventHandler, error) {
	indexName := clusterReferenceIndexName(name)
	if err := mgr.GetFieldIndexer().IndexField(ctx, o, indexName, indexByClusterSource(func(cr *redpandav1alpha2.ClusterRef) bool {
		return cr.IsV2()
	})); err != nil {
		return nil, err
	}
	return enqueueFromSourceCluster(mgr, name, l), nil
}

func registerV1ClusterSourceIndex[T client.Object, U clientList[T]](ctx context.Context, mgr ctrl.Manager, name string, o T, l U) (handler.EventHandler, error) {
	indexName := clusterReferenceIndexName(name)
	if err := mgr.GetFieldIndexer().IndexField(ctx, o, indexName, indexByClusterSource(func(cr *redpandav1alpha2.ClusterRef) bool {
		return cr.IsV1()
	})); err != nil {
		return nil, err
	}
	return enqueueFromSourceCluster(mgr, name, l), nil
}

func indexByClusterSource(checkRef func(*redpandav1alpha2.ClusterRef) bool) func(o client.Object) []string {
	return func(o client.Object) []string {
		clusterReferencingObject := o.(redpandav1alpha2.ClusterReferencingObject)
		source := clusterReferencingObject.GetClusterSource()

		clusters := []string{}
		if source != nil && source.ClusterRef != nil && checkRef(source.ClusterRef) {
			cluster := types.NamespacedName{Namespace: clusterReferencingObject.GetNamespace(), Name: source.ClusterRef.Name}
			clusters = append(clusters, cluster.String())
		}

		if remoteClusterReferencingObject, ok := o.(redpandav1alpha2.RemoteClusterReferencingObject); ok {
			remoteSource := remoteClusterReferencingObject.GetRemoteClusterSource()
			if remoteSource != nil && remoteSource.ClusterRef != nil && checkRef(source.ClusterRef) {
				cluster := types.NamespacedName{Namespace: clusterReferencingObject.GetNamespace(), Name: remoteSource.ClusterRef.Name}
				clusters = append(clusters, cluster.String())
			}
		}

		return clusters
	}
}

func sourceClusters[T client.Object, U clientList[T]](ctx context.Context, c client.Client, list U, name string, nn types.NamespacedName) ([]reconcile.Request, error) {
	err := c.List(ctx, list, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(clusterReferenceIndexName(name), nn.String()),
	})
	if err != nil {
		return nil, err
	}

	requests := []reconcile.Request{}
	for _, item := range list.GetItems() { //nolint:gocritic // this is necessary
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: item.GetNamespace(),
				Name:      item.GetName(),
			},
		})
	}

	return requests, nil
}

func enqueueFromSourceCluster[T client.Object, U clientList[T]](mgr ctrl.Manager, name string, l U) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
		list := reflect.New(reflect.TypeOf(l).Elem()).Interface().(U)
		requests, err := sourceClusters(ctx, mgr.GetClient(), list, name, client.ObjectKeyFromObject(o))
		if err != nil {
			mgr.GetLogger().V(1).Info(fmt.Sprintf("possibly skipping %s reconciliation due to failure to fetch %s associated with cluster", name, name), "error", err)
			return nil
		}
		return requests
	})
}

func fromSourceCluster[T client.Object, U clientList[T]](ctx context.Context, c client.Client, name string, cluster *redpandav1alpha2.Redpanda, l U) ([]T, error) {
	list := reflect.New(reflect.TypeOf(l).Elem()).Interface().(U)
	err := c.List(ctx, list, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(clusterReferenceIndexName(name), client.ObjectKeyFromObject(cluster).String()),
	})
	if err != nil {
		return nil, err
	}

	return list.GetItems(), nil
}
