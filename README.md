# sloptools

Core MCP server for the [sloppy.at](https://sloppy.at) product family.

Provides domain tools for workspace management, items, artifacts, actors,
email, calendar, handoffs, temporary files, and canvas relay via the
[Model Context Protocol](https://modelcontextprotocol.io/).

## Table of Contents

- [MCP Server](#mcp-server)
- [Wire Sloptools into Coding Agents](#wire-sloptools-into-coding-agents)
- [Harness integration](#harness-integration)
- [Documentation](#documentation)
- [License](#license)
- [Disclaimer](#disclaimer)

## MCP Server

The `mcp-server` subcommand speaks MCP over stdio. There is no listening port
or socket — the coding agent (Claude Code, Codex, opencode, qwen-code) spawns
`sloptools mcp-server` as a subprocess per session, the subprocess inherits
the spawning user's UID, and other local users cannot intercept anything.

Manual smoke test:

```bash
sloptools mcp-server --project-dir "$HOME" --data-dir "$HOME/.local/share/sloppy" < /dev/null
```

The separate `server` subcommand can expose the same MCP surface over a local
HTTP listener or a private Unix socket for embedded applications.

## Wire Sloptools into Coding Agents

One-shot installer (registers sloptools with whatever's on PATH — claude,
codex, opencode, qwen):

```bash
./scripts/setup-sloptools-mcp.sh
```

Per-tool installers:

- `scripts/setup-claude-mcp.sh` — `claude mcp add -s user sloppy -- sloptools mcp-server ...`
- `scripts/setup-codex-mcp.sh` — `codex mcp add sloppy -- sloptools mcp-server ...`
- `scripts/setup-opencode-mcp.sh` — writes `mcp.sloppy = {type: "local", command: [...]}` into `~/.config/opencode/opencode.json`
- `scripts/setup-qwen-mcp.sh` — writes `mcpServers.sloppy = {command: "sloptools", args: [...]}` into `~/.qwen/settings.json`

Each per-tool installer is idempotent and a no-op if its CLI isn't installed.
Defaults: project dir `$HOME`, data dir `$HOME/.local/share/sloppy`,
server name `sloppy`. Override via `SLOPTOOLS_PROJECT_DIR`,
`SLOPTOOLS_DATA_DIR`, `SLOPTOOLS_MCP_NAME`.

## Harness integration

This section documents the canonical launch command and per-harness
configuration so every harness wires sloptools the same way.

### Canonical launch command

All stdio harnesses launch the same command shape:

```bash
sloptools mcp-server --stdio --vault-config ~/.config/sloptools/vaults.toml
```

The `mcp-server` subcommand uses stdio by default; `--stdio` is accepted so
all harness snippets can be explicit. `--vault-config` sets the default brain
vault config for MCP calls. Individual brain tools can still override it with
their `config_path` argument. The repo ships zero defaults pointing at a real
vault path. See the [vaults.toml schema reference](docs/vaults.md) for the
user-owned config shape.

### Claude Code

Add the following stanza to `~/.claude/.mcp.json` (create the file if it
doesn't exist):

```jsonc
{
  "mcpServers": {
    "sloptools": {
      "command": "sloptools",
      "args": ["mcp-server", "--stdio", "--vault-config", "~/.config/sloptools/vaults.toml"]
    }
  }
}
```

After installation, call `mcp__sloptools__brain.vault.validate` with
`sphere` and, if needed, `config_path`.

### opencode

Use `scripts/setup-opencode-mcp.sh`. It writes a local MCP entry to the
configured opencode JSON file with this command array:

```json
["sloptools", "mcp-server", "--stdio", "--vault-config", "~/.config/sloptools/vaults.toml"]
```

### codex

Use `scripts/setup-codex-mcp.sh`. It registers the same stdio command through
`codex mcp add`.

### slopshell

For embedded Slopshell use, run the HTTP MCP server on a private Unix socket
with `sloptools server --mcp-unix-socket "$XDG_RUNTIME_DIR/sloppy/mcp.sock"`.
The socket is created with mode `0600`; no manual stdio registration is needed
inside Slopshell.

## Documentation

- [`docs/groupware.md`](docs/groupware.md) — MCP tool reference and per-backend capability matrix.
- [`docs/vaults.md`](docs/vaults.md) — brain vault config schema.

## License

MIT. See [LICENSE](LICENSE).

## Disclaimer

This software is provided as-is without warranty. Use at your own risk.
The authors are not responsible for any damages arising from its use.
