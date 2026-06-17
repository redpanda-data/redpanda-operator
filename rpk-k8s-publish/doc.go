// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Command rpk-k8s-publish packages the cross-compiled rpk-k8s binaries as
// rpk managed-plugin archives, uploads them to the rpk-plugins S3 bucket,
// and regenerates the plugin manifest. It is invoked from the operator
// release workflow on operator/v* tags.
package main
