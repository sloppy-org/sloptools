#!/usr/bin/env bash
# check-forbidden-tokens.sh
#
# Scans tracked source/docs/tests for personal/site-specific token families.
# Keep the built-in token list generic; per-user extensions come from local
# config or environment and must not be committed.
#
# Usage:
#   ./scripts/check-forbidden-tokens.sh
#   SLOPTOOLS_FORBIDDEN_TOKENS=$'mypattern1\nmypattern2' ./scripts/check-forbidden-tokens.sh
#
# Local config:
#   SLOPTOOLS_FORBIDDEN_TOKENS_FILE defaults to
#   ~/.config/sloptools/forbidden-tokens.txt when set via the default path.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

patterns=(
  '/home/[A-Za-z0-9._-]+/'
  '\btugraz\.at\b'
  '\bsloppy\.at\b'
  '\bqodo\.ai\b'
  '\bacademia-mail\.com\b'
  '\biter\.org\b'
)

add_pattern_file() {
  local file="$1"
  [[ -r "$file" ]] || return 0
  while IFS= read -r line || [[ -n "$line" ]]; do
    line="${line#"${line%%[![:space:]]*}"}"
    line="${line%"${line##*[![:space:]]}"}"
    [[ -z "$line" || "$line" == \#* ]] && continue
    patterns+=("$line")
  done < "$file"
}

if [[ -n "${SLOPTOOLS_FORBIDDEN_TOKENS_FILE:-}" ]]; then
  if [[ ! -r "${SLOPTOOLS_FORBIDDEN_TOKENS_FILE}" ]]; then
    echo "forbidden-token config not readable: ${SLOPTOOLS_FORBIDDEN_TOKENS_FILE}" >&2
    exit 1
  fi
  add_pattern_file "${SLOPTOOLS_FORBIDDEN_TOKENS_FILE}"
else
  add_pattern_file "${XDG_CONFIG_HOME:-${HOME}/.config}/sloptools/forbidden-tokens.txt"
fi

if [[ -n "${SLOPTOOLS_FORBIDDEN_TOKENS:-}" ]]; then
  while IFS= read -r line || [[ -n "$line" ]]; do
    line="${line#"${line%%[![:space:]]*}"}"
    line="${line%"${line##*[![:space:]]}"}"
    [[ -z "$line" || "$line" == \#* ]] && continue
    patterns+=("$line")
  done <<< "${SLOPTOOLS_FORBIDDEN_TOKENS}"
fi

pathspecs=(
  'README.md'
  'docs'
  'cmd'
  'internal'
  'scripts'
  '.github'
  ':!internal/store/storetest/store_test_05_test.go'
  ':!scripts/check-forbidden-tokens.sh'
)

fail=0
for pattern in "${patterns[@]}"; do
  tmp="$(mktemp)"
  if git grep -nI -E "$pattern" -- "${pathspecs[@]}" >"$tmp" 2>/dev/null; then
    if [[ -s "$tmp" ]]; then
      echo "FORBIDDEN: pattern $pattern found in:"
      cat "$tmp"
      fail=1
    fi
  else
    status=$?
    if [[ $status -ne 1 ]]; then
      cat "$tmp" >&2
      rm -f "$tmp"
      exit "$status"
    fi
  fi
  rm -f "$tmp"
done

if [[ "$fail" -ne 0 ]]; then
  echo
  echo "Remove or generalize the forbidden tokens above."
  echo "Set SLOPTOOLS_FORBIDDEN_TOKENS for per-user extensions, or point SLOPTOOLS_FORBIDDEN_TOKENS_FILE at a local config file."
  exit 1
fi

echo "check-forbidden-tokens: clean"
