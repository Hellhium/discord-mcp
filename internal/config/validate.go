package config

import (
	"errors"
	"fmt"

	"github.com/Hellhium/discord-mcp/internal/credential"
	"github.com/Hellhium/discord-mcp/internal/intents"
)

func (c *Config) validateDurations() error {
	if c.DirectAuth.CacheTTL < 0 {
		return errors.New("direct_auth.cache_ttl must be positive")
	}
	if c.Discord.Timeout < 0 {
		return errors.New("discord.timeout must be positive")
	}
	return nil
}

// validate checks everything that can be checked without Discord. Errors name
// the instance and the key, never a credential value.
func (c *Config) validate() error {
	if len(c.Instances) == 0 && !c.DirectAuth.Enabled {
		return errors.New("no instances configured and direct_auth is disabled: no client could authenticate")
	}
	names := make(map[string]bool)
	owners := make(map[string]string) // token plaintext -> instance name; local to this call
	for i, in := range c.Instances {
		if in.Name == "" {
			return fmt.Errorf("instances[%d]: name is required", i)
		}
		if names[in.Name] {
			return fmt.Errorf("instances[%d]: duplicate instance name %q", i, in.Name)
		}
		names[in.Name] = true
		where := fmt.Sprintf("instance %q", in.Name)

		if len(in.Auth.Tokens) == 0 {
			return fmt.Errorf("%s: auth.tokens must list at least one token", where)
		}
		for _, tok := range in.Auth.Tokens {
			if tok == "" {
				return fmt.Errorf("%s: auth.tokens contains an empty token", where)
			}
			if prev, ok := owners[tok.Reveal()]; ok {
				return fmt.Errorf("%s: an auth token is also used by instance %q", where, prev)
			}
			owners[tok.Reveal()] = in.Name
		}

		if (in.Bot == nil) == (in.Webhook == nil) {
			return fmt.Errorf("%s: exactly one of bot or webhook is required", where)
		}
		if in.Bot != nil {
			if !credential.IsBotToken(in.Bot.Token.Reveal()) {
				return fmt.Errorf("%s: bot.token is missing or is not a Discord bot token", where)
			}
			if ev := in.Bot.Events; ev != nil {
				if len(ev.Intents) == 0 {
					return fmt.Errorf("%s: bot.events.intents must list at least one intent", where)
				}
				if _, err := intents.Parse(ev.Intents); err != nil {
					return fmt.Errorf("%s: bot.events: %w", where, err)
				}
				if *ev.BufferSize < 1 {
					return fmt.Errorf("%s: bot.events.buffer_size must be at least 1", where)
				}
			}
			continue
		}
		if _, ok := credential.ParseWebhook(in.Webhook.URL.Reveal()); !ok {
			return fmt.Errorf("%s: webhook.url is missing or is not a Discord webhook URL", where)
		}
	}
	return nil
}
