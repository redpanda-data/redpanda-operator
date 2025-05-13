// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package helm

import (
	"context"
	"net"
	"path"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

func TestHelm(t *testing.T) {
	ctx := testutil.Context(t)

	configDir := path.Join(t.TempDir(), "helm-1")

	c, err := New(Options{ConfigHome: configDir})
	require.NoError(t, err)

	repos := []Repo{
		{Name: "redpanda", URL: "https://charts.redpanda.com"},
		{Name: "jetstack", URL: "https://charts.jetstack.io"},
	}

	listedRepos, err := c.RepoList(ctx)
	require.NoError(t, err)
	require.Len(t, listedRepos, 0)

	for _, repo := range repos {
		require.NoError(t, c.RepoAdd(ctx, repo.Name, repo.URL))
	}

	listedRepos, err = c.RepoList(ctx)
	require.NoError(t, err)
	require.ElementsMatch(t, repos, listedRepos)

	charts, err := c.Search(ctx, "redpanda/redpanda")
	require.NoError(t, err)
	require.Len(t, charts, 1)
	require.Equal(t, "Redpanda is the real-time engine for modern apps.", charts[0].Description)

	t.Run("GracefulShutdown", func(t *testing.T) {
		lis, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		t.Cleanup(func() {
			_ = lis.Close()
		})

		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		// In order to make helm hang, we tell it to connect to the listener
		// that's been established above which simply won't respond.
		errCh := make(chan error, 1)
		go func() {
			_, _, err := c.runHelm(ctx, "list", "--kube-apiserver", lis.Addr().String())
			errCh <- err
		}()

		// When helm connects to our listener we know the process has started
		// and that cancelling our context will result in the signalling
		// behavior we're looking to exercise.
		_, err = lis.Accept()
		require.NoError(t, err)

		cancel()

		select {
		case <-time.After(10 * time.Second):
			t.Fatalf("helm did not shutdown as expected")
		case err := <-errCh:
			// Assert that the error indicates that the context was canceled and that helm received SIGINT.
			require.ErrorIs(t, err, context.Canceled)
			require.Contains(t, err.Error(), "interrupt")
		}
	})
}
