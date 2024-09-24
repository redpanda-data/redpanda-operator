// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package collections

import "sync"

// Set is a generic interface for set implementations.
type Set[T comparable] interface {
	// Add adds the given values to a set.
	Add(values ...T)
	// HasAll checks if all of the given values exist in the set.
	HasAll(values ...T) bool
	// HasAny checks if any of the given values exist in the set.
	HasAny(values ...T) bool
	// Delete removes all of the given values from the set.
	Delete(values ...T)
	// Clear removes all values from the set.
	Clear()
	// Size returns the cardinality of the set.
	Size() int
	// Values returns a list of values found in the given set. The
	// order of the returned values is not guaranteed.
	Values() []T
	// Merge merges all of the values from a given set into this set.
	Merge(other Set[T])
	// Clone makes a copy of the current set. All set implementations should
	// clone to the same set implementation as the current set.
	Clone() Set[T]
	// RightDisjoint returns a new set of the same type as the current set with
	// items only found in the given set, any overlapping elements, or elements
	// only found in the current set are removed.
	RightDisjoint(other Set[T]) Set[T]
	// LeftDisjoint returns a new set of the same type as the current set with
	// items only found in the current set, any overlapping elements, or elements
	// only found in the given set are removed.
	LeftDisjoint(other Set[T]) Set[T]
	// Intersection returns a new set of the same type as the current set with
	// items only found in both the current and the given set, any non-overlapping
	// elements are removed.
	Intersection(other Set[T]) Set[T]
	// Union returns a new set of the same type as the current set with all
	// items found in either the current and the given set.
	Union(other Set[T]) Set[T]
}

type singleThreadedSet[T comparable] struct {
	data map[T]struct{}
}

var _ Set[string] = (*singleThreadedSet[string])(nil)

func NewSet[T comparable]() Set[T] {
	return &singleThreadedSet[T]{
		data: make(map[T]struct{}),
	}
}

func (s *singleThreadedSet[T]) Add(values ...T) {
	for _, value := range values {
		s.data[value] = struct{}{}
	}
}

func (s *singleThreadedSet[T]) HasAll(values ...T) bool {
	for _, value := range values {
		_, ok := s.data[value]
		if !ok {
			return false
		}
	}
	return true
}

func (s *singleThreadedSet[T]) HasAny(values ...T) bool {
	for _, value := range values {
		_, ok := s.data[value]
		if ok {
			return true
		}
	}
	return false
}

func (s *singleThreadedSet[T]) Delete(values ...T) {
	for _, value := range values {
		delete(s.data, value)
	}
}

func (s *singleThreadedSet[T]) Clear() {
	s.data = make(map[T]struct{})
}

func (s *singleThreadedSet[T]) Size() int {
	return len(s.data)
}

func (s *singleThreadedSet[T]) Values() []T {
	var values []T
	for value := range s.data {
		values = append(values, value)
	}
	return values
}

func (s *singleThreadedSet[T]) Merge(other Set[T]) {
	for _, value := range other.Values() {
		s.Add(value)
	}
}

func (s *singleThreadedSet[T]) Clone() Set[T] {
	set := &singleThreadedSet[T]{
		data: make(map[T]struct{}),
	}
	set.Add(s.Values()...)
	return set
}

func (s *singleThreadedSet[T]) RightDisjoint(other Set[T]) Set[T] {
	set := &singleThreadedSet[T]{
		data: make(map[T]struct{}),
	}
	set.Add(other.Values()...)
	set.Delete(s.Values()...)
	return set
}

func (s *singleThreadedSet[T]) LeftDisjoint(other Set[T]) Set[T] {
	set := s.Clone()
	set.Delete(other.Values()...)
	return set
}

func (s *singleThreadedSet[T]) Intersection(other Set[T]) Set[T] {
	set := &singleThreadedSet[T]{
		data: make(map[T]struct{}),
	}
	for _, value := range other.Values() {
		if _, ok := s.data[value]; ok {
			set.Add(value)
		}
	}
	return set
}

func (s *singleThreadedSet[T]) Union(other Set[T]) Set[T] {
	set := s.Clone()
	set.Add(other.Values()...)
	return set
}

type concurrentSet[T comparable] struct {
	set   *singleThreadedSet[T]
	mutex sync.RWMutex
}

var _ Set[string] = (*concurrentSet[string])(nil)

func NewConcurrentSet[T comparable]() Set[T] {
	return &concurrentSet[T]{
		set: &singleThreadedSet[T]{
			data: make(map[T]struct{}),
		},
	}
}

func (s *concurrentSet[T]) Add(values ...T) {
	s.mutex.Lock()
	s.set.Add(values...)
	s.mutex.Unlock()
}

func (s *concurrentSet[T]) HasAll(values ...T) bool {
	s.mutex.RLock()
	hasAll := s.set.HasAll(values...)
	s.mutex.RUnlock()
	return hasAll
}

func (s *concurrentSet[T]) HasAny(values ...T) bool {
	s.mutex.RLock()
	hasAny := s.set.HasAny(values...)
	s.mutex.RUnlock()
	return hasAny
}

func (s *concurrentSet[T]) Delete(values ...T) {
	s.mutex.Lock()
	s.set.Delete(values...)
	s.mutex.Unlock()
}

func (s *concurrentSet[T]) Clear() {
	s.mutex.Lock()
	s.set.Clear()
	s.mutex.Unlock()
}

func (s *concurrentSet[T]) Size() int {
	s.mutex.RLock()
	size := s.set.Size()
	s.mutex.RUnlock()
	return size
}

func (s *concurrentSet[T]) Values() []T {
	s.mutex.RLock()
	values := s.set.Values()
	s.mutex.RUnlock()
	return values
}

func (s *concurrentSet[T]) Merge(other Set[T]) {
	s.mutex.Lock()
	s.set.Merge(other)
	s.mutex.Unlock()
}

func (s *concurrentSet[T]) Clone() Set[T] {
	s.mutex.RLock()
	set := &concurrentSet[T]{
		set: s.set.Clone().(*singleThreadedSet[T]),
	}
	s.mutex.RUnlock()
	return set
}

func (s *concurrentSet[T]) RightDisjoint(other Set[T]) Set[T] {
	s.mutex.RLock()
	set := &concurrentSet[T]{
		set: s.set.RightDisjoint(other).(*singleThreadedSet[T]),
	}
	s.mutex.RUnlock()
	return set
}

func (s *concurrentSet[T]) LeftDisjoint(other Set[T]) Set[T] {
	s.mutex.RLock()
	set := &concurrentSet[T]{
		set: s.set.LeftDisjoint(other).(*singleThreadedSet[T]),
	}
	s.mutex.RUnlock()
	return set
}

func (s *concurrentSet[T]) Intersection(other Set[T]) Set[T] {
	s.mutex.RLock()
	set := &concurrentSet[T]{
		set: s.set.Intersection(other).(*singleThreadedSet[T]),
	}
	s.mutex.RUnlock()
	return set
}

func (s *concurrentSet[T]) Union(other Set[T]) Set[T] {
	s.mutex.RLock()
	set := &concurrentSet[T]{
		set: s.set.Union(other).(*singleThreadedSet[T]),
	}
	s.mutex.RUnlock()
	return set
}
