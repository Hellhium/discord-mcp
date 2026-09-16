# discord-mcp

An [MCP](https://modelcontextprotocol.io) server that lets an LLM drive Discord
through a bot account or a webhook.

- **Discord is the permission system.** The server adds no filtering of its own:
  whatever the bot's roles (or the webhook) allow, the LLM can do.
- **Every action is audited.** Each tool call writes one JSON line to stdout —
  who, which tool, which IDs, the arguments, the Discord calls made and the
  outcome. Credentials never appear in it.
- **Full API reach.** About thirty typed tools cover assistant work (messages,
  threads, channels, members, roles, moderation, search, DMs), and
  `discord_request` reaches every other REST endpoint.
- **Two modes.** Bot callers get the bot tools (plus live events for bots
  configured with `events`); webhook callers only get webhook tools.

## Getting started

You need a Discord bot token (Developer Portal → your application → Bot) or a
webhook URL (channel settings → Integrations → Webhooks), and either Go 1.26+
or Docker.

### 1. Write a config

Start from [`config.example.yaml`](config.example.yaml). The minimum:

```yaml
instances:
  - name: assistant
    auth:
      tokens: ["PASTE-A-GENERATED-TOKEN-HERE"]
    bot:
      token: "YOUR-DISCORD-BOT-TOKEN"
```

Each instance maps the tokens your MCP clients present to exactly one `bot` or
one `webhook`. Generate client tokens yourself:

```bash
openssl rand -base64 32 | tr -d '=+/'
```

Treat the config as a secret (`chmod 600`) and keep it out of git and images.

To receive live events, add `events` to a bot:

```yaml
    bot:
      token: "YOUR-DISCORD-BOT-TOKEN"
      events:
        intents: [guilds, guild_messages, direct_messages, message_content]
```

Privileged intents (`message_content`, `guild_members`, `guild_presences`) must
be enabled in the Developer Portal; startup fails naming any that are not.

### 2. Run it

```bash
make build
./discord-mcp -config config.yaml
```

or with Docker:

```bash
docker run --rm -p 8080:8080 \
  -v "$PWD/config.yaml:/etc/discord-mcp/config.yaml:ro" \
  ghcr.io/hellhium/discord-mcp:latest
```

At startup every bot token and webhook in the config is checked against
Discord, and bots with events connect to the Gateway; any failure stops the
server with a message naming the instance. `-verify-credentials=false` skips
this (event buffers then stay empty).

### 3. Check it is up

```bash
curl -s http://localhost:8080/healthz     # ok
```

### 4. Connect an MCP client

MCP is served over Streamable HTTP at `POST /mcp`. Send the token in
`Authorization: Bearer …`, or in `X-API-Key: …` for clients that cannot set an
Authorization header.

**Claude Code:**

```bash
claude mcp add --transport http discord http://localhost:8080/mcp \
  --header "Authorization: Bearer $TOKEN"
```

**JSON-configured clients** (`.mcp.json`, Cursor, VS Code …):

```json
{
  "mcpServers": {
    "discord": {
      "type": "http",
      "url": "http://localhost:8080/mcp",
      "headers": { "Authorization": "Bearer PASTE-A-GENERATED-TOKEN-HERE" }
    }
  }
}
```

**stdio-only clients** need a bridge such as
`npx -y mcp-remote http://localhost:8080/mcp --header "Authorization: Bearer …"`.

## Direct authentication

With `direct_auth.enabled: true`, a client may send a Discord credential in
place of a config token, in the same headers:

- a bot token → bot tools (no events);
- a webhook URL, or `<webhook id>/<webhook token>` → webhook tools.

Config tokens are always matched first. Anything else that is not shaped like a
Discord credential is refused without contacting Discord. A credential Discord
accepts is cached for `cache_ttl` since last use; one Discord rejects is refused
for a minute without asking again, so a retrying client cannot get the server's
IP banned by Discord.

## Tools

| Group | Tools |
|---|---|
| Discovery | `discord_get_me`, `discord_list_guilds`, `discord_get_guild`, `discord_list_channels`, `discord_get_channel` |
| Messages | `discord_read_messages`, `discord_get_message`, `discord_send_message`, `discord_edit_message`, `discord_delete_message`, `discord_pin_message`, `discord_add_reaction`, `discord_remove_reaction`, `discord_search_messages`, `discord_send_dm` |
| Threads & channels | `discord_create_thread`, `discord_list_active_threads`, `discord_create_channel`, `discord_edit_channel`, `discord_delete_channel` |
| Members & roles | `discord_list_members`, `discord_get_member`, `discord_search_members`, `discord_list_roles`, `discord_add_role`, `discord_remove_role`, `discord_timeout_member`, `discord_kick_member`, `discord_ban_member`, `discord_unban_member` |
| Everything else | `discord_request` — any REST endpoint, relative to `https://discord.com/api/v10` |
| Events (bots with `events`) | `discord_poll_events`, `discord_wait_for_message` |
| Webhook callers | `discord_webhook_get`, `discord_webhook_send`, `discord_webhook_get_message`, `discord_webhook_edit_message`, `discord_webhook_delete_message` |

Read tools accept `format: json` to get Discord's objects as-is. Tools that
change something accept `reason`, recorded in the server's Discord audit log.
Message tools accept `allow_mentions: false` to stop a message from pinging
anyone. Attachments are base64; the server never fetches URLs.

## Audit log

One JSON line per tool call on stdout:

```json
{"time":"2026-09-14T11:02:00Z","level":"INFO","msg":"action",
 "principal":{"kind":"instance","instance":"assistant"},
 "tool":"discord_send_message","outcome":"ok","duration_ms":142,
 "targets":{"channel_id":"123"},"args":{"channel_id":"123","content":"hello"},
 "discord":[{"method":"POST","route":"/channels/{channel_id}/messages","status":200,"rate_limited_ms":0}]}
```

`outcome` is `ok`, `invalid_args`, `discord_error`, `rate_limited` or
`credential_rejected`. Direct credentials appear as `"credential":"sha256:…"`.
Rejected requests log `auth_rejected` with the reason; Gateway connects,
resumes and reconnects are logged too.

## Operating notes

- **Events live in memory.** A restart empties the buffer; a cursor from before
  it is reported as a gap.
- **Run one replica per instance with events.** Two replicas would each hold
  their own Gateway and buffer. Instances without events are stateless.
- **Rate limits** are handled per credential; a call that would wait longer
  than `discord.timeout` fails with `retry_after`.
