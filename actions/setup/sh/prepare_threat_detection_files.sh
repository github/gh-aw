#!/usr/bin/env bash
set +o histexpand

set -euo pipefail

SOURCE_DIR="${1:-/tmp/gh-aw}"
DETECTION_DIR="${2:-${SOURCE_DIR}/threat-detection}"
PROMPT_SOURCE_DIR="${SOURCE_DIR}/aw-prompts"
PROMPT_DETECTION_DIR="${DETECTION_DIR}/aw-prompts"
INPUT_DIR="${SOURCE_DIR}/threat-detection-inputs"

copy_optional_file() {
  local source_file="$1"
  local destination_file="$2"

  if [ -f "${source_file}" ] && [ ! -L "${source_file}" ]; then
    cp "${source_file}" "${destination_file}"
  fi
}

copy_detection_input() {
  local source_file="$1"
  local destination_file="$2"

  if [ -f "${source_file}" ] && [ ! -L "${source_file}" ]; then
    mkdir -p "$(dirname "${destination_file}")"
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

if [ -d "${SOURCE_DIR}/comment-memory" ] && [ ! -L "${SOURCE_DIR}/comment-memory" ]; then
  mkdir -p "${DETECTION_DIR}/comment-memory"
  for memory_file in "${SOURCE_DIR}"/comment-memory/*.md; do
    if [ -f "${memory_file}" ] && [ ! -L "${memory_file}" ]; then
      cp "${memory_file}" "${DETECTION_DIR}/comment-memory/"
    fi
  done
fi

for artifact_pattern in aw-*.patch aw-*.bundle; do
  for artifact_file in "${SOURCE_DIR}"/${artifact_pattern}; do
    if [ -f "${artifact_file}" ] && [ ! -L "${artifact_file}" ]; then
      cp "${artifact_file}" "${DETECTION_DIR}/"
    fi
  done
done

rm -rf "${INPUT_DIR}"
mkdir -p "${INPUT_DIR}"

for prompt_file in prompt.txt prompt-template.txt prompt-import-tree.json; do
  copy_detection_input "${PROMPT_DETECTION_DIR}/${prompt_file}" "${INPUT_DIR}/aw-prompts/${prompt_file}"
done
copy_detection_input "${DETECTION_DIR}/aw_info.json" "${INPUT_DIR}/aw_info.json"
copy_detection_input "${DETECTION_DIR}/agent_output.json" "${INPUT_DIR}/agent_output.json"

if [ -d "${DETECTION_DIR}/comment-memory" ] && [ ! -L "${DETECTION_DIR}/comment-memory" ]; then
  for memory_file in "${DETECTION_DIR}"/comment-memory/*.md; do
    if [ -f "${memory_file}" ] && [ ! -L "${memory_file}" ]; then
      copy_detection_input "${memory_file}" "${INPUT_DIR}/comment-memory/$(basename "${memory_file}")"
    fi
  done
fi

for artifact_pattern in aw-*.patch aw-*.bundle; do
  for artifact_file in "${DETECTION_DIR}"/${artifact_pattern}; do
    if [ -f "${artifact_file}" ] && [ ! -L "${artifact_file}" ]; then
      copy_detection_input "${artifact_file}" "${INPUT_DIR}/$(basename "${artifact_file}")"
    fi
  done
done

echo "Prepared threat detection files:"
ls -la "${DETECTION_DIR}"
