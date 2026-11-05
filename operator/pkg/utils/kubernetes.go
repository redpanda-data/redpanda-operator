// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package utils

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

var ErrInvalidInputParameters = fmt.Errorf("invalid input parameters")

const (
	// crashLoopRunWindow mirrors kubelet's backoff reset: a run this long
	// counts as healthy, so the crash that ended it is a one-off, not a loop;
	// one this long whose startup probe hasn't passed may be hung, though
	// kubelet may wait far longer to restart it.
	crashLoopRunWindow = 10 * time.Minute
	// startFailureWindow outlasts the transient causes of a container never
	// running: registry or API server blips, a Secret not yet written, an
	// init container waiting on a dependency.
	startFailureWindow = time.Minute
)

// startFailureReasons are the waiting reasons of a container kubelet tried and
// failed to run.
var startFailureReasons = []string{
	"CrashLoopBackOff",
	"ErrImagePull",
	"ImagePullBackOff",
	"ErrImageNeverPull",
	"ImageInspectError",
	"InvalidImageName",
	"CreateContainerConfigError",
	"CreateContainerError",
	"PreCreateHookError",
	"PreStartHookError",
	"RunContainerError",
}

// IsPodReady tells if a given pod is ready looking at its status.
func IsPodReady(pod *corev1.Pod) bool {
	return pod.Status.Phase == corev1.PodRunning &&
		IsStatusPodConditionTrue(pod.Status.Conditions, corev1.ContainersReady) &&
		IsStatusPodConditionTrue(pod.Status.Conditions, corev1.PodReady) &&
		IsAllPodsStatusesReady(pod.Status.ContainerStatuses)
}

// ContainerFailingToStart reports whether the named container is down and
// cannot start on the pod's current spec: kubelet is backing off after a run
// that crashed within crashLoopRunWindow of starting, or it, or an init
// container ahead of it, has failed to run for startFailureWindow, or the pod
// has failed to schedule for that long: unbound, whatever a condition written
// back from a stale read says, and not preempting its way onto a node.
// Readiness is ignored: a broker's readiness reflects cluster health, so a
// live broker can be unready.
func ContainerFailingToStart(pod *corev1.Pod, container string) bool {
	if c := FindStatusPodCondition(pod.Status.Conditions, corev1.PodScheduled); pod.Spec.NodeName == "" && c != nil && c.Status == corev1.ConditionFalse && c.Reason == corev1.PodReasonUnschedulable {
		return pod.Status.NominatedNodeName == "" && failingFor(c.LastTransitionTime.Time)
	}
	i := slices.IndexFunc(pod.Status.ContainerStatuses, func(cs corev1.ContainerStatus) bool {
		return cs.Name == container
	})
	if i < 0 {
		return false
	}
	cs := &pod.Status.ContainerStatuses[i]
	if cs.State.Running != nil {
		return false
	}

	// Init runs before the container's first start, and again in a new
	// sandbox, as after a node reboot.
	initialized := FindStatusPodCondition(pod.Status.Conditions, corev1.PodInitialized)
	if initialized != nil && initialized.Status == corev1.ConditionFalse && initFailing(pod, initialized.LastTransitionTime.Time, hasRun(*cs)) {
		return true
	}

	if !waitingOnStartFailure(*cs) {
		return false
	}
	last := cs.LastTerminationState.Terminated
	if cs.State.Waiting.Reason == "CrashLoopBackOff" {
		if last == nil || last.FinishedAt.IsZero() {
			return false
		}
		// The runtime reports a container that failed before running as
		// started at the epoch.
		if last.StartedAt.Unix() > 0 && last.FinishedAt.Sub(last.StartedAt.Time) >= crashLoopRunWindow {
			return false
		}
		// Once the back-off kubelet reports has passed it may be running
		// again: the status stays stale until a PostStart hook returns.
		wait, ok := backOff(cs.State.Waiting)
		return !ok || time.Since(last.FinishedAt.Time) < wait
	}

	// Down since its last exit, or since init finished if it never ran.
	// Kubelet's placeholder for a container the runtime lost has no times.
	if last != nil && !last.FinishedAt.IsZero() {
		return failingFor(last.FinishedAt.Time)
	}
	if initialized != nil {
		return failingFor(initialized.LastTransitionTime.Time)
	}
	return false
}

