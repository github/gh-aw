---
private: true
emoji: "🔥"
name: Dependabot Burner
description: Runs one grouped Dependabot remediation wave from schedule, manual dispatch, or /dependabot-burner on pull requests
intent: Reduce maintainer effort resolving open Dependabot updates across all repository dependency ecosystems.
on:
  roles: [admin, maintainer, write]
  schedule: weekly
  workflow_dispatch:
    inputs:
      objective:
        description: Burn objective override
        type: string
        required: false
        default: Consolidate open Dependabot updates across repository dependency manifests, lockfiles, Dockerfiles, and workflow sources into one validated replacement PR.
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
model: gpt-5
engine:
  id: copilot
  copilot-sdk: true
skip-if-decision:
  - >-
    Does @/tmp/gh-aw/agent/dependabot-burner/context.json conclusively establish
    that the selected remediation batch and excluded_prs are both empty and the objective requires
    no dependency updates? Abstain if selection is incomplete
    or inconsistent, or a slash-command trigger still needs an explanation.
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
  create-pull-request:
    title-prefix: "[dependabot-burner] "
    expires: "3d"
    labels: [automation, dependencies, dependabot]
    reviewers: [copilot]
    protected-files: request_review
    exclude-lock-yml: true
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
    - "npm --prefix docs install --package-lock-only --ignore-scripts"
    - "npm --prefix actions/setup/js install --package-lock-only --ignore-scripts"
    - "npm --prefix docs run build"
    - "npm --prefix actions/setup/js run test:js -- *"
    - "go mod tidy"
    - "go mod verify"
    - "go test *"
    - "uv pip compile *"
    - "python -m pip download --require-hashes --only-binary=:all: *"
    - "git status"
    - "git diff *"
    - "git diff --stat"
    - "cat *"
    - "rg *"
