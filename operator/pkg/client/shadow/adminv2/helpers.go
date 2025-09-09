package adminv2

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/sethgrid/pester"
	"go.uber.org/zap"
)

// rng is a package-scoped, mutex guarded, seeded *rand.Rand.
var rng = func() func(int) int {
	var mu sync.Mutex
	rng := rand.New(rand.NewSource(time.Now().UnixNano())) //nolint:gosec // old rpk code.
	return func(n int) int {
		mu.Lock()
		defer mu.Unlock()
		return rng.Intn(n)
	}
}()

type DialContextFunc = func(ctx context.Context, network, addr string) (net.Conn, error)

type (
	clientOptFn func(*pester.Client)
	authFn      func(*http.Request)
)

// HTTPResponseError is the error response.
type HTTPResponseError struct {
	Method     string
	URL        string
	Response   *http.Response
	StatusCode int
	Body       []byte
}

// Error returns string representation of the error.
func (he HTTPResponseError) Error() string {
	return fmt.Sprintf("request %s %s failed: %s, body: %q\n",
		he.Method, he.URL, http.StatusText(he.Response.StatusCode), he.Body)
}

func newDefaultRetryClient() *pester.Client {
	// General purpose backoff, includes 503s and other errors
	const retryBackoffMs = 1500

	// In situations where a request can't be executed immediately (e.g. no
	// controller leader) the admin API does not block, it returns 503.
	// Use a retrying HTTP client to handle that gracefully.
	client := pester.New()

	// Backoff is the default redpanda raft election timeout: this enables us
	// to cleanly retry on 503s due to leadership changes in progress.
	client.Backoff = func(_ int) time.Duration {
		maxJitter := 100
		delayMs := retryBackoffMs + rng(maxJitter)
		return time.Duration(delayMs) * time.Millisecond
	}

	// This happens to be the same as the pester default, but make it explicit:
	// a raft election on a 3 node group might take 3x longer if it has
	// to repeat until the lowest-priority voter wins.
	client.MaxRetries = 3

	client.LogHook = func(e pester.ErrEntry) {
		// Only log from here when retrying: a final error propagates to caller
		if e.Err != nil && e.Retry <= client.MaxRetries {
			zap.L().Sugar().Debugf("Retrying %s for error: %s\n", e.Verb, e.Err.Error())
		}
	}

	client.Timeout = 10 * time.Second
	return client
}

func defaultTransport() *http.Transport {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}
