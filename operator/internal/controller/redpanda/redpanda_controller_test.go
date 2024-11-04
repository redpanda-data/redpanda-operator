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
	"fmt"
	"math/rand"
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
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/redpanda-data/helm-charts/pkg/kube"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	crds "github.com/redpanda-data/redpanda-operator/operator/config/crd/bases"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller/flux"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller/redpanda"
	"github.com/redpanda-data/redpanda-operator/operator/internal/testenv"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	goclientscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NB: This test setup is largely incompatible with webhooks. Though we might
// be able to figure something freaky out.
func TestRedpandaController(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping long running test as -short was specified")
	}
	suite.Run(t, new(RedpandaControllerSuite))
}

type RedpandaControllerSuite struct {
	suite.Suite

	ctx    context.Context
	env    *testenv.Env
	client client.Client
}

var _ suite.SetupAllSuite = (*RedpandaControllerSuite)(nil)

// TestStableUIDAndGeneration asserts that UIDs, Generations, Labels, and
// Annotations of all objects created by the controller are stable across flux
// and de-fluxed.
func (s *RedpandaControllerSuite) TestStableUIDAndGeneration() {
	isStable := func(a, b client.Object) {
		assert.Equal(s.T(), a.GetUID(), b.GetUID(), "%T %q's UID changed (Something recreated it)", a, a.GetName())
		assert.Equal(s.T(), a.GetLabels(), b.GetLabels(), "%T %q's Labels changed", a, a.GetName())
		assert.Equal(s.T(), a.GetAnnotations(), b.GetAnnotations(), "%T %q's Labels changed", a, a.GetName())
		assert.Equal(s.T(), a.GetGeneration(), b.GetGeneration(), "%T %q's Generation changed (Something changed .Spec)", a, a.GetName())
	}

	// A loop makes this easier to maintain but not to read. We're testing that
	// the following paths from "fresh" hold the isStable property defined
	// above.
	// - NoFlux (Fresh) -> Flux (Toggled) -> NoFlux (Toggled)
	// - Flux (Fresh) -> NoFlux (Toggled) -> Flux (Toggled)
	for _, useFlux := range []bool{true, false} {
		rp := s.minimalRP(useFlux)
		s.applyAndWait(rp)

		filter := client.MatchingLabels{"app.kubernetes.io/instance": rp.Name}

		fresh := s.snapshotCluster(filter)

		rp.Spec.ChartRef.UseFlux = ptr.To(!useFlux)
		s.applyAndWait(rp)

		flipped := s.snapshotCluster(filter)
		s.compareSnapshot(fresh, flipped, isStable)

		rp.Spec.ChartRef.UseFlux = ptr.To(useFlux)
		s.applyAndWait(rp)

		flippedBack := s.snapshotCluster(filter)
		s.compareSnapshot(flipped, flippedBack, isStable)

		s.deleteAndWait(rp)
	}
}

