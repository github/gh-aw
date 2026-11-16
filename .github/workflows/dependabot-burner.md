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
  job-discriminator: ${{ github.run_id }}
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
Apply this contract before repository setup guidance. Every inline subagent has
its own bounded contract; do not create nested delegations.

- Read the selection with `cat /tmp/gh-aw/agent/dependabot-burner/context.json`, not sed.
- Inspect local changes with `git status` or `git diff --stat`. Do not add global
  Git flags such as --no-pager or query remotes.
- Use the configured GitHub tools through the CLI proxy for PR files, diffs,
  comments, reviews, and author/maintainer metadata. The executable is `github`,
  not a standalone tool name: start with `github --help` or
  `github pull_request_read --help`, then use `github pull_request_read` with
  the documented JSON arguments. Do not use gh commands, executable discovery,
  shell help, authentication checks, or credential setup.
- Run only declared validation commands. Do not wrap GitHub reads in shell loops
  or add undeclared utilities or pipeline stages.
- After the first tool-permission denial, stop shell calls in that agent and
  return the concrete blocker to the orchestrator. Do not probe variants or
  relaunch the denied agent. Continue other independently validated items without
  changing permissions or the denial budget.
- Invoke each analyzer at most once. Analyzers cannot edit or delegate; their
  explicit tool lists are narrower than the orchestrator's. Use the read tool
  for file-only analysis, not Bash.

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
2. If `selected_batch_pr_numbers` is empty, use `noop` with a short explanation that includes `open_dependabot_pr_count` and the excluded PRs with their paths and reasons. Do not claim that all dependency updates are handled or that no open Dependabot PRs exist when `excluded_prs` is non-empty. Stop without invoking retry or remediation agents.
3. Use the `retry-history-analyzer` exactly once to inspect the selected PRs, the provided recent failed burner PRs, and maintainer-only comments or reviews. An empty `recent_failed_burns` means there are no supplied failed burns to inspect; do not search the filesystem for history or relaunch the analyzer. Still inspect selected-PR feedback, and distinguish unavailable feedback from confirmed absence.
4. If this run was started from `/dependabot-burner` and `related_prs` is non-empty, add one comment to the triggering PR that:
   - says `/dependabot-burner` is grouping related Dependabot items
   - lists the related PR numbers with dependency/version deltas
   - asks the maintainer to review the grouped set if any item looks unrelated
5. Use the `dependency-batch-analyzer` subagent to summarize the selected dependency batch and likely source files before editing. Inspect each PR's changed files and diff to enumerate actual updates, especially grouped PRs and lockfile-only updates; title parsing is only a hint.
6. If the retry-history summary confirms `first_attempt` with no maintainer feedback, pass that summary directly to the remediator; do not invoke the `retry-feedback-synthesizer`. Otherwise invoke the synthesizer once with the supplied summary only. Do not invent missing history or feedback. Propagate a `stop_for_human` result without attempting edits.
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
tools: [read]
---
Read `/tmp/gh-aw/agent/dependabot-burner/context.json` with the read tool and verify the grouped PR selection.
This is file-only analysis: no shell, GitHub queries, edits, or delegation.

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

## end agent: `pr-group-analyzer`

## agent: `retry-history-analyzer`
---
description: Extracts retry guidance from failed burner PRs and maintainer-only feedback
model: mai-code
tools: [read, bash]
---
Use the selected PR numbers plus `/tmp/gh-aw/agent/dependabot-burner/context.json` to inspect recent closed burner PRs and maintainer-only comments or reviews.

### Tool contract

- Read context with the read tool or `cat /tmp/gh-aw/agent/dependabot-burner/context.json`.
- The GitHub executable is `github`: use `github --help` and `github pull_request_read --help`,
  then call `github pull_request_read` with documented JSON arguments for reviews/comments.
  Use `github issue_read --help` for issue comments. Never invoke tool names as executables.
