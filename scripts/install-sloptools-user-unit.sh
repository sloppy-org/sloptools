#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PLATFORM="$(uname -s)"

log() { printf '[sloptools-units] %s\n' "$*"; }
fail() { printf '[sloptools-units] ERROR: %s\n' "$*" >&2; exit 1; }

if [ "$PLATFORM" != "Linux" ]; then
  fail "install-sloptools-user-unit.sh currently supports Linux systemd user services only"
fi

command -v systemctl >/dev/null 2>&1 || fail "systemctl is required"
command -v go >/dev/null 2>&1 || fail "go is required"

unit_src="${REPO_ROOT}/deploy/systemd/user/sloptools-runtime.service"
unit_dst="${HOME}/.config/systemd/user"
unit_path="${unit_dst}/sloptools-runtime.service"

[ -f "$unit_src" ] || fail "missing unit template: ${unit_src}"

mkdir -p "$unit_dst"
sed -e "s|@@REPO_ROOT@@|${REPO_ROOT}|g" "$unit_src" >"$unit_path"

systemctl --user daemon-reload
systemctl --user enable --now sloptools-runtime.service

sleep 2
if ! systemctl --user is-active sloptools-runtime.service >/dev/null 2>&1; then
  systemctl --user status sloptools-runtime.service --no-pager -n 20 2>&1 || true
  fail "sloptools-runtime.service did not start cleanly"
fi

log "Installed and started sloptools-runtime.service"
log "Socket: unix:${XDG_RUNTIME_DIR:-/run/user/$(id -u)}/sloppy/sloptools.sock"
