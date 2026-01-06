// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package shadow

import (
	"sort"

	adminv2api "buf.build/gen/go/redpandadata/core/protocolbuffers/go/redpanda/core/admin/v2"
	commonv1 "buf.build/gen/go/redpandadata/core/protocolbuffers/go/redpanda/core/common/v1"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"k8s.io/utils/ptr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/functional"
)

type RemoteClusterSettings struct {
	BootstrapServers []string
	TLSSettings      *TLSSettings
	Authentication   *AuthenticationSettings
}

type TLSSettings struct {
	// all of the following fields are in PEM format
	CA string
	// Key and Cert are optional but if one is provided, then both must be
	Key  string
	Cert string

	// The SHA-256 of the key, in base64 format
	KeyFingerprint string
}

type AuthenticationSettings struct {
	Username  string
	Password  string
	Mechanism redpandav1alpha2.SASLMechanism
}

func convertCRDToAPIShadowLink(link *redpandav1alpha2.ShadowLink, remoteClusterSettings RemoteClusterSettings) *adminv2api.ShadowLink {
	return &adminv2api.ShadowLink{
		Name:           link.Name,
		Configurations: convertCRDToAPIShadowLinkConfiguration(link, remoteClusterSettings),
	}
}

func convertCRDToAPIShadowLinkConfiguration(link *redpandav1alpha2.ShadowLink, remoteClusterSettings RemoteClusterSettings) *adminv2api.ShadowLinkConfigurations {
	return &adminv2api.ShadowLinkConfigurations{
		ClientOptions:             convertRemoteClusterSettingsToAPIShadowLinkClientOptions(remoteClusterSettings),
		TopicMetadataSyncOptions:  convertCRDToAPIShadowLinkTopicMetadataSyncOptions(link.Spec.TopicMetadataSyncOptions),
		ConsumerOffsetSyncOptions: convertCRDToAPIShadowLinkConsumerOffsetSyncOptions(link.Spec.ConsumerOffsetSyncOptions),
		SecuritySyncOptions:       convertCRDToAPIShadowLinkSecuritySyncOptions(link.Spec.SecuritySyncOptions),
		SchemaRegistrySyncOptions: convertCRDToAPISchemaRegistrySyncOptions(link.Spec.SchemaRegistrySyncOptions),
	}
}

func convertCRDToAPISchemaRegistrySyncOptions(options *redpandav1alpha2.ShadowLinkSchemaRegistrySyncOptions) *adminv2api.SchemaRegistrySyncOptions {
	if options == nil {
		return nil
	}
	apiOptions := &adminv2api.SchemaRegistrySyncOptions{}
	switch options.Mode {
	case redpandav1alpha2.ShadowLinkSchemaRegistrySyncOptionsModeTopic:
		apiOptions.SetShadowSchemaRegistryTopic(&adminv2api.SchemaRegistrySyncOptions_ShadowSchemaRegistryTopic{})
	}
	return apiOptions
}

func convertTLSSettingsToAPITLSConfig(tlsSettings *TLSSettings) *commonv1.TLSSettings {
	if tlsSettings == nil {
		return nil
	}
	settings := &commonv1.TLSSettings{
		Enabled: true,
	}
	settings.SetTlsPemSettings(&commonv1.TLSPEMSettings{
		Ca:             tlsSettings.CA,
		Cert:           tlsSettings.Cert,
		Key:            tlsSettings.Key,
		KeyFingerprint: tlsSettings.KeyFingerprint,
	})
	return settings
}

func convertScramToAPI(mechanism redpandav1alpha2.SASLMechanism) adminv2api.ScramMechanism {
	return map[redpandav1alpha2.SASLMechanism]adminv2api.ScramMechanism{
		redpandav1alpha2.SASLMechanismScramSHA256: adminv2api.ScramMechanism_SCRAM_MECHANISM_SCRAM_SHA_256,
		redpandav1alpha2.SASLMechanismScramSHA512: adminv2api.ScramMechanism_SCRAM_MECHANISM_SCRAM_SHA_512,
	}[mechanism]
}

