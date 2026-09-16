package tools

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

type userJSON struct {
	ID         string `json:"id"`
	Username   string `json:"username"`
	GlobalName string `json:"global_name"`
	Bot        bool   `json:"bot"`
}

type attachmentJSON struct {
	Filename string `json:"filename"`
	URL      string `json:"url"`
}

type messageJSON struct {
	ID               string           `json:"id"`
	ChannelID        string           `json:"channel_id"`
	Author           userJSON         `json:"author"`
	Content          string           `json:"content"`
	Timestamp        time.Time        `json:"timestamp"`
	EditedTimestamp  *time.Time       `json:"edited_timestamp"`
	Attachments      []attachmentJSON `json:"attachments"`
	Embeds           []map[string]any `json:"embeds"`
	Pinned           bool             `json:"pinned"`
	MessageReference *struct {
		MessageID string `json:"message_id"`
	} `json:"message_reference"`
	Reactions []struct {
		Count int `json:"count"`
		Emoji struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"emoji"`
	} `json:"reactions"`
	Thread *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"thread"`
}

type channelJSON struct {
	ID             string `json:"id"`
	Type           int    `json:"type"`
	GuildID        string `json:"guild_id"`
	Name           string `json:"name"`
	Topic          string `json:"topic"`
	ParentID       string `json:"parent_id"`
	Position       int    `json:"position"`
	NSFW           bool   `json:"nsfw"`
	ThreadMetadata *struct {
		Archived bool `json:"archived"`
		Locked   bool `json:"locked"`
	} `json:"thread_metadata"`
}

const timeLayout = "2006-01-02 15:04"

func displayName(u userJSON) string {
	name := u.Username
	if u.GlobalName != "" {
		name = u.GlobalName
	}
	if u.Bot {
		name += " [bot]"
	}
	return name
}

func renderMessage(m messageJSON) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "[%s] %s (%s) #%s: %s", m.Timestamp.UTC().Format(timeLayout), displayName(m.Author), m.Author.ID, m.ID, m.Content)
	if m.EditedTimestamp != nil {
		sb.WriteString(" (edited)")
	}
	if m.Pinned {
		sb.WriteString(" (pinned)")
	}
	if m.MessageReference != nil && m.MessageReference.MessageID != "" {
		fmt.Fprintf(&sb, "\n  reply to #%s", m.MessageReference.MessageID)
	}
	for _, a := range m.Attachments {
		fmt.Fprintf(&sb, "\n  attachment: %s %s", a.Filename, a.URL)
	}
	if len(m.Embeds) > 0 {
		fmt.Fprintf(&sb, "\n  %d embed(s)", len(m.Embeds))
	}
	if len(m.Reactions) > 0 {
		parts := make([]string, len(m.Reactions))
		for i, r := range m.Reactions {
			e := r.Emoji.Name
			if r.Emoji.ID != "" {
				e = r.Emoji.Name + ":" + r.Emoji.ID
			}
			parts[i] = fmt.Sprintf("%s x%d", e, r.Count)
		}
		fmt.Fprintf(&sb, "\n  reactions: %s", strings.Join(parts, ", "))
	}
	if m.Thread != nil {
		fmt.Fprintf(&sb, "\n  thread: %s (%s)", m.Thread.Name, m.Thread.ID)
	}
	return sb.String()
}

// renderMessages prints oldest first; Discord lists newest first.
func renderMessages(msgs []messageJSON) string {
	if len(msgs) == 0 {
		return "No messages."
	}
	sorted := slices.Clone(msgs)
	slices.SortStableFunc(sorted, func(a, b messageJSON) int { return a.Timestamp.Compare(b.Timestamp) })
	lines := make([]string, len(sorted))
	for i, m := range sorted {
		lines[i] = renderMessage(m)
	}
	return strings.Join(lines, "\n")
}

var channelTypes = map[int]string{
	0: "text", 1: "dm", 2: "voice", 3: "group-dm", 4: "category", 5: "announcement",
	10: "announcement-thread", 11: "public-thread", 12: "private-thread", 13: "stage", 14: "directory", 15: "forum", 16: "media",
}

func channelTypeName(t int) string {
	if n, ok := channelTypes[t]; ok {
		return n
	}
	return fmt.Sprintf("type-%d", t)
}

func renderChannel(c channelJSON) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "#%s (%s) %s", c.Name, c.ID, channelTypeName(c.Type))
	if c.ParentID != "" {
		fmt.Fprintf(&sb, " parent=%s", c.ParentID)
	}
	if c.NSFW {
		sb.WriteString(" nsfw")
	}
	if tm := c.ThreadMetadata; tm != nil {
		if tm.Archived {
			sb.WriteString(" archived")
		}
		if tm.Locked {
			sb.WriteString(" locked")
		}
	}
	if c.Topic != "" {
		fmt.Fprintf(&sb, " — %s", c.Topic)
	}
	return sb.String()
}
