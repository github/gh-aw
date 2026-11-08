#!/bin/bash
set -euo pipefail

# Claude Code's "don't ask again" approvals can persist secrets in command rules:
# https://apiiro.com/blog/coding-agents-leaking-secrets-github-dont-ask-again
# Inspect the Git index, not file contents or ignore rules: force-added files and
# already-tracked files must fail without printing any stored credentials.
REPO_ROOT=$(git rev-parse --show-toplevel)
TRACKED_FILES=$(mktemp)
trap 'rm -f "$TRACKED_FILES"' EXIT

git -C "$REPO_ROOT" ls-files -z >"$TRACKED_FILES"

violations=0
while IFS= read -r -d '' file; do
    case "$file" in
        .claude/settings.local.json|*/.claude/settings.local.json)
            printf 'ERROR: Claude Code local permission settings must not be tracked: %q\n' "$file" >&2
            violations=$((violations + 1))
            ;;
    esac
done <"$TRACKED_FILES"

if [ "$violations" -ne 0 ]; then
    echo "Remove these files from the Git index with git rm --cached -- <path>, keeping local copies private." >&2
    echo "If credentials were already published, rotate them; deleting the file does not remove Git history or forks." >&2
    exit 1
fi

echo "PASS: No Claude Code local permission settings are tracked."
