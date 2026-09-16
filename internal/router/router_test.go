package router

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Hellhium/discord-mcp/internal/audit"
	"github.com/Hellhium/discord-mcp/internal/auth"
)

const tokBot, tokHook = "bot-token-bot-token-bot-token-00", "hook-token-hook-token-hook-toke"

func setup(t *testing.T) (http.Handler, *bytes.Buffer) {
	t.Helper()
	bot := &auth.Principal{Kind: auth.KindInstance, Capability: auth.CapBot, Instance: "bot"}
	hook := &auth.Principal{Kind: auth.KindInstance, Capability: auth.CapWebhook, Instance: "hook"}
	res, err := auth.NewResolver([]auth.InstanceEntry{
		{Tokens: []string{tokBot}, Principal: bot},
		{Tokens: []string{tokHook}, Principal: hook},
	}, auth.DirectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	stub := func(name string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := auth.FromContext(r.Context())
			if !ok {
				t.Error("no principal in context")
				return
			}
			_, _ = io.WriteString(w, name+":"+p.Instance)
		})
	}
	var logs bytes.Buffer
	return New(res, Servers{auth.CapBot: stub("bot-server"), auth.CapWebhook: stub("webhook-server")}, audit.New(&logs)), &logs
}

func do(h http.Handler, method, path string, headers map[string]string) (int, string) {
	r := httptest.NewRequest(method, path, strings.NewReader("{}"))
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code, w.Body.String()
}

func TestHealthz(t *testing.T) {
	h, _ := setup(t)
	if code, body := do(h, "GET", HealthPath, nil); code != 200 || body != "ok\n" {
		t.Fatalf("GET healthz = %d %q", code, body)
	}
	if code, _ := do(h, "POST", HealthPath, nil); code != 405 {
		t.Fatalf("POST healthz = %d", code)
	}
}

func TestRejectionsAreIdentical(t *testing.T) {
	h, logs := setup(t)
	codeA, bodyA := do(h, "POST", MCPPath, nil)
	codeB, bodyB := do(h, "POST", MCPPath, map[string]string{"Authorization": "Bearer wrong"})
	codeC, bodyC := do(h, "POST", "/elsewhere", map[string]string{"X-API-Key": "wrong"})
	if codeA != 401 || codeB != 401 || codeC != 401 || bodyA != bodyB || bodyB != bodyC {
		t.Fatalf("rejections differ: %d %q / %d %q / %d %q", codeA, bodyA, codeB, bodyB, codeC, bodyC)
	}
	for _, want := range []string{`"reason":"missing_credential"`, `"reason":"unknown_token","header":"Authorization"`, `"header":"X-API-Key"`} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("logs missing %s:\n%s", want, logs.String())
		}
	}
	if strings.Contains(logs.String(), "wrong") {
		t.Fatal("rejected credential value was logged")
	}
}

func TestRoutesByCapability(t *testing.T) {
	h, _ := setup(t)
	if code, body := do(h, "POST", MCPPath, map[string]string{"Authorization": "Bearer " + tokBot}); code != 200 || body != "bot-server:bot" {
		t.Fatalf("bot = %d %q", code, body)
	}
	if code, body := do(h, "POST", MCPPath, map[string]string{"X-API-Key": tokHook}); code != 200 || body != "webhook-server:hook" {
		t.Fatalf("hook = %d %q", code, body)
	}
	if code, _ := do(h, "POST", "/other", map[string]string{"Authorization": "Bearer " + tokBot}); code != 404 {
		t.Fatalf("unknown path = %d", code)
	}
}
