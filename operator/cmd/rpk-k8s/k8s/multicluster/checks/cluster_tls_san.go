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
	"net"
	"strings"
)

// TLSSANCheck re-validates TLS certificate SANs after raft status and the
// Deployment have been inspected. It checks that the cert would accept a
// TLS handshake for the address this cluster publishes to its peers via
// the Deployment's --peer=<self>://<addr>:9443 flag — delegating to
// crypto/x509.Certificate.VerifyHostname so DNS wildcards and IP SANs
// are handled the same way Go's TLS stack would handle them during an
// actual peer dial.
type TLSSANCheck struct{}

func (c *TLSSANCheck) Name() string { return "tls-san" }

func (c *TLSSANCheck) Run(_ context.Context, cc *CheckContext) []Result {
	if cc.TLSCert == nil || cc.RaftStatus == nil || cc.RaftStatus.Name == "" || cc.DeployArgs == nil {
		return nil
	}

	// Find the --peer entry that names this cluster itself.
	selfName := cc.RaftStatus.Name
	var selfAddr string
	for _, p := range ExtractFlagAll(cc.DeployArgs, "--peer") {
		// Peer format: name://host-or-ip:port
		name, rest, ok := strings.Cut(p, "://")
		if !ok || name != selfName {
			continue
		}
		// Strip the port. SplitHostPort handles IPv6 brackets; fall
		// back to the raw value if there's no port present.
		if host, _, err := net.SplitHostPort(rest); err == nil {
			selfAddr = host
		} else {
			selfAddr = rest
		}
		break
	}

	secretName := ""
	if cc.TLSSecret != nil {
		secretName = cc.TLSSecret.Name
	}

	if selfAddr == "" {
		return []Result{Fail(c.Name(), fmt.Sprintf(
			"no --peer=%s://... flag on Deployment — cannot determine expected SAN",
			selfName,
		))}
	}

	if err := cc.TLSCert.VerifyHostname(selfAddr); err != nil {
		return []Result{Fail(c.Name(), fmt.Sprintf(
			"secret %s: tls.crt does not authenticate peer address %q: %v (DNS SANs: %v, IP SANs: %v)",
			secretName, selfAddr, err, cc.TLSCert.DNSNames, cc.TLSCert.IPAddresses,
		))}
	}

	return []Result{Pass(c.Name(), fmt.Sprintf(
		"tls.crt authenticates peer address %s (DNS SANs: %v, IP SANs: %v)",
		selfAddr, cc.TLSCert.DNSNames, cc.TLSCert.IPAddresses,
	))}
}
