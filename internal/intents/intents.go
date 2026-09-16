// Package intents maps the Gateway intent names used in the YAML config to
// Discord's intent bitmask, and knows which intents are privileged. It has no
// Discord client dependency so config validation can use it.
package intents

import "fmt"

// Mask is a Gateway intents bitmask, as sent in the Identify payload.
type Mask uint64

// byName uses Discord's documented intent names in snake_case.
var byName = map[string]Mask{
	"guilds":                        1 << 0,
	"guild_members":                 1 << 1,
	"guild_moderation":              1 << 2,
	"guild_expressions":             1 << 3,
	"guild_integrations":            1 << 4,
	"guild_webhooks":                1 << 5,
	"guild_invites":                 1 << 6,
	"guild_voice_states":            1 << 7,
	"guild_presences":               1 << 8,
	"guild_messages":                1 << 9,
	"guild_message_reactions":       1 << 10,
	"guild_message_typing":          1 << 11,
	"direct_messages":               1 << 12,
	"direct_message_reactions":      1 << 13,
	"direct_message_typing":         1 << 14,
	"message_content":               1 << 15,
	"guild_scheduled_events":        1 << 16,
	"auto_moderation_configuration": 1 << 20,
	"auto_moderation_execution":     1 << 21,
	"guild_message_polls":           1 << 24,
	"direct_message_polls":          1 << 25,
}

// Parse ORs the named intents together. An unknown name is an error that
// names it.
func Parse(names []string) (Mask, error) {
	var m Mask
	for _, n := range names {
		bit, ok := byName[n]
		if !ok {
			return 0, fmt.Errorf("unknown gateway intent %q", n)
		}
		m |= bit
	}
	return m, nil
}

// privileged lists, in a stable order, each privileged intent and the
// application flags that enable it: Discord sets the full flag for verified
// bots in 100+ servers and the _LIMITED flag below that, and either suffices.
var privileged = []struct {
	name   string
	intent Mask
	flags  uint64
}{
	{"guild_members", 1 << 1, 1<<14 | 1<<15},
	{"guild_presences", 1 << 8, 1<<12 | 1<<13},
	{"message_content", 1 << 15, 1<<18 | 1<<19},
}

// MissingPrivileged returns the privileged intents requested in m that the
// application flags do not enable, or nil when there are none. Opening a
// Gateway with such an intent fails with close code 4014; checking first lets
// startup name the intent instead.
func MissingPrivileged(m Mask, appFlags uint64) []string {
	var missing []string
	for _, p := range privileged {
		if m&p.intent != 0 && appFlags&p.flags == 0 {
			missing = append(missing, p.name)
		}
	}
	return missing
}
