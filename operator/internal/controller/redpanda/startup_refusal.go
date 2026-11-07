// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/redpanda-data/common-go/otelutil/log"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"

	redpandachart "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/chart"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/lifecycle"
	"github.com/redpanda-data/redpanda-operator/operator/internal/statuses"
)

// A broker started on a new major version refuses to start, before its Admin
// API opens, when the cluster uses enterprise features without a license, so
// only its logs say why. Once Redpanda's crash loop limit trips, later starts
// stop ahead of the license check; the reason then survives only in the crash
// list the limiter prints from the data directory on every start. Anchor drift
// across Redpanda versions degrades to "no evidence", never to a refusal.
const (
	// startupFailureToken precedes the exception of every failed start
	// (application.cc) and of every crash the crash tracker records.
	startupFailureToken = "Failure during startup: "
	// licenseRefusalToken opens a license refusal's feature list. The
	// periodic license nag shares it, so it counts only on a startup failure.
	licenseRefusalToken = "license is required to use the currently enabled feature(s): ("
	// crashLoopToken opens the crash loop limiter's refusal, followed by one
	// "Crash #N at <time> UTC - ..." line per recorded crash, oldest first.
	crashLoopToken      = "Crash loop detected."
	recordedCrashPrefix = "Crash #"
	recordedCrashLayout = "2006-01-02 15:04:05"
	// crashLoopResetWindow is the limiter's crash_reset_duration: it blocks
	// only while the last start it let through is this recent, so a crash
	// recorded earlier than that predates the last start and is not its
	// cause. Kills during startup record nothing, and crash reports outlive
	// clean runs, so the newest one can be weeks old.
	crashLoopResetWindow = time.Hour
	// crashLoopClockSlack absorbs skew between the broker's and kubelet's
	// clocks and the gap from container start to the limiter.
	crashLoopClockSlack = 5 * time.Minute
	// startupRefusalWindow is how long after a failed start, timed from the
	// next attempt while one runs, a broker counts as failing to start: twice
	// kubelet's 5m restart backoff cap.
	startupRefusalWindow = 10 * time.Minute
	// startupAttemptLimit is past the chart's 20m startup probe budget, after
	// which kubelet kills an attempt; a longer unstarted one is a status that
	// stopped updating, such as on a lost node.
	startupAttemptLimit = 30 * time.Minute
	// startupRefusalLogTail bounds the read of one finished start; the
	// refusal sits at or near its end.
	startupRefusalLogTail = int64(5000)
	// startupRefusalReadBudget bounds a pass's log reads together, and
	// startupRefusalReadConcurrency how many run at once: they run ahead of
	// pod management, and a pod on a dead node can stay a candidate.
	startupRefusalReadBudget      = 5 * time.Second
	startupRefusalReadConcurrency = 8
	// refusedMessageSeparator joins the refusing brokers to their features in
	// the LicenseValid message, which withCurrent reads back.
	refusedMessageSeparator = " refused to start without an Enterprise license for: "
	// eventReasonStartupRefused is the Warning event emitted when the
	// refusing brokers or their features change.
	eventReasonStartupRefused = "BrokerStartupRefused"
)

// reconcileStartupRefusals reads why brokers that keep failing to start
// failed. It only observes, so it never errors or requeues.
func (r *RedpandaReconciler) reconcileStartupRefusals(ctx context.Context, state *clusterReconciliationState, _ cluster.Cluster) (ctrl.Result, error) {
	state.refusals = collectStartupRefusals(ctx, r.LifecycleClient.GetPodLogs, state.pools.ExistingPods(), (*lifecycle.MulticlusterPod).GetName)
	state.refusalsCollected = true
	return ctrl.Result{}, nil
}

// startupRefusalInPlay reports whether this pass found a refusal or the
// current condition reports one. Only then does an aborted pass reconcile the
// license, so other clusters keep their behavior.
func (s *clusterReconciliationState) startupRefusalInPlay() bool {
	if len(s.refusals.refused) > 0 {
		return true
	}
	current := apimeta.FindStatusCondition(s.cluster.Redpanda.Status.Conditions, statuses.ClusterLicenseValid)
	return current != nil && current.Reason == string(statuses.ClusterLicenseValidReasonUpgradeRefused)
}

