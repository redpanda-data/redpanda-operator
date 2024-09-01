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
	"io/fs"
	"os"
	"sync"
)

type fileWriter interface {
	Write(name string, data []byte, perm fs.FileMode) error
}

type fsWriter struct{}

func (f *fsWriter) Write(name string, data []byte, perm fs.FileMode) error {
	return os.WriteFile(name, data, perm)
}

type testWriter struct {
	data   map[string][]byte
	mutex  sync.Mutex
	suffix string
	write  bool
}

func (f *testWriter) Write(name string, data []byte, perm fs.FileMode) error {
	f.mutex.Lock()
	f.data[name+f.suffix] = data
	f.mutex.Unlock()
	if f.write {
		return os.WriteFile(name+f.suffix, data, perm)
	}
	return nil
}
