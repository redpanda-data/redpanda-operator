package steps

import (
	"context"
	"time"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

func iCreateABasicClusterWithNodes(ctx context.Context, t framework.TestingT, clusterName string, nodeCount int) {
	key := t.ResourceKey(clusterName)
	image := &redpandav1alpha2.RedpandaImage{
		Tag:        ptr.To("dev"),
		Repository: ptr.To("localhost/redpanda-operator"),
	}

	require.NoError(t, t.Create(ctx, &redpandav1alpha2.Redpanda{
		ObjectMeta: metav1.ObjectMeta{
			Name:      key.Name,
			Namespace: key.Namespace,
		},
		Spec: redpandav1alpha2.RedpandaSpec{
			ClusterSpec: &redpandav1alpha2.RedpandaClusterSpec{
				Statefulset: &redpandav1alpha2.Statefulset{
					Replicas: ptr.To(nodeCount),
					SideCars: &redpandav1alpha2.SideCars{
						Image: image,
						Controllers: &redpandav1alpha2.RPControllers{
							Image: image,
						},
					},
				},
			},
		},
	}))

	t.Cleanup(func(ctx context.Context) {
		t := framework.T(ctx)

		t.Log("cleaning up Redpanda cluster")
		require.NoError(t, t.Delete(ctx, &redpandav1alpha2.Redpanda{
			ObjectMeta: metav1.ObjectMeta{
				Name:      key.Name,
				Namespace: key.Namespace,
			},
		}))

		var cluster redpandav1alpha2.Redpanda
		require.Eventually(t, func() bool {
			// this can take some time
			deleted := false
			if err := t.Get(ctx, key, &cluster); err != nil && apierrors.IsNotFound(err) {
				deleted = true
			}

			t.Logf("checking that Redpanda cluster %q is fully deleted: %v", clusterName, deleted)

			return deleted
		}, 2*time.Minute, 5*time.Second, `Cluster %q still exists`, clusterName)
	})
}

func iScaleToNodes(ctx context.Context, t framework.TestingT, clusterName string, nodeCount int) {
	var cluster redpandav1alpha2.Redpanda
	var err error

	key := t.ResourceKey(clusterName)

	require.Eventually(t, func() bool {
		// do this in a loop in case we are racing with the controller
		require.NoError(t, t.Get(ctx, key, &cluster))
		cluster.Spec.ClusterSpec.Statefulset.Replicas = ptr.To(nodeCount)

		err = t.Update(ctx, &cluster)
		return err == nil
	}, 20*time.Second, 2*time.Second, `Cluster %q was unable to scale, last error: %v`, key.String(), err)
}
