#!/usr/bin/env bash
set -euo pipefail

usage() {
    cat <<'EOF'
Usage: resolve.sh [BASE_REF]
       resolve.sh --verify-markers

Merge BASE_REF (default: origin/main) without committing, then resolve
workflow lock-file-only conflicts by running make recompile.

If a merge is already in progress, BASE_REF is ignored and the current
conflicts are processed.

--verify-markers scans all .github/workflows/*.md source files (not just
files touched by the current diff) for leftover git conflict-marker lines
(<<<<<<<, |||||||, =======, >>>>>>>) and exits non-zero listing any hits.
Run it after resolving mixed conflicts and before compiling, and as a
standalone pre-flight check any time a source workflow file may have been
merged manually — `git diff --check` only inspects changed hunks and will
not catch markers already committed in unchanged file content.
EOF
}

verify_markers() {
    local repo_root
    repo_root=$(git rev-parse --show-toplevel 2>/dev/null) ||
        die "run this command inside a Git repository"
    cd "$repo_root"

    local pattern='^(<{7}|\|{7}|={7}|>{7})([^=<>|]|$)'
    local hits=0
    while IFS= read -r -d '' file; do
        local matches
        matches=$(grep -nE "$pattern" -- "$file" || true)
        if [[ -n $matches ]]; then
            hits=1
            echo "error: leftover conflict marker(s) in $file:" >&2
            echo "$matches" >&2
        fi
    done < <(find .github/workflows -maxdepth 1 -name '*.md' -print0 2>/dev/null)

    if (( hits )); then
        die "unresolved conflict markers found in workflow source; fix before compiling"
    fi
    echo "No conflict markers found in .github/workflows/*.md"
}

die() {
    echo "error: $*" >&2
    exit 1
}

if [[ ${1:-} == "--help" || ${1:-} == "-h" ]]; then
    usage
    exit 0
fi
if [[ ${1:-} == "--verify-markers" ]]; then
    verify_markers
    exit 0
fi
if (( $# > 1 )); then
    usage >&2
    exit 2
fi

base_ref=${1:-origin/main}
repo_root=$(git rev-parse --show-toplevel 2>/dev/null) ||
    die "run this command inside a Git repository"
cd "$repo_root"

merge_in_progress=false
if git rev-parse -q --verify MERGE_HEAD >/dev/null; then
    merge_in_progress=true
else
    git rev-parse --verify "${base_ref}^{commit}" >/dev/null 2>&1 ||
        die "base ref '$base_ref' is unavailable; fetch it first if credentials are available"

    if [[ -n $(git status --porcelain) ]]; then
        die "working tree must be clean before starting the merge"
    fi

    echo "Merging $base_ref without committing..."
    if ! git merge --no-edit --no-commit "$base_ref"; then
        if ! git rev-parse -q --verify MERGE_HEAD >/dev/null; then
            die "merge failed before creating a resolvable merge state"
        fi
        merge_in_progress=true
    fi
fi

mapfile -d '' conflicts < <(git diff --name-only --diff-filter=U -z)
non_lock_conflicts=()
for path in "${conflicts[@]}"; do
    if [[ ! $path =~ ^\.github/workflows/[^/]+\.lock\.yml$ ]]; then
        non_lock_conflicts+=("$path")
    fi
done

if (( ${#non_lock_conflicts[@]} > 0 )); then
    echo "Refusing automatic resolution; resolve these source conflicts first:" >&2
    printf '  %s\n' "${non_lock_conflicts[@]}" >&2
    exit 1
fi

if (( ${#conflicts[@]} > 0 )); then
    echo "Regenerating ${#conflicts[@]} conflicted workflow lock file(s)..."
else
    echo "No unresolved source conflicts; recompiling workflows..."
fi

# A manually resolved .md source conflict can silently leave behind a
# conflict-marker line (e.g. "|||||| base (original)") that git diff --check
# will not flag once staged/committed. Fail fast before compiling so the
# error surfaces here instead of during a later scheduled recompilation.
verify_markers

make recompile

if (( ${#conflicts[@]} > 0 )); then
    git add -- "${conflicts[@]}"
fi

mapfile -d '' regenerated_locks < <(
    git diff --name-only -z -- '.github/workflows/*.lock.yml'
)
if (( ${#regenerated_locks[@]} > 0 )); then
    git add -- "${regenerated_locks[@]}"
fi

mapfile -d '' remaining < <(git diff --name-only --diff-filter=U -z)
if (( ${#remaining[@]} > 0 )); then
    printf 'error: unresolved conflicts remain:\n' >&2
    printf '  %s\n' "${remaining[@]}" >&2
    exit 1
fi

git diff --check
git diff --cached --check

if [[ $merge_in_progress == true ]] ||
    git rev-parse -q --verify MERGE_HEAD >/dev/null; then
    echo "Resolved and staged generated lock conflicts. Review and commit the merge."
else
    echo "Merge and workflow recompilation complete. Review the working tree."
fi
