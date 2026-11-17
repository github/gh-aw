#!/usr/bin/env bash
set +o histexpand

# Tests for setup_cache_memory_git.sh — pre-agent sanitization block
# Run: bash setup_cache_memory_git_test.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="${SCRIPT_DIR}/setup_cache_memory_git.sh"

# Test counters
TESTS_PASSED=0
TESTS_FAILED=0

# Temporary workspace for all tests
WORKSPACE=$(mktemp -d)

cleanup() {
  rm -rf "${WORKSPACE}"
}
trap cleanup EXIT

# Helper: assert a condition
assert() {
  local name="$1"
  local condition="$2"
  if eval "${condition}" 2>/dev/null; then
    echo "  ✓ ${name}"
    TESTS_PASSED=$((TESTS_PASSED + 1))
  else
    echo "  ✗ ${name}"
    TESTS_FAILED=$((TESTS_FAILED + 1))
  fi
}

# Helper: create a fresh git cache dir with the given files already committed.
# Usage: make_cache_dir <dir> [<file> ...]
# Files are created and committed to the 'none' branch (the lowest-trust default).
make_cache_dir() {
  local dir="$1"
  shift
  mkdir -p "${dir}"
  pushd "${dir}" >/dev/null
  git init -b merged -q
  git config user.email "test@example.com"
  git config user.name "test"
  git config core.hooksPath /dev/null
  git commit --allow-empty -m "initial" -q
  for level in approved unapproved none; do
    git branch "${level}" 2>/dev/null || true
  done
  git checkout -q none
  for f in "$@"; do
    mkdir -p "$(dirname "${f}")"
    echo "content" > "${f}"
  done
  git add -A
  git commit --allow-empty -m "test-files" -q
  popd >/dev/null
}

# Run the script, capturing stdout and ignoring the exit code.
run_script() {
  local dir="$1"
  local integrity="${2:-none}"
  local allowed_exts="${3:-}"
  GH_AW_CACHE_DIR="${dir}" \
  GH_AW_MIN_INTEGRITY="${integrity}" \
  GH_AW_ALLOWED_EXTENSIONS="${allowed_exts}" \
    bash "${SCRIPT}" 2>&1 || true
}

echo "Testing setup_cache_memory_git.sh — pre-agent sanitization"
echo ""

# ── Test 0: Git diagnostics use private, unpredictable temporary paths ────────
echo "Test 0: Git diagnostics use private temporary paths"
assert "diagnostic directory is created with mktemp" \
  "grep -q 'mktemp -d /tmp/gh-aw-cache-memory.XXXXXX' '${SCRIPT}'"
assert "fixed diagnostic paths are not used" \
  "! grep -Eq '/tmp/gh-aw-(git-fsck|checkout|merge)-' '${SCRIPT}'"
echo ""

# ── Test 1: Execute bits are stripped from restored files ────────────────────
echo "Test 1: Execute bits are stripped unconditionally"
D="${WORKSPACE}/test1"
make_cache_dir "${D}" "script.sh" "data.json"
# Make files executable before the script runs
chmod +x "${D}/script.sh" "${D}/data.json"
run_script "${D}" none >/dev/null
assert "script.sh is not executable"   "[ ! -x '${D}/script.sh' ]"
assert "data.json is not executable"   "[ ! -x '${D}/data.json' ]"
assert "script.sh still exists"        "[ -f '${D}/script.sh' ]"
assert "data.json still exists"        "[ -f '${D}/data.json' ]"
echo ""

# ── Test 2: .git directory files are NOT touched (sanity check) ──────────────
echo "Test 2: .git directory is not affected by chmod"
D="${WORKSPACE}/test2"
make_cache_dir "${D}" "file.txt"
HOOK_FILE="${D}/.git/hooks/pre-commit"
echo "#!/bin/bash" > "${HOOK_FILE}"
chmod +x "${HOOK_FILE}"
run_script "${D}" none >/dev/null
# The hook file cleanup happens earlier in the script but the .git dir itself is
# excluded from find. Verify find exclusion by checking the .git dir is intact.
assert ".git directory still exists"   "[ -d '${D}/.git' ]"
echo ""

