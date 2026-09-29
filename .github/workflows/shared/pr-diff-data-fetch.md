---
# Shared pre-agent-steps for pre-fetching PR diff, metadata, and review comments.
# Works for both pull_request events and slash_command (issue) events on PRs.
#
# Outputs written to:
#   /tmp/gh-aw/agent/pr-diff.patch          — unified diff of non-generated files (up to 2000 lines)
#   /tmp/gh-aw/agent/pr-meta.json           — PR metadata (number, title, body, etc.)
#   /tmp/gh-aw/agent/pr-review-comments.json — existing inline review comments
#
# Skip-check: if cached diff and metadata exist and the cached head SHA matches,
# they are reused. Review comments are always refreshed.
#
# Usage:
#   cache:
#     key: pr-prefetch-${{ github.event.pull_request.head.sha || format('{0}-{1}', github.event.issue.number || github.event.pull_request.number || (fromJSON(github.event.inputs.aw_context || github.event.client_payload.aw_context || '{}').item_type == 'pull_request' && fromJSON(github.event.inputs.aw_context || github.event.client_payload.aw_context || '{}').item_number), github.run_id) }}
#     path: /tmp/gh-aw/agent
#     restore-keys:
#       - pr-prefetch-${{ github.event.pull_request.number || github.event.issue.number || (fromJSON(github.event.inputs.aw_context || github.event.client_payload.aw_context || '{}').item_type == 'pull_request' && fromJSON(github.event.inputs.aw_context || github.event.client_payload.aw_context || '{}').item_number) }}-
#   imports:
#     - shared/pr-diff-data-fetch.md

pre-agent-steps:
  - name: Pre-fetch PR diff and review comments
    env:
      GH_TOKEN: ${{ github.token }}
      PR_NUMBER: ${{ github.event.issue.number || github.event.pull_request.number || (fromJSON(github.event.inputs.aw_context || github.event.client_payload.aw_context || '{}').item_type == 'pull_request' && fromJSON(github.event.inputs.aw_context || github.event.client_payload.aw_context || '{}').item_number) }}
      PR_HEAD_SHA: ${{ github.event.pull_request.head.sha }}
      EXPR_GITHUB_REPOSITORY: ${{ github.repository }}
      PR_DIFF_MAX_LINES: "2000"
    run: |
      set -euo pipefail
      mkdir -p /tmp/gh-aw/agent
      if [ -z "$PR_NUMBER" ]; then
        echo "::error::Unable to determine the pull request number from the event context." >&2
        exit 1
      fi
      fetch_review_comments() {
        local tmp=/tmp/gh-aw/agent/pr-review-comments.json.tmp
        if gh api "repos/$EXPR_GITHUB_REPOSITORY/pulls/$PR_NUMBER/comments" \
            --paginate \
            --jq '.[] | {id, path, line: (.line // .original_line), body: .body[:200], user: .user.login}' \
            | jq -s '.' > "$tmp"; then
          mv "$tmp" /tmp/gh-aw/agent/pr-review-comments.json
        else
          rm -f "$tmp"
          echo "::warning::Failed to fetch existing review comments for PR #${PR_NUMBER}; continuing with an empty list (duplicate comments are possible)." >&2
          echo '[]' > /tmp/gh-aw/agent/pr-review-comments.json
        fi
      }
      CURRENT_HEAD_SHA="${PR_HEAD_SHA:-}"
      if [ -z "$CURRENT_HEAD_SHA" ]; then
        CURRENT_HEAD_SHA=$(gh pr view "$PR_NUMBER" --repo "$EXPR_GITHUB_REPOSITORY" --json headRefOid --jq '.headRefOid' 2>/dev/null || true)
      fi
      CACHE_HEAD_SHA=""
      if [ -f /tmp/gh-aw/agent/pr-data-head-sha.txt ]; then
        CACHE_HEAD_SHA="$(tr -d '\n' < /tmp/gh-aw/agent/pr-data-head-sha.txt)"
      fi
      fetch_review_comments
      # Skip diff and metadata fetch only when cache data matches current PR head commit.
      if [ -n "$CURRENT_HEAD_SHA" ] && [ "$CURRENT_HEAD_SHA" = "$CACHE_HEAD_SHA" ] && [ -f /tmp/gh-aw/agent/pr-diff.patch ] && [ -f /tmp/gh-aw/agent/pr-meta.json ]; then
        LINES=$(wc -l < /tmp/gh-aw/agent/pr-diff.patch)
        COMMENT_COUNT=$(jq 'length' /tmp/gh-aw/agent/pr-review-comments.json)
        echo "Cache hit: using pre-fetched PR data for head ${CURRENT_HEAD_SHA} (${LINES} diff lines, ${COMMENT_COUNT} review comments)"
      else
        # The whole-PR diff endpoint rejects PRs with more than 300 files, even
        # when gh pr diff --exclude would discard most of them afterward.
        # Match the repository's linguist-generated paths in .gitattributes.
        if ! gh api --paginate "repos/$EXPR_GITHUB_REPOSITORY/pulls/$PR_NUMBER/files?per_page=100" \
          --jq '.[] | select(.filename | test("(^|/)(generated|dist|build)/|\\.lock\\.yml$|^\\.changeset/[^/]+\\.md$|^\\.github/aw/(github-agentic-workflows\\.md|imports/)|^pkg/cli/templates/(campaign-[^/]*|create-agentic-workflow)\\.md$|^pkg/workflow/(js/[^/]+\\.(js|cjs)|sh/[^/]+\\.sh|\\.github/aw/imports/)|^actions/([^/]+/index\\.js|setup-cli/install\\.(sh|ps1))$|^specs/artifacts\\.md$") | not) | select(.patch != null) | "diff --git a/\(.previous_filename // .filename) b/\(.filename)\n" + (if .status == "added" then "new file mode 100644\n--- /dev/null\n+++ b/\(.filename)\n" elif .status == "removed" then "deleted file mode 100644\n--- a/\(.filename)\n+++ /dev/null\n" else "--- a/\(.previous_filename // .filename)\n+++ b/\(.filename)\n" end) + .patch + "\n"' \
          > /tmp/gh-aw/agent/pr-diff.full 2> /tmp/gh-aw/agent/pr-diff.err; then
          echo "::error::Failed to fetch files for PR #${PR_NUMBER}: $(cat /tmp/gh-aw/agent/pr-diff.err)" >&2
          rm -f /tmp/gh-aw/agent/pr-diff.full /tmp/gh-aw/agent/pr-diff.err
          exit 1
        fi
        head -n "${PR_DIFF_MAX_LINES}" /tmp/gh-aw/agent/pr-diff.full > /tmp/gh-aw/agent/pr-diff.patch
        rm -f /tmp/gh-aw/agent/pr-diff.full /tmp/gh-aw/agent/pr-diff.err
        LINES=$(wc -l < /tmp/gh-aw/agent/pr-diff.patch)
        gh pr view "$PR_NUMBER" \
          --repo "$EXPR_GITHUB_REPOSITORY" \
          --json number,title,body,headRefName,headRefOid,additions,deletions,changedFiles,files \
          > /tmp/gh-aw/agent/pr-meta.json
        if [ -z "$CURRENT_HEAD_SHA" ]; then
          CURRENT_HEAD_SHA="$(jq -r '.headRefOid // empty' /tmp/gh-aw/agent/pr-meta.json)"
        fi
        if [ -n "$CURRENT_HEAD_SHA" ]; then
          printf '%s\n' "$CURRENT_HEAD_SHA" > /tmp/gh-aw/agent/pr-data-head-sha.txt
        else
          rm -f /tmp/gh-aw/agent/pr-data-head-sha.txt
        fi
        COMMENT_COUNT=$(jq 'length' /tmp/gh-aw/agent/pr-review-comments.json)
        echo "Pre-fetched PR diff (${LINES} lines), metadata, and ${COMMENT_COUNT} existing review comments for head ${CURRENT_HEAD_SHA:-unknown}"
      fi
