// Package discordtest is a fake Discord REST API for tests. It records every
// request and serves handlers registered with Go ServeMux patterns. Its
// HTTPClient rewrites requests for https://discord.com to the fake and refuses
// every other host, so a test also proves nothing was sent elsewhere.
package discordtest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
)

// Request is one request the fake received.
type Request struct {
	Method  string
	Path    string // decoded, e.g. /api/v10/channels/1/messages
	RawPath string // escaped, as sent on the wire
	Query   url.Values
	Header  http.Header
	Body    []byte
}

// Server is the fake. Unmatched requests get Discord's 404 shape.
type Server struct {
	srv  *httptest.Server
	mux  *http.ServeMux
	mu   sync.Mutex
	reqs []Request
}

// New starts a fake closed at the end of the test.
func New(t testing.TB) *Server {
	s := &Server{mux: http.NewServeMux()}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	s.reqs = append(s.reqs, Request{
		Method:  r.Method,
		Path:    r.URL.Path,
		RawPath: r.URL.EscapedPath(),
		Query:   r.URL.Query(),
		Header:  r.Header.Clone(),
		Body:    body,
	})
	s.mu.Unlock()
	r.Body = io.NopCloser(bytes.NewReader(body))
	if _, pattern := s.mux.Handler(r); pattern == "" {
		JSON(http.StatusNotFound, map[string]any{"message": "404: Not Found", "code": 0})(w, r)
		return
	}
	s.mux.ServeHTTP(w, r)
}

// Handle registers h for a ServeMux pattern such as
// "GET /api/v10/users/@me".
func (s *Server) Handle(pattern string, h http.HandlerFunc) { s.mux.HandleFunc(pattern, h) }

// Requests returns a copy of every request received so far.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.reqs...)
}

// HTTPClient returns a client that sends discord.com traffic to the fake.
func (s *Server) HTTPClient() *http.Client {
	target, _ := url.Parse(s.srv.URL)
	return &http.Client{Transport: rewrite{target: target, base: s.srv.Client().Transport}}
}

type rewrite struct {
	target *url.URL
	base   http.RoundTripper
}

func (rw rewrite) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host != "discord.com" {
		return nil, fmt.Errorf("discordtest: refusing request to host %q", r.URL.Host)
	}
	r2 := r.Clone(r.Context())
	r2.URL.Scheme = rw.target.Scheme
	r2.URL.Host = rw.target.Host
	r2.Host = rw.target.Host
	return rw.base.RoundTrip(r2)
}

// JSON answers with status and v encoded as JSON (no body for 204).
func JSON(status int, v any) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if status == http.StatusNoContent {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
}
