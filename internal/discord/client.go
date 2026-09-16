// Package discord is the only code that talks to Discord's REST API. It wraps
// a discordgo session per credential — discordgo keeps the rate-limit buckets —
// and adds what the tools and the audit log need: validated route templates,
// a fixed API host, typed errors that never contain a URL, a per-call timeout
// that also bounds rate-limit waits, and a record of every call made.
package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/bwmarrin/discordgo"
)

// APIBase is the only place requests are sent. No argument can change it.
const APIBase = "https://discord.com/api/v10"

const defaultTimeout = 15 * time.Second

// Options configures a client.
type Options struct {
	// Timeout bounds one Do call, rate-limit waits included.
	Timeout time.Duration
	// HTTPClient supplies the transport (tests inject the fake). Its Timeout
	// is ignored; Timeout above applies instead.
	HTTPClient *http.Client
}

// Client sends REST calls with one credential.
type Client struct {
	session *discordgo.Session
	timeout time.Duration
}

// NewBot returns a client authenticating as a bot.
func NewBot(token string, opts Options) *Client { return newClient("Bot "+token, opts) }

// NewWebhook returns a client with no Authorization header: webhook routes
// authenticate with the token in the path.
func NewWebhook(opts Options) *Client { return newClient("", opts) }

func newClient(authorization string, opts Options) *Client {
	s, _ := discordgo.New(authorization) // never fails in v0.29
	base := http.DefaultTransport
	if opts.HTTPClient != nil && opts.HTTPClient.Transport != nil {
		base = opts.HTTPClient.Transport
	}
	s.Client = &http.Client{Transport: statusTransport{base: base}}
	s.StateEnabled = false
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Client{session: s, timeout: timeout}
}

// Session exposes the discordgo session, for the Gateway.
func (c *Client) Session() *discordgo.Session { return c.session }

// Call describes one REST request.
type Call struct {
	Method string
	// Route is a template such as "/channels/{channel_id}/messages".
	Route  string
	Params map[string]string
	Query  url.Values
	// Body is JSON-encoded when non-nil; with Files it becomes payload_json.
	Body  any
	Files []File
	// Reason is sent as X-Audit-Log-Reason.
	Reason string
}

// File is an attachment uploaded as files[n].
type File struct {
	Name        string
	ContentType string
	Data        []byte
}

// Response is a successful answer (2xx, including 202).
type Response struct {
	Status int
	Body   json.RawMessage
}

// Do validates and sends call.
func (c *Client) Do(ctx context.Context, call Call) (Response, error) {
	path, err := expandRoute(call.Route, call.Params)
	if err != nil {
		return Response{}, err
	}
	return c.send(ctx, call, path, call.Route, bucketFor(call.Method, call.Route, call.Params))
}

// send performs the request. recordRoute is what the recorder stores.
func (c *Client) send(ctx context.Context, call Call, path, recordRoute, bucket string) (Response, error) {
	u := APIBase + path
	if len(call.Query) > 0 {
		u += "?" + call.Query.Encode()
	}
	contentType, body, err := encodeBody(call.Body, call.Files)
	if err != nil {
		return Response{}, &ArgError{Msg: err.Error()}
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	rec := recorderFrom(ctx)

	var waited time.Duration
	for {
		sink := &statusSink{}
		opts := []discordgo.RequestOption{
			discordgo.WithContext(context.WithValue(ctx, sinkKey{}, sink)),
			// Rate limits are handled here so a wait can be bounded by ctx.
			discordgo.WithRetryOnRatelimit(false),
		}
		if call.Reason != "" {
			// Discord requires the header value to be URL-encoded.
			opts = append(opts, discordgo.WithAuditLogReason(url.PathEscape(call.Reason)))
		}
		raw, err := c.session.RequestRaw(call.Method, u, contentType, body, bucket, 0, opts...)

		var rl *discordgo.RateLimitError
		if errors.As(err, &rl) {
			retry := rl.RetryAfter
			if dl, ok := ctx.Deadline(); ok && time.Until(dl) <= retry {
				rec.add(call.Method, recordRoute, sink.status, waited)
				return Response{}, &RateLimitedError{RetryAfter: retry}
			}
			select {
			case <-time.After(retry):
				waited += retry
				continue
			case <-ctx.Done():
				rec.add(call.Method, recordRoute, sink.status, waited)
				return Response{}, &RateLimitedError{RetryAfter: retry}
			}
		}
		rec.add(call.Method, recordRoute, sink.status, waited)
		return c.result(ctx, raw, sink.status, err)
	}
}

func (c *Client) result(ctx context.Context, raw []byte, status int, err error) (Response, error) {
	if err == nil {
		return Response{Status: status, Body: raw}, nil
	}
	var re *discordgo.RESTError
	if errors.As(err, &re) {
		st := re.Response.StatusCode
		if st == http.StatusAccepted {
			return Response{Status: st, Body: re.ResponseBody}, nil
		}
		ae := &APIError{Status: st, Message: http.StatusText(st)}
		if re.Message != nil {
			ae.Code = re.Message.Code
			if re.Message.Message != "" {
				ae.Message = re.Message.Message
			}
		}
		return Response{Status: st, Body: re.ResponseBody}, ae
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return Response{}, &UnavailableError{Cause: fmt.Sprintf("no response within %s", c.timeout)}
	}
	if ctx.Err() != nil {
		return Response{}, &UnavailableError{Cause: "request cancelled"}
	}
	// *url.Error embeds the full URL, which can contain a webhook token.
	var ue *url.Error
	if errors.As(err, &ue) {
		return Response{}, &UnavailableError{Cause: ue.Err.Error()}
	}
	return Response{}, &UnavailableError{Cause: err.Error()}
}

func encodeBody(body any, files []File) (string, []byte, error) {
	if len(files) > 0 {
		payload := body
		if payload == nil {
			payload = map[string]any{}
		}
		df := make([]*discordgo.File, len(files))
		for i, f := range files {
			df[i] = &discordgo.File{Name: f.Name, ContentType: f.ContentType, Reader: bytes.NewReader(f.Data)}
		}
		return discordgo.MultipartBodyWithJSON(payload, df)
	}
	if body == nil {
		return "", nil, nil
	}
	b, err := json.Marshal(body)
	if err != nil {
		return "", nil, fmt.Errorf("encode request body: %w", err)
	}
	return "application/json", b, nil
}

type sinkKey struct{}

type statusSink struct{ status int }

// statusTransport captures the HTTP status discordgo does not return on
// success, for the audit record.
type statusTransport struct{ base http.RoundTripper }

func (t statusTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(r)
	if sink, ok := r.Context().Value(sinkKey{}).(*statusSink); ok && resp != nil {
		sink.status = resp.StatusCode
	}
	return resp, err
}
