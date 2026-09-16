package tools

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"path/filepath"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/Hellhium/discord-mcp/internal/discord"
)

const (
	maxAttachments = 10
	maxEmbeds      = 10
)

// Annotations. mcp.NewTool defaults to destructive, so every tool sets both.
func readOnly() mcp.ToolOption {
	return func(t *mcp.Tool) {
		mcp.WithReadOnlyHintAnnotation(true)(t)
		mcp.WithDestructiveHintAnnotation(false)(t)
	}
}

func mutating() mcp.ToolOption {
	return func(t *mcp.Tool) {
		mcp.WithReadOnlyHintAnnotation(false)(t)
		mcp.WithDestructiveHintAnnotation(false)(t)
	}
}

func destructive() mcp.ToolOption {
	return func(t *mcp.Tool) {
		mcp.WithReadOnlyHintAnnotation(false)(t)
		mcp.WithDestructiveHintAnnotation(true)(t)
	}
}

func idParam(name, desc string) mcp.ToolOption {
	return mcp.WithString(name, mcp.Required(), mcp.Description(desc+" (Discord ID, as a string)"))
}

func optIDParam(name, desc string) mcp.ToolOption {
	return mcp.WithString(name, mcp.Description(desc+" (Discord ID, as a string)"))
}

func withFormat() mcp.ToolOption {
	return mcp.WithString("format", mcp.Enum("text", "json"),
		mcp.Description("text (default): compact summary. json: Discord's object exactly as returned."))
}

func withReason() mcp.ToolOption {
	return mcp.WithString("reason", mcp.Description("Recorded in the server's Discord audit log"))
}

func withAllowMentions() mcp.ToolOption {
	return mcp.WithBoolean("allow_mentions", mcp.Description(
		"Default true: @everyone, @here, role and user mentions ping as usual. false: nothing pings; the text is unchanged."))
}

func withAttachments() mcp.ToolOption {
	return mcp.WithArray("attachments",
		mcp.Description("Files to upload (max 10). Content is base64; URLs are not fetched."),
		mcp.Items(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"filename":       map[string]any{"type": "string"},
				"content_base64": map[string]any{"type": "string"},
				"description":    map[string]any{"type": "string"},
			},
			"required": []string{"filename", "content_base64"},
		}))
}

func withEmbeds() mcp.ToolOption {
	return mcp.WithArray("embeds", mcp.Description("Discord embed objects (max 10)"), mcp.Items(map[string]any{"type": "object"}))
}

func formatArg(a Args) (string, error) {
	f, err := a.String("format")
	if err != nil {
		return "", err
	}
	switch f {
	case "", "text":
		return "text", nil
	case "json":
		return "json", nil
	}
	return "", argErr("format must be text or json")
}

// messagePayload builds the JSON body for sending or editing a message.
// needBody requires at least one of content, embeds or attachments;
// allowFiles rejects attachments on tools that do not upload.
func messagePayload(a Args, needBody, allowFiles bool) (map[string]any, []discord.File, error) {
	body := map[string]any{}
	content, err := a.String("content")
	if err != nil {
		return nil, nil, err
	}
	if content != "" {
		body["content"] = content
	}
	embeds, err := a.List("embeds")
	if err != nil {
		return nil, nil, err
	}
	if len(embeds) > maxEmbeds {
		return nil, nil, argErr("at most %d embeds", maxEmbeds)
	}
	if embeds != nil {
		body["embeds"] = embeds
	}
	allow, err := a.Bool("allow_mentions", true)
	if err != nil {
		return nil, nil, err
	}
	if !allow {
		body["allowed_mentions"] = map[string]any{"parse": []string{}}
	}
	if _, ok := a.present("attachments"); ok && !allowFiles {
		return nil, nil, argErr("this tool does not accept attachments")
	}
	files, meta, err := attachments(a)
	if err != nil {
		return nil, nil, err
	}
	if len(files) > 0 {
		body["attachments"] = meta
	}
	if needBody && content == "" && len(embeds) == 0 && len(files) == 0 {
		return nil, nil, argErr("provide content, embeds or attachments")
	}
	return body, files, nil
}

func attachments(a Args) ([]discord.File, []map[string]any, error) {
	list, err := a.List("attachments")
	if err != nil {
		return nil, nil, err
	}
	if len(list) > maxAttachments {
		return nil, nil, argErr("at most %d attachments", maxAttachments)
	}
	var files []discord.File
	var meta []map[string]any
	for i, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, nil, argErr("attachments[%d] must be an object", i)
		}
		name, _ := m["filename"].(string)
		if name == "" || strings.ContainsAny(name, `/\`) {
			return nil, nil, argErr("attachments[%d].filename must be a plain file name", i)
		}
		b64, _ := m["content_base64"].(string)
		data, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return nil, nil, argErr("attachments[%d].content_base64 is not valid base64", i)
		}
		files = append(files, discord.File{Name: name, ContentType: mime.TypeByExtension(filepath.Ext(name)), Data: data})
		entry := map[string]any{"id": i, "filename": name}
		if d, ok := m["description"].(string); ok && d != "" {
			entry["description"] = d
		}
		meta = append(meta, entry)
	}
	return files, meta, nil
}

// output renders a Discord response body as text, or returns it as-is for
// format json.
func output[T any](body json.RawMessage, format string, render func(T) string) (string, error) {
	if format == "json" {
		return string(body), nil
	}
	var v T
	if err := json.Unmarshal(body, &v); err != nil {
		return "", fmt.Errorf("decode Discord response: %w", err)
	}
	return render(v), nil
}
