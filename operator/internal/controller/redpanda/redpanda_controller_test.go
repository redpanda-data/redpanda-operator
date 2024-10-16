// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda_test

import (
	"context"
	"testing"
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	helmcontrollerv2beta1 "github.com/fluxcd/helm-controller/api/v2beta1"
	helmcontrollerv2beta2 "github.com/fluxcd/helm-controller/api/v2beta2"
	fluxclient "github.com/fluxcd/pkg/runtime/client"
	sourcecontrollerv1 "github.com/fluxcd/source-controller/api/v1"
	sourcecontrollerv1beta1 "github.com/fluxcd/source-controller/api/v1beta1"
	sourcecontrollerv1beta2 "github.com/fluxcd/source-controller/api/v1beta2"
	"github.com/go-logr/logr/testr"
	"github.com/redpanda-data/helm-charts/pkg/testutil"
	"github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	crds "github.com/redpanda-data/redpanda-operator/operator/config/crd/bases"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller/flux"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller/redpanda"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/k3d"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/utils/testutils"
	"github.com/stretchr/testify/require"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	goclientscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

// NB: This test setup is largely incompatible with webhooks. Though we might
// be able to figure something freaky out.
func TestRedpandaController(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, certmanagerv1.AddToScheme(scheme))
	require.NoError(t, goclientscheme.AddToScheme(scheme))
	require.NoError(t, helmcontrollerv2beta1.AddToScheme(scheme))
	require.NoError(t, helmcontrollerv2beta2.AddToScheme(scheme))
	require.NoError(t, redpandav1alpha2.AddToScheme(scheme))
	require.NoError(t, sourcecontrollerv1.AddToScheme(scheme))
	require.NoError(t, sourcecontrollerv1beta1.AddToScheme(scheme))
	require.NoError(t, sourcecontrollerv1beta2.AddToScheme(scheme))

	ctx := context.Background()
	if deadline, ok := t.Deadline(); ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Until(deadline)-(30*time.Second))
		defer cancel()
	}

	// TODO extract a timelimit here
	logger := testr.New(t)
	ctrl.SetLogger(logger)
	ctx = ctrl.LoggerInto(ctx, logger)

	// Might be good to add a wait into k3d for all jobs (helm installs) to
	// finish.
	cluster, err := k3d.NewCluster(t.Name())
	require.NoError(t, err)

	t.Cleanup(func() {
		if !testutil.Retain() {
			require.NoError(t, cluster.Cleanup())
		}
	})

	// Ideally there'd be a better way to handle this.
	crds, err := envtest.InstallCRDs(cluster.RESTConfig(), envtest.CRDInstallOptions{
		ErrorIfPathMissing: true,
		CRDs:               crds.All(),
	})
	require.NoError(t, err)
	require.True(t, len(crds) > 0)

	mgr, err := ctrl.NewManager(cluster.RESTConfig(), ctrl.Options{
		Metrics: server.Options{BindAddress: "0"},
		Scheme:  scheme,
	})
	require.NoError(t, err)

	controllers := flux.NewFluxControllers(mgr, fluxclient.Options{}, fluxclient.KubeConfigOptions{})
	for _, controller := range controllers {
		require.NoError(t, controller.SetupWithManager(ctx, mgr))
	}

	// TODO should probably run other reconcilers here.
	require.NoError(t, (&redpanda.RedpandaReconciler{
		Client:        mgr.GetClient(),
		Scheme:        mgr.GetScheme(),
		EventRecorder: mgr.GetEventRecorderFor("Redpanda"),
	}).SetupWithManager(ctx, mgr))

	testutils.Go(t, ctx, mgr.Start)

	c := mgr.GetClient()

	// TODO why do we have to set Kind and APIVersion??
	rp := &v1alpha2.Redpanda{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Redpanda",
			APIVersion: "cluster.redpanda.com/v1alpha2",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "redpanda",
			Namespace: "default",
		},
		Spec: v1alpha2.RedpandaSpec{
			ChartRef: v1alpha2.ChartRef{
				// UseFlux: ptr.To(false),
			},
			ClusterSpec: &v1alpha2.RedpandaClusterSpec{},
		},
	}

	apply := func() {
		gvk, err := c.GroupVersionKindFor(rp)
		require.NoError(t, err)

		rp.SetManagedFields(nil)
		rp.GetObjectKind().SetGroupVersionKind(gvk)

		require.NoError(t, c.Patch(ctx, rp, client.Apply, client.ForceOwnership, client.FieldOwner("tests")))

		require.NoError(t, wait.PollUntilContextTimeout(ctx, 5*time.Second, 5*time.Minute, false, func(ctx context.Context) (done bool, err error) {
			if err := c.Get(ctx, client.ObjectKeyFromObject(rp), rp); err != nil {
				return false, err
			}

			ready := apimeta.IsStatusConditionTrue(rp.Status.Conditions, "Ready")
			upToDate := rp.Generation != 0 && rp.Generation == rp.Status.ObservedGeneration
			if upToDate && ready {
				t.Logf("%T is Ready: %d == %d\n%#v", rp, rp.Generation, rp.Status.ObservedGeneration, rp.Status)
				return true, nil
			}

			logger.Info(".Generation != .Status.ObservedGeneration")
			return false, nil
		}))
	}

	// Initial creation
	apply()

	// Disable Flux
	rp.Spec.ChartRef.UseFlux = ptr.To(false)
	apply()

	// Enable Flux
	rp.Spec.ChartRef.UseFlux = ptr.To(true)
	apply()

	// Disable Flux
	rp.Spec.ChartRef.UseFlux = ptr.To(false)
	apply()

	// Delete rp and assert that everything has been cleaned up correctly.
	require.NoError(t, c.Delete(ctx, rp, client.PropagationPolicy(metav1.DeletePropagationForeground)))
	// TODO need to do a wait loop here as well.
	// TODO make an assertion that all relevant resources have been cleaned up.
}

func apply(t *testing.T, c client.Client, ctx context.Context, obj client.Object) {
	gvk, err := c.GroupVersionKindFor(obj)
	require.NoError(t, err)

	obj.SetManagedFields(nil)
	obj.GetObjectKind().SetGroupVersionKind(gvk)

	require.NoError(t, c.Patch(ctx, obj, client.Apply, client.ForceOwnership, client.FieldOwner("test.apply")))
}
