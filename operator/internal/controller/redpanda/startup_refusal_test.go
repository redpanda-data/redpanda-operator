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
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/lifecycle"
	"github.com/redpanda-data/redpanda-operator/operator/internal/statuses"
)

// The log lines are reconstructed from Redpanda's application.cc,
// feature_manager.cc, and crash_tracker/limiter.cc, not captured from a broker.
const (
	bootLine     = "INFO  2026-11-02 22:29:20,001 [shard 0:main] main - application.cc:410 - Redpanda v26.3.1-rc3\n"
	shutdownLine = "INFO  2026-11-02 22:29:28,600 [shard 0:main] main - application.cc:260 - Stopping...\n"
	nagLine      = "WARN  2026-11-02 22:29:25,000 [shard 0:main] cluster - feature_manager.cc:200 - A Redpanda Enterprise Edition license is required to use the currently enabled feature(s): (cluster_cores_over_limit). Please provide a valid license key via rpk using 'rpk cluster license set <key>', or via Redpanda Console.\n"
	bindFailure  = "ERROR 2026-11-02 22:29:28,581 [shard 0:main] main - application.cc:453 - Failure during startup: std::system_error (error system:98, bind: Address already in use)\n"
	segfault     = "Segmentation fault on shard 0."
)

func TestParseStartupFailure(t *testing.T) {
	started := time.Date(2026, 11, 2, 22, 35, 32, 0, time.UTC)
	cores := refusalMessage("cluster_cores_over_limit")
	for name, tc := range map[string]struct {
		logs     string
		features []string
		blocked  bool
	}{
		"empty": {},
		"direct refusal": {
			logs:     bootLine + refusalLine("cluster_cores_over_limit") + shutdownLine,
			features: []string{"cluster_cores_over_limit"},
		},
		"direct refusal lists several features": {
			logs:     bootLine + refusalLine("partition_auto_balancing_continuous, cluster_cores_over_limit, partition_auto_balancing_continuous") + shutdownLine,
			features: []string{"cluster_cores_over_limit", "partition_auto_balancing_continuous"},
		},
		"crash loop keeps the newest recorded refusal": {
			logs:     bootLine + crashLoopLines(recorded{started.Add(-30 * time.Minute), segfault}, recorded{started.Add(-6 * time.Minute), refusalMessage("cluster_cores_over_limit, core_balancing_continuous")}) + crashLoopExit + shutdownLine,
			features: []string{"cluster_cores_over_limit", "core_balancing_continuous"},
			blocked:  true,
		},
		"crash loop whose newest crash is not a refusal": {
			logs:    bootLine + crashLoopLines(recorded{started.Add(-30 * time.Minute), cores}, recorded{started.Add(-6 * time.Minute), segfault}) + crashLoopExit + shutdownLine,
			blocked: true,
		},
		"crash loop whose newest refusal predates the last admitted start": {
			logs:    bootLine + crashLoopLines(recorded{started.Add(-21 * 24 * time.Hour), cores}) + crashLoopExit,
			blocked: true,
		},
		"crash loop refusal at the edge of the reset window": {
			logs:     crashLoopLines(recorded{started.Add(-crashLoopResetWindow - crashLoopClockSlack + time.Second), cores}) + crashLoopExit,
			features: []string{"cluster_cores_over_limit"},
			blocked:  true,
		},
		"crash loop refusal just outside the reset window": {
			logs:    crashLoopLines(recorded{started.Add(-crashLoopResetWindow - crashLoopClockSlack - time.Second), cores}) + crashLoopExit,
			blocked: true,
		},
		"crash loop refusal with an unparseable time": {
			logs:    crashLoopHeader + " The following crashes have been recorded:\nCrash #1 at sometime UTC - Redpanda version: v26.3.1-rc3. Arch: amd64. " + cores + "\n" + crashLoopExit,
			blocked: true,
		},
		"crash loop with no recorded crashes": {
			logs:    bootLine + crashLoopHeader + " (No crash files have been recorded.)\n" + crashLoopExit,
			blocked: true,
		},
		"crash loop list elided between oldest and newest": {
			logs:     bootLine + crashLoopHeader + " The following crashes have been recorded:\n" + recordedCrash(1, recorded{started.Add(-50 * time.Minute), segfault}) + "    ...\n" + recordedCrash(12, recorded{started.Add(-2 * time.Minute), cores}) + crashLoopExit,
			features: []string{"cluster_cores_over_limit"},
			blocked:  true,
		},
		"license nag on a running broker": {
			logs: bootLine + nagLine + shutdownLine,
		},
		"license nag ahead of an unrelated startup failure": {
			logs: bootLine + nagLine + bindFailure + shutdownLine,
		},
		"unrelated startup failure": {
			logs: bootLine + bindFailure + shutdownLine,
		},
		"truncated feature list": {
			logs: bootLine + "ERROR 2026-11-02 22:29:28,581 [shard 0:main] main - application.cc:453 - Failure during startup: std::runtime_error (A Redpanda Enterprise Edition license is required to use the currently enabled feature(s): (cluster_cores_ov",
		},
		"empty feature list": {
			logs: bootLine + refusalLine(""),
		},
	} {
		t.Run(name, func(t *testing.T) {
			got := parseStartupFailure(tc.logs, started)
			assert.Equal(t, tc.features, got.features)
			assert.Equal(t, tc.blocked, got.crashLoopBlocked)
		})
	}
}

