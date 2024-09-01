package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/sergi/go-diff/diffmatchpatch"
)

type diff struct {
	path  string
	diffs []diffmatchpatch.Diff
}

func (d *diff) string(differ *diffmatchpatch.DiffMatchPatch) string {
	var diffString strings.Builder
	lineNumber := 0
	for _, diff := range differ.DiffCleanupSemantic(d.diffs) {
		lines := strings.Split(diff.Text, "\n")
		if lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}

		var prefix string
		switch diff.Type {
		case diffmatchpatch.DiffDelete:
			prefix = "-"
		case diffmatchpatch.DiffInsert:
			prefix = "+"
		default:
			lineNumber++
			diffString.WriteString(strconv.Itoa(lineNumber) + "\t" + lines[0] + "\n")
			lineNumber += (len(lines) - 1)
			continue
		}
		for _, line := range lines {
			lineNumber++
			diffString.WriteString(strconv.Itoa(lineNumber) + "\t" + prefix + line + "\n")
		}
	}
	return fmt.Sprintf("%s:\n%s", d.path, diffString.String())
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
