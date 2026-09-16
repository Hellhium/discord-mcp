package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Hellhium/discord-mcp/internal/credential"
	"github.com/Hellhium/discord-mcp/internal/discord"
)

// Headers a credential is read from.
const (
	HeaderAuthorization = "Authorization"
	HeaderAPIKey        = "X-API-Key"
)

// Credential reads the client credential: a well-formed Authorization: Bearer
// header wins, otherwise X-API-Key. header names where it was found.
func Credential(r *http.Request) (value, header string, ok bool) {
	const bearer = "bearer "
	if h := r.Header.Get(HeaderAuthorization); len(h) >= len(bearer) && strings.EqualFold(h[:len(bearer)], bearer) {
		if v := strings.TrimSpace(h[len(bearer):]); v != "" {
			return v, HeaderAuthorization, true
		}
	}
	if v := strings.TrimSpace(r.Header.Get(HeaderAPIKey)); v != "" {
		return v, HeaderAPIKey, true
	}
	return "", "", false
}

// Rejection is why a credential was refused. It is logged, never sent to the
// client: every rejection gets the same 401.
type Rejection string

const (
	RejectMissing       Rejection = "missing_credential"
	RejectUnknown       Rejection = "unknown_token"
	RejectDiscord       Rejection = "discord_rejected"
	RejectRecentFailure Rejection = "recently_rejected"
	RejectUnavailable   Rejection = "discord_unavailable"
)

const defaultFailureTTL = time.Minute

// DirectOptions configures direct authentication.
type DirectOptions struct {
	Enabled bool
	// CacheTTL is how long a verified credential stays cached without use.
	CacheTTL time.Duration
	// FailureTTL is how long a credential Discord rejected is refused without
	// asking Discord again (default one minute). Discord bans an IP after
	// 10,000 invalid requests in 10 minutes; a client retrying a bad token
	// must not get the server banned.
	FailureTTL time.Duration
	Discord    discord.Options
}

// InstanceEntry maps config tokens to a prebuilt principal.
type InstanceEntry struct {
	Tokens    []string
	Principal *Principal
}

type indexed struct {
	token string
	p     *Principal
}

type cached struct {
	p        *Principal
	lastUsed time.Time
}

// Resolver resolves credentials. It is safe for concurrent use.
type Resolver struct {
	byHash map[[sha256.Size]byte]indexed
	direct DirectOptions
	now    func() time.Time

	mu        sync.Mutex
	ok        map[[sha256.Size]byte]*cached
	bad       map[[sha256.Size]byte]time.Time
	lastSweep time.Time
}

// NewResolver indexes config tokens by SHA-256. A token is never used as a
// map key in plaintext, and a hash hit is confirmed in constant time.
func NewResolver(entries []InstanceEntry, direct DirectOptions) (*Resolver, error) {
	if direct.FailureTTL <= 0 {
		direct.FailureTTL = defaultFailureTTL
	}
	r := &Resolver{
		byHash: make(map[[sha256.Size]byte]indexed),
		direct: direct,
		now:    time.Now,
		ok:     make(map[[sha256.Size]byte]*cached),
		bad:    make(map[[sha256.Size]byte]time.Time),
	}
	for _, e := range entries {
		for _, tok := range e.Tokens {
			if tok == "" {
				return nil, errors.New("empty instance token")
			}
			h := sha256.Sum256([]byte(tok))
			if _, dup := r.byHash[h]; dup {
				return nil, fmt.Errorf("instance %q: token already used by another instance", e.Principal.Instance)
			}
			r.byHash[h] = indexed{token: tok, p: e.Principal}
		}
	}
	return r, nil
}

// Resolve returns the principal for cred, or the reason it was refused.
func (r *Resolver) Resolve(ctx context.Context, cred string) (*Principal, Rejection) {
	if cred == "" {
		return nil, RejectMissing
	}
	h := sha256.Sum256([]byte(cred))
	if in, ok := r.byHash[h]; ok && subtle.ConstantTimeCompare([]byte(cred), []byte(in.token)) == 1 {
		return in.p, ""
	}
	if !r.direct.Enabled {
		return nil, RejectUnknown
	}
	wh, isWebhook := credential.ParseWebhook(cred)
	if !isWebhook && !credential.IsBotToken(cred) {
		return nil, RejectUnknown
	}
	key := h
	if isWebhook {
		// URL and id/token forms of one webhook share an entry.
		key = sha256.Sum256([]byte("webhook:" + wh.ID + "/" + wh.Token))
	}

	now := r.now()
	r.mu.Lock()
	r.sweepLocked(now)
	if c, ok := r.ok[key]; ok && now.Sub(c.lastUsed) <= r.direct.CacheTTL {
		c.lastUsed = now
		r.mu.Unlock()
		return c.p, ""
	}
	if t, ok := r.bad[key]; ok && now.Sub(t) < r.direct.FailureTTL {
		r.mu.Unlock()
		return nil, RejectRecentFailure
	}
	r.mu.Unlock()

	p, rej := r.verify(ctx, cred, wh, isWebhook, key)

	r.mu.Lock()
	defer r.mu.Unlock()
	switch rej {
	case "":
		if c, ok := r.ok[key]; ok { // a concurrent request verified it first
			c.lastUsed = now
			return c.p, ""
		}
		r.ok[key] = &cached{p: p, lastUsed: now}
	case RejectDiscord:
		r.bad[key] = now
	}
	return p, rej
}

func (r *Resolver) verify(ctx context.Context, cred string, wh credential.Webhook, isWebhook bool, key [sha256.Size]byte) (*Principal, Rejection) {
	hash := "sha256:" + hex.EncodeToString(key[:4])
	if isWebhook {
		c := discord.NewWebhook(r.direct.Discord)
		if _, err := c.VerifyWebhook(ctx, wh); err != nil {
			return nil, classify(err)
		}
		return &Principal{Kind: KindDirectWebhook, Capability: CapWebhook, CredentialHash: hash, Client: c, Webhook: wh, invalidate: r.invalidator(key)}, ""
	}
	c := discord.NewBot(cred, r.direct.Discord)
	id, err := c.VerifyBot(ctx)
	if err != nil {
		return nil, classify(err)
	}
	return &Principal{Kind: KindDirectBot, Capability: CapBot, CredentialHash: hash, Client: c, BotUserID: id.UserID, invalidate: r.invalidator(key)}, ""
}

// classify separates "Discord says this credential is invalid" (cached) from
// "Discord could not answer" (not cached).
func classify(err error) Rejection {
	var ae *discord.APIError
	if errors.As(err, &ae) && (ae.Status == http.StatusUnauthorized || ae.Status == http.StatusForbidden || ae.Status == http.StatusNotFound) {
		return RejectDiscord
	}
	return RejectUnavailable
}

func (r *Resolver) invalidator(key [sha256.Size]byte) func() {
	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		delete(r.ok, key)
		r.bad[key] = r.now()
	}
}

// sweepLocked drops expired entries at most once per FailureTTL.
func (r *Resolver) sweepLocked(now time.Time) {
	if now.Sub(r.lastSweep) < r.direct.FailureTTL {
		return
	}
	r.lastSweep = now
	for k, c := range r.ok {
		if now.Sub(c.lastUsed) > r.direct.CacheTTL {
			delete(r.ok, k)
		}
	}
	for k, t := range r.bad {
		if now.Sub(t) >= r.direct.FailureTTL {
			delete(r.bad, k)
		}
	}
}
