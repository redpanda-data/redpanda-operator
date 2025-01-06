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
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	commonnet "github.com/redpanda-data/common-go/net"
	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/plain"
	"github.com/twmb/franz-go/pkg/sasl/scram"
	"github.com/twmb/franz-go/pkg/sr"

	rpkconfig "github.com/redpanda-data/redpanda/src/go/rpk/pkg/config"
)

// this roughly implements https://github.com/redpanda-data/redpanda/blob/f5a7a13f7fca3f69a4380f0bbfa8fbc3e7f899d6/src/go/rpk/pkg/adminapi/admin.go#L46
// but without the OIDC support, that said, it seems real odd that we delegate to the KafkaAPI stanza for an admin API connection.
func getAdminAuth(p *rpkconfig.RpkProfile) rpadmin.Auth {
	if p.HasSASLCredentials() {
		return &rpadmin.BasicAuth{Username: p.KafkaAPI.SASL.User, Password: p.KafkaAPI.SASL.Password}
	}

	// we explicitly don't support anything else at the moment (i.e. OIDC)
	return &rpadmin.NopAuth{}
}

// redpandaAdminForRPKProfile returns a simple rpadmin.AdminAPI able to communicate with a cluster based on the given RPK profile.
func (c *Factory) redpandaAdminForRPKProfile(profile *rpkconfig.RpkProfile) (*rpadmin.AdminAPI, error) {
	tls, err := profile.AdminAPI.TLS.Config(c.fs)
	if err != nil {
		return nil, fmt.Errorf("unable to create admin api tls config: %v", err)
	}

	client, err := rpadmin.NewAdminAPIWithDialer(profile.AdminAPI.Addresses, getAdminAuth(profile), tls, c.dialer)
	if err != nil {
		return nil, fmt.Errorf("initializing admin client: %w", err)
	}

	if c.userAuth != nil {
		client.SetAuth(&rpadmin.BasicAuth{
			Username: c.userAuth.Username,
			Password: c.userAuth.Password,
		})
	}

	return client, nil
}

// this implements the basic logic found here https://github.com/redpanda-data/redpanda/blob/8e1ccaab1975150ed4b2aec630b4fed6f06a16bf/src/go/rpk/pkg/schemaregistry/client.go#L14
func normalizeSchemaRegistryURLs(profile *rpkconfig.RpkProfile) ([]string, error) {
	urls := profile.SR.Addresses
	for i, url := range urls {
		scheme, _, err := commonnet.ParseHostMaybeScheme(url)
		if err != nil {
			return nil, fmt.Errorf("unable to parse schema registry address %q: %v", url, err)
		}
		switch scheme {
		case "http", "https":
			continue
		case "":
			if profile.SR.TLS != nil {
				urls[i] = "https://" + url
				continue
			}

			urls[i] = "http://" + url
		default:
			return nil, fmt.Errorf("unsupported scheme %q in the schema registry address %q", scheme, url)
		}
	}

	return urls, nil
}

// schemaRegistryForRPKProfile returns a simple sr.Client able to communicate with a cluster based on the given RPK profile.
func (c *Factory) schemaRegistryForRPKProfile(profile *rpkconfig.RpkProfile) (*sr.Client, error) {
	urls, err := normalizeSchemaRegistryURLs(profile)
	if err != nil {
		return nil, err
	}

	tls, err := profile.SR.TLS.Config(c.fs)
	if err != nil {
		return nil, err
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

	if c.dialer == nil {
		transport.DialContext = (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext
	}

	if tls != nil {
		transport.TLSClientConfig = tls
	}

	opts := []sr.ClientOpt{sr.HTTPClient(&http.Client{
		Timeout:   5 * time.Second,
		Transport: transport,
	})}

	if profile.HasSASLCredentials() {
		opts = append(opts, sr.BasicAuth(profile.KafkaAPI.SASL.User, profile.KafkaAPI.SASL.Password))
	}

	opts = append(opts, sr.URLs(urls...))

	if c.userAuth != nil {
		// override explicit user auth
		opts = append(opts, sr.BasicAuth(c.userAuth.Username, c.userAuth.Password))
	}

	return sr.NewClient(opts...)
}

// this implements roughly https://github.com/redpanda-data/redpanda/blob/dev/src/go/rpk/pkg/kafka/client_franz.go#L100
func getKafkaAuth(profile *rpkconfig.RpkProfile) (kgo.Opt, error) {
	if profile.HasSASLCredentials() {
		auth := scram.Auth{
			User: profile.KafkaAPI.SASL.User,
			Pass: profile.KafkaAPI.SASL.Password,
		}

		switch name := strings.ToUpper(profile.KafkaAPI.SASL.Mechanism); name {
		case "SCRAM-SHA-256", "": // we default to SCRAM-SHA-256 -- people commonly specify user & pass without --sasl-mechanism
			return kgo.SASL(auth.AsSha256Mechanism()), nil
		case "SCRAM-SHA-512":
			return kgo.SASL(auth.AsSha512Mechanism()), nil
		case "PLAIN":
			return kgo.SASL((&plain.Auth{
				User: profile.KafkaAPI.SASL.User,
				Pass: profile.KafkaAPI.SASL.Password,
			}).AsMechanism()), nil
		default:
			return nil, fmt.Errorf("unknown SASL mechanism %q, supported: [SCRAM-SHA-256, SCRAM-SHA-512, PLAIN]", name)
		}
	}

	return nil, nil
}

// kafkaForRPKProfile returns a simple kgo.Client able to communicate with a cluster based on the given RPK profile.
func (c *Factory) kafkaForRPKProfile(profile *rpkconfig.RpkProfile, opts ...kgo.Opt) (*kgo.Client, error) {
	kopts := []kgo.Opt{
		kgo.SeedBrokers(profile.KafkaAPI.Brokers...),
	}

	tls, err := profile.KafkaAPI.TLS.Config(c.fs)
	if err != nil {
		return nil, err
	}

	authOpt, err := getKafkaAuth(profile)
	if err != nil {
		return nil, err
	}

	if authOpt != nil {
		kopts = append(kopts, authOpt)
	}

	if tls != nil {
		// we can only specify one of DialTLSConfig or Dialer
		if c.dialer == nil {
			kopts = append(kopts, kgo.DialTLSConfig(tls))
		} else {
			kopts = append(kopts, kgo.Dialer(wrapTLSDialer(c.dialer, tls)))
		}
	} else if c.dialer != nil {
		kopts = append(kopts, kgo.Dialer(c.dialer))
	}

	// append all user specified opts after
	kopts = append(kopts, opts...)

	// and finally handle factory-level auth override
	authOpt, err = c.kafkaUserAuth()
	if err != nil {
		return nil, err
	}

	if authOpt != nil {
		kopts = append(kopts, authOpt)
	}

	return kgo.NewClient(kopts...)
}
