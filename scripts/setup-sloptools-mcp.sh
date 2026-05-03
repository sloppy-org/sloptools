#!/usr/bin/env bash
# Register sloptools as a stdio MCP server with every supported coding agent
# present on PATH (claude, codex, opencode, qwen). Each per-tool installer
# is a no-op if its CLI isn't installed.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

"$SCRIPT_DIR/setup-claude-mcp.sh"
"$SCRIPT_DIR/setup-codex-mcp.sh"
"$SCRIPT_DIR/setup-opencode-mcp.sh"
"$SCRIPT_DIR/setup-qwen-mcp.sh"
"$SCRIPT_DIR/setup-gemini-mcp.sh"

echo "sloptools MCP (stdio) wired into all coding agents that are installed"
echo "for long-lived local runtime use, install the separate user service via scripts/install-sloptools-user-unit.sh"
