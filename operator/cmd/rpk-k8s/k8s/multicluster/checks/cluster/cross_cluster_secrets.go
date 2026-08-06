// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package cluster

import (
	"bytes"
	"fmt"
	"strings"
)

// BootstrapSecretConsistencyCheck verifies that the bootstrap user password
// is identical across all clusters. Mismatches cause reconciliation to block
// with a PasswordMismatch condition.
type BootstrapSecretConsistencyCheck struct{}

func (c *BootstrapSecretConsistencyCheck) Name() string { return "bootstrap-secret" }

func (c *BootstrapSecretConsistencyCheck) Run(contexts []*CheckContext) []Result {
	type entry struct {
		context  string
		password []byte
	}

	var entries []entry
	for _, cc := range contexts {
		if cc.BootstrapSecret == nil {
			continue
		}
		pw, ok := cc.BootstrapSecret.Data[bootstrapUserPasswordKey]
		if !ok || len(pw) == 0 {
			continue
		}
		entries = append(entries, entry{context: cc.Context, password: pw})
	}

	if len(entries) < 2 {
		return []Result{Skip(c.Name(), "bootstrap secret found on fewer than 2 clusters")}
	}

	reference := entries[0]
	var mismatched []string
	for _, e := range entries[1:] {
		if !bytes.Equal(reference.password, e.password) {
			mismatched = append(mismatched, e.context)
		}
	}

	if len(mismatched) > 0 {
		return []Result{Fail(c.Name(),
			fmt.Sprintf("bootstrap user password differs from %s on: %s — delete the incorrect secret(s) and let the operator recreate them",
				reference.context, strings.Join(mismatched, ", ")))}
	}

	return []Result{Pass(c.Name(),
		fmt.Sprintf("bootstrap user password consistent across %d clusters", len(entries)))}
}

// CASecretConsistencyCheck verifies that CA certificate data is identical
// across all clusters for each CA secret name.
type CASecretConsistencyCheck struct{}

func (c *CASecretConsistencyCheck) Name() string { return "ca-consistency" }

func (c *CASecretConsistencyCheck) Run(contexts []*CheckContext) []Result {
	// Group CA secrets by name across clusters.
	type entry struct {
		context string
		data    []byte
	}
	byName := map[string][]entry{}

	for _, cc := range contexts {
		for _, s := range cc.CASecrets {
			caCert, ok := s.Data["ca.crt"]
			if !ok {
				continue
			}
			byName[s.Name] = append(byName[s.Name], entry{context: cc.Context, data: caCert})
		}
	}

	if len(byName) == 0 {
		return []Result{Skip(c.Name(), "no CA secrets found on any cluster")}
	}

	var results []Result
	for name, entries := range byName {
		if len(entries) < 2 {
			results = append(results, Skip(c.Name(),
				fmt.Sprintf("CA secret %s found on only 1 cluster", name)))
			continue
		}

		reference := entries[0]
		var mismatched []string
		for _, e := range entries[1:] {
			if !bytes.Equal(reference.data, e.data) {
				mismatched = append(mismatched, e.context)
			}
		}

		if len(mismatched) > 0 {
			results = append(results, Fail(c.Name(),
				fmt.Sprintf("CA secret %s differs from %s on: %s",
					name, reference.context, strings.Join(mismatched, ", "))))
		} else {
			results = append(results, Pass(c.Name(),
				fmt.Sprintf("CA secret %s consistent across %d clusters", name, len(entries))))
		}
	}
	return results
}
