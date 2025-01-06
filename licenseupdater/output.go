// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package main

import (
	"io/fs"
	"os"
)

type fsWriter struct {
	suffix string
	write  bool
	differ *checkDiffs
}

func (f *fsWriter) Write(name string, data []byte, perm fs.FileMode) error {
	name = name + f.suffix

	if f.write {
		return os.WriteFile(name, data, perm)
	}

	if f.differ != nil {
		if err := f.differ.diff(name, data); err != nil {
			return err
		}
	}

	return nil
}
