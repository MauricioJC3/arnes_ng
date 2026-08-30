package provider

import (
	"context"
	"errors"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

// Transient reports whether err looks like a temporary provider failure worth
// retrying: an HTTP 429 or 5xx, or a stream that dropped or stalled before it
// finished. A cancelled or timed-out caller context is never transient (the
// user asked to stop), and a 4xx other than 429 (bad request, auth) is a real
// error that a retry will not fix.
func Transient(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) && apiErr.StatusCode != 0 {
		return apiErr.StatusCode == 429 || apiErr.StatusCode >= 500
	}

	msg := err.Error()
	for _, sub := range transientMarkers {
		if strings.Contains(msg, sub) {
			return true
		}
	}
	return false
}

// transientMarkers are substrings the provider adapters put in an error when the
// failure is a rate limit, a 5xx, or a broken/stalled stream.
var transientMarkers = []string{
	"HTTP 429", "HTTP 500", "HTTP 502", "HTTP 503", "HTTP 504",
	"sin respuesta tras", // openai_compat: retries exhausted
	"cortó el stream",    // openai_compat: mid-stream drop
	"stream se cortó",    // openai_compat: stalled stream
	"no respondió en",    // openai_compat: no time-to-first-byte
	"connection reset",
	"broken pipe",
	"unexpected EOF",
	"i/o timeout",
	"TLS handshake timeout",
	"429 Too Many Requests",
	"overloaded_error", // anthropic 529
}
