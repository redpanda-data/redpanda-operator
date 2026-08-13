// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package migration

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/redpanda-data/common-go/kube"
	"github.com/redpanda-data/common-go/kube/kubetest"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	crds "github.com/redpanda-data/redpanda-operator/operator/config/crd/bases"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/operator/internal/lifecycle"
)

func TestFieldManagers(t *testing.T) {
	scheme := controller.UnifiedScheme
	config := kubetest.NewEnv(t).RestConfig()

	oldctl, err := kube.FromRESTConfig(config, kube.Options{
		Options: client.Options{
			Scheme: scheme,
		},
		FieldManager: "*kube.Ctl",
	})
	require.NoError(t, err)

	newctl, err := kube.FromRESTConfig(config, kube.Options{
		Options: client.Options{
			Scheme: scheme,
		},
		FieldManager: "new",
	})
	require.NoError(t, err)

	k8sClient, err := client.New(config, client.Options{Scheme: scheme})
	require.NoError(t, err)

	// install our CRDs
	require.NoError(t, kube.ApplyAll(t.Context(), oldctl, crds.All()...))
	for _, crd := range crds.All() {
		require.NoError(t, kube.WaitFor(t.Context(), oldctl, crd.DeepCopy(), func(ext *apiextensionsv1.CustomResourceDefinition, err error) (bool, error) {
			for _, cond := range ext.Status.Conditions {
				if cond.Type == apiextensionsv1.Established && cond.Status == apiextensionsv1.ConditionTrue {
					return true, nil
				}
			}
			return false, nil
		}))
	}

	// Create a Redpanda cluster with the normal client
	cluster := &redpandav1alpha2.Redpanda{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "fieldmanagers-test",
			Namespace: "default",
		},
		Spec: redpandav1alpha2.RedpandaSpec{
			ClusterSpec: &redpandav1alpha2.RedpandaClusterSpec{
				Statefulset: &redpandav1alpha2.Statefulset{
					Replicas: ptr.To(1),
				},
			},
		},
	}
	require.NoError(t, k8sClient.Create(t.Context(), cluster))
	require.NoError(t, k8sClient.Get(t.Context(), client.ObjectKeyFromObject(cluster), cluster))

	// Create a Console cluster too
	console := &redpandav1alpha2.Console{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "fieldmanagers-test-console",
			Namespace: "default",
		},
		Spec: redpandav1alpha2.ConsoleSpec{
			ClusterSource: &redpandav1alpha2.ClusterSource{
				ClusterRef: &redpandav1alpha2.ClusterRef{
					Name: "fieldmanagers-test",
				},
			},
		},
	}
	require.NoError(t, k8sClient.Create(t.Context(), console))
	require.NoError(t, k8sClient.Get(t.Context(), client.ObjectKeyFromObject(console), console))

	{
		// Redpanda portion of the test

		// Add a junk statefulset associated with the Redpanda cluster using the old client
		set := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "fieldmanagers-test",
				Namespace: "default",
				Labels:    lifecycle.NewV2OwnershipResolver().GetOwnerLabels(&lifecycle.ClusterWithPools{Redpanda: cluster}),
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: redpandav1alpha2.GroupVersion.String(),
					Kind:       "Redpanda",
					Name:       cluster.Name,
					UID:        cluster.UID,
				}},
			},
			Spec: appsv1.StatefulSetSpec{
				Replicas: ptr.To[int32](1),
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "test",
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app": "test",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "test",
								Image: "test",
								Ports: []corev1.ContainerPort{
									{
										Name:          "test",
										ContainerPort: 8080,
									},
								},
							},
						},
					},
				},
			},
		}
		newSet := set.DeepCopy()
		// now change the port to have the same name and verify it gets merged
		newSet.Spec.Template.Spec.Containers[0].Ports = []corev1.ContainerPort{{
			Name:          "test",
			ContainerPort: 18080,
		}}
		// create one more for later
		finalSet := newSet.DeepCopy()

		// server-side-apply this on with the old client to simulate existing resources
		require.NoError(t, oldctl.Apply(t.Context(), set))

		// check the initial field managers
		require.NoError(t, k8sClient.Get(t.Context(), client.ObjectKeyFromObject(set), set))
		managers := getFieldManagers(set)
		t.Logf("Initial field managers: %+v", managers)
		require.True(t, slices.Contains(managers, "*kube.Ctl"))

		// and apply with the new client
		require.NoError(t, newctl.Apply(t.Context(), newSet))
		require.NoError(t, k8sClient.Get(t.Context(), client.ObjectKeyFromObject(set), set))

		managers = getFieldManagers(set)
		t.Logf("Updated field managers: %+v", managers)
		require.True(t, slices.Contains(managers, "*kube.Ctl"))
		require.True(t, slices.Contains(managers, "new"))

		// also check the poorly merged port
		ports := set.Spec.Template.Spec.Containers[0].Ports
		require.Len(t, ports, 2)
		require.Equal(t, "test", ports[0].Name)
		require.Equal(t, "test", ports[1].Name)

		// now run the migration
		require.NoError(t, migrateFieldManagers(t.Context(), newctl, k8sClient))

		// verify the field managers are updated
		require.NoError(t, k8sClient.Get(t.Context(), client.ObjectKeyFromObject(set), set))
		managers = getFieldManagers(set)
		t.Logf("Migrated field managers: %+v", managers)
		require.False(t, slices.Contains(managers, "*kube.Ctl"))
		require.True(t, slices.Contains(managers, "new"))

		// verify the ports are still messed up (since we just re-applied what was already there)
		require.NoError(t, k8sClient.Get(t.Context(), client.ObjectKeyFromObject(set), set))
		ports = set.Spec.Template.Spec.Containers[0].Ports
		require.Len(t, ports, 2)
		require.Equal(t, "test", ports[0].Name)
		require.Equal(t, "test", ports[1].Name)

		// now re-apply with the new client to mimic re-reconciliation and check the ports are fixed
		require.NoError(t, newctl.Apply(t.Context(), finalSet))
		require.NoError(t, k8sClient.Get(t.Context(), client.ObjectKeyFromObject(set), set))

		ports = set.Spec.Template.Spec.Containers[0].Ports
		require.Len(t, ports, 1)
		require.Equal(t, "test", ports[0].Name)
		require.Equal(t, int32(18080), ports[0].ContainerPort)
	}

	{
		// Console portion of the test

		// Add a junk deployment associated with the Console using the old client
		deploy := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "fieldmanagers-test-console",
				Namespace: "default",
				Labels:    consoleOwnershipLabels(console),
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: redpandav1alpha2.GroupVersion.String(),
					Kind:       "Console",
					Name:       console.Name,
					UID:        console.UID,
				}},
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: ptr.To[int32](1),
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "test-console",
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app": "test-console",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "test",
								Image: "test",
								Ports: []corev1.ContainerPort{
									{
										Name:          "test",
										ContainerPort: 8080,
									},
								},
							},
						},
					},
				},
			},
		}
		newDeploy := deploy.DeepCopy()
		// now change the port to have things get merged (different validation that stateful sets)
		newDeploy.Spec.Template.Spec.Containers[0].Ports = []corev1.ContainerPort{{
			Name:          "other",
			ContainerPort: 18080,
		}}
		// create one more for later
		finalDeploy := newDeploy.DeepCopy()

		require.NoError(t, oldctl.Apply(t.Context(), deploy))

		// check the initial field managers
		require.NoError(t, k8sClient.Get(t.Context(), client.ObjectKeyFromObject(deploy), deploy))
		managers := getFieldManagers(deploy)
		t.Logf("Initial field managers: %+v", managers)
		require.True(t, slices.Contains(managers, "*kube.Ctl"))

		// and apply with the new client
		require.NoError(t, newctl.Apply(t.Context(), newDeploy))
		require.NoError(t, k8sClient.Get(t.Context(), client.ObjectKeyFromObject(deploy), deploy))

		managers = getFieldManagers(deploy)
		t.Logf("Updated field managers: %+v", managers)
		require.True(t, slices.Contains(managers, "*kube.Ctl"))
		require.True(t, slices.Contains(managers, "new"))

		// also check the merged port
		ports := deploy.Spec.Template.Spec.Containers[0].Ports
		require.Len(t, ports, 2)
		require.Equal(t, "test", ports[0].Name)
		require.Equal(t, "other", ports[1].Name)

		// now run the migration
		require.NoError(t, migrateFieldManagers(t.Context(), newctl, k8sClient))

		// verify the field managers are updated
		require.NoError(t, k8sClient.Get(t.Context(), client.ObjectKeyFromObject(deploy), deploy))
		managers = getFieldManagers(deploy)
		t.Logf("Migrated field managers: %+v", managers)
		require.False(t, slices.Contains(managers, "*kube.Ctl"))
		require.True(t, slices.Contains(managers, "new"))

		// verify the ports are still messed up (since we just re-applied what was already there)
		require.NoError(t, k8sClient.Get(t.Context(), client.ObjectKeyFromObject(deploy), deploy))
		ports = deploy.Spec.Template.Spec.Containers[0].Ports
		require.Len(t, ports, 2)
		require.Equal(t, "test", ports[0].Name)
		require.Equal(t, "other", ports[1].Name)

		// now re-apply with the new client to mimic re-reconciliation and check the ports are fixed
		require.NoError(t, newctl.Apply(t.Context(), finalDeploy))
		require.NoError(t, k8sClient.Get(t.Context(), client.ObjectKeyFromObject(deploy), deploy))

		ports = deploy.Spec.Template.Spec.Containers[0].Ports
		require.Len(t, ports, 1)
		require.Equal(t, "other", ports[0].Name)
		require.Equal(t, int32(18080), ports[0].ContainerPort)
	}
}

