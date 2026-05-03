#!/usr/bin/env bash
# Register sloptools as a stdio MCP server in Gemini CLI.
set -euo pipefail

SLOPTOOLS_BIN="${SLOPTOOLS_BIN:-$(command -v sloptools 2>/dev/null || echo "")}"
DATA_DIR="${SLOPTOOLS_DATA_DIR:-${HOME}/.local/share/sloppy}"
PROJECT_DIR="${SLOPTOOLS_PROJECT_DIR:-${HOME}}"
SERVER_NAME="${SLOPTOOLS_MCP_NAME:-sloppy}"
VAULT_CONFIG="${SLOPTOOLS_VAULT_CONFIG:-${HOME}/.config/sloptools/vaults.toml}"

if [[ -z "${SLOPTOOLS_BIN}" ]]; then
  echo "sloptools binary not found. Install it first or set SLOPTOOLS_BIN." >&2
  exit 1
fi
if [[ ! -x "${SLOPTOOLS_BIN}" ]]; then
  echo "not executable: ${SLOPTOOLS_BIN}" >&2
  exit 1
fi

if ! command -v gemini >/dev/null 2>&1; then
  echo "gemini CLI not found; skipping" >&2
  exit 0
fi

mkdir -p "${DATA_DIR}"

gemini mcp remove "${SERVER_NAME}" >/dev/null 2>&1 || true
gemini mcp add "${SERVER_NAME}" "${SLOPTOOLS_BIN}" mcp-server   --stdio --vault-config "${VAULT_CONFIG}" --project-dir "${PROJECT_DIR}" --data-dir "${DATA_DIR}"
echo "registered ${SERVER_NAME} with gemini: ${SLOPTOOLS_BIN} mcp-server"
echo "  project-dir: ${PROJECT_DIR}"
echo "  data-dir:    ${DATA_DIR}"
echo "  vault-config:${VAULT_CONFIG}"
