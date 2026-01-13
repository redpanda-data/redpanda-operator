// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package lifecycle

import "fmt"

// CloudSecretsFlags contains the flags required to generate a default set of
// CLI arguments to the configurator / bootstrap templater to correctly instantiate
// a CloudExpander.
// TODO: find a way to deduplicate this machinery - perhaps move to a standard configuration struct in pkgsecrets
type CloudSecretsFlags struct {
	CloudSecretsEnabled          bool
	CloudSecretsPrefix           string
	CloudSecretsAWSRegion        string
	CloudSecretsAWSRoleARN       string
	CloudSecretsGCPProjectID     string
	CloudSecretsAzureKeyVaultURI string
}

// AdditionalConfiguratorArgs constructs a "standard" set of arguments to pass to a
// configurator (or bootstrap) initContainer, to specify the CloudExpander to instantiate.
func (c CloudSecretsFlags) AdditionalConfiguratorArgs() []string {
	var result []string
	if c.CloudSecretsEnabled {
		result = append(result, "--enable-cloud-secrets=true")
		result = append(result, fmt.Sprintf("--cloud-secrets-prefix=%s", c.CloudSecretsPrefix))
		if c.CloudSecretsAWSRegion != "" {
			result = append(result, fmt.Sprintf("--cloud-secrets-aws-region=%s", c.CloudSecretsAWSRegion))
		}
		if c.CloudSecretsAWSRoleARN != "" {
			result = append(result, fmt.Sprintf("--cloud-secrets-aws-role-arn=%s", c.CloudSecretsAWSRoleARN))
		}
		if c.CloudSecretsGCPProjectID != "" {
			result = append(result, fmt.Sprintf("--cloud-secrets-gcp-project-id=%s", c.CloudSecretsGCPProjectID))
		}
		if c.CloudSecretsAzureKeyVaultURI != "" {
			result = append(result, fmt.Sprintf("--cloud-secrets-azure-key-vault-uri=%s", c.CloudSecretsAzureKeyVaultURI))
		}
	}
	return result
}
