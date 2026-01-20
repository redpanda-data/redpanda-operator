// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package backends

import (
	"context"
	"errors"

	adminv2api "buf.build/gen/go/redpandadata/core/protocolbuffers/go/redpanda/core/admin/v2"
	"connectrpc.com/connect"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/api/virtual"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/functional"
)

type ShadowLinkBackend struct {
	*EmptyBackend[*virtual.ShadowLink]
	factory client.ClientFactory
}

func NewShadowLinkBackend(factory client.ClientFactory) *ShadowLinkBackend {
	return &ShadowLinkBackend{
		EmptyBackend: NewEmptyBackend[*virtual.ShadowLink](),
		factory:      factory,
	}
}

func (b *ShadowLinkBackend) Read(ctx context.Context, cluster *redpandav1alpha2.Redpanda, id string) (*virtual.ShadowLink, error) {
	client, err := b.factory.RedpandaAdminClient(ctx, cluster)
	if err != nil {
		return nil, err
	}

	service := client.ShadowLinkService()
	response, err := service.GetShadowLink(ctx, connect.NewRequest(&adminv2api.GetShadowLinkRequest{
		Name: id,
	}))
	if err != nil {
		if connectErr := new(connect.Error); errors.As(err, &connectErr) && connectErr.Code() == connect.CodeNotFound {
			return nil, nil
		}
		return nil, err
	}

	return &virtual.ShadowLink{
		ObjectMeta: metav1.ObjectMeta{
			Name:      response.Msg.ShadowLink.Name,
			Namespace: cluster.Namespace,
		},
		Spec: virtual.ShadowLinkSpec{
			ClusterRef: virtual.ClusterRef{
				Name: cluster.Name,
			},
			TopicMetadataSyncOptions: virtual.ShadowLinkTopicMetadataSyncOptions{
				Interval: &metav1.Duration{
					Duration: response.Msg.ShadowLink.Configurations.TopicMetadataSyncOptions.EffectiveInterval.AsDuration(),
				},
				AutoCreateShadowTopicFilters: functional.MapFn(mapFilter, response.Msg.ShadowLink.Configurations.TopicMetadataSyncOptions.AutoCreateShadowTopicFilters),
				SyncedShadowTopicProperties:  response.Msg.ShadowLink.Configurations.TopicMetadataSyncOptions.SyncedShadowTopicProperties,
				ExcludeDefault:               response.Msg.ShadowLink.Configurations.TopicMetadataSyncOptions.ExcludeDefault,
				Paused:                       response.Msg.ShadowLink.Configurations.TopicMetadataSyncOptions.Paused,
				StartOffset:                  mapStartOffset(response.Msg.ShadowLink.Configurations.TopicMetadataSyncOptions),
				StartOffsetTimestamp:         mapStartOffsetTimestamp(response.Msg.ShadowLink.Configurations.TopicMetadataSyncOptions),
			},
		},
		Status: virtual.ShadowLinkStatus{},
	}, nil
}

func mapFilter(f *adminv2api.NameFilter) virtual.NameFilter {
	return virtual.NameFilter{
		Name:        f.Name,
		FilterType:  mapFilterType(f.FilterType),
		PatternType: mapPatternType(f.PatternType),
	}
}

func mapFilterType(ft adminv2api.FilterType) virtual.FilterType {
	return map[string]virtual.FilterType{
		"FILTER_TYPE_INCLUDE": virtual.FilterTypeInclude,
		"FILTER_TYPE_EXCLUDE": virtual.FilterTypeExclude,
	}[ft.String()]
}

func mapPatternType(pt adminv2api.PatternType) virtual.PatternType {
	return map[string]virtual.PatternType{
		"PATTERN_TYPE_LITERAL":  virtual.PatternTypeLiteral,
		"PATTERN_TYPE_PREFIX":   virtual.PatternTypePrefixed,
		"PATTERN_TYPE_PREFIXED": virtual.PatternTypePrefixed,
	}[pt.String()]
}

func mapStartOffset(options *adminv2api.TopicMetadataSyncOptions) *virtual.TopicMetadataSyncOffset {
	switch options.StartOffset.(type) {
	case *adminv2api.TopicMetadataSyncOptions_StartAtEarliest:
		return ptr.To(virtual.TopicMetadataSyncOffsetEarliest)
	case *adminv2api.TopicMetadataSyncOptions_StartAtLatest:
		return ptr.To(virtual.TopicMetadataSyncOffsetLatest)
	case *adminv2api.TopicMetadataSyncOptions_StartAtTimestamp:
		return ptr.To(virtual.TopicMetadataSyncOffsetTimestamp)
	default:
		return nil
	}
}

func mapStartOffsetTimestamp(options *adminv2api.TopicMetadataSyncOptions) *metav1.Time {
	switch startOffset := options.StartOffset.(type) {
	case *adminv2api.TopicMetadataSyncOptions_StartAtTimestamp:
		return &metav1.Time{
			Time: startOffset.StartAtTimestamp.AsTime(),
		}
	default:
		return nil
	}
}
