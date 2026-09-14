# discord-mcp — design

- **Date:** 2026-09-14
- **Status:** approved in brainstorming, pending spec review
- **Conventions template:** `references/loki-filtered-mcp`

## 1. Goal

An HTTP MCP server that lets an LLM drive Discord through a bot account or a webhook.

- **Discord is the permission system.** The server does not filter guilds, channels or actions:
  whatever the bot's roles (or the webhook) allow, the LLM can do. The server's job is to expose
  the API well and to **log every action**.
- **Primary use is assistant work** (read, summarize, answer, post, organize), but the **whole
  Discord REST API** must be reachable.
- **Two modes:** *bot* (REST tools, plus live events for configured bots) and *webhook* (webhook
  operations only).
- **Two ways to authenticate:** a token written in the config file, or — when enabled — a Discord
  bot token or webhook passed directly by the client.

### Non-goals

- Voice connections.
- Server-side scoping or permission rules on top of Discord's.
- Persisting events across restarts.
- Interactions / slash commands (receiving them needs a public endpoint or Gateway handling that
  the assistant use case does not need; the long-tail tool can still manage command definitions).

## 2. Architecture

Go 1.26, `github.com/mark3labs/mcp-go` (Streamable HTTP), `github.com/bwmarrin/discordgo`,
`gopkg.in/yaml.v3`, `log/slog`. Layout mirrors the reference: thin `cmd/server/main.go`, logic in
`internal/`.

| Package | Responsibility |
|---|---|
| `internal/config` | Load and validate the YAML file into typed structs; `Secret` type that redacts itself in `String`, `GoString`, YAML and JSON. |
| `internal/auth` | Resolve a request credential into a `Principal`; success and failure caches. |
| `internal/discord` | REST client: `discordgo` session per bot token, webhook client, route guard for `discord_request`, Discord error decoding. |
| `internal/events` | Gateway session per configured bot with events enabled; bounded event buffer with cursors and waiters. |
| `internal/tools` | MCP tool definitions and handlers: bot tools, event tools, webhook tools, `discord_request`. |
| `internal/audit` | Middleware wrapping every tool handler; emits one JSON line per call. |
| `internal/router` | HTTP mux: `GET /healthz` (unauthenticated liveness), `POST /mcp`; auth middleware. |

### Request flow

```
POST /mcp
  → router: read credential (Authorization: Bearer, else X-API-Key)
  → auth.Resolve → Principal{kind, instance?, credential hash, discord client, events?}   (401 if unresolved)
  → MCP server selected by the Principal's capability set
  → tool handler (wrapped by audit) → discord client → Discord REST
```

### Capability sets and MCP servers

Three MCP servers are built once at startup, one per capability set:

| Capability set | Who gets it | Tools |
|---|---|---|
| `bot+events` | config instance with `bot.events` | bot tools + event tools + `discord_request` |
| `bot` | config instance with `bot` and no `events`; any direct bot token | bot tools + `discord_request` |
| `webhook` | config instance with `webhook`; any direct webhook | webhook tools |