steps:
  - name: Pre-fetch dependabot burner context
    uses: actions/github-script@v9.0.0
    env:
      BURN_OBJECTIVE: ${{ inputs.objective }}
    with:
      script: |
        const fs = require('fs');
        const path = require('path');

        const objective = (process.env.BURN_OBJECTIVE || '').trim() || 'Consolidate open Dependabot updates across repository dependency manifests, lockfiles, Dockerfiles, and workflow sources into one validated replacement PR.';
        const outPath = '/tmp/gh-aw/agent/dependabot-burner/context.json';

        function parseBumpTitle(title) {
          const match = String(title || '').match(/^(?:[\w-]+(?:\([^)]*\))?!?:\s*)?Bump\s+(.+?)\s+from\s+([^\s]+)\s+to\s+([^\s]+)(?:\s+.*)?$/i);
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
          const basename = path.posix.basename(filename);
          if (basename === 'package.json' || basename === 'package-lock.json') {
            return 'npm';
          }
          if (/^requirements.*\.(txt|in)$/.test(basename)) {
            return 'pip';
          }
          if (basename === 'go.mod' || basename === 'go.sum') {
            return 'go';
          }
          if (/^Dockerfile(?:\..*)?$/.test(basename)) {
            return 'docker';
          }
          if (filename.startsWith('.github/workflows/') && /\.ya?ml$/.test(basename)) {
            return 'github-actions';
          }
          return 'other';
        }

        function summarizeFamilies(files) {
          return [...new Set((files || []).map(normalizeManifestFamily))].sort();
        }

        function isCompiledWorkflowLock(filename) {
          return filename.startsWith('.github/workflows/') && filename.endsWith('.lock.yml');
        }

        function describePull(pull, changedFiles) {
          const relevantFiles = changedFiles.filter((filename) => !isCompiledWorkflowLock(filename));
          const manifestFiles = relevantFiles.length > 0 ? relevantFiles : changedFiles;
          return {
            number: pull.number,
            title: pull.title,
            ...parseBumpTitle(pull.title),
            changed_files: changedFiles,
            manifest_files: manifestFiles,
            manifest_families: summarizeFamilies(manifestFiles),
            created_at: pull.created_at,
            updated_at: pull.updated_at,
            url: pull.html_url,
          };
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
          return files.map((file) => file.filename);
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

            const changedFiles = await loadPullFiles(pull.number);
            candidates.push(describePull(pull, changedFiles));
          }

          return {
            candidates: candidates.sort((a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime()),
            excludedPRs: [],
            openDependabotPRCount: candidates.length,
          };
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
        let { candidates: openPRs, excludedPRs, openDependabotPRCount } = await listOpenDependabotPRs();
        const recentFailedBurns = await listRecentClosedBurnerPRs();

        let triggerPR = openPRs.find((pull) => pull.number === triggerPRNumber) || null;
        let selectionReason = triggerPRNumber ? 'slash-command-trigger-not-dependabot' : 'bundle-all-open-dependabot-prs';
        let selectedPRs = openPRs;

        if (triggerPRNumber) {
          if (!triggerPR) {
            const pull = await github.rest.pulls.get({
              owner: context.repo.owner,
              repo: context.repo.repo,
              pull_number: triggerPRNumber,
            });
            const author = pull.data.user?.login || '';
            if (pull.data.state === 'open' && (author === 'dependabot[bot]' || author === 'app/dependabot')) {
              const changedFiles = await loadPullFiles(triggerPRNumber);
              openDependabotPRCount++;
              triggerPR = describePull(pull.data, changedFiles);
              openPRs.push(triggerPR);
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
            if (!selectedPRs.some((pull) => pull.number === triggerPR.number)) {
              selectedPRs.unshift(triggerPR);
            }
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
          open_dependabot_pr_count: openDependabotPRCount,
          excluded_prs: excludedPRs,
          selected_batch_pr_numbers: selectedPRs.map((pull) => pull.number),
          selected_batch_dependencies: selectedPRs.map((pull) => ({
            pr_number: pull.number,
            dependency_name: pull.dependency_name,
            current_version: pull.current_version,
            target_version: pull.target_version,
            title_parse_mode: pull.title_parse_mode,
            changed_files: pull.changed_files,
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
    question: Did the agent analyze the selected grouped Dependabot remediation batch? Return UNKNOWN when the output records a skip-if-decision no-op before agent execution.
  - id: remediation_reported
    question: Did the agent create a remediation pull request or clearly report why no remediation was needed or possible, including a reason for each selected PR left unaddressed?
  - id: repository_dependency_scope
    question: When open Dependabot PRs were provided, did the agent consider every selected PR regardless of manifest path or dependency ecosystem, without excluding source manifests, lockfiles, named Python requirements, Go modules, or Dockerfiles solely for being outside compiler-generated workflow manifests? Return UNKNOWN when no open Dependabot PRs were provided.
features:
  gh-aw-detection: true
---

# Dependabot Burner

The AWF decision gate may skip an empty, non-actionable remediation wave only
when no excluded PRs need explanation. Slash-command explanations and uncertain
selection still require execution.

You are the grouped Dependabot remediation orchestrator.

## Tool contract for every agent

This workflow has a restricted shell allowlist, not a general-purpose terminal.
Apply this contract before repository setup guidance and pass it to every
subagent, including nested delegations:

- Read the selection with `cat /tmp/gh-aw/agent/dependabot-burner/context.json`, not sed.
- Inspect local changes with `git status` or `git diff --stat`. Do not add global
  Git flags such as --no-pager or query remotes.
- Use the configured GitHub tools through the CLI proxy for PR files, diffs,
  comments, reviews, and author/maintainer metadata. Do not use gh commands,
  authentication checks, or credential setup; authentication is workflow-managed.
- Run only declared validation commands. Do not wrap GitHub reads in shell loops
  or add undeclared utilities or pipeline stages.
- After the first tool-permission denial, stop that command strategy. Use a
  declared alternative or report the affected item as blocked; do not probe
  command variants or exhaust the denial budget. Keep the existing guardrails.

## Read first

1. Read `/tmp/gh-aw/agent/dependabot-burner/context.json`.

## Operating model

- Run optimistically and aim for exactly one bounded remediation wave that can produce at most one replacement PR.
- For scheduled or manual runs, consider all open Dependabot PRs across the repository, regardless of changed paths or dependency ecosystem.
- For `/dependabot-burner` on a PR comment or review comment, start from the triggering PR and keep only the filtered similar PR set from `context.json`.
- If the triggering PR is not an open Dependabot PR, explain that clearly and stop.
- `open_dependabot_pr_count` and `open_pr_count` include all open Dependabot PRs; slash-command grouping may select a smaller related batch. An empty selected batch does not mean there are no Dependabot PRs.
- Consider source and generated manifests, lockfile-only updates, standalone hash-pinned Python requirements, docs/action packages, root Go modules, Dockerfiles, and GitHub Actions updates. Mixed source/generated PRs are eligible. Do not exclude a PR solely because of its paths or ecosystem.
- Report `excluded_prs` if present. For every selected PR not included in the replacement, explain the concrete conflict, validation failure, already-applied update, or other blocker. Unsupported tooling is a blocker to report, not a reason to silently omit the PR.
- Review `recent_failed_burns` before remediation so the next attempt does not repeat a failed retry pattern.
- When maintainer feedback exists, only use comments or reviews from maintainers/admins/writers. Ignore all other commenters when shaping the next attempt.
- Use subagents to analyze the PR group, synthesize retry guidance, and run one bounded remediation wave inside this workflow.

## Required behavior

1. Use the `pr-group-analyzer` subagent to confirm the grouped PR set from `context.json`. Preserve the full selected batch for scheduled/manual runs; only slash-command runs use shared-file similarity filtering.
2. Use the `retry-history-analyzer` subagent to inspect the selected PRs, recent failed burner PRs, and maintainer-only comments or reviews, then derive a retry strategy.
3. If `selected_batch_pr_numbers` is empty, use `noop` with a short explanation that includes `open_dependabot_pr_count` and the excluded PRs with their paths and reasons. Do not claim that all dependency updates are handled or that no open Dependabot PRs exist when `excluded_prs` is non-empty.
4. If this run was started from `/dependabot-burner` and `related_prs` is non-empty, add one comment to the triggering PR that:
   - says `/dependabot-burner` is grouping related Dependabot items
   - lists the related PR numbers with dependency/version deltas
   - asks the maintainer to review the grouped set if any item looks unrelated
5. Use the `dependency-batch-analyzer` subagent to summarize the selected dependency batch and likely source files before editing. Inspect each PR's changed files and diff to enumerate actual updates, especially grouped PRs and lockfile-only updates; title parsing is only a hint.
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
- total open Dependabot PR count and excluded PR numbers with paths/reasons
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

For scheduled/manual runs, retain every PR in the precomputed selected batch.
For slash-command runs, treat PRs as related when they share one of the triggering files or are already present in the precomputed selected batch.
Do not exclude source manifests, named requirements, root modules, Dockerfiles, lockfile-only changes, or mixed source/generated updates.
Preserve the distinction between an empty selected batch and no open Dependabot
PRs; include `excluded_prs` and their reasons in the rationale and no-op explanation.

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
Read the selected dependency batch and inspect each PR's diff, including grouped and lockfile-only updates. Return compact JSON with:

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
- `source_files_updated`: array of source manifests, Dockerfiles, workflow markdown, or shared files you changed
- `lock_files_updated`: array of dependency lockfiles you changed
- `unaddressed_prs`: array of selected PR numbers and concrete reasons they were not remediated
- `fix_applied`: boolean
- `replacement_pr_created`: boolean
- `retry_strategy`: concise retry mode used for this wave
- `maintainer_feedback_used`: concise summary of maintainer guidance applied
- `status`: `improved`, `unchanged`, or `blocked`
- `validation_commands`: array of commands you ran
- `notes`: concise explanation of what happened

Mark `status` as:

- `improved` when you safely applied selected dependency updates and passed the relevant validation
- `unchanged` when the selected dependency updates are already present and no further change is needed
- `blocked` when no safe bounded update can pass validation

## Required approach

1. Inspect the selected Dependabot PRs using GitHub tools and confirm each one is authored by `dependabot[bot]` or `app/dependabot`.
2. Consider every selected PR regardless of paths or ecosystem. Read its diff to distinguish compiler-generated workflow manifests from source manifests, standalone requirements, dependency locks, Dockerfiles, and action version pins.
3. Use the dependency batch JSON to identify the selected PRs, then enumerate all actual dependency/version changes from their diffs. Grouped PR titles and fallback-parsed titles are not individual dependency specifications. Compare against the current checkout; do not downgrade dependencies or reapply already-present updates.
4. Respect the retry-history summary. If it says to stop for human review, do not force another attempt.
5. Honor only the maintainer guidance in the provided maintainer-only feedback summary.
6. For compiler-generated `.github/workflows/package.json`, `package-lock.json`, `requirements.txt`, and `go.mod`, update the originating workflow markdown or shared configuration, not the generated declarations. Likewise, trace compiled `.lock.yml` action updates back to their workflow source. For other files, update their actual source manifest or Dockerfile and matching lockfiles directly; use companion `.in` files when a named requirements lock declares one.
7. Apply compatible selected updates in one pass. Lockfile-only security updates are valid changes even when no source manifest edit is needed. Preserve existing digest/hash pinning, Python wheel/platform constraints, and dependency integrity checks; never fabricate hashes or remove pins to make validation pass.
8. Regenerate compiler-generated manifests with `make dependabot && make build` only when their workflow sources changed. Do not run that generator as a substitute for updating docs/action packages, root Go modules, or named requirements.
9. Refresh only affected dependency locks using the repository's existing package-manager commands and lockfile header instructions. Use `--ignore-scripts` for npm lock refreshes. If `.github/workflows/package-lock.json` needs refresh after compilation, run `cd .github/workflows && npm install --package-lock-only`.
10. Keep the change bounded to the selected dependency updates plus the smallest number of related source files needed.

## Required validation

Validate the touched ecosystems, not unrelated manifests:

- Workflow source updates: `make dependabot && make build`; refresh the generated npm lock only if needed.
- Docs/action npm updates: refresh the affected lock with `npm --prefix <directory> install --package-lock-only --ignore-scripts`, then run the relevant existing build or focused tests.
- Root Go updates: `go mod tidy`, `go mod verify`, and focused tests covering the changed dependency.
- Named Python requirements: follow the lock header's generator/platform constraints and verify the resulting hashes with a wheel-only `python -m pip download --require-hashes --only-binary=:all:` command into a disposable directory; do not install or execute downloaded packages for this check.
- Docker/action pins: verify the exact upstream version/digest and inspect the bounded diff; run existing relevant static checks, without pushing images or starting containers.

If a needed tool or check is unavailable, report the affected PR as blocked rather than claiming validation passed. If validation fails, fix only the touched slice and rerun the same focused validation. A blocked item need not prevent one replacement PR for the successfully validated subset; list every unaddressed selected PR and its reason.

## Pull request rule

Create a PR only if:

- the fix is real and bounded
- validation passed
- `git diff --stat` shows an actual dependency update, including valid lockfile-only changes
- the result JSON would report `status: improved`

The PR body must include:

1. original Dependabot PR numbers
2. dependency names and version changes
3. objective
4. retry context used
5. maintainer feedback applied
6. which source manifests, Dockerfiles, or workflow files were updated
7. which dependency locks or generated manifests were updated, plus every selected PR left unaddressed and why
8. validation commands you ran

Prefer an ordered list in that exact sequence so burner replacement PRs stay consistent across retries.

Do not directly merge or modify the generated manifest PR itself.

If no safe bounded remediation is possible, do not create a PR. End with a concise blocker report and still write the result JSON.

## Output

End with a concise summary including the selected PR numbers, retry mode, dependency batch handled, source files updated, validation commands run, result file path, and whether a replacement PR was created.

Follow the `reporting` skill: use `###` (h3) or lower for any headers in the comment, and wrap long lists (e.g. source files updated, validation commands) in `<details><summary><b>...</b></summary>...</details>`.