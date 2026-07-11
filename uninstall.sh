#!/bin/sh
# claude-says uninstaller: removes ~/.claude-says (config) via the binary's own
# `uninstall` command, then removes the binary.
#
#   curl -fsSL https://raw.githubusercontent.com/Sudhanshu069/claude-says/main/uninstall.sh | sh
#
# Keep the binary with KEEP_BINARY=1; keep config with KEEP_CONFIG=1.
set -eu

BINDIR="${BINDIR:-/usr/local/bin}"

# Locate the binary: PATH first, then BINDIR.
if command -v claude-says >/dev/null 2>&1; then
  bin="$(command -v claude-says)"
elif [ -x "${BINDIR}/claude-says" ]; then
  bin="${BINDIR}/claude-says"
else
  bin=""
fi

# Remove ~/.claude-says via the binary's uninstall command. claude-says installs
# nothing into Claude Code, so there is no hook to strip from settings.json.
if [ -n "${bin}" ]; then
  if [ "${KEEP_CONFIG:-0}" = "1" ]; then
    "${bin}" uninstall --keep-config || true
  else
    "${bin}" uninstall || true
  fi
elif [ "${KEEP_CONFIG:-0}" != "1" ] && [ -d "${HOME}/.claude-says" ]; then
  rm -rf "${HOME}/.claude-says"
  echo "Removed ${HOME}/.claude-says"
fi

# Remove the binary.
if [ "${KEEP_BINARY:-0}" != "1" ] && [ -n "${bin}" ] && [ -f "${bin}" ]; then
  dir="$(dirname "${bin}")"
  if [ -w "${dir}" ]; then rm -f "${bin}"; else sudo rm -f "${bin}"; fi
  echo "Removed ${bin}"
fi
echo "Done."
