#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="${SCRIPT_DIR}/generate_git_patch.sh"
WORKSPACE="$(mktemp -d)"
trap 'rm -rf -- "$WORKSPACE"' EXIT

REPOSITORY="$WORKSPACE/repository"
PATCH_DIR="$WORKSPACE/patches"
SUMMARY="$WORKSPACE/summary.md"
SAFE_OUTPUTS="$WORKSPACE/safe-outputs.jsonl"
mkdir -p "$REPOSITORY" "$PATCH_DIR"
git -C "$REPOSITORY" init -q -b main
git -C "$REPOSITORY" config user.email "test@example.com"
git -C "$REPOSITORY" config user.name "Test"
printf 'base\n' > "$REPOSITORY/file.txt"
git -C "$REPOSITORY" add file.txt
git -C "$REPOSITORY" commit -qm base
BASE_SHA="$(git -C "$REPOSITORY" rev-parse HEAD)"
git -C "$REPOSITORY" checkout -qb feature
printf 'change\n' >> "$REPOSITORY/file.txt"
git -C "$REPOSITORY" commit -qam 'feature change'
touch "$SUMMARY" "$SAFE_OUTPUTS"

run_generator() {
  local patch_dir="${1:-$PATCH_DIR}"
  (
    cd "$REPOSITORY"
    GITHUB_SHA="$BASE_SHA" \
      DEFAULT_BRANCH=main \
      GH_AW_SAFE_OUTPUTS="$SAFE_OUTPUTS" \
      GH_AW_TMP_DIR="$patch_dir" \
      GITHUB_STEP_SUMMARY="$SUMMARY" \
      bash "$SCRIPT" >/dev/null 2>&1
  )
}

run_generator
test -f "$PATCH_DIR/aw-feature.patch"
grep -q 'feature change' "$PATCH_DIR/aw-feature.patch"

rm "$PATCH_DIR/aw-feature.patch"
printf 'external sentinel\n' > "$WORKSPACE/sentinel"
ln -s "$WORKSPACE/sentinel" "$PATCH_DIR/aw-feature.patch"
if run_generator; then
  echo "Expected symlinked patch destination to be rejected" >&2
  exit 1
fi
test "$(cat "$WORKSPACE/sentinel")" = "external sentinel"
test -L "$PATCH_DIR/aw-feature.patch"

ln -s "$PATCH_DIR" "$WORKSPACE/patch-link"
if run_generator "$WORKSPACE/patch-link///"; then
  echo "Expected symlinked patch directory with trailing slashes to be rejected" >&2
  exit 1
fi

rm "$PATCH_DIR/aw-feature.patch"
ln -s "$WORKSPACE/sentinel" "$WORKSPACE/summary-link"
if (
  cd "$REPOSITORY"
  GITHUB_SHA="$BASE_SHA" \
    DEFAULT_BRANCH=main \
    GH_AW_SAFE_OUTPUTS="$SAFE_OUTPUTS" \
    GH_AW_TMP_DIR="$PATCH_DIR" \
    GITHUB_STEP_SUMMARY="$WORKSPACE/summary-link" \
    bash "$SCRIPT" >/dev/null 2>&1
); then
  echo "Expected symlinked step summary to be rejected" >&2
  exit 1
fi
test "$(cat "$WORKSPACE/sentinel")" = "external sentinel"

echo "Git patch symlink tests passed"
