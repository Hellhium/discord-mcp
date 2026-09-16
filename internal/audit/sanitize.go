package audit

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/Hellhium/discord-mcp/internal/discord"
)

// MaxBodyBytes caps a logged discord_request body.
const MaxBodyBytes = 16 << 10

// SanitizeArgs returns a copy of tool arguments safe to log: attachment content
// becomes filename, size and SHA-256; a discord_request body over MaxBodyBytes
// is truncated; a discord_request route is redacted. Everything else, message
// text included, is logged as given.
func SanitizeArgs(args map[string]any) map[string]any {
	out := make(map[string]any, len(args))
	for k, v := range args {
		switch k {
		case "attachments":
			out[k] = sanitizeAttachments(v)
		case "body":
			out[k] = truncateJSON(v)
		case "route":
			if s, ok := v.(string); ok {
				out[k] = discord.RedactRoute(s)
			} else {
				out[k] = v
			}
		default:
			out[k] = v
		}
	}
	return out
}

func sanitizeAttachments(v any) any {
	list, ok := v.([]any)
	if !ok {
		return v
	}
	out := make([]any, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			out = append(out, "[invalid attachment]")
			continue
		}
		s := map[string]any{"filename": m["filename"]}
		if d, ok := m["description"]; ok {
			s["description"] = d
		}
		b64, _ := m["content_base64"].(string)
		data, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			s["size"] = len(b64)
			s["invalid_base64"] = true
		} else {
			sum := sha256.Sum256(data)
			s["size"] = len(data)
			s["sha256"] = hex.EncodeToString(sum[:])
		}
		out = append(out, s)
	}
	return out
}

func truncateJSON(v any) any {
	b, err := json.Marshal(v)
	if err != nil || len(b) <= MaxBodyBytes {
		return v
	}
	return map[string]any{
		"truncated": true,
		"bytes":     len(b),
		"prefix":    strings.ToValidUTF8(string(b[:MaxBodyBytes]), ""),
	}
}

// Targets extracts the Discord IDs a call was aimed at: every string argument
// whose name ends in _id.
func Targets(args map[string]any) map[string]string {
	t := make(map[string]string)
	for k, v := range args {
		if s, ok := v.(string); ok && strings.HasSuffix(k, "_id") {
			t[k] = s
		}
	}
	return t
}
