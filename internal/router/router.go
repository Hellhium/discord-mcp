// Package router is the front door. A request is authenticated before it is
// routed: the reply to a missing or unknown credential is the same 401
// whatever path was asked for, so it reveals nothing about the server. An
// authenticated request reaches the MCP server for its principal's capability
// set with the principal in its context.
package router

import (
	"encoding/json"
	"net/http"

	"github.com/Hellhium/discord-mcp/internal/audit"
	"github.com/Hellhium/discord-mcp/internal/auth"
)

// Paths served. Everything else is 404 once authenticated.
const (
	HealthPath = "/healthz"
	MCPPath    = "/mcp"
)

// Servers maps each capability set to its MCP handler.
type Servers map[auth.Capability]http.Handler

// Router serves /healthz and /mcp.
type Router struct {
	res     *auth.Resolver
	servers Servers
	log     *audit.Logger
}

// New builds the router.
func New(res *auth.Resolver, servers Servers, log *audit.Logger) *Router {
	return &Router{res: res, servers: servers, log: log}
}

func (rt *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == HealthPath {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeJSON(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
		return
	}

	cred, header, ok := auth.Credential(r)
	if !ok {
		rt.log.AuthRejected(string(auth.RejectMissing), "")
		writeJSON(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	p, rej := rt.res.Resolve(r.Context(), cred)
	if rej != "" {
		rt.log.AuthRejected(string(rej), header)
		writeJSON(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	h, ok := rt.servers[p.Capability]
	if r.URL.Path != MCPPath || !ok {
		writeJSON(w, http.StatusNotFound, "unknown endpoint")
		return
	}
	h.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), p)))
}

func writeJSON(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": message})
}
