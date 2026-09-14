# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project goal

**`discord-mcp`** — an **MCP server that controls a Discord bot**. An MCP client (an LLM) calls
tools such as reading channels or sending messages, and the server carries them out through a
Discord bot account.

The scope of the tool surface and the permission model are still to be designed. Whatever they
become, the guiding principle is the one from the reference project: **the operator decides the
scope in config, and the LLM cannot widen it** — no tool argument may select a different bot,
token, guild or channel outside what the config allows.

## `references/` — read-only, gitignored

`references/` is in `.gitignore` and is **not** part of this project. It holds upstream clones
kept purely as design references. Read them for patterns; never import from them or edit them.

- `references/loki-filtered-mcp` (`github.com/Hellhium/loki-filtered-mcp`) — the conventions
  template for this project: layout, config handling, MCP wiring, testing, Docker and CI. Key files:
  - `cmd/server/main.go` — `-config` flag, `run()` returning an error, one HTTP listener,
    graceful shutdown on SIGINT/SIGTERM, `version` injected via `-ldflags`.
  - `internal/config/config.go` — YAML loaded once into typed structs, validated at startup,
    `Secret` type that redacts itself in every rendering (`String`, `GoString`, YAML, JSON).
  - `internal/handlers/handlers.go` — MCP tool definitions/handlers with injected dependencies,
    no globals; nothing from config (credentials, IDs) is leaked into tool descriptions.
  - `internal/instance/instance.go` — builds the per-scope object graph at startup.
  - `Makefile`, `Dockerfile`, `.github/workflows/docker.yml`, `.claude/` — build, image, CI and
    Claude Code hooks to mirror.

## Architecture the code should follow

- **Language/toolchain: Go** (Go 1.26, as the reference). MCP via `github.com/mark3labs/mcp-go`.
  Discord via `github.com/bwmarrin/discordgo` unless a design decision says otherwise.
- **Transport: HTTP MCP.** Serve MCP over Streamable HTTP at `POST /mcp`, not stdio, plus an
  unauthenticated `GET /healthz`. One listener, one process.
- **Layout:** `cmd/server/main.go` is a thin entrypoint; all logic lives in `internal/<pkg>`.
  Keep concerns in distinct packages — config parsing, the Discord client wrapper, the MCP tool
  handlers, HTTP routing/auth — so each can be unit-tested without a live Discord or network.
- **Config is the source of truth.** A single YAML file (`-config path`) defines the bot token,
  listen address, client auth and the allowed scope. Parse it once at startup into typed structs,
  validate it, and refuse to start on anything ambiguous. Pass config values (not env vars or
  globals) into constructors. Use `gopkg.in/yaml.v3`. Ship a `config.example.yaml`; never
  commit a real `config.yaml`.
- **Secrets never leak.** The Discord bot token and client tokens use a redacting `Secret` type;
  call `Reveal()` only at the exact point the plaintext is needed. Never log a token, never put
  one (or any internal ID not meant for the client) in an MCP tool description or error message.
- **Dependency injection, no globals.** Structs are built once at startup and wired together
  explicitly; nothing reads process-wide mutable state at request time.
- **Errors:** return errors up to `run()`; wrap with context using `fmt.Errorf("...: %w", err)`.
  A tool failure is returned as an MCP tool error with a message the LLM can act on.
- **Comments:** every package has a package doc comment explaining its role and invariants.
  Comments explain *why* (a design choice, a security reason, an upstream quirk), not *what*.

## Commands

```bash
make build               # go build -o discord-mcp ./cmd/server
make test                # go test ./...
make vet                 # go vet ./...
make fmt / fmt-check     # gofmt -w . / fail on unformatted files
make run CONFIG=config.yaml
go test ./internal/pkg -run TestName   # run a single test
```

Claude Code hooks (`.claude/settings.json`) run `gofmt -w` after every edit to a `.go` file and
`go vet ./... && go test ./...` before a turn ends; a failure blocks the stop until fixed.

## Testing focus

- **Table-driven tests** next to the code (`foo_test.go` in the same package), using `t.Helper()`
  and `t.TempDir()` for config files.
- Fake Discord at the HTTP boundary (`httptest.Server`) rather than mocking deep internals, so
  handlers are tested through the same client code that runs in production.
- Test the scope invariant adversarially: tool arguments that try to reach a guild/channel outside
  the configured scope must be rejected with nothing sent to Discord.
- Config tests cover both valid configs and every validation error.

## Packaging

Mirror the reference: a multi-stage `Dockerfile` (static `CGO_ENABLED=0` build on
`$BUILDPLATFORM`, Alpine runtime, numeric non-root `USER 65532:65532`, `HEALTHCHECK` on
`/healthz`, config mounted read-only), and a GitHub Actions workflow building multi-arch images
to GHCR with a smoke test on pull requests.

## Git commits

Commits follow **[Conventional Commits 1.0.0](https://www.conventionalcommits.org/en/v1.0.0/)**:

```
<type>[optional scope]: <description>

[optional body]

[optional footer(s)]
```

- **Types:** `feat` (new feature → minor), `fix` (bug fix → patch), and also `build`, `chore`,
  `ci`, `docs`, `perf`, `refactor`, `revert`, `style`, `test`.
- **Scope** is optional, lowercase, usually the package or area: `feat(config): ...`,
  `fix(handlers): ...`, `ci(docker): ...`.
- **Description:** imperative mood, lowercase first letter, no trailing period, ≤ 72 chars
  (e.g. `feat(handlers): add send_message tool`).
- **Breaking changes:** append `!` after the type/scope (`feat(config)!: ...`) and/or add a
  `BREAKING CHANGE: <explanation>` footer.
- **Body** (after one blank line), wrapped at ~72 columns, in prose: explain the problem, *why*
  this change, the behavior before vs. after, and edge cases or security implications. Name
  concrete errors, tools and config keys. See `git -C references/loki-filtered-mcp log` for the
  expected depth of explanation.
- One logical change per commit; code, tests and docs for that change go together.
- **Never add a `Co-Authored-By:` trailer**, nor any other tool-attribution line
  ("Generated with …", etc.). Write the message as the human author's own. This is also enforced
  by `.claude/settings.local.json` (attribution disabled + a hook blocking such commits).
