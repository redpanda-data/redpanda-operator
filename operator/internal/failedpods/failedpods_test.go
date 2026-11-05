// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package failedpods_test

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/redpanda-data/redpanda-operator/operator/internal/failedpods"
)

func TestCandidate(t *testing.T) {
	for name, tc := range map[string]struct {
		pods    []failedpods.Pod
		reports map[string][]int
		unseen  bool
		want    bool
	}{
		"outdated and crash looping": {
			pods: []failedpods.Pod{outdated(crashLooping("pod-0"))},
			want: true,
		},
		"crash looping on its current template is kubelet's to restart": {
			pods: []failedpods.Pod{current(crashLooping("pod-0"))},
		},
		"outdated but running is left to the gated roll": {
			pods: []failedpods.Pod{outdated(running("pod-0"))},
		},
		"outdated and unschedulable, as with requests no node fits": {
			pods: []failedpods.Pod{outdated(unschedulable("pod-0"))},
			want: true,
		},
		"outdated and still starting is left to start": {
			pods: []failedpods.Pod{outdated(starting("pod-0"))},
		},
		"hung starting, alone: a kubelet restart looks the same": {
			pods: []failedpods.Pod{outdated(hung("pod-0"))},
		},
		"hung starting, its peers report it down": {
			pods:    []failedpods.Pod{outdated(hung("pod-0")), current(running("pod-1"))},
			reports: map[string][]int{"pod-1": {0}},
			want:    true,
		},
		"hung starting, a peer sees it alive: a kubelet restart reset its start": {
			pods:    []failedpods.Pod{outdated(hung("pod-0")), current(running("pod-1"))},
			reports: map[string][]int{"pod-1": {}},
		},
		"emptyDir data dir would be wiped": {
			pods: []failedpods.Pod{func() failedpods.Pod {
				p := outdated(crashLooping("pod-0"))
				p.Spec.Volumes = []corev1.Volume{{Name: "datadir", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}}
				return p
			}()},
		},
		"already terminating": {
			pods: []failedpods.Pod{terminating(outdated(crashLooping("pod-0")))},
		},
		"a scale-down is removing it": {
			pods: []failedpods.Pod{removing(outdated(crashLooping("pod-0")))},
		},
		"never joined, crash looping while its peers answer: it holds the gated roll's sync gate": {
			pods:    []failedpods.Pod{unmapped(outdated(crashLooping("pod-0"))), current(running("pod-1"))},
			reports: map[string][]int{"pod-1": {}},
			want:    true,
		},
		"never joined and unschedulable while its peers answer: the gated roll replaces it": {
			pods:    []failedpods.Pod{unmapped(outdated(unschedulable("pod-0"))), current(running("pod-1"))},
			reports: map[string][]int{"pod-1": {}},
		},
		"never joined, alone: its container decides": {
			pods: []failedpods.Pod{unmapped(outdated(unschedulable("pod-0")))},
			want: true,
		},
		"its peers report it down": {
			pods:    []failedpods.Pod{outdated(crashLooping("pod-0")), current(running("pod-1"))},
			reports: map[string][]int{"pod-1": {0}},
			want:    true,
		},
		"a peer sees its broker alive: its status is stale": {
			pods:    []failedpods.Pod{outdated(crashLooping("pod-0")), current(running("pod-1"))},
			reports: map[string][]int{"pod-1": {}},
		},
		"running peers that don't answer can't say it's down": {
			pods: []failedpods.Pod{outdated(crashLooping("pod-0")), current(running("pod-1"))},
		},
		"a peer still starting can't answer yet: the container decides": {
			pods: []failedpods.Pod{outdated(crashLooping("pod-0")), current(starting("pod-1"))},
			want: true,
		},
		"a peer stuck terminating on a node that's gone can't answer": {
			pods: []failedpods.Pod{outdated(crashLooping("pod-0")), stuckTerminating(current(running("pod-1")))},
			want: true,
		},
		"peers sharing a name can't be told apart, so neither answers": {
			pods:    []failedpods.Pod{outdated(crashLooping("pod-0")), inPool("east", running("pod-1")), inPool("west", running("pod-1"))},
			reports: map[string][]int{"pod-1": {0}},
		},
		"brokers the operator can't see may be running": {
			pods:   []failedpods.Pod{outdated(crashLooping("pod-0"))},
			unseen: true,
		},
		"a hostPath data dir is kept": {
			pods: []failedpods.Pod{func() failedpods.Pod {
				p := outdated(crashLooping("pod-0"))
				p.Spec.Volumes = []corev1.Volume{{Name: "datadir", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/mnt/redpanda"}}}}
				return p
			}()},
			want: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, failedpods.Candidate(tc.pods[0], askPeers(tc.pods, tc.reports, tc.unseen)))
		})
	}
}

