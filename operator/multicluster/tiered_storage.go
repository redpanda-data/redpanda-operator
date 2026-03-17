// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package multicluster

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/pkg/clusterconfiguration"
)

// tieredStorageToConfiguration extracts tiered storage config entries, fixups, and env vars
// from the Storage spec. This mirrors the Helm chart's TieredStorageConfig.Translate()
// and TieredStorageCredentials.AsEnvVars() logic.
func tieredStorageToConfiguration(storage *redpandav1alpha2.StretchStorage) (map[string]any, []clusterconfiguration.Fixup, []corev1.EnvVar) {
	if storage == nil || storage.Tiered == nil {
		return nil, nil, nil
	}

	attrs := map[string]any{}
	var fixups []clusterconfiguration.Fixup
	var envVars []corev1.EnvVar

	// Translate TieredConfig struct fields to config map entries.
	if tc := storage.Tiered.Config; tc != nil {
		setPtr(attrs, "cloud_storage_enabled", tc.CloudStorageEnabled)
		setPtr(attrs, "cloud_storage_api_endpoint", tc.CloudStorageAPIEndpoint)
		setPtr(attrs, "cloud_storage_api_endpoint_port", tc.CloudStorageAPIEndpointPort)
		setPtr(attrs, "cloud_storage_bucket", tc.CloudStorageBucket)
		setPtr(attrs, "cloud_storage_azure_container", tc.CloudStorageAzureContainer)
		setPtr(attrs, "cloud_storage_azure_managed_identity_id", tc.CloudStorageAzureManagedIdentityID)
		setPtr(attrs, "cloud_storage_azure_storage_account", tc.CloudStorageAzureStorageAccount)
		setPtr(attrs, "cloud_storage_azure_shared_key", tc.CloudStorageAzureSharedKey)
		setPtr(attrs, "cloud_storage_azure_adls_endpoint", tc.CloudStorageAzureADLSEndpoint)
		setPtr(attrs, "cloud_storage_azure_adls_port", tc.CloudStorageAzureADLSPort)
		setPtr(attrs, "cloud_storage_cache_check_interval", tc.CloudStorageCacheCheckInterval)
		setPtr(attrs, "cloud_storage_cache_directory", tc.CloudStorageCacheDirectory)
		if tc.CloudStorageCacheSize != nil {
			if q, err := resource.ParseQuantity(*tc.CloudStorageCacheSize); err == nil {
				attrs["cloud_storage_cache_size"] = q.Value()
			}
		}
		setPtr(attrs, "cloud_storage_credentials_source", tc.CloudStorageCredentialsSource)
		setPtr(attrs, "cloud_storage_disable_tls", tc.CloudStorageDisableTLS)
		setPtr(attrs, "cloud_storage_enable_remote_read", tc.CloudStorageEnableRemoteRead)
		setPtr(attrs, "cloud_storage_enable_remote_write", tc.CloudStorageEnableRemoteWrite)
		setPtr(attrs, "cloud_storage_initial_backoff_ms", tc.CloudStorageInitialBackoffMs)
		setPtr(attrs, "cloud_storage_manifest_upload_timeout_ms", tc.CloudStorageManifestUploadTimeoutMs)
		setPtr(attrs, "cloud_storage_max_connection_idle_time_ms", tc.CloudStorageMaxConnectionIdleTimeMs)
		setPtr(attrs, "cloud_storage_max_connections", tc.CloudStorageMaxConnections)
		setPtr(attrs, "cloud_storage_region", tc.CloudStorageRegion)
		setPtr(attrs, "cloud_storage_segment_max_upload_interval_sec", tc.CloudStorageSegmentMaxUploadIntervalSec)
		setPtr(attrs, "cloud_storage_segment_upload_timeout_ms", tc.CloudStorageSegmentUploadTimeoutMs)
		setPtr(attrs, "cloud_storage_trust_file", tc.CloudStorageTrustFile)
		setPtr(attrs, "cloud_storage_upload_ctrl_d_coeff", tc.CloudStorageUploadCtrlDCoeff)
		setPtr(attrs, "cloud_storage_upload_ctrl_max_shares", tc.CloudStorageUploadCtrlMaxShares)
		setPtr(attrs, "cloud_storage_upload_ctrl_min_shares", tc.CloudStorageUploadCtrlMinShares)
		setPtr(attrs, "cloud_storage_upload_ctrl_p_coeff", tc.CloudStorageUploadCtrlPCoeff)
		setPtr(attrs, "cloud_storage_upload_ctrl_update_interval_ms", tc.CloudStorageUploadCtrlUpdateIntervalMs)
	}

	// Handle CredentialsSecretRef → env vars + fixups.
	creds := storage.Tiered.CredentialsSecretRef
	if creds == nil {
		return attrs, fixups, envVars
	}

	_, hasAccessKey := attrs["cloud_storage_access_key"]
	_, hasSecretKey := attrs["cloud_storage_secret_key"]
	_, hasSharedKey := attrs["cloud_storage_azure_shared_key"]

	// Azure detection: if both azure_container and azure_storage_account are
	// set in the config, we're targeting Azure Blob Storage. In that case,
	// the CredentialsSecretRef.SecretKey maps to cloud_storage_azure_shared_key
	// instead of the AWS-style cloud_storage_secret_key.
	_, hasAzureContainer := attrs["cloud_storage_azure_container"]
	_, hasAzureAccount := attrs["cloud_storage_azure_storage_account"]
	isAzure := hasAzureContainer && hasAzureAccount

	// Access key (AWS/GCS only).
	if !hasAccessKey && credSecretRefIsValid(creds.AccessKey) {
		env, fixup := credFixup("cloud_storage_access_key", "REDPANDA_CLOUD_STORAGE_ACCESS_KEY", creds.AccessKey)
		envVars = append(envVars, env)
		fixups = append(fixups, fixup)
	}

	// Secret key — target field depends on whether we're Azure or AWS/GCS.
	if credSecretRefIsValid(creds.SecretKey) {
		switch {
		case !hasSecretKey && !isAzure:
			env, fixup := credFixup("cloud_storage_secret_key", "REDPANDA_CLOUD_STORAGE_SECRET_KEY", creds.SecretKey)
			envVars = append(envVars, env)
			fixups = append(fixups, fixup)
		case !hasSharedKey && isAzure:
			env, fixup := credFixup("cloud_storage_azure_shared_key", "REDPANDA_CLOUD_STORAGE_AZURE_SHARED_KEY", creds.SecretKey)
			envVars = append(envVars, env)
			fixups = append(fixups, fixup)
		}
	}

	return attrs, fixups, envVars
}

// credFixup creates an env var + CEL fixup pair for a credential secret reference.
func credFixup(field, envName string, ref *redpandav1alpha2.SecretWithConfigField) (corev1.EnvVar, clusterconfiguration.Fixup) {
	return corev1.EnvVar{Name: envName, ValueFrom: credSecretRefSource(ref)},
		clusterconfiguration.Fixup{Field: field, CEL: fmt.Sprintf(`repr(envString("%s"))`, envName)}
}

// credSecretRefIsValid returns true if the SecretWithConfigField has both a name and key.
func credSecretRefIsValid(ref *redpandav1alpha2.SecretWithConfigField) bool {
	return ref != nil && ptr.Deref(ref.Key, "") != "" && ptr.Deref(ref.Name, "") != ""
}

// credSecretRefSource returns an EnvVarSource for a SecretWithConfigField.
func credSecretRefSource(ref *redpandav1alpha2.SecretWithConfigField) *corev1.EnvVarSource {
	return &corev1.EnvVarSource{
		SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: *ref.Name},
			Key:                  *ref.Key,
		},
	}
}
