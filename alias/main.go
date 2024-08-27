// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package main

import (
	"os"
	"strings"
	"syscall"
)

var AliasTo string

func main() {
	if AliasTo == "" {
		panic("AliasTo not set. Did you miss the `-X main.AliasTo=` build flags?")
	}

	to := strings.Split(AliasTo, " ")
	argv := append(to, os.Args[1:]...)

	if err := syscall.Exec(to[0], argv, os.Environ()); err != nil {
		panic(err)
	}

	panic("unreachable")
}