func TestStartupRefusalCandidate(t *testing.T) {
	now := time.Date(2026, 11, 2, 23, 0, 0, 0, time.UTC)
	for name, tc := range map[string]struct {
		status corev1.ContainerStatus
		want   bool
	}{
		"backing off after exit 1": {
			status: redpandaStatus(waiting(), exited(1, now.Add(-time.Minute)), nil),
			want:   true,
		},
		"starting again after exit 1": {
			status: redpandaStatus(running(now.Add(-30*time.Second)), exited(1, now.Add(-time.Minute)), ptr.To(false)),
			want:   true,
		},
		"slow start after the maximum backoff": {
			status: redpandaStatus(running(now.Add(-9*time.Minute)), exited(1, now.Add(-14*time.Minute)), ptr.To(false)),
			want:   true,
		},
		"started since": {
			status: redpandaStatus(running(now.Add(-30*time.Second)), exited(1, now.Add(-time.Minute)), ptr.To(true)),
		},
		"killed rather than refused": {
			status: redpandaStatus(waiting(), exited(137, now.Add(-time.Minute)), nil),
		},
		"waiting long after the last exit": {
			status: redpandaStatus(waiting(), exited(1, now.Add(-time.Hour)), nil),
		},
		"restarted long after the last exit": {
			status: redpandaStatus(running(now.Add(-30*time.Minute)), exited(1, now.Add(-time.Hour)), ptr.To(false)),
		},
		"unstarted past the startup probe budget": {
			status: redpandaStatus(running(now.Add(-startupAttemptLimit)), exited(1, now.Add(-startupAttemptLimit-time.Minute)), ptr.To(false)),
		},
		"never terminated": {
			status: redpandaStatus(running(now.Add(-30*time.Second)), corev1.ContainerState{}, ptr.To(false)),
		},
		"another container": {
			status: func() corev1.ContainerStatus {
				cs := redpandaStatus(waiting(), exited(1, now.Add(-time.Minute)), nil)
				cs.Name = "sidecar"
				return cs
			}(),
		},
	} {
		t.Run(name, func(t *testing.T) {
			pod := &corev1.Pod{Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{tc.status}}}
			assert.Equal(t, tc.want, startupRefusalCandidate(pod, now))
		})
	}
}

