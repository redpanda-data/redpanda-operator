// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package k3d

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

func TestIntegrationMultiInstance(t *testing.T) {
	testutil.SkipIfNotIntegration(t)

	clusters := 3

	var running sync.WaitGroup
	running.Add(clusters)

	errCh := make(chan error, clusters)
	done := make(chan struct{})
	defer func() {
		close(done)
		running.Wait()
	}()

	for i := 0; i < clusters; i++ {
		go func() {
			defer running.Done()

			name := fmt.Sprintf("cluster-%d", i)
			_, err := GetOrCreate(name, WithAgents(1))
			defer forceCleanup(name)
			errCh <- err

			<-done
		}()
	}

	errs := []error{}
	for i := 0; i < clusters; i++ {
		if err := <-errCh; err != nil {
			errs = append(errs, err)
		}
	}

	assert.NoError(t, errors.Join(errs...))
}
