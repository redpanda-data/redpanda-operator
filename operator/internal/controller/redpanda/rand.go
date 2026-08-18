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
	"crypto/rand"
	"math/big"
)

const alphaNumChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// randAlphaNum returns a cryptographically random alphanumeric string of the
// given length. It mirrors gotohelm/helmette.RandAlphaNum (sprig's
// randAlphaNum) so the stretch controllers don't depend on the chart runtime.
func randAlphaNum(length int) string {
	b := make([]byte, length)
	charCount := big.NewInt(int64(len(alphaNumChars)))
	for i := range b {
		n, err := rand.Int(rand.Reader, charCount)
		if err != nil {
			// crypto/rand failures are not expected; panic mirrors the
			// effective behavior of the previous helper which could not
			// surface an error to callers.
			panic(err)
		}
		b[i] = alphaNumChars[n.Int64()]
	}
	return string(b)
}