func TestFirstAttemptStarting(t *testing.T) {
	now := time.Date(2026, 11, 2, 23, 0, 0, 0, time.UTC)
	for name, tc := range map[string]struct {
		created  time.Time
		statuses []corev1.ContainerStatus
		want     bool
	}{
		"no container status yet": {
			created: now.Add(-time.Minute),
			want:    true,
		},
		"creating": {
			created:  now.Add(-time.Minute),
			statuses: []corev1.ContainerStatus{redpandaStatus(corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ContainerCreating"}}, corev1.ContainerState{}, nil)},
			want:     true,
		},
		"running, not yet started": {
			created:  now.Add(-time.Hour),
			statuses: []corev1.ContainerStatus{redpandaStatus(running(now.Add(-time.Minute)), corev1.ContainerState{}, ptr.To(false))},
			want:     true,
		},
		"started": {
			created:  now.Add(-time.Minute),
			statuses: []corev1.ContainerStatus{redpandaStatus(running(now.Add(-time.Minute)), corev1.ContainerState{}, ptr.To(true))},
		},
		"already exited once": {
			created:  now.Add(-time.Minute),
			statuses: []corev1.ContainerStatus{redpandaStatus(waiting(), exited(1, now.Add(-30*time.Second)), nil)},
		},
		"unscheduled past the startup probe budget": {
			created: now.Add(-startupAttemptLimit),
		},
		"unstarted past the startup probe budget": {
			created:  now.Add(-time.Hour),
			statuses: []corev1.ContainerStatus{redpandaStatus(running(now.Add(-startupAttemptLimit)), corev1.ContainerState{}, ptr.To(false))},
		},
	} {
		t.Run(name, func(t *testing.T) {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(tc.created)},
				Status:     corev1.PodStatus{ContainerStatuses: tc.statuses},
			}
			assert.Equal(t, tc.want, firstAttemptStarting(pod, now))
		})
	}
}

func TestCollectStartupRefusals(t *testing.T) {
	now := time.Now()
	failing := redpandaStatus(waiting(), exited(1, now.Add(-time.Minute)), nil)
	healthy := redpandaStatus(running(now.Add(-time.Hour)), corev1.ContainerState{}, ptr.To(true))
	starting := brokerPod("redpanda-5", redpandaStatus(running(now.Add(-time.Minute)), corev1.ContainerState{}, ptr.To(false)))
	starting.CreationTimestamp = metav1.NewTime(now.Add(-2 * time.Minute))
	pods := []*lifecycle.MulticlusterPod{
		brokerPod("redpanda-2", failing),
		brokerPod("redpanda-0", failing),
		brokerPod("redpanda-1", healthy),
		brokerPod("redpanda-3", failing),
		brokerPod("redpanda-4", failing),
		starting,
	}
	logs := map[string]string{
		"redpanda-0": bootLine + refusalLine("partition_auto_balancing_continuous") + shutdownLine,
		"redpanda-2": crashLoopLines(recorded{now.Add(-5 * time.Minute), refusalMessage("cluster_cores_over_limit")}) + crashLoopExit,
		"redpanda-4": crashLoopLines(recorded{now.Add(-5 * time.Minute), segfault}) + crashLoopExit,
	}

	var mu sync.Mutex
	var read []string
	reader := func(_ context.Context, pod *lifecycle.MulticlusterPod, opts *corev1.PodLogOptions) (string, error) {
		mu.Lock()
		read = append(read, pod.GetName())
		mu.Unlock()
		if opts.Container != "redpanda" || !opts.Previous || opts.TailLines == nil {
			return "", errors.Newf("must read the last finished start of the redpanda container, got %+v", opts)
		}
		l, ok := logs[pod.GetName()]
		if !ok {
			return "", errors.New("unreadable")
		}
		return l, nil
	}

	got := collectStartupRefusals(context.Background(), reader, pods, (*lifecycle.MulticlusterPod).GetName)
	want := startupRefusals{
		refused:          []string{"redpanda-0", "redpanda-2"},
		features:         []string{"cluster_cores_over_limit", "partition_auto_balancing_continuous"},
		crashLoopBlocked: []string{"redpanda-2", "redpanda-4"},
		unknown:          []string{"redpanda-3", "redpanda-5"},
	}
	assert.Equal(t, want, got)
	assert.NotContains(t, read, "redpanda-1", "a started broker's logs must not be read")
	assert.NotContains(t, read, "redpanda-5", "a first attempt has no finished start to read")

	slices.Reverse(pods)
	assert.Equal(t, want, collectStartupRefusals(context.Background(), reader, pods, (*lifecycle.MulticlusterPod).GetName), "pod order must not change the result")
}