func TestFieldManagersRegression(t *testing.T) {
	scheme := controller.UnifiedScheme
	config := kubetest.NewEnv(t).RestConfig()

	helmctl, err := kube.FromRESTConfig(config, kube.Options{
		Options: client.Options{
			Scheme: scheme,
		},
		FieldManager: "helm-controller",
	})
	require.NoError(t, err)

	operatorctl, err := kube.FromRESTConfig(config, kube.Options{
		Options: client.Options{
			Scheme: scheme,
		},
		FieldManager: "new",
	})
	require.NoError(t, err)

	k8sClient, err := client.New(config, client.Options{Scheme: scheme})
	require.NoError(t, err)

	// install our CRDs
	require.NoError(t, kube.ApplyAll(t.Context(), helmctl, crds.All()...))
	for _, crd := range crds.All() {
		require.NoError(t, kube.WaitFor(t.Context(), helmctl, crd.DeepCopy(), func(ext *apiextensionsv1.CustomResourceDefinition, err error) (bool, error) {
			for _, cond := range ext.Status.Conditions {
				if cond.Type == apiextensionsv1.Established && cond.Status == apiextensionsv1.ConditionTrue {
					return true, nil
				}
			}
			return false, nil
		}))
	}

	// Create a Redpanda cluster for ownership
	cluster := &redpandav1alpha2.Redpanda{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "regression-test",
			Namespace: "default",
		},
		Spec: redpandav1alpha2.RedpandaSpec{
			ClusterSpec: &redpandav1alpha2.RedpandaClusterSpec{
				Statefulset: &redpandav1alpha2.Statefulset{
					Replicas: ptr.To(1),
				},
			},
		},
	}
	require.NoError(t, k8sClient.Create(t.Context(), cluster))
	require.NoError(t, k8sClient.Get(t.Context(), client.ObjectKeyFromObject(cluster), cluster))

	// Create a StatefulSet with an exec liveness probe using the helm-controller field manager,
	// simulating a Redpanda instance deployed via embedded Flux.
	set := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "regression-test",
			Namespace: "default",
			Labels:    lifecycle.NewV2OwnershipResolver().GetOwnerLabels(&lifecycle.ClusterWithPools{Redpanda: cluster}),
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: redpandav1alpha2.GroupVersion.String(),
				Kind:       "Redpanda",
				Name:       cluster.Name,
				UID:        cluster.UID,
			}},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: ptr.To[int32](1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "regression-test",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "regression-test",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "redpanda",
							Image: "redpanda:latest",
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									Exec: &corev1.ExecAction{
										Command: []string{"/bin/sh", "-c", "echo healthy"},
									},
								},
								InitialDelaySeconds: 10,
								PeriodSeconds:       10,
							},
						},
					},
				},
			},
		},
	}
	require.NoError(t, helmctl.Apply(t.Context(), set))

	// Verify initial state: helm-controller owns the fields, exec probe is set
	require.NoError(t, k8sClient.Get(t.Context(), client.ObjectKeyFromObject(set), set))
	managers := getFieldManagers(set)
	t.Logf("Initial field managers: %+v", managers)
	require.True(t, slices.Contains(managers, "helm-controller"))
	require.NotNil(t, set.Spec.Template.Spec.Containers[0].LivenessProbe.Exec)
	require.Nil(t, set.Spec.Template.Spec.Containers[0].LivenessProbe.TCPSocket)

	// Now the new operator applies the same StatefulSet but with a TCP liveness probe
	// instead of exec. Due to the conflicting field manager, SSA will merge both probes.
	newSet := set.DeepCopy()
	newSet.Spec.Template.Spec.Containers[0].LivenessProbe = &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			TCPSocket: &corev1.TCPSocketAction{
				Port: intstr.FromInt32(9644),
			},
		},
		InitialDelaySeconds: 10,
		PeriodSeconds:       10,
	}
	finalSet := newSet.DeepCopy()

	require.NoError(t, operatorctl.Apply(t.Context(), newSet))
	require.NoError(t, k8sClient.Get(t.Context(), client.ObjectKeyFromObject(set), set))

	managers = getFieldManagers(set)
	t.Logf("After new operator apply, field managers: %+v", managers)
	require.True(t, slices.Contains(managers, "helm-controller"))
	require.True(t, slices.Contains(managers, "new"))

	// This is the regression: both exec AND tcpSocket are present on the probe
	// because helm-controller still owns the exec field and it gets merged.
	probe := set.Spec.Template.Spec.Containers[0].LivenessProbe
	require.NotNil(t, probe.Exec, "exec probe should still be present due to field manager conflict")
	require.NotNil(t, probe.TCPSocket, "tcp probe should also be present due to merge")

	// Run the field manager migration
	require.NoError(t, migrateFieldManagers(t.Context(), operatorctl, k8sClient))

	// Verify helm-controller field manager is removed
	require.NoError(t, k8sClient.Get(t.Context(), client.ObjectKeyFromObject(set), set))
	managers = getFieldManagers(set)
	t.Logf("After migration, field managers: %+v", managers)
	require.False(t, slices.Contains(managers, "helm-controller"))
	require.True(t, slices.Contains(managers, "new"))

	// Re-apply with the new operator to reconcile — now with only one field manager,
	// the exec probe should be properly removed.
	require.NoError(t, operatorctl.Apply(t.Context(), finalSet))
	require.NoError(t, k8sClient.Get(t.Context(), client.ObjectKeyFromObject(set), set))

	probe = set.Spec.Template.Spec.Containers[0].LivenessProbe
	require.Nil(t, probe.Exec, "exec probe should be gone after migration + re-apply")
	require.NotNil(t, probe.TCPSocket, "tcp probe should be the only one remaining")
	require.Equal(t, intstr.FromInt32(9644), probe.TCPSocket.Port)
}

