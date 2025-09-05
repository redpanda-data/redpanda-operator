package adminv2

import (
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/sethgrid/pester"
)

type wrappedClient struct {
	authFns   []authFn
	urls      []*url.URL
	client    *pester.Client
	oneshot   *http.Client
	transport *http.Transport
	mutex     sync.RWMutex
}

func (c *wrappedClient) updateURLs(urls []string) error {
	if len(urls) == 0 {
		return errors.New("must specify at least one url")
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	parsedURLs := make([]*url.URL, len(urls))
	for i, u := range urls {
		parsed, err := url.Parse(u)
		if err != nil {
			return err
		}
		parsedURLs[i] = parsed
	}

	c.urls = parsedURLs
	return nil
}

func (c *wrappedClient) Do(req *http.Request) (*http.Response, error) {
	rng := rand.New(rand.NewSource(time.Now().UnixNano())) //nolint:gosec // old rpk code.

	shuffled := make([]*url.URL, len(c.urls))
	copy(shuffled, c.urls)
	rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })

	for _, authFn := range c.authFns {
		authFn(req)
	}

	// After a 503 or 504, wait a little for an election
	const unavailableBackoff = 1500 * time.Millisecond

	var err error
	for i := range shuffled {
		req.URL = shuffled[i].JoinPath(req.URL.Path)

		// If err is set, we are retrying after a failure on the previous node
		if err != nil {
			var httpErr *HTTPResponseError
			if errors.As(err, &httpErr) {
				status := httpErr.Response.StatusCode

				// The node was up but told us the cluster
				// wasn't ready: wait before retry.
				if status == 503 || status == 504 {
					time.Sleep(unavailableBackoff)
				}
			}
		}

		// Where there are multiple nodes, disable the HTTP request retry in favour of our
		// own retry across the available nodes
		retryable := len(shuffled) == 1

		var res *http.Response
		res, err = c.sendAndReceive(req, retryable)
		if err == nil {
			return res, err
		}
	}

	return nil, err
}

func (c *wrappedClient) sendAndReceive(req *http.Request, retryable bool) (*http.Response, error) {
	var res *http.Response
	var err error
	if retryable {
		res, err = c.client.Do(req)
	} else {
		res, err = c.oneshot.Do(req)
	}

	if err != nil {
		// When the server expects a TLS connection, but the TLS config isn't
		// set/ passed, The client returns an error like
		// Get "http://localhost:9644/v1/security/users": EOF
		// which doesn't make it obvious to the user what's going on.
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%s to server %s expected a tls connection: %w", req.Method, req.URL.String(), err)
		}
		return nil, err
	}

	if res.StatusCode/100 != 2 {
		// we read the body just below, so this response is now
		// junked and we need to close it.
		defer res.Body.Close()

		resBody, err := io.ReadAll(res.Body)
		status := http.StatusText(res.StatusCode)
		if err != nil {
			return nil, fmt.Errorf("request %s %s failed: %s, unable to read body: %w", req.Method, req.URL.String(), status, err)
		}
		return nil, &HTTPResponseError{Response: res, Body: resBody, Method: req.Method, URL: req.URL.String()}
	}

	return res, nil
}

func (c *wrappedClient) close() {
	c.transport.CloseIdleConnections()
}
