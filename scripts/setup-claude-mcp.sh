#!/usr/bin/env bash
# Register sloptools as a stdio MCP server in Claude Code (user scope).
# No listening socket: Claude spawns `sloptools mcp-server` per session.
set -euo pipefail

SLOPTOOLS_BIN="${SLOPTOOLS_BIN:-$(command -v sloptools 2>/dev/null || echo "")}"
DATA_DIR="${SLOPTOOLS_DATA_DIR:-${HOME}/.local/share/sloppy}"
PROJECT_DIR="${SLOPTOOLS_PROJECT_DIR:-${HOME}}"
SERVER_NAME="${SLOPTOOLS_MCP_NAME:-sloppy}"

if [[ -z "${SLOPTOOLS_BIN}" ]]; then
  echo "sloptools binary not found. Install it first or set SLOPTOOLS_BIN." >&2
  exit 1
fi
if [[ ! -x "${SLOPTOOLS_BIN}" ]]; then
  echo "not executable: ${SLOPTOOLS_BIN}" >&2
  exit 1
fi

if ! command -v claude >/dev/null 2>&1; then
  echo "claude CLI not found; skipping" >&2
  exit 0
fi

mkdir -p "${DATA_DIR}"

claude mcp remove -s user "${SERVER_NAME}" >/dev/null 2>&1 || true
claude mcp add -s user "${SERVER_NAME}" -- "${SLOPTOOLS_BIN}" mcp-server \
  --project-dir "${PROJECT_DIR}" --data-dir "${DATA_DIR}"
echo "registered ${SERVER_NAME} with claude (user scope): ${SLOPTOOLS_BIN} mcp-server"
echo "  project-dir: ${PROJECT_DIR}"
echo "  data-dir:    ${DATA_DIR}"