- Do not run gh, auth checks, executable discovery, shell help, Git commands, shell
  loops, pipelines, or compound commands. Do not edit or delegate.
- After the first permission denial, stop shell calls and return the blocker.
  Do not probe variants. Missing feedback is unknown, not an empty result.
- Use only supplied history and selected PRs. If `recent_failed_burns` is empty,
  do not search local files for failed burns. Inspect selected-PR feedback once;
  return `first_attempt` when no failed burns or maintainer feedback are found.

Return compact JSON with:

- `retry_mode` (`first_attempt`, `retry_with_feedback`, or `stop_for_human`)
- `recent_failed_pr_numbers`
- `maintainer_feedback_summary`
- `strategy_adjustments`
- `blocking_reason`

Only keep feedback from maintainers/admins/writers. Ignore comments from bots and non-maintainers. Focus on concrete retry signals such as CI failures, rejected scope, or explicit maintainer requests.

## end agent: `retry-history-analyzer`

## agent: `dependency-batch-analyzer`
---
description: Summarizes the selected Dependabot batch and maps it to likely source files
model: mai-code
tools: [read, bash]
---
Read the selected dependency batch and inspect each PR's diff, including grouped and lockfile-only updates.

### Tool contract

- Read context with the read tool or `cat /tmp/gh-aw/agent/dependabot-burner/context.json`.
- The GitHub executable is `github`: use `github --help` and `github pull_request_read --help`.
  Call `github pull_request_read` with documented JSON arguments for each selected PR's
  files and diff. Tool names such as pull_request_read are not executables.
- Do not run gh, auth checks, executable discovery, shell help, Git commands, shell
  loops, pipelines, or compound commands. Do not edit or delegate.
- After the first permission denial, stop shell calls and return the blocker.
  Do not probe variants or infer actual updates from titles instead of unavailable diffs.

Return compact JSON with:

- `dependencies`
- `likely_source_files`
- `manifest_families`
- `risk_notes`

Keep the output short and evidence-first.

## end agent: `dependency-batch-analyzer`

## agent: `retry-feedback-synthesizer`
---
description: Distills retry history and maintainer-only feedback into execution constraints
model: mai-code
tools: []
---
Use only the supplied retry-history analysis and maintainer-only feedback summary.
No tools, filesystem/history discovery, edits, or delegation are needed.
Return compact JSON with:

- `retry_mode`
- `must_follow`
- `must_avoid`
- `blocking_reason`

Do not invent new constraints. Only restate concrete retry and maintainer signals that should affect this one optimistic replacement PR attempt.

## end agent: `retry-feedback-synthesizer`

## agent: `dependabot-remediator`
---
description: Executes the single grouped Dependabot remediation wave inside dependabot-burner
model: inherited
tools: [read, search, edit, bash]
---
You execute one grouped Dependabot remediation wave inside `dependabot-burner`.
Use the supplied objective, selected PR numbers, exact dependency batch JSON,
retry-history summary, and maintainer-only feedback. Do not delegate.

## Tool contract

- Read files with the read/search tools; use edit/create tools to write changes and
  the result JSON. Do not use shell redirections or helpers to write files.
- The GitHub executable is `github`: use `github --help` and `github pull_request_read --help`,
  then `github pull_request_read` with documented JSON arguments. Tool names are not
  executables. Do not use gh, auth checks, executable discovery, or shell help.
- Inspect changes with `git status` or `git diff --stat`; do not add global Git
  flags, query remotes, commit, push, or mutate PRs directly.
- Run only declared validation commands. No shell loops, undeclared helpers,
  pipelines, or compound commands except the explicitly allowed validation chains.
- After the first permission denial, stop shell calls. Write a blocked result with
  the concrete unverified items; do not probe variants or claim validation passed.
- Publish only through the `safeoutputs` executable. Use `safeoutputs --help` and
  `safeoutputs create_pull_request --help`, then documented JSON arguments. The
  compiler-managed safe output, not Git or gh, owns publication.

