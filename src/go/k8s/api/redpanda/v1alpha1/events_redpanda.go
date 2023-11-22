package v1alpha1

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
