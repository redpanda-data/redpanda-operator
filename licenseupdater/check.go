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
	"bytes"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/sergi/go-diff/diffmatchpatch"
)

type diff struct {
	path  string
	diffs []diffmatchpatch.Diff
}

func (d *diff) string(differ *diffmatchpatch.DiffMatchPatch) string {
	diff := differ.DiffPrettyText(d.diffs)
	return fmt.Sprintf("%s:\n%s", d.path, diff)
}

type checkDiffs struct {
	differ *diffmatchpatch.DiffMatchPatch
	diffs  []diff
	mutex  sync.RWMutex
}

func diffChecker() *checkDiffs {
	differ := diffmatchpatch.New()
	return &checkDiffs{
		differ: differ,
	}
}

func (c *checkDiffs) diff(path string, newData []byte) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	if !bytes.Equal(data, newData) {
		diffs := c.differ.DiffMain(string(data), string(newData), false)
		c.mutex.Lock()
		c.diffs = append(c.diffs, diff{path, diffs})
		c.mutex.Unlock()
	}

	return nil
}

func (c *checkDiffs) error() error {
	c.mutex.RLock()

	if len(c.diffs) == 0 {
		c.mutex.RUnlock()
		return nil
	}

	diffs := make([]diff, len(c.diffs))
	copy(diffs, c.diffs)
	c.mutex.RUnlock()

	sort.SliceStable(diffs, func(i, j int) bool {
		a, b := diffs[i], diffs[j]
		return a.path < b.path
	})

	errs := []string{}
	for _, diff := range diffs {
		errs = append(errs, diff.string(c.differ))
	}

	return errors.New(strings.Join(errs, "\n"))
}
