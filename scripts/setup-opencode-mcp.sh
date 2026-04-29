#!/usr/bin/env bash
# Add sloptools as a local stdio MCP server in the user's opencode.json.
#
# Idempotent: re-running replaces the existing entry.
# Stdio mode means there is no listening port or socket — opencode spawns
# `sloptools mcp-server` as a subprocess per session, the subprocess inherits
# the spawning user's UID, and other local users cannot intercept anything.
# That's the right model on shared boxes (university workstations etc.) where
# loopback TCP is reachable by every co-tenant.
#
# Env overrides:
#   OPENCODE_CONFIG          override config path (default ~/.config/opencode/opencode.json)
#   SLOPTOOLS_BIN            override sloptools binary (default: $(command -v sloptools))
#   SLOPTOOLS_DATA_DIR       data dir passed to mcp-server (default ~/.local/share/sloppy)
#   SLOPTOOLS_PROJECT_DIR    project dir passed to mcp-server (default current dir at install)
#   SLOPTOOLS_MCP_LABEL      key used in opencode.json mcp.<label> (default: sloppy)
#   SLOPTOOLS_VAULT_CONFIG   brain vault config path (default ~/.config/sloptools/vaults.toml)
set -euo pipefail

CONFIG_PATH="${OPENCODE_CONFIG:-${HOME}/.config/opencode/opencode.json}"
SLOPTOOLS_BIN="${SLOPTOOLS_BIN:-$(command -v sloptools 2>/dev/null || echo "")}"
DATA_DIR="${SLOPTOOLS_DATA_DIR:-${HOME}/.local/share/sloppy}"
PROJECT_DIR="${SLOPTOOLS_PROJECT_DIR:-$(pwd)}"
LABEL="${SLOPTOOLS_MCP_LABEL:-sloppy}"
VAULT_CONFIG="${SLOPTOOLS_VAULT_CONFIG:-${HOME}/.config/sloptools/vaults.toml}"

if [[ -z "${SLOPTOOLS_BIN}" ]]; then
  echo "sloptools binary not found. Install it first or set SLOPTOOLS_BIN." >&2
  exit 1
fi
if [[ ! -x "${SLOPTOOLS_BIN}" ]]; then
  echo "not executable: ${SLOPTOOLS_BIN}" >&2
  exit 1
fi

mkdir -p "$(dirname "${CONFIG_PATH}")" "${DATA_DIR}"

if [[ ! -f "${CONFIG_PATH}" ]]; then
  echo "{\"\$schema\": \"https://opencode.ai/config.json\"}" > "${CONFIG_PATH}"
fi

python3 - "${CONFIG_PATH}" "${SLOPTOOLS_BIN}" "${DATA_DIR}" "${PROJECT_DIR}" "${LABEL}" "${VAULT_CONFIG}" <<'PY'
import json
import sys

config_path, bin_, data_dir, project_dir, label, vault_config = sys.argv[1:7]
with open(config_path) as f:
    config = json.load(f)

config.setdefault("mcp", {})
config["mcp"][label] = {
    "type": "local",
    "command": [
        bin_,
        "mcp-server",
        "--stdio",
        "--vault-config", vault_config,
        "--project-dir", project_dir,
        "--data-dir", data_dir,
    ],
    "enabled": True,
}

with open(config_path, "w") as f:
    json.dump(config, f, indent=2)
    f.write("\n")
PY

echo "added sloptools stdio MCP server to ${CONFIG_PATH}"
echo "  label:    ${LABEL}"
echo "  command:  ${SLOPTOOLS_BIN} mcp-server --stdio --vault-config ${VAULT_CONFIG} --project-dir ${PROJECT_DIR} --data-dir ${DATA_DIR}"
