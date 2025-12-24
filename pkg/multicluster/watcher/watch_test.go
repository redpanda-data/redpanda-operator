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
	"crypto/tls"
	"net"
	"os"
	"path"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/go-logr/logr/testr"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster/bootstrap"
	"github.com/stretchr/testify/require"
)

func TestWatcher(t *testing.T) {
	directory := t.TempDir()

	ca, err := bootstrap.GenerateCA("test", "test CA", &bootstrap.CAConfiguration{})
	require.NoError(t, err)
	cert, err := ca.Sign("127.0.0.1")
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(path.Join(directory, "ca.crt"), ca.Bytes(), 0o644))
	require.NoError(t, os.WriteFile(path.Join(directory, "tls.crt"), cert.Bytes(), 0o644))
	require.NoError(t, os.WriteFile(path.Join(directory, "tls.key"), cert.PrivateKeyBytes(), 0o644))

	watcher, err := New(path.Join(directory, "ca.crt"), path.Join(directory, "tls.crt"), path.Join(directory, "tls.key"))
	require.NoError(t, err)
	watcher.SetLogger(testr.NewWithOptions(t, testr.Options{Verbosity: 7}))
	watcher.Start(t.Context())

	serverCert := &tls.Config{
		ClientAuth: tls.RequireAndVerifyClientCert,
	}
	clientCert := &tls.Config{}
	watcher.ServerTLSOptions(serverCert)
	watcher.ClientTLSOptions(clientCert)

	listener, err := tls.Listen("tcp", "127.0.0.1:0", serverCert)
	require.NoError(t, err)

	var serverInitialized atomic.Bool
	closed := make(chan struct{})
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				close(closed)
				return
			}
			go func() {
				serverInitialized.Store(true)

				defer conn.Close()
				for {
					b := make([]byte, 1)
					_, _ = conn.Read(b)
				}
			}()
		}
	}()

	conn, err := tls.Dial("tcp", "127.0.0.1:"+strconv.Itoa(listener.Addr().(*net.TCPAddr).Port), clientCert)
	require.NoError(t, err)
	conn.Close()
	listener.Close()
	<-closed
}
