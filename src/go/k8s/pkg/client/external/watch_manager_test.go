package external

import (
	"context"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type testFetcher struct {
	value atomic.Int32
}

func testFetcherFor(i int32) *testFetcher {
	fetcher := &testFetcher{}
	fetcher.value.Store(i)
	return fetcher
}

func (t *testFetcher) Fetch(ctx context.Context) (int32, error) {
	return t.value.Load(), nil
}

func (t *testFetcher) Equal(a, b int32) bool {
	return a == b
}

func TestWatchManager(t *testing.T) {
	watchers := int32(10)

	ensureFire := func() {
		// make sure we all fire by waiting a bit
		time.Sleep(2 * time.Millisecond)
	}

	manager := NewResourceWatchManager[int32](logr.Discard(), 1*time.Millisecond)

	requestFor := func(i int32) reconcile.Request {
		return reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name: strconv.Itoa(int(i)),
			},
		}
	}

	triggered := map[reconcile.Request]int{}
	stop := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			select {
			case event := <-manager.requestCh:
				triggered[event.Object]++
			case <-stop:
				return
			}
		}
	}()

	// start at 1 indexing so we don't have odd issues
	// with the zero-value for the atomic.Int32 above
	for i := int32(1); i <= watchers; i++ {
		fetcher := testFetcherFor(i)
		manager.Watch(requestFor(i), fetcher)

		ensureFire()

		value, err := manager.Value(requestFor(i))
		if assert.NoError(t, err) {
			assert.Equal(t, i, value)
		}

		fetcher.value.Add(1)

		ensureFire()

		if i > watchers/2 {
			// kill half the watchers
			manager.Unwatch(requestFor(i))
		}

		fetcher.value.Add(1)

		ensureFire()
	}

	manager.Stop()
	close(stop)
	<-stopped

	for i := int32(1); i <= watchers; i++ {
		if i > watchers/2 {
			assert.Equal(t, 2, triggered[requestFor(i)])
		} else {
			assert.Equal(t, 3, triggered[requestFor(i)])
		}
	}
}
