---
private: true
name: Copilot Centralization Optimizer
description: Finds repeated cross-user prompt patterns that should become centralized workflows, tools, deterministic steps, shared prompts, or other automations.
on:
  schedule: weekly on monday
  workflow_dispatch:
permissions:
  contents: read
  copilot-requests: write
sandbox:
  agent:
    runtime: cloud-hypervisor
tools:
  repo-memory:
    branch-name: memory/copilot-centralization-optimizer
    description: Long-lived centralization trend snapshots and history
    file-glob: ["*.json", "*.jsonl"]
engine:
  id: codex
  model-provider: github
model: copilot/gpt-5.3-codex
strict: true
max-ai-credits: 250
max-daily-ai-credits: 1000
safe-outputs:
  mentions: false
  allowed-github-references: []
  max-bot-mentions: 1
  create-issue:
    title-prefix: "[copilot-centralization] "
    labels: [report, ai-optimization]
    close-older-issues: true
    expires: 30d
steps:
  - name: Collect agent task data
    env:
      GH_TOKEN: ${{ secrets.COPILOT_GITHUB_TOKEN }}
      REPO: ${{ github.repository }}
      TASK_LOOKBACK_DAYS: 30
      TASK_LIMIT: 300
    run: |
      set -euo pipefail
      mkdir -p /tmp/gh-aw/data
      : > /tmp/gh-aw/data/task-summaries.jsonl

      SINCE="$(date -u -d "-${TASK_LOOKBACK_DAYS} days" +%Y-%m-%dT%H:%M:%SZ)"
      API_VERSION="2026-03-10"

      _task_ids=$(mktemp)
      trap 'rm -f "$_task_ids"' EXIT

      if gh api --paginate \
          -H "Accept: application/vnd.github+json" \
          -H "X-GitHub-Api-Version: ${API_VERSION}" \
          "/agents/repos/$REPO/tasks?per_page=100" \
          --jq '.tasks[].id' \
          > "$_task_ids"; then
        while IFS= read -r task_id; do
            gh api \
              -H "Accept: application/vnd.github+json" \
              -H "X-GitHub-Api-Version: ${API_VERSION}" \
              "/agents/repos/$REPO/tasks/$task_id" \
              | jq -c --arg since "$SINCE" '
              | def sorted_sessions: (.sessions // [] | sort_by(.created_at // ""));
                def artifact_types: ([.artifacts[]?.type | select(.)] | join(","));
                def first_non_empty_index($sessions):
                  ([range(0; ($sessions | length))
                    | select((($sessions[.]?.prompt // "") | length) > 0)] | .[0]);

                sorted_sessions as $sessions
                | ($sessions | length) as $session_count
                | (if $session_count == 0 then null else ($sessions[0].prompt // null) end) as $earliest_input
                | first_non_empty_index($sessions) as $first_non_empty_index
                | (if $first_non_empty_index == null then null else $sessions[$first_non_empty_index].prompt end) as $first_non_empty_input
                | (if $session_count == 0 then "none"
                   elif $first_non_empty_index == null or $first_non_empty_index == 0 then "earliest"
                   else "first_non_empty"
                   end) as $input_source
                | (if $session_count == 0 then null
                   elif $first_non_empty_index == null then $earliest_input
                   else $first_non_empty_input
                   end) as $input
                | (artifact_types) as $artifact_types
                | ($earliest_input == "") as $earliest_prompt_empty
                | {
                    artifact_types: $artifact_types,
                    creator_id: (if .creator?.id == null then null else (.creator.id | tostring) end),
                    creator_login: (.creator?.login // null),
                    earliest_prompt_empty: $earliest_prompt_empty,
                    id: .id,
                    name: .name,
                    state: .state,
                    created_at: .created_at,
                    updated_at: .updated_at,
                    url: .html_url,
                    earliest_input: $earliest_input,
                    first_non_empty_input: $first_non_empty_input,
                    input: $input,
                    input_source: $input_source,
                    input_session_index: (if $session_count == 0 then null elif $first_non_empty_index == null then 0 else $first_non_empty_index end),
                    non_empty_prompt_count: ([.sessions[]?.prompt // "" | select(length > 0)] | length),
                    session_count: $session_count,
                    start_context_confidence: "derived",
                    start_context_guess: (
                      if $artifact_types == "pull,branch" and $earliest_prompt_empty then "pull_branch_bootstrap"
                      elif $artifact_types == "pull,branch,pull" and $earliest_prompt_empty then "pull_branch_review_bootstrap"
                      elif ($artifact_types | contains("pull")) and $earliest_prompt_empty then "pull_bootstrap"
                      elif $earliest_prompt_empty then "bootstrap_unknown"
                      else "direct_prompt"
                      end
                    ),
                    used_fallback_input: ($session_count > 0 and $first_non_empty_index != null and $first_non_empty_index > 0)
                  }
            ' || true
          done < "$_task_ids" \
        | head -n "$TASK_LIMIT" \
        >> /tmp/gh-aw/data/task-summaries.jsonl || true
      else
        echo "Agent tasks API request failed; proceeding with empty dataset" >&2
      fi

      # Convert JSONL to JSON array for downstream analysis steps
      jq -s '.' /tmp/gh-aw/data/task-summaries.jsonl > /tmp/gh-aw/data/task-summaries.json

  - name: Precompute optimization datasets
    env:
      EXPR_GITHUB_REPOSITORY: ${{ github.repository }}
      EXPR_GITHUB_RUN_ID: ${{ github.run_id }}
      EXPR_GITHUB_SERVER_URL: ${{ github.server_url }}
      EXPR_GITHUB_EVENT_NAME: ${{ github.event_name }}
    run: |
      set -euo pipefail
      GH_AW_SAFE_OUTPUTS="${GH_AW_SAFE_OUTPUTS:-${RUNNER_TEMP:-/tmp}/gh-aw/safeoutputs/outputs.jsonl}"

      jq '
        def round2: .*100 | round / 100;
        {
          total_tasks: length,
          distinct_creators: ([.[].creator_id | select(. != null)] | unique | length),
          tasks_with_prompts: ([.[] | select(.input != null)] | length),
          avg_sessions_per_task: (if length == 0 then 0 else ([.[].session_count] | add / length | round2) end),
          fallback_rate: (if length == 0 then 0 else ([.[].used_fallback_input | if . then 1 else 0 end] | add / length | round2) end)
        }
      ' /tmp/gh-aw/data/task-summaries.json > /tmp/gh-aw/data/overall-stats.json

      jq '
        def normalize_prompt:
          ascii_downcase
          | gsub("\\r|\\n"; " ")
          | gsub("\\s+"; " ")
          | gsub("^\\s+|\\s+$"; "");
        [ .[]
          | select(.input != null and (.input | gsub("^\\s+|\\s+$"; "") | length) >= 24)
          | . + {
              normalized_prompt: (.input | normalize_prompt),
              artifact_signature: (.artifact_types // "none"),
              creator_key: (.creator_id // .creator_login // "unknown")
            }
        ]
        | sort_by(.normalized_prompt)
        | group_by(.normalized_prompt)
        | map({
            normalized_prompt: .[0].normalized_prompt,
            sample_prompt: .[0].input,
            task_count: length,
            distinct_users: (map(.creator_key) | unique | length),
            artifact_signatures: (map(.artifact_signature) | unique | length),
            sample_creators: (map(.creator_login // .creator_id // "unknown") | unique | .[0:5]),
            sample_context: .[0].start_context_guess
          })
        | map(select(.distinct_users >= 2))
        | sort_by(-.distinct_users, - .task_count)
        | .[0:25]
      ' /tmp/gh-aw/data/task-summaries.json > /tmp/gh-aw/data/exact-repeats.json

      jq '
        def round2: .*100 | round / 100;
        def intent_bucket($prompt):
          ($prompt // "" | ascii_downcase) as $p
          | if ($p | test("triage|incident|failing workflow|\\bci\\b")) then "ci_triage"
            elif ($p | test("review|pull request|\\bpr\\b")) then "pr_review"
            elif ($p | test("dependency|dependabot|upgrade")) then "dependency_maintenance"
            elif ($p | test("release|changelog")) then "release_ops"
            elif ($p | test("issue|label")) then "issue_ops"
            elif ($p | test("docs|documentation")) then "docs_ops"
            elif ($p | test("security")) then "security_ops"
            else "other"
            end;
        [ .[]
          | select(.input != null)
          | . + {
              intent_bucket: intent_bucket(.input),
              creator_key: (.creator_id // .creator_login // "unknown")
            }
        ]
        | sort_by(.intent_bucket)
        | group_by(.intent_bucket)
        | map({
            intent_bucket: .[0].intent_bucket,
            task_count: length,
            distinct_users: (map(.creator_key) | unique | length),
            avg_sessions: ((map(.session_count) | add / length) | round2),
            fallback_rate: ((map(if .used_fallback_input then 1 else 0 end) | add / length) | round2)
          })
        | sort_by(-.distinct_users, - .task_count)
      ' /tmp/gh-aw/data/task-summaries.json > /tmp/gh-aw/data/intent-buckets.json

      jq '
        def round2: .*100 | round / 100;
        def intent_bucket($prompt):
          ($prompt // "" | ascii_downcase) as $p
          | if ($p | test("triage|incident|failing workflow|\\bci\\b")) then "ci_triage"
            elif ($p | test("review|pull request|\\bpr\\b")) then "pr_review"
            elif ($p | test("dependency|dependabot|upgrade")) then "dependency_maintenance"
            elif ($p | test("release|changelog")) then "release_ops"
            elif ($p | test("issue|label")) then "issue_ops"
            elif ($p | test("docs|documentation")) then "docs_ops"
            elif ($p | test("security")) then "security_ops"
            else "other"
            end;
        [ .[]
          | select(.input != null)
          | . + {
              intent_bucket: intent_bucket(.input),
              artifact_signature: (if (.artifact_types // "") == "" then "none" else .artifact_types end),
              creator_key: (.creator_id // .creator_login // "unknown")
            }
        ]
        | sort_by(.intent_bucket, .artifact_signature, .start_context_guess)
        | group_by([.intent_bucket, .artifact_signature, .start_context_guess])
        | map({
            intent_bucket: .[0].intent_bucket,
            artifact_types: .[0].artifact_signature,
            start_context_guess: .[0].start_context_guess,
            task_count: length,
            distinct_users: (map(.creator_key) | unique | length),
            avg_sessions: ((map(.session_count) | add / length) | round2),
            fallback_rate: ((map(if .used_fallback_input then 1 else 0 end) | add / length) | round2)
          })
        | map(select(.distinct_users >= 2))
        | map(. + {
            centralization_score: ((.task_count * .distinct_users * (1 + (.avg_sessions / 3) + .fallback_rate)) | round2),
            recommendation_kind: (
              if .distinct_users >= 5 and .task_count >= 10 then "continuous_workflow"
              elif .distinct_users >= 3 and .task_count >= 5 then "shared_prompt_or_chatops"
              else "monitor_only"
              end
            )
          })
        | sort_by(-.centralization_score)
        | .[0:25]
      ' /tmp/gh-aw/data/task-summaries.json > /tmp/gh-aw/data/workflow-opportunities.json

      cat > /tmp/gh-aw/data/run-context.json <<EOF
      {
        "repository": "$EXPR_GITHUB_REPOSITORY",
        "run_id": "$EXPR_GITHUB_RUN_ID",
        "run_url": "$EXPR_GITHUB_SERVER_URL/$EXPR_GITHUB_REPOSITORY/actions/runs/$EXPR_GITHUB_RUN_ID",
        "trigger": "$EXPR_GITHUB_EVENT_NAME"
      }
      EOF

      jq -n \
        --arg generated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
        --arg repository "$EXPR_GITHUB_REPOSITORY" \
        --slurpfile stats /tmp/gh-aw/data/overall-stats.json \
        --slurpfile exact /tmp/gh-aw/data/exact-repeats.json \
        --slurpfile intents /tmp/gh-aw/data/intent-buckets.json \
        --slurpfile opportunities /tmp/gh-aw/data/workflow-opportunities.json '
        {
          generated_at: $generated_at,
          repository: $repository,
          overall_stats: ($stats[0] // {}),
          top_exact_repeats: (($exact[0] // [])[0:10]),
          intent_buckets: ($intents[0] // []),
          workflow_opportunities: (($opportunities[0] // [])[0:15])
        }
      ' > /tmp/gh-aw/data/current-snapshot.json

      jq -n \
        --arg generated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" '
        {
          baseline_available: false,
          current_generated_at: $generated_at,
          note: "Trend analysis is computed during the agent phase after repo-memory is cloned."
        }
      ' > /tmp/gh-aw/data/trend-analysis.json

      tasks_with_prompts=$(jq -r '.tasks_with_prompts // 0' /tmp/gh-aw/data/overall-stats.json)
      cross_user_buckets=$(jq '[.[] | select((.distinct_users // 0) >= 2)] | length' /tmp/gh-aw/data/intent-buckets.json)
      actionable_candidates=$(jq '[.[] | select((.recommendation_kind // "") != "monitor_only")] | length' /tmp/gh-aw/data/workflow-opportunities.json)
      if [ "$tasks_with_prompts" -lt 8 ] || { [ "$cross_user_buckets" -lt 2 ] && [ "$actionable_candidates" -lt 1 ]; }; then
        printf '%s\n' '{"type":"noop","message":"Insufficient cross-user repetition signal in the recent Agent Tasks sample; skipping centralization analysis."}' >> "$GH_AW_SAFE_OUTPUTS"
      fi

  - name: Upload centralization analysis dataset
    if: always()
    continue-on-error: true
    uses: actions/upload-artifact@v7.0.1
    with:
      name: copilot-centralization-analysis
      path: /tmp/gh-aw/data
      retention-days: 14
      if-no-files-found: ignore
evals:
  - id: patterns_identified
    question: Did the agent identify repeated cross-user prompt patterns suitable for centralization?
  - id: recommendations_produced
    question: Were recommendations produced for new workflows, tools, shared prompts, or other automations?
imports:
  - shared/reporting.md
features:
  gh-aw-detection: true
---

# Copilot Centralization Optimizer

## Goal

Identify repeated prompt behavior across different users and decide where intelligence should be centralized to reduce AI credits.

Centralization can mean:

- a continuous GitHub agentic workflow
- a scheduled report or monitor
- a slash-command or other ChatOps entrypoint
- a reusable tool, MCP surface, or CLI wrapper
- a deterministic pre-agent context-gathering or normalization step
- a deterministic postcompute, validation, or summarization step
- a shared prompt template or playbook
- leaving the work ad hoc when evidence is weak

## Inputs

Read only these prepared files first:

- `/tmp/gh-aw/data/overall-stats.json`
- `/tmp/gh-aw/data/exact-repeats.json`
- `/tmp/gh-aw/data/intent-buckets.json`
- `/tmp/gh-aw/data/workflow-opportunities.json`
- `/tmp/gh-aw/data/trend-analysis.json`
- `/tmp/gh-aw/data/current-snapshot.json`
- `/tmp/gh-aw/data/run-context.json`
- optional prior baseline: `/tmp/gh-aw/repo-memory/default/centralization-baseline.json`

These files were precomputed in `steps:` to keep token usage low.

## Task

Investigate whether repeated cross-user prompting suggests centralizing intelligence.

Before drafting the report:
- If `/tmp/gh-aw/repo-memory/default/centralization-baseline.json` exists, recompute `/tmp/gh-aw/data/trend-analysis.json` by comparing that baseline to `/tmp/gh-aw/data/current-snapshot.json`.
- Persist the sanitized current snapshot for the next run:
  - write `/tmp/gh-aw/repo-memory/default/centralization-baseline.json`
  - append to `/tmp/gh-aw/repo-memory/default/centralization-history.jsonl`
  - strip `top_exact_repeats[].sample_prompt` before writing either file.

Focus on:

- prompt or intent patterns that appear across multiple users
- repeated work that should become a continuous workflow or reusable automation
- repeated work that should move out of the agent loop into deterministic precompute or postcompute steps
- repeated work that should become a reusable tool rather than a larger workflow
- candidates likely to save AI credits by reducing repeated manual prompting
- signals of friction, especially high session counts or frequent fallback behavior
- patterns that are growing or persist across runs, not just one-off spikes
- reusable agentic skills that could capture the repeated prompting behavior

Be conservative. Do not overclaim centralization opportunities from weak evidence.
If `trend-analysis.json` says `baseline_available` is `false`, treat this run as a baseline and avoid strong trend claims.

## Decision Rules

- Recommend `continuous workflow` when the same intent recurs across many users and tasks and the work looks event-driven or periodic.
- Recommend `shared prompt or ChatOps` when the intent repeats but still needs human initiation or scoped context.
- Recommend `tool or deterministic step` when the repeated work is primarily data gathering, normalization, validation, postprocessing, or another narrow function that should not consume full agent turns.
- Recommend `keep ad hoc` when repetition is low, user-specific, or exploratory.
- If the dataset is too small or cross-user evidence is weak, use `noop` with a short explanation.

## Output

Use `create-issue` to publish a concise recurring report.

The report must use GitHub-flavored markdown and this structure:

### Summary

- one short paragraph on whether centralization opportunities are present
- include the main evidence counts from `overall-stats.json`

### Top Candidates

- list up to 5 strongest opportunities from `workflow-opportunities.json`
- for each one, say whether it should be a continuous workflow, shared prompt/ChatOps pattern, reusable tool/deterministic step, or remain ad hoc
- explain why the pattern is repeated enough to justify centralization

### Repeated Prompt Evidence

- summarize the strongest exact or near-exact repeated prompts from `exact-repeats.json`
- note when exact repeats are sparse and intent-level buckets are stronger evidence

### Trend Changes

- summarize the most important items from `trend-analysis.json`
- call out which repeated behaviors look new, which are growing, and which appear stable enough to justify centralization

### Reusable Agentic Skills

- list up to 5 candidate reusable skills implied by the repeated prompting behavior
- for each, give a concise skill name, the best pattern (`continuous workflow`, `ChatOps`, `workflow_dispatch`, `tool or deterministic step`, or `keep ad hoc`), and the minimum required context

### AI Credit Savings Rationale

- explain how centralization would likely reduce prompts, context duplication, or repeated frontier-model turns
- prefer concrete reasoning over generic cost claims

### Recommendations

- give 3 actionable next steps in priority order

### References

- include the workflow run as a link using the format `[§${{ github.run_id }}](${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }})`

## Safe Outputs

- Use `create-issue` for the visible report.
- Use `noop` when there are fewer than 2 meaningful cross-user centralization candidates or the prompt coverage is too weak to support a recommendation.