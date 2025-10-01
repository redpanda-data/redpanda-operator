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
	"reflect"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redpandav1alpha2ac "github.com/redpanda-data/redpanda-operator/operator/api/applyconfiguration/redpanda/v1alpha2"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/kubernetes"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/shadow"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/functional"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/utils"
)

//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=shadowlinks,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=shadowlinks/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=shadowlinks/finalizers,verbs=update

// ShadowLinkReconciler reconciles a ShadowLink object
type ShadowLinkReconciler struct{}

func (r *ShadowLinkReconciler) FinalizerPatch(request ResourceRequest[*redpandav1alpha2.ShadowLink]) client.Patch {
	shadowLink := request.object
	config := redpandav1alpha2ac.ShadowLink(shadowLink.Name, shadowLink.Namespace)
	return kubernetes.ApplyPatch(config.WithFinalizers(FinalizerKey))
}

func (r *ShadowLinkReconciler) SyncResource(ctx context.Context, request ResourceRequest[*redpandav1alpha2.ShadowLink]) (client.Patch, error) {
	shadowLink := request.object

	createPatch := func(err error, state redpandav1alpha2.ShadowLinkState, tasks []redpandav1alpha2.ShadowLinkTaskStatus, topics []redpandav1alpha2.ShadowTopicStatus) (client.Patch, error) {
		var syncCondition metav1.Condition
		config := redpandav1alpha2ac.ShadowLink(shadowLink.Name, shadowLink.Namespace)

		if err != nil {
			syncCondition, err = handleResourceSyncErrors(err)
		} else {
			syncCondition = redpandav1alpha2.ResourceSyncedCondition(shadowLink.Name)
		}

		return kubernetes.ApplyPatch(config.WithStatus(redpandav1alpha2ac.ShadowLinkStatus().
			WithState(state).
			WithShadowTopicStatuses(ShadowTopicStatusesToConfigs(shadowLink.Status.ShadowTopicStatuses, topics)...).
			WithTaskStatuses(ShadowLinkTaskStatusesToConfigs(shadowLink.Status.TaskStatuses, tasks)...).
			WithConditions(utils.StatusConditionConfigs(shadowLink.Status.Conditions, shadowLink.Generation, []metav1.Condition{
				syncCondition,
			})...))), err
	}

	state := shadowLink.Status.State
	tasks := shadowLink.Status.TaskStatuses
	topics := shadowLink.Status.ShadowTopicStatuses
	syncer, err := request.factory.ShadowLinks(ctx, shadowLink)
	if err != nil {
		return createPatch(err, state, tasks, topics)
	}

	// TODO: map the cluster ref to RemoteClusterSettings
	status, err := syncer.Sync(ctx, shadowLink, shadow.RemoteClusterSettings{})
	return createPatch(err, status.State, status.TaskStatuses, status.ShadowTopicStatuses)
}

func (r *ShadowLinkReconciler) DeleteResource(ctx context.Context, request ResourceRequest[*redpandav1alpha2.ShadowLink]) error {
	syncer, err := request.factory.ShadowLinks(ctx, request.object)
	if err != nil {
		return ignoreAllConnectionErrors(request.logger, err)
	}
	if err := syncer.Delete(ctx, request.object); err != nil {
		return ignoreAllConnectionErrors(request.logger, err)
	}

	return nil
}

func SetupShadowLinkController(ctx context.Context, mgr ctrl.Manager, includeV1 bool) error {
	c := mgr.GetClient()
	config := mgr.GetConfig()
	factory := internalclient.NewFactory(config, c)
	controller := NewResourceController(c, factory, &ShadowLinkReconciler{}, "ShadowLinkReconciler")

	builder := ctrl.NewControllerManagedBy(mgr).
		For(&redpandav1alpha2.ShadowLink{})

	if includeV1 {
		enqueueV1ShadowLink, err := registerV1ClusterSourceIndex(ctx, mgr, "shadow_link_v1", &redpandav1alpha2.ShadowLink{}, &redpandav1alpha2.ShadowLinkList{})
		if err != nil {
			return err
		}
		builder.Watches(&vectorizedv1alpha1.Cluster{}, enqueueV1ShadowLink)
	}

	enqueueV2ShadowLink, err := registerClusterSourceIndex(ctx, mgr, "shadow_link", &redpandav1alpha2.ShadowLink{}, &redpandav1alpha2.ShadowLinkList{})
	if err != nil {
		return err
	}
	builder.Watches(&redpandav1alpha2.Redpanda{}, enqueueV2ShadowLink)

	// Every 5 minutes try and check to make sure no manual modifications
	// happened on the resource synced to the cluster and attempt to correct
	// any drift.
	return builder.Complete(controller.PeriodicallyReconcile(5 * time.Minute))
}

