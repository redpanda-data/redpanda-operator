package log

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShouldLogOTEL(t *testing.T) {
	for name, tt := range map[string]struct {
		shouldLog   []string
		shouldntLog []string
	}{
		"debug": {
			shouldLog: []string{
				"info",
				"debug",
			},
			shouldntLog: []string{
				"verbose",
				"timing",
				"trace",
			},
		},
		"verbose": {
			shouldLog: []string{
				"info",
				"debug",
				"verbose",
				"timing",
				"trace",
			},
		},
		"timing": {
			shouldLog: []string{
				"info",
				"debug",
				"timing",
				"trace",
			},
			shouldntLog: []string{
				"verbose",
			},
		},
		"trace": {
			shouldLog: []string{
				"info",
				"debug",
				"trace",
			},
			shouldntLog: []string{
				"verbose",
				"timing",
			},
		},
		"info": {
			shouldLog: []string{
				"info",
			},
			shouldntLog: []string{
				"debug",
				"verbose",
				"timing",
				"trace",
			},
		},
	} {
		tt := tt
		name := name
		t.Run(name, func(t *testing.T) {
			severity := LevelFromString(name).OTELLevel
			for _, should := range tt.shouldLog {
				require.True(t, shouldLogOTEL(severity, LevelFromString(should).OTELLevel), "should log %q, but doesn't", should)
			}
			for _, shouldnt := range tt.shouldntLog {
				require.False(t, shouldLogOTEL(severity, LevelFromString(shouldnt).OTELLevel), "shouldn't log %q, but does", shouldnt)
			}
		})
	}
}
