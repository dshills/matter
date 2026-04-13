package llm

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/dshills/matter/internal/errtype"
)

const maxBackoffDelay = 30 * time.Second

// RetryClient wraps an LLM Client with exponential backoff retry logic.
type RetryClient struct {
	inner      Client
	maxRetries int
	baseDelay  time.Duration
}

// NewRetryClient creates a retry-aware client wrapper.
// maxRetries is the maximum number of retry attempts (0 means no retries).
func NewRetryClient(inner Client, maxRetries int) *RetryClient {
	if maxRetries < 0 {
		maxRetries = 0
	}
	return &RetryClient{
		inner:      inner,
		maxRetries: maxRetries,
		baseDelay:  500 * time.Millisecond,
	}
}

// Complete calls the inner client, retrying on retriable errors with
// exponential backoff and jitter. Terminal errors are returned immediately.
func (r *RetryClient) Complete(ctx context.Context, req Request) (Response, error) {
	var lastErr error

	for attempt := range r.maxRetries + 1 {
		resp, err := r.inner.Complete(ctx, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err

		if !isRetriable(err) {
			return Response{}, err
		}

		// Don't sleep after the last attempt.
		if attempt < r.maxRetries {
			delay := r.backoff(attempt)
			select {
			case <-ctx.Done():
				return Response{}, ctx.Err()
			case <-time.After(delay):
			}
		}
	}

	return Response{}, lastErr
}

// backoff returns the delay for a given attempt using exponential backoff
// with jitter, capped at maxBackoffDelay.
func (r *RetryClient) backoff(attempt int) time.Duration {
	delay := time.Duration(1<<attempt) * r.baseDelay
	if delay > maxBackoffDelay {
		delay = maxBackoffDelay
	}
	// Add jitter: ±25% of the delay.
	if half := int64(delay / 2); half > 0 {
		jitter := time.Duration(rand.Int64N(half)) - delay/4
		delay += jitter
	}
	return delay
}

// isRetriable determines whether an error should be retried.
// Uses the agent error classification system when available.
func isRetriable(err error) bool {
	if agentErr, ok := errors.AsType[*errtype.AgentError](err); ok {
		return agentErr.Classification == errtype.ClassRetriable
	}
	// Context errors are not retriable (the caller cancelled).
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	// Unknown errors default to retriable (conservative — let retry logic handle).
	return true
}
