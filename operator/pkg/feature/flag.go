// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package feature

import (
	"context"

	"github.com/cockroachdb/errors"

	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/log"
)

type FlagBundle int

const (
	_ FlagBundle = iota
	V1Flags
	V2Flags

	maxBundles
)

var bundles = make([][]*AnnotationFeatureFlag[any], maxBundles-1)

// Register adds the given flag to the provided [FlagBundle] and returns a handle it it.
// Usage:
//
//	var MyFlag = Register(MyBundle, AnnotationFeatureFlag{/* ... */})
func Register[T any](bundle FlagBundle, flag AnnotationFeatureFlag[T]) *AnnotationFeatureFlag[T] {
	// NB: FlagBundle are base 1 to prevent silently successes of zero values.
	// The downside is having to -1 everywhere.
	bundles[bundle-1] = append(bundles[bundle-1], flag.AsAny())
	return &flag
}

type AnnotationGetSetter interface {
	AnnotationGetter
	SetAnnotations(map[string]string)
}

// SetDefaults updates the annotations of obj with the default values all flags,
// if the flag is not provided, in the given [FlagBundle] and returns a bool
// indicating whether or not any changes where made.
func SetDefaults(ctx context.Context, bundle FlagBundle, obj AnnotationGetSetter) bool {
	flags := bundles[bundle-1]

	annos := obj.GetAnnotations()
	if annos == nil {
		annos = make(map[string]string, len(flags))
	}

	changed := false
	for _, flag := range flags {
		if _, ok := annos[flag.Key]; ok {
			continue
		}

		annos[flag.Key] = flag.Default
		changed = true
	}

	if !changed {
		return false
	}

	obj.SetAnnotations(annos)
	return true
}

type AnnotationFeatureFlag[T any] struct {
	Key     string
	Default string
	Parse   func(string) (T, error)
}

func (f *AnnotationFeatureFlag[T]) AsAny() *AnnotationFeatureFlag[any] {
	return &AnnotationFeatureFlag[any]{
		Key:     f.Key,
		Default: f.Default,
		Parse: func(s string) (any, error) {
			out, err := f.Parse(s)
			if err != nil {
				return nil, err
			}
			return any(out), nil
		},
	}
}

type AnnotationGetter interface {
	GetAnnotations() map[string]string
}

// Get extracts and parsed the value of this flag from obj. If any errors are
// encountered, an error is logged and the default is returned.
func (f *AnnotationFeatureFlag[T]) Get(ctx context.Context, obj AnnotationGetter) T {
	annotations := obj.GetAnnotations()
	if annotations != nil {
		if value, ok := annotations[f.Key]; ok {
			parsed, err := f.Parse(value)
			if err == nil {
				return parsed
			}
			log.Error(ctx, err, "failed to parsed annotation; interpreting as default", "default", f.Default, "value", value)
		}
	}

	parsed, err := f.Parse(f.Default)
	if err != nil {
		panic(errors.Wrapf(err, "failed to parsed default value %q of %q", f.Default, f.Key))
	}

	return parsed
}