func convertAuthenticationSettingsToAPIAuthSettings(authSettings *AuthenticationSettings) *adminv2api.AuthenticationConfiguration {
	if authSettings == nil {
		return nil
	}
	settings := &adminv2api.AuthenticationConfiguration{}
	settings.SetScramConfiguration(&adminv2api.ScramConfig{
		Username:       authSettings.Username,
		Password:       authSettings.Password,
		PasswordSet:    true,
		ScramMechanism: convertScramToAPI(authSettings.Mechanism),
	})
	return settings
}

func convertRemoteClusterSettingsToAPIShadowLinkClientOptions(remoteClusterSettings RemoteClusterSettings) *adminv2api.ShadowLinkClientOptions {
	return &adminv2api.ShadowLinkClientOptions{
		BootstrapServers:            remoteClusterSettings.BootstrapServers,
		TlsSettings:                 convertTLSSettingsToAPITLSConfig(remoteClusterSettings.TLSSettings),
		AuthenticationConfiguration: convertAuthenticationSettingsToAPIAuthSettings(remoteClusterSettings.Authentication),
	}
}

func convertCRDToAPIShadowLinkConsumerOffsetSyncOptions(options *redpandav1alpha2.ShadowLinkConsumerOffsetSyncOptions) *adminv2api.ConsumerOffsetSyncOptions {
	if options == nil {
		return nil
	}
	return &adminv2api.ConsumerOffsetSyncOptions{
		Interval:     durationpb.New(options.Interval.Duration),
		Paused:       options.Paused,
		GroupFilters: functional.MapFn(convertCRDToAPINameFilter, options.GroupFilters),
	}
}

func convertCRDToAPINameFilter(filter redpandav1alpha2.NameFilter) *adminv2api.NameFilter {
	return &adminv2api.NameFilter{
		PatternType: convertCRDToAPIPatternType(filter.PatternType),
		FilterType:  convertCRDToAPIFilterType(filter.FilterType),
		Name:        filter.Name,
	}
}

func convertCRDToAPIPatternType(patternType redpandav1alpha2.PatternType) adminv2api.PatternType {
	return map[redpandav1alpha2.PatternType]adminv2api.PatternType{
		redpandav1alpha2.PatternTypeLiteral:  adminv2api.PatternType_PATTERN_TYPE_LITERAL,
		redpandav1alpha2.PatternTypePrefixed: adminv2api.PatternType_PATTERN_TYPE_PREFIX,
	}[patternType]
}

func convertCRDToAPIFilterType(filterType redpandav1alpha2.FilterType) adminv2api.FilterType {
	return map[redpandav1alpha2.FilterType]adminv2api.FilterType{
		redpandav1alpha2.FilterTypeExclude: adminv2api.FilterType_FILTER_TYPE_EXCLUDE,
		redpandav1alpha2.FilterTypeInclude: adminv2api.FilterType_FILTER_TYPE_INCLUDE,
	}[filterType]
}

func convertCRDToAPIShadowLinkTopicMetadataSyncOptions(options *redpandav1alpha2.ShadowLinkTopicMetadataSyncOptions) *adminv2api.TopicMetadataSyncOptions {
	if options == nil {
		return nil
	}
	return setStartOffsetFromCRD(&adminv2api.TopicMetadataSyncOptions{
		Interval:                     durationpb.New(options.Interval.Duration),
		AutoCreateShadowTopicFilters: functional.MapFn(convertCRDToAPINameFilter, options.AutoCreateShadowTopicFilters),
		SyncedShadowTopicProperties:  options.SyncedShadowTopicProperties,
		ExcludeDefault:               options.ExcludeDefault,
		Paused:                       options.Paused,
	}, options)
}

