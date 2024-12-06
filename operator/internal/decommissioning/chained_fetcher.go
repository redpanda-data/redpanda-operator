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
	"errors"
	"fmt"
)

// ChainedFetcher delegates fetching behavior to a list of sub fetchers
// moving down the list if an error occurs in the previous fetcher.
type ChainedFetcher struct {
	fetchers []Fetcher
}

var _ Fetcher = (*ChainedFetcher)(nil)

func NewChainedFetcher(fetchers ...Fetcher) *ChainedFetcher {
	return &ChainedFetcher{fetchers: fetchers}
}

func (c *ChainedFetcher) FetchLatest(ctx context.Context, name, namespace string) (any, error) {
	if len(c.fetchers) == 0 {
		return nil, errors.New("chained fetcher does not have any supplied sub-fetchers")
	}

	errs := []error{}

	for _, fetcher := range c.fetchers {
		object, err := fetcher.FetchLatest(ctx, name, namespace)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		if object != nil {
			return object, nil
		}
	}

	return nil, fmt.Errorf("all sub-fetchers failed: %w", errors.Join(errs...))
}
