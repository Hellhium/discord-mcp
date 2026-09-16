// Package config loads the single YAML file that drives discord-mcp: the
// listen address, direct authentication, Discord client settings, and the
// instances — each one a set of client tokens mapped to exactly one bot or one
// webhook.
//
// The file is parsed once at startup. Unknown keys are errors, defaults are
// applied, and validation runs before anything is built, so everything
// downstream receives a complete, valid value and never re-checks it.
//
// Every credential in the file is a Secret, which redacts itself in every
// rendering; call Reveal at the exact point the plaintext is needed.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Defaults applied by Parse to absent (or zero) values.
const (
	DefaultListen     = ":8080"
	DefaultCacheTTL   = 10 * time.Minute
	DefaultTimeout    = 15 * time.Second
	DefaultBufferSize = 1000
)

// Duration is a time.Duration that unmarshals from a YAML string like "30s".
type Duration time.Duration

// UnmarshalYAML parses a Go duration string.
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return fmt.Errorf("duration must be a string like \"30s\": %w", err)
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

// Duration returns the value as a time.Duration.
func (d Duration) Duration() time.Duration { return time.Duration(d) }

// Secret is a credential read from the config. Every rendering method redacts
// it, so an accidental %v of a config struct, a log line or a re-serialised
// config cannot leak it.
type Secret string

const redacted = "[redacted]"

// Reveal returns the plaintext. This is the only way to read a Secret.
func (s Secret) Reveal() string { return string(s) }

// String renders "[redacted]", or "" when unset.
func (s Secret) String() string {
	if s == "" {
		return ""
	}
	return redacted
}

// GoString redacts under %#v.
func (s Secret) GoString() string { return `"` + s.String() + `"` }

// MarshalYAML redacts when a config struct is re-serialised.
func (s Secret) MarshalYAML() (any, error) { return s.String(), nil }

// MarshalJSON redacts when a config struct is serialised to JSON.
func (s Secret) MarshalJSON() ([]byte, error) { return []byte(`"` + s.String() + `"`), nil }

// Config is the whole file.
type Config struct {
	Server     Server     `yaml:"server"`
	DirectAuth DirectAuth `yaml:"direct_auth"`
	Discord    Discord    `yaml:"discord"`
	Instances  []Instance `yaml:"instances"`
}

// Server holds listener settings.
type Server struct {
	Listen string `yaml:"listen"`
}

// DirectAuth controls accepting a Discord bot token or webhook in place of a
// config token.
type DirectAuth struct {
	Enabled  bool     `yaml:"enabled"`
	CacheTTL Duration `yaml:"cache_ttl"`
}

// Discord holds REST client settings shared by every principal.
type Discord struct {
	Timeout Duration `yaml:"timeout"`
}

// Instance maps client tokens to exactly one bot or one webhook.
type Instance struct {
	Name    string       `yaml:"name"`
	Auth    InstanceAuth `yaml:"auth"`
	Bot     *Bot         `yaml:"bot"`
	Webhook *Webhook     `yaml:"webhook"`
}

// InstanceAuth lists the tokens clients present (Bearer or X-API-Key).
type InstanceAuth struct {
	Tokens []Secret `yaml:"tokens"`
}

// Bot is a bot account. Events, when set, opens a Gateway session.
type Bot struct {
	Token  Secret  `yaml:"token"`
	Events *Events `yaml:"events"`
}

// Events configures the Gateway session and its buffer. BufferSize is a
// pointer so an explicit 0 is rejected rather than silently defaulted.
type Events struct {
	Intents    []string `yaml:"intents"`
	BufferSize *int     `yaml:"buffer_size"`
}

// Webhook is a webhook. The URL embeds the webhook token, so it is a Secret.
type Webhook struct {
	URL Secret `yaml:"url"`
}

// Load reads and parses the config file at path.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	return Parse(b)
}

// Parse decodes, applies defaults and validates a config document.
func Parse(b []byte) (*Config, error) {
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	var c Config
	if err := dec.Decode(&c); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("config is empty")
		}
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if err := c.validateDurations(); err != nil {
		return nil, err
	}
	c.applyDefaults()
	if err := c.validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) applyDefaults() {
	if c.Server.Listen == "" {
		c.Server.Listen = DefaultListen
	}
	if c.DirectAuth.CacheTTL == 0 {
		c.DirectAuth.CacheTTL = Duration(DefaultCacheTTL)
	}
	if c.Discord.Timeout == 0 {
		c.Discord.Timeout = Duration(DefaultTimeout)
	}
	for i := range c.Instances {
		if b := c.Instances[i].Bot; b != nil && b.Events != nil && b.Events.BufferSize == nil {
			size := DefaultBufferSize
			b.Events.BufferSize = &size
		}
	}
}