func setStartOffsetFromCRD(payload *adminv2api.TopicMetadataSyncOptions, options *redpandav1alpha2.ShadowLinkTopicMetadataSyncOptions) *adminv2api.TopicMetadataSyncOptions {
	if options.StartOffset == nil {
		return payload
	}

	switch *options.StartOffset {
	case redpandav1alpha2.TopicMetadataSyncOffsetEarliest:
		payload.SetStartAtEarliest(ptr.To(adminv2api.TopicMetadataSyncOptions_EarliestOffset{}))
	case redpandav1alpha2.TopicMetadataSyncOffsetLatest:
		payload.SetStartAtLatest(ptr.To(adminv2api.TopicMetadataSyncOptions_LatestOffset{}))
	case redpandav1alpha2.TopicMetadataSyncOffsetTimestamp:
		if options.StartOffsetTimestamp == nil {
			return payload
		}
		payload.SetStartAtTimestamp(timestamppb.New(options.StartOffsetTimestamp.Time))
	}

	return payload
}

func convertCRDToCommonPatternType(patternType *redpandav1alpha2.PatternType) commonv1.ACLPattern {
	if patternType == nil {
		return commonv1.ACLPattern_ACL_PATTERN_ANY
	}

	return map[redpandav1alpha2.PatternType]commonv1.ACLPattern{
		redpandav1alpha2.PatternTypeLiteral:  commonv1.ACLPattern_ACL_PATTERN_LITERAL,
		redpandav1alpha2.PatternTypePrefixed: commonv1.ACLPattern_ACL_PATTERN_PREFIXED,
		redpandav1alpha2.PatternTypeMatch:    commonv1.ACLPattern_ACL_PATTERN_MATCH,
	}[*patternType]
}

func convertCRDToCommonResourceType(resourceType *redpandav1alpha2.ResourceType) commonv1.ACLResource {
	if resourceType == nil {
		return commonv1.ACLResource_ACL_RESOURCE_ANY
	}

	return map[redpandav1alpha2.ResourceType]commonv1.ACLResource{
		redpandav1alpha2.ResourceTypeTopic:                  commonv1.ACLResource_ACL_RESOURCE_TOPIC,
		redpandav1alpha2.ResourceTypeGroup:                  commonv1.ACLResource_ACL_RESOURCE_GROUP,
		redpandav1alpha2.ResourceTypeCluster:                commonv1.ACLResource_ACL_RESOURCE_CLUSTER,
		redpandav1alpha2.ResourceTypeTransactionalID:        commonv1.ACLResource_ACL_RESOURCE_TXN_ID,
		redpandav1alpha2.ResourceTypeSchemaRegistrySubject:  commonv1.ACLResource_ACL_RESOURCE_SR_SUBJECT,
		redpandav1alpha2.ResourceTypeSchemaRegistryRegistry: commonv1.ACLResource_ACL_RESOURCE_SR_REGISTRY,
	}[*resourceType]
}

func convertCRDToCommonOperationType(operationType *redpandav1alpha2.ACLOperation) commonv1.ACLOperation {
	if operationType == nil {
		return commonv1.ACLOperation_ACL_OPERATION_ANY
	}

	return map[redpandav1alpha2.ACLOperation]commonv1.ACLOperation{
		redpandav1alpha2.ACLOperationRead:            commonv1.ACLOperation_ACL_OPERATION_READ,
		redpandav1alpha2.ACLOperationWrite:           commonv1.ACLOperation_ACL_OPERATION_WRITE,
		redpandav1alpha2.ACLOperationDelete:          commonv1.ACLOperation_ACL_OPERATION_REMOVE,
		redpandav1alpha2.ACLOperationAlter:           commonv1.ACLOperation_ACL_OPERATION_ALTER,
		redpandav1alpha2.ACLOperationDescribe:        commonv1.ACLOperation_ACL_OPERATION_DESCRIBE,
		redpandav1alpha2.ACLOperationIdempotentWrite: commonv1.ACLOperation_ACL_OPERATION_IDEMPOTENT_WRITE,
		redpandav1alpha2.ACLOperationClusterAction:   commonv1.ACLOperation_ACL_OPERATION_CLUSTER_ACTION,
		redpandav1alpha2.ACLOperationCreate:          commonv1.ACLOperation_ACL_OPERATION_CREATE,
		redpandav1alpha2.ACLOperationAlterConfigs:    commonv1.ACLOperation_ACL_OPERATION_ALTER_CONFIGS,
		redpandav1alpha2.ACLOperationDescribeConfigs: commonv1.ACLOperation_ACL_OPERATION_DESCRIBE_CONFIGS,
	}[*operationType]
}

