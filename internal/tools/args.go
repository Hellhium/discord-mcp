package tools

import (
	"fmt"
	"math"
	"strings"

	"github.com/Hellhium/discord-mcp/internal/credential"
	"github.com/Hellhium/discord-mcp/internal/discord"
)

// Args are a tool call's arguments as decoded JSON. Every accessor returns a
// *discord.ArgError the LLM can act on.
type Args map[string]any

func argErr(format string, a ...any) error {
	return &discord.ArgError{Msg: fmt.Sprintf(format, a...)}
}

func (a Args) present(key string) (any, bool) {
	v, ok := a[key]
	return v, ok && v != nil
}

// String returns an optional string ("" when absent).
func (a Args) String(key string) (string, error) {
	v, ok := a.present(key)
	if !ok {
		return "", nil
	}
	s, isStr := v.(string)
	if !isStr {
		return "", argErr("%s must be a string", key)
	}
	return s, nil
}

// RequiredString returns a non-empty string.
func (a Args) RequiredString(key string) (string, error) {
	s, err := a.String(key)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(s) == "" {
		return "", argErr("%s is required", key)
	}
	return s, nil
}

// ID returns a required Discord ID. IDs must be strings: JSON numbers lose
// precision above 2^53, which every modern snowflake exceeds.
func (a Args) ID(key string) (string, error) {
	v, ok := a.present(key)
	if !ok {
		return "", argErr("%s is required", key)
	}
	return checkID(key, v)
}

// OptionalID returns a Discord ID or "".
func (a Args) OptionalID(key string) (string, error) {
	v, ok := a.present(key)
	if !ok {
		return "", nil
	}
	if s, isStr := v.(string); isStr && s == "" {
		return "", nil
	}
	return checkID(key, v)
}

func checkID(key string, v any) (string, error) {
	s, isStr := v.(string)
	if !isStr {
		return "", argErr("%s must be a Discord ID passed as a string, e.g. \"123456789012345678\"", key)
	}
	if !credential.IsSnowflake(s) {
		return "", argErr("%s must be a Discord ID (digits only), got %q", key, s)
	}
	return s, nil
}

// Int returns an integer in [min, max], or def when absent.
func (a Args) Int(key string, def, min, max int) (int, error) {
	v, ok := a.present(key)
	if !ok {
		return def, nil
	}
	f, isNum := v.(float64)
	if !isNum || f != math.Trunc(f) {
		return 0, argErr("%s must be an integer", key)
	}
	if f < float64(min) || f > float64(max) {
		return 0, argErr("%s must be between %d and %d", key, min, max)
	}
	return int(f), nil
}

// Bool returns a boolean, or def when absent.
func (a Args) Bool(key string, def bool) (bool, error) {
	v, ok := a.present(key)
	if !ok {
		return def, nil
	}
	b, isBool := v.(bool)
	if !isBool {
		return false, argErr("%s must be true or false", key)
	}
	return b, nil
}

// Object returns a JSON object, or nil when absent.
func (a Args) Object(key string) (map[string]any, error) {
	v, ok := a.present(key)
	if !ok {
		return nil, nil
	}
	m, isObj := v.(map[string]any)
	if !isObj {
		return nil, argErr("%s must be an object", key)
	}
	return m, nil
}

// List returns a JSON array, or nil when absent.
func (a Args) List(key string) ([]any, error) {
	v, ok := a.present(key)
	if !ok {
		return nil, nil
	}
	l, isList := v.([]any)
	if !isList {
		return nil, argErr("%s must be an array", key)
	}
	return l, nil
}

// StringList returns an array of strings, or nil when absent.
func (a Args) StringList(key string) ([]string, error) {
	l, err := a.List(key)
	if err != nil || l == nil {
		return nil, err
	}
	out := make([]string, len(l))
	for i, v := range l {
		s, isStr := v.(string)
		if !isStr {
			return nil, argErr("%s must be an array of strings", key)
		}
		out[i] = s
	}
	return out, nil
}

// OneOf returns which of keys is set, "" when none, and an error when more
// than one is.
func (a Args) OneOf(keys ...string) (string, error) {
	found := ""
	for _, k := range keys {
		if v, ok := a.present(k); ok && v != "" {
			if found != "" {
				return "", argErr("set at most one of %s", strings.Join(keys, ", "))
			}
			found = k
		}
	}
	return found, nil
}
