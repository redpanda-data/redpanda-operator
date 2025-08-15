package kube_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/pkg/kube"
	"github.com/redpanda-data/redpanda-operator/pkg/kube/kubetest"
)

func TestSyncer(t *testing.T) {
	// Notably:
	// - excludes apiextensions.
	// - includes certmanagerv1 but CRDs are NOT installed.
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, certmanagerv1.AddToScheme(scheme))

	ctl := kubetest.NewEnv(t, kube.Options{
		Options: client.Options{
			Scheme: scheme,
		},
	})

	// This namespace with hold our test resources and act as the "owner" of
	// them.
	ns, err := kube.Create[corev1.Namespace](t.Context(), ctl, corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-",
			Labels: map[string]string{
				"owned_by": "no-one",
			},
		},
	})
	require.NoError(t, err)

	r := &renderer{Prefix: "test", Namespace: ns.Name}
	syncer := kube.Syncer{
		Ctl:       ctl,
		Renderer:  r,
		Namespace: ns.Name,
		Owner: metav1.OwnerReference{
			Name:       ns.Name,
			APIVersion: "v1",
			Kind:       "Namespace",
			UID:        ns.UID,
		},
		OwnershipLabels: map[string]string{
			"owned_by": "syncer",
		},
	}

	t.Run("NotInTypes", func(t *testing.T) {
		r.ObjNotInTypes = true
		defer func() { r.ObjNotInTypes = false }()

		_, err := syncer.Sync(t.Context())
		require.EqualError(t, err, ".Render returned *v1.PersistentVolume which isn't present in .Types")
	})

	t.Run("NotInScheme", func(t *testing.T) {
		r.ObjNotInScheme = true
		defer func() { r.ObjNotInScheme = false }()

		_, err := syncer.Sync(t.Context())
		require.EqualError(t, err, `no kind is registered for the type v1.CustomResourceDefinition in scheme "pkg/runtime/scheme.go:110"`)
	})

	t.Run("NotInAPI", func(t *testing.T) {
		r.ObjNotInAPI = true
		defer func() { r.ObjNotInAPI = false }()

		// If we have a type that's not registered in the API, we'll swallow
		// any such errors an instead log a warning.
		// This may not be the optimal behavior.
		_, err := syncer.Sync(t.Context())
		require.NoError(t, err)
	})

	t.Run("Sync", func(t *testing.T) {
		defer func() {
			r.Prefix = "test"
			syncer.Preprocess = nil
		}()

		// Test syncing by changing object names and labels.
		for i := range 5 {
			r.Prefix = fmt.Sprintf("test-%d", i)

			for j := range 5 {
				syncer.Preprocess = func(o kube.Object) {
					if o.GetLabels() == nil {
						o.SetLabels(map[string]string{})
					}
					o.GetLabels()["iteration"] = fmt.Sprintf("%d", j)
				}

				objs, err := syncer.Sync(t.Context())
				require.NoError(t, err)

				for _, obj := range objs {
					// assert that objects are updated in place.
					require.NotEmpty(t, obj.GetUID())
					// Ownership labels have been applied.
					require.Equal(t, "syncer", obj.GetLabels()["owned_by"])

					//
					require.Equal(t, fmt.Sprintf("%d", j), obj.GetLabels()["iteration"])

					gvk, err := kube.GVKFor(ctl.Scheme(), obj)
					require.NoError(t, err)

					scope, err := ctl.ScopeOf(gvk)
					require.NoError(t, err)

					if scope == meta.RESTScopeNameNamespace {
						require.Equal(t, []metav1.OwnerReference{{
							Name:       ns.Name,
							APIVersion: "v1",
							Kind:       "Namespace",
							UID:        ns.UID,
						}}, obj.GetOwnerReferences())
					}
				}
			}
		}
	})

	// Asserts that the syncer will take ownership of objects instead of
	// returning errors.
	t.Run("SSAConflicts", func(t *testing.T) {
		objs, err := syncer.Sync(t.Context())
		require.NoError(t, err)

		// NB: This must be on the same key that's referenced in renderer to
		// actually generate a conflict.
		cm := objs[0].(*corev1.ConfigMap)
		cm.Data["some"] = "update"

		err = ctl.Apply(t.Context(), objs[0], client.FieldOwner("conflicter"))
		require.True(t, apierrors.IsConflict(err))

		err = ctl.Apply(t.Context(), objs[0], client.FieldOwner("conflicter"), client.ForceOwnership)
		require.NoError(t, err)

		_, err = syncer.Sync(t.Context())
		require.NoError(t, err)
	})

	t.Run("DeleteAll", func(t *testing.T) {
		ctx := t.Context()

		require.NoError(t, ctl.Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "not-owned",
				Namespace: ns.Name,
				// No OwnerReference means this object will remain untouched.
				Labels: syncer.OwnershipLabels,
			},
		}))

		_, err := syncer.Sync(ctx)
		require.NoError(t, err)

		_, err = syncer.DeleteAll(ctx)
		require.NoError(t, err)

		require.EventuallyWithT(t, func(t *assert.CollectT) {
			// Our owning namespace hasn't been removed but the other one(s)
			// have been cleaned up. NB: envtest namespaces never get fully
			// deleted, so we filter to Active ones.
			nss, err := kube.List[corev1.NamespaceList](ctx, ctl, "", client.MatchingFields{
				"status.phase": "Active",
			}, client.HasLabels{"owned_by"})
			require.NoError(t, err)
			require.Len(t, nss.Items, 1)
			require.Equal(t, ns.UID, nss.Items[0].UID)

			// The only left over configmap is our unowned one.
			cms, err := kube.List[corev1.ConfigMapList](ctx, ctl, ns.Name, client.HasLabels{"owned_by"})
			require.NoError(t, err)
			require.Len(t, cms.Items, 1)
			require.Equal(t, "not-owned", cms.Items[0].Name)
		}, 30*time.Second, time.Second)
	})
}

type renderer struct {
	Prefix         string
	Namespace      string
	ObjNotInScheme bool
	ObjNotInAPI    bool
	ObjNotInTypes  bool
}

func (r *renderer) Render(_ context.Context) ([]kube.Object, error) {
	objs := []kube.Object{
		// A Namespace scoped object.
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: r.Prefix + "-cm", Namespace: r.Namespace}, Data: map[string]string{"some": "data"}},
		// A cluster scoped object.
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: r.Prefix + "-ns"}},
	}
	if r.ObjNotInAPI {
		objs = append(objs, &certmanagerv1.Certificate{ObjectMeta: metav1.ObjectMeta{Name: r.Prefix + "-c", Namespace: r.Namespace}})
	}
	if r.ObjNotInScheme {
		objs = append(objs, &apiextensionsv1.CustomResourceDefinition{ObjectMeta: metav1.ObjectMeta{Name: r.Prefix + "-crd"}})
	}
	if r.ObjNotInTypes {
		objs = append(objs, &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: r.Prefix + "-pv"}})
	}
	return objs, nil
}

func (r *renderer) Types() []kube.Object {
	ts := []kube.Object{
		&corev1.ConfigMap{},
		&corev1.Namespace{},
	}
	if r.ObjNotInScheme {
		ts = append(ts, &apiextensionsv1.CustomResourceDefinition{})
	}
	if r.ObjNotInAPI {
		ts = append(ts, &certmanagerv1.Certificate{})
	}
	return ts
}
