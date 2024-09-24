// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha1

// These constants define valid event severity values.
const (
	// EventSeverityTrace represents a trace event, usually
	// informing about actions taken during reconciliation.
	EventSeverityTrace string = "trace"
	// EventSeverityInfo represents an informational event, usually
	// informing about changes.
	EventSeverityInfo string = "info"
	// EventSeverityError represent an error event, usually a warning
	// that something goes wrong.
	EventSeverityError string = "error"
)

// EventTypeTrace represents a trace event.
const EventTypeTrace string = "Trace"
