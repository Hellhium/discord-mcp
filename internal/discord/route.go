package discord

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/Hellhium/discord-mcp/internal/credential"
)

var paramRe = regexp.MustCompile(`\{([a-z_]+)\}`)

// expandRoute substitutes validated parameters into a route template. Every
// value is checked by kind before substitution, so a tool argument can never
// add path segments.
func expandRoute(route string, params map[string]string) (string, error) {
	var bad error
	used := 0
	out := paramRe.ReplaceAllStringFunc(route, func(m string) string {
		name := m[1 : len(m)-1]
		v, ok := params[name]
		if !ok {
			bad = fmt.Errorf("internal: route %s has no value for %s", route, name)
			return m
		}
		used++
		switch {
		case name == "emoji":
			if v == "" || len(v) > 100 {
				bad = &ArgError{Msg: "emoji must be a unicode emoji or name:id"}
			}
			return url.PathEscape(v)
		case name == "webhook_token":
			if !credential.IsWebhookToken(v) {
				bad = &ArgError{Msg: "invalid webhook token"}
			}
			return v
		case strings.HasSuffix(name, "_id"):
			if !credential.IsSnowflake(v) {
				bad = &ArgError{Msg: fmt.Sprintf("%s must be a Discord ID (digits only), got %q", name, v)}
			}
			return v
		default:
			bad = fmt.Errorf("internal: route parameter %s has no validation rule", name)
			return m
		}
	})
	if bad != nil {
		return "", bad
	}
	if used != len(params) {
		return "", errors.New("internal: route " + route + " was given unused parameters")
	}
	return out, nil
}

// majorParams are the IDs that make a Discord rate-limit bucket distinct.
var majorParams = []string{"channel_id", "guild_id", "webhook_id"}

// bucketFor keys discordgo's rate limiter the way Discord buckets requests:
// per method and route, per major resource.
func bucketFor(method, route string, params map[string]string) string {
	b := route
	for _, p := range majorParams {
		if v, ok := params[p]; ok {
			b = strings.ReplaceAll(b, "{"+p+"}", v)
		}
	}
	return method + " " + b
}
