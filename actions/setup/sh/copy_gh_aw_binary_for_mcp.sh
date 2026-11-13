#!/usr/bin/env bash
set +o histexpand

set -euo pipefail

ensure_directory_path_without_symlinks() {
  local path="$1"
  local current_path
  local component
  local -a components

  if [[ "$path" = /* ]]; then
    current_path="/"
  else
    current_path="$(pwd -P)"
  fi

  IFS='/' read -r -a components <<< "$path"
  for component in "${components[@]}"; do
    case "$component" in
      ""|.) continue ;;
      ..) current_path="$(dirname "$current_path")"; continue ;;
    esac

    if [[ "$current_path" = "/" ]]; then
      current_path="/$component"
    else
      current_path="$current_path/$component"
    fi
    if [[ -L "$current_path" ]]; then
      echo "::error::Refusing to use a path with a symlinked component: $current_path"
      return 1
    fi
    if [[ -e "$current_path" ]]; then
      if [[ ! -d "$current_path" ]]; then
        echo "::error::Output path component is not a directory: $current_path"
        return 1
      fi
    else
      if ! mkdir -- "$current_path"; then
        echo "::error::Failed to create output path component: $current_path"
        return 1
      fi
      if [[ -L "$current_path" || ! -d "$current_path" ]]; then
        echo "::error::Output path component is not a real directory: $current_path"
        return 1
      fi
    fi
  done
}

# Copy the gh-aw binary to ${RUNNER_TEMP}/gh-aw for MCP Server containerization
OUTPUT_DIR="${RUNNER_TEMP}/gh-aw"
if ! ensure_directory_path_without_symlinks "$OUTPUT_DIR"; then
  echo "::error::gh-aw output directory is not a real directory"
  exit 1
fi

GH_AW_BIN=""
GH_AW_BIN=$(command -v gh-aw 2>/dev/null) || true

if [ -z "$GH_AW_BIN" ] && [ -x "${OUTPUT_DIR}/bin/gh-aw" ] && [ ! -L "${OUTPUT_DIR}/bin/gh-aw" ]; then
  GH_AW_BIN="${RUNNER_TEMP}/gh-aw/bin/gh-aw"
fi

if [ -z "$GH_AW_BIN" ]; then
  GH_AW_BIN=$(find -P "${OUTPUT_DIR}/bin" -maxdepth 1 -type f -name 'gh-aw*' -perm -111 2>/dev/null | head -1) || true
fi

if [ -z "$GH_AW_BIN" ]; then
  GH_AW_BIN=$(find -P "${HOME}/.local/share/gh/extensions/gh-aw" -name 'gh-aw*' -type f -perm -111 2>/dev/null | head -1) || true
fi

if [ -z "$GH_AW_BIN" ] && [ -n "${GH_CONFIG_DIR:-}" ]; then
  GH_AW_BIN=$(find -P "${GH_CONFIG_DIR}/extensions/gh-aw" -name 'gh-aw*' -type f -perm -111 2>/dev/null | head -1) || true
fi

if [ -z "$GH_AW_BIN" ] && [ -f "${GITHUB_WORKSPACE}/gh-aw" ]; then
  GH_AW_BIN="${GITHUB_WORKSPACE}/gh-aw"
fi

if [ -n "$GH_AW_BIN" ] && [ -f "$GH_AW_BIN" ]; then
  GH_AW_BIN="$(realpath -e -- "$GH_AW_BIN")"
  if [ -L "${OUTPUT_DIR}/gh-aw" ]; then
    echo "::error::Refusing to replace a symlinked gh-aw binary"
    exit 1
  fi
  STAGED_BINARY="$(mktemp "${OUTPUT_DIR}/.gh-aw.XXXXXX")"
  trap 'rm -f -- "${STAGED_BINARY:-}"' EXIT
  cp -- "$GH_AW_BIN" "$STAGED_BINARY"
  chmod +x "$STAGED_BINARY"
  mv -f -- "$STAGED_BINARY" "${OUTPUT_DIR}/gh-aw"
  echo "Copied gh-aw binary to ${RUNNER_TEMP}/gh-aw/gh-aw"
else
  echo "::error::Failed to find gh-aw binary for MCP Server"
  exit 1
fi
