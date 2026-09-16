// Package credential recognises the shape of Discord credentials and IDs
// without contacting Discord. It is a leaf package so the config validator,
// the auth resolver and tool argument checks share one definition of what a
// snowflake, a bot token and a webhook look like.
//
// Shape is not validity. A value that passes here may still be rejected by
// Discord; a value that fails here is never sent to Discord at all, which keeps
// garbage from counting against Discord's invalid-request limit.
package credential

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	snowflakeRe    = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
	botTokenRe     = regexp.MustCompile(`^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$`)
	webhookTokenRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	webhookPathRe  = regexp.MustCompile(`^/api(?:/v[0-9]+)?/webhooks/([^/]+)/([^/]+)/?$`)
)

// webhookHosts are the hosts Discord serves webhook URLs from. The port is part
// of url.URL.Host, so "discord.com:8443" does not match.
var webhookHosts = map[string]bool{
	"discord.com":        true,
	"discordapp.com":     true,
	"ptb.discord.com":    true,
	"canary.discord.com": true,
}

// Webhook identifies a Discord webhook. Token is a credential: anyone holding
// ID and Token can post as the webhook.
type Webhook struct {
	ID    string
	Token string
}

// IsSnowflake reports whether s looks like a Discord ID: 1 to 20 decimal
// digits, no leading zero.
func IsSnowflake(s string) bool { return snowflakeRe.MatchString(s) }

// IsBotToken reports whether s looks like a Discord bot token: three non-empty
// base64url segments separated by dots.
func IsBotToken(s string) bool { return botTokenRe.MatchString(s) }

// IsWebhookToken reports whether s looks like a webhook token.
func IsWebhookToken(s string) bool { return webhookTokenRe.MatchString(s) }

// ParseWebhook accepts a full webhook URL on a Discord host, or the short
// "<id>/<token>" form. A query string on a URL is ignored.
func ParseWebhook(s string) (Webhook, bool) {
	if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err != nil || u.Scheme != "https" || u.User != nil || !webhookHosts[strings.ToLower(u.Host)] {
			return Webhook{}, false
		}
		m := webhookPathRe.FindStringSubmatch(u.Path)
		if m == nil {
			return Webhook{}, false
		}
		return webhook(m[1], m[2])
	}
	id, token, ok := strings.Cut(s, "/")
	if !ok {
		return Webhook{}, false
	}
	return webhook(id, token)
}

func webhook(id, token string) (Webhook, bool) {
	if !IsSnowflake(id) || !IsWebhookToken(token) {
		return Webhook{}, false
	}
	return Webhook{ID: id, Token: token}, true
}
