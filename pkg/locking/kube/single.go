// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package kube

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	coordinationv1client "k8s.io/client-go/kubernetes/typed/coordination/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

type singleLock struct {
	*resourcelock.LeaseLock
}

// newSingleResourceLock creates a new resource lock for use in a leader election loop.
func newSingleResourceLock(id string, config LockConfiguration, cnConfig ClusterNamespaceConfig) (resourcelock.Interface, error) {
	k8sConfig := rest.AddUserAgent(cnConfig.Config, "leader-election")

	if config.RenewDeadline != 0 {
		timeout := config.RenewDeadline / 2
		if timeout < time.Second {
			timeout = time.Second
		}
		k8sConfig.Timeout = timeout
	}

	coordinationClient, err := coordinationv1client.NewForConfig(k8sConfig)
	if err != nil {
		return nil, err
	}

	lockConfig := resourcelock.ResourceLockConfig{
		Identity: id,
	}

	if config.RecorderProvider != nil {
		lockConfig.EventRecorder = config.RecorderProvider.GetEventRecorderFor(id) //nolint:staticcheck
	}

	return &singleLock{LeaseLock: &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Namespace: cnConfig.Namespace,
			Name:      config.Name,
		},
		Client:     coordinationClient,
		LockConfig: lockConfig,
		Labels:     config.LeaderLabels,
	}}, nil
}
