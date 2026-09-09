#!/bin/bash
set -euo pipefail

REPO_ROOT="$(pwd)"

while [[ $# -gt 0 ]]; do
    case "$1" in
        --repo-root)
            REPO_ROOT="${2:?--repo-root requires an argument}"
            shift 2
            ;;
        *)
            echo "ERROR: unknown argument: $1" >&2
            echo "Usage: check-skill-file-paths.sh [--repo-root <path>]" >&2
            exit 1
            ;;
    esac
done

SKILL_ROOT="$REPO_ROOT/.github/skills"
if [[ ! -d "$SKILL_ROOT" ]]; then
    echo "ERROR: skill directory not found: $SKILL_ROOT" >&2
    exit 1
fi

# Extract backtick-delimited strings from SKILL.md files and validate the ones that
# look like code-path references in the repo. This guard is intentionally narrow:
# it catches stale repo-relative paths in the code and action source tree without
# flagging documentation examples, package names, or wildcard/glob examples.
invalid=()
while IFS= read -r -d '' file; do
    while IFS= read -r candidate; do
        [[ -z "$candidate" ]] && continue
        candidate="${candidate#@}"
        [[ "$candidate" == *"://"* ]] && continue
        [[ "$candidate" == *" "* ]] && continue
        [[ "$candidate" == *'('* || "$candidate" == *')'* ]] && continue
        [[ "$candidate" == *'{'* || "$candidate" == *'}'* ]] && continue
        [[ "$candidate" == *'['* || "$candidate" == *']'* ]] && continue
        [[ "$candidate" == *'"'* || "$candidate" == *"'"* ]] && continue
        [[ "$candidate" == *"<"* || "$candidate" == *">"* || "$candidate" == *"*"* || "$candidate" == *"?"* ]] && continue
        [[ "$candidate" == *"..."* ]] && continue

        if [[ "$candidate" == pkg/* ]]; then
            :
        elif [[ "$candidate" == scripts/* ]]; then
            :
        elif [[ "$candidate" == internal/* || "$candidate" == cmd/* || "$candidate" == eslint-factory/* ]]; then
            :
        elif [[ "$candidate" == .github/skills/* ]]; then
            :
        elif [[ "$candidate" =~ ^actions/.+\.(cjs|js|mjs|ts|md|yaml|yml)$ || "$candidate" =~ ^actions/.+/src/.+ || "$candidate" =~ ^actions/.+/index\.(cjs|js|mjs)$ ]]; then
            :
        else
            continue
        fi

        repo_path="$candidate"
        if [[ "$repo_path" == /* ]]; then
            repo_path="${repo_path#/}"
        fi

        if [[ ! -e "$REPO_ROOT/$repo_path" ]]; then
            invalid+=("$file: $candidate")
        fi
    done < <(python3 - "$file" <<'PY'
import pathlib, re, sys
path = pathlib.Path(sys.argv[1])
text = path.read_text(encoding='utf-8', errors='ignore')
for match in re.findall(r'`([^`]+)`', text):
    if not match or '://' in match:
        continue
    if any(ch.isspace() for ch in match):
        continue
    if any(ch in match for ch in "()[]{}'\"<>*?"):
        continue
    if '...' in match:
        continue
    if match.startswith('@'):
        match = match[1:]
    if '/' in match or match.startswith('.'):
        print(match)
PY
)
done < <(find "$SKILL_ROOT" -type f -name 'SKILL.md' -print0 | sort -z)

if [[ ${#invalid[@]} -gt 0 ]]; then
    echo "ERROR: invalid repo paths referenced in skill docs:" >&2
    printf '%s\n' "${invalid[@]}" | sort -u >&2
    echo >&2
    echo "Fix the backticked file path so it matches a real repository file or directory." >&2
    exit 1
fi

echo "✓ All backticked repo paths in skill docs exist."
