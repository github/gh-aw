# Git Simulator Lessons

Derived view built from `git-sim-strategies.md` and `git-sim-state.json`, checked against repository code at commit 41e1970 (2026-11-22). Line citations were re-located on 2026-11-22; several earlier line numbers had drifted. Classifications:

- **code-verified**: current repository code shows the behavior.
- **measured**: a simulator measurement that this review did not reproduce.
- **conflict**: records disagree; unresolved.
- **policy**: an operational constraint, not a software behavior.

Only code-verified lessons are actionable. None of them currently produces a configuration signal.

### Lesson 1

**Title:** create-pull-request allows one successful call per run by default
- **Scope:** create-pull-request without an explicit `max`, in one workflow run.
- **Evidence:** when `max` is unset the default is 1. A later call returns `E002 ... limit reached — N of M already used this run`. The simulator recorded three of four parallel create_pull_request calls failing this way on 2026-08-10 (idx 196, 197, 199).
- **Citations:** `pkg/workflow/create_pull_request.go:215-218`; `actions/setup/js/safe_outputs_handlers.cjs:489`.
- **Caveats:** this is a quota rejection, not a patch-size or git-size failure. The simulator's per-cell fan-out spends the single slot. It does not show that the cells' configurations are broken.
- **Classification:** code-verified boundary; not a configuration outcome.

### Lesson 2

**Title:** max-patch-files defaults to 100; exactly 100 files is accepted
- **Scope:** create-pull-request patch file count under the default configuration.
- **Evidence:** `MAX_FILES = 100` (line 527). The check at line 608 rejects only when `fileCount > limit`, so 100 files pass and 101 fail. The config value is read at line 777. The simulator sets `max-patch-files: 200` (its config line 153), so its batch=100 cells are not at the cap.
- **Citations:** `actions/setup/js/create_pull_request.cjs:527`; `actions/setup/js/create_pull_request.cjs:608`; `actions/setup/js/create_pull_request.cjs:777`; `.github/aw/safe-outputs-runtime.md:270`.
- **Caveats:** push-to-pull-request-branch has a separate "exceeds max-patch-files" check. Its test expectation is at `actions/setup/js/push_signed_commits.test.cjs:2991` (recorded as 2953 on 2026-11-15), and its boundary was not re-read here. This closes the simulator's open ">" versus ">=" question for create-pull-request only.
- **Classification:** code-verified.

### Lesson 3

**Title:** max-patch-size defaults to 4096 KB; the simulator overrides it to 5120 KB
- **Scope:** create-pull-request and push-to-pull-request-branch size caps.
- **Evidence:** the default is 4096 (KB) in the global config and in the pull-request handler registry. The simulator sets `max-patch-size: 5120` on both tools. The earlier headroom analysis compared xlarge cells against 4096, which is the default and not this workflow's cap.
- **Citations:** `pkg/workflow/safe_outputs_config_global.go:101`; `pkg/workflow/safe_outputs_handler_registry_pull_requests.go:359`; `.github/workflows/daily-safeoutputs-git-simulator.md:152`; `.github/workflows/daily-safeoutputs-git-simulator.md:164`.
- **Caveats:** earlier cited locations no longer resolve (Lesson 10). Headroom percentages are measurements and were not verified here.
- **Classification:** code-verified for the default and the override; headroom figures are measured.

### Lesson 4

**Title:** push-to-pull-request-branch cannot chain to a PR created in the same run
- **Scope:** push-to-pull-request-branch after create-pull-request in the same run (simulator ahead and diverged cells).
- **Evidence:** incremental patch generation needs a PR-head baseline that exists before the agent runs, and it does not fetch. The `pull_request_number` value is read raw from the message (`push_to_pull_request_branch.cjs:621` and `:886`), and the `push_to_pull_request_branch` schema entry declares no temporary-ID property for it (`actions/setup/js/safe_outputs_tools.json:1468`). The simulator's failures on 2026-08-11 (idx 201) and 2026-08-13 (idx 204) match this mechanism.
- **Citations:** `actions/setup/js/generate_git_patch.cjs:36-37`; `actions/setup/js/generate_git_patch.cjs:518`; `actions/setup/js/push_to_pull_request_branch.cjs:621`; `actions/setup/js/push_to_pull_request_branch.cjs:886`; `actions/setup/js/safe_outputs_tools.json:1468`.
- **Caveats:** the error text recorded on 2026-08-13 differs from current wording ("could not be fetched" versus "never fetches"). The simulator's "DEFINITIVE" label rests on one run per cell. The code-level reason is what supports the conclusion. This is not a regression.
- **Classification:** code-verified mechanism. Not a retry target: measure ahead and diverged cells locally, and do not re-attempt same-run chaining.

### Lesson 5

**Title:** temporary_id echo: the recorded run conflicts with current code
- **Scope:** the `temporary_id` field on create-pull-request.
- **Evidence:** the simulator recorded on 2026-08-13 that `temporary_id` was not echoed back. Current generic handler code adds `temporary_id` to the success response when `temporary_id_field` is configured. Config generation copies the connector's `x-gh-aw-temporary-id` marker into `temporary_id_field` (`pkg/workflow/safe_outputs_config_generation.go:249-250`). Whether the simulator's run had it configured is not recorded.
- **Citations:** `actions/setup/js/safe_outputs_handlers.cjs:690`; `actions/setup/js/safe_outputs_handlers.cjs:703`; `pkg/workflow/safe_outputs_config_generation.go:249-250`.
- **Caveats:** unresolved. Lesson 4 still blocks same-run chaining under either version. Do not act on either version until a run records its configuration.
- **Classification:** conflict.

