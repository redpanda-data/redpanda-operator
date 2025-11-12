// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package client

import (
	"context"
	"crypto/tls"
	"net"

	redpanda "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/client"
)

func wrapTLSDialer(dialer redpanda.DialContextFunc, config *tls.Config) redpanda.DialContextFunc {
	return func(ctx context.Context, network, host string) (net.Conn, error) {
		conn, err := dialer(ctx, network, host)
		if err != nil {
			return nil, err
		}

		serverName, _, err := net.SplitHostPort(host)
		if err != nil {
			// we likely didn't have a port, use
			// the whole string as the serverName
			serverName = host
		}

		config = config.Clone()
		if config.ServerName == "" {
			config.ServerName = serverName
		}

		return tls.Client(conn, config), nil
	}
}
