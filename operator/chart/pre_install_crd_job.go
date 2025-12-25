// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_post-install-crd-job.go.tpl
package operator

import (
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

// This is a pre-install job as the operator will crash loop without the CRDs
// which deadlocks helm install commands.
func PreInstallCRDJob(dot *helmette.Dot) *batchv1.Job {
	values := helmette.Unwrap[Values](dot.Values)

	if !(values.CRDs.Enabled || values.CRDs.Experimental) {
		return nil
	}

	return &batchv1.Job{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "batch/v1",
			Kind:       "Job",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-crds", Fullname(dot)),
			Namespace: dot.Release.Namespace,
			Labels: helmette.Merge(
				Labels(dot),
			),
			Annotations: map[string]string{
				"helm.sh/hook":               "pre-install,pre-upgrade",
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
					ServiceAccountName:            CRDJobServiceAccountName(dot),
					NodeSelector:                  values.NodeSelector,
					Tolerations:                   values.Tolerations,
					Volumes:                       []corev1.Volume{serviceAccountTokenVolume()},
					Containers:                    crdJobContainers(dot),
				},
			},
		},
	}
}

func crdJobContainers(dot *helmette.Dot) []corev1.Container {
	values := helmette.Unwrap[Values](dot.Values)

	args := []string{"crd"}
	if values.CRDs.Experimental {
		args = append(args, "--experimental")
	}

	if values.VectorizedControllers.Enabled {
		args = append(args, "--vectorized")
	}

	if values.Multicluster.Enabled {
		args = append(args, "--multicluster")
	}

	return []corev1.Container{
		{
			Name:            "crd-installation",
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