# ── Test 2b: Cached git config/info state is cleared and reset ─────────────────
echo "Test 2b: cached git config/info state is cleared"
D="${WORKSPACE}/test2b"
make_cache_dir "${D}" "file.txt"
mkdir -p "${D}/.git/info"
cat > "${D}/.git/config" <<'EOF'
[user]
	email = attacker@example.com
	name = attacker
[core]
	fsmonitor = /tmp/evil.sh
	sshCommand = /tmp/ssh-wrapper.sh
	hooksPath = /tmp/hooks
	attributesFile = /tmp/evil-attributes
[include]
	path = /tmp/evil-include
[includeIf "onbranch:approved"]
	path = /tmp/evil-includeif
[credential]
	helper = /tmp/credential-helper
[credential "https://github.com"]
	helper = /tmp/credential-helper-url
[alias]
	co = !/tmp/evil-alias.sh
[filter "evil"]
	smudge = /tmp/evil-smudge.sh
	process = /tmp/evil-process.sh
[merge "evil"]
	driver = /tmp/evil-merge-driver.sh %O %A %B %L
EOF
echo "*.txt" > "${D}/.git/info/exclude"
echo "*.txt -text" > "${D}/.git/info/attributes"
echo "0000000000000000000000000000000000000000 1111111111111111111111111111111111111111" > "${D}/.git/info/grafts"
echo "/tmp" > "${D}/.git/info/sparse-checkout"
run_script "${D}" none >/dev/null
EMAIL_CFG="$(git -C "${D}" config user.email)"
NAME_CFG="$(git -C "${D}" config user.name)"
FSMONITOR_CFG="$(git -C "${D}" config --default '' core.fsmonitor)"
HOOKSPATH_CFG="$(git -C "${D}" config --default '' core.hooksPath)"
SSHCOMMAND_CFG="$(git -C "${D}" config --default '' core.sshCommand)"
ATTRIBUTES_FILE_CFG="$(git -C "${D}" config --default '' core.attributesFile)"
INCLUDE_CFG="$(git -C "${D}" config --local --name-only --get-regexp '^include\.' 2>/dev/null || true)"
INCLUDEIF_CFG="$(git -C "${D}" config --local --name-only --get-regexp '^includeif\.' 2>/dev/null || true)"
CREDENTIAL_CFG="$(git -C "${D}" config --local --name-only --get-regexp '^credential\.' 2>/dev/null || true)"
ALIAS_CFG="$(git -C "${D}" config --local --name-only --get-regexp '^alias\.' 2>/dev/null || true)"
FILTER_CFG="$(git -C "${D}" config --local --name-only --get-regexp '^filter\.' 2>/dev/null || true)"
MERGE_CFG="$(git -C "${D}" config --local --name-only --get-regexp '^merge\.' 2>/dev/null || true)"
assert ".git config user email reset" \
  "[ \"${EMAIL_CFG}\" = 'gh-aw@github.com' ]"
assert ".git config user name reset" \
  "[ \"${NAME_CFG}\" = 'gh-aw' ]"
assert ".git fsmonitor disabled" \
  "[ \"${FSMONITOR_CFG}\" = 'false' ]"
assert ".git hooksPath reset to /dev/null" \
  "[ \"${HOOKSPATH_CFG}\" = '/dev/null' ]"
assert ".git sshCommand removed" \
  "[ -z \"${SSHCOMMAND_CFG}\" ]"
assert ".git core.attributesFile removed" \
  "[ -z \"${ATTRIBUTES_FILE_CFG}\" ]"
assert ".git include sections removed" \
  "[ -z \"${INCLUDE_CFG}\" ]"
assert ".git includeIf sections removed" \
  "[ -z \"${INCLUDEIF_CFG}\" ]"
assert ".git credential sections removed" \
  "[ -z \"${CREDENTIAL_CFG}\" ]"
assert ".git alias sections removed" \
  "[ -z \"${ALIAS_CFG}\" ]"
assert ".git filter sections removed" \
  "[ -z \"${FILTER_CFG}\" ]"
