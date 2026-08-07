// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package brokerset

import (
	"fmt"
	"time"
)

// RequeueDuration is the time after which blocked operations (migration
// preconditions, in-flight rolls) ask their reconciler to requeue.
const RequeueDuration = time.Second * 10

// RequeueAfterError error carrying the time after which to requeue.
//
// This is the canonical definition; operator/pkg/resources aliases it so V1
// call sites checking for *resources.RequeueAfterError match errors produced
// here.
type RequeueAfterError struct {
	RequeueAfter time.Duration
	Msg          string
}

func (e *RequeueAfterError) Error() string {
	return fmt.Sprintf("RequeueAfterError %s", e.Msg)
}

func (e *RequeueAfterError) Is(target error) bool {
	return e.Error() == target.Error()
}
