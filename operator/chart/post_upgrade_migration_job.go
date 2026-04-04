// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_post-upgrade-migration-job.go.tpl
package operator

import (
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

// This is a post-upgrade job to make sure it just runs once.
func PostUpgradeMigrationJob(dot *helmette.Dot) *batchv1.Job {
	values := helmette.Unwrap[Values](dot.Values)

	return &batchv1.Job{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "batch/v1",
			Kind:       "Job",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-migration", Fullname(dot)),
			Namespace: dot.Release.Namespace,
			Labels: helmette.Merge(
				Labels(dot),
			),
			Annotations: map[string]string{
				"helm.sh/hook":               "post-upgrade",
				"helm.sh/hook-delete-policy": "before-hook-creation,hook-succeeded,hook-failed",
				// run this after the CRD job
				"helm.sh/hook-weight": "-4",
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
					ServiceAccountName:            MigrationJobServiceAccountName(dot),
					NodeSelector:                  values.NodeSelector,
					Tolerations:                   values.Tolerations,
					Volumes:                       []corev1.Volume{serviceAccountTokenVolume()},
					Containers:                    migrationJobContainers(dot),
				},
			},
		},
	}
}

func migrationJobContainers(dot *helmette.Dot) []corev1.Container {
	values := helmette.Unwrap[Values](dot.Values)

	args := []string{"migration"}

	return []corev1.Container{
		{
			Name:            "migration",
			Image:           containerImage(dot),
			ImagePullPolicy: values.Image.PullPolicy,
			Command:         []string{"/redpanda-operator"},
			Args:            args,
			SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: ptr.To(false)},
			VolumeMounts:    []corev1.VolumeMount{serviceAccountTokenVolumeMount()},
			Resources:       values.Resources,
		},
	}
}