assert ".git merge sections removed" \
  "[ -z \"${MERGE_CFG}\" ]"
assert ".git info/exclude removed" "[ ! -f '${D}/.git/info/exclude' ]"
assert ".git info/attributes removed" "[ ! -f '${D}/.git/info/attributes' ]"
assert ".git info/grafts removed" "[ ! -f '${D}/.git/info/grafts' ]"
assert ".git info/sparse-checkout removed" "[ ! -f '${D}/.git/info/sparse-checkout' ]"
echo ""

# ── Test 2c: Symlinked git metadata is rejected before .git operations ─────────
echo "Test 2c: symlinked .git metadata is rejected"
D="${WORKSPACE}/test2c"
make_cache_dir "${D}" "file.txt"
mkdir -p "${D}/outside-info"
echo "*.tmp" > "${D}/outside-info/exclude"
mv "${D}/.git/config" "${D}/outside-config"
ln -s "${D}/outside-config" "${D}/.git/config"
rm -rf "${D}/.git/info"
ln -s "${D}/outside-info" "${D}/.git/info"
run_script "${D}" none >/dev/null
EMAIL_CFG_2C="$(git -C "${D}" config user.email)"
assert "external info file remains untouched" "[ -f '${D}/outside-info/exclude' ]"
assert "external config file remains untouched" "[ -f '${D}/outside-config' ]"
assert "local .git/info is rebuilt as directory" "[ -d '${D}/.git/info' ] && [ ! -L '${D}/.git/info' ]"
assert "local .git/config is rebuilt as regular file" "[ -f '${D}/.git/config' ] && [ ! -L '${D}/.git/config' ]"
assert "reinitialized config sets safe user.email" \
  "[ \"${EMAIL_CFG_2C}\" = 'gh-aw@github.com' ]"
echo ""

# ── Test 3: No extension filter — all files kept when GH_AW_ALLOWED_EXTENSIONS is empty ─
echo "Test 3: No extension filter when GH_AW_ALLOWED_EXTENSIONS is unset"
D="${WORKSPACE}/test3"
make_cache_dir "${D}" "file.json" "file.md" "helper.sh" "binary"
run_script "${D}" none ""
assert "file.json kept"  "[ -f '${D}/file.json' ]"
assert "file.md kept"    "[ -f '${D}/file.md' ]"
assert "helper.sh kept"  "[ -f '${D}/helper.sh' ]"
assert "binary kept"     "[ -f '${D}/binary' ]"
echo ""

# ── Test 4: Extension filter removes disallowed files ────────────────────────
echo "Test 4: Extension filter removes disallowed file types"
D="${WORKSPACE}/test4"
make_cache_dir "${D}" "data.json" "notes.md" "helper.sh" "archive.zip"
run_script "${D}" none ".json:.md"
assert "data.json kept"     "[ -f '${D}/data.json' ]"
assert "notes.md kept"      "[ -f '${D}/notes.md' ]"
assert "helper.sh removed"  "[ ! -f '${D}/helper.sh' ]"
assert "archive.zip removed" "[ ! -f '${D}/archive.zip' ]"
echo ""

# ── Test 5: Extension filter removes files without any extension ─────────────
echo "Test 5: Extension filter removes files with no extension"
D="${WORKSPACE}/test5"
make_cache_dir "${D}" "data.json" "noext"
run_script "${D}" none ".json"
assert "data.json kept"  "[ -f '${D}/data.json' ]"
assert "noext removed"   "[ ! -f '${D}/noext' ]"
echo ""

# ── Test 6: Extension filter with single extension ───────────────────────────
echo "Test 6: Extension filter with a single allowed extension"
D="${WORKSPACE}/test6"
make_cache_dir "${D}" "report.json" "notes.txt" "image.png"
run_script "${D}" none ".json"
assert "report.json kept"  "[ -f '${D}/report.json' ]"
assert "notes.txt removed" "[ ! -f '${D}/notes.txt' ]"
assert "image.png removed" "[ ! -f '${D}/image.png' ]"
echo ""

