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
	"crypto/tls"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"buf.build/gen/go/redpandadata/dataplane/connectrpc/go/redpanda/api/dataplane/v1/dataplanev1connect"
	"connectrpc.com/connect"
	"github.com/cockroachdb/errors"
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
	// CertFile and KeyFile enable TLS, re-read when they change so that
	// cert-manager renewals apply without a restart. Without them the server
	// speaks HTTP/1.1 and unencrypted HTTP/2.
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

	if opts.CertFile != "" {
		certs := &certReloader{certFile: opts.CertFile, keyFile: opts.KeyFile}
		// Fail at startup, not on the first handshake.
		if _, err := certs.getCertificate(nil); err != nil {
			return err
		}
		srv.TLSConfig = &tls.Config{
			MinVersion:     tls.VersionTLS12,
			GetCertificate: certs.getCertificate,
		}
	}

	errCh := make(chan error, 1)
	go func() {
		if srv.TLSConfig != nil {
			errCh <- srv.ListenAndServeTLS("", "")
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

// certReloaderInterval bounds how often certReloader checks the files.
const certReloaderInterval = 10 * time.Second

// certReloader serves a key pair from files, reloading it when the
// certificate file's modification time changes. A failed reload keeps the
// previous key pair.
type certReloader struct {
	certFile, keyFile string

	mu      sync.Mutex
	cert    *tls.Certificate
	modTime time.Time
	checked time.Time
}

func (r *certReloader) getCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	if r.cert != nil && now.Sub(r.checked) < certReloaderInterval {
		return r.cert, nil
	}
	r.checked = now

	info, err := os.Stat(r.certFile)
	if err != nil {
		return r.cachedOr(errors.Wrap(err, "reading TLS certificate"))
	}
	if r.cert != nil && info.ModTime().Equal(r.modTime) {
		return r.cert, nil
	}
	cert, err := tls.LoadX509KeyPair(r.certFile, r.keyFile)
	if err != nil {
		return r.cachedOr(errors.Wrap(err, "loading TLS key pair"))
	}
	r.cert, r.modTime = &cert, info.ModTime()
	return r.cert, nil
}

func (r *certReloader) cachedOr(err error) (*tls.Certificate, error) {
	if r.cert != nil {
		return r.cert, nil
	}
	return nil, err
}