### Lesson 6

**Title:** "No changes to commit" for simulated cells reflects handler workspace visibility
- **Scope:** create-pull-request and bundle generation for simulator repos outside the workspace.
- **Evidence:** on 2026-11-13 all four cells (idx 216-219) returned `No changes to commit - no commits found`. Git commands default to the explicit cwd option, then `GITHUB_WORKSPACE`, then `process.cwd()`. The simulator says its commits live under `/tmp/gh-aw/agent/git-sim-*`, which that default does not reach.
- **Citations:** `actions/setup/js/generate_git_bundle.cjs:123`; `actions/setup/js/generate_git_bundle.cjs:575`.
- **Caveats:** the simulator did not record whether it passed a cwd option. This attribution is consistent with the default but not proven. The state file records these cells as `error`. Treat that as a harness limitation, not a configuration outcome.
- **Classification:** harness limitation; consistent with code; not reproduced.

### Lesson 7

**Title:** local sizing is measurement, not safe-output verification
- **Scope:** any "pass" that rests only on local git measurement, with no real safe-output response.
- **Evidence:** a 2026-08-10 review found that earlier passes may have been recorded from local measurement alone. The only evidence for the safe-output dimension is the tool response, which carries `result` plus patch and bundle metadata. Size laws the simulator recorded are measurements this review did not reproduce: about 0.27 KB of header per file, same-file re-append up to about 3x, a topic-name leak in merge-message artifacts, and two-dot versus three-dot polarity that differs by git command.
- **Citations:** `actions/setup/js/safe_outputs_handlers.cjs:1291-1297`.
- **Caveats:** the 2026-08-10 doubt about early cells is unresolved. Treat the size laws as hypotheses to re-measure, not as caps.
- **Classification:** measured (size laws); the evidence rule is code-verified.

### Lesson 8

**Title:** the tested map mixes two conventions
- **Scope:** the `tested` map in `git-sim-state.json` and cell selection in the simulator workflow.
- **Evidence:** the workflow defines `tested` as outcome `pass|fail|error|rejected` and skips any listed cell. The state file keeps only non-pass outcomes (9 entries out of 220 recorded cells, using `fail`, `error`, and `rejected`) and treats a missing entry below `next_index` as pass. The cursor keeps this workable only while `next_index` advances consistently. The state file and the strategy log label cell 204 differently (`error` versus "push failed"). Both describe the same failed push, so this is a labeling inconsistency, not a substantive conflict.
- **Citations:** `.github/workflows/daily-safeoutputs-git-simulator.md:232`; `.github/workflows/daily-safeoutputs-git-simulator.md:287`.
- **Caveats:** a missing entry is not a verified pass for the safe-output dimension (Lesson 7). The regression refill (fewer than four untested cells) is not near: 220 of 3600 cells are recorded.
- **Classification:** conflict in the producer's convention; not product behavior.

### Lesson 9

**Title:** real safe-output probes need explicit authorization
- **Scope:** the simulator's real create-pull-request, push, and create-issue calls against github/gh-aw.
- **Evidence:** the simulator halted real probing from 2026-08-16 through 2026-11-08, citing the outer safe-outputs policy against placeholder probes. The 2026-11-11 run withheld safe outputs. The 2026-11-13 run made four create_pull_request calls that returned no-change errors (Lesson 6). Escalation #74693 was recorded as open with no comments on 2026-11-08; this review did not re-check it. The safe-output config sets no target-repo.
- **Citations:** `.github/workflows/daily-safeoutputs-git-simulator.md:136-164`.
- **Caveats:** this is an operational constraint, not a software behavior. It must not produce cell outcomes or signals.
- **Classification:** policy.

### Lesson 10

**Title:** producer citations are stale; re-locate before use
- **Scope:** file and line references in `git-sim-strategies.md`.
- **Evidence:** checked 2026-11-15 and re-checked 2026-11-22 at commit 41e1970. `compiler_types.go` has 97 lines on 2026-11-22 (95 on 2026-11-15), so the cited line 734 does not exist. `safe_outputs_config.go` and `handler_registry.go` are absent at the cited paths. The max-patch-files default is now at `safe-outputs-runtime.md:270`, not 204. The simulator's max-patch-files line is now 153, not 46. The "exceeds max-patch-files" expectation is at `push_signed_commits.test.cjs:2991` (2953 on 2026-11-15; not 2143). The 4096 KB default is now at `safe_outputs_config_global.go:101`, not 100. `.claude/agents/config-simulator.md` is absent from this checkout. The cell-to-index formula still reproduces the state entries for idx 196, 197, 199, 201, 204, and 216-219, so the index mapping is sound even though the locations drifted.
- **Citations:** `pkg/workflow/compiler_types.go:1-97`; `.github/aw/safe-outputs-runtime.md:270`; `pkg/workflow/safe_outputs_config_global.go:101`.
- **Caveats:** only the paths and lines listed above were checked. Other producer citations were not audited.
- **Classification:** correction (verified).
