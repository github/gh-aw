#!/bin/bash
set +o histexpand

set -euo pipefail

CACHE_DIR="${GH_AW_CACHE_DIR:-${GH_AW_RUNTIME_DIR:?}/cache-memory}"

if [ -d "$CACHE_DIR/.git" ]; then
  if ! git -C "$CACHE_DIR" fsck --no-dangling >/dev/null 2>&1; then
    echo "::warning title=cache-memory git integrity::Detected git corruption; reseeding cache-memory git repository"
    rm -rf "$CACHE_DIR/.git" || true
    git -C "$CACHE_DIR" init >/dev/null 2>&1 || true
    git -C "$CACHE_DIR" \
      -c user.name="github-actions[bot]" \
      -c user.email="41898282+github-actions[bot]@users.noreply.github.com" \
      commit --allow-empty -m "chore(cache-memory): reseed after corruption" >/dev/null 2>&1 || true
  else
    git -C "$CACHE_DIR" gc --prune=now >/dev/null 2>&1 || true
  fi
fi
