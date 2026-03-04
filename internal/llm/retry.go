// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package llm

import (
	"context"
	"errors"
	"net"
	"time"
)

// retryDelay is the wait time before a single retry attempt.
const retryDelay = 2 * time.Second

// retryProvider wraps a Provider with single-retry logic for transient failures.
type retryProvider struct {
	inner Provider
}

// WithRetry wraps a Provider to retry once on transient errors.
func WithRetry(p Provider) Provider {
	return &retryProvider{inner: p}
}

func (r *retryProvider) Name() string { return r.inner.Name() }

func (r *retryProvider) ChatCompletion(ctx context.Context, req Request) (*Response, error) {
	resp, err := r.inner.ChatCompletion(ctx, req)
	if err == nil {
		return resp, nil
	}

	if !isTransient(err) {
		return nil, err
	}

	// Wait before retry, respecting context cancellation.
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(retryDelay):
	}

	return r.inner.ChatCompletion(ctx, req)
}

// TransientError marks an error as retryable. Providers wrap HTTP 429/5xx
// and network errors with this type.
type TransientError struct {
	Err error
}

func (e *TransientError) Error() string { return e.Err.Error() }
func (e *TransientError) Unwrap() error { return e.Err }

func isTransient(err error) bool {
	var te *TransientError
	if errors.As(err, &te) {
		return true
	}
	// Also retry on network-level errors.
	var netErr net.Error
	return errors.As(err, &netErr)
}
