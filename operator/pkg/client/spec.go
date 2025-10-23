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
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/redpanda-data/console/backend/pkg/config"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/shadow"
	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/log"
)

// KafkaForSpec returns a simple kgo.Client able to communicate with the given cluster specified via KafkaAPISpec.
func (c *Factory) kafkaForSpec(ctx context.Context, namespace string, metricNamespace *string, spec *redpandav1alpha2.KafkaAPISpec, opts ...kgo.Opt) (*kgo.Client, error) {
	logger := log.FromContext(ctx)

	if len(spec.Brokers) == 0 {
		return nil, ErrEmptyBrokerList
	}
	kopts := []kgo.Opt{
		kgo.SeedBrokers(spec.Brokers...),
	}

	metricsLabel := "redpanda_operator"
	if metricNamespace != nil && *metricNamespace != "" {
		metricsLabel = *metricNamespace
	}

	hooks := newClientHooks(logger, metricsLabel)

	// Create Logger
	kopts = append(kopts, kgo.WithLogger(wrapLogger(logger)), kgo.WithHooks(hooks))

	if spec.SASL != nil {
		saslOpt, err := c.configureKafkaSpecSASL(ctx, namespace, spec)
		if err != nil {
			return nil, err
		}

		kopts = append(kopts, saslOpt)
	}

	authOpt, err := c.kafkaUserAuth()
	if err != nil {
		return nil, err
	}

	if authOpt != nil {
		kopts = append(kopts, authOpt)
	}

	if spec.TLS != nil {
		tlsConfig, err := c.configureSpecTLS(ctx, namespace, spec.TLS)
		if err != nil {
			return nil, err
		}

		if c.dialer != nil {
			kopts = append(kopts, kgo.Dialer(wrapTLSDialer(c.dialer, tlsConfig)))
		} else {
			dialer := &tls.Dialer{
				NetDialer: &net.Dialer{Timeout: 10 * time.Second},
				Config:    tlsConfig,
			}
			kopts = append(kopts, kgo.Dialer(dialer.DialContext))
		}
	} else if c.dialer != nil {
		kopts = append(kopts, kgo.Dialer(c.dialer))
	}

	return kgo.NewClient(append(opts, kopts...)...)
}

func (c *Factory) redpandaAdminForSpec(ctx context.Context, namespace string, spec *redpandav1alpha2.AdminAPISpec) (*rpadmin.AdminAPI, error) {
	if len(spec.URLs) == 0 {
		return nil, ErrEmptyURLList
	}

	var err error
	var tlsConfig *tls.Config
	if spec.TLS != nil {
		tlsConfig, err = c.configureSpecTLS(ctx, namespace, spec.TLS)
		if err != nil {
			return nil, err
		}
	}

	var auth rpadmin.Auth
	var username, password, token string
	username, password, token, err = c.configureAdminSpecSASL(ctx, namespace, spec)
	if err != nil {
		return nil, err
	}

	switch {
	case username != "":
		auth = &rpadmin.BasicAuth{
			Username: username,
			Password: password,
		}
	case token != "":
		auth = &rpadmin.BearerToken{
			Token: token,
		}
	default:
		auth = &rpadmin.NopAuth{}
	}

	client, err := rpadmin.NewAdminAPIWithDialer(spec.URLs, auth, tlsConfig, c.dialer, rpadmin.ClientTimeout(c.adminClientTimeout))
	if err != nil {
		return nil, err
	}

	if c.userAuth != nil {
		client.SetAuth(&rpadmin.BasicAuth{
			Username: c.userAuth.Username,
			Password: c.userAuth.Password,
		})
	}

	return client, nil
}

