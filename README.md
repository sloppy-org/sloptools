# sloptools

Core MCP server for the [sloppy.at](https://sloppy.at) product family. Provides
workspace, items, artifacts, actors, mail, calendar, contacts, tasks, brain,
handoffs, temp files, and canvas tools over the
[Model Context Protocol](https://modelcontextprotocol.io/).

There are exactly two external agent-facing MCP servers in the sloppy stack:

- `sloppy` = `sloptools mcp-server`
- `helpy` = `helpy mcp-stdio`

`slopshell` is the UI/runtime layer and is not an MCP server.

Docs:

- [`docs/groupware.md`](docs/groupware.md) — MCP tool reference and per-backend capability matrix.
- [`docs/vaults.md`](docs/vaults.md) — brain vault config schema.

## Install

Requires Go 1.24+.

```bash
go install github.com/sloppy-org/sloptools/cmd/sloptools@latest
```

Or from a checkout:

```bash
go build -o sloptools ./cmd/sloptools
```

## Run

```bash
sloptools mcp-server --stdio --vault-config ~/.config/sloptools/vaults.toml
                             # MCP over stdio (per-agent subprocess; default)

sloptools server \
  --project-dir "$HOME" \
  --data-dir "$HOME/.local/share/sloppy" \
  --mcp-unix-socket "$XDG_RUNTIME_DIR/sloppy/sloptools.sock"
                             # long-lived runtime daemon (Unix)

sloptools server \
  --project-dir "$HOME" \
  --data-dir "$HOME/.local/share/sloppy" \
  --mcp-host 127.0.0.1 --mcp-port 9420
                             # long-lived runtime daemon (Windows / TCP)
```

## Wire into coding agents

Registers `sloppy` with every CLI present on PATH (claude, codex, opencode,
qwen, gemini). Idempotent.

```bash
./scripts/setup-sloptools-mcp.sh
```

Per-tool scripts live next to it. Override defaults via `SLOPTOOLS_PROJECT_DIR`,
`SLOPTOOLS_DATA_DIR`, `SLOPTOOLS_MCP_NAME`.

## Install as a service

### Linux (systemd user)

```bash
./scripts/install-sloptools-user-unit.sh
```

Installs and starts `sloptools-runtime.service` on
`$XDG_RUNTIME_DIR/sloppy/sloptools.sock` with backend state in
`$HOME/.local/share/sloppy`.

### Windows (Scheduled Task, no admin)

```powershell
.\scripts\install-sloptools-windows-service.ps1
```

Registers a per-user Scheduled Task that starts `sloptools server` at logon
on `127.0.0.1:9420`, with data in `%LOCALAPPDATA%\sloppy`, and auto-restarts
on failure. No administrator rights, no third-party tools. Runs in the
current user's interactive session.

```powershell
.\scripts\install-sloptools-windows-service.ps1 -Uninstall
```

Override defaults with `-Name`, `-BinaryPath`, `-ProjectDir`, `-DataDir`,
`-Bind`, `-Port`.

## Security

- MCP stdio: no listening socket; subprocess inherits the spawning UID.
- Runtime daemon: private Unix socket (`0700` dir, `0600` socket) on Unix;
  loopback TCP on Windows. Non-loopback binds are blocked unless
  `--unsafe-public-mcp` is passed.

## License

MIT. See [LICENSE](LICENSE). Provided as-is, no warranty.
