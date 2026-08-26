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
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/go-logr/logr"
	"github.com/redpanda-data/common-go/otelutil/log"
	corev1 "k8s.io/api/core/v1"

	"github.com/redpanda-data/redpanda-operator/operator/internal/probes"
)

const (
	// schemaRegistryReadyPath is the sidecar endpoint that reports whether that
	// broker's Schema Registry has caught up on _schemas. See
	// probes.Server.HandleSchemaRegistryReadyCheck for the status-code contract.
	schemaRegistryReadyPath = "/schema-registry/ready"

	// schemaRegistryGateRequestTimeout bounds one probe. The sidecar's own
	// request to the local SR listener is bounded at 5s, so this only needs to
	// cover that plus in-cluster latency; a longer timeout would just make a
	// wedged sidecar look like a slow one.
	schemaRegistryGateRequestTimeout = 10 * time.Second
)

// schemaRegistryGateClient is the HTTP client used to reach sidecars. Plain
// HTTP by design: the sidecar's probe server is unauthenticated and
// bound to the pod, and the operator addresses it by pod IP, so there is no
// listener TLS to reproduce here. What is behind it — the broker's own SR
// listener, TLS and all — is the sidecar's problem, which is the point of
// asking the sidecar rather than the broker.
var schemaRegistryGateClient = &http.Client{Timeout: schemaRegistryGateRequestTimeout}

// schemaRegistryStillReplaying reports whether any of the given pods has a
// Schema Registry that has not finished replaying _schemas.
//
// This is the gate that keeps an upgrade from putting every broker's SR into
// replay at once (K8S-908, INC-2903). After a broker restarts, Redpanda serves
// Kafka immediately but SR replays _schemas before its store is consistent —
// ~13 min on a large _schemas topic — returning 50005/429 the whole time while
// the broker stays in traffic rotation. Roll the next broker before the
// previous one's SR has caught up and the windows overlap; do it across a whole
// cluster and there is no consistent SR endpoint left.
//
// It deliberately does NOT make SR requests reliable: a replaying broker is
// still published and still serving errors for its own window. Fixing that
// needs service-side steering, tracked separately.
//
// Pods with no assigned IP are skipped rather than treated as unready: a pod
// that has not been scheduled yet has no SR to replay, and blocking on it would
// stall a roll on something this gate is not about.
func schemaRegistryStillReplaying(ctx context.Context, pods []*corev1.Pod, sidecarPort int, logger logr.Logger) (bool, error) {
	var firstErr error

	for _, pod := range pods {
		if pod == nil || pod.Status.PodIP == "" {
			continue
		}

		ready, gateable, err := podSchemaRegistryReady(ctx, pod, sidecarPort)
		if err != nil {
			// One unreachable sidecar must not short-circuit the scan: a
			// different pod may be confirmably still replaying, and that answer
			// is more actionable than the first error encountered. Mirrors
			// brokersStillRecovering.
			if firstErr == nil {
				firstErr = errors.Wrapf(err, "probing schema registry readiness of %s", pod.Name)
			}
			continue
		}
		if !gateable {
			// No SR listener on this broker, or a Redpanda too old to have
			// /status/ready. Nothing to wait for.
			continue
		}
		if !ready {
			logger.V(log.DebugLevel).Info("schema registry still replaying", "pod", pod.Name)
			return true, nil
		}
	}

	return false, firstErr
}

// podSchemaRegistryReady asks one pod's sidecar about its Schema Registry.
//
// gateable is false when there is nothing to gate on, which the caller treats
// as "proceed" — see the status-code contract in
// probes.Server.HandleSchemaRegistryReadyCheck. A sidecar predating that
// endpoint returns 404 from its ServeMux and lands in the same branch, so
// mixed-version fleets during an operator upgrade degrade to the old behaviour
// instead of stalling.
func podSchemaRegistryReady(ctx context.Context, pod *corev1.Pod, sidecarPort int) (ready bool, gateable bool, err error) {
	url := fmt.Sprintf("http://%s%s", net.JoinHostPort(pod.Status.PodIP, strconv.Itoa(sidecarPort)), schemaRegistryReadyPath)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, false, err
	}

	resp, err := schemaRegistryGateClient.Do(req)
	if err != nil {
		return false, false, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	switch resp.StatusCode {
	case http.StatusOK:
		return true, true, nil
	case http.StatusNotFound:
		return false, false, nil
	case http.StatusServiceUnavailable:
		return false, true, nil
	default:
		return false, false, fmt.Errorf("unexpected status %d from %s", resp.StatusCode, url)
	}
}

// SchemaRegistryGateSidecarPort is the port the sidecar's probe server listens
// on, which is where the roll gate looks for the readiness endpoint. It matches
// the chart's rendered sidecar probe port.
const SchemaRegistryGateSidecarPort = probes.DefaultBrokerProbePort
