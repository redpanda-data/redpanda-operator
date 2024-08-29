package redpanda

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha2"
	internalclient "github.com/redpanda-data/redpanda-operator/src/go/k8s/internal/client"
	"github.com/redpanda-data/redpanda-operator/src/go/k8s/internal/testutils"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

func TestUserReconcile(t *testing.T) { // nolint:funlen // These tests have clear subtests.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	server := &envtest.APIServer{}
	etcd := &envtest.Etcd{}

	testEnv := testutils.RedpandaTestEnv{
		Environment: envtest.Environment{
			ControlPlane: envtest.ControlPlane{
				APIServer: server,
				Etcd:      etcd,
			},
		},
	}
	cfg, err := testEnv.StartRedpandaTestEnv(false)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	t.Cleanup(func() {
		testEnv.Stop()
	})

	container, err := redpanda.Run(ctx, "docker.redpanda.com/redpandadata/redpanda:v23.2.8")
	require.NoError(t, err)

	t.Cleanup(func() {
		container.Terminate(context.Background())
	})

	broker, err := container.KafkaSeedBroker(ctx)
	require.NoError(t, err)

	admin, err := container.AdminAPIAddress(ctx)
	require.NoError(t, err)

	err = redpandav1alpha2.AddToScheme(scheme.Scheme)
	require.NoError(t, err)

	c, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	require.NoError(t, err)
	require.NotNil(t, c)

	reconciler := UserReconciler{
		ClientFactory: internalclient.NewFactory(cfg, c),
	}

	user := &redpandav1alpha2.User{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "user",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: redpandav1alpha2.UserSpec{
			ClusterSource: &redpandav1alpha2.ClusterSource{
				StaticConfiguration: &redpandav1alpha2.StaticConfigurationSource{
					Kafka: &redpandav1alpha2.KafkaAPISpec{
						Brokers: []string{broker},
					},
					Admin: &redpandav1alpha2.AdminAPISpec{
						URLs: []string{admin},
					},
				},
			},
		},
	}

	key := client.ObjectKeyFromObject(user)
	req := ctrl.Request{NamespacedName: key}

	require.NoError(t, c.Create(ctx, user))
	_, err = reconciler.Reconcile(ctx, req)
	require.NoError(t, err)

	require.NoError(t, c.Get(ctx, key, user))
	require.Equal(t, []string{FinalizerKey}, user.Finalizers)
	require.Len(t, user.Status.Conditions, 1)
	fmt.Println("one", user.Status.Conditions[0])
	then := user.Status.Conditions[0].LastTransitionTime

	// re-reconcile, check status
	_, err = reconciler.Reconcile(ctx, req)
	require.NoError(t, err)

	require.NoError(t, c.Get(ctx, key, user))
	require.Len(t, user.Status.Conditions, 1)
	fmt.Println("two", user.Status.Conditions[0])
	now := user.Status.Conditions[0].LastTransitionTime

	require.Equal(t, then, now)

	require.NoError(t, c.Delete(ctx, user))
	_, err = reconciler.Reconcile(ctx, req)
	require.NoError(t, err)

	require.True(t, apierrors.IsNotFound(c.Get(ctx, key, user)))
}
