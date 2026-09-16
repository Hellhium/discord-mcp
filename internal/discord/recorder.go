package discord

import (
	"context"
	"sync"
	"time"
)

// CallRecord is one Discord request as the audit log reports it. Route is the
// template (or, for discord_request, the redacted path) — never the expanded
// path, which may contain a webhook token.
type CallRecord struct {
	Method        string `json:"method"`
	Route         string `json:"route"`
	Status        int    `json:"status"`
	RateLimitedMS int64  `json:"rate_limited_ms"`
}

// Recorder collects the Discord calls made while serving one tool call.
type Recorder struct {
	mu    sync.Mutex
	calls []CallRecord
}

// NewRecorder returns an empty recorder.
func NewRecorder() *Recorder { return &Recorder{} }

type recorderKey struct{}

// WithRecorder attaches r to ctx; Client.Do appends to it.
func WithRecorder(ctx context.Context, r *Recorder) context.Context {
	return context.WithValue(ctx, recorderKey{}, r)
}

func recorderFrom(ctx context.Context) *Recorder {
	r, _ := ctx.Value(recorderKey{}).(*Recorder)
	return r
}

func (r *Recorder) add(method, route string, status int, waited time.Duration) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, CallRecord{Method: method, Route: route, Status: status, RateLimitedMS: waited.Milliseconds()})
}

// Calls returns a copy of the recorded calls.
func (r *Recorder) Calls() []CallRecord {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]CallRecord(nil), r.calls...)
}
