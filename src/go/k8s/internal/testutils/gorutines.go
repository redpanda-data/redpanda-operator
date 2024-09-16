package testutils

import (
	"context"
	"github.com/stretchr/testify/assert"
	"testing"
)

// TGo is a helper for ensuring that goroutines spawned in test cases are
// appropriately shutdown.
func TGo(t *testing.T, ctx context.Context, fn func(context.Context) error) {
	doneCh := make(chan struct{})
	ctx, cancel := context.WithCancel(ctx)

	t.Cleanup(func() {
		cancel()
		<-doneCh
	})

	go func() {
		assert.NoError(t, fn(ctx))
		close(doneCh)
	}()
}
