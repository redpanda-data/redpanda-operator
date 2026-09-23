// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package k3d

import (
	"fmt"
	"sync"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

func TestClusterCreateArgs(t *testing.T) {
	fastDetectionArgs := []string{
		`--kube-controller-manager-arg=node-monitor-grace-period=10s@server:*`,
		`--kube-apiserver-arg=default-not-ready-toleration-seconds=10@server:*`,
		`--kube-apiserver-arg=default-unreachable-toleration-seconds=10@server:*`,
	}

	t.Run("kube default node health unless opted in", func(t *testing.T) {
		args := clusterCreateArgs("test", defaultClusterConfig())
		for _, arg := range fastDetectionArgs {
			require.NotContains(t, args, arg)
		}
	})

	t.Run("WithFastNodeFailureDetection", func(t *testing.T) {
		config := defaultClusterConfig()
		WithFastNodeFailureDetection().apply(config)
		args := clusterCreateArgs("test", config)
		for _, arg := range fastDetectionArgs {
			require.Contains(t, args, arg)
		}
	})
}

func TestNormalizeImageRef(t *testing.T) {
	for given, want := range map[string]string{
		"localhost/redpanda-operator:dev":                "localhost/redpanda-operator:dev",
		"redpandadata/redpanda:v25.2.1":                  "docker.io/redpandadata/redpanda:v25.2.1",
		"rancher/mirrored-library-busybox:1.36.1":        "docker.io/rancher/mirrored-library-busybox:1.36.1",
		"busybox:1.36.1":                                 "docker.io/library/busybox:1.36.1",
		"busybox":                                        "docker.io/library/busybox:latest",
		"redpandadata/redpanda":                          "docker.io/redpandadata/redpanda:latest",
		"quay.io/jetstack/cert-manager-controller:v1.17": "quay.io/jetstack/cert-manager-controller:v1.17",
		"ghcr.io/loft-sh/vcluster-pro:4.4.0":             "ghcr.io/loft-sh/vcluster-pro:4.4.0",
		"registry:5000/img":                              "registry:5000/img:latest",
	} {
		require.Equal(t, want, normalizeImageRef(given), "input %q", given)
	}
}
