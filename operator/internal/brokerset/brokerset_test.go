// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package brokerset

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

func TestRenderBrokersDataGuard(t *testing.T) {
	datadir := corev1.Volume{Name: "datadir", VolumeSource: corev1.VolumeSource{
		PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "datadir"},
	}}
	ephemeral := corev1.Volume{Name: "datadir", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}
	tuning := corev1.Container{Name: "tuning", Image: "redpanda:latest"}

	for _, tc := range []struct {
		name      string
		image     string
		volumes   []corev1.Volume
		wantGuard bool
	}{
		{name: "guard in front of the init containers", image: "operator:dev", volumes: []corev1.Volume{datadir}, wantGuard: true},
		{name: "no image, no guard", image: "", volumes: []corev1.Volume{datadir}},
		{name: "no datadir volume, no guard", image: "operator:dev", volumes: nil},
		{name: "ephemeral datadir, no guard", image: "operator:dev", volumes: []corev1.Volume{ephemeral}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, redpandav1alpha2.Install(scheme))

			sts := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{Name: "rp", Namespace: "test"},
				Spec: appsv1.StatefulSetSpec{
					ServiceName: "rp",
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}},
						Spec: corev1.PodSpec{
							InitContainers: []corev1.Container{tuning},
							Containers:     []corev1.Container{{Name: "redpanda", Image: "redpanda:latest"}},
							Volumes:        tc.volumes,
						},
					},
					VolumeClaimTemplates: []corev1.PersistentVolumeClaim{{ObjectMeta: metav1.ObjectMeta{Name: "datadir"}}},
				},
			}
			s := &BrokerSet{
				Owner:             &redpandav1alpha2.Redpanda{ObjectMeta: metav1.ObjectMeta{Name: "rp", Namespace: "test"}},
				ClusterRef:        redpandav1alpha2.ClusterRef{Name: "rp"},
				PoolName:          "default",
				BrokerLabels:      map[string]string{"app": "redpanda"},
				PoolSelector:      k8slabels.Everything(),
				ClusterSelector:   k8slabels.Everything(),
				PodSelector:       k8slabels.Everything(),
				ConfigChecksumKey: "checksum",
				DataGuardImage:    tc.image,
				Scheme:            scheme,
			}

			brokers, err := s.RenderBrokers(sts, 1, false)
			require.NoError(t, err)
			require.Len(t, brokers, 1)
			spec := brokers[0].Spec.PodTemplate.Spec

			if !tc.wantGuard {
				assert.Equal(t, []corev1.Container{tuning}, spec.InitContainers)
				assert.Len(t, spec.Volumes, len(tc.volumes), "no broker-id volume is added")
				return
			}

			require.Len(t, spec.InitContainers, 2)
			guard := spec.InitContainers[0]
			assert.Equal(t, DataGuardContainerName, guard.Name, "the guard runs before anything else touches the data dir")
			assert.Equal(t, tc.image, guard.Image)
			assert.Equal(t, tuning, spec.InitContainers[1])
			assert.Contains(t, guard.Command, "data-guard")
			assert.True(t, ptr.Deref(guard.SecurityContext.ReadOnlyRootFilesystem, false))

			mounts := map[string]corev1.VolumeMount{}
			for _, m := range guard.VolumeMounts {
				mounts[m.Name] = m
			}
			require.Contains(t, mounts, "datadir")
			assert.True(t, mounts["datadir"].ReadOnly, "the guard only reads the data dir")
			require.Contains(t, mounts, dataGuardVolumeName)
			assert.Equal(t, dataGuardMountPath, mounts[dataGuardVolumeName].MountPath)

			var downward *corev1.DownwardAPIVolumeSource
			for _, v := range spec.Volumes {
				if v.Name == dataGuardVolumeName {
					downward = v.DownwardAPI
				}
			}
			require.NotNil(t, downward, "the broker-id volume must exist")
			require.Len(t, downward.Items, 1)
			assert.Equal(t, dataGuardIDFile, downward.Items[0].Path)
			assert.Equal(t, "metadata.annotations['"+redpandav1alpha2.BrokerIDAnnotation+"']", downward.Items[0].FieldRef.FieldPath)
		})
	}
}
