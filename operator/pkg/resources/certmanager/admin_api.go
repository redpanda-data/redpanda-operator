// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package certmanager contains resources for TLS certificate handling using cert-manager
package certmanager

const (
	adminAPI           = "admin"
	adminAPIClientCert = "admin-api-client"
	adminAPINodeCert   = "admin-api-node"

	// adminAPITrustedClientCAs is the suffix of the certificate bundle name
	adminAPITrustedClientCAs = "admin-api-trusted-client-ca"
)