- `tools/list` therefore shows exactly what the caller can use.
- Handlers **never** capture a Discord client at registration. They read the `Principal` from the
  request context (injected with mcp-go's `server.WithHTTPContextFunc`).
- Every HTTP request is authenticated on its own. A reused MCP session ID cannot carry one
  caller's Discord credential into another caller's request, because the credential comes from
  the current request only.

## 3. Configuration

```yaml
server:
  listen: ":8080"

direct_auth:
  enabled: false        # accept a Discord bot token / webhook in place of a config token
  cache_ttl: 10m        # how long a validated direct credential is cached (keyed by SHA-256)

discord:
  timeout: 15s          # per REST call, including rate-limit waits

instances:
  - name: assistant
    auth:
      tokens: ["<generated>"]
    bot:
      token: "<discord bot token>"
      events:                          # optional; absent = REST only
        intents: [guilds, guild_messages, guild_message_reactions, direct_messages, message_content]
        buffer_size: 1000

  - name: alerts
    auth:
      tokens: ["<generated>"]
    webhook:
      url: "https://discord.com/api/webhooks/<id>/<token>"
```

Defaults: `server.listen` `:8080`, `direct_auth.enabled` `false`, `direct_auth.cache_ttl` `10m`,
`discord.timeout` `15s`, `events.buffer_size` `1000`.

**Validation — the server refuses to start when:**

- a YAML key is unknown;
- an instance name is empty or duplicated;
- an instance has neither or both of `bot` and `webhook`;
- an `auth.tokens` list is empty, a token is empty, or a token appears in two instances;
- a bot token or webhook URL is missing or malformed;
- an intent name is unknown, or `buffer_size` < 1;
- with credential verification on (default), a bot token fails `GET /users/@me` or a webhook
  fails `GET /webhooks/{id}/{token}`;
- a bot with `events` cannot open its Gateway session; a disallowed privileged intent (close code
  4014) is reported by name.

`-verify-credentials=false` skips the Discord checks and the Gateway connection at startup (for
offline smoke tests); event tools then return empty results. Flags: `-config <path>` (required),
`-verify-credentials` (default `true`).

## 4. Authentication

### Resolution order

1. **Read the credential.** A well-formed `Authorization: Bearer <v>` wins; otherwise
   `X-API-Key: <v>`. None → reject.
2. **Config token.** Constant-time comparison against every config token. Match → **instance**
   principal (capability set from the instance).
3. **Direct auth disabled** → reject.
4. **Webhook shape.** Either a full webhook URL — `https://` host `discord.com`,
   `discordapp.com`, `ptb.discord.com` or `canary.discord.com`, path
   `/api[/v<n>]/webhooks/<snowflake>/<token>` — or `<snowflake>/<token>`. Validated with
   `GET /webhooks/{id}/{token}`. OK → **direct webhook** principal.
5. **Bot token shape.** Three base64url segments separated by dots
   (`^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$`, each segment non-empty). Validated with
   `GET /users/@me` using `Authorization: Bot <token>`. OK → **direct bot** principal
   (capability set `bot`).
6. **Anything else** → reject without contacting Discord.

### Caches

- **Success cache:** keyed by SHA-256 of the credential, holding the principal and its
  `discordgo` session. Entry expires after `direct_auth.cache_ttl` without use. One session per
  token keeps discordgo's rate-limit buckets correct per token.
- **Failure cache:** a credential Discord rejected is remembered for 1 minute and rejected
  without calling Discord again. Discord bans an IP (Cloudflare) after 10,000 invalid
  401/403/429 requests in 10 minutes; a client retrying a bad token must not get the server
  banned.
- **Revocation:** a 401 from Discord during a tool call evicts the credential from the success
  cache; the tool returns `credential_rejected`.

### Rejection

- Always the same `401` response body, whatever the reason.
- An `auth_rejected` audit line records the reason and the header name, never the value.
- Neither `Authorization` nor `X-API-Key` is ever forwarded to Discord; Discord calls carry only
  the principal's bot token or webhook token.

## 5. Tools

Naming: all tools are prefixed `discord_`. IDs are snowflake strings, validated as decimal digits
before any Discord call.

### Shared behaviour

- **`format: text|json`** on read tools. `text` (default) is compact for the LLM, e.g.
  `[2026-09-14 11:02] alice (1234…): message`; `json` returns Discord's object as-is.
- **`reason`** (optional) on every mutating tool, sent as `X-Audit-Log-Reason` (URL-encoded) so
  the action appears in Discord's own audit log.
- **MCP annotations:** `readOnlyHint` on read tools, `destructiveHint` on delete, kick, ban,
  remove-role, timeout, and on `discord_request` (which may mutate).
- **`allow_mentions`** (boolean, default `true`) on `send_message`, `send_dm`, `edit_message`,
  `webhook_send`, `webhook_edit_message`. `true` or omitted: no `allowed_mentions` is sent, Discord
  parses `@everyone`, `@here`, roles and users normally. `false`: `allowed_mentions: {"parse": []}`,
  nothing pings and the text is unchanged.
- **Attachments** on `send_message`, `send_dm`, `webhook_send`: list of
  `{filename, content_base64, description?}`. Base64 only; the server never fetches URLs (SSRF).
- **Embeds** accepted as Discord embed objects (JSON) on the send/edit tools.

### Bot tools (capability sets `bot`, `bot+events`)

| Group | Tool | Notes |
|---|---|---|
| Discovery | `discord_get_me` | Bot user. |
| | `discord_list_guilds` | Guilds the bot is in. |
| | `discord_get_guild` | Guild info with approximate counts. |
| | `discord_list_channels` | Channels of a guild: type, name, topic, parent category, position. |
| | `discord_get_channel` | One channel or thread. |
| Messages | `discord_read_messages` | `channel_id`, `limit` ≤ 100, one of `before`/`after`/`around`. |
| | `discord_get_message` | |
| | `discord_send_message` | `content`, `embeds`, `attachments`, `reply_to`, `allow_mentions`. |
| | `discord_edit_message` | |
| | `discord_delete_message` | |
| | `discord_pin_message` | `pinned: true|false`, on `/channels/{id}/messages/pins/{message.id}`. |
| | `discord_add_reaction` | Unicode emoji or `name:id`. |
| | `discord_remove_reaction` | Own reaction, or a given user's. |
| | `discord_search_messages` | `GET /guilds/{guild.id}/messages/search`: content, channel, author, `limit` ≤ 25, `offset` ≤ 9975; a `202` (guild still being indexed) is reported with its `retry_after`. |
| Threads | `discord_create_thread` | From a message or standalone. |
| | `discord_list_active_threads` | Per guild. |
| Channels | `discord_create_channel` | |
| | `discord_edit_channel` | Includes archive/lock for threads. |
| | `discord_delete_channel` | |
| Members & roles | `discord_list_members` | Paginated; needs the `GUILD_MEMBERS` privileged intent in the portal. |
| | `discord_get_member` | |
| | `discord_search_members` | By name prefix. |
| | `discord_list_roles` | |
| | `discord_add_role` | |
| | `discord_remove_role` | |
| Moderation | `discord_timeout_member` | Duration ≤ 28 days; `0` clears. |
| | `discord_kick_member` | |
| | `discord_ban_member` | Optional delete-message window. |
| | `discord_unban_member` | |
| DMs | `discord_send_dm` | Opens the DM channel, then sends. |
| Long tail | `discord_request` | See below. |

Message search was verified during planning: the endpoint is documented, needs
`READ_MESSAGE_HISTORY`, and its results are limited by the `MESSAGE_CONTENT` intent.

### `discord_request`

Arguments: `method` (`GET|POST|PUT|PATCH|DELETE`), `route`, `query` (object), `body` (JSON),
`reason`.

- `route` must start with `/`, must not contain `://`, `\`, `?`, `#`, a `..` segment (also
  percent-encoded), or `//`. Anything else → `invalid_args`, nothing sent.
