package v1alpha1

// These constants define valid values for event severity levels.
const (
	// A trace event that usually
	// informs you about actions taken during reconciliation.
	EventSeverityTrace string = "trace"
	// An informational event that usually
	// informs you about changes.
	EventSeverityInfo string = "info"
	// An error event that usually warns you
	// that something went wrong.
	EventSeverityError string = "error"
)

// EventTypeTrace represents a trace event.
const EventTypeTrace string = "Trace"
