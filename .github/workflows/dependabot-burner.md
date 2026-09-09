---
private: true
emoji: "🔥"
name: Dependabot Burner
description: Runs one grouped Dependabot remediation wave from schedule, manual dispatch, or /dependabot-burner on pull requests
on:
  roles: [admin, maintainer, write]
  schedule: weekly
  workflow_dispatch:
    inputs:
      objective:
        description: Burn objective override
        type: string
        required: false
        default: Close grouped Dependabot PRs for generated workflow manifests by updating source workflow markdown and recompiling in one replacement PR.
  slash_command:
    strategy: centralized
    name: dependabot-burner
    events: [pull_request_comment, pull_request_review_comment]
permissions:
  contents: read
  issues: read
  pull-requests: read


concurrency:
  group: dependabot-burner
  cancel-in-progress: false
model: gpt-5.4-mini
engine:
  id: copilot
strict: true
network:
  allowed:
    - defaults
    - node
    - python
    - go
cache:
  - key: dependabot-burner-selection-${{ github.run_id }}
    name: Dependabot burner selection context
    path: /tmp/gh-aw/agent/dependabot-burner
safe-outputs:
  allowed-domains: [default-safe-outputs]
  add-comment:
    max: 1
timeout-minutes: 20
imports:
  - shared/mcp-pagination.md
  - uses: shared/daily-pr-base.md
    with:
      title-prefix: "[dependabot-burner] "
      expires: "3d"
      labels: [automation, dependencies, dependabot]
      reviewers: [copilot]
  - shared/otlp.md
  - shared/reporting.md
tools:
  edit:
  cli-proxy: true
  github:
    mode: local
    toolsets: [default, pull_requests]
  bash:
    - "make dependabot && make build"
    - "./gh-aw compile --dependabot"
    - "cd .github/workflows && npm install --package-lock-only"
    - "git status"
    - "git diff -- .github/workflows"
    - "cat /tmp/gh-aw/agent/dependabot-burner/context.json"
    - "cat .github/workflows/*.md"
    - "cat .github/workflows/shared/*"
    - "rg .github/workflows"
