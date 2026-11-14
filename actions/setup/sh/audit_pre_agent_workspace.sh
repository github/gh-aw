#!/usr/bin/env bash
set +o histexpand

#
# audit_pre_agent_workspace.sh - Capture a file listing of agent-related directories
# before the AI engine starts.
#
# This script runs after all pre-agent preparation (skills, agents, MCP servers) is
# complete and writes a complete file listing of agent-related directories to
# /tmp/gh-aw/pre-agent-audit.txt.  The listing is also surfaced via GITHUB_OUTPUT
# so downstream steps can reference it.
#
# Directories scanned (workspace — all agentic engines assumed active):
#   $GITHUB_WORKSPACE/.github/agents/       - workspace agent files (Copilot)
#   $GITHUB_WORKSPACE/.github/skills/       - workspace skill files (Copilot)
#   $GITHUB_WORKSPACE/.github/copilot/      - workspace Copilot config
#   $GITHUB_WORKSPACE/.claude/              - Claude engine config
#   $GITHUB_WORKSPACE/.codex/               - Codex engine config
#   $GITHUB_WORKSPACE/.gemini/              - Gemini engine config
#   $GITHUB_WORKSPACE/.crush/               - Crush engine config
#   $GITHUB_WORKSPACE/.opencode/            - OpenCode engine config
#   $GITHUB_WORKSPACE/.pi/                  - Pi engine config
#
# Directories scanned (user home):
#   $HOME/.github/                          - agent user home .github
#   $HOME/.claude/                          - Claude per-user config
#   $HOME/.gemini/                          - Gemini per-user config
#   $HOME/.local/share/gh/extensions/       - installed gh extensions
#   $RUNNER_TEMP/gh-aw/                     - runner temp gh-aw directory
#
# Common cache directories (node_modules, __pycache__, .cache, vendor, .npm, .yarn,
# .pnpm-store, site-packages, .bundle, tool-cache) are excluded to keep the listing concise.
# Exclusions use -prune so find does not descend into excluded trees.
#
# Environment variables (set automatically by GitHub Actions):
#   GITHUB_WORKSPACE   - path to the checked-out repository
#   HOME               - agent user home directory
#   RUNNER_TEMP        - runner temporary directory
#   GITHUB_OUTPUT      - path to the GitHub Actions output file
#   GITHUB_STEP_SUMMARY- path to the GitHub Actions step summary file (optional)
#
# GitHub Actions outputs written:
#   pre-agent-audit-file        - path to the audit file
#   pre-agent-audit-line-count  - number of lines in the audit file
#
# Exit codes:
#   0 - always (uses continue-on-error in the workflow step)

set -euo pipefail

AUDIT_FILE="/tmp/gh-aw/pre-agent-audit.txt"
if [ -L /tmp/gh-aw ]; then
  echo "::error::Refusing to use a symlinked gh-aw temporary directory" >&2
  exit 1
fi
mkdir -p /tmp/gh-aw
if [ -L /tmp/gh-aw ] || [ ! -d /tmp/gh-aw ]; then
  echo "::error::gh-aw temporary path is not a real directory" >&2
  exit 1
fi
if [ -L "$AUDIT_FILE" ] || [ -L "${GITHUB_OUTPUT}" ] || { [ -n "${GITHUB_STEP_SUMMARY:-}" ] && [ -L "${GITHUB_STEP_SUMMARY}" ]; }; then
  echo "::error::Refusing to write through a symlinked audit output path" >&2
  exit 1
fi
AUDIT_TMP="$(mktemp /tmp/gh-aw/.pre-agent-audit.XXXXXX)"
trap 'rm -f -- "$AUDIT_TMP"' EXIT

# list_dir prints a section header and runs find on the given directory,
# pruning common cache folders so find does not descend into them.
# Missing directories are silently noted.
list_dir() {
  local label="$1"
  local dir="$2"
  echo "--- ${label}: ${dir} ---"
  find -P "${dir}" \
    \( \
      -name 'node_modules' \
      -o -name '__pycache__' \
      -o -name '.cache' \
      -o -name 'vendor' \
      -o -name '.npm' \
      -o -name '.yarn' \
      -o -name '.pnpm-store' \
      -o -name 'site-packages' \
      -o -name '.bundle' \
      -o -name 'tool-cache' \
    \) -prune \
    -o -print \
    2>/dev/null || echo "(not found)"
}

{
  echo "=== Pre-agent workspace audit ==="

  echo "--- Copilot engine ---"
  list_dir "Workspace agents"        "${GITHUB_WORKSPACE}/.github/agents"
  list_dir "Workspace skills"        "${GITHUB_WORKSPACE}/.github/skills"
  list_dir "Workspace copilot"       "${GITHUB_WORKSPACE}/.github/copilot"

  echo "--- Engine config dirs ---"
  list_dir "Workspace claude"        "${GITHUB_WORKSPACE}/.claude"
  list_dir "Workspace codex"         "${GITHUB_WORKSPACE}/.codex"
  list_dir "Workspace gemini"        "${GITHUB_WORKSPACE}/.gemini"
  list_dir "Workspace crush"         "${GITHUB_WORKSPACE}/.crush"
  list_dir "Workspace opencode"      "${GITHUB_WORKSPACE}/.opencode"
  list_dir "Workspace pi"            "${GITHUB_WORKSPACE}/.pi"

  echo "--- User home ---"
  list_dir "Agent user home .github" "${HOME}/.github"
  list_dir "Agent user home .claude" "${HOME}/.claude"
  list_dir "Agent user home .gemini" "${HOME}/.gemini"
  list_dir "gh extensions"           "${HOME}/.local/share/gh/extensions"

  echo "--- Runner ---"
  list_dir "gh-aw temp directory"    "${RUNNER_TEMP}/gh-aw"
} > "${AUDIT_TMP}"

# Render the audit listing as a collapsed, compact tree in the step summary.
render_summary_tree() {
  local line
  local label
  echo "<details>"
  echo "<summary>Pre-agent workspace audit</summary>"
  echo
  echo '```text'
  while IFS= read -r line; do
    case "${line}" in
      "=== "*) continue ;;
      "--- "*": "*" ---")
        label="${line#--- }"
        echo "│   ├── ${label% ---}"
        ;;
      "--- "*" ---")
        label="${line#--- }"
        echo "├── ${label% ---}"
        ;;
      "(not found)") echo "│   │   └── ${line}" ;;
      "") continue ;;
      *) echo "│   │   └── ${line}" ;;
    esac
  done < "${AUDIT_TMP}"
  echo '```'
  echo "</details>"
}

LINE_COUNT="$(wc -l < "${AUDIT_TMP}" | tr -d ' ')"
render_summary_tree >> "${GITHUB_STEP_SUMMARY:-/dev/null}"
if [ -L "$AUDIT_FILE" ]; then
  echo "::error::Refusing to replace a symlinked audit file" >&2
  exit 1
fi
mv -f -- "$AUDIT_TMP" "$AUDIT_FILE"
echo "pre-agent-audit-file=${AUDIT_FILE}" >> "${GITHUB_OUTPUT}"
echo "pre-agent-audit-line-count=${LINE_COUNT}" >> "${GITHUB_OUTPUT}"
echo "Pre-agent audit written to ${AUDIT_FILE} (${LINE_COUNT} lines)"
