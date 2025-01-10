// Copyright 2025 Redpanda Data, Inc.
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
	"errors"
	"net/http"
	"time"

	"github.com/go-logr/logr"
)

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

func (s *Server) Start(ctx context.Context) error {
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