// setLicenseFromRefusals sets LicenseValid when brokers refused to start and
// the cluster is not licensed, returning the license status to write. base is
// the status to fold the refused features into; extra trails the refusal in
// the message.
func (r *RedpandaReconciler) setLicenseFromRefusals(state *clusterReconciliationState, cl cluster.Cluster, licenseValid bool, base *redpandav1alpha2.RedpandaLicenseStatus, extra ...string) (*redpandav1alpha2.RedpandaLicenseStatus, bool) {
	rp := state.cluster.Redpanda
	reason := statuses.ClusterLicenseValidReasonUpgradeRefused
	current := apimeta.FindStatusCondition(rp.Status.Conditions, statuses.ClusterLicenseValid)
	refusals := state.refusals.withCurrent(current, string(reason))
	// A refusal needs an unlicensed cluster, so a valid license settles it.
	if licenseValid || len(refusals.refused) == 0 {
		return nil, false
	}
	messages := refusals.licenseMessages()
	recorder := cl.GetEventRecorderFor("RedpandaReconciler") //nolint:staticcheck // TODO: migrate to GetEventRecorder (new events API)
	recordStartupRefusal(recorder, rp, current, string(reason), messages[0])
	state.status.Status.SetLicenseValid(reason, append(messages, extra...)...)
	return refusals.licenseStatus(base), true
}

// reconcileStartupRefusals reads why brokers that keep failing to start
// failed. It only observes, so it never errors or requeues.
func (r *MulticlusterReconciler) reconcileStartupRefusals(ctx context.Context, state *stretchClusterReconciliationState, _ cluster.Cluster) (ctrl.Result, error) {
	state.refusals = collectStartupRefusals(ctx, r.LifecycleClient.GetPodLogs, state.pools.ExistingPods(), func(pod *lifecycle.MulticlusterPod) string {
		if name := pod.GetCanonicalClusterName(); name != "" {
			return name + "/" + pod.GetName()
		}
		return pod.GetName()
	})
	state.refusals.unseenClusters = state.pools.UnseenClusters()
	return ctrl.Result{}, nil
}

// setLicenseFromRefusals is RedpandaReconciler.setLicenseFromRefusals for a
// StretchCluster.
func (r *MulticlusterReconciler) setLicenseFromRefusals(state *stretchClusterReconciliationState, cl cluster.Cluster, licenseValid bool, base *redpandav1alpha2.RedpandaLicenseStatus, extra ...string) (*redpandav1alpha2.RedpandaLicenseStatus, bool) {
	sc := state.cluster.StretchCluster
	reason := statuses.StretchClusterLicenseValidReasonUpgradeRefused
	current := apimeta.FindStatusCondition(statuses.GetConditions(sc), statuses.StretchClusterLicenseValid)
	refusals := state.refusals.withCurrent(current, string(reason))
	if licenseValid || len(refusals.refused) == 0 {
		return nil, false
	}
	messages := refusals.licenseMessages()
	recorder := cl.GetEventRecorderFor("MulticlusterReconciler") //nolint:staticcheck // TODO: migrate to GetEventRecorder (new events API)
	recordStartupRefusal(recorder, sc, current, string(reason), messages[0])
	state.status.StretchClusterStatus.SetLicenseValid(reason, append(messages, extra...)...)
	return refusals.licenseStatus(base), true
}

// recordStartupRefusal emits a Warning event unless the current condition
// already opens with this refusal, so it fires once per change of brokers or
// features rather than per pass or crash loop cycle.
func recordStartupRefusal(recorder record.EventRecorder, o client.Object, current *metav1.Condition, reason, refusal string) {
	if current != nil && current.Reason == reason && (current.Message == refusal || strings.HasPrefix(current.Message, refusal+"; ")) {
		return
	}
	recorder.Eventf(o, corev1.EventTypeWarning, eventReasonStartupRefused, "%s", refusal)
}

// startupRefusals is what broker logs say about failed starts this pass. It is
// recomputed every pass and never persisted: a crash-looping broker reprints
// its recorded crashes on every start, so each pass recovers the same evidence.
type startupRefusals struct {
	// refused labels the brokers that refused to start without a license.
	refused []string
	// features is the union of the enterprise features they refused over.
	features []string
	// crashLoopBlocked labels the brokers held by Redpanda's crash loop limit,
	// whatever their recorded failure.
	crashLoopBlocked []string
	// unknown labels the brokers this pass can't speak for: failing ones whose
	// logs were unreadable, and first attempts still starting, as a recreated
	// pod's is before it can refuse again.
	unknown []string
	// unseenClusters are the clusters whose pods this pass couldn't list; the
	// brokers labelled "<cluster>/<pod>" under them are unknown too.
	unseenClusters []string
}

