// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package utils

import (
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// DeletePredicate implements a predicate function that watches only delete events
type DeletePredicate struct {
	predicate.Funcs
}

func (DeletePredicate) Delete(e event.DeleteEvent) bool {
	return true
}
