package speech

// OpenRouter, and the providers behind it, fail now and then: a connection
// drops, an overloaded upstream answers 502, a rate limit kicks in. Losing a
// four-minute recording to one of those is the worst thing the voice path can
// do, so every call gets two more tries before its failure reaches the user.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

const (
	// attempts is the first try plus two retries.
	attempts = 3
	// retryDelay is the wait before the first retry; each later one waits longer.
	retryDelay = time.Second
)

// ErrUnavailable marks a call that kept failing, for reasons another try
// might have fixed, until every attempt was spent: the trouble was reaching
// OpenRouter, not the request itself.
var ErrUnavailable = errors.New("speech: openrouter is unavailable")

// StatusError is a failure OpenRouter reported, either as the HTTP status of
// its reply or inside a reply that arrived as 200.
type StatusError struct {
	Code    int
	Message string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("openrouter %d: %s", e.Code, e.Message)
}

// retry runs call until it succeeds, fails in a way another try cannot fix,
// or has used every attempt, waiting a little longer before each retry. model
// names the call in the log.
func (c *Client) retry(ctx context.Context, model string, call func() error) error {
	for attempt := 1; ; attempt++ {
		err := call()
		if err == nil || !transient(ctx, err) {
			return err
		}

		if attempt == attempts {
			return fmt.Errorf("%w (tried %d times): %w", ErrUnavailable, attempts, err)
		}

		c.logger.WarnContext(ctx, "openrouter call failed, trying again", "model", model, "attempt", attempt, "error", err)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt) * c.retryDelay):
		}
	}
}

// transient reports whether another attempt might succeed where err failed.
func transient(ctx context.Context, err error) bool {
	if ctx.Err() != nil || errors.Is(err, ErrDisabled) {
		return false // the caller is gone, or there is nothing to call
	}

	var status *StatusError
	if errors.As(err, &status) {
		return status.Code == http.StatusRequestTimeout ||
			status.Code == http.StatusTooManyRequests ||
			status.Code >= http.StatusInternalServerError
	}

	return true // the connection failed, or the reply broke off or came back empty
}
