// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package pipelineapi

import (
	"context"
	"crypto/sha256"
	"slices"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/cockroachdb/errors"
	authenticationv1 "k8s.io/api/authentication/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// PrincipalHeader carries the Console user a call is made for. It's only
	// read from callers that authenticated as an allowed principal.
	PrincipalHeader = "X-Redpanda-Principal"

	// DefaultAudience is the audience Console's projected ServiceAccount
	// token must be issued for, so a token minted for another service can't
	// be replayed here.
	DefaultAudience = "redpanda-pipeline-api"

	reviewCacheTTL  = 30 * time.Second
	reviewCacheSize = 128
)

type (
	principalKey struct{}
	callerKey    struct{}
)

// PrincipalFromContext returns the Console user the call was made for.
func PrincipalFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(principalKey{}).(string)
	return v, ok && v != ""
}

// CallerFromContext returns the authenticated Kubernetes username of the
// caller, normally Console's ServiceAccount.
func CallerFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(callerKey{}).(string)
	return v, ok && v != ""
}

// TokenReviewer authenticates a bearer token and returns its username.
type TokenReviewer interface {
	Review(ctx context.Context, token string) (username string, err error)
}

// errUnauthenticated is returned by a TokenReviewer for a token the API
// server rejected, as opposed to a failure to ask.
var errUnauthenticated = errors.New("token is not valid for the pipeline API")

// KubernetesTokenReviewer authenticates tokens with the TokenReview API. It
// needs create on tokenreviews, for example through system:auth-delegator.
type KubernetesTokenReviewer struct {
	Client    client.Client
	Audiences []string
}

// Review implements TokenReviewer.
func (r *KubernetesTokenReviewer) Review(ctx context.Context, token string) (string, error) {
	tr := &authenticationv1.TokenReview{Spec: authenticationv1.TokenReviewSpec{Token: token, Audiences: r.Audiences}}
	if err := r.Client.Create(ctx, tr); err != nil {
		return "", err
	}
	if !tr.Status.Authenticated {
		return "", errUnauthenticated
	}
	// When audiences are requested, the API server returns the ones the
	// token is valid for; require at least one of ours.
	if len(r.Audiences) > 0 && !slices.ContainsFunc(tr.Status.Audiences, func(a string) bool { return slices.Contains(r.Audiences, a) }) {
		return "", errUnauthenticated
	}
	return tr.Status.User.Username, nil
}

// AuthOptions configures NewAuthInterceptor.
type AuthOptions struct {
	Reviewer TokenReviewer
	// AllowedCallers are the Kubernetes usernames allowed to call the API,
	// such as system:serviceaccount:<namespace>:<console-service-account>.
	AllowedCallers []string
	// AllowUnauthenticated skips authentication. For local development only:
	// anything that can reach the API can then manage pipelines.
	AllowUnauthenticated bool
}

// NewAuthInterceptor authenticates every call with its bearer token and
// rejects callers outside AllowedCallers. Only then does it trust
// PrincipalHeader.
func NewAuthInterceptor(opts AuthOptions) (connect.Interceptor, error) {
	if !opts.AllowUnauthenticated && (opts.Reviewer == nil || len(opts.AllowedCallers) == 0) {
		return nil, errors.New("authentication needs a token reviewer and at least one allowed caller")
	}
	a := &authenticator{opts: opts, cache: map[[sha256.Size]byte]cachedReview{}}
	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			ctx, err := a.authenticate(ctx, req)
			if err != nil {
				return nil, err
			}
			return next(ctx, req)
		}
	}), nil
}

type cachedReview struct {
	username string
	expires  time.Time
}

type authenticator struct {
	opts  AuthOptions
	mu    sync.Mutex
	cache map[[sha256.Size]byte]cachedReview
}

func (a *authenticator) authenticate(ctx context.Context, req connect.AnyRequest) (context.Context, error) {
	if a.opts.AllowUnauthenticated {
		return context.WithValue(ctx, principalKey{}, req.Header().Get(PrincipalHeader)), nil
	}

	token, ok := strings.CutPrefix(req.Header().Get("Authorization"), "Bearer ")
	if !ok || token == "" {
		// Consoles without cloud.redpandaConnect.tokenFile forward the
		// user's request headers instead of sending Console's token.
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("a bearer token is required: Console must send its ServiceAccount token with cloud.redpandaConnect.tokenFile"))
	}
	username, err := a.review(ctx, token)
	if errors.Is(err, errUnauthenticated) {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("failed to authenticate the caller"))
	}
	if !slices.Contains(a.opts.AllowedCallers, username) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("caller is not allowed to use the pipeline API"))
	}

	ctx = context.WithValue(ctx, callerKey{}, username)
	return context.WithValue(ctx, principalKey{}, req.Header().Get(PrincipalHeader)), nil
}

// review caches successful reviews briefly, so a Console polling the
// pipeline list doesn't cost a TokenReview per request.
func (a *authenticator) review(ctx context.Context, token string) (string, error) {
	key := sha256.Sum256([]byte(token))
	now := time.Now()

	a.mu.Lock()
	if c, ok := a.cache[key]; ok && now.Before(c.expires) {
		a.mu.Unlock()
		return c.username, nil
	}
	a.mu.Unlock()

	username, err := a.opts.Reviewer.Review(ctx, token)
	if err != nil {
		return "", err
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.cache) >= reviewCacheSize {
		for k, c := range a.cache {
			if !now.Before(c.expires) {
				delete(a.cache, k)
			}
		}
		if len(a.cache) >= reviewCacheSize {
			clear(a.cache)
		}
	}
	a.cache[key] = cachedReview{username: username, expires: now.Add(reviewCacheTTL)}
	return username, nil
}