// collectStartupRefusals reads the last finished start of every broker pod
// that is failing to start.
func collectStartupRefusals(ctx context.Context, read podLogsReader, pods []*lifecycle.MulticlusterPod, label func(*lifecycle.MulticlusterPod) string) startupRefusals {
	now := time.Now()
	var refusals startupRefusals
	var candidates []*lifecycle.MulticlusterPod
	for _, pod := range pods {
		switch {
		case startupRefusalCandidate(pod.Pod, now):
			candidates = append(candidates, pod)
		case firstAttemptStarting(pod.Pod, now):
			refusals.unknown = append(refusals.unknown, label(pod))
		}
	}

	ctx, cancel := context.WithTimeout(ctx, startupRefusalReadBudget)
	defer cancel()
	logs := make([]string, len(candidates))
	errs := make([]error, len(candidates))
	slots := make(chan struct{}, startupRefusalReadConcurrency)
	var wg sync.WaitGroup
	for i, pod := range candidates {
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			logs[i], errs[i] = read(ctx, pod, &corev1.PodLogOptions{
				Container: redpandachart.RedpandaContainerName,
				// Kubelet resolves Previous to the last terminated instance
				// whether the container is backing off or starting again.
				Previous:  true,
				TailLines: ptr.To(startupRefusalLogTail),
			})
		})
	}
	wg.Wait()

	logger := log.FromContext(ctx)
	for i, pod := range candidates {
		if errs[i] != nil {
			logger.V(log.DebugLevel).Info("broker logs unreadable; no startup refusal evidence", "pod", label(pod), "error", errs[i].Error())
			refusals.unknown = append(refusals.unknown, label(pod))
			continue
		}
		failure := parseStartupFailure(logs[i], lastStartedAt(pod.Pod))
		if len(failure.features) > 0 {
			refusals.refused = append(refusals.refused, label(pod))
			refusals.features = append(refusals.features, failure.features...)
		}
		if failure.crashLoopBlocked {
			refusals.crashLoopBlocked = append(refusals.crashLoopBlocked, label(pod))
		}
	}
	slices.Sort(refusals.refused)
	refusals.features = sortedUnique(refusals.features)
	slices.Sort(refusals.crashLoopBlocked)
	slices.Sort(refusals.unknown)
	return refusals
}

// withCurrent keeps the brokers the current refusal names that this pass can't
// speak for, so an unreadable or recreated pod doesn't flap the condition.
// Their features come from the current message, which lists them only as a
// union.
func (s startupRefusals) withCurrent(current *metav1.Condition, refusedReason string) startupRefusals {
	if current == nil || current.Reason != refusedReason || (len(s.unknown) == 0 && len(s.unseenClusters) == 0) {
		return s
	}
	head, rest, ok := strings.Cut(current.Message, refusedMessageSeparator)
	if !ok {
		return s
	}
	var kept []string
	for pod := range strings.SplitSeq(head, ", ") {
		if s.isUnknown(pod) && !slices.Contains(s.refused, pod) {
			kept = append(kept, pod)
		}
	}
	if len(kept) == 0 {
		return s
	}
	list, _, _ := strings.Cut(rest, "; ")
	features := slices.Clone(s.features)
	for feature := range strings.SplitSeq(list, ", ") {
		if feature != "" {
			features = append(features, feature)
		}
	}
	s.refused = sortedUnique(append(slices.Clone(s.refused), kept...))
	s.features = sortedUnique(features)
	return s
}

func (s startupRefusals) isUnknown(label string) bool {
	if slices.Contains(s.unknown, label) {
		return true
	}
	cluster, _, ok := strings.Cut(label, "/")
	return ok && slices.Contains(s.unseenClusters, cluster)
}

// licenseMessages returns the LicenseValid messages for the refusing brokers:
// who refused over what, then which of them the crash loop limit holds.
func (s startupRefusals) licenseMessages() []string {
	messages := []string{strings.Join(s.refused, ", ") + refusedMessageSeparator + strings.Join(s.features, ", ")}
	var blocked []string
	for _, pod := range s.refused {
		if slices.Contains(s.crashLoopBlocked, pod) {
			blocked = append(blocked, pod)
		}
	}
	if len(blocked) > 0 {
		messages = append(messages, crashLoopMessage(blocked))
	}
	return messages
}

// healthMessages returns message followed by the brokers held by the crash
// loop limit, if any.
func (s startupRefusals) healthMessages(message string) []string {
	if len(s.crashLoopBlocked) == 0 {
		return []string{message}
	}
	return []string{message, crashLoopMessage(s.crashLoopBlocked)}
}

func crashLoopMessage(pods []string) string {
	return "Redpanda's crash loop limit holds " + strings.Join(pods, ", ") + " until startup_log is deleted from the data directory or an hour passes"
}

// licenseStatus folds the refused features into status, nil when there is
// none to build on.
func (s startupRefusals) licenseStatus(status *redpandav1alpha2.RedpandaLicenseStatus) *redpandav1alpha2.RedpandaLicenseStatus {
	if status == nil {
		status = &redpandav1alpha2.RedpandaLicenseStatus{}
	}
	status.Violation = true
	status.InUseFeatures = sortedUnique(append(slices.Clone(status.InUseFeatures), s.features...))
	return status
}

