#!/usr/bin/env bash
set +o histexpand

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PREPARE_SCRIPT="${SCRIPT_DIR}/prepare_threat_detection_files.sh"

TESTS_PASSED=0
TESTS_FAILED=0

pass() { echo "PASS: $1"; TESTS_PASSED=$((TESTS_PASSED + 1)); }
fail() { echo "FAIL: $1"; echo "  $2"; TESTS_FAILED=$((TESTS_FAILED + 1)); }

assert_file_content() {
  local description="$1"
  local path="$2"
  local expected="$3"

  if [ ! -f "${path}" ]; then
    fail "${description}" "missing file: ${path}"
  elif [ "$(cat "${path}")" != "${expected}" ]; then
    fail "${description}" "unexpected content in ${path}"
  else
    pass "${description}"
  fi
}

test_stages_detection_inputs() {
  local sandbox
  sandbox="$(mktemp -d)"
  local source_dir="${sandbox}/gh-aw"
  local detection_dir="${source_dir}/threat-detection"

  mkdir -p "${source_dir}/aw-prompts" "${source_dir}/comment-memory"
  printf 'prompt\n' >"${source_dir}/aw-prompts/prompt.txt"
  printf 'template\n' >"${source_dir}/aw-prompts/prompt-template.txt"
  printf '{"imports":[]}\n' >"${source_dir}/aw-prompts/prompt-import-tree.json"
  printf '{"engine":"copilot"}\n' >"${source_dir}/aw_info.json"
  printf '{"output":"done"}\n' >"${source_dir}/agent_output.json"
  printf 'memory\n' >"${source_dir}/comment-memory/context.md"
  printf 'ignored\n' >"${source_dir}/comment-memory/context.txt"
  printf 'patch\n' >"${source_dir}/aw-test.patch"
  printf 'bundle\n' >"${source_dir}/aw-test.bundle"

  local output
  if ! output="$(bash "${PREPARE_SCRIPT}" "${source_dir}" "${detection_dir}" 2>&1)"; then
    fail "stages detection inputs" "script failed: ${output}"
    rm -rf "${sandbox}"
    return
  fi

  assert_file_content "copies prompt" "${detection_dir}/aw-prompts/prompt.txt" "prompt"
  assert_file_content "copies prompt template" "${detection_dir}/aw-prompts/prompt-template.txt" "template"
  assert_file_content "copies prompt import tree" "${detection_dir}/aw-prompts/prompt-import-tree.json" '{"imports":[]}'
  assert_file_content "copies workflow information" "${detection_dir}/aw_info.json" '{"engine":"copilot"}'
  assert_file_content "copies agent output" "${detection_dir}/agent_output.json" '{"output":"done"}'
  assert_file_content "copies markdown comment memory" "${detection_dir}/comment-memory/context.md" "memory"
  assert_file_content "copies patch artifact" "${detection_dir}/aw-test.patch" "patch"
  assert_file_content "copies bundle artifact" "${detection_dir}/aw-test.bundle" "bundle"

  if [ -e "${detection_dir}/comment-memory/context.txt" ]; then
    fail "ignores non-markdown comment memory" "context.txt was copied"
  else
    pass "ignores non-markdown comment memory"
  fi

  rm -rf "${sandbox}"
}

test_warns_for_missing_prompt() {
  local sandbox
  sandbox="$(mktemp -d)"
  local source_dir="${sandbox}/gh-aw"
  local detection_dir="${source_dir}/threat-detection"
  mkdir -p "${source_dir}"

  local output
  if ! output="$(bash "${PREPARE_SCRIPT}" "${source_dir}" "${detection_dir}" 2>&1)"; then
    fail "accepts absent optional inputs" "script failed: ${output}"
  elif [[ "${output}" != *"ERR_VALIDATION: Missing or empty detection context prompt"* ]]; then
    fail "warns for missing prompt" "warning was not emitted: ${output}"
  else
    pass "accepts absent optional inputs"
    pass "warns for missing prompt"
  fi

  rm -rf "${sandbox}"
}

echo "Running prepare_threat_detection_files.sh tests..."
echo

test_stages_detection_inputs
test_warns_for_missing_prompt

echo
echo "==============================="
echo "Tests passed: ${TESTS_PASSED}"
echo "Tests failed: ${TESTS_FAILED}"
echo "==============================="

if [ "${TESTS_FAILED}" -gt 0 ]; then
  exit 1
fi
