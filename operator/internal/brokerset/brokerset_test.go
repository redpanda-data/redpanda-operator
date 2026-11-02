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

	redpandachart "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/chart"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

func TestRenderBrokersDetectDiskLost(t *testing.T) {
	datadir := corev1.Volume{Name: "datadir", VolumeSource: corev1.VolumeSource{
		PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "datadir"},
	}}
	ephemeral := corev1.Volume{Name: "datadir", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}
	tuning := corev1.Container{Name: "tuning", Image: "redpanda:latest"}
	const defaultDataDir = "/var/lib/redpanda/data"

	// The guard's own hardening, plus whatever user Redpanda runs as.
	baseline := &corev1.SecurityContext{
		AllowPrivilegeEscalation: ptr.To(false),
		ReadOnlyRootFilesystem:   ptr.To(true),
		RunAsNonRoot:             ptr.To(true),
	}
	asRedpanda := &corev1.SecurityContext{RunAsUser: ptr.To[int64](101), RunAsGroup: ptr.To[int64](101), RunAsNonRoot: ptr.To(true)}
	guardAsRedpanda := baseline.DeepCopy()
	guardAsRedpanda.RunAsUser, guardAsRedpanda.RunAsGroup = ptr.To[int64](101), ptr.To[int64](101)
	asRoot := &corev1.SecurityContext{RunAsUser: ptr.To[int64](0), RunAsNonRoot: ptr.To(false)}
	guardAsRoot := baseline.DeepCopy()
	guardAsRoot.RunAsUser, guardAsRoot.RunAsNonRoot = ptr.To[int64](0), ptr.To(false)

	// redpandaContainer builds the container the guard mirrors; an empty
	// mountPath leaves the data volume unmounted.
	redpandaContainer := func(sc *corev1.SecurityContext, mountPath string) *corev1.Container {
		c := &corev1.Container{Name: redpandachart.RedpandaContainerName, Image: "redpanda:latest", SecurityContext: sc}
		if mountPath != "" {
			c.VolumeMounts = []corev1.VolumeMount{{Name: "datadir", MountPath: mountPath}}
		}
		return c
	}
	// A sidecar that also writes the data volume and runs as root: never
	// the guard's model.
	sidecar := corev1.Container{
		Name: "sidecar", Image: "operator:dev", SecurityContext: asRoot,
		VolumeMounts: []corev1.VolumeMount{{Name: "datadir", MountPath: "/elsewhere"}},
	}

	for _, tc := range []struct {
		name    string
		image   string
		volumes []corev1.Volume
		// redpanda is the redpanda container; nil leaves it out of the pod.
		redpanda *corev1.Container
		// extra containers ride along with the redpanda one.
		extra       []corev1.Container
		wantGuard   bool
		wantSC      *corev1.SecurityContext
		wantDataDir string
	}{
		{name: "guard in front of the init containers", image: "operator:dev", volumes: []corev1.Volume{datadir}, redpanda: redpandaContainer(nil, defaultDataDir), wantGuard: true, wantSC: baseline, wantDataDir: defaultDataDir},
		{name: "guard runs as the redpanda user", image: "operator:dev", volumes: []corev1.Volume{datadir}, redpanda: redpandaContainer(asRedpanda, defaultDataDir), wantGuard: true, wantSC: guardAsRedpanda, wantDataDir: defaultDataDir},
		{name: "a root redpanda is mirrored, not second-guessed", image: "operator:dev", volumes: []corev1.Volume{datadir}, redpanda: redpandaContainer(asRoot, defaultDataDir), wantGuard: true, wantSC: guardAsRoot, wantDataDir: defaultDataDir},
		{name: "guard mounts the data volume where redpanda does", image: "operator:dev", volumes: []corev1.Volume{datadir}, redpanda: redpandaContainer(asRedpanda, "/mnt/redpanda"), wantGuard: true, wantSC: guardAsRedpanda, wantDataDir: "/mnt/redpanda"},
		{name: "other containers are ignored", image: "operator:dev", volumes: []corev1.Volume{datadir}, redpanda: redpandaContainer(asRedpanda, defaultDataDir), extra: []corev1.Container{sidecar}, wantGuard: true, wantSC: guardAsRedpanda, wantDataDir: defaultDataDir},
		{name: "no image, no guard", image: "", volumes: []corev1.Volume{datadir}, redpanda: redpandaContainer(nil, defaultDataDir)},
		{name: "no datadir volume, no guard", image: "operator:dev", volumes: nil, redpanda: redpandaContainer(nil, defaultDataDir)},
		{name: "ephemeral datadir, no guard", image: "operator:dev", volumes: []corev1.Volume{ephemeral}, redpanda: redpandaContainer(nil, defaultDataDir)},
		{name: "no redpanda container, no guard", image: "operator:dev", volumes: []corev1.Volume{datadir}, extra: []corev1.Container{sidecar}},
		{name: "redpanda without a data mount, no guard", image: "operator:dev", volumes: []corev1.Volume{datadir}, redpanda: redpandaContainer(nil, "")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, redpandav1alpha2.Install(scheme))

			var containers []corev1.Container
			if tc.redpanda != nil {
				containers = append(containers, *tc.redpanda)
			}
			containers = append(containers, tc.extra...)

			sts := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{Name: "rp", Namespace: "test"},
				Spec: appsv1.StatefulSetSpec{
					ServiceName: "rp",
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}},
						Spec: corev1.PodSpec{
							InitContainers: []corev1.Container{tuning},
							Containers:     containers,
							Volumes:        tc.volumes,
						},
					},
					VolumeClaimTemplates: []corev1.PersistentVolumeClaim{{ObjectMeta: metav1.ObjectMeta{Name: "datadir"}}},
				},
			}
			s := &BrokerSet{
				Owner:               &redpandav1alpha2.Redpanda{ObjectMeta: metav1.ObjectMeta{Name: "rp", Namespace: "test"}},
				ClusterRef:          redpandav1alpha2.ClusterRef{Name: "rp"},
				PoolName:            "default",
				BrokerLabels:        map[string]string{"app": "redpanda"},
				PoolSelector:        k8slabels.Everything(),
				ClusterSelector:     k8slabels.Everything(),
				PodSelector:         k8slabels.Everything(),
				ConfigChecksumKey:   "checksum",
				DetectDiskLostImage: tc.image,
				Scheme:              scheme,
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
			assert.Equal(t, DetectDiskLostContainerName, guard.Name, "the guard runs before anything else touches the data dir")
			assert.Equal(t, tc.image, guard.Image)
			assert.Equal(t, tuning, spec.InitContainers[1])
			assert.Contains(t, guard.Command, "detect-disk-lost")
			assert.Equal(t, []string{"--data-dir", tc.wantDataDir}, guard.Args, "the guard judges the directory at the path Redpanda uses")
			assert.Equal(t, tc.wantSC, guard.SecurityContext)

			mounts := map[string]corev1.VolumeMount{}
			for _, m := range guard.VolumeMounts {
				mounts[m.Name] = m
			}
			require.Contains(t, mounts, "datadir")
			assert.Equal(t, tc.wantDataDir, mounts["datadir"].MountPath, "the guard mounts the data volume where Redpanda does")
			assert.True(t, mounts["datadir"].ReadOnly, "the guard only reads the data dir")
			require.Contains(t, mounts, detectDiskLostVolumeName)
			assert.Equal(t, detectDiskLostMountPath, mounts[detectDiskLostVolumeName].MountPath)

			var downward *corev1.DownwardAPIVolumeSource
			for _, v := range spec.Volumes {
				if v.Name == detectDiskLostVolumeName {
					downward = v.DownwardAPI
				}
			}
			require.NotNil(t, downward, "the broker-id volume must exist")
			require.Len(t, downward.Items, 1)
			assert.Equal(t, detectDiskLostIDFile, downward.Items[0].Path)
			assert.Equal(t, "metadata.annotations['"+redpandav1alpha2.BrokerIDAnnotation+"']", downward.Items[0].FieldRef.FieldPath)
		})
	}
}
