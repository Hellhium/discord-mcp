package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/Hellhium/discord-mcp/internal/auth"
	"github.com/Hellhium/discord-mcp/internal/discord"
)

const (
	maxRawResponse  = 64 << 10
	maxRawErrorBody = 4 << 10
)

func requestTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_request",
			mcp.WithDescription("Call any Discord REST API endpoint not covered by another tool, as the bot. "+
				"route is relative to https://discord.com/api/v10, e.g. /guilds/123/emojis. "+
				"Returns {status, body}; bodies over 64 KB are truncated."),
			destructive(),
			mcp.WithString("method", mcp.Required(), mcp.Enum("GET", "POST", "PUT", "PATCH", "DELETE")),
			mcp.WithString("route", mcp.Required(), mcp.Description("Path relative to /api/v10, starting with /; no query string")),
			mcp.WithObject("query", mcp.Description("Query parameters; array values repeat the parameter")),
			mcp.WithAny("body", mcp.Description("JSON body (not allowed with GET)")),
			withReason()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			method, err := a.RequiredString("method")
			if err != nil {
				return "", err
			}
			route, err := a.RequiredString("route")
			if err != nil {
				return "", err
			}
			rawQuery, err := a.Object("query")
			if err != nil {
				return "", err
			}
			query, err := toQuery(rawQuery)
			if err != nil {
				return "", err
			}
			body, hasBody := a.present("body")
			if hasBody && method == "GET" {
				return "", argErr("body is not allowed with GET")
			}
			reason, err := a.String("reason")
			if err != nil {
				return "", err
			}
			resp, err := p.Client.DoRaw(ctx, method, route, query, body, reason)
			if err != nil {
				var ae *discord.APIError
				if errors.As(err, &ae) && len(resp.Body) > 0 {
					return "", fmt.Errorf("%w\n%s", err, truncateString(string(resp.Body), maxRawErrorBody))
				}
				return "", err
			}
			return rawResult(resp), nil
		},
	}
}

func toQuery(m map[string]any) (url.Values, error) {
	if len(m) == 0 {
		return nil, nil
	}
	q := url.Values{}
	for k, v := range m {
		values := []any{v}
		if list, ok := v.([]any); ok {
			values = list
		}
		for _, item := range values {
			s, err := queryValue(k, item)
			if err != nil {
				return nil, err
			}
			q.Add(k, s)
		}
	}
	return q, nil
}

func queryValue(key string, v any) (string, error) {
	switch x := v.(type) {
	case string:
		return x, nil
	case bool:
		return strconv.FormatBool(x), nil
	case float64:
		if x == math.Trunc(x) {
			return strconv.FormatInt(int64(x), 10), nil
		}
		return strconv.FormatFloat(x, 'f', -1, 64), nil
	}
	return "", argErr("query.%s must be a string, number, boolean or an array of those", key)
}

func rawResult(resp discord.Response) string {
	if len(resp.Body) > maxRawResponse {
		out, _ := json.Marshal(map[string]any{
			"status":      resp.Status,
			"truncated":   true,
			"bytes":       len(resp.Body),
			"body_prefix": strings.ToValidUTF8(string(resp.Body[:maxRawResponse]), ""),
		})
		return string(out)
	}
	body := json.RawMessage("null")
	if len(resp.Body) > 0 && json.Valid(resp.Body) {
		body = resp.Body
	}
	out, _ := json.Marshal(struct {
		Status int             `json:"status"`
		Body   json.RawMessage `json:"body"`
	}{resp.Status, body})
	return string(out)
}

func truncateString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "") + "…"
}