func TestHold(t *testing.T) {
	for name, tc := range map[string]struct {
		pods []failedpods.Pod
		// reports maps an answering pod to the brokers it reports down.
		reports map[string][]int
		held    bool
	}{
		"converged": {
			pods:    []failedpods.Pod{current(running("pod-0")), current(running("pod-1"))},
			reports: map[string][]int{"pod-0": {}, "pod-1": {}},
		},
		"stalled upgrade: nothing else moves onto the refused template": {
			pods:    []failedpods.Pod{current(crashLooping("pod-0")), outdated(crashLooping("pod-1")), outdated(running("pod-2"))},
			reports: map[string][]int{"pod-2": {0, 1}},
			held:    true,
		},
		"image swapped back: the refused pod is replaceable": {
			pods:    []failedpods.Pod{outdated(crashLooping("pod-0")), current(running("pod-1")), current(running("pod-2"))},
			reports: map[string][]int{"pod-1": {0}, "pod-2": {0}},
		},
		"image swapped back with another broker down for its own reasons": {
			pods:    []failedpods.Pod{outdated(crashLooping("pod-0")), current(crashLooping("pod-1")), current(running("pod-2"))},
			reports: map[string][]int{"pod-2": {0, 1}},
		},
		"fix-forward: the first replacement is still starting": {
			pods:    []failedpods.Pod{current(creating("pod-0")), outdated(crashLooping("pod-1")), outdated(running("pod-2"))},
			reports: map[string][]int{"pod-2": {0, 1}},
			held:    true,
		},
		"fix-forward: the first replacement runs but its peers report it down": {
			pods:    []failedpods.Pod{current(running("pod-0")), outdated(crashLooping("pod-1")), outdated(running("pod-2"))},
			reports: map[string][]int{"pod-0": {1}, "pod-2": {0, 1}},
			held:    true,
		},
		"fix-forward: the first replacement rejoined": {
			pods:    []failedpods.Pod{current(running("pod-0")), outdated(crashLooping("pod-1")), outdated(running("pod-2"))},
			reports: map[string][]int{"pod-0": {1}, "pod-2": {1}},
		},
		"a peer back from a crash still vouches": {
			pods:    []failedpods.Pod{current(running("pod-0")), outdated(crashLooping("pod-1")), outdated(betweenCrashes("pod-2"))},
			reports: map[string][]int{"pod-0": {1}, "pod-2": {0, 1}},
			held:    true,
		},
		"running brokers that don't answer leave nothing known, as when the operator can't reach them": {
			pods: []failedpods.Pod{current(running("pod-0")), current(running("pod-1")), outdated(crashLooping("pod-2"))},
			held: true,
		},
		"a broker that answers alone can't confirm itself while the other running broker hangs": {
			pods:    []failedpods.Pod{outdated(crashLooping("pod-0")), current(running("pod-1")), current(running("pod-2"))},
			reports: map[string][]int{"pod-1": {0, 2}},
			held:    true,
		},
		"a peer that doesn't answer adds nothing": {
			pods:    []failedpods.Pod{current(running("pod-0")), current(running("pod-1")), outdated(crashLooping("pod-2")), removing(current(running("pod-3")))},
			reports: map[string][]int{"pod-0": {2}, "pod-1": {2}},
		},
		"a lone running broker is judged by its container": {
			pods: []failedpods.Pod{current(running("pod-0")), outdated(crashLooping("pod-1"))},
		},
		"a lone broker still starting isn't up": {
			pods: []failedpods.Pod{current(starting("pod-0")), outdated(crashLooping("pod-1"))},
			held: true,
		},
		"a pod stuck terminating doesn't stop a lone broker's container deciding": {
			pods: []failedpods.Pod{current(running("pod-0")), outdated(crashLooping("pod-1")), stuckTerminating(outdated(running("pod-2")))},
		},
		"a pod on the current template that can't be scheduled holds, though another is up": {
			pods:    []failedpods.Pod{outdated(crashLooping("pod-0")), current(running("pod-1")), current(unschedulable("pod-2"))},
			reports: map[string][]int{"pod-1": {0, 2}},
			held:    true,
		},
		"nor one just now": {
			pods: []failedpods.Pod{outdated(crashLooping("pod-0")), current(running("pod-1")), func() failedpods.Pod {
				p := current(unschedulable("pod-2"))
				p.Status.Conditions[0].LastTransitionTime = metav1.Now()
				return p
			}()},
			reports: map[string][]int{"pod-1": {0, 2}},
			held:    true,
		},
		"an outdated pod that can't be scheduled doesn't": {
			pods:    []failedpods.Pod{outdated(crashLooping("pod-0")), current(running("pod-1")), outdated(unschedulable("pod-2"))},
			reports: map[string][]int{"pod-1": {0, 2}},
		},
		"nor one in another pool": {
			pods:    []failedpods.Pod{outdated(crashLooping("pod-0")), current(running("pod-1")), inPool("b", current(unschedulable("pod-2")))},
			reports: map[string][]int{"pod-1": {0, 2}},
		},
		"nor one bound since, its condition written back stale": {
			pods: []failedpods.Pod{outdated(crashLooping("pod-0")), current(running("pod-1")), func() failedpods.Pod {
				p := current(unschedulable("pod-2"))
				p.Spec.NodeName = "node-1"
				return p
			}()},
			reports: map[string][]int{"pod-1": {0, 2}},
		},
		"one gated from scheduling doesn't": {
			pods: []failedpods.Pod{outdated(crashLooping("pod-0")), current(running("pod-1")), func() failedpods.Pod {
				p := current(unschedulable("pod-2"))
				p.Status.Conditions[0].Reason = corev1.PodReasonSchedulingGated
				return p
			}()},
			reports: map[string][]int{"pod-1": {0, 2}},
		},
		"one a scale-down is removing doesn't": {
			pods:    []failedpods.Pod{outdated(crashLooping("pod-0")), current(running("pod-1")), removing(current(unschedulable("pod-2")))},
			reports: map[string][]int{"pod-1": {0, 2}},
		},
		"a pod is terminating": {
			pods: []failedpods.Pod{terminating(outdated(crashLooping("pod-0"))), outdated(crashLooping("pod-1"))},
			held: true,
		},
		"a pod stuck terminating on a dead node doesn't hold": {
			pods: []failedpods.Pod{outdated(crashLooping("pod-0")), stuckTerminating(outdated(crashLooping("pod-1")))},
		},
		"nor says anything about its template": {
			pods:    []failedpods.Pod{outdated(crashLooping("pod-0")), stuckTerminating(current(running("pod-1"))), outdated(running("pod-2"))},
			reports: map[string][]int{"pod-2": {0, 1}},
		},
		"a refused template in another pool doesn't hold this one": {
			pods: []failedpods.Pod{
				inPool("a", current(crashLooping("a-0"))), inPool("a", outdated(running("a-1"))),
				inPool("b", outdated(crashLooping("b-2"))), inPool("b", current(running("b-3"))),
			},
			reports: map[string][]int{"a-1": {0, 2}, "b-3": {0, 2}},
		},
		"a broker on the current template running between crashes isn't up": {
			pods: []failedpods.Pod{outdated(crashLooping("pod-0")), current(betweenCrashes("pod-1"))},
			held: true,
		},
		"a broker that never joined says nothing of its template while others answer": {
			pods:    []failedpods.Pod{outdated(crashLooping("pod-0")), unmapped(current(running("pod-1"))), outdated(running("pod-2"))},
			reports: map[string][]int{"pod-1": {0}, "pod-2": {0}},
		},
		"nor shows it runs": {
			pods:    []failedpods.Pod{outdated(crashLooping("pod-0")), unmapped(current(running("pod-1"))), outdated(running("pod-2")), current(creating("pod-3"))},
			reports: map[string][]int{"pod-1": {0, 3}, "pod-2": {0, 3}},
			held:    true,
		},
		"nor hung waiting to join, as while the brokers held are down": {
			pods:    []failedpods.Pod{outdated(crashLooping("pod-0")), unmapped(current(hung("pod-1"))), outdated(running("pod-2"))},
			reports: map[string][]int{"pod-2": {0}},
		},
		"one that never joined and fails to start holds": {
			pods:    []failedpods.Pod{outdated(crashLooping("pod-0")), unmapped(current(imagePullFailing("pod-1"))), outdated(running("pod-2"))},
			reports: map[string][]int{"pod-2": {0}},
			held:    true,
		},
		"as one between crashes does": {
			pods:    []failedpods.Pod{outdated(crashLooping("pod-0")), unmapped(current(betweenCrashes("pod-1"))), outdated(running("pod-2"))},
			reports: map[string][]int{"pod-1": {0}, "pod-2": {0}},
			held:    true,
		},
		"a broker no answer registers can't show its template runs": {
			pods: []failedpods.Pod{outdated(crashLooping("pod-0")), unmapped(current(running("pod-1")))},
			held: true,
		},
		"a replacement still starting holds, though only a broker that never joined answers": {
			pods:    []failedpods.Pod{outdated(crashLooping("pod-0")), unmapped(current(creating("pod-1"))), unmapped(outdated(running("pod-2")))},
			reports: map[string][]int{"pod-2": {0}},
			held:    true,
		},
		"nor one that answers alone, never having joined, beside a member down on it": {
			pods:    []failedpods.Pod{current(crashLooping("pod-0")), outdated(crashLooping("pod-1")), unmapped(current(running("pod-2")))},
			reports: map[string][]int{"pod-2": {0, 1}},
			held:    true,
		},
		"a broker a scale-down is removing says nothing about its template": {
			pods: []failedpods.Pod{outdated(crashLooping("pod-0")), removing(current(crashLooping("pod-1")))},
		},
	} {
		t.Run(name, func(t *testing.T) {
			// The first outdated pod that can't start is the candidate.
			candidate := tc.pods[max(0, slices.IndexFunc(tc.pods, failedpods.MayBeCandidate))]
			require.Equal(t, tc.held, failedpods.Hold(tc.pods, candidate, askPeers(tc.pods, tc.reports, false)) != "")
		})
	}
}