func convertCRDToCommonPermissionType(aclType *redpandav1alpha2.ACLType) commonv1.ACLPermissionType {
	if aclType == nil {
		return commonv1.ACLPermissionType_ACL_PERMISSION_TYPE_ANY
	}

	return map[redpandav1alpha2.ACLType]commonv1.ACLPermissionType{
		redpandav1alpha2.ACLTypeAllow: commonv1.ACLPermissionType_ACL_PERMISSION_TYPE_ALLOW,
		redpandav1alpha2.ACLTypeDeny:  commonv1.ACLPermissionType_ACL_PERMISSION_TYPE_DENY,
	}[*aclType]
}

func convertCRDToAPIACLResourceFilter(filter redpandav1alpha2.ACLResourceFilter) *adminv2api.ACLResourceFilter {
	return &adminv2api.ACLResourceFilter{
		Name:         filter.Name,
		PatternType:  convertCRDToCommonPatternType(filter.PatternType),
		ResourceType: convertCRDToCommonResourceType(filter.ResourceType),
	}
}

func convertCRDToAPIACLAccessFilter(filter redpandav1alpha2.ACLAccessFilter) *adminv2api.ACLAccessFilter {
	return &adminv2api.ACLAccessFilter{
		Principal:      filter.Principal,
		Host:           filter.Host,
		Operation:      convertCRDToCommonOperationType(filter.Operation),
		PermissionType: convertCRDToCommonPermissionType(filter.PermissionType),
	}
}

func convertCRDToAPIACLFilter(filter redpandav1alpha2.ACLFilter) *adminv2api.ACLFilter {
	return &adminv2api.ACLFilter{
		ResourceFilter: convertCRDToAPIACLResourceFilter(filter.ResourceFilter),
		AccessFilter:   convertCRDToAPIACLAccessFilter(filter.AccessFilter),
	}
}

func convertCRDToAPIShadowLinkSecuritySyncOptions(options *redpandav1alpha2.ShadowLinkSecuritySettingsSyncOptions) *adminv2api.SecuritySettingsSyncOptions {
	if options == nil {
		return nil
	}
	return &adminv2api.SecuritySettingsSyncOptions{
		Interval: durationpb.New(options.Interval.Duration),
		Paused:   options.Paused,
		// TODO: the following were recently (temporarily?) removed
		// RoleFilters:      functional.MapFn(convertCRDToAPINameFilter, options.RoleFilters),
		// ScramCredFilters: functional.MapFn(convertCRDToAPINameFilter, options.ScramCredentialFilters),
		AclFilters: functional.MapFn(convertCRDToAPIACLFilter, options.ACLFilters),
	}
}

func convertAPIToCRDStatus(status *adminv2api.ShadowLinkStatus) redpandav1alpha2.ShadowLinkStatus {
	return redpandav1alpha2.ShadowLinkStatus{
		State:               convertAPIToCRDState(status.State),
		TaskStatuses:        functional.MapFn(convertAPIToCRDTaskStatus, sortByName(status.TaskStatuses)),
		ShadowTopicStatuses: functional.MapFn(convertAPIToCRDTopicStatus, sortByName(status.ShadowTopics)),
	}
}