# ── Test 7: Execute bits stripped AND disallowed files removed together ───────
echo "Test 7: Execute-bit stripping and extension filtering both apply"
D="${WORKSPACE}/test7"
make_cache_dir "${D}" "keep.json" "drop.sh"
chmod +x "${D}/keep.json" "${D}/drop.sh"
run_script "${D}" none ".json"
assert "keep.json exists"        "[ -f '${D}/keep.json' ]"
assert "keep.json not executable" "[ ! -x '${D}/keep.json' ]"
assert "drop.sh removed"         "[ ! -f '${D}/drop.sh' ]"
echo ""

# ── Test 8: Extension matching is case-insensitive ───────────────────────────
echo "Test 8: Extension matching is case-insensitive"
D="${WORKSPACE}/test8"
make_cache_dir "${D}" "data.json" "data.JSON" "notes.MD"
# Allow list uses lowercase; both .json and .JSON files, and .MD files, should be kept
run_script "${D}" none ".json:.md"
assert "data.json kept (exact match)"     "[ -f '${D}/data.json' ]"
assert "data.JSON kept (uppercase file)"  "[ -f '${D}/data.JSON' ]"
assert "notes.MD kept (uppercase file)"   "[ -f '${D}/notes.MD' ]"
echo ""

# ── Test 9: Whitespace in GH_AW_ALLOWED_EXTENSIONS is trimmed ────────────────
echo "Test 9: Whitespace in allowed extensions list is trimmed"
D="${WORKSPACE}/test9"
make_cache_dir "${D}" "data.json" "note.md" "drop.sh"
# Extensions with leading/trailing spaces should still match
run_script "${D}" none " .json : .md "
assert "data.json kept (trimmed .json)"  "[ -f '${D}/data.json' ]"
assert "note.md kept (trimmed .md)"      "[ -f '${D}/note.md' ]"
assert "drop.sh removed"                 "[ ! -f '${D}/drop.sh' ]"
echo ""

# ── Test 10: Symlinks are deleted unconditionally ────────────────────────────
echo "Test 10: Symlinks in working tree are deleted"
D="${WORKSPACE}/test10"
make_cache_dir "${D}" "real.json"
# Plant a symlink (simulating a compromised prior run)
ln -s /etc/passwd "${D}/evil-link"
assert "symlink exists before script"    "[ -L '${D}/evil-link' ]"
run_script "${D}" none >/dev/null
assert "symlink removed by script"       "[ ! -L '${D}/evil-link' ]"
assert "real file still exists"          "[ -f '${D}/real.json' ]"
echo ""

# ── Test 11: Files with spaces in name are handled correctly ─────────────────
echo "Test 11: Files with spaces in names are handled correctly"
D="${WORKSPACE}/test11"
make_cache_dir "${D}" "my data.json" "my script.sh"
run_script "${D}" none ".json"
assert "file with space and .json kept"    "[ -f '${D}/my data.json' ]"
assert "file with space and .sh removed"   "[ ! -f '${D}/my script.sh' ]"
echo ""

# ── Test 12: Legacy nested artifact layout is flattened before git setup ─────
echo "Test 12: Legacy nested cache directory is flattened"
D="${WORKSPACE}/test12"
mkdir -p "${D}/$(basename "${D}")"
echo '{"totalRuns":15}' > "${D}/$(basename "${D}")/chaos-pr-bundle-fuzzer.json"
set +e
OUTPUT="$(
  GH_AW_CACHE_DIR="${D}" \
  GH_AW_MIN_INTEGRITY="none" \
    bash "${SCRIPT}" 2>&1
)"
EXIT_CODE=$?
set -e
assert "legacy nested layout exits successfully" \
  "[ '${EXIT_CODE}' -eq 0 ]"
assert "legacy nested file moved to cache root" \
  "[ -f '${D}/chaos-pr-bundle-fuzzer.json' ]"
assert "legacy nested directory removed" \
  "[ ! -d '${D}/$(basename "${D}")' ]"
assert "flattening message logged" \
  "printf '%s' \"${OUTPUT}\" | grep -q 'Flattening legacy nested cache directory'"
echo ""