// TestFieldManagersSkipsForbiddenListTypes verifies that a Forbidden error
// while listing a swept resource type skips that type (warning once) instead
// of failing the migration, and that the sweep continues on to later types.
func TestFieldManagersSkipsForbiddenListTypes(t *testing.T) {
	scheme := controller.UnifiedScheme
	config := kubetest.NewEnv(t).RestConfig()

	oldctl, err := kube.FromRESTConfig(config, kube.Options{
		Options: client.Options{
			Scheme: scheme,
		},
		FieldManager: "*kube.Ctl",
	})
	require.NoError(t, err)

	newctl, err := kube.FromRESTConfig(config, kube.Options{
		Options: client.Options{
			Scheme: scheme,
		},
		FieldManager: "new",
	})
	require.NoError(t, err)

	k8sClient, err := client.NewWithWatch(config, client.Options{Scheme: scheme})
	require.NoError(t, err)

	installCRDs(t, oldctl)

	// Two clusters so the forbidden type is swept twice, proving the warning
	// dedupes to one line per type.
	clusterA := newTestCluster(t, k8sClient, "forbidden-list-a")
	newTestCluster(t, k8sClient, "forbidden-list-b")

	// A StatefulSet owned by clusterA carrying an undesired field manager
	// alongside the new one (a resource whose only manager is the undesired
	// one can't be cleared by the migration's update: the server ignores an
	// empty managedFields list in update requests). Deployments sort before
	// StatefulSets in the swept type list, so a migrated StatefulSet proves
	// the sweep continued past the denial.
	set := newTestStatefulSet(clusterA)
	require.NoError(t, oldctl.Apply(t.Context(), set))
	require.NoError(t, newctl.Apply(t.Context(), newTestStatefulSet(clusterA)))

	denied := interceptor.NewClient(k8sClient, interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, ok := list.(*appsv1.DeploymentList); ok {
				return apierrors.NewForbidden(schema.GroupResource{Group: "apps", Resource: "deployments"}, "", errors.New("test denies listing deployments"))
			}
			return c.List(ctx, list, opts...)
		},
	})

	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	require.NoError(t, migrateFieldManagers(t.Context(), newctl, denied))
	log.SetOutput(os.Stderr)

	// The sweep continued past the forbidden Deployment list: the
	// StatefulSet's undesired manager was migrated.
	require.NoError(t, k8sClient.Get(t.Context(), client.ObjectKeyFromObject(set), set))
	managers := getFieldManagers(set)
	require.False(t, slices.Contains(managers, "*kube.Ctl"))
	require.True(t, slices.Contains(managers, "new"))

	// Two clusters swept the denied type; the warning fired exactly once.
	require.Equal(t, 1, strings.Count(logs.String(), "skipping Deployment.apps"), "logs:\n%s", logs.String())
}

