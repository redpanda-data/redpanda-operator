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
	"slices"
	"testing"

	"github.com/redpanda-data/common-go/kube"
	"github.com/redpanda-data/common-go/kube/kubetest"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

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

func getFieldManagers(o client.Object) []string {
	managers := o.GetManagedFields()
	names := make([]string, 0, len(managers))
	for _, m := range managers {
		names = append(names, m.Manager)
	}
	return names
}