# ── Test 13: Corrupted git metadata is healed automatically ───────────────────
echo "Test 13: Corrupted git metadata is reinitialized"
D="${WORKSPACE}/test13"
make_cache_dir "${D}" "data.json"
pushd "${D}" >/dev/null
TREE_OBJ="$(git rev-parse HEAD^{tree})"
TREE_OBJ_PATH=".git/objects/${TREE_OBJ:0:2}/${TREE_OBJ:2}"
if [ ! -f "${TREE_OBJ_PATH}" ]; then
  echo "  ✗ expected loose tree object to exist at ${TREE_OBJ_PATH}"
  TESTS_FAILED=$((TESTS_FAILED + 1))
else
  rm -f "${TREE_OBJ_PATH}"
fi
popd >/dev/null

DIAGNOSTIC_DIRS_BEFORE="$(find /tmp -maxdepth 1 -name 'gh-aw-cache-memory.*' -print | sort)"
set +e
OUTPUT="$(run_script "${D}" none)"
EXIT_CODE=$?
set -e
DIAGNOSTIC_DIRS_AFTER="$(find /tmp -maxdepth 1 -name 'gh-aw-cache-memory.*' -print | sort)"
assert "corrupted repo exits successfully" \
  "[ '${EXIT_CODE}' -eq 0 ]"
assert "corruption warning logged" \
  "printf '%s' \"${OUTPUT}\" | grep -qi 'Detected corrupted cache-memory git repository'"
assert "git metadata recreated" \
  "[ -d '${D}/.git' ]"
assert "restored file preserved after recovery" \
  "[ -f '${D}/data.json' ]"
assert "integrity branch exists after recovery" \
  "git -C '${D}' rev-parse --verify none >/dev/null 2>&1"
assert "fsck diagnostics do not use predictable paths" \
  "! compgen -G '/tmp/gh-aw-git-fsck-*' >/dev/null"
assert "private diagnostic directory is removed after recovery" \
  '[[ "${DIAGNOSTIC_DIRS_AFTER}" == "${DIAGNOSTIC_DIRS_BEFORE}" ]]'
echo ""

# ── Test 14: Missing HEAD corruption is healed before hook config ─────────────
echo "Test 14: Missing HEAD corruption is reinitialized"
D="${WORKSPACE}/test14"
make_cache_dir "${D}" "data.json"
rm -f "${D}/.git/HEAD"
set +e
OUTPUT="$(run_script "${D}" none)"
EXIT_CODE=$?
set -e
assert "missing HEAD exits successfully" \
  "[ '${EXIT_CODE}' -eq 0 ]"
assert "corruption warning logged for missing HEAD" \
  "printf '%s' \"${OUTPUT}\" | grep -qi 'Detected corrupted cache-memory git repository'"
assert "git metadata recreated after missing HEAD" \
  "[ -d '${D}/.git' ]"
assert "restored file preserved after missing HEAD recovery" \
  "[ -f '${D}/data.json' ]"
assert "integrity branch exists after missing HEAD recovery" \
  "git -C '${D}' rev-parse --verify none >/dev/null 2>&1"
echo ""

# ── Test 15: Daily SPDD rotation directory preflight is created and writable ──
echo "Test 15: Daily SPDD rotation cache directory is writable"
D="${WORKSPACE}/test15"
make_cache_dir "${D}" "data.json"
set +e
OUTPUT="$(run_script "${D}" none)"
EXIT_CODE=$?
set -e
assert "preflight exits successfully" \
  "[ '${EXIT_CODE}' -eq 0 ]"
assert "spdd-daily directory created" \
  "[ -d '${D}/spdd-daily' ]"
assert "spdd-daily directory writable" \
  "[ -w '${D}/spdd-daily' ]"
assert "preflight success message logged" \
  "printf '%s' \"${OUTPUT}\" | grep -q 'Cache memory preflight write checks passed'"
echo ""

# ── Summary ──────────────────────────────────────────────────────────────────
echo "Tests passed: ${TESTS_PASSED}"
echo "Tests failed: ${TESTS_FAILED}"

if [ "${TESTS_FAILED}" -gt 0 ]; then
  exit 1
fi

echo "✓ All tests passed!"