## Deterministic result

Always write one result JSON file for this wave, even if the work is blocked or no change is applied.

Write the result file to this runtime-resolved path pattern:

`/tmp/gh-aw/agent/dependabot-burner/results/${{ github.run_id }}-result.json`

Include `pr_numbers`, `dependencies_processed` (batch summaries),
`source_files_updated`, `lock_files_updated`, `unaddressed_prs` (PR numbers/reasons),
`fix_applied` and `replacement_pr_created` (booleans), `retry_strategy`,
`maintainer_feedback_used`, `status`, `validation_commands`, and `notes`.
Use `improved` only for real, validated dependency updates; `unchanged` only for
already-applied updates; `blocked` when no safe bounded update passes validation.
Safe-output submission is not proof that a PR was created: keep
`replacement_pr_created` false unless publication is confirmed.

## Required approach

1. Confirm every selected PR is Dependabot-authored. Inspect every diff, including
   grouped and lockfile-only updates; titles are hints, not dependency specifications.
   Consider every path/ecosystem. Compare with checkout; never downgrade or reapply updates.
2. Respect retry history and maintainer-only feedback. If `stop_for_human`, write
   a blocked result without editing.
3. For generated `.github/workflows/package.json`, `package-lock.json`,
   `requirements.txt`, `go.mod`, or `.lock.yml` updates, change the originating
   workflow markdown/shared configuration, not generated declarations. Update other
   source manifests/Dockerfiles and matching locks directly; follow companion `.in` files.
4. Apply compatible updates once. Lockfile-only security updates are valid.
   Preserve hashes, digests, Python wheel/platform constraints, and integrity checks.
   Never fabricate hashes or remove pins.
5. Run `make dependabot && make build` only for changed workflow sources.
   Refresh only affected locks using repository commands and lock headers.
   Use `--ignore-scripts` for npm refreshes; refresh generated workflow npm locks
   with `cd .github/workflows && npm install --package-lock-only` only when needed.
6. Bound edits to selected updates and their smallest directly related source slice.

## Required validation

Validate the touched ecosystems, not unrelated manifests:

- Workflow sources: `make dependabot && make build`; refresh generated npm locks if needed.
- Docs/action npm: `npm --prefix <directory> install --package-lock-only --ignore-scripts`,
  then the declared docs build or focused action tests.
- Root Go: `go mod tidy`, `go mod verify`, focused dependency tests.
- Named Python requirements: follow lock-header generator/platform constraints;
  verify hashes with wheel-only `python -m pip download --require-hashes --only-binary=:all:`
  into a disposable directory. Do not install or execute downloaded packages.
- Docker/action pins: verify exact upstream version/digest and bounded diff;
  use existing allowed static checks. Do not push images or start containers.

Unavailable tooling/checks block the affected PR. Fix failed validation only in
the touched slice and rerun that check. A blocked item need not prevent one PR
for a successfully validated subset; explain every unaddressed selected PR.

## Pull request rule

Create a PR only if:

- the fix is real and bounded
- validation passed
- `git diff --stat` shows an actual dependency update, including valid lockfile-only changes
- the result JSON would report `status: improved`

Use an ordered PR body: original Dependabot PR numbers; dependency/version deltas;
objective; retry context; maintainer feedback; updated sources/Dockerfiles/workflows;
updated locks/generated manifests and unaddressed PRs with reasons; validation commands.

Do not directly merge or modify the generated manifest PR itself.

If no safe bounded remediation is possible, do not create a PR. End with a concise blocker report and still write the result JSON.

## Output

End with a concise summary including the selected PR numbers, retry mode, dependency batch handled, source files updated, validation commands run, result file path, and whether a replacement PR was created.

Follow the `reporting` skill: comment headers must be h3 or lower; collapse long
lists in `<details><summary><b>...</b></summary>...</details>`.

## end agent: `dependabot-remediator`