func TestAskPeers(t *testing.T) {
	asked := func(pods ...failedpods.Pod) []string {
		var names []string
		var mu sync.Mutex
		failedpods.AskPeers(context.Background(), pods, false, func(_ context.Context, pod *corev1.Pod) (failedpods.Answer, error) {
			mu.Lock()
			defer mu.Unlock()
			names = append(names, pod.Name)
			return failedpods.Answer{}, nil
		})
		slices.Sort(names)
		return names
	}
	require.Equal(t, []string{"pod-1", "pod-2"}, asked(outdated(crashLooping("pod-0")), current(running("pod-1")), outdated(betweenCrashes("pod-2")), starting("pod-3"), stuckTerminating(running("pod-4"))),
		"every broker that may answer, even one between crashes; not one still starting, nor one stuck terminating")
	require.Equal(t, []string{"pod-2"}, asked(inPool("east", running("pod-1")), inPool("west", running("pod-1")), running("pod-2")),
		"not two pods sharing a name: their answers can't be told apart")

	// Pods whose broker isn't known map by address from any answer's member
	// list, so a broker that hangs costs only its own answer.
	pods := []failedpods.Pod{unmapped(outdated(crashLooping("pod-0"))), unmapped(current(running("pod-1"))), unmapped(current(running("pod-2")))}
	peers := failedpods.AskPeers(context.Background(), pods, false, func(_ context.Context, pod *corev1.Pod) (failedpods.Answer, error) {
		if pod.Name == "pod-2" {
			return failedpods.Answer{}, errors.New("hung")
		}
		return failedpods.Answer{
			Health:  rpadmin.ClusterHealthOverview{AllNodes: []int{0, 1, 2}, NodesDown: []int{0}},
			Brokers: members(pods),
		}, nil
	})
	require.True(t, peers.Up(pods[2]), "pod-1 vouches for pod-2, though pod-2 hung")
	require.False(t, peers.Up(pods[1]), "the only other running broker hung, so nothing confirms it")
	require.False(t, peers.SeenAlive(pods[0]), "pod-1 reports pod-0 down")
}

