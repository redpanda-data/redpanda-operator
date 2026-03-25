// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package multicluster

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

func statefulSetContainerSidecar(state *RenderState, pool *redpandav1alpha2.NodePool) corev1.Container {
	image := pool.SidecarImage()

	args := []string{
		`/redpanda-operator`,
		`sidecar`,
		`--redpanda-yaml`, redpandaConfigMountPath + `/redpanda.yaml`,
		`--redpanda-cluster-namespace`, state.namespace,
		`--redpanda-cluster-name`, state.fullname(),
		fmt.Sprintf("--selector=%s=%s,%s=%s", labelNameKey, labelNameValue, labelInstanceKey, state.releaseName),
		`--run-broker-probe`,
		`--broker-probe-broker-url`,
		fmt.Sprintf("$(SERVICE_NAME).%s:%d", state.Spec().InternalDomain(state.fullname(), state.namespace), state.Spec().AdminPort()),
	}

	volumeMounts := append(
		state.commonMounts(),
		corev1.VolumeMount{Name: configVolumeName, MountPath: redpandaConfigMountPath},
		corev1.VolumeMount{Name: serviceAccountVolumeName, MountPath: defaultAPITokenMountPath, ReadOnly: true},
	)

	return corev1.Container{
		Name:         sidecarContainerName,
		Image:        image,
		Command:      []string{`/redpanda-operator`},
		Args:         append([]string{`supervisor`, `--`}, args...),
		Env:          statefulSetRedpandaEnv(),
		VolumeMounts: volumeMounts,
		SecurityContext: &corev1.SecurityContext{
			RunAsNonRoot:             ptr.To(true),
			AllowPrivilegeEscalation: ptr.To(false),
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: sidecarHealthPath,
					Port: intstr.FromInt32(sidecarHealthPort),
				},
			},
			FailureThreshold:    3,
			InitialDelaySeconds: 1,
			PeriodSeconds:       10,
			SuccessThreshold:    1,
		},
	}
}
