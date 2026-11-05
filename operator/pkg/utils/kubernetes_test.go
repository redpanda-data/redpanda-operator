// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package utils_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/operator/pkg/utils"
)

func TestGetPodOrdinal(t *testing.T) {
	tcs := []struct {
		podName         string
		clusterName     string
		expectedError   bool
		expectedOrdinal int32
	}{
		{"", "", true, -1},
		{"test", "", true, -1},
		{"pod-0", "pod", false, 0},
		{"pod-99", "pod", false, 99},
		{"", "unexpected longer cluster name", true, -1},
		{"test+0", "test", true, -1},
		{"without-ordinal-", "without-ordinal", true, -1},
	}

	for _, tc := range tcs {
		t.Run(fmt.Sprintf("pod %s and cluster %s", tc.podName, tc.clusterName), func(t *testing.T) {
			ordinal, err := utils.GetPodOrdinal(tc.podName, tc.clusterName)
			if tc.expectedError {
				require.Error(t, err)
			}
			require.Equal(t, tc.expectedOrdinal, ordinal)
		})
	}
}

func TestContainerFailingToStart(t *testing.T) {
	now := time.Now()
	ago := func(d time.Duration) metav1.Time { return metav1.NewTime(now.Add(-d)) }

	waiting := func(reason string) corev1.ContainerState {
		return corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: reason}}
	}
	exited := func(started, finished metav1.Time) corev1.ContainerState {
		return corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1, StartedAt: started, FinishedAt: finished}}
	}
	running := corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}
	backingOff := func(wait string) corev1.ContainerState {
		return corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{
			Reason:  "CrashLoopBackOff",
			Message: "back-off " + wait + " restarting failed container=redpanda pod=rp-0_ns(uid)",
		}}
	}

	finished := func(at metav1.Time) corev1.ContainerState {
		return corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{FinishedAt: at}}
	}

	pod := func(initialized corev1.ConditionStatus, since metav1.Time, redpanda corev1.ContainerStatus, sidecars ...corev1.ContainerStatus) *corev1.Pod {
		redpanda.Name = "redpanda"
		return &corev1.Pod{Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodInitialized, Status: initialized, LastTransitionTime: since},
				{Type: corev1.PodReady, Status: corev1.ConditionFalse},
			},
			ContainerStatuses: append([]corev1.ContainerStatus{redpanda}, sidecars...),
		}}
	}
	// Init containers named native-* restart beside the pod (native sidecars).
	withInits := func(p *corev1.Pod, inits ...corev1.ContainerStatus) *corev1.Pod {
		for _, cs := range inits {
			c := corev1.Container{Name: cs.Name}
			if strings.HasPrefix(cs.Name, "native-") {
				c.RestartPolicy = ptr.To(corev1.ContainerRestartPolicyAlways)
			}
			p.Spec.InitContainers = append(p.Spec.InitContainers, c)
			p.Status.InitContainerStatuses = append(p.Status.InitContainerStatuses, cs)
		}
		return p
	}
	initializing := corev1.ContainerStatus{State: waiting("PodInitializing")}

	for name, tc := range map[string]struct {
		pod  *corev1.Pod
		want bool
	}{
		"no status for the container": {
			pod: &corev1.Pod{},
		},
		"running but unready is a live broker": {
			pod: pod(corev1.ConditionTrue, ago(time.Hour), corev1.ContainerStatus{State: running}),
		},
		"running long without its startup probe passing runs": {
			pod: pod(corev1.ConditionTrue, ago(time.Hour), corev1.ContainerStatus{
				State:   corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: ago(11 * time.Minute)}},
				Started: ptr.To(false),
			}),
		},
		"exited, not yet backing off": {
			pod: pod(corev1.ConditionTrue, ago(time.Hour), corev1.ContainerStatus{State: exited(ago(time.Minute), ago(time.Second))}),
		},
		"crashed again soon after a restart": {
			pod: pod(corev1.ConditionTrue, ago(time.Hour), corev1.ContainerStatus{
				State:                waiting("CrashLoopBackOff"),
				LastTerminationState: exited(ago(5*time.Second), ago(4*time.Second)),
			}),
			want: true,
		},
		"crash looping while kubelet backs off": {
			pod: pod(corev1.ConditionTrue, ago(time.Hour), corev1.ContainerStatus{
				State:                backingOff("40s"),
				LastTerminationState: exited(ago(6*time.Second), ago(5*time.Second)),
			}),
			want: true,
		},
		"crash looping past its back-off may be running again": {
			pod: pod(corev1.ConditionTrue, ago(time.Hour), corev1.ContainerStatus{
				State:                backingOff("10s"),
				LastTerminationState: exited(ago(31*time.Second), ago(30*time.Second)),
			}),
		},
		"crash looping while a restarting native sidecar flips Initialized": {
			pod: withInits(pod(corev1.ConditionFalse, ago(time.Hour), corev1.ContainerStatus{
				State:                waiting("CrashLoopBackOff"),
				LastTerminationState: exited(ago(5*time.Second), ago(4*time.Second)),
			}),
				corev1.ContainerStatus{Name: "native-mesh", State: waiting("CrashLoopBackOff")},
				corev1.ContainerStatus{Name: "init", State: finished(ago(time.Hour))}),
			want: true,
		},
		"a failing native sidecar listed last doesn't override a container that has run": {
			pod: withInits(pod(corev1.ConditionFalse, ago(time.Hour), corev1.ContainerStatus{
				State:                backingOff("10s"),
				LastTerminationState: exited(ago(31*time.Second), ago(30*time.Second)),
			}),
				corev1.ContainerStatus{Name: "init", State: finished(ago(time.Hour))},
				corev1.ContainerStatus{Name: "native-log", State: waiting("CrashLoopBackOff")}),
		},
		"backing off after a long run isn't a crash loop": {
			pod: pod(corev1.ConditionTrue, ago(30*24*time.Hour), corev1.ContainerStatus{
				State:                waiting("CrashLoopBackOff"),
				LastTerminationState: exited(ago(30*24*time.Hour), ago(5*time.Second)),
			}),
		},
		"failed before running, start reported at the epoch": {
			pod: pod(corev1.ConditionTrue, ago(time.Hour), corev1.ContainerStatus{
				State:                waiting("CrashLoopBackOff"),
				LastTerminationState: exited(metav1.Unix(0, 0), ago(time.Second)),
			}),
			want: true,
		},
		"backing off with no recorded exit": {
			pod: pod(corev1.ConditionTrue, ago(time.Hour), corev1.ContainerStatus{
				State:                waiting("CrashLoopBackOff"),
				LastTerminationState: exited(metav1.Time{}, metav1.Time{}),
			}),
		},
		"image pull failing past the window since its last exit": {
			pod: pod(corev1.ConditionTrue, ago(time.Hour), corev1.ContainerStatus{
				State:                waiting("ImagePullBackOff"),
				LastTerminationState: exited(ago(time.Hour), ago(2*time.Minute)),
			}),
			want: true,
		},
		"image pull failing within the window since its last exit": {
			pod: pod(corev1.ConditionTrue, ago(time.Hour), corev1.ContainerStatus{
				State:                waiting("ImagePullBackOff"),
				LastTerminationState: exited(ago(time.Hour), ago(10*time.Second)),
			}),
		},
		"never ran, image pull failing past the window": {
			pod:  pod(corev1.ConditionTrue, ago(2*time.Minute), corev1.ContainerStatus{State: waiting("ErrImagePull")}),
			want: true,
		},
		"image pull failing after the runtime lost the container": {
			pod: pod(corev1.ConditionTrue, ago(2*time.Minute), corev1.ContainerStatus{
				State:                waiting("ImagePullBackOff"),
				LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "ContainerStatusUnknown", ExitCode: 137}},
			}),
			want: true,
		},
		"never ran, Secret missing within the window": {
			pod: pod(corev1.ConditionTrue, ago(10*time.Second), corev1.ContainerStatus{State: waiting("CreateContainerConfigError")}),
		},
		"init container failing past the window": {
			pod: withInits(pod(corev1.ConditionFalse, ago(2*time.Minute), initializing),
				corev1.ContainerStatus{Name: "init", State: waiting("CrashLoopBackOff")}),
			want: true,
		},
		"init container failing within the window": {
			pod: withInits(pod(corev1.ConditionFalse, ago(10*time.Second), initializing),
				corev1.ContainerStatus{Name: "init", State: waiting("CrashLoopBackOff")}),
		},
		"init container failing past the window since the one ahead of it finished": {
			pod: withInits(pod(corev1.ConditionFalse, ago(time.Hour), initializing),
				corev1.ContainerStatus{Name: "chown", State: finished(ago(2 * time.Minute))},
				corev1.ContainerStatus{Name: "init", State: waiting("CrashLoopBackOff")}),
			want: true,
		},
		"init container failing just after a long init ahead of it": {
			pod: withInits(pod(corev1.ConditionFalse, ago(time.Hour), initializing),
				corev1.ContainerStatus{Name: "chown", State: finished(ago(10 * time.Second))},
				corev1.ContainerStatus{Name: "init", State: waiting("CrashLoopBackOff")}),
		},
		"init container still running, e.g. waiting on a dependency": {
			pod: withInits(pod(corev1.ConditionFalse, ago(time.Hour), initializing),
				corev1.ContainerStatus{Name: "init", State: running}),
		},
		"native sidecar crash looping while a later init container runs": {
			pod: withInits(pod(corev1.ConditionFalse, ago(time.Hour), initializing),
				corev1.ContainerStatus{Name: "native-mesh", State: waiting("CrashLoopBackOff")},
				corev1.ContainerStatus{Name: "init", State: running}),
		},
		"native sidecar failing to start holds back the init containers after it": {
			pod: withInits(pod(corev1.ConditionFalse, ago(2*time.Minute), initializing),
				corev1.ContainerStatus{Name: "native-mesh", State: waiting("ImagePullBackOff")},
				corev1.ContainerStatus{Name: "init", State: waiting("PodInitializing")}),
			want: true,
		},
		"init container failing past the window in a new sandbox, after the container ran": {
			pod: withInits(pod(corev1.ConditionFalse, ago(2*time.Minute), corev1.ContainerStatus{
				State:                waiting("PodInitializing"),
				LastTerminationState: exited(ago(time.Hour), ago(2*time.Minute)),
			}),
				corev1.ContainerStatus{Name: "init", State: waiting("CreateContainerConfigError")}),
			want: true,
		},
		"init container failing within the window in a new sandbox, after the container ran": {
			pod: withInits(pod(corev1.ConditionFalse, ago(10*time.Second), corev1.ContainerStatus{
				State:                waiting("PodInitializing"),
				LastTerminationState: exited(ago(time.Hour), ago(10*time.Second)),
			}),
				corev1.ContainerStatus{Name: "init", State: waiting("CreateContainerConfigError")}),
		},
		"never ran, image absent under pull policy Never": {
			pod:  pod(corev1.ConditionTrue, ago(2*time.Minute), corev1.ContainerStatus{State: waiting("ErrImageNeverPull")}),
			want: true,
		},
		"unschedulable past the window": {
			pod: &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodPending, Conditions: []corev1.PodCondition{
				{Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: corev1.PodReasonUnschedulable, LastTransitionTime: ago(2 * time.Minute)},
			}}},
			want: true,
		},
		"unschedulable within the window, as while a node is added": {
			pod: &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodPending, Conditions: []corev1.PodCondition{
				{Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: corev1.PodReasonUnschedulable, LastTransitionTime: ago(10 * time.Second)},
			}}},
		},
		"bound, its Unschedulable written back from a stale read": {
			pod: &corev1.Pod{Spec: corev1.PodSpec{NodeName: "node-1"}, Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				Conditions: []corev1.PodCondition{
					{Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: corev1.PodReasonUnschedulable, LastTransitionTime: ago(time.Hour)},
				},
				ContainerStatuses: []corev1.ContainerStatus{{Name: "redpanda", State: running, Started: ptr.To(true)}},
			}},
		},
		"preempting its way onto a node": {
			pod: &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodPending, NominatedNodeName: "node-1", Conditions: []corev1.PodCondition{
				{Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: corev1.PodReasonUnschedulable, LastTransitionTime: ago(2 * time.Minute)},
			}}},
		},
		"gated from scheduling": {
			pod: &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodPending, Conditions: []corev1.PodCondition{
				{Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: corev1.PodReasonSchedulingGated, LastTransitionTime: ago(time.Hour)},
			}}},
		},
		"sidecar crash looping beside a running broker": {
			pod: pod(corev1.ConditionTrue, ago(time.Hour), corev1.ContainerStatus{State: running},
				corev1.ContainerStatus{Name: "sidecar", State: waiting("CrashLoopBackOff"), LastTerminationState: exited(ago(5*time.Second), ago(4*time.Second))}),
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, utils.ContainerFailingToStart(tc.pod, "redpanda"))
		})
	}
}

