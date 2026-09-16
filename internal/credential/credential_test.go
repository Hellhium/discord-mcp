package credential

import "testing"

func TestIsSnowflake(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"1", true},
		{"123456789012345678", true},
		{"12345678901234567890", true},
		{"123456789012345678901", false}, // 21 digits
		{"0123", false},
		{"", false},
		{"12a", false},
		{"-1", false},
		{"1 2", false},
		{"@me", false},
	}
	for _, tt := range tests {
		if got := IsSnowflake(tt.in); got != tt.want {
			t.Errorf("IsSnowflake(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestIsBotToken(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"MTIzNDU2Nzg5MDEyMzQ1Njc4.GAbCdE.abcdefghijklmnopqrstuvwxyz0123456789_-", true},
		{"a.b.c", true},
		{"a.b", false},
		{"a..c", false},
		{".b.c", false},
		{"a.b.c.d", false},
		{"a.b.c=", false},
		{"a b.c.d", false},
		{"", false},
		{"123/abc", false},
	}
	for _, tt := range tests {
		if got := IsBotToken(tt.in); got != tt.want {
			t.Errorf("IsBotToken(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestParseWebhook(t *testing.T) {
	const id, tok = "123456789012345678", "AbC-def_GHI123"
	tests := []struct {
		name string
		in   string
		ok   bool
	}{
		{"short form", id + "/" + tok, true},
		{"url discord.com", "https://discord.com/api/webhooks/" + id + "/" + tok, true},
		{"url with version", "https://discord.com/api/v10/webhooks/" + id + "/" + tok, true},
		{"url trailing slash", "https://discord.com/api/webhooks/" + id + "/" + tok + "/", true},
		{"url discordapp.com", "https://discordapp.com/api/webhooks/" + id + "/" + tok, true},
		{"url ptb", "https://ptb.discord.com/api/webhooks/" + id + "/" + tok, true},
		{"url canary", "https://canary.discord.com/api/webhooks/" + id + "/" + tok, true},
		{"url host case", "https://Discord.com/api/webhooks/" + id + "/" + tok, true},
		{"url with query ignored", "https://discord.com/api/webhooks/" + id + "/" + tok + "?wait=true", true},
		{"http scheme", "http://discord.com/api/webhooks/" + id + "/" + tok, false},
		{"other host", "https://evil.example/api/webhooks/" + id + "/" + tok, false},
		{"lookalike host", "https://discord.com.evil.example/api/webhooks/" + id + "/" + tok, false},
		{"host with port", "https://discord.com:8443/api/webhooks/" + id + "/" + tok, false},
		{"userinfo", "https://x@discord.com/api/webhooks/" + id + "/" + tok, false},
		{"extra path", "https://discord.com/api/webhooks/" + id + "/" + tok + "/messages/1", false},
		{"bad id", "abc/" + tok, false},
		{"empty token", id + "/", false},
		{"token with slash", id + "/" + tok + "/x", false},
		{"token with dot", id + "/a.b", false},
		{"no slash", id, false},
		{"empty", "", false},
		{"bot token", "a.b.c", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseWebhook(tt.in)
			if ok != tt.ok {
				t.Fatalf("ParseWebhook(%q) ok = %v, want %v", tt.in, ok, tt.ok)
			}
			if ok && (got.ID != id || got.Token != tok) {
				t.Fatalf("ParseWebhook(%q) = %+v, want id %s token %s", tt.in, got, id, tok)
			}
		})
	}
}
