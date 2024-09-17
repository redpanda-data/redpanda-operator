// Copyright 2024 Redpanda Data, Inc.
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
	"fmt"

	krbclient "github.com/jcmturner/gokrb5/v8/client"
	krbconfig "github.com/jcmturner/gokrb5/v8/config"
	"github.com/jcmturner/gokrb5/v8/keytab"
	"github.com/redpanda-data/console/backend/pkg/config"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha2"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl"
	"github.com/twmb/franz-go/pkg/sasl/aws"
	"github.com/twmb/franz-go/pkg/sasl/kerberos"
	"github.com/twmb/franz-go/pkg/sasl/oauth"
	"github.com/twmb/franz-go/pkg/sasl/plain"
	"github.com/twmb/franz-go/pkg/sasl/scram"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func (c *Factory) configureAdminSpecSASL(ctx context.Context, namespace string, spec *redpandav1alpha2.AdminAPISpec) (username, password, token string, err error) {
	if spec.SASL == nil {
		return "", "", "", nil
	}

	//nolint:exhaustive // we don't need this to be exhaustive, as we only support 3 auth mechanisms in this API.
	switch spec.SASL.Mechanism {
	// SCRAM
	case config.SASLMechanismScramSHA256, config.SASLMechanismScramSHA512:
		p, err := spec.SASL.Password.GetValue(ctx, c.Client, namespace, "password")
		if err != nil {
			return "", "", "", fmt.Errorf("unable to fetch sasl password: %w", err)
		}

		return spec.SASL.Username, string(p), "", nil
	// OAUTH
	case config.SASLMechanismOAuthBearer:
		token, err := spec.SASL.AuthToken.GetValue(ctx, c.Client, namespace, "password")
		if err != nil {
			return "", "", "", fmt.Errorf("unable to fetch sasl token: %w", err)
		}
		return "", "", string(token), nil
	}

	return "", "", "", fmt.Errorf("unsupported SASL mechanism: %s", spec.SASL.Mechanism)
}

func (c *Factory) configureKafkaSpecSASL(ctx context.Context, namespace string, spec *redpandav1alpha2.KafkaAPISpec) (kgo.Opt, error) {
	logger := log.FromContext(ctx)

	switch spec.SASL.Mechanism {
	// SASL Plain
	case config.SASLMechanismPlain:
		p, err := spec.SASL.Password.GetValue(ctx, c.Client, namespace, "password")
		if err != nil {
			return nil, fmt.Errorf("unable to fetch sasl plain password: %w", err)
		}

		return kgo.SASL(plain.Auth{
			User: spec.SASL.Username,
			Pass: string(p),
		}.AsMechanism()), nil

	// SASL SCRAM
	case config.SASLMechanismScramSHA256, config.SASLMechanismScramSHA512:
		p, err := spec.SASL.Password.GetValue(ctx, c.Client, namespace, "password")
		if err != nil {
			return nil, fmt.Errorf("unable to fetch sasl scram password: %w", err)
		}

		var mechanism sasl.Mechanism
		scramAuth := scram.Auth{
			User: spec.SASL.Username,
			Pass: string(p),
		}

		if spec.SASL.Mechanism == config.SASLMechanismScramSHA256 {
			logger.V(traceLevel).Info("configuring SCRAM-SHA-256 mechanism")
			mechanism = scramAuth.AsSha256Mechanism()
		}

		if spec.SASL.Mechanism == config.SASLMechanismScramSHA512 {
			logger.V(traceLevel).Info("configuring SCRAM-SHA-512 mechanism")
			mechanism = scramAuth.AsSha512Mechanism()
		}

		return kgo.SASL(mechanism), nil

	// OAuth Bearer
	case config.SASLMechanismOAuthBearer:
		t, err := spec.SASL.OAUth.Token.GetValue(ctx, c.Client, namespace, "token")
		if err != nil {
			return nil, fmt.Errorf("unable to fetch token: %w", err)
		}

		return kgo.SASL(oauth.Auth{
			Token: string(t),
		}.AsMechanism()), nil

	// Kerberos
	case config.SASLMechanismGSSAPI:
		logger.V(traceLevel).Info("configuring SCRAM-SHA-512 mechanism")
		var krbClient *krbclient.Client

		kerbCfg, err := krbconfig.Load(spec.SASL.GSSAPIConfig.KerberosConfigPath)
		if err != nil {
			return nil, fmt.Errorf("creating kerberos config from specified config (%s) filepath: %w", spec.SASL.GSSAPIConfig.KerberosConfigPath, err)
		}

		switch spec.SASL.GSSAPIConfig.AuthType {
		case "USER_AUTH":
			p, err := spec.SASL.GSSAPIConfig.Password.GetValue(ctx, c.Client, namespace, "password")
			if err != nil {
				return nil, fmt.Errorf("unable to fetch sasl gssapi password: %w", err)
			}

			krbClient = krbclient.NewWithPassword(
				spec.SASL.GSSAPIConfig.Username,
				spec.SASL.GSSAPIConfig.Realm,
				string(p),
				kerbCfg,
				krbclient.DisablePAFXFAST(!spec.SASL.GSSAPIConfig.EnableFast),
			)

		case "KEYTAB_AUTH":
			ktb, err := keytab.Load(spec.SASL.GSSAPIConfig.KeyTabPath)
			if err != nil {
				return nil, fmt.Errorf("loading keytab from (%s) key tab path: %w", spec.SASL.GSSAPIConfig.KeyTabPath, err)
			}

			krbClient = krbclient.NewWithKeytab(
				spec.SASL.GSSAPIConfig.Username,
				spec.SASL.GSSAPIConfig.Realm,
				ktb,
				kerbCfg,
				krbclient.DisablePAFXFAST(!spec.SASL.GSSAPIConfig.EnableFast),
			)
		}

		return kgo.SASL(kerberos.Auth{
			Client:           krbClient,
			Service:          spec.SASL.GSSAPIConfig.ServiceName,
			PersistAfterAuth: true,
		}.AsMechanism()), nil

	// AWS MSK IAM
	case config.SASLMechanismAWSManagedStreamingIAM:
		s, err := spec.SASL.AWSMskIam.SecretKey.GetValue(ctx, c.Client, namespace, "secret")
		if err != nil {
			return nil, fmt.Errorf("unable to fetch aws msk secret key: %w", err)
		}

		t, err := spec.SASL.AWSMskIam.SessionToken.GetValue(ctx, c.Client, namespace, "token")
		if err != nil {
			return nil, fmt.Errorf("unable to fetch aws msk secret key: %w", err)
		}

		return kgo.SASL(aws.Auth{
			AccessKey:    spec.SASL.AWSMskIam.AccessKey,
			SecretKey:    string(s),
			SessionToken: string(t),
			UserAgent:    spec.SASL.AWSMskIam.UserAgent,
		}.AsManagedStreamingIAMMechanism()), nil
	}

	return nil, fmt.Errorf("unsupported sasl mechanism: %s", spec.SASL.Mechanism)
}
