// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package bootstrap contains exports a subcommand that configures the
// .bootstrap.yaml for V2 clusters, using the same teamplate-and-fixup mechanism
// used by the V1 operator.
package bootstrap

import (
	"context"
	"fmt"
	"log"
	"path"

	"github.com/spf13/cobra"

	"github.com/redpanda-data/redpanda-operator/operator/cmd/configurator"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/clusterconfiguration"
	pkgsecrets "github.com/redpanda-data/redpanda-operator/operator/pkg/secrets"
)

func Command() *cobra.Command {
	var (
		configSrcDir  string
		configDestDir string

		cloudSecretsEnabled          bool
		cloudSecretsPrefix           string
		cloudSecretsAWSRegion        string
		cloudSecretsAWSRoleARN       string
		cloudSecretsGCPProjectID     string
		cloudSecretsAzureKeyVaultURI string
	)
	cmd := &cobra.Command{
		Use:     "bootstrap",
		Short:   "Configure .bootstrap.yaml based on supplied fixups",
		Aliases: []string{"configure"},
		Run: func(cmd *cobra.Command, args []string) {
			ctx := cmd.Context()
			var cloudExpander *pkgsecrets.CloudExpander
			if cloudSecretsEnabled {
				cloudConfig := pkgsecrets.ExpanderCloudConfiguration{}
				if cloudSecretsAWSRegion != "" {
					cloudConfig.AWSRegion = cloudSecretsAWSRegion
					cloudConfig.AWSRoleARN = cloudSecretsAWSRoleARN
				} else if cloudSecretsGCPProjectID != "" {
					cloudConfig.GCPProjectID = cloudSecretsGCPProjectID
				} else if cloudSecretsAzureKeyVaultURI != "" {
					cloudConfig.AzureKeyVaultURI = cloudSecretsAzureKeyVaultURI
				} else {
					log.Fatal("Cloud secrets are enabled but configuration for cloud provider is missing or invalid")
				}
				var err error
				cloudExpander, err = pkgsecrets.NewCloudExpander(ctx, cloudSecretsPrefix, cloudConfig)
				if err != nil {
					log.Fatalf("Unable to start manager: %s", err)
				}
			}

			run(
				ctx,
				cloudExpander,
				configSrcDir,
				configDestDir,
			)
		},
	}

	// Filesystem path-related flags
	cmd.Flags().StringVar(&configSrcDir, "in-dir", "", "Location of bootstrap template and fixups")
	cmd.Flags().StringVar(&configDestDir, "out-dir", "", "Target directory for .bootstrap.yaml")

	// secret store related flags
	cmd.Flags().BoolVar(&cloudSecretsEnabled, "enable-cloud-secrets", false, "Set to true if config values can reference secrets from cloud secret store")
	cmd.Flags().StringVar(&cloudSecretsPrefix, "cloud-secrets-prefix", "", "Prefix for all names of cloud secrets")
	cmd.Flags().StringVar(&cloudSecretsAWSRegion, "cloud-secrets-aws-region", "", "AWS Region in which the secrets are stored")
	cmd.Flags().StringVar(&cloudSecretsAWSRoleARN, "cloud-secrets-aws-role-arn", "", "AWS role ARN to assume when fetching secrets")
	cmd.Flags().StringVar(&cloudSecretsGCPProjectID, "cloud-secrets-gcp-project-id", "", "GCP project ID in which the secrets are stored")
	cmd.Flags().StringVar(&cloudSecretsAzureKeyVaultURI, "cloud-secrets-azure-key-vault-uri", "", "Azure Key Vault URI in which the secrets are stored")

	return cmd
}

func run(
	ctx context.Context,
	cloudExpander *pkgsecrets.CloudExpander,
	configSrcDir string,
	configDestDir string,
) {
	log.Print("Expanding bootstrap template file")

	err := configurator.TemplateBootstrapYaml(ctx, cloudExpander,
		path.Join(configSrcDir, clusterconfiguration.BootstrapTemplateFile),
		path.Join(configDestDir, clusterconfiguration.BootstrapTargetFile),
		path.Join(configSrcDir, clusterconfiguration.BootstrapFixupFile),
	)
	if err != nil {
		log.Fatalf("%s", fmt.Errorf("unable to template the .bootstrap.yaml file: %w", err))
	}

	log.Printf("Bootstrap saved to: %s", configDestDir)
}