func convertAPIToCRDState(state adminv2api.ShadowLinkState) redpandav1alpha2.ShadowLinkState {
	return map[adminv2api.ShadowLinkState]redpandav1alpha2.ShadowLinkState{
		adminv2api.ShadowLinkState_SHADOW_LINK_STATE_ACTIVE: redpandav1alpha2.ShadowLinkStateActive,
		adminv2api.ShadowLinkState_SHADOW_LINK_STATE_PAUSED: redpandav1alpha2.ShadowLinkStatePaused,
	}[state]
}

func convertAPIToCRDTaskStatus(status *adminv2api.ShadowLinkTaskStatus) redpandav1alpha2.ShadowLinkTaskStatus {
	return redpandav1alpha2.ShadowLinkTaskStatus{
		Name:     status.Name,
		Reason:   status.Reason,
		BrokerID: status.BrokerId,
		State:    convertAPIToCRDTaskStatusState(status.State),
	}
}

func convertAPIToCRDTaskStatusState(state adminv2api.TaskState) redpandav1alpha2.TaskState {
	return map[adminv2api.TaskState]redpandav1alpha2.TaskState{
		adminv2api.TaskState_TASK_STATE_ACTIVE:           redpandav1alpha2.TaskStateActive,
		adminv2api.TaskState_TASK_STATE_FAULTED:          redpandav1alpha2.TaskStateFaulted,
		adminv2api.TaskState_TASK_STATE_PAUSED:           redpandav1alpha2.TaskStateNotRunning,
		adminv2api.TaskState_TASK_STATE_LINK_UNAVAILABLE: redpandav1alpha2.TaskStateUnavailable,
		adminv2api.TaskState_TASK_STATE_NOT_RUNNING:      redpandav1alpha2.TaskStateNotRunning,
	}[state]
}

func convertAPIToCRDTopicStatus(status *adminv2api.ShadowTopic) redpandav1alpha2.ShadowTopicStatus {
	return redpandav1alpha2.ShadowTopicStatus{
		Name:    status.Name,
		TopicID: status.TopicId,
		State:   convertAPIToCRDTopicStatusState(status.Status.State),
	}
}

func convertAPIToCRDTopicStatusState(state adminv2api.ShadowTopicState) redpandav1alpha2.ShadowTopicState {
	return map[adminv2api.ShadowTopicState]redpandav1alpha2.ShadowTopicState{
		adminv2api.ShadowTopicState_SHADOW_TOPIC_STATE_ACTIVE:       redpandav1alpha2.ShadowTopicStateActive,
		adminv2api.ShadowTopicState_SHADOW_TOPIC_STATE_FAULTED:      redpandav1alpha2.ShadowTopicStateFaulted,
		adminv2api.ShadowTopicState_SHADOW_TOPIC_STATE_PAUSED:       redpandav1alpha2.ShadowTopicStatePaused,
		adminv2api.ShadowTopicState_SHADOW_TOPIC_STATE_FAILING_OVER: redpandav1alpha2.ShadowTopicStateFailingOver,
		adminv2api.ShadowTopicState_SHADOW_TOPIC_STATE_FAILED_OVER:  redpandav1alpha2.ShadowTopicStateFailedOver,
		adminv2api.ShadowTopicState_SHADOW_TOPIC_STATE_PROMOTING:    redpandav1alpha2.ShadowTopicStatePromoting,
		adminv2api.ShadowTopicState_SHADOW_TOPIC_STATE_PROMOTED:     redpandav1alpha2.ShadowTopicStatePromoted,
	}[state]
}

type named interface {
	GetName() string
}

func sortByName[T named](v []T) []T {
	sort.SliceStable(v, func(i, j int) bool {
		return v[i].GetName() < v[j].GetName()
	})
	return v
}
