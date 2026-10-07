#!/usr/bin/env bash
set +o histexpand

# Test script for clean_git_credentials.sh
# Run: bash clean_git_credentials_test.sh

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLEAN_SCRIPT="${SCRIPT_DIR}/clean_git_credentials.sh"
VERIFY_SCRIPT="${SCRIPT_DIR}/verify_git_credentials.sh"

# Test counters
TESTS_PASSED=0
TESTS_FAILED=0

# Temporary workspace for tests
TEST_WORKSPACE=$(mktemp -d)

cleanup() {
  rm -rf "${TEST_WORKSPACE}"
}
trap cleanup EXIT

# Helper: create a minimal git repo with a .git/config file
make_git_config() {
  local dir="$1"
  local config="$2"
  mkdir -p "${dir}/.git"
  echo "${config}" >"${dir}/.git/config"
}

# Helper: assert a condition
assert() {
  local name="$1"
  local condition="$2"
  if eval "${condition}"; then
    echo "✓ ${name}"
    TESTS_PASSED=$((TESTS_PASSED + 1))
  else
    echo "✗ ${name}"
    TESTS_FAILED=$((TESTS_FAILED + 1))
  fi
}

echo "Testing clean_git_credentials.sh..."
echo ""

# ── Test 1: No .git/config anywhere (no-op, exit 0) ─────────────────────────
echo "Test 1: No .git/config files → exit 0 with informational message"
EMPTY_WS=$(mktemp -d)
OUTPUT=$(GITHUB_WORKSPACE="${EMPTY_WS}" bash "${CLEAN_SCRIPT}" 2>&1)
EXIT_CODE=$?
rmdir "${EMPTY_WS}"
assert "exits 0 when no .git/config found" "[ ${EXIT_CODE} -eq 0 ]"
assert "prints informational message" "echo '${OUTPUT}' | grep -q 'No .git/config'"
echo ""

# ── Test 2: Removes [credential] section ────────────────────────────────────
echo "Test 2: Removes [credential] section from workspace .git/config"
REPO="${TEST_WORKSPACE}/repo2"
make_git_config "${REPO}" "[core]
	repositoryformatversion = 0
[credential]
	helper = store
