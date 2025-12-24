// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0
package watcher

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
)

type Watcher struct {
	certWatcher *certwatcher.CertWatcher
	caWatcher   *CAWatcher
	logger      logr.Logger
}

func New(caFile, certFile, privateKeyFile string) (*Watcher, error) {
	certWatcher, err := certwatcher.New(certFile, privateKeyFile)
	if err != nil {
		return nil, err
	}
	caWatcher, err := NewCAWatcher(caFile)
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

func (w *Watcher) Start(ctx context.Context) {
	go func() {
		if err := w.certWatcher.Start(ctx); err != nil {
			w.logger.Error(err, "cert watcher exited with an error")
		}
	}()
	go func() {
		if err := w.caWatcher.Start(ctx); err != nil {
			w.logger.Error(err, "ca watcher exited with error")
		}
	}()
}

func (w *Watcher) ClientTLSOptions(c *tls.Config) {
	c.GetClientCertificate = func(_ *tls.CertificateRequestInfo) (*tls.Certificate, error) {
		return w.certWatcher.GetCertificate(nil)
	}
	c.InsecureSkipVerify = true // nolint:gosec // verification below
	c.VerifyConnection = func(cs tls.ConnectionState) error {
		w.logger.V(7).Info("verifying server config")
		roots, err := w.caWatcher.GetCA()
		if err != nil {
			return err
		}
		opts := x509.VerifyOptions{
			Roots:         roots,
			Intermediates: x509.NewCertPool(),
			KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		}
		for _, cert := range cs.PeerCertificates[1:] {
			opts.Intermediates.AddCert(cert)
		}
		_, err = cs.PeerCertificates[0].Verify(opts)
		if err != nil {
			w.logger.V(7).Info("verifying server config failed", "error", err)
		} else {
			w.logger.V(7).Info("verifying server config succeeded")
		}
		return err
	}
}

func (w *Watcher) ServerTLSOptions(c *tls.Config) {
	c.GetCertificate = w.certWatcher.GetCertificate
	c.GetConfigForClient = func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
		w.logger.V(7).Info("fetching client config")
		roots, err := w.caWatcher.GetCA()
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
			Certificates: []tls.Certificate{*cert},
			RootCAs:      roots,
			ClientAuth:   tls.RequireAndVerifyClientCert,
		}, nil
	}
}
