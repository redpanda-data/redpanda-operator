// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package flux

import (
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	controllers "github.com/fluxcd/source-controller/shim"
	"github.com/go-logr/logr"
	"helm.sh/helm/v3/pkg/registry"
	"k8s.io/apimachinery/pkg/util/errors"
)

func MustInitStorage(path, storageAdvAddr string, artifactRetentionTTL time.Duration, artifactRetentionRecords int, l logr.Logger) controllers.Storage {
	if path == "" {
		p, _ := os.Getwd()
		path = filepath.Join(p, "bin")
		err := os.MkdirAll(path, 0o700)
		if err != nil {
			l.Error(err, "unable make directory with right permissions")
		}
	}

	storage, err := controllers.NewStorage(path, storageAdvAddr, artifactRetentionTTL, artifactRetentionRecords)
	if err != nil {
		l.Error(err, "unable to initialize storage")
		os.Exit(1)
	}

	return storage
}

func DetermineAdvStorageAddr(storageAddr string, l logr.Logger) string {
	host, port, err := net.SplitHostPort(storageAddr)
	if err != nil {
		l.Error(err, "unable to parse storage address")
		os.Exit(1)
	}
	switch host {
	case "":
		host = "localhost"
	case "0.0.0.0":
		host = os.Getenv("HOSTNAME")
		if host == "" {
			hn, err := os.Hostname()
			if err != nil {
				l.Error(err, "0.0.0.0 specified in storage addr but hostname is invalid")
				os.Exit(1)
			}
			host = hn
		}
	}
	return net.JoinHostPort(host, port)
}

func StartFileServer(path, address string, l logr.Logger) {
	l.Info("starting file server")
	fs := http.FileServer(http.Dir(path))
	mux := http.NewServeMux()
	mux.Handle("/", fs)
	//nolint:gosec // we are aware there are no timeouts supported
	err := http.ListenAndServe(address, mux)
	if err != nil {
		l.Error(err, "file server error")
	}
}

// ClientGenerator generates a registry client and a temporary credential file.
// The client is meant to be used for a single reconciliation.
// The file is meant to be used for a single reconciliation and deleted after.
func ClientGenerator(tlsConfig *tls.Config, isLogin, insecureHTTP bool) (*registry.Client, string, error) {
	if !isLogin {
		rClient, err := newClient("", tlsConfig, insecureHTTP)
		if err != nil {
			return nil, "", err
		}
		return rClient, "", nil
	}
	// create a temporary file to store the credentials
	// this is needed because otherwise the credentials are stored in ~/.docker/config.json.
	credentialsFile, err := os.CreateTemp("", "credentials")
	if err != nil {
		return nil, "", err
	}

	var errs []error
	rClient, err := newClient(credentialsFile.Name(), tlsConfig, insecureHTTP)
	if err != nil {
		errs = append(errs, err)
		// attempt to delete the temporary file
		if credentialsFile != nil {
			err := os.Remove(credentialsFile.Name())
			if err != nil {
				errs = append(errs, err)
			}
		}
		return nil, "", errors.NewAggregate(errs)
	}
	return rClient, credentialsFile.Name(), nil
}

func newClient(credentialsFile string, tlsConfig *tls.Config, insecureHTTP bool) (*registry.Client, error) {
	opts := []registry.ClientOption{
		registry.ClientOptWriter(io.Discard),
	}
	if insecureHTTP {
		opts = append(opts, registry.ClientOptPlainHTTP())
	}
	if tlsConfig != nil {
		opts = append(opts, registry.ClientOptHTTPClient(&http.Client{
			Transport: &http.Transport{
				TLSClientConfig: tlsConfig,
			},
		}))
	}
	if credentialsFile != "" {
		opts = append(opts, registry.ClientOptCredentialsFile(credentialsFile))
	}

	return registry.NewClient(opts...)
}