- The request always goes to `https://discord.com/api/v10` + route, with the principal's bot
  token. The host is never taken from arguments.
- JSON bodies only (no multipart).
- Returns HTTP status and the JSON response, truncated past 64 KB with a `truncated: true` flag.

### Event tools (capability set `bot+events`)

- **`discord_poll_events`** `(cursor?, types?, guild_id?, channel_id?, limit ≤ 100, format)` —
  events after `cursor` matching the filters, plus `next_cursor`. `gap: true` when the cursor is
  older than the oldest buffered event or from another boot. No cursor → the latest `limit`
  events.
- **`discord_wait_for_message`** `(channel_id?, author_id?, include_self = false, cursor?,
  timeout = 60s, max 300s)` — blocks until the first matching `MESSAGE_CREATE` after `cursor`
  (after "now" when absent). Returns the message and `next_cursor`. A timeout is a normal result
  (`{"message": null, "next_cursor": …}`), not an error. Cancelled when the client disconnects.

### Webhook tools (capability set `webhook`)

| Tool | Notes |
|---|---|
| `discord_webhook_get` | Webhook name, channel and guild IDs. |
| `discord_webhook_send` | `content`, `username`, `avatar_url`, `embeds`, `attachments`, `thread_id`, `allow_mentions`; sent with `wait=true`, returns the created message. |
| `discord_webhook_get_message` | Message previously sent by this webhook. |
| `discord_webhook_edit_message` | |
| `discord_webhook_delete_message` | |

### MCP server instructions

Each capability set has a fixed instructions string describing the mode and its tools. It never
contains a token, instance name, webhook URL or any config value.

## 6. Event buffer

- **Gateway:** one `discordgo` session per config bot with `events`, opened at startup with the
  configured intents. discordgo handles heartbeat, resume and reconnect.
- **Stored events:** every dispatch event delivered, except lifecycle ones (`READY`, `RESUMED`,
  and the `GUILD_CREATE` burst following `READY`). Each entry: server sequence number, type,
  receive time, guild ID, channel ID, author ID (when present), raw payload.
- **Bounded:** ring buffer of `buffer_size` entries; oldest entries are dropped.
- **Cursors:** `<boot-id>:<seq>`, `boot-id` random per process start. A cursor with another
  boot ID or older than the oldest entry yields `gap: true`.
- **Reconnect marker:** after a reconnect that was not a successful resume, a synthetic
  `gateway_reconnected` event is appended so readers know events may be missing.
- **Waiters:** appending an event broadcasts to waiting `wait_for_message` calls; each waiter
  re-checks its filter.
- **No per-client state:** the buffer is shared read-only; the cursor lives with the client.
- **Limits:** in memory only, lost on restart. An instance with `events` must run as a **single
  replica**. REST-only and webhook traffic is stateless and can scale horizontally.
