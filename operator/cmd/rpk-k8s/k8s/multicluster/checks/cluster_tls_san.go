// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package checks

import (
	"context"
	"fmt"
	"strings"
)

// TLSSANCheck re-validates TLS certificate SANs after raft status is available.
// This runs after both TLSCheck and RaftCheck have populated CheckContext.
type TLSSANCheck struct{}

func (c *TLSSANCheck) Name() string { return "tls-san" }

func (c *TLSSANCheck) Run(_ context.Context, cc *CheckContext) []Result {
	if cc.TLSCert == nil || cc.RaftStatus == nil || cc.RaftStatus.Name == "" {
		return nil
	}
	expectedName := cc.RaftStatus.Name
	for _, dns := range cc.TLSCert.DNSNames {
		if strings.Contains(dns, expectedName) {
			return []Result{Pass(c.Name(), fmt.Sprintf("tls.crt SAN matches expected name %q", expectedName))}
		}
	}
	secretName := ""
	if cc.TLSSecret != nil {
		secretName = cc.TLSSecret.Name
	}
	return []Result{Fail(c.Name(), fmt.Sprintf("secret %s: tls.crt SANs %v do not contain expected name %q", secretName, cc.TLSCert.DNSNames, expectedName))}
}
