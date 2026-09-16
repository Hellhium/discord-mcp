package intents

import (
	"reflect"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	m, err := Parse([]string{"guilds", "guild_messages", "message_content", "guild_messages"})
	if err != nil {
		t.Fatal(err)
	}
	if want := Mask(1<<0 | 1<<9 | 1<<15); m != want {
		t.Fatalf("Parse = %b, want %b", m, want)
	}
}

func TestParseUnknown(t *testing.T) {
	_, err := Parse([]string{"guilds", "guild_mesages"})
	if err == nil || !strings.Contains(err.Error(), `"guild_mesages"`) {
		t.Fatalf("want error naming the unknown intent, got %v", err)
	}
}

func TestPrivilegedMask(t *testing.T) {
	// Privileged must be the OR of all privileged intents, so there is one source of truth.
	var computed Mask
	for _, p := range []struct {
		intent Mask
	}{
		{1 << 1},  // guild_members
		{1 << 8},  // guild_presences
		{1 << 15}, // message_content
	} {
		computed |= p.intent
	}
	if Privileged != computed {
		t.Errorf("Privileged = %b, want %b", Privileged, computed)
	}
}

func TestMissingPrivileged(t *testing.T) {
	all := Mask(1<<1 | 1<<8 | 1<<15 | 1<<9)
	tests := []struct {
		name  string
		flags uint64
		want  []string
	}{
		{"none enabled", 0, []string{"guild_members", "guild_presences", "message_content"}},
		{"limited flags count", 1<<13 | 1<<15 | 1<<19, nil},
		{"full flags count", 1<<12 | 1<<14 | 1<<18, nil},
		{"only content", 1 << 18, []string{"guild_members", "guild_presences"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MissingPrivileged(all, tt.flags); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("MissingPrivileged = %v, want %v", got, tt.want)
			}
		})
	}
	if got := MissingPrivileged(Mask(1<<9), 0); got != nil {
		t.Fatalf("non-privileged mask: got %v, want nil", got)
	}
}
