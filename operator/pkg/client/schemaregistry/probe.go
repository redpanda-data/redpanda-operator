// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package schemaregistry holds the shared per-broker Schema Registry
// readiness probe used by the operator's rolling-restart gates (v1 Cluster,
// v2 Redpanda, and stretch controllers). It is a leaf package on purpose:
// pkg/client imports pkg/resources, so probe logic shared between them has to
// live below both.
package schemaregistry

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/go-logr/logr"
	"github.com/redpanda-data/common-go/otelutil/log"
	"github.com/twmb/franz-go/pkg/sr"
)

// ErrDisabled reports that the target cluster's Schema Registry listener is
// disabled — there is nothing to probe. Callers gating behavior on SR
// readiness should treat this as "skip", not as a failure. It lives in this
// leaf package so both pkg/client (which returns it) and pkg/resources
// (which matches on it) can reference it without an import cycle.
var ErrDisabled = errors.New("schema registry listener is disabled on the cluster")

// ProbeTimeout bounds a single /status/ready probe. The endpoint
// intentionally BLOCKS until the broker's Schema Registry store has caught up
// on _schemas (core awaits writer().read_sync() before answering 200), so a
// timeout is not a transport failure — it is the "still replaying" signal
// the probe exists to observe.
const ProbeTimeout = 5 * time.Second

// Broker pairs one broker's Schema Registry base URL with a client scoped to
// exactly that URL, for per-broker probing.
type Broker struct {
	// URL is the broker's Schema Registry base URL (scheme included).
	URL string
	// Client is an sr.Client restricted to URL.
	Client *sr.Client
}

// Listener describes a cluster's internal Schema Registry listener well
// enough to probe one broker at a time at an address the caller already
// holds -- a pod IP. Where [PerBrokerClients] splits a client whose URLs
// came from the cluster's DNS, a caller that publishes those DNS records
// itself (the endpoint steering controller) cannot depend on records it is
// in the middle of deciding, so it addresses pods directly.
//
// No credentials are needed: /status/ready is registered auth-exempt in
// core. Nor is there a dialer to configure -- probes go straight to the pod
// network, so the only caller that can use this is one running in-cluster.
type Listener struct {
	// Port is the listener's port on every broker.
	Port int32
	// TLSConfig returns the listener's client TLS configuration, or nil for
	// a plaintext listener. It is consulted per probe rather than up front,
	// so a cluster whose certificates cannot be read yet fails only its
	// Schema Registry probes and not everything else the caller knows about
	// the cluster; implementations that read Secrets should memoize.
	TLSConfig func(ctx context.Context) (*tls.Config, error)
}

// BrokerAt returns a [Broker] addressed at host, which is normally a pod IP.
// serverName is the DNS name to verify the listener's certificate against,
// needed because broker certificates never carry pod IPs; it is ignored for
// a plaintext listener, or when the TLS config names a server itself.
func (l *Listener) BrokerAt(ctx context.Context, host, serverName string) (Broker, error) {
	scheme := "http"
	var opts []sr.ClientOpt

	if l.TLSConfig != nil {
		tlsConfig, err := l.TLSConfig(ctx)
		if err != nil {
			return Broker{}, errors.Wrap(err, "reading schema registry listener TLS configuration")
		}
		if tlsConfig != nil {
			scheme = "https"
			if tlsConfig.ServerName == "" && serverName != "" {
				tlsConfig = tlsConfig.Clone()
				tlsConfig.ServerName = serverName
			}
			opts = append(opts, sr.DialTLSConfig(tlsConfig))
		}
	}

	url := fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(host, strconv.Itoa(int(l.Port))))
	client, err := sr.NewClient(append(opts, sr.URLs(url))...)
	if err != nil {
		return Broker{}, errors.Wrapf(err, "building schema registry client for %q", url)
	}
	return Broker{URL: url, Client: client}, nil
}

// PerBrokerClients splits an aggregate sr.Client into one client per
// configured base URL. The aggregate client fails over across broker URLs
// internally, which turns any per-broker question ("is THIS broker's SR
// store caught up?") into an any-broker answer; the scoped clients exist so
// callers can hold each broker to its own verdict.
func PerBrokerClients(client *sr.Client) ([]Broker, error) {
	// sr.Client exposes no getter for its base URLs; OptValue is franz-go's
	// introspection API, taking the option-constructor FUNCTION (sr.URLs, the
	// same one used to build the client) as a reflective lookup key and
	// returning the value that option holds — here the []string of per-broker
	// base URLs the client was configured with.
	urls, _ := client.OptValue(sr.URLs).([]string)
	if len(urls) == 0 {
		return nil, errors.New("schema registry client has no broker URLs")
	}

	brokers := make([]Broker, 0, len(urls))
	for _, u := range urls {
		// Clone: appending to the shared Opts() slice in a loop could let a
		// later iteration overwrite spare capacity retained by an earlier
		// scoped client.
		scoped, err := sr.NewClient(append(slices.Clone(client.Opts()), sr.URLs(u))...)
		if err != nil {
			return nil, errors.Wrapf(err, "scoping schema registry client to %q", u)
		}
		brokers = append(brokers, Broker{URL: u, Client: scoped})
	}
	return brokers, nil
}

// Synced probes every broker's Schema Registry with GET /status/ready.
// /status/ready is registered auth-exempt in core (auth::none, present since
// v23.1) and blocks until the broker's SR store has replayed _schemas, so
// with ProbeTimeout a replaying broker surfaces as a timeout and a
// restarting pod as a connection error: both mean "not synced".
//
// Returns true only when every broker answers 200. On the first failure it
// returns (false, err); roll-gate callers treat false as "defer the roll"
// (fail closed) — a broker whose SR store can't be confirmed synced must not
// have the roll advance past it, since rolling the next broker would stack a
// second replay window on top.
func Synced(ctx context.Context, brokers []Broker, logger logr.Logger) (bool, error) {
	for _, broker := range brokers {
		probeCtx, cancel := context.WithTimeout(ctx, ProbeTimeout)
		err := broker.Client.Do(probeCtx, http.MethodGet, "/status/ready", nil, nil)
		cancel()
		if err != nil {
			logger.V(log.DebugLevel).Info("schema registry store not confirmed synced", "url", broker.URL, "error", err.Error())
			return false, errors.Wrapf(err, "probing schema registry readiness at %q", broker.URL)
		}
	}
	return true, nil
}
