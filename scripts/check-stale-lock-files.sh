#!/bin/bash
set +o histexpand

# check-stale-lock-files.sh - Lightweight guard for stale .lock.yml files
#
# Detects workflow .md files that changed without their compiled .lock.yml being
# regenerated.
#
# Unlike check-workflow-drift.sh, this script does not require the gh-aw binary:
# it uses git to identify modified .md files and checks whether each has a
# corresponding .lock.yml that was also modified.  Use check-workflow-drift.sh
# for a thorough recompile-based verification; use this script as a fast early
# gate that catches the obvious case where a .md was edited and not recompiled.
#
# By default, the script inspects staged/unstaged changes in the current working
# tree. With --base-ref, it instead compares HEAD to a base ref, which is useful
# for CI runs on a clean checkout.
#
# Usage:
#   check-stale-lock-files.sh [--dir <workflows-dir>] [--base-ref <git-ref>]
#
# Options:
#   --dir <dir>   Workflows directory to scan (default: .github/workflows).
#                 The script only examines .md files under this directory.
#   --base-ref <git-ref>
#                 Git base ref used to detect changed files via
#                 `git diff <base-ref>...HEAD`. Intended for CI.
#
# Exit codes:
#   0 - No modified .md files detected, or every modified .md has a
#       correspondingly modified .lock.yml
#   1 - One or more modified .md files lack an up-to-date .lock.yml

set -euo pipefail

# Disable colors when not connected to a TTY, when NO_COLOR is set, or when
# TERM=dumb — this keeps output readable when captured into CI step summaries.
if [ -t 1 ] && [ -z "${NO_COLOR:-}" ] && [ "${TERM:-}" != "dumb" ]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[1;33m'
    NC='\033[0m'
else
    RED=''
    GREEN=''
    YELLOW=''
    NC=''
fi

WORKFLOWS_DIR=".github/workflows"
BASE_REF=""

while [[ $# -gt 0 ]]; do
    case "$1" in
        --dir)
            WORKFLOWS_DIR="${2:?--dir requires an argument}"
            shift 2
            ;;
        --base-ref)
            BASE_REF="${2:?--base-ref requires an argument}"
            shift 2
            ;;
        *)
            echo -e "${RED}ERROR${NC}: unknown argument: $1" >&2
            echo "Usage: check-stale-lock-files.sh [--dir <workflows-dir>] [--base-ref <git-ref>]" >&2
            exit 1
            ;;
    esac
done

if [ ! -d "$WORKFLOWS_DIR" ]; then
    echo -e "${RED}ERROR${NC}: workflows directory not found: $WORKFLOWS_DIR" >&2
    exit 1
fi

collect_modified_files() {
    # Compare the merge-base to the current working tree so committed, staged,
    # and unstaged branch changes are all covered.
    if [ -n "$BASE_REF" ]; then
        if git rev-parse --verify "${BASE_REF}^{commit}" >/dev/null 2>&1; then
            merge_base=$(git merge-base "$BASE_REF" HEAD 2>/dev/null || true)
            if [ -n "$merge_base" ]; then
                git diff --name-only "$merge_base" 2>/dev/null || true
                git ls-files --others --exclude-standard
                return
            fi
        fi
        echo -e "${YELLOW}WARN${NC}: --base-ref not found (${BASE_REF}); falling back to working-tree check." >&2
    fi

    # Local contributor path: staged/unstaged and untracked files vs HEAD.
    git diff --name-only HEAD 2>/dev/null || true
    git ls-files --others --exclude-standard
}

all_modified=$(collect_modified_files)

# Filter to .md files within the workflows directory.
# Strip a leading "./" from WORKFLOWS_DIR for consistent prefix matching.
# Exclude subdirectories whose files are compiled into parent workflow lock files
# rather than producing their own lock file (shared/, connectors/, and skills/).
workflows_prefix="${WORKFLOWS_DIR#./}"
modified_mds=$(printf '%s\n' "$all_modified" \
    | grep "^${workflows_prefix}.*\.md$" \
    | grep -v "^${workflows_prefix}/shared/" \
    | grep -v "^${workflows_prefix}/connectors/" \
    | grep -v "^${workflows_prefix}/skills/" \
    || true)

if [ -z "$modified_mds" ]; then
    echo -e "${GREEN}✓ No modified workflow markdown files detected.${NC}"
    exit 0
fi

stale_files=()
missing_locks=()

while IFS= read -r md; do
    [ -f "$md" ] || continue
    [ -n "$md" ] || continue
    lock="${md%.md}.lock.yml"

    if [ ! -f "$lock" ]; then
        missing_locks+=("$md")
    elif ! printf '%s\n' "$all_modified" | grep -Fx "$lock" >/dev/null; then
        stale_files+=("$md")
    fi
done <<< "$modified_mds"

if [ ${#stale_files[@]} -eq 0 ] && [ ${#missing_locks[@]} -eq 0 ]; then
    echo -e "${GREEN}✓ All modified workflow lock files are up to date.${NC}"
    exit 0
fi

echo ""

if [ ${#missing_locks[@]} -gt 0 ]; then
    echo -e "${RED}ERROR${NC}: The following workflow .md files have no compiled .lock.yml:"
    echo ""
    for f in "${missing_locks[@]}"; do
        echo "  $f"
    done
    echo ""
fi

if [ ${#stale_files[@]} -gt 0 ]; then
    echo -e "${RED}ERROR${NC}: The following workflow .md files were modified but their .lock.yml was not regenerated:"
    echo ""
    for f in "${stale_files[@]}"; do
        echo "  $f"
    done
    echo ""
fi

echo -e "${YELLOW}Fix:${NC} Recompile the workflow lock files, then commit them together with their .md sources:"
echo ""
echo "If you edited any .github/workflows/*.md file in this repository, run make recompile before committing so the matching .lock.yml files stay in sync."
echo ""
echo "  make recompile"
echo ""
exit 1