func TestAnotherDown(t *testing.T) {
	holder := current(running("pod-0"))
	for name, tc := range map[string]struct {
		pods    []failedpods.Pod
		reports map[string][]int
		unseen  bool
		want    bool
	}{
		"a peer reports another broker down": {
			pods:    []failedpods.Pod{holder, running("pod-1"), crashLooping("pod-2")},
			reports: map[string][]int{"pod-1": {2}},
			want:    true,
		},
		"a peer reports one down that no pod runs, as a dead disk's old ID": {
			pods:    []failedpods.Pod{holder, running("pod-1")},
			reports: map[string][]int{"pod-1": {7}},
			want:    true,
		},
		"a peer reports only the holder down": {
			pods:    []failedpods.Pod{holder, running("pod-1"), crashLooping("pod-2")},
			reports: map[string][]int{"pod-1": {0}},
		},
		"only the holder's own answer reports another down": {
			pods:    []failedpods.Pod{holder, running("pod-1"), crashLooping("pod-2")},
			reports: map[string][]int{"pod-0": {2}, "pod-1": {}},
		},
		"the holder's broker isn't known": {
			pods:    []failedpods.Pod{unmapped(holder), running("pod-1"), crashLooping("pod-2")},
			reports: map[string][]int{"pod-1": {2}},
		},
		"others run but none answers": {
			pods: []failedpods.Pod{holder, running("pod-1"), crashLooping("pod-2")},
		},
		"alone, the holder's own answer reports another down": {
			pods:    []failedpods.Pod{holder, crashLooping("pod-1"), starting("pod-2")},
			reports: map[string][]int{"pod-0": {1, 2}},
			want:    true,
		},
		"alone, one decommissioning is still a member": {
			pods:    []failedpods.Pod{holder, removing(crashLooping("pod-1"))},
			reports: map[string][]int{"pod-0": {1}},
			want:    true,
		},
		"alone beside a broker that never joined, which no one waits on": {
			pods:    []failedpods.Pod{holder, unmapped(crashLooping("pod-1"))},
			reports: map[string][]int{"pod-0": {}},
		},
		"alone, and the holder doesn't answer": {
			pods: []failedpods.Pod{holder, crashLooping("pod-1")},
		},
		"no other broker at all": {
			pods:    []failedpods.Pod{holder},
			reports: map[string][]int{"pod-0": {}},
		},
		"no other broker seen, but some may run unseen": {
			pods:   []failedpods.Pod{holder, crashLooping("pod-1")},
			unseen: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, askPeers(tc.pods, tc.reports, tc.unseen).AnotherDown(tc.pods[0]))
		})
	}
}