// ContainerHung reports whether the named container has run crashLoopRunWindow
// without its startup probe passing. One status can't tell that from a kubelet
// restart, which resets Started until the probe next passes, so it takes
// another signal to act on. Without a startup probe, Started tracks Running.
func ContainerHung(pod *corev1.Pod, container string) bool {
	i := slices.IndexFunc(pod.Status.ContainerStatuses, func(cs corev1.ContainerStatus) bool {
		return cs.Name == container
	})
	if i < 0 {
		return false
	}
	cs := &pod.Status.ContainerStatuses[i]
	r := cs.State.Running
	return r != nil && cs.Started != nil && !*cs.Started && !r.StartedAt.IsZero() && time.Since(r.StartedAt.Time) >= crashLoopRunWindow
}

// CrashedRecently reports whether the container's last run lasted less than
// crashLoopRunWindow and ended within it: a crash-looping container that is
// running again between crashes.
func CrashedRecently(cs corev1.ContainerStatus) bool {
	last := cs.LastTerminationState.Terminated
	if last == nil || last.FinishedAt.IsZero() {
		return false
	}
	short := last.StartedAt.Unix() <= 0 || last.FinishedAt.Sub(last.StartedAt.Time) < crashLoopRunWindow
	return short && time.Since(last.FinishedAt.Time) < crashLoopRunWindow
}

// initFailing reports whether an init container has failed to run for
// startFailureWindow, timed from when it could first run: once the init
// container ahead of it finished, else once initialization began. A native
// sidecar holds back what follows it only until it starts, so its failure
// counts only while nothing after it, the init containers or the container
// (ran), has run.
func initFailing(pod *corev1.Pod, initStarted time.Time, ran bool) bool {
	since := initStarted
	statuses := pod.Status.InitContainerStatuses
	for i, cs := range statuses {
		if waitingOnStartFailure(cs) && (!nativeSidecar(pod, cs.Name) || !ran && !slices.ContainsFunc(statuses[i+1:], hasRun)) {
			return failingFor(since)
		}
		if t := cs.State.Terminated; t != nil && t.FinishedAt.After(since) {
			since = t.FinishedAt.Time
		}
	}
	return false
}

func hasRun(cs corev1.ContainerStatus) bool {
	return cs.State.Waiting == nil || cs.LastTerminationState.Terminated != nil
}

func nativeSidecar(pod *corev1.Pod, name string) bool {
	i := slices.IndexFunc(pod.Spec.InitContainers, func(c corev1.Container) bool { return c.Name == name })
	return i >= 0 && ptr.Deref(pod.Spec.InitContainers[i].RestartPolicy, "") == corev1.ContainerRestartPolicyAlways
}

// backOff returns the wait kubelet reports before restarting a crash-looping
// container, measured from its last exit.
func backOff(w *corev1.ContainerStateWaiting) (time.Duration, bool) {
	rest, ok := strings.CutPrefix(w.Message, "back-off ")
	if !ok {
		return 0, false
	}
	wait, err := time.ParseDuration(strings.SplitN(rest, " ", 2)[0])
	return wait, err == nil
}

func failingFor(since time.Time) bool {
	return !since.IsZero() && time.Since(since) >= startFailureWindow
}

func waitingOnStartFailure(cs corev1.ContainerStatus) bool {
	return cs.State.Waiting != nil && slices.Contains(startFailureReasons, cs.State.Waiting.Reason)
}

func GetPodOrdinal(podName, clusterName string) (int32, error) {
	// Pod name needs to have at least 2 more characters
	if len(podName) < len(clusterName)+2 {
		return -1, fmt.Errorf("pod name (%s) and cluster name (%s): %w", podName, clusterName, ErrInvalidInputParameters)
	}

	toks := strings.Split(podName, "-")
	if len(toks) < 2 {
		return -1, fmt.Errorf("malformed pod name %s", podName)
	}

	ordinalStr := toks[len(toks)-1]
	ordinal, err := strconv.ParseInt(ordinalStr, 10, 0)
	if err != nil {
		return -1, fmt.Errorf("parsing int failed (%s): %w", ordinalStr, err)
	}
	return int32(ordinal), nil
}
