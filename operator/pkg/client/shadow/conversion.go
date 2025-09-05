package shadow

import (
	adminv2api "buf.build/gen/go/redpandadata/core/protocolbuffers/go/redpanda/core/admin/v2"
	"buf.build/gen/go/redpandadata/core/protocolbuffers/go/redpanda/core/common"
	"google.golang.org/protobuf/types/known/durationpb"

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
	}
}

func convertTLSSettingsToAPITLSConfig(tlsSettings *TLSSettings) *adminv2api.TLSSettings {
	if tlsSettings == nil {
		return nil
	}
	settings := &adminv2api.TLSSettings{}
	settings.SetTlsPemSettings(&adminv2api.TLSPEMSettings{
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
		Interval:     durationpb.New(options.Interval),
		Enabled:      options.Enabled,
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
	return &adminv2api.TopicMetadataSyncOptions{
		Interval:                durationpb.New(options.Interval),
		TopicFilters:            functional.MapFn(convertCRDToAPINameFilter, options.TopicFilters),
		ShadowedTopicProperties: options.ShadowedTopicProperties,
	}
}

func convertCRDToCommonPatternType(patternType *redpandav1alpha2.PatternType) common.ACLPattern {
	if patternType == nil {
		return common.ACLPattern_ACL_PATTERN_ANY
	}

	return map[redpandav1alpha2.PatternType]common.ACLPattern{
		redpandav1alpha2.PatternTypeLiteral:  common.ACLPattern_ACL_PATTERN_LITERAL,
		redpandav1alpha2.PatternTypePrefixed: common.ACLPattern_ACL_PATTERN_PREFIXED,
		// handle match?
	}[*patternType]
}

func convertCRDToCommonResourceType(resourceType *redpandav1alpha2.ResourceType) common.ACLResource {
	if resourceType == nil {
		return common.ACLResource_ACL_RESOURCE_ANY
	}

	return map[redpandav1alpha2.ResourceType]common.ACLResource{
		redpandav1alpha2.ResourceTypeTopic:           common.ACLResource_ACL_RESOURCE_TOPIC,
		redpandav1alpha2.ResourceTypeGroup:           common.ACLResource_ACL_RESOURCE_GROUP,
		redpandav1alpha2.ResourceTypeCluster:         common.ACLResource_ACL_RESOURCE_CLUSTER,
		redpandav1alpha2.ResourceTypeTransactionalID: common.ACLResource_ACL_RESOURCE_TXN_ID,
		// other types?
	}[*resourceType]
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
		Principal: filter.Principal,
		Host:      filter.Host,
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
		Interval:         durationpb.New(options.Interval),
		Enabled:          options.Enabled,
		RoleFilters:      functional.MapFn(convertCRDToAPINameFilter, options.RoleFilters),
		ScramCredFilters: functional.MapFn(convertCRDToAPINameFilter, options.ScramCredentialFilters),
		AclFilters:       functional.MapFn(convertCRDToAPIACLFilter, options.ACLFilters),
	}
}

func convertAPIToCRDStatus(status *adminv2api.ShadowLinkStatus) redpandav1alpha2.ShadowLinkStatus {
	return redpandav1alpha2.ShadowLinkStatus{
		State:               convertAPIToCRDState(status.State),
		TaskStatuses:        functional.MapFn(convertAPIToCRDTaskStatus, status.TaskStatuses),
		ShadowTopicStatuses: functional.MapFn(convertAPIToCRDTopicStatus, status.ShadowTopicStatuses),
	}
}

func convertAPIToCRDState(state adminv2api.ShadowLinkState) redpandav1alpha2.ShadowLinkState {
	return map[adminv2api.ShadowLinkState]redpandav1alpha2.ShadowLinkState{
		adminv2api.ShadowLinkState_SHADOW_LINK_STATE_ACTIVE:       redpandav1alpha2.ShadowLinkStateActive,
		adminv2api.ShadowLinkState_SHADOW_LINK_STATE_PAUSED:       redpandav1alpha2.ShadowLinkStatePaused,
		adminv2api.ShadowLinkState_SHADOW_LINK_STATE_FAILING_OVER: redpandav1alpha2.ShadowLinkStateFailingOver,
		adminv2api.ShadowLinkState_SHADOW_LINK_STATE_FAILED_OVER:  redpandav1alpha2.ShadowLinkStateFailedOver,
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

func convertAPIToCRDTopicStatus(status *adminv2api.ShadowTopicStatus) redpandav1alpha2.ShadowTopicStatus {
	return redpandav1alpha2.ShadowTopicStatus{
		Name:                 status.Name,
		TopicID:              status.TopicId,
		State:                convertAPIToCRDTopicStatusState(status.State),
		PartitionInformation: functional.MapFn(convertAPIToCRDPartitionInformation, status.PartitionInformation),
	}
}

func convertAPIToCRDTopicStatusState(state adminv2api.ShadowTopicState) redpandav1alpha2.ShadowTopicState {
	return map[adminv2api.ShadowTopicState]redpandav1alpha2.ShadowTopicState{
		adminv2api.ShadowTopicState_SHADOW_TOPIC_STATE_ACTIVE:   redpandav1alpha2.ShadowTopicStateActive,
		adminv2api.ShadowTopicState_SHADOW_TOPIC_STATE_FAULTED:  redpandav1alpha2.ShadowTopicStateFaulted,
		adminv2api.ShadowTopicState_SHADOW_TOPIC_STATE_PAUSED:   redpandav1alpha2.ShadowTopicStatePaused,
		adminv2api.ShadowTopicState_SHADOW_TOPIC_STATE_PROMOTED: redpandav1alpha2.ShadowTopicStatePromoted,
	}[state]
}

func convertAPIToCRDPartitionInformation(info *adminv2api.TopicPartitionInformation) redpandav1alpha2.TopicPartitionInformation {
	return redpandav1alpha2.TopicPartitionInformation{
		PartitionID:            info.PartitionId,
		SourceLastStableOffset: info.SourceLastStableOffset,
		SourceHighWatermark:    info.SourceHighWatermark,
		HighWatermark:          info.HighWatermark,
	}
}
