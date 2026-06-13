// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package telemetry

import (
	"context"
	_ "embed"
	"time"

	"github.com/go-logr/logr"
	commontelemetry "github.com/redpanda-data/common-go/telemetry"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
)

//go:embed key.pem
var signingKey []byte

// Enabled reports whether a signing key is embedded in this build.
func Enabled() bool { return len(signingKey) > 0 }

type logrShim struct{ l logr.Logger }

func (s logrShim) Debug(msg string, kv ...any) { s.l.V(1).Info(msg, kv...) }

// Options configures a telemetry Runnable.
type Options struct {
	Endpoint        string
	ID              string
	OperatorVersion string
	Features        map[string]bool
	// Delay is the wait before the first report; zero means the shared library default (5m).
	Delay  time.Duration
	Period time.Duration
}

// Runnable adapts the shared telemetry Reporter to the controller-runtime
// manager.Runnable interface so it can be added to the manager.
type Runnable struct{ reporter *commontelemetry.Reporter }

// Engage implements the multicluster-runtime cluster-aware interface. Operator
// telemetry is scoped to the operator installation as a whole: it reports a
// single anonymous, aggregate cluster-shape record collected from the local
// manager's cache by the periodic loop in Start. There is no per-engaged-cluster
// work to perform, so this is a no-op that accepts the engagement; returning a
// non-nil error here would cause the manager to retry engaging the cluster.
func (r *Runnable) Engage(_ context.Context, _ string, _ cluster.Cluster) error {
	return nil
}

// NewRunnable builds a telemetry Runnable backed by the shared client. If no
// signing key is embedded, the underlying client is a disabled no-op. reader
// should be the manager's uncached API reader (mgr.GetAPIReader()); the
// discovery client resolves the Kubernetes server version each cycle and may be
// nil, in which case kubeVersion is omitted.
func NewRunnable(reader client.Reader, disco discovery.ServerVersionInterface, log logr.Logger, opts Options) (*Runnable, error) {
	tc, err := commontelemetry.New(commontelemetry.Config{
		Endpoint:      opts.Endpoint,
		Path:          "/kubernetes",
		UserAgent:     "RedpandaOperator/" + opts.OperatorVersion,
		SigningKeyPEM: signingKey,
	})
	if err != nil {
		return nil, err
	}

	collector := &Collector{
		Reader:          reader,
		ID:              opts.ID,
		OperatorVersion: opts.OperatorVersion,
		Features:        opts.Features,
		Discovery:       disco,
	}

	return &Runnable{reporter: &commontelemetry.Reporter{
		Client: tc,
		// Collect returns *Payload; adapt it to the Reporter's any-typed collector.
		Collector: func(ctx context.Context) (any, error) { return collector.Collect(ctx) },
		Delay:     opts.Delay,
		Period:    opts.Period,
		Logger:    logrShim{l: log},
	}}, nil
}

// Start runs the telemetry reporter until the context is cancelled.
func (r *Runnable) Start(ctx context.Context) error { return r.reporter.Run(ctx) }