// TestFieldManagersForbiddenCRListIsFatal verifies that Forbidden while
// listing the Redpanda CRs themselves still fails the migration.
func TestFieldManagersForbiddenCRListIsFatal(t *testing.T) {
	scheme := controller.UnifiedScheme
	config := kubetest.NewEnv(t).RestConfig()

	newctl, err := kube.FromRESTConfig(config, kube.Options{
		Options: client.Options{
			Scheme: scheme,
		},
		FieldManager: "new",
	})
	require.NoError(t, err)

	k8sClient, err := client.NewWithWatch(config, client.Options{Scheme: scheme})
	require.NoError(t, err)

	denied := interceptor.NewClient(k8sClient, interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, ok := list.(*redpandav1alpha2.RedpandaList); ok {
				return apierrors.NewForbidden(schema.GroupResource{Group: "cluster.redpanda.com", Resource: "redpandas"}, "", errors.New("test denies listing redpandas"))
			}
			return c.List(ctx, list, opts...)
		},
	})

	err = migrateFieldManagers(t.Context(), newctl, denied)
	require.Error(t, err)
	require.True(t, apierrors.IsForbidden(err))
}

// TestFieldManagersForbiddenUpdateContinues verifies that a Forbidden error
// while updating a found resource warns and moves on to the remaining
// resources instead of failing the migration.
func TestFieldManagersForbiddenUpdateContinues(t *testing.T) {
	scheme := controller.UnifiedScheme
	config := kubetest.NewEnv(t).RestConfig()

	oldctl, err := kube.FromRESTConfig(config, kube.Options{
		Options: client.Options{
			Scheme: scheme,
		},
		FieldManager: "*kube.Ctl",
	})
	require.NoError(t, err)

	newctl, err := kube.FromRESTConfig(config, kube.Options{
		Options: client.Options{
			Scheme: scheme,
		},
		FieldManager: "new",
	})
	require.NoError(t, err)

	k8sClient, err := client.NewWithWatch(config, client.Options{Scheme: scheme})
	require.NoError(t, err)

	installCRDs(t, oldctl)

	cluster := newTestCluster(t, k8sClient, "forbidden-update")

	// A Deployment and a StatefulSet, both owned by the cluster and both
	// carrying an undesired field manager. Deployments are swept first, so a
	// migrated StatefulSet proves the sweep continued past the denied update.
	// Apply with the old manager, then co-own with the new manager — the
	// state migration acts on. (A resource whose only manager is the
	// undesired one can't be cleared by the migration's update: the server
	// ignores an empty managedFields list in update requests.)
	deploy := newTestDeployment(cluster)
	require.NoError(t, oldctl.Apply(t.Context(), deploy))
	require.NoError(t, newctl.Apply(t.Context(), newTestDeployment(cluster)))
	set := newTestStatefulSet(cluster)
	require.NoError(t, oldctl.Apply(t.Context(), set))
	require.NoError(t, newctl.Apply(t.Context(), newTestStatefulSet(cluster)))

	denied := interceptor.NewClient(k8sClient, interceptor.Funcs{
		Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			if _, ok := obj.(*appsv1.Deployment); ok {
				return apierrors.NewForbidden(schema.GroupResource{Group: "apps", Resource: "deployments"}, obj.GetName(), errors.New("test denies updating deployments"))
			}
			return c.Update(ctx, obj, opts...)
		},
	})

	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	require.NoError(t, migrateFieldManagers(t.Context(), newctl, denied))
	log.SetOutput(os.Stderr)

	// The denied Deployment keeps its managers.
	require.NoError(t, k8sClient.Get(t.Context(), client.ObjectKeyFromObject(deploy), deploy))
	require.True(t, slices.Contains(getFieldManagers(deploy), "*kube.Ctl"))

	// The sweep continued: the StatefulSet was migrated.
	require.NoError(t, k8sClient.Get(t.Context(), client.ObjectKeyFromObject(set), set))
	managers := getFieldManagers(set)
	require.False(t, slices.Contains(managers, "*kube.Ctl"))
	require.True(t, slices.Contains(managers, "new"))

	require.Contains(t, logs.String(), "cannot migrate field managers of Deployment.apps")
}

