package external

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var (
	// ErrNoFetchedValue is returned when a user requests the last fetched
	// value for a request, but there is a watcher has not yet fetched a request
	// yet.
	ErrNoFetchedValue = errors.New("no value fetched for request")
	// ErrRequestNotWatched is returned when a user requests the last fetched
	// value for a request, but there is not a registered watcher for the
	// request.
	ErrRequestNotWatched = errors.New("request not watched")
)

// ResourceFetcher encapsulates fetching and comparison logic to see if
// an external resource has been updated.
type ResourceFetcher[T any] interface {
	Fetch(ctx context.Context) (T, error)
	Equal(a, b T) bool
}

type watcher[T any] struct {
	logger logr.Logger

	request reconcile.Request

	fetcher   ResourceFetcher[T]
	fetched   atomic.Bool
	lastFetch T
	mutex     sync.RWMutex

	timeout time.Duration

	requestCh  chan event.TypedGenericEvent[reconcile.Request]
	signalCh   chan struct{}
	shutdownCh chan struct{}
	stoppedCh  chan struct{}
}

func newWatcher[T any](logger logr.Logger, timeout time.Duration, request reconcile.Request, fetcher ResourceFetcher[T], requestCh chan event.TypedGenericEvent[reconcile.Request]) *watcher[T] {
	return &watcher[T]{
		logger:     logger,
		request:    request,
		fetcher:    fetcher,
		timeout:    timeout,
		requestCh:  requestCh,
		signalCh:   make(chan struct{}),
		shutdownCh: make(chan struct{}),
		stoppedCh:  make(chan struct{}),
	}
}

func (w *watcher[T]) doFetch(ctx context.Context) (T, bool, error) {
	var response T
	var err error

	response, err = w.fetcher.Fetch(ctx)
	if err != nil {
		return response, false, err
	}

	w.mutex.Lock()
	defer w.mutex.Unlock()

	updated := false
	if !w.fetcher.Equal(w.lastFetch, response) {
		w.lastFetch = response
		w.fetched.Store(true)
		updated = true
	}

	return w.lastFetch, updated, nil
}

func (w *watcher[T]) run() {
	defer close(w.stoppedCh)

	ctx, cancel := context.WithCancel(context.Background())

	fetch := func() {
		_, updated, err := w.doFetch(ctx)
		if err != nil {
			select {
			case <-w.shutdownCh:
				return
			default:
				w.logger.Error(err, "error fetching external resource", "request", w.request)
			}
		}

		if updated {
			select {
			case <-w.shutdownCh:
			case w.requestCh <- event.TypedGenericEvent[reconcile.Request]{
				Object: w.request,
			}:
			}
		}
	}

	// kick off an initial fetch
	fetch()

	timer := time.NewTimer(w.timeout)
	stopTimer := func() {
		if !timer.Stop() {
			<-timer.C
		}
	}
	defer stopTimer()

	for {
		select {
		case <-timer.C:
			fetch()
			timer.Reset(w.timeout)
		case <-w.signalCh:
			fetch()
			stopTimer()
			timer.Reset(w.timeout)
		case <-w.shutdownCh:
			cancel()
			return
		}
	}
}

func (w *watcher[T]) stop() {
	close(w.shutdownCh)
	<-w.stoppedCh
}

// ResourceWatchManager is a goroutine manager for poll-based watchers
// that fetch resources associated with a particular reconciler request and
// can be used to enqueue reconciliation based on some external resource changing.
type ResourceWatchManager[T any] struct {
	logger logr.Logger

	timeout   time.Duration
	requestCh chan event.TypedGenericEvent[reconcile.Request]
	watchers  map[reconcile.Request]*watcher[T]
	mutex     sync.RWMutex
}

// NewResourceWatchManager returns an ResourceWatchManager with the given logger and timeout.
func NewResourceWatchManager[T any](logger logr.Logger, timeout time.Duration) *ResourceWatchManager[T] {
	return &ResourceWatchManager[T]{
		logger:    logger,
		timeout:   timeout,
		requestCh: make(chan event.TypedGenericEvent[reconcile.Request]),
		watchers:  make(map[reconcile.Request]*watcher[T]),
	}
}

// Watch creates a new watcher with the given fetch behavior for the provided request.
func (m *ResourceWatchManager[T]) Watch(req reconcile.Request, fetcher ResourceFetcher[T]) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if _, ok := m.watchers[req]; ok {
		return
	}

	reqWatcher := newWatcher(m.logger, m.timeout, req, fetcher, m.requestCh)
	m.watchers[req] = reqWatcher
	go reqWatcher.run()
}

// Signal tells the watcher associated with the request to fetch.
func (m *ResourceWatchManager[T]) Signal(req reconcile.Request) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	m.signal(req)
}

// Broadcast tells all watchers to fetch.
func (m *ResourceWatchManager[T]) Broadcast() {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	for req := range m.watchers {
		m.signal(req)
	}
}

// Unwatch stops the underlying watcher associated with the reconcile request.
func (m *ResourceWatchManager[T]) Unwatch(req reconcile.Request) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.stop(req)
}

// Stop stops all underlying watchers.
func (m *ResourceWatchManager[T]) Stop() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	for req := range m.watchers {
		m.stop(req)
	}
}

// Value returns the latest fetched value associated with the given request. If
// there is no watcher associated with the given request, or if the watcher has
// not yet started, return an error.
func (m *ResourceWatchManager[T]) Value(req reconcile.Request) (T, error) {
	var t T

	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if w, ok := m.watchers[req]; ok && w.fetched.Load() {
		w.mutex.RLock()
		defer w.mutex.RUnlock()

		return w.lastFetch, nil
	}

	return t, ErrNoFetchedValue
}

// Fetch directly fetches the underlying resource, returning any errors encountered
// in the process.
func (m *ResourceWatchManager[T]) Fetch(ctx context.Context, req reconcile.Request) (T, error) {
	var t T
	if w, ok := m.watchers[req]; ok {
		t, _, err := w.doFetch(ctx)
		return t, err
	}
	return t, ErrRequestNotWatched
}

// Source provides a source for a controller's Watch setup.
func (m *ResourceWatchManager[T]) Source() source.Source {
	return source.Channel(m.requestCh, handler.TypedEnqueueRequestsFromMapFunc[reconcile.Request](func(_ context.Context, request reconcile.Request) []reconcile.Request {
		return []reconcile.Request{request}
	}))
}

func (m *ResourceWatchManager[T]) signal(req reconcile.Request) {
	if watcher, ok := m.watchers[req]; ok {
		watcher.signalCh <- struct{}{}
	}
}

func (m *ResourceWatchManager[T]) stop(req reconcile.Request) {
	if watcher, ok := m.watchers[req]; ok {
		watcher.stop()
		delete(m.watchers, req)
	}
}
