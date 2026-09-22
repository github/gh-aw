#!/usr/bin/env bash
set +o histexpand
set -euo pipefail

# Keep the host-side preparation and consumers at their canonical path while
# exposing the detector and its working directory to the separate Docker daemon.
# The optional second argument isolates the host directory in runtime tests.
MODE="${1:?Expected reset, stage or collect}"
DETECTION_DIR="${2:-/tmp/gh-aw/threat-detection}"
STAGED_DIR="${RUNNER_TEMP:?RUNNER_TEMP must be set}/gh-aw/threat-detection"
BIN_DIR="${RUNNER_TEMP}/gh-aw/bin"
RESULT_FILES=(detection_result.json detection_result_full.json detection_usage.json detection_usage.jsonl)

if [[ -L "$STAGED_DIR" || -L "$DETECTION_DIR" || "$STAGED_DIR" = "$DETECTION_DIR" ]]; then
  echo "ERROR: Detection staging requires separate, non-symlink directories" >&2
  exit 1
fi

# Reset is also a separate, unconditional step before preparation/installation:
# execution may be skipped after a failed install, but stale verdicts must go.
case "$MODE" in
  reset|stage)
    for name in "${RESULT_FILES[@]}"; do
      rm -f "${DETECTION_DIR}/${name}" "${STAGED_DIR}/${name}"
    done
    mkdir -p "$STAGED_DIR"
    # Clear optional inputs too, including nested files and dotfiles. Do not
    # follow symlinks when clearing this dedicated, validated staging directory.
    find "$STAGED_DIR" -mindepth 1 -delete
    ;;
esac

case "$MODE" in
  reset) ;;
  stage)
    detector="${THREAT_DETECT_BINARY:?Verified detector path must be supplied by the install step}"
    if [[ ! -f "$detector" || ! -x "$detector" || -L "$detector" ]]; then
      echo "ERROR: Verified detector must be a regular executable file" >&2
      exit 1
    fi
    mkdir -p "$BIN_DIR"
    cp -R "${DETECTION_DIR}/." "$STAGED_DIR/"
    # Remove a stale destination symlink instead of following it during copy.
    rm -f "${BIN_DIR}/threat-detect"
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
    echo "ERROR: Expected reset, stage or collect, got: $MODE" >&2
    exit 1
    ;;
esac