func (c *Factory) schemaRegistryForSpec(ctx context.Context, namespace string, spec *redpandav1alpha2.SchemaRegistrySpec) (*sr.Client, error) {
	if len(spec.URLs) == 0 {
		return nil, ErrEmptyURLList
	}

	// These transport values come from the TLS client options found here:
	// https://github.com/twmb/franz-go/blob/cea7aa5d803781e5f0162187795482ba1990c729/pkg/sr/clientopt.go#L48-L68
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   100,
		DialContext:           c.dialer,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	var err error
	var tlsConfig *tls.Config
	if spec.TLS != nil {
		tlsConfig, err = c.configureSpecTLS(ctx, namespace, spec.TLS)
		if err != nil {
			return nil, err
		}
		transport.TLSClientConfig = tlsConfig
	}

	opts := []sr.ClientOpt{
		sr.HTTPClient(&http.Client{
			Timeout:   5 * time.Second,
			Transport: transport,
		}),
	}

	authOpt, err := c.configureSchemaRegistrySpecSASL(ctx, namespace, spec)
	if err != nil {
		return nil, err
	}

	if c.userAuth != nil {
		opts = append(opts, sr.BasicAuth(c.userAuth.Username, c.userAuth.Password))
	} else if authOpt != nil {
		opts = append(opts, authOpt)
	}

	opts = append(opts, sr.URLs(spec.URLs...))

	return sr.NewClient(opts...)
}

func (c *Factory) remoteClusterSettingsForSpec(ctx context.Context, namespace string, spec *redpandav1alpha2.KafkaAPISpec) (shadow.RemoteClusterSettings, error) {
	var settings shadow.RemoteClusterSettings

	if len(spec.Brokers) == 0 {
		return settings, ErrEmptyBrokerList
	}

	settings.BootstrapServers = spec.Brokers

	if spec.SASL != nil {
		auth, err := c.remoteClusterAuthSettingsForSpec(ctx, namespace, spec)
		if err != nil {
			return settings, err
		}
		settings.Authentication = auth
	}

	if spec.TLS != nil {
		tls, err := c.remoteClusterTLSSettingsForSpec(ctx, namespace, spec.TLS)
		if err != nil {
			return settings, err
		}
		settings.TLSSettings = tls
	}

	return settings, nil
}

func (c *Factory) remoteClusterTLSSettingsForSpec(ctx context.Context, namespace string, spec *redpandav1alpha2.CommonTLS) (*shadow.TLSSettings, error) {
	// NB: we elide the check of the `Enabled` field because if any field is set, `Enabled` included, the spec is not nil
	// and we should be leveraging TLS.
	settings := &shadow.TLSSettings{}

	// Root CA
	if spec.CaCert != nil {
		ca, err := spec.CaCert.GetValue(ctx, c.Client, namespace, "ca.crt")
		if err != nil {
			return nil, fmt.Errorf("failed to read ca certificate secret: %w", err)
		}

		settings.CA = string(ca)
	}

	// If configured load TLS cert & key - Mutual TLS
	if spec.Cert != nil && spec.Key != nil {
		// 1. Read certificates
		cert, err := spec.Cert.GetValue(ctx, c.Client, namespace, "tls.crt")
		if err != nil {
			return nil, fmt.Errorf("failed to read certificate secret: %w", err)
		}

		settings.Cert = string(cert)

		key, err := spec.Cert.GetValue(ctx, c.Client, namespace, "tls.key")
		if err != nil {
			return nil, fmt.Errorf("failed to read key certificate secret: %w", err)
		}

		settings.Key = string(key)
	}

	return settings, nil
}

func (c *Factory) remoteClusterAuthSettingsForSpec(ctx context.Context, namespace string, spec *redpandav1alpha2.KafkaAPISpec) (*shadow.AuthenticationSettings, error) {
	switch spec.SASL.Mechanism {
	case config.SASLMechanismPlain, config.SASLMechanismScramSHA256, config.SASLMechanismScramSHA512:
		p, err := spec.SASL.Password.GetValue(ctx, c.Client, namespace, "password")
		if err != nil {
			return nil, fmt.Errorf("unable to fetch sasl plain password: %w", err)
		}

		return &shadow.AuthenticationSettings{
			Username:  spec.SASL.Username,
			Password:  string(p),
			Mechanism: spec.SASL.Mechanism,
		}, nil
	}

	return nil, fmt.Errorf("unsupported sasl mechanism: %s", spec.SASL.Mechanism)
}
