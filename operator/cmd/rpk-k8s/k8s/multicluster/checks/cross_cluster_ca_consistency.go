// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package checks

// CAConsistencyCheck verifies that all clusters share the same CA certificate.
type CAConsistencyCheck struct{}

func (c *CAConsistencyCheck) Name() string { return "ca-consistency" }

func (c *CAConsistencyCheck) Run(contexts []*CheckContext) []Result {
	type caInfo struct {
		context string
		raw     []byte
	}
	var cas []caInfo
	for _, cc := range contexts {
		if cc.CACert != nil {
			cas = append(cas, caInfo{context: cc.Context, raw: cc.CACert.Raw})
		}
	}

	if len(cas) < 2 {
		return nil // not enough data to compare
	}

	allSame := true
	for i := 1; i < len(cas); i++ {
		if string(cas[i].raw) != string(cas[0].raw) {
			allSame = false
			break
		}
	}

	if allSame {
		return []Result{Pass(c.Name(), "all clusters share the same CA")}
	}
	return []Result{Fail(c.Name(), "CA certificate mismatch across clusters — raft mTLS will fail")}
}
