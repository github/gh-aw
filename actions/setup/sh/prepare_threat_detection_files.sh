#!/usr/bin/env bash
set +o histexpand

set -euo pipefail

SOURCE_DIR="${1:-/tmp/gh-aw}"
DETECTION_DIR="${2:-${SOURCE_DIR}/threat-detection}"
PROMPT_SOURCE_DIR="${SOURCE_DIR}/aw-prompts"
PROMPT_DETECTION_DIR="${DETECTION_DIR}/aw-prompts"

copy_optional_file() {
  local source_file="$1"
  local destination_file="$2"

  if [ -f "${source_file}" ]; then
    cp "${source_file}" "${destination_file}"
  fi
}

mkdir -p "${PROMPT_DETECTION_DIR}"

copy_optional_file "${PROMPT_SOURCE_DIR}/prompt.txt" "${PROMPT_DETECTION_DIR}/prompt.txt"
if [ ! -s "${PROMPT_DETECTION_DIR}/prompt.txt" ]; then
  echo "::warning::ERR_VALIDATION: Missing or empty detection context prompt at ${PROMPT_DETECTION_DIR}/prompt.txt. Ensure the agent artifact includes ${PROMPT_SOURCE_DIR}/prompt.txt. Detection will continue with fallback workflow context."
fi

copy_optional_file "${PROMPT_SOURCE_DIR}/prompt-template.txt" "${PROMPT_DETECTION_DIR}/prompt-template.txt"
copy_optional_file "${PROMPT_SOURCE_DIR}/prompt-import-tree.json" "${PROMPT_DETECTION_DIR}/prompt-import-tree.json"
copy_optional_file "${SOURCE_DIR}/aw_info.json" "${DETECTION_DIR}/aw_info.json"
copy_optional_file "${SOURCE_DIR}/agent_output.json" "${DETECTION_DIR}/agent_output.json"

if [ -d "${SOURCE_DIR}/comment-memory" ]; then
  mkdir -p "${DETECTION_DIR}/comment-memory"
  for memory_file in "${SOURCE_DIR}"/comment-memory/*.md; do
    if [ -f "${memory_file}" ]; then
      cp "${memory_file}" "${DETECTION_DIR}/comment-memory/"
    fi
  done
fi

for artifact_pattern in aw-*.patch aw-*.bundle; do
  for artifact_file in "${SOURCE_DIR}"/${artifact_pattern}; do
    if [ -f "${artifact_file}" ]; then
      cp "${artifact_file}" "${DETECTION_DIR}/"
    fi
  done
done

echo "Prepared threat detection files:"
ls -la "${DETECTION_DIR}"
