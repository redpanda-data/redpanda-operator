// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package multicluster

import (
	"context"
	"fmt"
	"io"
	"path"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/operator/cmd/rpk-k8s/k8s/multicluster/checks"
)

// LogsOptions controls per-container log retrieval. Zero values disable the
// corresponding cap.
type LogsOptions struct {
	// LimitBytes caps the number of bytes returned per container. 0 means
	// no cap (the apiserver still applies its own internal limit, which is
	// typically generous).
	LimitBytes int64
	// TailLines caps how many lines from the tail of the log are returned
	// per container. 0 means no cap.
	TailLines int64
}

// defaultLogsOptions returns the limits the bundle command applies when the
// caller doesn't override them. Values picked to bound bundle size at ~5 MiB
// per container while still being useful for incident review.
func defaultLogsOptions() LogsOptions {
	return LogsOptions{LimitBytes: 5 * 1024 * 1024, TailLines: 5000}
}

// logFetcher is the minimal abstraction over the Kubernetes log API needed
// by collectClusterLogs. Production uses a kubernetes.Interface-backed
// implementation; tests stub it because envtest's apiserver doesn't run
// kubelets and cannot return real container output.
type logFetcher interface {
	Logs(ctx context.Context, namespace, podName, container string, opts *corev1.PodLogOptions) ([]byte, error)
}

// kubeLogFetcher fetches logs via a kubernetes.Interface built from a REST
// config. It buffers the entire response (capped by the caller's
// LimitBytes/TailLines) into memory before returning so the bundle writer
// only sees a complete blob.
type kubeLogFetcher struct {
	cs kubernetes.Interface
}

// newKubeLogFetcher returns a logFetcher backed by a kubernetes.Interface
// built from cfg. cfg is the same REST config the kube.Ctl was built from.
func newKubeLogFetcher(cfg *rest.Config) (*kubeLogFetcher, error) {
	if cfg == nil {
		return nil, fmt.Errorf("newKubeLogFetcher: nil rest.Config")
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("building kubernetes clientset: %w", err)
	}
	return &kubeLogFetcher{cs: cs}, nil
}

func (k *kubeLogFetcher) Logs(ctx context.Context, namespace, podName, container string, opts *corev1.PodLogOptions) ([]byte, error) {
	// Defensive copy + container override so a caller-passed opts struct
	// can be reused across containers without surprises.
	cp := opts.DeepCopy()
	cp.Container = container
	stream, err := k.cs.CoreV1().Pods(namespace).GetLogs(podName, cp).Stream(ctx)
	if err != nil {
		return nil, err
	}
	defer stream.Close() //nolint:errcheck // best-effort close on stream
	return io.ReadAll(stream)
}

// collectClusterLogs writes per-container log files into the bundle for the
// pod recorded on cc.Pod. For every container — init and main — the function
// fetches the current log and, when the container has restarted, also the
// previous instance's log (Previous: true).
//
// Per-container fetches are independent: a failure on one container is
// recorded in the returned []error and the next container is attempted. A
// nil cc.Pod (PodCheck didn't find a pod) yields no work and no errors.
func collectClusterLogs(ctx context.Context, bw *bundleWriter, cc *checks.CheckContext, fetcher logFetcher, opts LogsOptions) []error {
	if cc == nil || cc.Pod == nil || fetcher == nil {
		return nil
	}

	restarts := containerRestartCounts(cc.Pod)

	type entry struct {
		name string
	}
	var containers []entry
	// Init containers first, matching `kubectl logs --all-containers` order
	// and making the bundle predictable when humans glance at it.
	for _, c := range cc.Pod.Spec.InitContainers {
		containers = append(containers, entry{name: c.Name})
	}
	for _, c := range cc.Pod.Spec.Containers {
		containers = append(containers, entry{name: c.Name})
	}

	var errs []error
	for _, ce := range containers {
		// Always fetch the current log; fetch previous only if the
		// container has actually restarted, otherwise the apiserver
		// returns "previous terminated container not found".
		fetches := []bool{false}
		if restarts[ce.name] > 0 {
			fetches = append(fetches, true)
		}
		for _, previous := range fetches {
			getOpts := &corev1.PodLogOptions{Previous: previous}
			if opts.LimitBytes > 0 {
				getOpts.LimitBytes = ptr.To(opts.LimitBytes)
			}
			if opts.TailLines > 0 {
				getOpts.TailLines = ptr.To(opts.TailLines)
			}

			data, err := fetcher.Logs(ctx, cc.Namespace, cc.Pod.Name, ce.name, getOpts)
			if err != nil {
				errs = append(errs, fmt.Errorf("logs for %s/%s container %s previous=%v: %w",
					cc.Pod.Namespace, cc.Pod.Name, ce.name, previous, err))
				continue
			}

			fname := ce.name + ".log"
			if previous {
				fname = ce.name + ".previous.log"
			}
			if werr := bw.writeBytes(path.Join("clusters", cc.Context, "logs", fname), data); werr != nil {
				errs = append(errs, werr)
			}
		}
	}
	return errs
}

// containerRestartCounts returns a map from container name to its restart
// count, including init containers. Used to decide whether to attempt a
// `Previous: true` fetch (the apiserver returns an error if no previous
// instance exists).
func containerRestartCounts(p *corev1.Pod) map[string]int32 {
	out := make(map[string]int32, len(p.Status.ContainerStatuses)+len(p.Status.InitContainerStatuses))
	for _, cs := range p.Status.InitContainerStatuses {
		out[cs.Name] = cs.RestartCount
	}
	for _, cs := range p.Status.ContainerStatuses {
		out[cs.Name] = cs.RestartCount
	}
	return out
}
