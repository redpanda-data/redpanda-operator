// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_post-delete-finalizer-removal-job.go.tpl
package operator

import (
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

// PostDeleteFinalizerRemovalJob is a post-delete hook job that removes
// finalizers from all operator-managed CRs after uninstall. Running
// post-delete ensures the operator is already gone before finalizers are
// stripped, eliminating any race where the controller could re-add them.
func PostDeleteFinalizerRemovalJob(dot *helmette.Dot) *batchv1.Job {
	values := helmette.Unwrap[Values](dot.Values)

	if !values.FinalizerRemoval.Enabled {
		return nil
	}

	return &batchv1.Job{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "batch/v1",
			Kind:       "Job",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-finalizer-removal", Fullname(dot)),
			Namespace: dot.Release.Namespace,
			Labels:    helmette.Merge(Labels(dot)),
			Annotations: map[string]string{
				"helm.sh/hook":               "post-delete",
				"helm.sh/hook-delete-policy": "before-hook-creation,hook-succeeded,hook-failed",
				"helm.sh/hook-weight":        "-5",
			},
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: values.PodAnnotations,
					Labels:      helmette.Merge(SelectorLabels(dot), values.PodLabels),
				},
				Spec: corev1.PodSpec{
					RestartPolicy:                 corev1.RestartPolicyOnFailure,
					AutomountServiceAccountToken:  ptr.To(false),
					TerminationGracePeriodSeconds: ptr.To(int64(10)),
					ImagePullSecrets:              values.ImagePullSecrets,
					ServiceAccountName:            PostDeleteFinalizerRemovalJobServiceAccountName(dot),
					NodeSelector:                  values.NodeSelector,
					Tolerations:                   values.Tolerations,
					Volumes:                       []corev1.Volume{serviceAccountTokenVolume()},
					Containers:                    finalizerRemovalJobContainers(dot),
				},
			},
		},
	}
}

func finalizerRemovalJobContainers(dot *helmette.Dot) []corev1.Container {
	values := helmette.Unwrap[Values](dot.Values)

	return []corev1.Container{
		{
			Name:            "finalizer-removal",
			Image:           containerImage(dot),
			ImagePullPolicy: values.Image.PullPolicy,
			Command:         []string{"/redpanda-operator"},
			Args:            []string{"finalizer-removal"},
			SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: ptr.To(false)},
			VolumeMounts:    []corev1.VolumeMount{serviceAccountTokenVolumeMount()},
			Resources:       values.Resources,
		},
	}
}
