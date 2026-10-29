// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package pipelineapi

import (
	"context"
	"net"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/console/backend/pkg/protogen/redpanda/api/dataplane/v1/dataplanev1connect"
)

// Handler serves svc, and an unauthenticated /healthz for probes.
func Handler(svc *Service, interceptors ...connect.Interceptor) http.Handler {
	mux := http.NewServeMux()
	path, h := dataplanev1connect.NewPipelineServiceHandler(svc, connect.WithInterceptors(interceptors...))
	mux.Handle(path, h)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	return mux
}

// ServeOptions configures Serve.
type ServeOptions struct {
	Address string
	// CertFile and KeyFile enable TLS. Without them the server speaks
	// HTTP/1.1 and unencrypted HTTP/2, which is what Console's default
	// pipeline proxy client uses.
	CertFile, KeyFile string
}

// Serve serves h until ctx is done, then shuts down gracefully.
func Serve(ctx context.Context, h http.Handler, opts ServeOptions) error {
	protocols := &http.Protocols{}
	protocols.SetHTTP1(true)
	protocols.SetHTTP2(true)
	protocols.SetUnencryptedHTTP2(true)

	srv := &http.Server{
		Addr:              opts.Address,
		Handler:           h,
		Protocols:         protocols,
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	errCh := make(chan error, 1)
	go func() {
		if opts.CertFile != "" {
			errCh <- srv.ListenAndServeTLS(opts.CertFile, opts.KeyFile)
			return
		}
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-errCh; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