func TestStartupRefusalsWithCurrent(t *testing.T) {
	reason := string(statuses.ClusterLicenseValidReasonUpgradeRefused)
	current := func(message string) *metav1.Condition {
		return &metav1.Condition{Reason: reason, Message: message}
	}
	refusedZero := current("redpanda-0 refused to start without an Enterprise license for: cluster_cores_over_limit; Redpanda's crash loop limit holds redpanda-0 until startup_log is deleted from the data directory or an hour passes; dial tcp 10.0.0.5:9644: connect: connection refused")
	for name, tc := range map[string]struct {
		refusals startupRefusals
		current  *metav1.Condition
		want     startupRefusals
	}{
		"nothing unknown": {
			refusals: startupRefusals{refused: []string{"redpanda-1"}, features: []string{"core_balancing_continuous"}},
			current:  refusedZero,
			want:     startupRefusals{refused: []string{"redpanda-1"}, features: []string{"core_balancing_continuous"}},
		},
		"no condition yet": {
			refusals: startupRefusals{unknown: []string{"redpanda-0"}},
			want:     startupRefusals{unknown: []string{"redpanda-0"}},
		},
		"current condition is not a refusal": {
			refusals: startupRefusals{unknown: []string{"redpanda-0"}},
			current:  &metav1.Condition{Reason: string(statuses.ClusterLicenseValidReasonNotPresent), Message: "No cluster license is present"},
			want:     startupRefusals{unknown: []string{"redpanda-0"}},
		},
		"unknown broker the refusal names is kept": {
			refusals: startupRefusals{unknown: []string{"redpanda-0"}},
			current:  refusedZero,
			want:     startupRefusals{refused: []string{"redpanda-0"}, features: []string{"cluster_cores_over_limit"}, unknown: []string{"redpanda-0"}},
		},
		"kept broker joins fresh refusals": {
			refusals: startupRefusals{refused: []string{"redpanda-1"}, features: []string{"core_balancing_continuous"}, unknown: []string{"redpanda-0"}},
			current:  refusedZero,
			want: startupRefusals{
				refused:  []string{"redpanda-0", "redpanda-1"},
				features: []string{"cluster_cores_over_limit", "core_balancing_continuous"},
				unknown:  []string{"redpanda-0"},
			},
		},
		"broker the refusal names that this pass knows about is dropped": {
			refusals: startupRefusals{unknown: []string{"redpanda-2"}},
			current:  current("redpanda-0, redpanda-1 refused to start without an Enterprise license for: cluster_cores_over_limit"),
			want:     startupRefusals{unknown: []string{"redpanda-2"}},
		},
		"broker names are matched whole": {
			refusals: startupRefusals{unknown: []string{"redpanda-1"}},
			current:  current("redpanda-10 refused to start without an Enterprise license for: cluster_cores_over_limit"),
			want:     startupRefusals{unknown: []string{"redpanda-1"}},
		},
		"unrecognized message": {
			refusals: startupRefusals{unknown: []string{"redpanda-0"}},
			current:  current("redpanda-0 refused for some other reason"),
			want:     startupRefusals{unknown: []string{"redpanda-0"}},
		},
		"empty feature list": {
			refusals: startupRefusals{unknown: []string{"redpanda-0"}},
			current:  current("redpanda-0 refused to start without an Enterprise license for: "),
			want:     startupRefusals{refused: []string{"redpanda-0"}, unknown: []string{"redpanda-0"}},
		},
		"broker in an unseen cluster is kept": {
			refusals: startupRefusals{unseenClusters: []string{"peer"}},
			current:  current("local/redpanda-0, peer/redpanda-1 refused to start without an Enterprise license for: cluster_cores_over_limit"),
			want:     startupRefusals{refused: []string{"peer/redpanda-1"}, features: []string{"cluster_cores_over_limit"}, unseenClusters: []string{"peer"}},
		},
		"unseen clusters don't cover unqualified brokers": {
			refusals: startupRefusals{unseenClusters: []string{"redpanda-0"}},
			current:  current("redpanda-0 refused to start without an Enterprise license for: cluster_cores_over_limit"),
			want:     startupRefusals{unseenClusters: []string{"redpanda-0"}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.refusals.withCurrent(tc.current, reason))
		})
	}

	// Its own messages read back unchanged.
	refusals := startupRefusals{refused: []string{"redpanda-0", "redpanda-2"}, features: []string{"cluster_cores_over_limit", "core_balancing_continuous"}, crashLoopBlocked: []string{"redpanda-2"}}
	message := strings.Join(refusals.licenseMessages(), "; ")
	unknown := startupRefusals{unknown: []string{"redpanda-0", "redpanda-2"}}
	got := unknown.withCurrent(current(message), reason)
	assert.Equal(t, refusals.refused, got.refused)
	assert.Equal(t, refusals.features, got.features)
}

