package users

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPasswordGenerator(t *testing.T) {
	generator := newPasswordGenerator()
	password, err := generator.Generate()
	require.NoError(t, err)

	require.Len(t, password, 32)
}
