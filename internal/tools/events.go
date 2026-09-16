package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/Hellhium/discord-mcp/internal/auth"
	"github.com/Hellhium/discord-mcp/internal/events"
)

const (
	defaultWaitSeconds = 60
	maxWaitSeconds     = 300
)

func buffer(p *auth.Principal) (*events.Buffer, error) {
	if p.Events == nil {
		return nil, argErr("events are not enabled for this credential")
	}
	return p.Events, nil
}

// cursorErr turns buffer cursor errors into argument errors.
func cursorErr(err error) error {
	if errors.Is(err, events.ErrBadCursor) {
		return argErr("%s", err.Error())
	}
	return err
}

func renderEvent(e events.Event) string {
	if e.Type == "MESSAGE_CREATE" || e.Type == "MESSAGE_UPDATE" {
		var m messageJSON
		if json.Unmarshal(e.Data, &m) == nil && m.ID != "" {
			return fmt.Sprintf("#%d %s %s", e.Seq, e.Type, renderMessage(m))
		}
	}
	if e.Type == events.TypeGatewayReconnected {
		return fmt.Sprintf("#%d %s: the Gateway reconnected; events before this may be missing", e.Seq, e.Type)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "#%d %s", e.Seq, e.Type)
	for _, kv := range [][2]string{{"guild", e.GuildID}, {"channel", e.ChannelID}, {"user", e.AuthorID}} {
		if kv[1] != "" {
			fmt.Fprintf(&sb, " %s=%s", kv[0], kv[1])
		}
	}
	return sb.String()
}

const gapNote = "gap: some events were missed (cursor older than the buffer, or the server restarted)"

func pollEventsTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_poll_events",
			mcp.WithDescription("Read buffered Discord Gateway events (new messages, reactions, member joins…) after a cursor. "+
				"Without a cursor, returns the latest events. Pass next_cursor back to continue."),
			readOnly(),
			mcp.WithString("cursor", mcp.Description("next_cursor from a previous call")),
			mcp.WithArray("types", mcp.Description("Only these event types, e.g. MESSAGE_CREATE"), mcp.Items(map[string]any{"type": "string"})),
			optIDParam("guild_id", "Only events in this guild"),
			optIDParam("channel_id", "Only events in this channel"),
			mcp.WithNumber("limit", mcp.Description("Events, 1-100 (default 50)")),
			withFormat()),
		Handle: func(_ context.Context, p *auth.Principal, a Args) (string, error) {
			buf, err := buffer(p)
			if err != nil {
				return "", err
			}
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			cursor, err := a.String("cursor")
			if err != nil {
				return "", err
			}
			types, err := a.StringList("types")
			if err != nil {
				return "", err
			}
			guild, err := a.OptionalID("guild_id")
			if err != nil {
				return "", err
			}
			channel, err := a.OptionalID("channel_id")
			if err != nil {
				return "", err
			}
			limit, err := a.Int("limit", 50, 1, 100)
			if err != nil {
				return "", err
			}
			page, err := buf.Since(cursor, events.Filter{Types: types, GuildID: guild, ChannelID: channel}, limit)
			if err != nil {
				return "", cursorErr(err)
			}
			if format == "json" {
				evs := page.Events
				if evs == nil {
					evs = []events.Event{}
				}
				out, _ := json.Marshal(map[string]any{"events": evs, "next_cursor": page.NextCursor, "gap": page.Gap})
				return string(out), nil
			}
			lines := []string{"next_cursor: " + page.NextCursor}
			if page.Gap {
				lines = append(lines, gapNote)
			}
			if len(page.Events) == 0 {
				lines = append(lines, "No new events.")
			}
			for _, e := range page.Events {
				lines = append(lines, renderEvent(e))
			}
			return strings.Join(lines, "\n"), nil
		},
	}
}

func waitForMessageTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_wait_for_message",
			mcp.WithDescription("Wait for the next new message, optionally in one channel or from one user. "+
				"Ignores the bot's own messages unless include_self. A timeout is a normal result that returns a cursor to keep waiting from."),
			readOnly(),
			optIDParam("channel_id", "Only messages in this channel"),
			optIDParam("author_id", "Only messages from this user"),
			mcp.WithBoolean("include_self", mcp.Description("Also match the bot's own messages (default false)")),
			mcp.WithString("cursor", mcp.Description("Wait for messages after this cursor (default: from now)")),
			mcp.WithNumber("timeout_seconds", mcp.Description("1-300 (default 60)")),
			withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			buf, err := buffer(p)
			if err != nil {
				return "", err
			}
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			channel, err := a.OptionalID("channel_id")
			if err != nil {
				return "", err
			}
			author, err := a.OptionalID("author_id")
			if err != nil {
				return "", err
			}
			includeSelf, err := a.Bool("include_self", false)
			if err != nil {
				return "", err
			}
			cursor, err := a.String("cursor")
			if err != nil {
				return "", err
			}
			seconds, err := a.Int("timeout_seconds", defaultWaitSeconds, 1, maxWaitSeconds)
			if err != nil {
				return "", err
			}
			f := events.Filter{Types: []string{"MESSAGE_CREATE"}, ChannelID: channel, AuthorID: author}
			if !includeSelf {
				f.ExcludeAuthorID = p.BotUserID
			}

			waitCtx, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
			defer cancel()
			ev, next, gap, err := buf.Wait(waitCtx, cursor, f)
			switch {
			case err == nil:
			case errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil:
				ev = nil
			case errors.Is(err, events.ErrClosed):
				return "", errors.New("the server is shutting down")
			case errors.Is(err, events.ErrBadCursor):
				return "", cursorErr(err)
			default:
				return "", fmt.Errorf("wait cancelled: %w", err)
			}

			if format == "json" {
				var msg json.RawMessage = json.RawMessage("null")
				if ev != nil {
					msg = ev.Data
				}
				out, _ := json.Marshal(map[string]any{"message": msg, "next_cursor": next, "gap": gap})
				return string(out), nil
			}
			var lines []string
			if ev == nil {
				lines = append(lines, fmt.Sprintf("No matching message within %ds.", seconds))
			} else {
				lines = append(lines, renderEvent(*ev))
			}
			if gap {
				lines = append(lines, gapNote)
			}
			lines = append(lines, "next_cursor: "+next)
			return strings.Join(lines, "\n"), nil
		},
	}
}