func (s *RedpandaControllerSuite) TestObjectsGCed() {
	rp := s.minimalRP(false)
	rp.Spec.ClusterSpec.Console.Enabled = ptr.To(true)
	s.applyAndWait(rp)

	// Create a list of secrets with varying labels that we expect to NOT get
	// GC'd.
	secrets := []*corev1.Secret{
		{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "no-gc-",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "no-gc-",
				Labels: map[string]string{
					"helm.toolkit.fluxcd.io/name":      rp.Name,
					"helm.toolkit.fluxcd.io/namespace": rp.Namespace,
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "no-gc-",
				Labels: map[string]string{
					"helm.toolkit.fluxcd.io/name":      rp.Name,
					"helm.toolkit.fluxcd.io/namespace": rp.Namespace,
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "no-gc-",
				Labels: map[string]string{
					"app.kubernetes.io/instance": rp.Name,
				},
			},
		},
	}

	for _, secret := range secrets {
		s.Require().NoError(s.client.Create(s.ctx, secret))
	}

	// Assert that the console deployment exists
	var deployments appsv1.DeploymentList
	s.NoError(s.client.List(s.ctx, &deployments, client.MatchingLabels{"app.kubernetes.io/instance": rp.Name}))
	s.Len(deployments.Items, 1)

	rp.Spec.ClusterSpec.Console.Enabled = ptr.To(false)
	s.applyAndWait(rp)

	// Assert that the console deployment has been garbage collected.
	s.NoError(s.client.List(s.ctx, &deployments, client.MatchingLabels{"app.kubernetes.io/instance": rp.Name}))
	s.Len(deployments.Items, 0)

	// Assert that our previously created secrets have not been GC'd.
	for _, secret := range secrets {
		key := client.ObjectKeyFromObject(secret)
		s.Require().NoError(s.client.Get(s.ctx, key, secret))
	}

	s.deleteAndWait(rp)
}

func (s *RedpandaControllerSuite) TestTPLValues() {
	rp := s.minimalRP(false)

	extraVolumeMount := ptr.To(`- name: test-extra-volume
  mountPath: {{ upper "/fake/lifecycle" }}`)

	rp.Spec.ClusterSpec.Statefulset.ExtraVolumeMounts = extraVolumeMount
	rp.Spec.ClusterSpec.Statefulset.ExtraVolumes = ptr.To(fmt.Sprintf(`- name: test-extra-volume
  secret:
    secretName: %s-sts-lifecycle
    defaultMode: 0774`, rp.Name))
	rp.Spec.ClusterSpec.Statefulset.InitContainers = &redpandav1alpha2.InitContainers{
		Configurator:                      &redpandav1alpha2.Configurator{ExtraVolumeMounts: extraVolumeMount},
		FsValidator:                       &redpandav1alpha2.FsValidator{ExtraVolumeMounts: extraVolumeMount},
		Tuning:                            &redpandav1alpha2.Tuning{ExtraVolumeMounts: extraVolumeMount},
		SetDataDirOwnership:               &redpandav1alpha2.SetDataDirOwnership{ExtraVolumeMounts: extraVolumeMount},
		SetTieredStorageCacheDirOwnership: &redpandav1alpha2.SetTieredStorageCacheDirOwnership{ExtraVolumeMounts: extraVolumeMount},
		ExtraInitContainers: ptr.To(`- name: "test-init-container"
  image: "mintel/docker-alpine-bash-curl-jq:latest"
  command: [ "/bin/bash", "-c" ]
  volumeMounts:
  - name: test-extra-volume
    mountPath: /FAKE/LIFECYCLE
  args:
    - |
      set -xe
      echo "Hello World!"`),
	}
	rp.Spec.ClusterSpec.Statefulset.SideCars = &redpandav1alpha2.SideCars{ConfigWatcher: &redpandav1alpha2.ConfigWatcher{ExtraVolumeMounts: extraVolumeMount}}
	s.applyAndWait(rp)

	var sts appsv1.StatefulSet
	s.NoError(s.client.Get(s.ctx, types.NamespacedName{Name: rp.Name, Namespace: rp.Namespace}, &sts))

	s.Contains(sts.Spec.Template.Spec.Volumes, corev1.Volume{
		Name: "test-extra-volume",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{DefaultMode: ptr.To(int32(508)), SecretName: fmt.Sprintf("%s-sts-lifecycle", rp.Name)},
		},
	})
	for _, c := range sts.Spec.Template.Spec.InitContainers {
		if c.Name == "bootstrap-yaml-envsubst" {
			continue
		}

		s.Contains(c.VolumeMounts, corev1.VolumeMount{Name: "test-extra-volume", MountPath: "/FAKE/LIFECYCLE"})

		if c.Name == "test-init-container" {
			s.Equal(c.Command, []string{"/bin/bash", "-c"})
			s.Equal(c.Args, []string{"set -xe\necho \"Hello World!\""})
			s.Equal(c.Image, "mintel/docker-alpine-bash-curl-jq:latest")
		}
	}
	for _, c := range sts.Spec.Template.Spec.Containers {
		s.Contains(c.VolumeMounts, corev1.VolumeMount{Name: "test-extra-volume", MountPath: "/FAKE/LIFECYCLE"})
	}
}

func (s *RedpandaControllerSuite) SetupSuite() {
	t := s.T()

	scheme := runtime.NewScheme()
	require.NoError(t, certmanagerv1.AddToScheme(scheme))
	require.NoError(t, goclientscheme.AddToScheme(scheme))
	require.NoError(t, helmcontrollerv2beta1.AddToScheme(scheme))
	require.NoError(t, helmcontrollerv2beta2.AddToScheme(scheme))
	require.NoError(t, monitoringv1.AddToScheme(scheme))
	require.NoError(t, redpandav1alpha2.AddToScheme(scheme))
	require.NoError(t, sourcecontrollerv1.AddToScheme(scheme))
	require.NoError(t, sourcecontrollerv1beta1.AddToScheme(scheme))
	require.NoError(t, sourcecontrollerv1beta2.AddToScheme(scheme))

	// TODO SetupManager currently runs with admin permissions on the cluster.
	// This will allow the operator's ClusterRole and Role to get out of date.
	// Ideally, we should bind the declared permissions of the operator to the
	// rest config given to the manager.
	s.ctx = context.Background()
	s.env = testenv.New(t, testenv.Options{
		Scheme: scheme,
		CRDs:   crds.All(),
		Logger: testr.New(t),
	})

	s.client = s.env.Client()

	s.env.SetupManager(func(mgr ctrl.Manager) error {
		controllers := flux.NewFluxControllers(mgr, fluxclient.Options{}, fluxclient.KubeConfigOptions{})
		for _, controller := range controllers {
			if err := controller.SetupWithManager(s.ctx, mgr); err != nil {
				return err
			}
		}

		dialer := kube.NewPodDialer(mgr.GetConfig())
		clientFactory := internalclient.NewFactory(mgr.GetConfig(), mgr.GetClient()).WithDialer(dialer.DialContext)

		// TODO should probably run other reconcilers here.
		return (&redpanda.RedpandaReconciler{
			Client:        mgr.GetClient(),
			Scheme:        mgr.GetScheme(),
			EventRecorder: mgr.GetEventRecorderFor("Redpanda"),
			ClientFactory: clientFactory,
		}).SetupWithManager(s.ctx, mgr)
	})
}

