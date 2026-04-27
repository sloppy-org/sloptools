# sloptools

Core MCP server for the [sloppy.at](https://sloppy.at) product family.

Provides domain tools for workspace management, items, artifacts, actors,
email, calendar, handoffs, temporary files, and canvas relay via the
[Model Context Protocol](https://modelcontextprotocol.io/).

## MCP Server (stdio only)

Sloptools speaks MCP over stdio. There is no listening port or socket — the
coding agent (Claude Code, Codex, opencode, qwen-code) spawns
`sloptools mcp-server` as a subprocess per session, the subprocess inherits
the spawning user's UID, and other local users cannot intercept anything.

Manual smoke test:

```bash
sloptools mcp-server --project-dir "$HOME" --data-dir "$HOME/.local/share/sloppy" < /dev/null
```

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

## Documentation

- [`docs/groupware.md`](docs/groupware.md) — MCP tool reference and per-backend capability matrix.

## License

MIT. See [LICENSE](LICENSE).

## Disclaimer

This software is provided as-is without warranty. Use at your own risk.
The authors are not responsible for any damages arising from its use.
