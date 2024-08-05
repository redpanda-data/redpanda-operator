package pvcunbinder

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	"github.com/redpanda-data/helm-charts/pkg/testutil"
	"github.com/redpanda-data/redpanda-operator/src/go/k8s/pkg/k3d"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

func TestPVCUnbinderShouldRemediate(t *testing.T) {
	cases := []struct {
		Given        []func(*corev1.Pod)
		Should       bool
		RequeueAfter time.Duration
	}{
		{
			Given: []func(*corev1.Pod){
				ownedByStatefulSet("my-sts"),
				withPhase(corev1.PodPending),
				withUnschedulable(time.Minute, "volume node affinity"),
				withLabels(map[string]string{
					"key": "value",
				}),
			},
			Should:       true,
			RequeueAfter: 0,
		},
		{
			Given: []func(*corev1.Pod){
				ownedByStatefulSet("my-sts"),
				withPhase(corev1.PodPending),
				withUnschedulable(time.Minute, "0/10 nodes are available: 2 node(s) had untolerated taint {node.kubernetes.io/unreachable: }."),
				withLabels(map[string]string{
					"key": "value",
				}),
			},
			Should:       true,
			RequeueAfter: 0,
		},
		{
			Given: []func(*corev1.Pod){
				ownedByStatefulSet("my-sts"),
				withPhase(corev1.PodPending),
				withUnschedulable(10*time.Second, "volume node affinity"),
				withLabels(map[string]string{
					"key": "value",
				}),
			},
			Should:       true,
			RequeueAfter: 20 * time.Second,
		},
		// Permutations on the above to demonstrate that all conditions must be
		// satisfied.
		{
			Given: []func(*corev1.Pod){
				ownedByStatefulSet("my-sts"),
				withPhase(corev1.PodPending),
				// If all nodes have disappeared from the cluster, don't do anything.
				withUnschedulable(10*time.Second, "0/0 nodes are available."),
				withLabels(map[string]string{
					"key": "value",
				}),
			},
		},
		{
			Given: []func(*corev1.Pod){},
		},
		{
			Given: []func(*corev1.Pod){
				ownedByStatefulSet("my-sts"),
				withPhase(corev1.PodPending),
				withUnschedulable(time.Minute, "volume node affinity"),
			},
		},
		{
			Given: []func(*corev1.Pod){
				ownedByStatefulSet("my-sts"),
				withPhase(corev1.PodPending),
				withLabels(map[string]string{
					"key": "value",
				}),
			},
		},
	}

	r := &Reconciler{
		Timeout: 30 * time.Second,
		Selector: labels.SelectorFromSet(labels.Set{
			"key": "value",
		}),
	}

	for _, tc := range cases {
		var pod corev1.Pod
		for _, fn := range tc.Given {
			fn(&pod)
		}

		ok, requeue := r.shouldRemediate(context.Background(), &pod)
		require.Equal(t, tc.Should, ok)
		require.InDelta(t, tc.RequeueAfter, requeue, float64(500*time.Millisecond) /* Leeway so we don't have to monkey patch time.Now */)
	}
}

func TestPVCUnbinder(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := testr.New(t).V(0)
	log.SetLogger(logger)
	ctx = log.IntoContext(ctx, logger)

	cluster, err := k3d.NewCluster(t.Name())
	require.NoError(t, err)
	t.Logf("created cluster %T %q", cluster, cluster.Name)

	t.Cleanup(func() {
		if testutil.Retain() {
			t.Logf("retain flag is set; not deleting cluster %q", cluster.Name)
			return
		}
		t.Logf("Deleting cluster %q", cluster.Name)
		require.NoError(t, cluster.Cleanup())
	})

	c, err := client.New(cluster.RESTConfig(), client.Options{})
	require.NoError(t, err)

	require.NoError(t, c.Create(ctx, &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "hostpath",
			Namespace: "default",
		},
		Spec: stsSpec(map[string]string{
			"my":   "sts",
			"type": "hostpath",
		}, "hostpath"),
	}))

	require.NoError(t, c.Create(ctx, &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "local",
			Namespace: "default",
		},
		Spec: stsSpec(map[string]string{
			"my":   "sts",
			"type": "local",
		}, "local"),
	}))

	// Wait until all our replicas are up
	require.Eventually(t, func() bool {
		var pods corev1.PodList
		require.NoError(t, c.List(ctx, &pods, &client.ListOptions{
			LabelSelector: labels.SelectorFromSet(labels.Set{
				"my": "sts",
			}),
			FieldSelector: fields.SelectorFromSet(fields.Set{
				"status.phase": string(corev1.PodRunning),
			}),
		}))

		return len(pods.Items) == 6
	}, 2*time.Minute, 5*time.Second)

	var pods corev1.PodList
	require.NoError(t, c.List(ctx, &pods, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{
			"my": "sts",
		}),
		FieldSelector: fields.SelectorFromSet(fields.Set{
			"status.phase": string(corev1.PodRunning),
		}),
	}))

	// Delete a node that's hosting at least one of our Pods.
	require.NoError(t, cluster.DeleteNode(pods.Items[0].Spec.NodeName))

	// We should now have at least one Pod stuck in Pending. There are no
	// anti-affinities so multiple Pods could be on the same Node.
	require.Eventually(t, func() bool {
		var pods corev1.PodList
		if err := c.List(ctx, &pods, &client.ListOptions{
			LabelSelector: labels.SelectorFromSet(labels.Set{
				"my": "sts",
			}),
			FieldSelector: fields.SelectorFromSet(fields.Set{
				"status.phase": string(corev1.PodPending),
			}),
		}); err != nil {
			return false
		}

		return len(pods.Items) > 0
	}, time.Minute, time.Second)

	// Start up our manager
	mgr, err := manager.New(cluster.RESTConfig(), manager.Options{
		BaseContext: func() context.Context {
			return log.IntoContext(ctx, logger)
		},
	})
	require.NoError(t, err)

	r := Reconciler{Client: c}
	require.NoError(t, r.SetupWithManager(mgr))

	tgo(t, ctx, func(ctx context.Context) error {
		return mgr.Start(log.IntoContext(ctx, logger))
	})

	// No more Pods stuck in pending!
	require.Eventually(t, func() bool {
		var pods corev1.PodList
		if err := c.List(ctx, &pods, &client.ListOptions{
			LabelSelector: labels.SelectorFromSet(labels.Set{
				"my": "sts",
			}),
			FieldSelector: fields.SelectorFromSet(fields.Set{
				"status.phase": string(corev1.PodPending),
			}),
		}); err != nil {
			return false
		}

		return len(pods.Items) == 0
	}, time.Minute, time.Second)

	// Assert that we have more than 6 (The initial #) PVs.
	var pvs corev1.PersistentVolumeList
	require.NoError(t, c.List(ctx, &pvs))
	require.Greater(t, len(pvs.Items), 6, "Should have more than 6 PVs")
}