// TestWaits: one that can't be scheduled waits for another of its pool that
// can be, and is down at any point of a crash loop.
func TestWaits(t *testing.T) {
	waiting := outdated(unschedulable("pod-0"))
	backedOff := outdated(crashLooping("pod-2"))
	backedOff.Status.ContainerStatuses[0].State.Waiting.Message = "back-off 10s restarting failed container=redpanda pod=pod-2"
	backedOff.Status.ContainerStatuses[0].LastTerminationState.Terminated.FinishedAt = metav1.NewTime(time.Now().Add(-30 * time.Second))
	for name, tc := range map[string]struct {
		other failedpods.Pod
		want  bool
	}{
		"crash looping":                      {other: outdated(crashLooping("pod-2")), want: true},
		"between crashes":                    {other: outdated(betweenCrashes("pod-2")), want: true},
		"past its back-off, not yet running": {other: backedOff, want: true},
		"failing to pull its image":          {other: outdated(imagePullFailing("pod-2")), want: true},
		"running":                            {other: outdated(running("pod-2"))},
		"hung, as one waiting to join is":    {other: outdated(hung("pod-2"))},
		"stuck terminating":                  {other: stuckTerminating(outdated(crashLooping("pod-2")))},
		"on a data dir it would lose":        {other: emptyDir(outdated(crashLooping("pod-2")))},
		"on its template":                    {other: current(crashLooping("pod-2"))},
		"being removed":                      {other: removing(outdated(crashLooping("pod-2")))},
		"also unschedulable":                 {other: outdated(unschedulable("pod-2"))},
		"in another pool":                    {other: inPool("b", outdated(crashLooping("pod-2")))},
	} {
		t.Run(name, func(t *testing.T) {
			pods := []failedpods.Pod{waiting, current(running("pod-1")), tc.other}
			require.Equal(t, tc.want, failedpods.Waits(pods, waiting, func(failedpods.Pod) bool { return true }))
			require.False(t, failedpods.Waits(pods, tc.other, func(failedpods.Pod) bool { return true }), "only one that can't be scheduled waits")
		})
	}
	pods := []failedpods.Pod{waiting, outdated(crashLooping("pod-2"))}
	require.False(t, failedpods.Waits(pods, waiting, func(p failedpods.Pod) bool { return p.Name != "pod-2" }), "nor for one the caller won't replace")
}