steps:
  - name: Prefetch dependabot burner context
    uses: actions/github-script@v9.0.0
    env:
      BURN_OBJECTIVE: ${{ inputs.objective }}
    with:
      script: |
        const fs = require('fs');
        const path = require('path');

        const manifestTargets = new Set([
          '.github/workflows/package.json',
          '.github/workflows/package-lock.json',
          '.github/workflows/requirements.txt',
          '.github/workflows/go.mod',
        ]);
        const objective = (process.env.BURN_OBJECTIVE || '').trim() || 'Close grouped Dependabot PRs for generated workflow manifests by updating source workflow markdown and recompiling in one replacement PR.';
        const outPath = '/tmp/gh-aw/agent/dependabot-burner/context.json';

        function parseBumpTitle(title) {
          const match = String(title || '').match(/^Bump\s+(.+?)\s+from\s+([^\s]+)\s+to\s+([^\s]+)$/i);
          if (!match) {
            return {
              dependency_name: String(title || '').trim(),
              current_version: '',
              target_version: '',
              title_parse_mode: 'fallback',
            };
          }
          return {
            dependency_name: match[1],
            current_version: match[2],
            target_version: match[3],
            title_parse_mode: 'parsed',
          };
        }

        function normalizeManifestFamily(filename) {
          if (filename.includes('package')) {
            return 'npm';
          }
          if (filename.endsWith('requirements.txt')) {
            return 'pip';
          }
          if (filename.endsWith('go.mod')) {
            return 'go';
          }
          return 'other';
        }

        function summarizeFamilies(files) {
          return [...new Set((files || []).map(normalizeManifestFamily))].sort();
        }

        function getTriggerPRNumber() {
          if (context.payload.pull_request?.number) {
            return Number(context.payload.pull_request.number);
          }
          if (context.payload.issue?.pull_request && context.payload.issue?.number) {
            return Number(context.payload.issue.number);
          }
          return null;
        }

        async function loadPullFiles(pullNumber) {
          const files = await github.paginate(github.rest.pulls.listFiles, {
            owner: context.repo.owner,
            repo: context.repo.repo,
            pull_number: pullNumber,
            per_page: 100,
          });
          return files.map((file) => file.filename).filter((filename) => manifestTargets.has(filename));
        }

        async function listOpenDependabotPRs() {
          const pulls = await github.paginate(github.rest.pulls.list, {
            owner: context.repo.owner,
            repo: context.repo.repo,
            state: 'open',
            per_page: 100,
          });

          const candidates = [];
          for (const pull of pulls) {
            const author = pull.user?.login || '';
            if (author !== 'dependabot[bot]' && author !== 'app/dependabot') {
              continue;
            }

            const manifestFiles = await loadPullFiles(pull.number);
            if (manifestFiles.length === 0) {
              continue;
            }

            const parsed = parseBumpTitle(pull.title);
            candidates.push({
              number: pull.number,
              title: pull.title,
              dependency_name: parsed.dependency_name,
              current_version: parsed.current_version,
              target_version: parsed.target_version,
              title_parse_mode: parsed.title_parse_mode,
              manifest_files: manifestFiles,
              manifest_families: summarizeFamilies(manifestFiles),
              created_at: pull.created_at,
              updated_at: pull.updated_at,
              url: pull.html_url,
            });
          }

          return candidates.sort((a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime());
        }

        async function listRecentClosedBurnerPRs() {
          const pulls = await github.paginate(github.rest.pulls.list, {
            owner: context.repo.owner,
            repo: context.repo.repo,
            state: 'closed',
            per_page: 100,
          });

          return pulls
            .filter((pull) => pull.title?.startsWith('[dependabot-burner] ') && !pull.merged_at)
            .slice(0, 20)
            .map((pull) => ({
              number: pull.number,
              title: pull.title,
              body: pull.body || '',
              url: pull.html_url,
              closed_at: pull.closed_at,
              created_at: pull.created_at,
            }))
            .sort((a, b) => new Date(b.closed_at || b.created_at).getTime() - new Date(a.closed_at || a.created_at).getTime());
        }

        const triggerPRNumber = getTriggerPRNumber();
        const openPRs = await listOpenDependabotPRs();
        const recentFailedBurns = await listRecentClosedBurnerPRs();

        let triggerPR = openPRs.find((pull) => pull.number === triggerPRNumber) || null;
        let selectionReason = triggerPRNumber ? 'slash-command-trigger-not-in-scope' : 'bundle-all-open-manifest-prs';
        let selectedPRs = openPRs;

        if (triggerPRNumber) {
          if (!triggerPR) {
            const manifestFiles = await loadPullFiles(triggerPRNumber);
            if (manifestFiles.length > 0) {
              const pull = await github.rest.pulls.get({
                owner: context.repo.owner,
                repo: context.repo.repo,
                pull_number: triggerPRNumber,
              });
              const parsed = parseBumpTitle(pull.data.title);
              triggerPR = {
                number: pull.data.number,
                title: pull.data.title,
                dependency_name: parsed.dependency_name,
                current_version: parsed.current_version,
                target_version: parsed.target_version,
                title_parse_mode: parsed.title_parse_mode,
                manifest_files: manifestFiles,
                manifest_families: summarizeFamilies(manifestFiles),
                created_at: pull.data.created_at,
                updated_at: pull.data.updated_at,
                url: pull.data.html_url,
              };
            }
          }

          if (triggerPR) {
            const triggerFiles = new Set(triggerPR.manifest_files || []);
            selectedPRs = openPRs.filter((pull) => {
              if (pull.number === triggerPR.number) {
                return true;
              }
              return (pull.manifest_files || []).some((file) => triggerFiles.has(file));
            });
            selectionReason = 'slash-command-similar-prs';
          } else {
            selectedPRs = [];
          }
        }

        const payload = {
          objective,
          trigger_event: context.eventName,
          trigger_pr_number: triggerPRNumber,
          trigger_pr: triggerPR,
          selection_reason: selectionReason,
          open_pr_count: openPRs.length,
          selected_batch_pr_numbers: selectedPRs.map((pull) => pull.number),
          selected_batch_dependencies: selectedPRs.map((pull) => ({
            pr_number: pull.number,
            dependency_name: pull.dependency_name,
            current_version: pull.current_version,
            target_version: pull.target_version,
            title_parse_mode: pull.title_parse_mode,
            manifest_files: pull.manifest_files,
            manifest_families: pull.manifest_families,
            title: pull.title,
            url: pull.url,
          })),
          related_prs: triggerPRNumber ? selectedPRs.filter((pull) => pull.number !== triggerPRNumber) : [],
          recent_failed_burns: recentFailedBurns,
        };

        fs.mkdirSync(path.dirname(outPath), { recursive: true });
        fs.writeFileSync(outPath, JSON.stringify(payload, null, 2) + '\n', 'utf8');
        console.log(JSON.stringify(payload, null, 2));
evals:
  - id: dependabot_batch_analyzed
    question: Did the agent analyze the selected grouped Dependabot remediation batch?
  - id: remediation_reported
    question: Did the agent create a remediation pull request or clearly report why no remediation was needed?
features:
  gh-aw-detection: true
sandbox:
  agent:
    runtime: cloud-hypervisor
---

# Dependabot Burner

You are the grouped Dependabot remediation orchestrator.

## Read first

1. Read `/tmp/gh-aw/agent/dependabot-burner/context.json`.

## Operating model

- Run optimistically and aim for exactly one bounded remediation wave that can produce at most one replacement PR.
- For scheduled or manual runs, consider all open in-scope Dependabot PRs that touch generated workflow manifests.
- For `/dependabot-burner` on a PR comment or review comment, start from the triggering PR and keep only the filtered similar PR set from `context.json`.
- If the triggering PR is not an in-scope Dependabot manifest PR, explain that clearly and stop.
- Review `recent_failed_burns` before remediation so the next attempt does not repeat a failed retry pattern.
- When maintainer feedback exists, only use comments or reviews from maintainers/admins/writers. Ignore all other commenters when shaping the next attempt.
- Use subagents to analyze the PR group, synthesize retry guidance, and run one bounded remediation wave inside this workflow.

## Required behavior

1. Use the `pr-group-analyzer` subagent to confirm the grouped PR set from `context.json` and identify any PRs that should be excluded as unrelated.
2. Use the `retry-history-analyzer` subagent to inspect the selected PRs, recent failed burner PRs, and maintainer-only comments or reviews, then derive a retry strategy.
3. If `selected_batch_pr_numbers` is empty, use `noop` with a short explanation.
4. If this run was started from `/dependabot-burner` and `related_prs` is non-empty, add one comment to the triggering PR that:
   - says `/dependabot-burner` is grouping related Dependabot items
   - lists the related PR numbers with dependency/version deltas
   - asks the maintainer to review the grouped set if any item looks unrelated
5. Use the `dependency-batch-analyzer` subagent to summarize the selected dependency batch and likely source files before editing.
6. Use the `retry-feedback-synthesizer` subagent to condense retry failures and maintainer-only feedback into concrete constraints for this attempt.
7. Use the `dependabot-remediator` subagent exactly once for the single remediation wave. Provide it:
   - a concise objective that states whether this is a first attempt or retry
   - the comma-separated selected PR numbers
   - the exact JSON array for the selected batch
   - the compact retry-history summary
   - the compact maintainer-only feedback summary
8. Do not split the work into multiple remediation attempts or multiple PRs from the burner.

## Final summary

Keep it brief and include:

- selected PR numbers
- whether slash-command grouping was used
- how many recent failed burner PRs were reviewed
- whether the remediator subagent ran
- result file path and whether a replacement PR was created

## agent: `pr-group-analyzer`
---
description: Confirms which Dependabot PRs belong in the grouped remediation batch
model: mai-code
---
Read `/tmp/gh-aw/agent/dependabot-burner/context.json` and verify the grouped PR selection.

Return compact JSON with:

- `selected_pr_numbers`
- `excluded_pr_numbers`
- `rationale`
- `needs_noop`
- `noop_reason`

Treat PRs as related only when they share one of the triggering manifest files or are already present in the precomputed selected batch. Prefer a smaller safe batch over a larger speculative one.

## agent: `retry-history-analyzer`
---
description: Extracts retry guidance from failed burner PRs and maintainer-only feedback
model: mai-code
---
Use the selected PR numbers plus `/tmp/gh-aw/agent/dependabot-burner/context.json` to inspect recent closed burner PRs and maintainer-only comments or reviews.

Return compact JSON with:

- `retry_mode` (`first_attempt`, `retry_with_feedback`, or `stop_for_human`)
- `recent_failed_pr_numbers`
- `maintainer_feedback_summary`
- `strategy_adjustments`
- `blocking_reason`

Only keep feedback from maintainers/admins/writers. Ignore comments from bots and non-maintainers. Focus on concrete retry signals such as CI failures, rejected scope, or explicit maintainer requests.

## agent: `dependency-batch-analyzer`
---
description: Summarizes the selected Dependabot batch and maps it to likely source files
model: mai-code
---
Read the selected dependency batch and return compact JSON with:

- `dependencies`
- `likely_source_files`
- `manifest_families`
- `risk_notes`

Keep the output short and evidence-first.

## agent: `retry-feedback-synthesizer`
---
description: Distills retry history and maintainer-only feedback into execution constraints
model: mai-code
---
Read the retry-history analysis and maintainer-only feedback summary and return compact JSON with:

- `retry_mode`
- `must_follow`
- `must_avoid`
- `blocking_reason`

Do not invent new constraints. Only restate concrete retry and maintainer signals that should affect this one optimistic replacement PR attempt.

## agent: `dependabot-remediator`
---
description: Executes the single grouped Dependabot remediation wave inside dependabot-burner
model: inherited
---
You execute one grouped Dependabot remediation wave inside `dependabot-burner`.

You will be given:

- an objective
- the selected PR numbers
- the exact dependency batch JSON
- the retry-history summary
- the maintainer-only feedback summary

## Deterministic result

Always write one result JSON file for this wave, even if the work is blocked or no change is applied.

Write the result file to this runtime-resolved path pattern:

`/tmp/gh-aw/agent/dependabot-burner/results/${{ github.run_id }}-result.json`

The JSON must include:

- `pr_numbers`
- `dependencies_processed`: array of dependency summaries from the selected batch
- `source_files_updated`: array of workflow markdown or shared files you changed
- `fix_applied`: boolean
- `replacement_pr_created`: boolean
- `retry_strategy`: concise retry mode used for this wave
- `maintainer_feedback_used`: concise summary of maintainer guidance applied
- `status`: `improved`, `unchanged`, or `blocked`
- `validation_commands`: array of commands you ran
- `notes`: concise explanation of what happened

Mark `status` as:

- `improved` when you safely updated source files and regenerated the manifests
- `unchanged` when no matching source change was needed or possible but nothing was wrong locally
- `blocked` when the PR requires risky changes, cannot be traced back to source workflow markdown, or validation fails

## Required approach

1. Inspect the selected Dependabot PRs using GitHub tools and confirm each one is authored by `dependabot[bot]` or `app/dependabot`.
2. Confirm every selected PR touches only compiler-generated workflow manifests such as `.github/workflows/package.json`, `.github/workflows/package-lock.json`, `.github/workflows/requirements.txt`, or `.github/workflows/go.mod`.
3. Treat the dependency batch JSON as the selected dependency payload and use it to enumerate the dependencies to update.
4. Respect the retry-history summary. If it says to stop for human review, do not force another attempt.
5. Honor only the maintainer guidance in the provided maintainer-only feedback summary.
6. For each selected dependency, find the source workflow markdown or shared config files that reference the outdated dependency.
7. Apply all safe version updates to source `.md` files in one pass and do not edit the generated manifest files directly.
8. Regenerate the manifests once with `make dependabot && make build`.
9. If `.github/workflows/package-lock.json` needs refresh after compilation, run `cd .github/workflows && npm install --package-lock-only`.
10. Keep the change bounded to the selected dependency updates plus the smallest number of related source files needed.

## Required validation

After your first substantial edit, immediately run:

```bash
make dependabot && make build
```

If the generated npm manifest changed, also run:

```bash
cd .github/workflows && npm install --package-lock-only
```

If validation fails, fix only the touched slice and rerun the same focused validation.

## Pull request rule

Create a PR only if:

- the fix is real and bounded
- validation passed
- `git diff --stat` shows an actual code change
- the result JSON would report `status: improved`

The PR body must include:

1. original Dependabot PR numbers
2. dependency names and version changes
3. objective
4. retry context used
5. maintainer feedback applied
6. which source workflow files were updated
7. which manifest files were regenerated
8. validation commands you ran

Prefer an ordered list in that exact sequence so burner replacement PRs stay consistent across retries.

Do not directly merge or modify the generated manifest PR itself.

If no safe bounded remediation is possible, do not create a PR. End with a concise blocker report and still write the result JSON.

## Output

End with a concise summary including the selected PR numbers, retry mode, dependency batch handled, source files updated, validation commands run, result file path, and whether a replacement PR was created.

Follow the `reporting` skill: use `###` (h3) or lower for any headers in the comment, and wrap long lists (e.g. source files updated, validation commands) in `<details><summary><b>...</b></summary>...</details>`.