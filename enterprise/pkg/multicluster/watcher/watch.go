// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// package watcher is a wrapper around certwatcher code from controller-runtime, but includes CA files
// and some helpers for constructing dynamic Server and TLS options
package watcher

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	"golang.org/x/sync/errgroup"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
)

type Watcher struct {
	certWatcher *certwatcher.CertWatcher
	caWatcher   *caWatcher
	logger      logr.Logger
}

func New(caFile, certFile, privateKeyFile string) (*Watcher, error) {
	certWatcher, err := certwatcher.New(certFile, privateKeyFile)
	if err != nil {
		return nil, err
	}
	caWatcher, err := newCAWatcher(caFile)
	if err != nil {
		return nil, err
	}

	return &Watcher{
		certWatcher: certWatcher,
		caWatcher:   caWatcher,
		logger:      logr.Discard(),
	}, nil
}

func (w *Watcher) SetLogger(logger logr.Logger) {
	w.logger = logger
}

func (w *Watcher) Start(ctx context.Context) error {
	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error {
		if err := w.certWatcher.Start(groupCtx); err != nil {
			return fmt.Errorf("cert watcher exited unexpectedly: %w", err)
		}
		return nil
	})
	group.Go(func() error {
		if err := w.caWatcher.start(groupCtx); err != nil {
			return fmt.Errorf("ca watcher exited unexpectedly: %w", err)
		}
		return nil
	})
	return group.Wait()
}

func (w *Watcher) ClientTLSOptions(c *tls.Config) {
	c.GetClientCertificate = func(_ *tls.CertificateRequestInfo) (*tls.Certificate, error) {
		return w.certWatcher.GetCertificate(nil)
	}
	c.MinVersion = tls.VersionTLS13
	c.InsecureSkipVerify = true // nolint:gosec // verification happens below, we do this here so that we can manually verify with dynamic roots
	c.VerifyConnection = func(cs tls.ConnectionState) error {
		w.logger.V(7).Info("verifying server config", "server", cs.ServerName)
		roots, err := w.caWatcher.getCA()
		if err != nil {
			return err
		}
		opts := x509.VerifyOptions{
			Roots:         roots,
			DNSName:       cs.ServerName,
			Intermediates: x509.NewCertPool(),
			KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		}
		for _, cert := range cs.PeerCertificates[1:] {
			opts.Intermediates.AddCert(cert)
		}
		_, err = cs.PeerCertificates[0].Verify(opts)
		if err != nil {
			w.logger.Error(err, "verifying server config failed")
		} else {
			w.logger.V(7).Info("verifying server config succeeded")
		}
		return err
	}
}

func (w *Watcher) ServerTLSOptions(c *tls.Config) {
	c.GetConfigForClient = func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
		w.logger.V(7).Info("fetching client config")
		roots, err := w.caWatcher.getCA()
		if err != nil {
			return nil, err
		}
		cert, err := w.certWatcher.GetCertificate(hello)
		if err != nil {
			return nil, err
		}
		if cert == nil {
			return nil, errors.New("certificate not loaded")
		}
		return &tls.Config{
			MinVersion:   tls.VersionTLS13,
			Certificates: []tls.Certificate{*cert},
			ClientCAs:    roots,
			ClientAuth:   tls.RequireAndVerifyClientCert,
		}, nil
	}
}

func (w *Watcher) NeedLeaderElection() bool {
	return false
}
