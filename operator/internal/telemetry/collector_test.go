// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package telemetry

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apimachineryruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/redpanda-data/redpanda-operator/operator/api/apiutil"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
)

// fakeServerVersion is a discovery.ServerVersionInterface stub for tests.
type fakeServerVersion struct {
	info *version.Info
	err  error
}

func (f fakeServerVersion) ServerVersion() (*version.Info, error) { return f.info, f.err }

func testScheme(t *testing.T) *apimachineryruntime.Scheme {
	t.Helper()
	scheme := apimachineryruntime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, apiextensionsv1.AddToScheme(scheme))
	require.NoError(t, redpandav1alpha2.Install(scheme))
	require.NoError(t, vectorizedv1alpha1.Install(scheme))
	return scheme
}

func TestCollect_PopulatedCluster(t *testing.T) {
	scheme := testScheme(t)

	objs := []client.Object{
		&redpandav1alpha2.NodePool{
			ObjectMeta: metav1.ObjectMeta{Name: "np-1", Namespace: "default"},
			Spec: redpandav1alpha2.NodePoolSpec{
				// ClusterRef ties this pool to rp-1 so it inherits rp-1's
				// per-broker resources for sizing.
				ClusterRef:           redpandav1alpha2.ClusterRef{Name: "rp-1"},
				EmbeddedNodePoolSpec: redpandav1alpha2.EmbeddedNodePoolSpec{Replicas: ptr.To(int32(2))},
			},
		},
		&redpandav1alpha2.StretchCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "sc-1", Namespace: "default"},
		},
		&redpandav1alpha2.RedpandaBrokerPool{
			ObjectMeta: metav1.ObjectMeta{Name: "bp-1", Namespace: "default"},
			Spec: redpandav1alpha2.BrokerPoolSpec{
				EmbeddedBrokerPoolSpec: redpandav1alpha2.EmbeddedBrokerPoolSpec{
					Replicas: ptr.To(int32(3)),
					Resources: &redpandav1alpha2.StretchResources{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("8"),
							corev1.ResourceMemory: resource.MustParse("32Gi"),
						},
					},
				},
			},
		},
		&redpandav1alpha2.Topic{
			ObjectMeta: metav1.ObjectMeta{Name: "topic-1", Namespace: "default"},
		},
		&redpandav1alpha2.User{ObjectMeta: metav1.ObjectMeta{Name: "user-1", Namespace: "default"}},
		&redpandav1alpha2.Schema{ObjectMeta: metav1.ObjectMeta{Name: "schema-1", Namespace: "default"}},
		&redpandav1alpha2.RedpandaRole{ObjectMeta: metav1.ObjectMeta{Name: "role-1", Namespace: "default"}},
		&redpandav1alpha2.ShadowLink{ObjectMeta: metav1.ObjectMeta{Name: "sl-1", Namespace: "default"}},
		&redpandav1alpha2.Console{ObjectMeta: metav1.ObjectMeta{Name: "console-1", Namespace: "default"}},
		&vectorizedv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "v1-1", Namespace: "default"},
			Spec: vectorizedv1alpha1.ClusterSpec{
				Replicas: ptr.To(int32(1)),
				Resources: vectorizedv1alpha1.RedpandaResourceRequirements{
					ResourceRequirements: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("2"),
							corev1.ResourceMemory: resource.MustParse("8Gi"),
						},
					},
				},
			},
		},
		&redpandav1alpha2.Redpanda{
			ObjectMeta: metav1.ObjectMeta{Name: "rp-1", Namespace: "default"},
			Spec: redpandav1alpha2.RedpandaSpec{
				ClusterSpec: &redpandav1alpha2.RedpandaClusterSpec{
					Image:       &redpandav1alpha2.RedpandaImage{Tag: ptr.To("v25.2.1")},
					Statefulset: &redpandav1alpha2.Statefulset{Replicas: ptr.To(3)},
					Resources: &redpandav1alpha2.Resources{
						CPU:    &redpandav1alpha2.CPU{Cores: ptr.To(resource.MustParse("4"))},
						Memory: &redpandav1alpha2.Memory{Container: &redpandav1alpha2.ContainerResources{Max: ptr.To(resource.MustParse("16Gi"))}},
					},
					TLS:        &redpandav1alpha2.TLS{Enabled: ptr.To(true)},
					Auth:       &redpandav1alpha2.Auth{SASL: &redpandav1alpha2.SASL{Enabled: ptr.To(true)}},
					Console:    &redpandav1alpha2.RedpandaConsole{Enabled: ptr.To(true)},
					Connectors: &redpandav1alpha2.RedpandaConnectors{Enabled: ptr.To(true)},
					Storage: &redpandav1alpha2.Storage{
						Tiered: &redpandav1alpha2.Tiered{
							Config: &redpandav1alpha2.TieredConfig{
								CloudStorageEnabled: &apiutil.JSONBoolean{Raw: []byte("true")},
							},
						},
					},
				},
			},
		},
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pvc-1",
				Namespace: "default",
				Labels:    map[string]string{"app.kubernetes.io/name": "redpanda"},
				Annotations: map[string]string{
					"volume.kubernetes.io/storage-provisioner": "ebs.csi.aws.com",
				},
			},
		},
		&apiextensionsv1.CustomResourceDefinition{
			ObjectMeta: metav1.ObjectMeta{Name: "redpandas.cluster.redpanda.com"},
			Spec:       apiextensionsv1.CustomResourceDefinitionSpec{Group: "cluster.redpanda.com"},
		},
		&apiextensionsv1.CustomResourceDefinition{
			ObjectMeta: metav1.ObjectMeta{Name: "certificates.cert-manager.io"},
			Spec:       apiextensionsv1.CustomResourceDefinitionSpec{Group: "cert-manager.io"},
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()

	collector := &Collector{
		Reader:          c,
		ID:              "source-id",
		OperatorVersion: "v26.2.0",
		IDHash:          "deadbeefcafe",
		Features:        map[string]bool{"pvcUnbinder": true},
		Discovery:       fakeServerVersion{info: &version.Info{GitVersion: "v1.32.0"}},
	}

	raw, err := collector.Collect(t.Context())
	require.NoError(t, err)

	payload := raw

	require.Equal(t, "source-id", payload.ID)
	require.Equal(t, "v26.2.0", payload.OperatorVersion)
	require.Equal(t, runtime.Version(), payload.GoVersion)
	require.Equal(t, "v1.32.0", payload.KubeVersion)
	require.Equal(t, "deadbeefcafe", payload.IDHash)

	require.True(t, payload.NodePools.Enabled)
	require.Equal(t, 1, payload.NodePools.Count)

	require.True(t, payload.StretchCluster.Enabled)
	require.Equal(t, 1, payload.StretchCluster.Count)

	// Redpanda fleet aggregates.
	require.Equal(t, 1, payload.Redpanda.Count)
	require.Equal(t, 5, payload.Redpanda.BrokerCount) // 3 rendered statefulset + 2 nodepool
	require.Equal(t, []string{"v25.2.1"}, payload.Redpanda.Versions)
	require.Equal(t, 1, payload.Redpanda.TLS)
	require.Equal(t, 1, payload.Redpanda.SASL)
	require.Equal(t, 1, payload.Redpanda.TieredStorage)
	require.Equal(t, 1, payload.Redpanda.Console)
	require.Equal(t, 1, payload.Redpanda.ManagedConnectors)
	// Sizing: 4c/16Gi per broker × 5 brokers (rp-1 default pool + correlated np-1).
	require.Equal(t, 20, payload.Redpanda.TotalCPUCores)
	require.Equal(t, 80, payload.Redpanda.TotalMemoryGiB)
	require.Equal(t, []string{"4c/16Gi"}, payload.Redpanda.BrokerSizes)

	// Stretch sizing from the RedpandaBrokerPool: 8c/32Gi × 3.
	require.Equal(t, 3, payload.StretchCluster.BrokerCount)
	require.Equal(t, 24, payload.StretchCluster.TotalCPUCores)
	require.Equal(t, 96, payload.StretchCluster.TotalMemoryGiB)
	require.Equal(t, []string{"8c/32Gi"}, payload.StretchCluster.BrokerSizes)

	// Vectorized sizing: 2c/8Gi × 1.
	require.Equal(t, 1, payload.VectorizedClusters.Count)
	require.Equal(t, 1, payload.VectorizedClusters.BrokerCount)
	require.Equal(t, 2, payload.VectorizedClusters.TotalCPUCores)
	require.Equal(t, 8, payload.VectorizedClusters.TotalMemoryGiB)
	require.Equal(t, []string{"2c/8Gi"}, payload.VectorizedClusters.BrokerSizes)

	require.Equal(t, []string{"ebs.csi.aws.com"}, payload.Storage.CSIDrivers)

	// Supporting CR-type counts.
	require.Equal(t, 1, payload.Resources.Topics)
	require.Equal(t, 1, payload.Resources.Users)
	require.Equal(t, 1, payload.Resources.Schemas)
	require.Equal(t, 1, payload.Resources.Roles)
	require.Equal(t, 1, payload.Resources.ShadowLinks)
	require.Equal(t, 1, payload.Resources.Consoles)

	require.Equal(t, 1, payload.CRDCount)
	// PVC Unbinder usage is reported via the features map, not a dedicated field.
	require.True(t, payload.Features["pvcUnbinder"])
	require.Equal(t, map[string]bool{"pvcUnbinder": true}, payload.Features)
}

