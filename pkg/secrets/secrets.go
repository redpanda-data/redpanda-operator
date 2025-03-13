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

	var secretsAPI secrets.SecretAPI
	var err error
	if cloudConfig.AWSRegion != "" {
		secretsAPI, err = secrets.NewAWSSecretsManager(
			ctx,
			slogLogger,
			cloudConfig.AWSRegion,
			cloudConfig.AWSRoleARN,
		)
		if err != nil {
			return nil, err
		}
	} else if cloudConfig.GCPProjectID != "" {
		secretsAPI, err = secrets.NewGCPSecretsManager(
			ctx,
			slogLogger,
			cloudConfig.GCPProjectID,
		)
		if err != nil {
			return nil, err
		}
	} else if cloudConfig.AzureKeyVaultURI != "" {
		secretsAPI, err = secrets.NewAzSecretsManager(
			slogLogger,
			cloudConfig.AzureKeyVaultURI,
		)
		if err != nil {
			return nil, err
		}
	} else {
		return nil, errors.New("incorrect cloud secret store configuration")
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
		return "", errors.Newf("secret %s not found", name)
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
