package discord

import (
	"errors"
	"fmt"
	"net/http"
	"time"
)

// ArgError is an invalid tool argument caught before any request was sent.
type ArgError struct{ Msg string }

func (e *ArgError) Error() string { return e.Msg }

// APIError is a non-2xx answer from Discord.
type APIError struct {
	Status  int
	Code    int
	Message string
}

func (e *APIError) Error() string {
	if e.Code != 0 {
		return fmt.Sprintf("Discord %d: %s (%d)", e.Status, e.Message, e.Code)
	}
	return fmt.Sprintf("Discord %d: %s", e.Status, e.Message)
}

// RateLimitedError means Discord asked to wait longer than the call's timeout
// allows.
type RateLimitedError struct{ RetryAfter time.Duration }

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("Discord rate limit: retry after %s", e.RetryAfter.Round(10*time.Millisecond))
}

// UnavailableError is a transport failure or timeout. Cause never contains the
// request URL, which may embed a webhook token.
type UnavailableError struct{ Cause string }

func (e *UnavailableError) Error() string { return "Discord unavailable: " + e.Cause }

// IsUnauthorized reports whether Discord rejected the credential itself.
func IsUnauthorized(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Status == http.StatusUnauthorized
}