- `/healthz` stays liveness only; Gateway state changes are logged.

## 7. Audit log

`log/slog` JSON handler on stdout. One `action` line per tool call, including reads and event
tools:

```json
{"time":"…","level":"INFO","msg":"action",
 "principal":{"kind":"instance","instance":"assistant"},
 "tool":"discord_send_message","outcome":"ok","duration_ms":142,
 "targets":{"guild_id":"…","channel_id":"…","message_id":"…"},
 "args":{"content":"…","allow_mentions":true,
         "attachments":[{"filename":"a.png","size":20480,"sha256":"…"}]},
 "discord":[{"method":"POST","route":"/channels/{channel_id}/messages","status":200,"rate_limited_ms":0}]}
```

- `principal.kind`: `instance`, `direct_bot` or `direct_webhook`. Direct principals carry
  `"credential":"sha256:<first 8 hex>"` instead of an instance name.
- `outcome`: `ok`, `invalid_args`, `discord_error`, `rate_limited`, `credential_rejected`.
  Level is `INFO` for `ok`, `WARN` otherwise.
- `discord` is a list: one entry per Discord call made by the tool, with the templated route.
- `args` are logged in full, except: attachment content is replaced by filename, size and
  SHA-256; `discord_request` bodies are truncated at 16 KB.
- Tokens and webhook tokens never appear anywhere in logs.
- `auth_rejected` lines: reason and header name.
- Gateway lifecycle (`gateway_connected`, `gateway_resumed`, `gateway_reconnected`,
  `gateway_disconnected`) logged with the instance name.

## 8. Errors and rate limits

| Situation | Result |
|---|---|
| Invalid arguments (snowflake, route, mutually exclusive params) | MCP tool error, `invalid_args`, nothing sent |
| Discord 4xx | Tool error with status, Discord code and message, e.g. `Discord 403: Missing Permissions (50013)`; `discord_error` |
| Discord 401 | Evict cached credential; `credential_rejected` |
| 429 | discordgo waits and retries per bucket; if the wait would exceed `discord.timeout`, tool error with `retry_after`; `rate_limited` |
| Discord 5xx / network | Tool error `Discord unavailable: …`; `discord_error` |

Startup and fatal errors propagate to `run()` and are wrapped with context (`fmt.Errorf("…: %w")`).
Graceful shutdown on SIGINT/SIGTERM closes the HTTP server, wakes waiters, and closes Gateway
sessions.

## 9. Testing

Table-driven tests next to the code, no network.

- **Fake Discord:** an `http.RoundTripper` that redirects `discord.com` requests to an
  `httptest.Server`, so handlers run through the real discordgo code.
- **config:** valid configs and every validation error in §3.
- **auth:** every credential shape (webhook URL host and version variants, `id/token`, bot tokens,
  garbage), config token winning over direct shapes, Bearer over X-API-Key, direct auth off,
  success cache, failure cache, revocation eviction, identical 401 bodies.
- **route guard:** hostile routes — `//evil.com`, `/../`, `%2e%2e`, `https://…`, backslashes,
  embedded `?` and `#`.
- **events:** wraparound, cursor parsing, `gap` on old cursor and on boot-ID mismatch, waiter
  match / timeout / cancellation, `include_self`, reconnect marker.
- **audit:** exact log lines via a buffer-backed slog handler; token redaction; attachment
  replacement; body truncation.
- **tools:** each handler against the fake Discord: request method, route, body,
  `X-Audit-Log-Reason`, `allowed_mentions` for both `allow_mentions` values, text and json output.
- **end to end:** router + MCP handshake + `tools/list` for each capability set; direct auth on
  and off.

## 10. Packaging

As in `CLAUDE.md` and the reference:

- `Makefile` (`build`, `test`, `vet`, `fmt`, `fmt-check`, `tidy`, `run`, `clean`), binary
  `discord-mcp`.
- Multi-stage `Dockerfile`: static `CGO_ENABLED=0` build on `$BUILDPLATFORM`, Alpine runtime with
  `ca-certificates`, `USER 65532:65532`, `HEALTHCHECK` on `/healthz`, config mounted read-only at
  `/etc/discord-mcp/config.yaml`, `version` via `-ldflags`.
- GitHub Actions: multi-arch image to GHCR; pull requests build amd64 only and run a smoke test
  (missing `-config` fails; `config.example.yaml` with `-verify-credentials=false` answers
  `/healthz`).
- `config.example.yaml` and a README with a Getting started guide (config, run, check, connect an
  MCP client with Bearer or X-API-Key, direct auth).
