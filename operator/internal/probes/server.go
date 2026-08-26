// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package probes

import (
	"context"
	"net/http"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// DefaultBrokerProbePort is the port the sidecar's broker probe server listens
// on. It backs the chart's pod readiness probe and, since it is the only
// unauthenticated per-pod HTTP surface the operator can reach, the
// rolling-restart gate's Schema Registry check as well.
const DefaultBrokerProbePort = 8093

type Server struct {
	prober *Prober
	url    string

	logger logr.Logger

	shutdownTimeout time.Duration

	server *http.Server
}

type Config struct {
	Prober          *Prober
	ShutdownTimeout time.Duration
	Address         string
	Logger          logr.Logger
	URL             string
}

func NewServer(config Config) (*Server, error) {
	if config.Prober == nil {
		return nil, errors.New("must specify a prober")
	}

	logger := config.Logger
	if logger.IsZero() {
		logger = logr.Discard()
	}

	shutdownTimeout := config.ShutdownTimeout
	if shutdownTimeout == 0 {
		shutdownTimeout = 5 * time.Second
	}

	address := config.Address
	if address == "" {
		address = ":9999"
	}

	server := &Server{
		shutdownTimeout: shutdownTimeout,
		logger:          logger,
		prober:          config.Prober,
		url:             config.URL,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", server.HandleHealthyCheck)
	mux.HandleFunc("/readyz", server.HandleReadyCheck)
	// Deliberately NOT wired into pod readiness. Pod readiness is the wrong
	// lever for Schema Registry: the broker discovery Service sets
	// publishNotReadyAddresses because raft needs it, so failing readiness
	// wouldn't remove the pod from the Service anyway. This endpoint exists for
	// the operator's rolling-restart gate to consult per broker.
	mux.HandleFunc("/schema-registry/ready", server.HandleSchemaRegistryReadyCheck)

	server.server = &http.Server{
		Addr:    address,
		Handler: mux,
		// just some sane defaults
		ReadTimeout:       5 * time.Second,
		ReadHeaderTimeout: 2 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	return server, nil
}

func (s *Server) HandleHealthyCheck(w http.ResponseWriter, r *http.Request) {
	healthy, err := s.prober.IsClusterBrokerHealthy(r.Context(), s.url)
	if err != nil {
		s.logger.Error(err, "error running health check")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if healthy {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusBadRequest)
}

func (s *Server) HandleReadyCheck(w http.ResponseWriter, r *http.Request) {
	ready, err := s.prober.IsClusterBrokerReady(r.Context(), s.url)
	if err != nil {
		s.logger.Error(err, "error running ready check")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if ready {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusBadRequest)
}

func (s *Server) NeedLeaderElection() bool {
	// explicitly elect this as not needing leadership election
	return false
}

var _ manager.LeaderElectionRunnable = (*Server)(nil)

func (s *Server) Start(ctx context.Context) error {
	s.logger.Info("running health probe server", "address", s.server.Addr)

	shutdownServer := func() error {
		// we use the background context here since the parent context might
		// already be canceled
		ctx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
		defer cancel()

		return s.server.Shutdown(ctx)
	}

	serverExitedCh := make(chan error, 1)

	// This goroutine is responsible for starting the server.
	go func() {
		if err := s.server.ListenAndServe(); err != nil {
			if !errors.Is(err, http.ErrServerClosed) {
				s.logger.Info("server exited", "error", err)
				serverExitedCh <- err
			}
		}
		close(serverExitedCh)
	}()

	var err error

	select {
	case err = <-serverExitedCh:
	case <-ctx.Done():
		err = shutdownServer()
	}

	return err
}

// HandleSchemaRegistryReadyCheck reports whether this broker's Schema Registry
// has finished replaying _schemas.
//
// The status codes are a contract with the operator's roll gate:
//
//	200 — SR is caught up; rolling the next broker is fine.
//	503 — SR is still replaying; the roll must wait.
//	404 — there is nothing to gate on: either this broker has no Schema
//	      Registry listener, or Redpanda is older than v23.1 and has no
//	      /status/ready. The operator treats this as "proceed", so a cluster
//	      without SR is never blocked by a gate meant to protect it.
//	500 — the check itself failed. The operator fails closed on this and defers
//	      the roll, because an unknown SR state is not a safe one to roll into.
func (s *Server) HandleSchemaRegistryReadyCheck(w http.ResponseWriter, r *http.Request) {
	ready, configured, err := s.prober.SchemaRegistryReady(r.Context())
	switch {
	case err != nil:
		s.logger.Error(err, "checking schema registry readiness")
		w.WriteHeader(http.StatusInternalServerError)
	case !configured:
		w.WriteHeader(http.StatusNotFound)
	case !ready:
		w.WriteHeader(http.StatusServiceUnavailable)
	default:
		w.WriteHeader(http.StatusOK)
	}
}