func TestStartupRefusalInPlay(t *testing.T) {
	for name, tc := range map[string]struct {
		refusals startupRefusals
		reason   statuses.ClusterLicenseValidCondition
		want     bool
	}{
		"no refusal": {
			reason: statuses.ClusterLicenseValidReasonValid,
		},
		"refusal this pass": {
			refusals: startupRefusals{refused: []string{"redpanda-0"}},
			reason:   statuses.ClusterLicenseValidReasonNotPresent,
			want:     true,
		},
		"refusal reported": {
			reason: statuses.ClusterLicenseValidReasonUpgradeRefused,
			want:   true,
		},
		"unknown brokers alone": {
			refusals: startupRefusals{unknown: []string{"redpanda-0"}},
			reason:   statuses.ClusterLicenseValidReasonError,
		},
	} {
		t.Run(name, func(t *testing.T) {
			rp := &redpandav1alpha2.Redpanda{Status: redpandav1alpha2.RedpandaStatus{Conditions: []metav1.Condition{{Type: statuses.ClusterLicenseValid, Reason: string(tc.reason)}}}}
			state := &clusterReconciliationState{cluster: &lifecycle.ClusterWithPools{Redpanda: rp}, refusals: tc.refusals}
			assert.Equal(t, tc.want, state.startupRefusalInPlay())
		})
	}
}

func TestStartupRefusalMessages(t *testing.T) {
	refusals := startupRefusals{
		refused:          []string{"redpanda-0", "redpanda-2"},
		features:         []string{"cluster_cores_over_limit"},
		crashLoopBlocked: []string{"redpanda-1", "redpanda-2"},
	}
	assert.Equal(t, []string{
		"redpanda-0, redpanda-2 refused to start without an Enterprise license for: cluster_cores_over_limit",
		"Redpanda's crash loop limit holds redpanda-2 until startup_log is deleted from the data directory or an hour passes",
	}, refusals.licenseMessages())
	assert.Equal(t, []string{
		"Cluster is not healthy",
		"Redpanda's crash loop limit holds redpanda-1, redpanda-2 until startup_log is deleted from the data directory or an hour passes",
	}, refusals.healthMessages("Cluster is not healthy"))

	unblocked := startupRefusals{refused: []string{"redpanda-0"}, features: []string{"cluster_cores_over_limit"}}
	assert.Len(t, unblocked.licenseMessages(), 1)
	assert.Equal(t, []string{"Cluster is not healthy"}, unblocked.healthMessages("Cluster is not healthy"))
}

func TestStartupRefusalsLicenseStatus(t *testing.T) {
	refusals := startupRefusals{refused: []string{"redpanda-0"}, features: []string{"cluster_cores_over_limit", "core_balancing_continuous"}}
	for name, tc := range map[string]struct {
		base *redpandav1alpha2.RedpandaLicenseStatus
		want *redpandav1alpha2.RedpandaLicenseStatus
	}{
		"no status to build on": {
			want: &redpandav1alpha2.RedpandaLicenseStatus{
				Violation:     true,
				InUseFeatures: []string{"cluster_cores_over_limit", "core_balancing_continuous"},
			},
		},
		"Admin API status": {
			base: &redpandav1alpha2.RedpandaLicenseStatus{
				InUseFeatures: []string{"core_balancing_continuous", "partition_auto_balancing_continuous"},
				Expired:       ptr.To(true),
				Organization:  ptr.To("acme"),
			},
			want: &redpandav1alpha2.RedpandaLicenseStatus{
				Violation:     true,
				InUseFeatures: []string{"cluster_cores_over_limit", "core_balancing_continuous", "partition_auto_balancing_continuous"},
				Expired:       ptr.To(true),
				Organization:  ptr.To("acme"),
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, refusals.licenseStatus(tc.base))
		})
	}
}