---

<!--
## PR Diff Data Fetch

Shared pre-agent-steps component used by PR reviewer workflows to pre-fetch PR
diff, metadata, and inline review comments before the agent starts.

### Why this shared component exists

Three reviewer workflows (pr-code-quality-reviewer, impeccable-skills-reviewer,
mattpocock-skills-reviewer) previously each duplicated identical pre-fetch shell
steps. Extracting them into this shared component eliminates the duplication and
ensures all three workflows use the same fetch logic and cache key, so that the
dedicated `pr-data-prefetch.yml` workflow can warm the cache once per commit
before the reviewer agents start.

### How the cache works

1. `pr-data-prefetch.yml` triggers simultaneously with the reviewer workflows on
   `pull_request: [ready_for_review]` events. Because it has no AI engine, it
   completes (and saves the `pr-prefetch-<sha>` Actions cache) in ~30–60 s.
2. Reviewer workflows' activation jobs take ~60–90 s; their agent jobs restore
   the cache before running `pre-agent-steps`.
3. When the cache is warm, this shared step detects the pre-fetched files and
   skips the diff and metadata calls. Review comments are always refreshed so
   duplicate detection uses current data.

### Output files

| File | Content |
|---|---|
| `/tmp/gh-aw/agent/pr-diff.patch` | Unified diff (linguist-generated/generated/dist/build excluded, capped at 2000 lines) |
| `/tmp/gh-aw/agent/pr-meta.json` | `number, title, body, headRefName, additions, deletions, changedFiles, files` |
| `/tmp/gh-aw/agent/pr-review-comments.json` | Array of `{id, path, line, body, user}` (body capped at 200 chars) |
-->
