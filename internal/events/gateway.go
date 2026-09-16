package events

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/Hellhium/discord-mcp/internal/audit"
	"github.com/Hellhium/discord-mcp/internal/intents"
)

// TypeGatewayReconnected marks a point where events may be missing: the
// session was re-identified instead of resumed. Lowercase so it can never
// collide with a Discord dispatch type.
const TypeGatewayReconnected = "gateway_reconnected"

// Gateway feeds one bot's Gateway dispatches into a Buffer. discordgo handles
// heartbeats, resumes and reconnects; this type decides what is buffered.
type Gateway struct {
	instance string
	session  *discordgo.Session
	buf      *Buffer
	log      *audit.Logger

	mu            sync.Mutex
	readyCount    int
	startupGuilds map[string]bool
	ready         chan struct{}
}

// NewGateway prepares a session: sets the given intents, disables its state
// cache, and registers permanent event and disconnect handlers. Nothing
// connects until Start.
func NewGateway(instance string, session *discordgo.Session, mask intents.Mask, buf *Buffer, log *audit.Logger) *Gateway {
	session.Identify.Intents = discordgo.Intent(mask)
	session.StateEnabled = false
	g := &Gateway{instance: instance, session: session, buf: buf, log: log, ready: make(chan struct{})}
	session.AddHandler(func(_ *discordgo.Session, e *discordgo.Event) { g.handle(e.Type, e.RawData) })
	session.AddHandler(func(_ *discordgo.Session, _ *discordgo.Disconnect) {
		g.log.Gateway(g.instance, "gateway_disconnected", nil)
	})
	return g
}

// Start opens the Gateway and waits for the first READY. A bad token or a
// disallowed intent never produces READY, so this fails after readyTimeout
// instead of letting discordgo retry forever in the background.
func (g *Gateway) Start(readyTimeout time.Duration) error {
	if err := g.session.Open(); err != nil {
		return fmt.Errorf("instance %q: open gateway: %w", g.instance, err)
	}
	select {
	case <-g.ready:
		return nil
	case <-time.After(readyTimeout):
		_ = g.session.Close()
		return fmt.Errorf("instance %q: gateway not ready within %s (check the bot token and intents)", g.instance, readyTimeout)
	}
}

// Close disconnects the Gateway.
func (g *Gateway) Close() error { return g.session.Close() }

// handle buffers dispatches except lifecycle ones: READY and RESUMED, and
// the GUILD_CREATE burst that follows each READY for the guilds it listed.
func (g *Gateway) handle(typ string, raw json.RawMessage) {
	switch typ {
	case "":
		return
	case "READY":
		var r struct {
			Guilds []struct {
				ID string `json:"id"`
			} `json:"guilds"`
		}
		_ = json.Unmarshal(raw, &r)
		g.mu.Lock()
		g.startupGuilds = make(map[string]bool, len(r.Guilds))
		for _, gu := range r.Guilds {
			g.startupGuilds[gu.ID] = true
		}
		g.readyCount++
		first := g.readyCount == 1
		g.mu.Unlock()
		if first {
			g.log.Gateway(g.instance, "gateway_connected", nil)
			close(g.ready)
			return
		}
		g.log.Gateway(g.instance, "gateway_reconnected", nil)
		g.buf.Append(TypeGatewayReconnected, json.RawMessage(`{}`))
		return
	case "RESUMED":
		g.log.Gateway(g.instance, "gateway_resumed", nil)
		return
	case "GUILD_CREATE":
		var gc struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(raw, &gc)
		g.mu.Lock()
		startup := g.startupGuilds[gc.ID]
		delete(g.startupGuilds, gc.ID)
		g.mu.Unlock()
		if startup {
			return
		}
	}
	g.buf.Append(typ, raw)
}
