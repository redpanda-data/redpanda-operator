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
)

// Fetcher acts as a mechanism for fetching an object that can be used in initializing
// a connection to Redpanda. This can come in the form of a Redpanda CR or an RPK profile.
type Fetcher interface {
	FetchLatest(ctx context.Context, name, namespace string) (any, error)
}