func TestCollect_KubeVersionBestEffort(t *testing.T) {
	scheme := testScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	// A discovery failure must not fail collection; kubeVersion is left empty.
	collector := &Collector{
		Reader:    c,
		Discovery: fakeServerVersion{err: errors.New("api server unreachable")},
	}
	raw, err := collector.Collect(t.Context())
	require.NoError(t, err)
	require.Empty(t, raw.KubeVersion)

	// A nil discovery client also yields an empty version, no panic.
	collector = &Collector{Reader: c}
	raw, err = collector.Collect(t.Context())
	require.NoError(t, err)
	require.Empty(t, raw.KubeVersion)
}

func TestCollect_EmptyCluster(t *testing.T) {
	scheme := testScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	collector := &Collector{
		Reader:          c,
		ID:              "source-id",
		OperatorVersion: "v26.2.0",
	}

	raw, err := collector.Collect(t.Context())
	require.NoError(t, err)

	payload := raw

	require.False(t, payload.NodePools.Enabled)
	require.Equal(t, 0, payload.NodePools.Count)
	require.False(t, payload.StretchCluster.Enabled)
	require.Equal(t, 0, payload.StretchCluster.Count)
	require.Equal(t, 0, payload.Redpanda.Count)
	require.Equal(t, 0, payload.Redpanda.BrokerCount)
	require.Empty(t, payload.Redpanda.Versions)
	require.Equal(t, 0, payload.Redpanda.ManagedConnectors)
	require.Equal(t, 0, payload.VectorizedClusters.Count)
	require.Empty(t, payload.Storage.CSIDrivers)
	require.Equal(t, 0, payload.Resources.Topics)
	require.Equal(t, 0, payload.CRDCount)
}