func installCRDs(t *testing.T, ctl *kube.Ctl) {
	t.Helper()
	require.NoError(t, kube.ApplyAll(t.Context(), ctl, crds.All()...))
	for _, crd := range crds.All() {
		require.NoError(t, kube.WaitFor(t.Context(), ctl, crd.DeepCopy(), func(ext *apiextensionsv1.CustomResourceDefinition, err error) (bool, error) {
			for _, cond := range ext.Status.Conditions {
				if cond.Type == apiextensionsv1.Established && cond.Status == apiextensionsv1.ConditionTrue {
					return true, nil
				}
			}
			return false, nil
		}))
	}
}

func newTestCluster(t *testing.T, k8sClient client.Client, name string) *redpandav1alpha2.Redpanda {
	t.Helper()
	cluster := &redpandav1alpha2.Redpanda{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: redpandav1alpha2.RedpandaSpec{
			ClusterSpec: &redpandav1alpha2.RedpandaClusterSpec{
				Statefulset: &redpandav1alpha2.Statefulset{
					Replicas: ptr.To(1),
				},
			},
		},
	}
	require.NoError(t, k8sClient.Create(t.Context(), cluster))
	require.NoError(t, k8sClient.Get(t.Context(), client.ObjectKeyFromObject(cluster), cluster))
	return cluster
}