// TestALaterList: peers asked of one list judge a pod listed later by who could
// answer then, as a pass's pools each list pods themselves: its own later
// status says only what its container does.
func TestALaterList(t *testing.T) {
	asked := []failedpods.Pod{outdated(crashLooping("pod-0")), current(starting("pod-1")), outdated(running("pod-2"))}
	peers := askPeers(asked, map[string][]int{"pod-2": {0, 1}}, false)
	later := []failedpods.Pod{asked[0], current(running("pod-1")), asked[2]}
	require.False(t, peers.Up(later[1]), "pod-1 started since, but pod-2 reported it down")
	require.NotEmpty(t, failedpods.Hold(later, later[0], peers))

	// No broker could answer when its peers were asked, and two have started
	// since: either may say the other is down.
	asked = []failedpods.Pod{outdated(crashLooping("pod-0")), current(starting("pod-1")), inPool("b", starting("pod-2"))}
	peers = askPeers(asked, nil, false)
	later = []failedpods.Pod{asked[0], current(running("pod-1")), inPool("b", running("pod-2"))}
	require.True(t, peers.Up(later[1]), "no other broker could answer: its container decides")
	require.NotEmpty(t, failedpods.Hold(later, later[0], peers), "neither was asked")
}

func TestUnchanged(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithObjects(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-0", Namespace: "ns"}}).WithStatusSubresource(&corev1.Pod{}).Build()
	var judged corev1.Pod
	require.NoError(t, c.Get(ctx, client.ObjectKey{Name: "pod-0", Namespace: "ns"}, &judged))

	started := judged.DeepCopy()
	started.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "redpanda", Started: ptr.To(true)}}
	require.NoError(t, c.Status().Update(ctx, started))
	require.True(t, apierrors.IsConflict(c.Delete(ctx, &judged, failedpods.Unchanged(&judged))), "deleted a pod whose container started since")
	require.NoError(t, c.Delete(ctx, started, failedpods.Unchanged(started)))
}

// askPeers asks pods' brokers, each reporting every pod's broker and the ones
// reports lists down; a pod missing from reports doesn't answer.
func askPeers(pods []failedpods.Pod, reports map[string][]int, unseen bool) failedpods.Peers {
	var all []int
	for _, p := range pods {
		if p.BrokerID != nil {
			all = append(all, *p.BrokerID)
		}
	}
	return failedpods.AskPeers(context.Background(), pods, unseen, func(_ context.Context, pod *corev1.Pod) (failedpods.Answer, error) {
		down, ok := reports[pod.Name]
		if !ok {
			return failedpods.Answer{}, errors.New("unreachable")
		}
		return failedpods.Answer{Health: rpadmin.ClusterHealthOverview{AllNodes: all, NodesDown: down}}, nil
	})
}

// members lists pods' brokers as a member list names them, each by the ID its
// pod name ends in.
func members(pods []failedpods.Pod) []rpadmin.Broker {
	var brokers []rpadmin.Broker
	for _, p := range pods {
		brokers = append(brokers, rpadmin.Broker{NodeID: int(p.Name[len(p.Name)-1] - '0'), InternalRPCAddress: p.Name + ".rp.ns.svc:33145"})
	}
	return brokers
}

