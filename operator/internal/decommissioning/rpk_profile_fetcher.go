// Copyright 2025 Redpanda Data, Inc.
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
	"sync"

	rpkconfig "github.com/redpanda-data/redpanda/src/go/rpk/pkg/config"
	"github.com/spf13/afero"
)

// RPKProfileFetcher loads up an RPK profile used in initializing a cluster
// connection. It should only ever be used when we only care about managing
// a single cluster, as it ignores the name and the namespace handed it and
// solely uses a profile found on disk.
type RPKProfileFetcher struct {
	configPath string
	fs         afero.Fs
	profile    *rpkconfig.RpkProfile
	mutex      sync.Mutex
}

var _ Fetcher = (*RPKProfileFetcher)(nil)

func NewRPKProfileFetcher(configPath string) *RPKProfileFetcher {
	return &RPKProfileFetcher{configPath: configPath, fs: afero.NewOsFs()}
}

func (f *RPKProfileFetcher) FetchLatest(_ context.Context, _, _ string) (any, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	if f.profile != nil {
		// returned the memoized profile so we don't have to keep reading it, if we need
		// to handle the profile on disk changing, then we should implement something like
		// an fsnotify watcher that clears the memoized profile when the file changes on disk
		return f.profile, nil
	}

	params := rpkconfig.Params{ConfigFlag: f.configPath}

	config, err := params.Load(f.fs)
	if err != nil {
		return nil, err
	}

	f.profile = config.VirtualProfile()

	return f.profile, nil
}
