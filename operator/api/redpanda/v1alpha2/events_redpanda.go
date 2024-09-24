// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha2

// These constants define valid values for event severity levels.
const (
	// EventSeverityTrace is a trace event that usually
	// informs you about actions taken during reconciliation.
	EventSeverityTrace string = "trace"
	// EventSeverityInfo is an informational event that usually
	// informs you about changes.
	EventSeverityInfo string = "info"
	// EventSeverityError is an error event that usually warns you
	// that something went wrong.
	EventSeverityError string = "error"
)

// EventTypeTrace represents a trace event.
const EventTypeTrace string = "Trace"