func (s *RedpandaControllerSuite) minimalRP(useFlux bool) *redpandav1alpha2.Redpanda {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

	name := "rp-"
	for i := 0; i < 6; i++ {
		//nolint:gosec // not meant to be a secure random string.
		name += string(alphabet[rand.Intn(len(alphabet))])
	}

	return &redpandav1alpha2.Redpanda{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: redpandav1alpha2.RedpandaSpec{
			ChartRef: redpandav1alpha2.ChartRef{
				UseFlux: ptr.To(useFlux),
			},
			ClusterSpec: &redpandav1alpha2.RedpandaClusterSpec{
				Image: &redpandav1alpha2.RedpandaImage{
					Repository: ptr.To("redpandadata/redpanda"), // Override the default to make use of the docker-io image cache.
				},
				Console: &redpandav1alpha2.RedpandaConsole{
					Enabled: ptr.To(false), // Speed up most cases by not enabling console to start.
				},
				Statefulset: &redpandav1alpha2.Statefulset{
					Replicas: ptr.To(1), // Speed up tests ever so slightly.
				},
			},
		},
	}
}

func (s *RedpandaControllerSuite) deleteAndWait(obj client.Object) {
	gvk, err := s.client.GroupVersionKindFor(obj)
	s.NoError(err)

	obj.SetManagedFields(nil)
	obj.GetObjectKind().SetGroupVersionKind(gvk)

	if err := s.client.Delete(s.ctx, obj, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil {
		// obj, might not exist at all. If so, no-op.
		if apierrors.IsNotFound(err) {
			return
		}
		s.Require().NoError(err)
	}

	s.NoError(wait.PollUntilContextTimeout(s.ctx, 5*time.Second, 5*time.Minute, false, func(ctx context.Context) (done bool, err error) {
		if err := s.client.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	}))
}

func (s *RedpandaControllerSuite) applyAndWait(obj client.Object) {
	gvk, err := s.client.GroupVersionKindFor(obj)
	s.NoError(err)

	obj.SetManagedFields(nil)
	obj.GetObjectKind().SetGroupVersionKind(gvk)

	s.Require().NoError(s.client.Patch(s.ctx, obj, client.Apply, client.ForceOwnership, client.FieldOwner("tests")))

	s.NoError(wait.PollUntilContextTimeout(s.ctx, 5*time.Second, 5*time.Minute, false, func(ctx context.Context) (done bool, err error) {
		if err := s.client.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
			return false, err
		}

		switch obj := obj.(type) {
		case *redpandav1alpha2.Redpanda:
			ready := apimeta.IsStatusConditionTrue(obj.Status.Conditions, "Ready")
			upToDate := obj.Generation != 0 && obj.Generation == obj.Status.ObservedGeneration
			return upToDate && ready, nil

		default:
			s.Fail("unhandled object %T in applyAndWait", obj)

		}

		s.T().Logf("waiting for %T %q to be ready", obj, obj.GetName())
		return false, nil
	}))
}

func (s *RedpandaControllerSuite) snapshotCluster(opts ...client.ListOption) []kube.Object {
	var objs []kube.Object

	// TODO export a list of object types from the redpanda chart.
	// for _, t := range chart.Types() {
	for _, t := range []kube.Object{} {
		gvk, err := s.client.GroupVersionKindFor(t)
		s.NoError(err)

		gvk.Kind += "List"

		list, err := s.client.Scheme().New(gvk)
		s.NoError(err)

		if err := s.client.List(s.ctx, list.(client.ObjectList), opts...); err != nil {
			if apimeta.IsNoMatchError(err) {
				s.T().Logf("skipping unknown list type %T", list)
				continue
			}
			s.NoError(err)
		}

		s.NoError(apimeta.EachListItem(list, func(o runtime.Object) error {
			obj := o.(client.Object)
			obj.SetManagedFields(nil)
			objs = append(objs, obj)
			return nil
		}))
	}

	return objs
}

func (s *RedpandaControllerSuite) compareSnapshot(a, b []client.Object, fn func(a, b client.Object)) {
	assert.Equal(s.T(), len(a), len(b))

	getGVKName := func(o client.Object) string {
		gvk, err := s.client.GroupVersionKindFor(o)
		s.NoError(err)
		return gvk.String() + client.ObjectKeyFromObject(o).String()
	}

	groupedA := mapBy(a, getGVKName)
	groupedB := mapBy(b, getGVKName)

	for key, a := range groupedA {
		b := groupedB[key]
		fn(a, b)
	}
}

func mapBy[T any, K comparable](items []T, fn func(T) K) map[K]T {
	out := make(map[K]T, len(items))
	for _, item := range items {
		key := fn(item)
		if _, ok := out[key]; ok {
			panic(fmt.Sprintf("duplicate key: %v", key))
		}
		out[key] = item
	}
	return out
}
