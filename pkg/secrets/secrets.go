// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package secrets

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/go-logr/logr"
	"github.com/redpanda-data/common-go/secrets"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ErrSecretNotFound is returned when a cloud secret cannot be found.
var ErrSecretNotFound = errors.New("cloud secret not found")

type CloudExpander struct {
	client secrets.SecretAPI
	logger *slog.Logger
}

type ExpanderCloudConfiguration struct {
	AWSRegion        string
	AWSRoleARN       string
	GCPProjectID     string
	AzureKeyVaultURI string
}

// NewCloudExpander creates a new CloudExpander
func NewCloudExpander(
	ctx context.Context,
	prefix string,
	cloudConfig ExpanderCloudConfiguration,
) (*CloudExpander, error) {
	logger := log.FromContext(ctx)
	slogLogger := slog.New(logr.ToSlogHandler(logger.WithName("slog").WithValues("mode", "slog")))

	var err error
	var secretsAPI secrets.SecretAPI

	switch {
	case cloudConfig.AWSRegion != "":
		secretsAPI, err = secrets.NewAWSSecretsManager(
			ctx,
			slogLogger,
			cloudConfig.AWSRegion,
			cloudConfig.AWSRoleARN,
		)

	case cloudConfig.GCPProjectID != "":
		// Updated for common-go/secrets v0.1.4 API compatibility.
		// The 4th parameter is the audience for federated secrets authentication.
		// Set to empty string to use default authentication without federation.
		// This secret expander fetches config values from cloud secret stores
		// when external secret references are used in Redpanda configurations.
		secretsAPI, err = secrets.NewGCPSecretsManager(
			ctx,
			slogLogger,
			cloudConfig.GCPProjectID,
			"", // audience for federated secrets - empty uses default authentication
		)

	case cloudConfig.AzureKeyVaultURI != "":
		secretsAPI, err = secrets.NewAzSecretsManager(
			slogLogger,
			cloudConfig.AzureKeyVaultURI,
		)

	default:
		return nil, errors.New("failed to construct SecretAPI: none of AWSRegion, GCPProjectID, nor AzureKeyVaultURI provided")
	}

	if err != nil {
		return nil, errors.Wrapf(err, "constructing %T", secretsAPI)
	}

	provider, err := secrets.NewSecretProvider(secretsAPI, prefix, "")
	if err != nil {
		return nil, errors.WithStack(err)
	}
	return &CloudExpander{client: provider, logger: slogLogger}, nil
}

// NewCloudExpanderFromAPI creates a new CloudExpander
func NewCloudExpanderFromAPI(api secrets.SecretAPI) *CloudExpander {
	return &CloudExpander{client: api}
}

// Expand expands the secret value by retrieving it from the cloud secret store
func (t *CloudExpander) Expand(ctx context.Context, name string) (string, error) {
	value, found := t.client.GetSecretValue(ctx, name)
	if !found {
		return "", errors.Wrapf(ErrSecretNotFound, "secret %s", name)
	}
	return value, nil
}

// MaybeExpand expands the secret value if it is in the format ${secrets.<secret-name>}
func (t *CloudExpander) MaybeExpand(ctx context.Context, value string) (string, error) {
	maybeExpandedValue := os.Expand(value, func(s string) string {
		if !strings.HasPrefix(s, "secrets.") {
			return "${" + s + "}" // return unmodified
		}
		expandedValue, err := t.Expand(ctx, value)
		if err != nil {
			t.logger.Warn("failed to expand secret", slog.Any("error", err))
			return ""
		}
		return expandedValue
	})
	return maybeExpandedValue, nil
}