func sortedUnique(values []string) []string {
	slices.Sort(values)
	return slices.Compact(values)
}

// startupRefusalCandidate reports whether the pod's redpanda container last
// exited 1 and has not started since. That holds through every phase of
// kubelet's restart cycle, so the evidence does not flap between passes.
func startupRefusalCandidate(pod *corev1.Pod, now time.Time) bool {
	cs := redpandaContainerStatus(pod)
	if cs == nil {
		return false
	}
	last := cs.LastTerminationState.Terminated
	if last == nil || last.ExitCode != 1 || ptr.Deref(cs.Started, false) {
		return false
	}
	// Timed from the attempt underway, so a slow start can't age out of it.
	since := now
	if running := cs.State.Running; running != nil && !running.StartedAt.IsZero() {
		if now.Sub(running.StartedAt.Time) >= startupAttemptLimit {
			return false
		}
		since = running.StartedAt.Time
	}
	return since.Sub(last.FinishedAt.Time) < startupRefusalWindow
}

// firstAttemptStarting reports whether the pod's redpanda container is on its
// first attempt and has neither finished nor started.
func firstAttemptStarting(pod *corev1.Pod, now time.Time) bool {
	cs := redpandaContainerStatus(pod)
	if cs != nil && (cs.LastTerminationState.Terminated != nil || ptr.Deref(cs.Started, false)) {
		return false
	}
	since := pod.CreationTimestamp.Time
	if cs != nil && cs.State.Running != nil && !cs.State.Running.StartedAt.IsZero() {
		since = cs.State.Running.StartedAt.Time
	}
	return now.Sub(since) < startupAttemptLimit
}

// lastStartedAt is when the redpanda container's last finished instance
// started, which its log describes.
func lastStartedAt(pod *corev1.Pod) time.Time {
	cs := redpandaContainerStatus(pod)
	if cs == nil || cs.LastTerminationState.Terminated == nil {
		return time.Time{}
	}
	last := cs.LastTerminationState.Terminated
	if !last.StartedAt.IsZero() {
		return last.StartedAt.Time
	}
	return last.FinishedAt.Time
}

func redpandaContainerStatus(pod *corev1.Pod) *corev1.ContainerStatus {
	i := slices.IndexFunc(pod.Status.ContainerStatuses, func(cs corev1.ContainerStatus) bool {
		return cs.Name == redpandachart.RedpandaContainerName
	})
	if i < 0 {
		return nil
	}
	return &pod.Status.ContainerStatuses[i]
}

// startupFailure is what one finished start's log says about why it failed.
type startupFailure struct {
	// features are the enterprise features the broker refused to start
	// without, sorted; empty when the failure was not a license refusal.
	features []string
	// crashLoopBlocked is set when the crash loop limit stopped the start
	// ahead of the license check; features then come from the newest
	// recorded crash, if it is the last admitted start's.
	crashLoopBlocked bool
}

// parseStartupFailure reads the log of one finished start, which began at
// startedAt.
func parseStartupFailure(logs string, startedAt time.Time) startupFailure {
	var reason string
	var blocked bool
	for line := range strings.Lines(logs) {
		switch {
		case strings.Contains(line, crashLoopToken):
			blocked = true
			reason = ""
		case blocked && strings.HasPrefix(strings.TrimSpace(line), recordedCrashPrefix):
			reason = line
		case !blocked && strings.Contains(line, startupFailureToken):
			// Once blocked, this is the limiter's own refusal.
			reason = line
		}
	}
	if blocked && !recordedSince(reason, startedAt.Add(-crashLoopResetWindow-crashLoopClockSlack)) {
		reason = ""
	}
	return startupFailure{features: refusedFeatures(reason), crashLoopBlocked: blocked}
}

// recordedSince reports whether a recorded-crash line carries a parseable time
// no earlier than since.
func recordedSince(line string, since time.Time) bool {
	_, rest, ok := strings.Cut(line, " at ")
	if !ok {
		return false
	}
	stamp, _, ok := strings.Cut(rest, " UTC")
	if !ok {
		return false
	}
	at, err := time.Parse(recordedCrashLayout, stamp)
	return err == nil && !at.Before(since)
}

// refusedFeatures returns the sorted features a license refusal lists, or nil
// when line is not one.
func refusedFeatures(line string) []string {
	if !strings.Contains(line, startupFailureToken) {
		return nil
	}
	_, rest, ok := strings.Cut(line, licenseRefusalToken)
	if !ok {
		return nil
	}
	list, _, ok := strings.Cut(rest, ")")
	if !ok {
		return nil
	}
	var features []string
	for feature := range strings.SplitSeq(list, ",") {
		if feature = strings.TrimSpace(feature); feature != "" {
			features = append(features, feature)
		}
	}
	return sortedUnique(features)
}
