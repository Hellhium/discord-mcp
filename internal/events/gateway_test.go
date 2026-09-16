package events

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"

	"github.com/Hellhium/discord-mcp/internal/audit"
	"github.com/Hellhium/discord-mcp/internal/intents"
)

func newTestGateway(t *testing.T) (*Gateway, *Buffer, *bytes.Buffer) {
	t.Helper()
	s, err := discordgo.New("Bot a.b.c")
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	buf := newBuffer(10, "boot")
	m, _ := intents.Parse([]string{"guilds", "guild_messages", "message_content"})
	return NewGateway("assistant", s, m, buf, audit.New(&logs)), buf, &logs
}

func types(t *testing.T, b *Buffer) []string {
	t.Helper()
	p, err := b.Since("boot:0", Filter{}, 100)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range p.Events {
		out = append(out, e.Type)
	}
	return out
}

func TestNewGatewaySetsIntents(t *testing.T) {
	g, _, _ := newTestGateway(t)
	if want := discordgo.Intent(1<<0 | 1<<9 | 1<<15); g.session.Identify.Intents != want {
		t.Fatalf("intents = %b, want %b", g.session.Identify.Intents, want)
	}
}

func TestGatewayHandling(t *testing.T) {
	g, buf, logs := newTestGateway(t)

	g.handle("READY", json.RawMessage(`{"guilds":[{"id":"1","unavailable":true},{"id":"2","unavailable":true}]}`))
	select {
	case <-g.ready:
	default:
		t.Fatal("first READY must mark the gateway ready")
	}
	g.handle("GUILD_CREATE", json.RawMessage(`{"id":"1"}`)) // startup burst: dropped
	g.handle("GUILD_CREATE", json.RawMessage(`{"id":"3"}`)) // joined a new guild: kept
	g.handle("MESSAGE_CREATE", json.RawMessage(`{"channel_id":"5","author":{"id":"7"}}`))
	g.handle("RESUMED", json.RawMessage(`{}`))              // clean resume: no marker
	g.handle("GUILD_CREATE", json.RawMessage(`{"id":"1"}`)) // already consumed: kept now

	g.handle("READY", json.RawMessage(`{"guilds":[{"id":"1"}]}`)) // new session after a failed resume
	g.handle("GUILD_CREATE", json.RawMessage(`{"id":"1"}`))       // burst of the new session: dropped

	want := []string{"GUILD_CREATE", "MESSAGE_CREATE", "GUILD_CREATE", TypeGatewayReconnected}
	if got := types(t, buf); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("buffered = %v, want %v", got, want)
	}
	for _, msg := range []string{`"msg":"gateway_connected"`, `"msg":"gateway_resumed"`, `"msg":"gateway_reconnected"`} {
		if !strings.Contains(logs.String(), msg) {
			t.Errorf("logs missing %s:\n%s", msg, logs.String())
		}
	}
}
