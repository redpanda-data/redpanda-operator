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

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

const (
	nodepoolStatefulSetIndex = "__nodepool_referencing_statefulset"
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
	if err := mgr.GetFieldIndexer().IndexField(ctx, o, indexName, indexByClusterSource); err != nil {
		return nil, err
	}
	return enqueueFromSourceCluster(mgr, name, l), nil
}

func registerPoolStatefulset(ctx context.Context, mgr ctrl.Manager) (handler.EventHandler, error) {
	if err := mgr.GetFieldIndexer().IndexField(ctx, &appsv1.StatefulSet{}, nodepoolStatefulSetIndex, indexByNodePoolLabels); err != nil {
		return nil, err
	}
	return enqueueNodePoolFromStatefulSet(mgr), nil
}

func indexByNodePoolLabels(o client.Object) []string {
	labels := o.GetLabels()
	if labels != nil {
		nodepool := types.NamespacedName{Namespace: labels[redpanda.NodePoolLabelNamespace], Name: labels[redpanda.NodePoolLabelName]}
		if nodepool.Name != "" && nodepool.Namespace != "" {
			return []string{nodepool.String()}
		}
	}
	return nil
}

func indexByClusterSource(o client.Object) []string {
	clusterReferencingObject := o.(redpandav1alpha2.ClusterReferencingObject)
	source := clusterReferencingObject.GetClusterSource()

	clusters := []string{}
	if source != nil && source.ClusterRef != nil {
		cluster := types.NamespacedName{Namespace: clusterReferencingObject.GetNamespace(), Name: source.ClusterRef.Name}
		clusters = append(clusters, cluster.String())
	}

	return clusters
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

func sourceNodePoolStatefulSet(ctx context.Context, c client.Client, nn types.NamespacedName) ([]reconcile.Request, error) {
	pool := &redpandav1alpha2.NodePool{}
	if err := c.Get(ctx, nn, pool); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	return []reconcile.Request{{NamespacedName: client.ObjectKeyFromObject(pool)}}, nil
}

func statefulSetForNodePool(ctx context.Context, c client.Client, pool *redpandav1alpha2.NodePool) (*appsv1.StatefulSet, error) {
	nn := client.ObjectKeyFromObject(pool).String()
	var list appsv1.StatefulSetList
	err := c.List(ctx, &list, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(nodepoolStatefulSetIndex, nn),
	})
	if err != nil {
		return nil, err
	}

	if len(list.Items) > 1 {
		return nil, fmt.Errorf("node pool %q maps to multiple(%d) StatefulSets", nn, len(list.Items))
	}

	if len(list.Items) == 0 {
		return nil, nil
	}

	return &list.Items[0], nil
}

func enqueueNodePoolFromStatefulSet(mgr ctrl.Manager) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
		labels := o.GetLabels()
		if labels == nil {
			return nil
		}
		nn := types.NamespacedName{Namespace: labels[redpanda.NodePoolLabelNamespace], Name: labels[redpanda.NodePoolLabelName]}
		if nn.Name == "" || nn.Namespace == "" {
			return nil
		}
		requests, err := sourceNodePoolStatefulSet(ctx, mgr.GetClient(), nn)
		if err != nil {
			mgr.GetLogger().V(1).Info("possibly skipping NodePool reconciliation due to failure to fetch associated StatefulSets", "error", err)
			return nil
		}
		return requests
	})
}
