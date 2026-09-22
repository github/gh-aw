#!/usr/bin/env bash
set +o histexpand
set -euo pipefail

# Keep the host-side preparation and consumers at their canonical path while
# exposing the detector and its working directory to the separate Docker daemon.
# The optional second argument isolates the host directory in runtime tests.
MODE="${1:?Expected stage or collect}"
DETECTION_DIR="${2:-/tmp/gh-aw/threat-detection}"
STAGED_DIR="${RUNNER_TEMP:?RUNNER_TEMP must be set}/gh-aw/threat-detection"
BIN_DIR="${RUNNER_TEMP}/gh-aw/bin"
RESULT_FILES=(detection_result.json detection_result_full.json detection_usage.json detection_usage.jsonl)

if [[ -L "$STAGED_DIR" || -L "$DETECTION_DIR" || "$STAGED_DIR" = "$DETECTION_DIR" ]]; then
  echo "ERROR: Detection staging requires separate, non-symlink directories" >&2
  exit 1
fi

case "$MODE" in
  stage)
    mkdir -p "$STAGED_DIR" "$BIN_DIR"
    # Never accept results inherited from an artifact or an earlier attempt.
    for name in "${RESULT_FILES[@]}"; do
      rm -f "${DETECTION_DIR}/${name}" "${STAGED_DIR}/${name}"
    done
    cp -R "${DETECTION_DIR}/." "$STAGED_DIR/"
    detector="$(command -v threat-detect)"
    cp "$detector" "${BIN_DIR}/threat-detect"
    chmod +x "${BIN_DIR}/threat-detect"
    ;;
  collect)
    mkdir -p "$DETECTION_DIR"
    # Copy only detector outputs. In particular, never replace detection.log
    # (written by the host's tee) or execution.json (host execution evidence).
    for name in "${RESULT_FILES[@]}"; do
      if [[ -f "${STAGED_DIR}/${name}" && ! -L "${STAGED_DIR}/${name}" ]]; then
        cp "${STAGED_DIR}/${name}" "${DETECTION_DIR}/${name}"
      fi
    done
    ;;
  *)
    echo "ERROR: Expected stage or collect, got: $MODE" >&2
    exit 1
    ;;
esac