func TestCollect_CSIDriversSortedAndDistinct(t *testing.T) {
	scheme := testScheme(t)

	pvc := func(name, provisioner string) client.Object {
		return &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "default",
				Labels:    map[string]string{"app.kubernetes.io/name": "redpanda"},
				Annotations: map[string]string{
					"volume.kubernetes.io/storage-provisioner": provisioner,
				},
			},
		}
	}

	objs := []client.Object{
		pvc("pvc-1", "pd.csi.storage.gke.io"),
		pvc("pvc-2", "ebs.csi.aws.com"),
		pvc("pvc-3", "ebs.csi.aws.com"),
		// Unlabeled PVC must be ignored.
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pvc-other",
				Namespace: "default",
				Annotations: map[string]string{
					"volume.kubernetes.io/storage-provisioner": "other.csi.driver",
				},
			},
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()

	collector := &Collector{Reader: c}
	raw, err := collector.Collect(t.Context())
	require.NoError(t, err)

	require.Equal(t, []string{"ebs.csi.aws.com", "pd.csi.storage.gke.io"}, raw.Storage.CSIDrivers)
}

// stubReader wraps a client.Reader and lets a test force specific List calls to
// fail, to exercise the collector's per-field degradation.
type stubReader struct {
	client.Reader
	listErr func(client.ObjectList) error
}

