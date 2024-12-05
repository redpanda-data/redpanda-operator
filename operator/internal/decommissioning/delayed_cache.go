// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package decommissioning

import (
	"iter"
	"slices"
	"sync"
	"time"
)

type cacheInfo struct {
	lastMarked time.Time
	count      int
}

// CategorizedDelayedCache keeps track of any item that meets some threshold
// cound over a period of time and then processes an item when it meets that
// threshold.
type CategorizedDelayedCache[Category, Entry comparable] struct {
	data          map[Category]map[Entry]*cacheInfo
	mutex         sync.RWMutex
	maxCount      int
	countInterval time.Duration
}

// NewCategorizedDelayedCache creates a delayed cache with an additional level of organization by category.
func NewCategorizedDelayedCache[Category, Entry comparable](maxCount int, interval time.Duration) *CategorizedDelayedCache[Category, Entry] {
	return &CategorizedDelayedCache[Category, Entry]{
		data:          make(map[Category]map[Entry]*cacheInfo),
		maxCount:      maxCount,
		countInterval: interval,
	}
}

// Clean removes all of the entries for the given category.
func (d *CategorizedDelayedCache[Category, Entry]) Clean(category Category) {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	delete(d.data, category)
}

// Size returns the number of entries in the cache for the given category.
func (d *CategorizedDelayedCache[Category, Entry]) Size(category Category) int {
	d.mutex.RLock()
	defer d.mutex.RUnlock()

	if entries, ok := d.data[category]; ok {
		return len(entries)
	}

	return 0
}

// Entries returns an iterator over the entries of a category.
func (d *CategorizedDelayedCache[Category, Entry]) Entries(category Category) iter.Seq[Entry] {
	return func(yield func(Entry) bool) {
		d.mutex.RLock()
		defer d.mutex.RUnlock()

		if entries, ok := d.data[category]; ok {
			for entry := range entries {
				if !yield(entry) {
					return
				}
			}
		}
	}
}

// Filter keeps only the entries in the cache which are equal to the given entries.
func (d *CategorizedDelayedCache[Category, Entry]) Filter(category Category, entries ...Entry) {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	if stored, ok := d.data[category]; ok {
		for entry := range stored {
			if !slices.Contains(entries, entry) {
				delete(stored, entry)
			}
		}
	}
}

// Prune deletes any entries in the cache matching the given entries, it is the opposite of Filter.
func (d *CategorizedDelayedCache[Category, Entry]) Prune(category Category, entries ...Entry) {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	if stored, ok := d.data[category]; ok {
		for _, entry := range entries {
			delete(stored, entry)
		}
	}
}

// Mark marks the entry in the cache, incrementing its count if it hasn't been marked in
// over the configurable interval.
func (d *CategorizedDelayedCache[Category, Entry]) Mark(category Category, entry Entry) {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	if _, ok := d.data[category]; !ok {
		d.data[category] = make(map[Entry]*cacheInfo)
	}
	entries := d.data[category]

	if info, ok := entries[entry]; ok {
		if time.Since(info.lastMarked) > d.countInterval {
			info.count++
			info.lastMarked = time.Now()
		}
		return
	}

	entries[entry] = &cacheInfo{
		lastMarked: time.Now(),
		count:      1,
	}

	return
}

// Process processes and removes a cache item, calling the callback if the minimum threshold count is met.
func (d *CategorizedDelayedCache[Category, Entry]) Process(category Category, entry Entry, onThresholdMet func() error) (bool, error) {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	processed := false
	if entries, ok := d.data[category]; ok {
		if info, ok := entries[entry]; ok {
			if d.maxCount <= info.count {
				processed = true
				if err := onThresholdMet(); err != nil {
					return processed, err
				}
				delete(entries, entry)
			}
		}
	}

	return processed, nil
}
