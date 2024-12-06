// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package decommissioning

import (
	"context"

	rpkconfig "github.com/redpanda-data/redpanda/src/go/rpk/pkg/config"
	"github.com/spf13/afero"
)

// RPKProfileFetcher loads up an RPK profile used in initializing a cluster
// connection. It should only ever be used when we only care about managing
// a single cluster, as it ignores the name and the namespace handed it and
// solely uses a profile found on disk.
type RPKProfileFetcher struct {
	configPath string
}

var _ Fetcher = (*RPKProfileFetcher)(nil)

func NewRPKProfileFetcher(configPath string) *RPKProfileFetcher {
	return &RPKProfileFetcher{configPath: configPath}
}

func (f *RPKProfileFetcher) FetchLatest(_ context.Context, _, _ string) (any, error) {
	params := rpkconfig.Params{ConfigFlag: f.configPath}

	config, err := params.Load(afero.NewOsFs())
	if err != nil {
		return nil, err
	}

	return config.VirtualProfile(), nil
}
