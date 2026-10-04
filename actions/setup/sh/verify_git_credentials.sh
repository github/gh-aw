#!/usr/bin/env bash
set -euo pipefail
set +o histexpand

snapshot=""
trap 'if [ -n "$snapshot" ]; then rm -f -- "$snapshot"; fi' EXIT

verify_config() {
  local config="$1" entry key status=0
  snapshot=$(mktemp "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/gh-aw-git-config.XXXXXX")
  if ! git --git-dir="${config%/*}" config --file "$config" --includes --null --list >"$snapshot"; then
    echo "ERROR: Cannot verify git credential removal from $config" >&2
    return 1
  fi
  while IFS= read -r -d '' entry; do
    key="${entry%%$'\n'*}"
    case "$key" in
      credential.*|http.extraheader|http.*.extraheader)
        echo "ERROR: Git credentials remain in $config; refusing agent execution" >&2
        status=1
        break
        ;;
    esac
    if [[ "$entry" =~ https?://[^[:space:]/]*@ ]]; then
      echo "ERROR: Authenticated git URL remains in $config; refusing agent execution" >&2
      status=1
      break
    fi
  done <"$snapshot"
  rm -f -- "$snapshot"
  snapshot=""
  return "$status"
}

if [ "$#" -gt 0 ]; then
  for config in "$@"; do
    verify_config "$config"
  done
else
  while IFS= read -r -d '' config; do
    verify_config "$config"
  done < <(find "${GITHUB_WORKSPACE:-.}" /tmp -maxdepth 15 -type f -name config \
    \( -path '*/.git/config' -o -path '*/.git/modules/*/config' \) -print0 2>/dev/null)
fi
