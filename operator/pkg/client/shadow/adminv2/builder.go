// this package should really belong in common-go, but it's here for now since that's going to require some work

package adminv2

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	"github.com/sethgrid/pester"
)

type Builder struct {
	tlsConfig    *tls.Config
	urls         []string
	authFns      []authFn
	dialer       DialContextFunc
	clientOptFns []clientOptFn
}

func NewClientBuilder(urls ...string) *Builder {
	return &Builder{
		urls: urls,
	}
}

func (b *Builder) WithBasicAuth(username, password string) *Builder {
	b.authFns = append(b.authFns, func(r *http.Request) {
		r.SetBasicAuth(username, password)
	})
	return b
}

func (b *Builder) WithBearerToken(token string) *Builder {
	b.authFns = append(b.authFns, func(r *http.Request) {
		r.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	})
	return b
}

func (b *Builder) WithDialer(dialer DialContextFunc) *Builder {
	b.dialer = dialer
	return b
}

func (b *Builder) WithTLS(tlsConfig *tls.Config) *Builder {
	b.tlsConfig = tlsConfig
	return b
}

func (b *Builder) WithClientTimeout(t time.Duration) *Builder {
	b.clientOptFns = append(b.clientOptFns, func(c *pester.Client) {
		c.Timeout = t
	})
	return b
}

func (b *Builder) WithMaxRetries(r int) *Builder {
	b.clientOptFns = append(b.clientOptFns, func(c *pester.Client) {
		c.MaxRetries = r
	})
	return b
}

func (b *Builder) Build() (*Client, error) {
	wrappedClient, err := b.buildWrappedClient()
	if err != nil {
		return nil, err
	}

	return &Client{client: wrappedClient}, nil
}

func (b *Builder) buildWrappedClient() (*wrappedClient, error) {
	client := newDefaultRetryClient()
	for _, opt := range b.clientOptFns {
		opt(client)
	}
	transport := defaultTransport()

	oneshot := &http.Client{Timeout: client.Timeout}
	if b.tlsConfig != nil {
		transport.TLSClientConfig = b.tlsConfig
	}
	if b.dialer != nil {
		transport.DialContext = b.dialer
	}

	client.Transport = transport
	oneshot.Transport = transport

	wrapped := &wrappedClient{authFns: b.authFns, client: client, oneshot: oneshot, transport: transport}
	if err := wrapped.updateURLs(b.urls); err != nil {
		return nil, err
	}
	return wrapped, nil
}