func TestBrokerID(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "rp-1"}, Status: corev1.PodStatus{PodIP: "10.0.0.7"}}

	for name, tc := range map[string]struct {
		brokers []rpadmin.Broker
		want    *int
	}{
		"by pod name": {
			brokers: []rpadmin.Broker{{NodeID: 0, InternalRPCAddress: "rp-0.rp.ns.svc"}, {NodeID: 4, InternalRPCAddress: "rp-1.rp.ns.svc:33145"}},
			want:    ptr.To(4),
		},
		"by bare IP": {
			brokers: []rpadmin.Broker{{NodeID: 2, InternalRPCAddress: "10.0.0.7"}},
			want:    ptr.To(2),
		},
		"claimed by more than one broker": {
			brokers: []rpadmin.Broker{{NodeID: 1, InternalRPCAddress: "rp-1.a.svc"}, {NodeID: 7, InternalRPCAddress: "rp-1.b.svc"}},
		},
		"one broker under two spellings": {
			brokers: []rpadmin.Broker{{NodeID: 4, InternalRPCAddress: "rp-1.rp.ns.svc:33145"}, {NodeID: 4, InternalRPCAddress: "rp-1.rp.ns.svc.cluster.local.:33145"}},
			want:    ptr.To(4),
		},
		"never joined": {
			brokers: []rpadmin.Broker{{NodeID: 0, InternalRPCAddress: "rp-0.rp.ns.svc"}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, failedpods.BrokerID(tc.brokers, pod))
		})
	}
}

// TestPodsMappedByAddress: a pod whose broker isn't known stands for every
// broker registered at its address, seen alive if any is and up only if none
// is down.
func TestPodsMappedByAddress(t *testing.T) {
	at := func(id int, host string) rpadmin.Broker {
		return rpadmin.Broker{NodeID: id, InternalRPCAddress: host + ":33145"}
	}
	for name, tc := range map[string]struct {
		members         []rpadmin.Broker
		down            []int
		seenAlive, isUp bool
	}{
		"one broker at its address": {
			members:   []rpadmin.Broker{at(0, "pod-0.rp.ns.svc"), at(1, "pod-1.rp.ns.svc")},
			seenAlive: true,
			isUp:      true,
		},
		"one broker under two spellings": {
			members:   []rpadmin.Broker{at(0, "pod-0.rp.ns.svc"), at(0, "pod-0.rp.ns.svc.cluster.local."), at(1, "pod-1.rp.ns.svc")},
			seenAlive: true,
			isUp:      true,
		},
		"a lost disk's old ID left at its address": {
			members:   []rpadmin.Broker{at(0, "pod-0.rp.ns.svc"), at(7, "pod-0.rp.ns.svc"), at(1, "pod-1.rp.ns.svc")},
			down:      []int{7},
			seenAlive: true,
		},
		"every broker at its address down": {
			members: []rpadmin.Broker{at(0, "pod-0.rp.ns.svc"), at(7, "pod-0.rp.ns.svc"), at(1, "pod-1.rp.ns.svc")},
			down:    []int{0, 7},
		},
	} {
		t.Run(name, func(t *testing.T) {
			pods := []failedpods.Pod{unmapped(current(running("pod-0"))), unmapped(current(running("pod-1")))}
			var all []int
			for _, m := range tc.members {
				all = append(all, m.NodeID)
			}
			peers := failedpods.AskPeers(context.Background(), pods, false, func(_ context.Context, pod *corev1.Pod) (failedpods.Answer, error) {
				if pod.Name != "pod-1" {
					return failedpods.Answer{}, errors.New("unreachable")
				}
				return failedpods.Answer{Health: rpadmin.ClusterHealthOverview{AllNodes: all, NodesDown: tc.down}, Brokers: tc.members}, nil
			})
			require.Equal(t, tc.seenAlive, peers.SeenAlive(pods[0]), "seen alive")
			require.Equal(t, tc.isUp, peers.Up(pods[0]), "up")
		})
	}
}

