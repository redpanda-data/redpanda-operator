// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package enterprisedrift

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	entcontroller "github.com/redpanda-data/redpanda-operator/enterprise/operator/controller"
	syncclusterconfig "github.com/redpanda-data/redpanda-operator/operator/cmd/syncclusterconfig"
)

// These tests pin the superusers helpers inlined into the enterprise
// controller package (enterprise/operator/controller/superusers.go) to the
// syncclusterconfig originals. They exist because the helpers are duplicated
// on purpose: the enterprise module must not import syncclusterconfig. If one
// of these fails, port the syncclusterconfig change into the enterprise copy
// (or vice versa).

func TestNormalizeSuperusersMatchesSyncClusterConfig(t *testing.T) {
	cases := [][]string{
		nil,
		{},
		{"a"},
		{"b", "a", "b", "a"},
		{"z", "z", "z"},
		{"user1", "User1", "user1 ", ""},
	}

	rng := rand.New(rand.NewSource(0))
	for range 100 {
		n := rng.Intn(20)
		entries := make([]string, n)
		for i := range entries {
			entries[i] = fmt.Sprintf("user-%d", rng.Intn(10))
		}
		cases = append(cases, entries)
	}

	for _, entries := range cases {
		require.Equal(t, syncclusterconfig.NormalizeSuperusers(entries), entcontroller.NormalizeSuperusers(entries), "input: %v", entries)
	}
}

func TestLoadUsersFileMatchesSyncClusterConfig(t *testing.T) {
	cases := []string{
		"",
		"alice:password:SCRAM-SHA-256",
		"alice:password",
		"malformed",
		"a:b:c:d",
		"alice:password:SCRAM-SHA-256\nbob:hunter2\n\ncharlie",
		strings.Repeat("user:pw:SCRAM-SHA-512\n", 50),
		"trailing:newline:SCRAM-SHA-256\n",
		":empty:name",
	}

	ctx := t.Context()
	for _, contents := range cases {
		require.Equal(t,
			syncclusterconfig.LoadUsersFile(ctx, "users.txt", []byte(contents)),
			entcontroller.LoadUsersFile(ctx, "users.txt", []byte(contents)),
			"input: %q", contents)
	}
}
