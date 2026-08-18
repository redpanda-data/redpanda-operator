// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/redpanda-data/common-go/otelutil/log"
)

// Pure superuser helpers, inlined from the syncclusterconfig package (which is
// otherwise chart/admin-coupled and stays behind when the stretch controllers
// move to the enterprise module). Behavioral equivalence with the originals is
// asserted by superusers_drift_test.go.

// normalizeSuperusers de-duplicates and sorts the superusers.
func normalizeSuperusers(entries []string) []string {
	if len(entries) == 0 {
		return []string{}
	}

	var sorted sort.StringSlice
	unique := make(map[string]struct{})
	for _, value := range entries {
		if _, ok := unique[value]; !ok {
			sorted = append(sorted, value)
		}
		unique[value] = struct{}{}
	}

	sorted.Sort()
	return sorted
}

// loadUsersFile parses a superusers file (Format:
// USER_NAME:PASSWORD:SASL_MECHANISM_TYPE) and returns the list of user names.
func loadUsersFile(ctx context.Context, filename string, usersFile []byte) []string {
	scanner := bufio.NewScanner(bytes.NewReader(usersFile))

	users := []string{}
	i := 0
	for scanner.Scan() {
		i++
		line := scanner.Text()
		tokens := strings.Split(line, ":")
		if len(tokens) != 2 && len(tokens) != 3 {
			log.FromContext(ctx).Info(fmt.Sprintf("Skipping malformatted line number %d in file %q", i, filename))
			continue
		}
		users = append(users, tokens[0])
	}

	return users
}