func ShadowLinkTaskStatusesToConfigs(existing, updated []redpandav1alpha2.ShadowLinkTaskStatus) []*redpandav1alpha2ac.ShadowLinkTaskStatusApplyConfiguration {
	now := metav1.Now()
	tasks := []*redpandav1alpha2ac.ShadowLinkTaskStatusApplyConfiguration{}

	findStatus := func(status redpandav1alpha2.ShadowLinkTaskStatus) *redpandav1alpha2.ShadowLinkTaskStatus {
		for _, o := range existing {
			if o.Name == status.Name {
				return &o
			}
		}
		return nil
	}

	for _, task := range updated {
		existingTask := findStatus(task)
		if existingTask == nil {
			tasks = append(tasks, shadowLinkTaskStatusToConfig(now, task))
			continue
		}

		if existingTask.State != task.State {
			tasks = append(tasks, shadowLinkTaskStatusToConfig(now, task))
			continue
		}

		if existingTask.Reason != task.Reason {
			tasks = append(tasks, shadowLinkTaskStatusToConfig(now, task))
			continue
		}

		if existingTask.BrokerID != task.BrokerID {
			tasks = append(tasks, shadowLinkTaskStatusToConfig(now, task))
			continue
		}

		tasks = append(tasks, shadowLinkTaskStatusToConfig(existingTask.LastTransitionTime, *existingTask))
	}

	return tasks
}

func shadowLinkTaskStatusToConfig(now metav1.Time, task redpandav1alpha2.ShadowLinkTaskStatus) *redpandav1alpha2ac.ShadowLinkTaskStatusApplyConfiguration {
	return redpandav1alpha2ac.ShadowLinkTaskStatus().
		WithName(task.Name).
		WithState(task.State).
		WithReason(task.Reason).
		WithBrokerID(task.BrokerID).
		WithLastTransitionTime(now)
}

func ShadowTopicStatusesToConfigs(existing, updated []redpandav1alpha2.ShadowTopicStatus) []*redpandav1alpha2ac.ShadowTopicStatusApplyConfiguration {
	now := metav1.Now()
	topics := []*redpandav1alpha2ac.ShadowTopicStatusApplyConfiguration{}

	findStatus := func(status redpandav1alpha2.ShadowTopicStatus) *redpandav1alpha2.ShadowTopicStatus {
		for _, o := range existing {
			if o.Name == status.Name && o.TopicID == status.TopicID {
				return &o
			}
		}
		return nil
	}

OUTER:
	for _, topic := range updated {
		existingTopic := findStatus(topic)
		if existingTopic == nil {
			topics = append(topics, shadowTopicStatusToConfig(now, topic))
			continue
		}

		if existingTopic.State != topic.State {
			topics = append(topics, shadowTopicStatusToConfig(now, topic))
			continue
		}

		if len(existingTopic.PartitionInformation) != len(topic.PartitionInformation) {
			topics = append(topics, shadowTopicStatusToConfig(now, topic))
			continue
		}

		for i, updatedPartition := range topic.PartitionInformation {
			if !reflect.DeepEqual(updatedPartition, existingTopic.PartitionInformation[i]) {
				continue OUTER
			}
		}

		topics = append(topics, shadowTopicStatusToConfig(existingTopic.LastTransitionTime, *existingTopic))
	}

	return topics
}

func shadowTopicStatusToConfig(now metav1.Time, topic redpandav1alpha2.ShadowTopicStatus) *redpandav1alpha2ac.ShadowTopicStatusApplyConfiguration {
	return redpandav1alpha2ac.ShadowTopicStatus().
		WithName(topic.Name).
		WithTopicID(topic.TopicID).
		WithState(topic.State).
		WithPartitionInformation(functional.MapFn(func(info redpandav1alpha2.TopicPartitionInformation) *redpandav1alpha2ac.TopicPartitionInformationApplyConfiguration {
			return redpandav1alpha2ac.TopicPartitionInformation().
				WithPartitionID(info.PartitionID).
				WithSourceLastStableOffset(info.SourceLastStableOffset).
				WithSourceHighWatermark(info.SourceHighWatermark).
				WithHighWatermark(info.HighWatermark)
		}, topic.PartitionInformation)...).
		WithLastTransitionTime(now)
}