func newTestStatefulSet(cluster *redpandav1alpha2.Redpanda) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: testOwnedObjectMeta(cluster, cluster.Name+"-sts"),
		Spec: appsv1.StatefulSetSpec{
			Replicas: ptr.To[int32](1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "test",
				},
			},
			Template: testPodTemplate(),
		},
	}
}

func newTestDeployment(cluster *redpandav1alpha2.Redpanda) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: testOwnedObjectMeta(cluster, cluster.Name+"-deploy"),
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To[int32](1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "test",
				},
			},
			Template: testPodTemplate(),
		},
	}
}

func testOwnedObjectMeta(cluster *redpandav1alpha2.Redpanda, name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:      name,
		Namespace: cluster.Namespace,
		Labels:    lifecycle.NewV2OwnershipResolver().GetOwnerLabels(&lifecycle.ClusterWithPools{Redpanda: cluster}),
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: redpandav1alpha2.GroupVersion.String(),
			Kind:       "Redpanda",
			Name:       cluster.Name,
			UID:        cluster.UID,
		}},
	}
}

func testPodTemplate() corev1.PodTemplateSpec {
	return corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"app": "test",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "test",
					Image: "test",
				},
			},
		},
	}
}

func getFieldManagers(o client.Object) []string {
	managers := o.GetManagedFields()
	names := make([]string, 0, len(managers))
	for _, m := range managers {
		names = append(names, m.Manager)
	}
	return names
}
