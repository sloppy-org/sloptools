#!/usr/bin/env bash
# Register sloptools as a stdio MCP server in qwen-code.
set -euo pipefail

SETTINGS_PATH="${QWEN_SETTINGS_PATH:-${HOME}/.qwen/settings.json}"
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

mkdir -p "$(dirname "${SETTINGS_PATH}")" "${DATA_DIR}"

python3 - "${SETTINGS_PATH}" "${SLOPTOOLS_BIN}" "${PROJECT_DIR}" "${DATA_DIR}" "${SERVER_NAME}" <<'PY'
import json
import sys
from pathlib import Path

path = Path(sys.argv[1])
bin_, project_dir, data_dir, name = sys.argv[2:6]

if path.exists() and path.read_text(encoding="utf-8").strip():
    data = json.loads(path.read_text(encoding="utf-8"))
else:
    data = {}

if not isinstance(data, dict):
    raise SystemExit(f"invalid JSON object in {path}")

servers = data.get("mcpServers")
if not isinstance(servers, dict):
    servers = {}
data["mcpServers"] = servers
servers[name] = {
    "command": bin_,
    "args": ["mcp-server", "--project-dir", project_dir, "--data-dir", data_dir],
}

path.write_text(json.dumps(data, indent=2, sort_keys=True) + "\n", encoding="utf-8")
PY

echo "registered ${SERVER_NAME} with qwen (stdio): ${SLOPTOOLS_BIN} mcp-server"
echo "  settings:    ${SETTINGS_PATH}"
echo "  project-dir: ${PROJECT_DIR}"
echo "  data-dir:    ${DATA_DIR}"