// Pods are named <pool>-<broker ID> and map to that broker unless unmapped.
func brokerPod(name string, state corev1.ContainerState) failedpods.Pod {
	id := int(name[len(name)-1] - '0')
	return failedpods.Pod{
		Pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: corev1.PodSpec{Volumes: []corev1.Volume{{
				Name:         "datadir",
				VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "datadir-" + name}},
			}}},
			Status: corev1.PodStatus{
				Conditions:        []corev1.PodCondition{{Type: corev1.PodInitialized, Status: corev1.ConditionTrue}},
				ContainerStatuses: []corev1.ContainerStatus{{Name: "redpanda", State: state}},
			},
		},
		Pool:     "pool",
		BrokerID: &id,
	}
}

func running(name string) failedpods.Pod {
	p := brokerPod(name, corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(time.Now().Add(-time.Hour))}})
	p.Status.ContainerStatuses[0].Started = ptr.To(true)
	return p
}

// starting is a broker whose container runs but whose startup probe hasn't
// passed yet: its admin API doesn't answer until it is ready.
func starting(name string) failedpods.Pod {
	return startedAgo(name, time.Minute)
}

// hung is one whose startup probe hasn't passed in a run that long.
func hung(name string) failedpods.Pod {
	return startedAgo(name, 15*time.Minute)
}

func startedAgo(name string, d time.Duration) failedpods.Pod {
	p := running(name)
	p.Status.ContainerStatuses[0].State.Running.StartedAt = metav1.NewTime(time.Now().Add(-d))
	p.Status.ContainerStatuses[0].Started = ptr.To(false)
	return p
}

// imagePullFailing has failed to pull its image for two minutes: it never ran.
func imagePullFailing(name string) failedpods.Pod {
	p := brokerPod(name, corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}})
	p.Status.Conditions[0].LastTransitionTime = metav1.NewTime(time.Now().Add(-2 * time.Minute))
	return p
}

func creating(name string) failedpods.Pod {
	return brokerPod(name, corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ContainerCreating"}})
}

// unschedulable is a pod the scheduler hasn't placed in two minutes: nothing
// has run.
func unschedulable(name string) failedpods.Pod {
	p := brokerPod(name, corev1.ContainerState{})
	p.Status.Phase = corev1.PodPending
	p.Status.ContainerStatuses = nil
	p.Status.Conditions = []corev1.PodCondition{{
		Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: corev1.PodReasonUnschedulable,
		LastTransitionTime: metav1.NewTime(time.Now().Add(-2 * time.Minute)),
	}}
	return p
}

func crashLooping(name string) failedpods.Pod {
	p := brokerPod(name, corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}})
	now := metav1.NewTime(time.Now())
	p.Status.ContainerStatuses[0].LastTerminationState.Terminated = &corev1.ContainerStateTerminated{ExitCode: 1, StartedAt: now, FinishedAt: now}
	return p
}

// betweenCrashes is a crash-looping pod caught running, with no startup probe
// to hold it back.
func betweenCrashes(name string) failedpods.Pod {
	p := crashLooping(name)
	p.Status.ContainerStatuses[0].State = corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}
	p.Status.ContainerStatuses[0].Started = ptr.To(true)
	return p
}

func current(p failedpods.Pod) failedpods.Pod { p.Current = true; return p }

func outdated(p failedpods.Pod) failedpods.Pod { p.Current = false; return p }

func unmapped(p failedpods.Pod) failedpods.Pod { p.BrokerID = nil; return p }

func inPool(pool string, p failedpods.Pod) failedpods.Pod { p.Pool = pool; return p }

func removing(p failedpods.Pod) failedpods.Pod { p.Removing = true; return p }

func emptyDir(p failedpods.Pod) failedpods.Pod {
	p.Spec.Volumes = []corev1.Volume{{Name: "datadir", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}}
	return p
}

func terminating(p failedpods.Pod) failedpods.Pod {
	p.DeletionTimestamp = ptr.To(metav1.Now())
	return p
}

// stuckTerminating is a pod long past its deletion deadline, as on a node
// that died.
func stuckTerminating(p failedpods.Pod) failedpods.Pod {
	p.DeletionTimestamp = ptr.To(metav1.NewTime(time.Now().Add(-10 * time.Minute)))
	return p
}
