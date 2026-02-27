package gdrive

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"time"

	"google.golang.org/api/googleapi"
)

const (
	maxRetries     = 5
	baseBackoff    = 500 * time.Millisecond
	maxBackoff     = 30 * time.Second
	jitterFraction = 0.3
)

// isRateLimitError checks if the error is a rate limit (429) or quota (403) error.
func isRateLimitError(err error) bool {
	if apiErr, ok := err.(*googleapi.Error); ok {
		return apiErr.Code == 429 || apiErr.Code == 403
	}
	return false
}

// withRetry executes fn with exponential backoff on rate limit errors.
func withRetry(ctx context.Context, desc string, fn func() error) error {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		lastErr = fn()
		if lastErr == nil {
			return nil
		}
		if !isRateLimitError(lastErr) {
			return lastErr
		}
		if attempt == maxRetries {
			break
		}

		backoff := time.Duration(float64(baseBackoff) * math.Pow(2, float64(attempt)))
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
		// Add jitter
		jitter := time.Duration(float64(backoff) * jitterFraction * rand.Float64())
		wait := backoff + jitter

		slog.Warn("rate limited, retrying",
			"operation", desc,
			"attempt", attempt+1,
			"backoff_ms", wait.Milliseconds())

		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return fmt.Errorf("%s: exhausted %d retries: %w", desc, maxRetries, lastErr)
}