func stsSpec(labels map[string]string, volumeType string) appsv1.StatefulSetSpec { //nolint:gocritic // Shadowing is acceptable
	return appsv1.StatefulSetSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: labels,
		},
		Replicas:            ptr.To(int32(3)),
		PodManagementPolicy: appsv1.ParallelPodManagement,
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: labels,
			},
			Spec: corev1.PodSpec{
				// No grace period, kill -9 Pods immediately for speed.
				TerminationGracePeriodSeconds: ptr.To(int64(0)),
				// Don't schedule on the master/control-plane node as we're
				// going to kill a random node that these Pods schedule
				// onto.
				Affinity: &corev1.Affinity{
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{
											Key:      "node-role.kubernetes.io/master",
											Operator: corev1.NodeSelectorOpDoesNotExist,
										},
										{
											Key:      "node-role.kubernetes.io/control-plane",
											Operator: corev1.NodeSelectorOpDoesNotExist,
										},
									},
								},
							},
						},
					},
				},
				Containers: []corev1.Container{
					{
						Name:            "alpine",
						Image:           "alpine",
						ImagePullPolicy: corev1.PullIfNotPresent,
						Command:         []string{"/bin/sh", "-c", "sleep 9000"},
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "my-pvc",
								MountPath: "/vol",
							},
						},
					},
				},
			},
		},
		VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "my-pvc",
					Labels: labels,
					Annotations: map[string]string{
						"volumeType": volumeType,
					},
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{
						corev1.ReadWriteOnce,
					},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("10Mi"),
						},
					},
				},
			},
		},
	}
}

// tgo is a helper for ensuring that goroutines spawned in test cases are
// appropriately shutdown.
func tgo(t *testing.T, ctx context.Context, fn func(context.Context) error) {
	doneCh := make(chan struct{})
	ctx, cancel := context.WithCancel(ctx)

	t.Cleanup(func() {
		cancel()
		<-doneCh
	})

	go func() {
		assert.NoError(t, fn(ctx))
		close(doneCh)
	}()
}

func withLabels(lbs map[string]string) func(*corev1.Pod) {
	return func(p *corev1.Pod) {
		if p.Labels == nil {
			p.Labels = map[string]string{}
		}
		for k, v := range lbs {
			p.Labels[k] = v
		}
	}
}

func ownedByStatefulSet(name string) func(*corev1.Pod) {
	return func(p *corev1.Pod) {
		p.OwnerReferences = append(p.OwnerReferences, metav1.OwnerReference{
			APIVersion: "apps/v1",
			Controller: ptr.To(true),
			Kind:       "StatefulSet",
			Name:       name,
		})
	}
}

func withPhase(phase corev1.PodPhase) func(*corev1.Pod) {
	return func(p *corev1.Pod) {
		p.Status.Phase = phase
	}
}

func withUnschedulable(d time.Duration, message string) func(*corev1.Pod) {
	return func(p *corev1.Pod) {
		p.Status.Phase = "Pending"
		p.Status.Conditions = append(p.Status.Conditions, corev1.PodCondition{
			LastTransitionTime: metav1.Time{Time: time.Now().Add(-1 * d)},
			Message:            message,
			Reason:             "Unschedulable",
			Status:             corev1.ConditionFalse,
			Type:               corev1.PodScheduled,
		})
	}
}
