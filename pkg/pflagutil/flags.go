// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// package pflagutil contains custom implementations of [pflag.Value] for
// commonly used CLI options.
package pflagutil

import (
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/labels"
)

// LabelSelectorValue is a [pflag.Value] implementation for Kubernetes label selectors.
// Usage:
//
//	var selector LabelSelectorValue
//	cmd.Flags().Var(&selector, ...)
type LabelSelectorValue struct {
	Selector labels.Selector
}

var _ pflag.Value = ((*LabelSelectorValue)(nil))

func (s *LabelSelectorValue) Set(value string) error {
	if value == "" {
		return nil
	}
	var err error
	s.Selector, err = labels.Parse(value)
	return err
}

func (s *LabelSelectorValue) String() string {
	if s.Selector == nil {
		return ""
	}
	return s.Selector.String()
}

func (s *LabelSelectorValue) Type() string {
	return "label-selector"
}