func TestContainerHung(t *testing.T) {
	ago := func(d time.Duration) metav1.Time { return metav1.NewTime(time.Now().Add(-d)) }
	for name, tc := range map[string]struct {
		cs   corev1.ContainerStatus
		want bool
	}{
		"a run that long without its startup probe passing": {
			cs:   corev1.ContainerStatus{State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: ago(11 * time.Minute)}}, Started: ptr.To(false)},
			want: true,
		},
		"still starting within the window": {
			cs: corev1.ContainerStatus{State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: ago(time.Minute)}}, Started: ptr.To(false)},
		},
		"started": {
			cs: corev1.ContainerStatus{State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: ago(time.Hour)}}, Started: ptr.To(true)},
		},
		"no word on its start": {
			cs: corev1.ContainerStatus{State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: ago(time.Hour)}}},
		},
		"not running": {
			cs: corev1.ContainerStatus{State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}, Started: ptr.To(false)},
		},
	} {
		t.Run(name, func(t *testing.T) {
			tc.cs.Name = "redpanda"
			require.Equal(t, tc.want, utils.ContainerHung(&corev1.Pod{Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{tc.cs}}}, "redpanda"))
		})
	}
}

func TestCrashedRecently(t *testing.T) {
	now := time.Now()
	ago := func(d time.Duration) metav1.Time { return metav1.NewTime(now.Add(-d)) }
	lastRun := func(started, finished metav1.Time) corev1.ContainerStatus {
		return corev1.ContainerStatus{
			State:                corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
			LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1, StartedAt: started, FinishedAt: finished}},
		}
	}

	for name, tc := range map[string]struct {
		cs   corev1.ContainerStatus
		want bool
	}{
		"never crashed":                       {cs: corev1.ContainerStatus{State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}},
		"running again after a quick crash":   {cs: lastRun(ago(2*time.Minute), ago(time.Minute)), want: true},
		"quick crash long ago":                {cs: lastRun(ago(time.Hour), ago(time.Hour-time.Minute))},
		"recent crash after a long run":       {cs: lastRun(ago(30*24*time.Hour), ago(time.Minute))},
		"failed before running, recently":     {cs: lastRun(metav1.Unix(0, 0), ago(time.Minute)), want: true},
		"terminated state with no timestamps": {cs: lastRun(metav1.Time{}, metav1.Time{})},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, utils.CrashedRecently(tc.cs))
		})
	}
}