func TestRecordStartupRefusal(t *testing.T) {
	reason := string(statuses.ClusterLicenseValidReasonUpgradeRefused)
	refusal := "redpanda-0 refused to start without an Enterprise license for: cluster_cores_over_limit"
	for name, tc := range map[string]struct {
		current *metav1.Condition
		emits   bool
	}{
		"no condition yet": {
			emits: true,
		},
		"first refusal": {
			current: &metav1.Condition{Reason: string(statuses.ClusterLicenseValidReasonNotPresent), Message: "No cluster license is present"},
			emits:   true,
		},
		"refusal already reported": {
			current: &metav1.Condition{Reason: reason, Message: refusal},
		},
		"refusal already reported as held by the crash loop limit": {
			current: &metav1.Condition{Reason: reason, Message: refusal + "; " + crashLoopMessage([]string{"redpanda-0"})},
		},
		"refusal already reported with an Admin API error": {
			current: &metav1.Condition{Reason: reason, Message: refusal + "; dial tcp 10.0.0.5:9644: connect: connection refused"},
		},
		"refused features changed": {
			current: &metav1.Condition{Reason: reason, Message: refusal + ", core_balancing_continuous"},
			emits:   true,
		},
		"refusing brokers changed": {
			current: &metav1.Condition{Reason: reason, Message: "redpanda-1 refused to start without an Enterprise license for: cluster_cores_over_limit"},
			emits:   true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			recorder := record.NewFakeRecorder(1)
			recordStartupRefusal(recorder, &redpandav1alpha2.Redpanda{}, tc.current, reason, refusal)
			if tc.emits {
				require.Len(t, recorder.Events, 1)
				assert.Equal(t, "Warning BrokerStartupRefused "+refusal, <-recorder.Events)
			} else {
				assert.Empty(t, recorder.Events)
			}
		})
	}
}

const (
	crashLoopHeader = "ERROR 2026-11-02 22:35:32,512 [shard 0:main] crash_tracker - limiter.cc:123 - Crash loop detected. Too many consecutive crashes 6, exceeded crash_loop_limit configured value 5. To recover Redpanda from this state, manually remove file at path \"/var/lib/redpanda/data/startup_log\". Crash loop automatically resets 1h after last crash or with node configuration changes."
	crashLoopExit   = "ERROR 2026-11-02 22:35:32,513 [shard 0:main] main - application.cc:453 - Failure during startup: crash_tracker::crash_loop_limit_reached (Crash loop detected, aborting startup.)\n"
)

// recorded is one crash in the limiter's list.
type recorded struct {
	at      time.Time
	message string
}

func refusalMessage(features string) string {
	return "Failure during startup: std::runtime_error (A Redpanda Enterprise Edition license is required to use the currently enabled feature(s): (" + features + "). To apply your license, downgrade this broker to the pre-upgrade version and provide a valid license key via rpk using 'rpk cluster license set <key>', or via Redpanda Console. This cluster has 3 cores across 3 brokers (0, 1, 2), more than the 2-core limit of Redpanda Community Edition.)"
}

func refusalLine(features string) string {
	return "ERROR 2026-11-02 22:29:28,581 [shard 0:main] main - application.cc:453 - " + refusalMessage(features) + "\n"
}

// crashLoopLines renders the limiter's refusal with crashes listed oldest
// first.
func crashLoopLines(crashes ...recorded) string {
	var b strings.Builder
	b.WriteString(crashLoopHeader + " The following crashes have been recorded:\n")
	for i, crash := range crashes {
		b.WriteString(recordedCrash(i+1, crash))
	}
	return b.String()
}

func recordedCrash(n int, crash recorded) string {
	return "Crash #" + strconv.Itoa(n) + " at " + crash.at.UTC().Format(recordedCrashLayout) + " UTC - Redpanda version: v26.3.1-rc3. Arch: amd64. " + crash.message + " Backtrace: 0x1 0x2.\n"
}

func brokerPod(name string, status corev1.ContainerStatus) *lifecycle.MulticlusterPod {
	return &lifecycle.MulticlusterPod{Pod: &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Status:     corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{status}},
	}}
}

func redpandaStatus(state, last corev1.ContainerState, started *bool) corev1.ContainerStatus {
	return corev1.ContainerStatus{Name: "redpanda", State: state, LastTerminationState: last, Started: started}
}

func waiting() corev1.ContainerState {
	return corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}
}

func running(started time.Time) corev1.ContainerState {
	return corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(started)}}
}

func exited(code int32, finished time.Time) corev1.ContainerState {
	return corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
		ExitCode:   code,
		StartedAt:  metav1.NewTime(finished.Add(-10 * time.Second)),
		FinishedAt: metav1.NewTime(finished),
	}}
}