[remote \"origin\"]
	url = https://github.com/org/repo.git"
GITHUB_WORKSPACE="${REPO}" bash "${CLEAN_SCRIPT}" >/dev/null 2>&1
assert "credential section removed" "! grep -q '\[credential\]' '${REPO}/.git/config'"
assert "core section preserved"    "grep -q '\[core\]' '${REPO}/.git/config'"
assert "remote section preserved"  "grep -q '\[remote' '${REPO}/.git/config'"
echo ""

# ── Test 3: Removes http.extraheader ────────────────────────────────────────
echo "Test 3: Removes http.extraheader from git config"
REPO="${TEST_WORKSPACE}/repo3"
make_git_config "${REPO}" "[core]
	repositoryformatversion = 0
[http]
	extraheader = Authorization: Basic dXNlcjpwYXNz"
GITHUB_WORKSPACE="${REPO}" bash "${CLEAN_SCRIPT}" >/dev/null 2>&1
assert "http.extraheader removed" "! git config --file '${REPO}/.git/config' http.extraheader 2>/dev/null"
assert "config still valid"       "git config --file '${REPO}/.git/config' --list >/dev/null 2>&1"
echo ""

# ── Test 4: Strips credentials from remote URL ──────────────────────────────
echo "Test 4: Strips credentials from authenticated remote URL"
REPO="${TEST_WORKSPACE}/repo4"
make_git_config "${REPO}" "[core]
	repositoryformatversion = 0
[remote \"origin\"]
	url = https://x-access-token:ghs_abc123@github.com/org/repo.git
	fetch = +refs/heads/*:refs/remotes/origin/*"
GITHUB_WORKSPACE="${REPO}" bash "${CLEAN_SCRIPT}" >/dev/null 2>&1
CLEANED_URL=$(git config --file "${REPO}/.git/config" remote.origin.url)
assert "credentials stripped from URL" "[ '${CLEANED_URL}' = 'https://github.com/org/repo.git' ]"
echo ""

# ── Test 5: Recursively finds repo nested inside workspace ──────────────────
echo "Test 5: Recursively cleans nested git repo inside workspace"
OUTER="${TEST_WORKSPACE}/outer5"
INNER="${OUTER}/vendor/dep"
make_git_config "${OUTER}" "[core]
	repositoryformatversion = 0
[credential]
	helper = store"
make_git_config "${INNER}" "[core]
	repositoryformatversion = 0
[http]
	extraheader = Authorization: Basic dXNlcjpwYXNz"
GITHUB_WORKSPACE="${OUTER}" bash "${CLEAN_SCRIPT}" >/dev/null 2>&1
assert "outer credential section cleaned" "! grep -q '\[credential\]' '${OUTER}/.git/config'"
assert "inner extraheader cleaned"         "! git config --file '${INNER}/.git/config' http.extraheader 2>/dev/null"
echo ""

# ── Test 6: Finds git repo in /tmp ──────────────────────────────────────────
echo "Test 6: Cleans git repo located in /tmp"
TMP_REPO=$(mktemp -d)
make_git_config "${TMP_REPO}" "[core]
	repositoryformatversion = 0
[credential]
	helper = store"
# Use a workspace that does NOT contain the repo so it is found only via /tmp
GITHUB_WORKSPACE="${TEST_WORKSPACE}/workspace6"
mkdir -p "${GITHUB_WORKSPACE}"
GITHUB_WORKSPACE="${GITHUB_WORKSPACE}" bash "${CLEAN_SCRIPT}" >/dev/null 2>&1
assert "tmp repo credential section cleaned" "! grep -q '\[credential\]' '${TMP_REPO}/.git/config'"
rm -rf "${TMP_REPO}"
echo ""

# ── Test 7: Config file remains valid after all cleanups ────────────────────
echo "Test 7: Config file is still valid after cleaning"
REPO="${TEST_WORKSPACE}/repo7"
make_git_config "${REPO}" "[core]
	repositoryformatversion = 0
[credential]
	helper = /usr/lib/git-credential-gnome-keyring
[http]
	extraheader = Authorization: Bearer sometoken
[remote \"origin\"]
	url = https://oauth2:tok@github.com/org/repo.git
	fetch = +refs/heads/*:refs/remotes/origin/*"
GITHUB_WORKSPACE="${REPO}" bash "${CLEAN_SCRIPT}" >/dev/null 2>&1
assert "config still valid" "git config --file '${REPO}/.git/config' --list >/dev/null 2>&1"
assert "core settings intact" "git config --file '${REPO}/.git/config' core.repositoryformatversion >/dev/null 2>&1"
echo ""

# ── Test 8: Cleans submodule git configs under .git/modules ─────────────────
echo "Test 8: Cleans submodule git config files"
REPO="${TEST_WORKSPACE}/repo8"
mkdir -p "${REPO}/.git/modules/sub" "${REPO}/.git/modules/sub/modules/nested"
cat >"${REPO}/.git/modules/sub/config" <<'EOF'
[core]
	repositoryformatversion = 0
[credential "https://github.com"]
	helper = store
[http "https://github.com/"]
	extraheader = Authorization: Basic abc123
[remote "origin"]
	url = https://x-access-token:ghs_sub@github.com/org/sub.git
EOF
cat >"${REPO}/.git/modules/sub/modules/nested/config" <<'EOF'
[core]
	repositoryformatversion = 0
[credential]
	helper = store
[http]
	extraheader = Authorization: Basic def456
EOF
GITHUB_WORKSPACE="${REPO}" bash "${CLEAN_SCRIPT}" >/dev/null 2>&1
assert "submodule credential section removed" "! grep -q '\[credential' '${REPO}/.git/modules/sub/config'"
assert "submodule url extraheader removed" "! git config --file '${REPO}/.git/modules/sub/config' 'http.https://github.com/.extraheader' 2>/dev/null"
assert "submodule remote URL credentials stripped" "[ \"\$(git config --file '${REPO}/.git/modules/sub/config' remote.origin.url)\" = 'https://github.com/org/sub.git' ]"
assert "nested submodule credential section removed" "! grep -q '\[credential' '${REPO}/.git/modules/sub/modules/nested/config'"
assert "nested submodule http.extraheader removed" "! git config --file '${REPO}/.git/modules/sub/modules/nested/config' http.extraheader 2>/dev/null"
echo ""

# ── Test 9: Checkout includeIf credentials in repos and submodules ───────────
echo "Test 9: Removes checkout includeIf credentials without touching other includes"
REPO="${TEST_WORKSPACE}/repo9"
RUNNER_TEMP="${TEST_WORKSPACE}/runner-temp"
mkdir -p "${RUNNER_TEMP}" "${REPO}/.git/modules/sub"
git init -q "${REPO}"
git init --bare -q "${REPO}/.git/modules/sub"
printf '[core]\n\tfilemode = true\n' >"${TEST_WORKSPACE}/git-credentials-deadbeef.config"
printf '[http "https://github.com/"]\n\textraheader = Authorization: Basic abc123\n' >"${RUNNER_TEMP}/git-credentials-abc123.config"
printf '[core]\n\tignorecase = true\n' >"${RUNNER_TEMP}/other.config"
git config --file "${REPO}/.git/config" --add "includeIf.gitdir:${REPO}/.git.path" "${RUNNER_TEMP}/git-credentials-abc123.config"
git config --file "${REPO}/.git/config" --add "includeIf.gitdir:${REPO}/.git.path" "${RUNNER_TEMP}/git-credentials-abc123.config"
git config --file "${REPO}/.git/config" --add "includeIf.gitdir:/github/workspace/.git.path" "${RUNNER_TEMP}/git-credentials-abc123.config"
git config --file "${REPO}/.git/config" --add "includeIf.gitdir:${REPO}/.git/worktrees/*.path" "${TEST_WORKSPACE}/git-credentials-deadbeef.config"
git config --file "${REPO}/.git/config" --add "includeIf.gitdir:${REPO}/.git.path" "${RUNNER_TEMP}/other.config"
git config --file "${REPO}/.git/modules/sub/config" --add "includeIf.gitdir:${REPO}/.git/modules/sub.path" "${RUNNER_TEMP}/git-credentials-abc123.config"
assert "verifier detects included credentials before cleanup" "! RUNNER_TEMP='${RUNNER_TEMP}' bash '${VERIFY_SCRIPT}' '${REPO}/.git/config' >/dev/null 2>&1"
RUNNER_TEMP="${RUNNER_TEMP}" GITHUB_WORKSPACE="${REPO}" bash "${CLEAN_SCRIPT}" >/dev/null 2>&1
assert "verifier accepts repository and submodule after cleanup" "RUNNER_TEMP='${RUNNER_TEMP}' bash '${VERIFY_SCRIPT}' '${REPO}/.git/config' '${REPO}/.git/modules/sub/config' >/dev/null 2>&1"
assert "credential include references removed" "! git config --file '${REPO}/.git/config' --get-regexp '^includeif\\..*\\.path$' | grep -q 'git-credentials-abc123.config'"
assert "worktree credential include removed" "! git config --file '${REPO}/.git/config' --get-regexp '^includeif\\..*\\.path$' | grep -q 'git-credentials-deadbeef.config'"
assert "submodule credential include removed" "! git config --file '${REPO}/.git/modules/sub/config' --get-regexp '^includeif\\..*\\.path$' 2>/dev/null"
assert "runner temp credential file removed" "[ ! -e '${RUNNER_TEMP}/git-credentials-abc123.config' ]"
assert "credential file outside runner temp retained" "[ -f '${TEST_WORKSPACE}/git-credentials-deadbeef.config' ]"
assert "unrelated include preserved" "git --git-dir='${REPO}/.git' config --file '${REPO}/.git/config' --includes core.ignorecase | grep -q true"
echo ""

# ── Test 10: Include cleanup iteration boundary ────────────────────────────
echo "Test 10: Include cleanup succeeds at the iteration limit"
REPO="${TEST_WORKSPACE}/repo10"
make_git_config "${REPO}" '[includeIf "gitdir:/workspace/.git"]'
for ((i = 0; i < 1000; i++)); do
  printf '\tpath = /nonexistent/git-credentials-abc123.config\n' >>"${REPO}/.git/config"
done
assert "cleanup succeeds at 1000 includes" "GITHUB_WORKSPACE='${REPO}' bash '${CLEAN_SCRIPT}' >/dev/null 2>&1"
assert "all credential includes removed at limit" "! git config --file '${REPO}/.git/config' --get-regexp '^includeif\\..*\\.path$' 2>/dev/null"
echo ""

# ── Test 11: Include cleanup iteration limit exceeded ──────────────────────
echo "Test 11: Include cleanup fails when the iteration limit is exceeded"
make_git_config "${REPO}" '[includeIf "gitdir:/workspace/.git"]'
for ((i = 0; i < 1001; i++)); do
  printf '\tpath = /nonexistent/other.config\n' >>"${REPO}/.git/config"
done
printf '\tpath = /nonexistent/git-credentials-abc123.config\n' >>"${REPO}/.git/config"
EXIT_CODE=0
OUTPUT=$(GITHUB_WORKSPACE="${REPO}" bash "${CLEAN_SCRIPT}" 2>&1) || EXIT_CODE=$?
assert "cleanup exits 1 when limit exceeded" "[ ${EXIT_CODE} -eq 1 ]"
assert "reports iteration limit without include values" "echo '${OUTPUT}' | grep -q 'ERROR: Checkout credential include cleanup exceeded 1000 iterations'"
assert "credential include beyond limit is not processed" "git config --file '${REPO}/.git/config' --get-regexp '^includeif\\..*\\.path$' | grep -q 'git-credentials-abc123.config'"
rm -rf "${REPO}"
echo ""

# ── Summary ──────────────────────────────────────────────────────────────────
echo "Tests passed: ${TESTS_PASSED}"
echo "Tests failed: ${TESTS_FAILED}"

if [ "${TESTS_FAILED}" -gt 0 ]; then
  exit 1
fi

echo "✓ All tests passed!"
