---
private: true
emoji: "👨‍🍳"
name: PR Sous Chef
description: Keeps open non-draft PRs moving toward maintainer investigation by posting targeted Copilot nudges
on:
  schedule: every 15m
  workflow_dispatch:
  slash_command:
    strategy: centralized
    name: souschef
    events: [pull_request_comment]
  skip-if-no-match: "is:pr is:open -is:draft -author:app/dependabot"
permissions:
  contents: read
  pull-requests: read
  issues: read
  actions: read
  copilot-requests: write

features:
  gh-aw-detection: true

checkout:
  fetch: ["refs/pulls/open/*"]
  fetch-depth: 0
network:
  allowed: ["defaults", "go"]
model: openai/gpt-5.4
engine:
  id: pi
  model-provider: openai
strict: true
imports:
  - shared/mcp-pagination.md
  - shared/otlp.md
  - shared/graders.md
tools:
  cli-proxy: true
  github:
    mode: gh-proxy
    min-integrity: approved
    toolsets: [pull_requests, repos, issues]
  edit:
  bash:
    - "*"
steps:
  - name: Fetch open non-draft PR queue
    id: fetch-prs
    env:
      GH_TOKEN: ${{ secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}
      EXPR_GITHUB_REPOSITORY: ${{ github.repository }}
    run: |
      mkdir -p /tmp/gh-aw/agent
      candidate_file=/tmp/gh-aw/agent/pr-sous-chef-candidates.json
      eligible_file=/tmp/gh-aw/agent/pr-sous-chef-eligible.json
      sous_chef_nudge_marker='<!-- gh-aw-pr-sous-chef-nudge -->'
      cooldown_seconds=1800
      filtered_checks_pending=0
      filtered_last_comment_from_sous_chef=0
      filtered_cooldown=0
      zero_diff_age_hours="${ZERO_DIFF_AGE_HOURS:-24}"
      if ! [[ "$zero_diff_age_hours" =~ ^[0-9]+$ ]]; then
        echo "Invalid ZERO_DIFF_AGE_HOURS; defaulting to 24" >&2
        zero_diff_age_hours=24
      fi

      # statusCheckRollup is fetched here alongside other PR fields so that the
      # per-PR pending-checks filter below can classify check state without
      # issuing individual REST calls for each PR.  Including this field in the
      # batch GraphQL query replaces up to 3 REST calls per PR (PR detail +
      # check-runs + commit-status) with zero additional REST calls.
      # gh pr list auto-paginates as needed to satisfy --limit.
      pr_limit="${PR_QUEUE_LIMIT:-200}"
      pr_list_retries=3
      pr_list_backoff_seconds=5
      pr_list_attempt=1
      pr_list_ok=0
      while [ "$pr_list_attempt" -le "$pr_list_retries" ]; do
        set +e
        gh pr list --repo "$EXPR_GITHUB_REPOSITORY" \
          --state open \
          --search "is:pr is:open -is:draft -author:app/dependabot -label:broccoli sort:updated-desc" \
          --limit "$pr_limit" \
          --json number,title,url,headRefOid,headRefName,createdAt,updatedAt,changedFiles,author,mergeStateStatus,statusCheckRollup \
          > "$candidate_file" 2>&1
        pr_list_status=$?
        set -e
        if [ "$pr_list_status" -eq 0 ]; then
          pr_list_ok=1
          break
        fi
        pr_list_error=$(cat "$candidate_file" 2>/dev/null || true)
        if echo "$pr_list_error" | grep -qiE 'HTTP 50[0234]|HTTP 429|Bad Gateway|timeout|temporarily unavailable|EOF'; then
          echo "Transient gh pr list failure on attempt $pr_list_attempt/$pr_list_retries; retrying: $pr_list_error" >&2
          if [ "$pr_list_attempt" -lt "$pr_list_retries" ]; then
            sleep "$pr_list_backoff_seconds"
          fi
          pr_list_attempt=$((pr_list_attempt + 1))
          continue
        fi
        if echo "$pr_list_error" | grep -qiE 'auth|forbidden|unauthorized|not logged in|token'; then
          echo "gh pr list authentication error; continuing with empty queue: $pr_list_error" >&2
          break
        fi
        echo "gh pr list failed; continuing with empty queue: $pr_list_error" >&2
        break
      done
      if [ "$pr_list_ok" -ne 1 ]; then
        jq -n '[]' > "$candidate_file"
      fi
      if ! jq -e 'type == "array" and all(.[]; (type == "object") and (.number != null) and (.title != null) and (.url != null))' "$candidate_file" >/dev/null 2>&1; then
        echo "gh pr list returned an invalid payload; continuing with empty queue" >&2
        jq -n '[]' > "$candidate_file"
      fi

      jq -n '[]' > "$eligible_file"

      while IFS= read -r pr; do
        pr_number="$(jq -r '.number' <<<"$pr")"
        if [ -z "$pr_number" ] || [ "$pr_number" = "null" ]; then
          continue
        fi

        # Determine pending-check state from the statusCheckRollup data already
        # fetched in the gh pr list call above — no per-PR REST calls needed.
        # CheckRun statuses are UPPERCASE in the GraphQL response.
        # WAITING check runs are left eligible here so the agent can inspect the
        # associated workflow run and approve it through the approve_workflow_run
        # safe output when it belongs to the allowed CJS/CGO/CWI workflows. Other
        # short-running pending checks still gate nudges.
        # Checks that have been running for more than 1 hour are ignored so that
        # long-running agentic checks (Q, coding agents) do not permanently block
        # nudges.  Short CI checks (< 1 hour) still gate nudges correctly.
        # Timestamp resolution order: startedAt → createdAt → absent.  Using
        # createdAt as a fallback means a check stuck in QUEUED (never started)
        # will also be ignored after 1 hour, preventing a stalled queue from
        # permanently blocking nudges.
        checks_pending="$(
          jq -r '
            (.statusCheckRollup // []) as $checks |
            (now - 3600) as $cutoff |
            if ($checks | any(
              if .__typename == "CheckRun" then
                ((.status // "COMPLETED") | IN("QUEUED", "IN_PROGRESS", "REQUESTED", "PENDING")) and
                ((.startedAt // .createdAt) as $ts |
                  $ts == null or (($ts | fromdateiso8601) > $cutoff))
              elif .__typename == "StatusContext" then
                (.state // "") == "PENDING"
              else false end
            )) then "true" else "false" end
          ' <<<"$pr"
        )"
        if [ "$checks_pending" = "true" ]; then
          filtered_checks_pending=$((filtered_checks_pending + 1))
          continue
        fi

        # Fetch the 10 most-recent issue comments once; used for the skip checks below.
        recent_comments_json="$(
          gh api "repos/$EXPR_GITHUB_REPOSITORY/issues/$pr_number/comments?per_page=10&sort=created&direction=desc" \
            2>/dev/null || echo "[]"
        )"

        # Skip if the very last comment was posted by pr-sous-chef (never add two in a row).
        # Only treat a sous-chef comment as actionable (and thus skip-worthy) if it also
        # contains "@copilot"; comments without "@copilot" are purely informational.
        last_comment_is_sous_chef="$(
          jq -r --arg marker "$sous_chef_nudge_marker" '
            if length == 0 then "false"
            elif (.[0].body // "" | (contains($marker) and contains("@copilot"))) then "true"
            else "false"
            end
          ' <<<"$recent_comments_json"
        )"
        # Exception: if the PR is in a CONFLICTING merge state, don't skip even when the last
        # comment is from sous-chef — it should still ask Copilot to resolve the conflict.
        merge_state_status="$(jq -r '.mergeStateStatus // ""' <<<"$pr")"
        if [ "$last_comment_is_sous_chef" = "true" ] && [ "$merge_state_status" != "CONFLICTING" ]; then
          filtered_last_comment_from_sous_chef=$((filtered_last_comment_from_sous_chef + 1))
          continue
        fi

        # Skip if pr-sous-chef commented within the last 30 minutes (cooldown period).
        # Only actionable sous-chef comments (those containing "@copilot") count toward cooldown;
        # informational comments without "@copilot" are ignored.
        last_sous_chef_comment_at="$(
          jq -r --arg marker "$sous_chef_nudge_marker" '
            [.[] | select(.body // "" | (contains($marker) and contains("@copilot")))] | .[0].created_at // ""
          ' <<<"$recent_comments_json"
        )"
        if [ -n "$last_sous_chef_comment_at" ]; then
          comment_epoch="$(date -d "$last_sous_chef_comment_at" +%s 2>/dev/null || echo 0)"
          current_epoch="$(date +%s)"
          if [ $(( current_epoch - comment_epoch )) -lt "$cooldown_seconds" ]; then
            filtered_cooldown=$((filtered_cooldown + 1))
            continue
          fi
        fi

        jq --argjson pr "$pr" '. + [$pr]' "$eligible_file" > "${eligible_file}.tmp" && mv "${eligible_file}.tmp" "$eligible_file"
      # Process substitution keeps the loop in the current shell so counters persist.
      done < <(jq -c '.[]' "$candidate_file")

      jq --argjson filtered_checks_pending "$filtered_checks_pending" \
         --argjson filtered_last_comment_from_sous_chef "$filtered_last_comment_from_sous_chef" \
         --argjson filtered_cooldown "$filtered_cooldown" \
         --argjson zero_diff_age_hours "$zero_diff_age_hours" '{
        fetched: (length),
        generated_at: (now | todate),
        filtered_checks_pending: $filtered_checks_pending,
        filtered_last_comment_from_sous_chef: $filtered_last_comment_from_sous_chef,
        filtered_cooldown: $filtered_cooldown,
        prs: map({
          number,
          title,
          url,
          headRefOid,
          headRefName,
          createdAt,
          updatedAt,
          changedFiles,
          zero_diff_stalled: (
            (.changedFiles // -1) == 0 and
            (.createdAt != null) and
            ((.createdAt | fromdateiso8601) <= (now - ($zero_diff_age_hours * 3600)))
          ),
          author: (.author.login // "unknown"),
          mergeStateStatus,
          failed_checks: ((.statusCheckRollup // []) | if type == "array" then . else [] end | map(select(
            if .__typename == "CheckRun" then
              ((.conclusion // "") | IN("FAILURE", "TIMED_OUT", "ACTION_REQUIRED", "STARTUP_FAILURE")) or
              ((.status // "") == "WAITING")
            elif .__typename == "StatusContext" then
              ((.state // "") | IN("FAILURE", "ERROR"))
            else false end
          )) | map({
            name: (if .__typename == "StatusContext" then (.context // "unknown") else (.name // "unknown") end),
            conclusion: (if .__typename == "CheckRun" and (.status // "") == "WAITING" then "ACTION_REQUIRED" else (.conclusion // .state // "unknown") end),
            url: (.detailsUrl // .targetUrl // null)
          }))
        })
      }' "$eligible_file" \
        > /tmp/gh-aw/agent/pr-sous-chef-candidates-compact.json
      eligible_count="$(jq '.prs | length' /tmp/gh-aw/agent/pr-sous-chef-candidates-compact.json || echo 0)"
      fetched_count="$(jq '.fetched' /tmp/gh-aw/agent/pr-sous-chef-candidates-compact.json || echo 0)"
      zero_diff_stalled_count="$(jq '[.prs[] | select(.zero_diff_stalled)] | length' /tmp/gh-aw/agent/pr-sous-chef-candidates-compact.json || echo 0)"
      eligible_pull_request_numbers="$(jq -c '[.prs[]?.number | tostring]' /tmp/gh-aw/agent/pr-sous-chef-candidates-compact.json || echo '[]')"
      {
        echo "eligible_count=$eligible_count"
        echo "zero_diff_stalled_count=$zero_diff_stalled_count"
        echo "eligible_pull_request_numbers=$eligible_pull_request_numbers"
      } >> "$GITHUB_OUTPUT"

      # Write prefilter summary to the step summary for visibility
      {
        echo "### 🍳 PR Sous Chef — Prefilter Results"
        echo ""
        echo "| Metric | Count |"
        echo "|---|---|"
        echo "| Candidates fetched | $fetched_count |"
        echo "| Filtered (checks pending) | $filtered_checks_pending |"
        echo "| Filtered (last comment from sous-chef) | $filtered_last_comment_from_sous_chef |"
        echo "| Filtered (cooldown) | $filtered_cooldown |"
        echo "| Zero-diff stalled (>${zero_diff_age_hours}h) | $zero_diff_stalled_count |"
        echo "| **Eligible for nudge** | **$eligible_count** |"
      } >> "$GITHUB_STEP_SUMMARY"
  - name: Setup Go
    if: steps.fetch-prs.outputs.eligible_count != '0'
    uses: actions/setup-go@v7.0.0
    with:
      go-version-file: go.mod
      cache: true
  - name: Setup Node.js
    if: steps.fetch-prs.outputs.eligible_count != '0'
    uses: actions/setup-node@v7.0.0
    with:
      node-version: "24"
      cache: npm
      cache-dependency-path: actions/setup/js/package-lock.json
  - name: Install formatter dependencies
    if: steps.fetch-prs.outputs.eligible_count != '0'
    run: npm ci --prefix actions/setup/js
jobs:
  approval_allowlist:
    needs: agent
    if: always() && needs.agent.result != 'skipped'
    runs-on: ubuntu-slim
    permissions:
      actions: read
    outputs:
      eligible_pull_request_numbers: ${{ steps.extract.outputs.eligible_pull_request_numbers }}
    steps:
      - name: Download agent artifact
        continue-on-error: true
        uses: actions/download-artifact@v8.0.1
        with:
          name: agent
          path: ${{ runner.temp }}/gh-aw
      - name: Extract eligible PR allowlist
        id: extract
        run: |
          candidate_file="$(find "${RUNNER_TEMP}/gh-aw" -path '*/pr-sous-chef-candidates-compact.json' -print -quit)"
          if [ -n "$candidate_file" ] && [ -f "$candidate_file" ]; then
            eligible_pull_request_numbers="$(jq -c '[.prs[]?.number | tostring]' "$candidate_file" || echo '[]')"
          else
            eligible_pull_request_numbers='[]'
          fi
          echo "eligible_pull_request_numbers=$eligible_pull_request_numbers" >> "$GITHUB_OUTPUT"
safe-outputs:
  needs: [approval_allowlist]
  add-comment:
    max: 4
    target: "*"
    github-token: ${{ secrets.AWI_MAINTENANCE_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}
  approve-workflow-run:
    max: 8
    allowed-workflows: [cjs.yml, cgo.yml, CWI.yml]
    allowed-pull-requests: ${{ needs.approval_allowlist.outputs.eligible_pull_request_numbers }}
    github-token: ${{ secrets.AWI_MAINTENANCE_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}
  resolve-pull-request-review-thread:
    max: 40
  dismiss-pull-request-review:
    max: 20
    target: "*"
  update-pull-request:
    title: false
    body: true
    operation: append
    update-branch: true
    sync-stack: true
    max: 10
    target: "*"
  push-to-pull-request-branch:
    target: "*"
    if-no-changes: ignore
    commit-title-suffix: " [pr-sous-chef]"
    excluded-files:
      - ".github/workflows/**"
      - "README.md"
      - "docs/src/content/docs/index.mdx"
    max: 10
  create-issue:
    title-prefix: "[pr-sous-chef] "
    labels: ["automation"]
    expires: 3d
    group-by-day: true
    close-older-issues: true
  mentions:
    allowed: ["@copilot"]
  noop:
  messages:
    run-started: "🍳 [{workflow_name}]({run_url}) is preparing PRs for maintainer investigation."
    run-success: "✅ [{workflow_name}]({run_url}) finished PR sous-chef nudges."
    run-failure: "⚠️ [{workflow_name}]({run_url}) {status} while preparing PRs."
timeout-minutes: 25
evals:
  - id: comment-added
    question: Did the agent add a comment to at least one pull request?
  - id: nudge-targeted
    question: Does the agent output show a specific reason why the selected PR needs a nudge toward maintainer investigation?
  - id: pr-evaluated
    question: Does the agent output confirm that it evaluated at least one open PR for nudge eligibility?
graders:
  execution-duration: {}
experiments:
  remove_redundant_context_v1:
    variants: [control, candidate]
    description: "Test removing the 'Required skip rules per PR' backup table from the main-agent prompt: the same three conditions are already deterministically enforced by the fetch-prs prefilter step and re-verified by the pr-processor sub-agent, so the table is redundant context on every run."
    hypothesis: "H0: No meaningful difference in execution-duration between control and candidate. H1: Removing the redundant backup skip-rules table decreases execution-duration without lowering the comment-added/pr-evaluated eval pass rate."
    metric: "grader:execution-duration"
    guardrail_metrics:
      - name: "eval:comment-added"
        threshold: ">=0.90"
      - name: "eval:pr-evaluated"
        threshold: ">=0.90"
    min_samples: 20
    analysis_type: mann_whitney
    decision:
      minimum_effect: 15000
      regression_tolerance: 15000
      confidence: 0.95
    tags: ["harness_dimension:context assembly", "harness_subtype:remove_redundant_context"]
---

# PR Sous Chef 🍳

You are **pr-sous-chef**, a lightweight PR progress assistant.

## Mission

Move open non-draft PRs toward a state where a maintainer can investigate quickly.

Do not modify the top-level `README.md` or `docs/src/content/docs/index.mdx`. These files contain primary project messaging that must only be changed by maintainers. If a formatter or branch-maintenance step would alter either file, leave that change out of the pushed update.

## Slash-command acknowledgement requirement (mandatory)

When this workflow is triggered by the `/souschef` slash command on a PR comment (`pull_request_comment` event), you must always post a comment on the same PR as that triggering comment.

1. Resolve the target PR number from event context (`github.aw.context.item_type == "pull_request"` with `github.aw.context.item_number`, or equivalent PR number in the event payload).
2. Before applying skip logic, call `add_comment` exactly once for that PR.
3. The comment body must include `<!-- gh-aw-pr-sous-chef-nudge -->` and a short acknowledgement that sous-chef was invoked and will triage the PR.
4. Do not skip this acknowledgement due to cooldown, pending checks, or duplicate-comment safeguards.
5. Every slash-command-triggered run must include this acknowledgement comment; if PR number cannot be resolved, call `report_incomplete` explaining the missing PR target.

## Token efficiency rules (mandatory)

1. Read `/tmp/gh-aw/agent/pr-sous-chef-candidates-compact.json` first.
2. If `prs` is empty, create the run-report issue (see **Run summary** below) and stop. If `create_issue` is unavailable, fall back to `noop` with the message `"processed=0; nudged=0; no eligible PRs"` and stop.
3. Process PRs in `updatedAt` descending order.
4. Process at most 4 nudges per run.
5. Prioritize which PRs to nudge, in this order:
   - `mergeStateStatus == "CONFLICTING"` first (explicit merge-conflict unblock request).
   - PRs with `zero_diff_stalled == true` next (no files changed after 24 hours by default).
   - PRs with unresolved review threads where at least one thread already has a follow-up response from the PR author or `@copilot` but remains unresolved.
   - Remaining PRs by most-recent `updatedAt`.
   If two PRs are still tied, prioritize the lower PR number first for deterministic behavior and stable reruns.
6. After applying skip rules, stop creating new nudge comments once 4 PRs have been nudged in the current run. Continue processing only for required bookkeeping/reporting.
7. Use the `pr-processor` sub-agent for each PR; pass only the PR number and compact context.
8. If a `pr-processor` call returns non-JSON or an error, record `{pr_number: <N>, skip_reason: "sub_agent_error"}` in the `skipped` array of the run-summary issue payload and move to the next PR without retrying.
9. Do not fetch full PR diffs or large file lists unless absolutely required for a skip decision.

{{#if experiments.remove_redundant_context_v1 == 'control' }}
## Required skip rules per PR

Skip when **any** of these hold (candidate prefilter eliminates most; these are backup checks):

| # | Condition | Key detail | Exception |
|---|-----------|------------|-----------|
| 1 | Any check `queued/in_progress/pending` started < 1h | Use `--head-sha <headRefOid>` with `gh aw checks`. Long-running checks (>1h) are ignored. | — |
| 2 | Latest comment has marker `<!-- gh-aw-pr-sous-chef-nudge -->` **and** `@copilot` | Marker-only comments (no `@copilot`) are informational and do not count. | Do **not** skip when `mergeStateStatus == CONFLICTING`. |
| 3 | Any recent comment has marker **and** `@copilot` posted < 30min ago | Informational comments (no `@copilot`) do not trigger cooldown. | — |
{{#endif}}

## Required nudges for prioritized eligible PRs

For each PR that is not skipped:

0. **Approve allowed action-required workflow runs**
   - If the compact JSON `failed_checks` list contains `ACTION_REQUIRED`, inspect the PR's waiting workflow runs and call `safeoutputs approve_workflow_run --run_id <RUN_ID>` only for matching CJS/CGO/CWI runs.
   - To find run IDs, query `gh run list --repo "$EXPR_GITHUB_REPOSITORY" --branch <headRefName> --json databaseId,path,status,event,headBranch,headSha --jq '[.[] | select((.status == "waiting" or .status == "action_required") and (.path == ".github/workflows/cjs.yml" or .path == ".github/workflows/cgo.yml" or .path == ".github/workflows/CWI.yml"))]'`; then keep only runs whose `headBranch` matches the PR head branch and whose `headSha` matches the PR `headRefOid` when present.
   - Only approve workflow runs whose workflow file is exactly `cjs.yml`, `cgo.yml`, or `CWI.yml`; never approve any other action-required workflow.
   - **Precondition before every approval:** fetch the candidate run with `gh api "repos/$EXPR_GITHUB_REPOSITORY/actions/runs/<RUN_ID>"`. Approve only when its `event` is `pull_request`, it is still `waiting` or `action_required`, and its nonempty `pull_requests` array contains only the current PR number. Also verify its `workflow_id` resolves to a workflow whose filename is exactly `cjs.yml`, `cgo.yml`, or `CWI.yml`.
   - The current PR number must appear in `/tmp/gh-aw/agent/pr-sous-chef-candidates-compact.json` under `prs[].number`; this is the explicit `allowed-pull-requests` list for this scheduled run. If any precondition is not met, do not call `approve_workflow_run`; record `{run_id: <RUN_ID>, skip_reason: "approval_policy_mismatch"}` and continue. Never retry a skipped approval.
   - Increment `approved_workflow_runs` for each successful approval.
   - If approval succeeds for all action-required `CJS`/`CGO`/`CWI` runs on the PR and there is no other forward-progress nudge to post, do not add a `@copilot` nudge comment for that PR.
   - If approval fails, record `{pr_number: <N>, skip_reason: "approve_workflow_run_failed"}` in the `skipped` array and continue with the remaining PR workflow.

1. **Run formatters and push if needed**
   - Checkout the PR branch: `git checkout <headRefName>`
   - Run `make fmt` to format all code (Go, JavaScript, JSON)
   - If dirty (`git diff --quiet` exits non-zero), call `push_to_pull_request_branch` with the PR number
   - Return to the original branch: `git checkout -`
   - Skip this step silently if `make fmt` exits non-zero (tools unavailable)

2. **Update branch if possible (skip for CONFLICTING branches)**
   - If `mergeStateStatus` is `CONFLICTING`, **skip this step entirely**.
   - Otherwise, attempt `update_pull_request` with `update_branch: true` and a minimal append body marker including `pr-sous-chef` and the run URL.

3. **Post one combined nudge comment when forward-progress nudge is still needed** (at most ONE `add_comment` per PR per run)
   - Skip this step when the approval step already approved all action-required `CJS`/`CGO`/`CWI` runs for the PR and there is no other forward-progress nudge to post.
   - When posting, always start with `<!-- gh-aw-pr-sous-chef-nudge -->` as the first hidden marker line and a `@copilot` mention.
   - **If `CONFLICTING`**: instruct `@copilot` to run `make merge-main` to resolve conflicts; increment `merge_main_scheduled`.
   - **If `zero_diff_stalled`**: state that no files have changed since the PR was opened and ask `@copilot` to either push the implementation or close the stalled PR; increment `zero_diff_stalled`.
   - **Otherwise**: combine into one comment — unresolved reviews (reviewer + direct link per thread, newest first), `failed_checks` from compact JSON (name + URL), branch refresh, and instruction to run the `pr-finisher` skill.
   - Every `add_comment` must include `pr_number`. Never emit `add_comment` without a numeric target field.
   - Every `add_comment` must include `pr_number`. Never emit `add_comment` without a numeric target field.
   - Always set `pr_number` to the current PR's numeric number. Use `safeoutputs add_comment --pr_number <N> --body $'...'` syntax only. Never use `gh pr comment` or `gh api` for writes.
   - Example: `safeoutputs add_comment --pr_number 12345 --body $'<!-- gh-aw-pr-sous-chef-nudge -->\n@copilot ...'`

4. **Resolve review threads that already have a response using a safe output**
   - For `schedule` and `workflow_dispatch` runs, use the `resolve_review_threads` list returned by the `pr-processor` sub-agent.
   - Include a thread only when all of the following are true: the thread is currently unresolved; contains reviewer feedback; and has a later reply from the PR author or `@copilot`.
   - For each thread ID, call `safeoutputs resolve_pull_request_review_thread --thread_id <ID>`.
   - Each `<ID>` must be the review thread node ID (`PRRT_...`) taken from the `reviewThreads` data fetched via `gh api graphql` (see the `pr-processor` sub-agent instructions); never pass a review comment node ID (`PRRC_...`), and never fetch it via `gh pr view --json reviewThreads` (not a valid field).
   - Copy each `<ID>` verbatim, character-for-character, from the `reviewThreads` data returned for the current PR. Never guess, truncate, extend, or otherwise fabricate a thread ID.
   - If resolving one thread fails, record `{thread_id: <ID>, skip_reason: "resolve_review_thread_failed"}` in the `skipped` array and continue.

5. **Dismiss stale `github-actions[bot]` blocking reviews when all PR review threads are resolved**
   - **Slash-command guard**: slash-command runs are acknowledgment nudges and must not perform automated review cleanup — skip this step entirely on `/souschef` slash-command runs.
   - For `schedule` and `workflow_dispatch` runs, use the `dismiss_reviews` list returned by the `pr-processor` sub-agent (populated only when ALL review threads are resolved).
   - For each review ID, call `safeoutputs dismiss_pull_request_review --pull_request_number <N> --review_id <ID> --justification "Dismissing stale github-actions review because all PR review threads are resolved."`.
   - If dismissing one review fails, record the failure and continue.

### Run summary

At the end, call **exactly one** `create_issue` with a brief run report (mandatory):

The issue body **must** begin with:

```
<!-- gh-aw-pr-sous-chef-report -->
> ⚠️ **This is an automated status report. Do not assign this issue to a Copilot agent.**
```

Then include the run counts as a compact table:

| Counter | Value |
|---|---|
| processed | N |
| skipped_checks_running | N |
| skipped_last_comment_from_sous_chef | N |
| skipped_cooldown | N |
| zero_diff_stalled | N |
| nudged | N |
| branch_update_attempts | N |
| formatter_pushes | N |
| approved_workflow_runs | N |
| merge_main_scheduled | N |
| resolved_review_threads | N |
| dismissed_reviews | N |

If any PRs were nudged, include a collapsible list of their numbers and titles.

If `create_issue` is unavailable, fall back to `noop` with a condensed message, e.g. `"processed=4; skipped_checks_running=0; skipped_last_comment_from_sous_chef=1; skipped_cooldown=1; zero_diff_stalled=1; nudged=2; branch_update_attempts=0; formatter_pushes=0; approved_workflow_runs=1; merge_main_scheduled=1; resolved_review_threads=3; dismissed_reviews=1"`.

## Formatting Requirements

- **Header Levels**: Use h3 (`###`) or lower for all headers in your report to maintain proper document hierarchy. Never use h1 (`#`) or h2 (`##`) headers.
- **Progressive Disclosure**: Wrap long sections or verbose details in `<details><summary>Section Name</summary>` tags to improve readability and reduce scrolling.
- Keep critical information visible (summary, key outcomes, and recommendations) and use collapsible sections for secondary details.

### Recommended Report Structure

1. **Overview**: 1-2 paragraphs summarizing key findings (always visible)
2. **Critical Information**: Key metrics, status, critical issues (always visible)
3. **Details**: Use `<details><summary>Section Name</summary>` for expanded content
4. **Recommendations**: Actionable next steps (always visible)

## agent: `pr-processor`
---
description: Processes one PR with minimal API calls and returns skip/nudge decisions
model: sonnet
---
Given one PR number and compact metadata for `github/gh-aw`. Query only `github/gh-aw`; never use review thread or review IDs from another repository.

- `gh pr view --json` has **no** `reviewThreads` field; requesting it fails with `Unknown JSON field: "reviewThreads"`. Never pass `reviewThreads` to `gh pr view --json`.
- To fetch PR review thread node IDs (`PRRT_...`), issue this GraphQL query instead: `gh api graphql -f query='query($owner:String!,$repo:String!,$pr:Int!){repository(owner:$owner,name:$repo){pullRequest(number:$pr){reviewThreads(first:100){nodes{id isResolved comments(first:10){nodes{author{login} body}}}}}}}' -f owner=github -f repo=gh-aw -F pr=<N>` — the resulting `reviewThreads` array (from `data.repository.pullRequest.reviewThreads.nodes`) is the only valid source of `PRRT_...` IDs; each node's `id` is the thread ID and `isResolved` indicates resolution state.
- Use `gh pr view <N> --repo github/gh-aw --json number,comments,reviews,headRefName,headRefOid,mergeStateStatus,title` (without `reviewThreads`) for all other PR metadata.

1. Check skip conditions in this order:
   - checks/actions running — note: the candidate prefilter already excluded PRs with short-running pending checks (running < 1 hour) via `statusCheckRollup`; only re-verify if you have reason to believe state changed since the prefilter ran; Long-running checks (running > 1 hour) are intentionally ignored
   - latest comment contains both `<!-- gh-aw-pr-sous-chef-nudge -->` **and** `@copilot`, **and** `mergeStateStatus` is **not** `CONFLICTING` (when the branch has merge conflicts, do NOT skip even if the last actionable comment is from sous-chef — it must nudge Copilot to resolve them; also, comments with the marker but without `@copilot` are purely informational and do NOT count as a sous-chef nudge for this check)
   - any recent comment contains both `<!-- gh-aw-pr-sous-chef-nudge -->` and `@copilot` and was posted within the last 30 minutes (informational comments without `@copilot` do not count toward cooldown)
2. If skipped, return `skip_reason` only.
3. If not skipped, return:
   - `conflicting`: true if `mergeStateStatus` is `CONFLICTING` (indicates the branch has merge conflicts)
   - whether branch update should be attempted (always false when `conflicting` is true)
   - a single combined nudge comment body:
     - if `conflicting` is true: a targeted nudge asking `@copilot` to run `make merge-main` to resolve conflicts
     - otherwise: a combined nudge covering unresolved review feedback, failed checks (from `failed_checks` in the compact JSON — list each by name with URL when available), branch refresh, and any other forward-progress action including a direct instruction to run the `pr-finisher` skill — one comment only, never two; if unresolved PR reviews exist, include an explicit unresolved-reviews list (reviewer + direct link per unresolved review thread)
   - `resolve_review_threads`: an array of unresolved PR review thread node IDs (`PRRT_...`) from `reviewThreads` to resolve via safe output; never emit review comment node IDs (`PRRC_...`). Include a thread only when the thread already contains a follow-up response from the PR author or `@copilot` that addresses the feedback
   - `dismiss_reviews`: an array of review IDs — include a review ID only when the review was authored by `github-actions[bot]` with `CHANGES_REQUESTED` state AND all review threads on the PR are resolved (no unresolved threads remain); return an empty array if there are unresolved threads or no qualifying reviews
4. Make at most 8 tool calls total. If 8 calls are insufficient to reach a confident decision, set all fields to `null` and set `skip_reason: "insufficient_context"`.
5. Keep output compact JSON only — a single object, no prose.
6. If you cannot determine a field, set it to `null`.