func (s stubReader) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if s.listErr != nil {
		if err := s.listErr(list); err != nil {
			return err
		}
	}
	return s.Reader.List(ctx, list, opts...)
}

func TestCollect_DegradesOnMissingCRDAndForbidden(t *testing.T) {
	scheme := testScheme(t)
	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&redpandav1alpha2.NodePool{ObjectMeta: metav1.ObjectMeta{Name: "np-1", Namespace: "default"}},
		&redpandav1alpha2.Topic{ObjectMeta: metav1.ObjectMeta{Name: "t-1", Namespace: "default"}},
	).Build()

	reader := stubReader{
		Reader: base,
		listErr: func(list client.ObjectList) error {
			switch l := list.(type) {
			case *redpandav1alpha2.StretchClusterList:
				// CRD not installed -> RESTMapper no-match.
				return &meta.NoKindMatchError{GroupKind: schema.GroupKind{Group: redpandaCRDGroup, Kind: "StretchCluster"}}
			case *metav1.PartialObjectMetadataList:
				// Counts use metadata-only lists; match the User count by GVK.
				if l.GroupVersionKind().Kind == "UserList" {
					// RBAC not granted -> Forbidden.
					return apierrors.NewForbidden(schema.GroupResource{Group: redpandaCRDGroup, Resource: "users"}, "", errors.New("forbidden"))
				}
			}
			return nil
		},
	}

	collector := &Collector{Reader: reader, ID: "id", OperatorVersion: "v26.2.0"}
	raw, err := collector.Collect(t.Context())
	require.NoError(t, err) // tolerated, document still sent

	// Base fields and the resources that ARE available are still reported.
	require.Equal(t, "id", raw.ID)
	require.Equal(t, "v26.2.0", raw.OperatorVersion)
	require.True(t, raw.NodePools.Enabled)
	require.Equal(t, 1, raw.NodePools.Count)
	require.Equal(t, 1, raw.Resources.Topics)
	// Missing CRD (NoMatch) and missing RBAC (Forbidden) degrade to zero values.
	require.False(t, raw.StretchCluster.Enabled)
	require.Equal(t, 0, raw.StretchCluster.Count)
	require.Equal(t, 0, raw.Resources.Users)
}

func TestCollect_PropagatesUnexpectedError(t *testing.T) {
	scheme := testScheme(t)
	base := fake.NewClientBuilder().WithScheme(scheme).Build()

	reader := stubReader{
		Reader: base,
		listErr: func(list client.ObjectList) error {
			if _, ok := list.(*redpandav1alpha2.NodePoolList); ok {
				return errors.New("api server unavailable")
			}
			return nil
		},
	}

	collector := &Collector{Reader: reader}
	_, err := collector.Collect(t.Context())
	// A non-tolerated error drops the cycle rather than sending a partial payload.
	require.Error(t, err)
}

func TestSizing(t *testing.T) {
	var s sizing
	// Limits used directly.
	s.add(corev1.ResourceRequirements{Limits: corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("4"),
		corev1.ResourceMemory: resource.MustParse("16Gi"),
	}}, 3)
	// Requests used as fallback when Limits is empty.
	s.add(corev1.ResourceRequirements{Requests: corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("2"),
		corev1.ResourceMemory: resource.MustParse("8Gi"),
	}}, 2)
	// Unset resources contribute nothing (per-field degradation).
	s.add(corev1.ResourceRequirements{}, 5)
	// Zero replicas contribute nothing.
	s.add(corev1.ResourceRequirements{Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("99")}}, 0)
	// Fractional CPU rounds to the nearest integer core in the label.
	s.add(corev1.ResourceRequirements{Limits: corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("3500m"),
		corev1.ResourceMemory: resource.MustParse("16Gi"),
	}}, 1)

	var cores, gib int
	var sizes []string
	s.into(&cores, &gib, &sizes)

	// Σ cores: 4*3 + 2*2 + round(3.5)*1 = 12 + 4 + 4 = 20.
	require.Equal(t, 20, cores)
	// Σ GiB: 16*3 + 8*2 + 16*1 = 48 + 16 + 16 = 80.
	require.Equal(t, 80, gib)
	require.Equal(t, []string{"2c/8Gi", "4c/16Gi"}, sizes)
}
