---
title: Adaptive Routing
description: Record which model and effort each routed run used, whether it completed its task, and what it cost, without changing which model runs.
sidebar:
  order: 606
---

[Model routing](/gh-aw/reference/model-routing/) chooses a model and reasoning effort for each run from the task in front of it. Set `learning: observe` under `model-routing` to record which model and effort ran, whether the workflow completed its task, and what it cost. The router keeps making its ordinary pick.

For example, if the router's pick for an issue triage workflow moves from model B to model A, observe mode keeps running A as today and records each run. A week later, `gh aw routing status` might show B with 40 runs, all successful, at 28 credits a run, and A with 9 runs, one of them a failure, at 12 credits a run. That is the evidence a later live mode would act on. Observe mode makes no recommendation of its own.

## Turn it on

Add `learning: observe` to the top-level `model-routing` block of a routed workflow that uses [repo-memory](/gh-aw/reference/repo-memory/):

```aw wrap
---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: read
engine: copilot
model: router
model-routing:
  learning: observe
tools:
  repo-memory: true
safe-outputs:
  add-comment:
---
Read the new issue and post one suggested next step.
```

To turn it on for every eligible workflow in a repository, set the default in `.github/workflows/aw.json` and recompile:

```json title=".github/workflows/aw.json"
{
  "model_routing": { "learning": "observe" }
}
```

A workflow's own `learning` value wins, then the first import that sets `learning`, then the repository default. The default is off. Set `learning: false` to stop recording without disabling routing or deleting history. Only `observe` and `false` are supported.

Learning does not enable routing or require a shortlist. If you also use `allowed-models`, declare the list once, alongside `learning` or in a separate import, and use a literal router model value as usual.

## Which workflows take part

A workflow takes part when its main agent's model is chosen by AWF model routing on a Copilot, Claude, or Codex engine: `model: router`, a fixed level such as `router/balanced`, the [default routing](/gh-aw/reference/model-routing/#default-routing-for-workflows-without-a-model) for workflows without a model, or a model expression that resolves to a router value. It must also enable `tools.repo-memory`, and it must not be a work-queue worker, a reusable workflow (`on: workflow_call`), or run [experiments](/gh-aw/reference/experiments/). A workflow that sets `learning: observe` itself, or through an import, fails to compile with the reason when it does not qualify; the repository default skips such workflows silently.

At run time, gh-aw records only the first attempt of a run whose workflow definition comes from the default branch, so re-runs and pull request runs are not recorded. Every first attempt is recorded once the agent job has run, whether the run succeeded or failed.

## What is recorded

After the run's `conclusion` job has published its unified session, the `push_routing_history` job reads three things from it: the routing verdict (whether the run's result counts as evidence about the model and effort AWF selected, and if not, why), the model and effort AWF selected, and the run's cost as AWF priced it. It appends one line per run to `runs.jsonl`:

```json
{"run": "44156334376", "number": 812, "attempt": 1, "at": "2026-11-19T17:02:40Z", "generation": "<definition hash>", "scope": "copilot/cost/shared", "mode": "observe", "role": "observe", "pair": "github-copilot/model-a@high", "outcome": "failure", "credits": 12.0, "counted": true, "note": null}
```

A run counts as a success or a failure only when the verdict says its result was about the selected model. Otherwise its line explains why it did not count, for example `infrastructure`, `cancelled` or `rate_limited`, or `no_agent` when AWF did not route the run or its agent made no request. If a run's own save fails, the next save finds the run in the workflow's run list and records it as `unsaved`. If a re-run is already in progress, the writer checks the first attempt without waiting for the re-run to finish.

The first save checks earlier runs too. For example, if runs 10 and 11 both ran the agent but failed to save, run 12 records each as `unsaved` once, without assigning either a model outcome. Runs compiled before opt-in or for a dry run have no history job and owe no save. A reset excludes runs at or below its run number. If the first save cannot verify earlier runs, it still records its own result and leaves the unchecked runs for a later save.

`state.json` keeps, for each model and effort, its recent outcomes with their credits and token counts, its average cost, current streak and a summary of its success rate. It also tracks verified reconciliation coverage (`reconciled_through` and `unresolved_runs`) independently of `runs.jsonl`'s 500-line retention window and `last_run`, so an older run that ages out of `runs.jsonl` is never charged twice and an incomplete or failed run-list lookup never advances coverage past a gap. When a run number is missing from the default-branch, `pull_request_target`, and unfiltered listings, it is kept in `unresolved_runs` only while its higher listed neighbour is recent (within 5 minutes of the scan, where GitHub's run list can still lag); once an unfiltered scan covering both sides confirms that the higher listed neighbour is older than 5 minutes and the run number is still absent, it is treated as a deleted run rather than persisting in `unresolved_runs`. Providers report a request's input count either with or without its cached tokens. When AWF's record does not say which, Anthropic counts are treated as excluding cached tokens, OpenAI counts as including them, and for Copilot (or other providers) an input count smaller than the cached count is taken to exclude it and any other to include it, so a Copilot run's token counts are close but not always exact, while its credits are what AWF priced. Editing the workflow starts a new generation: earlier failures are forgiven and successes are kept, a run of the older definition that finishes late does not count against the new one, and reconciliation continues from the previous verified floor so a failed save on the first run of a new definition is not dropped. Records are shared across routing levels unless the workflow chooses its level at run time, in which case each level keeps its own.

`last_run` is the highest recorded run number, not proof that every preceding result was saved. For example, if run 12 saves while run 11 is still active, coverage can reach 12 with 11 listed in `unresolved_runs`. A missing entry in an Actions listing is not proof that 11 saved. Only the writer settles that obligation. Older histories without `reconciled_through` do not yet report verified coverage.

## The branch

Each workflow's history lives on its own branch, `memory/adaptive-routing/<workflow>`, named after the lock file without `.lock.yml`, so renaming the workflow file starts a new history. The first save creates the branch from its own root commit, and every commit is made through the GitHub API, which signs it. gh-aw never force-pushes or deletes the branch.

The agent never sees or writes the history. The branch is not checked out for the agent and is not in its prompt, and no repo-memory entry may use a branch under `memory/adaptive-routing/`, whether it names one directly or through an expression that resolves to one at run time, so the agent's own memory can never reach it.

## The added job and permission

Opting in adds one job, `push_routing_history`. It runs after `conclusion` whenever the agent job ran, with `contents: write` to save the history and `actions: read` to list the workflow's runs. It has no concurrency group. When several runs finish together, each save is committed only on the branch head it read; if another run saved first, the save reads the newer files and applies its run again, so every run lands once. If the save still cannot land, the job fails and the run shows the failure. Dry-run compilations (`--dry-run`) omit `push_routing_history` completely so dry-run executions are neither recorded nor charged as `unsaved`. The agent job, its prompt and its permissions are unchanged.

## Branch rulesets

Commits to the history are signed, so they satisfy rulesets on `memory/**` that require signed commits. The job saves with the workflow's `GITHUB_TOKEN`, so a ruleset that restricts who can update `memory/**` must allow GitHub Actions to update `memory/adaptive-routing/**`; otherwise the save fails and the run reports it.

## Retention

The history keeps the model and effort, the verdict, the credits and the token counts of each run, and never prompts or agent output. In `state.json`, each model and effort keeps its newest 50 outcomes within 60 days; `runs.jsonl` keeps the last 500 runs. Every earlier version of both files remains in the branch's Git history. To remove the history entirely, delete the branch by hand; the next run starts a new one.

## Status and reset

`gh aw routing status` lists the routing histories in the repository. Give it a workflow to see that workflow's evidence: for each scope, every model and effort with its runs, expected and hopeful success rates as of the last save, average credits, streak, tests and suspension, and the reasons recent runs did not count. Add `--json` for machine-readable output.

```bash
gh aw routing status issue-triage
```

`gh aw routing reset <workflow>` starts a history afresh. It reads the workflow's latest run number and, in one commit that lands only if no run saved in the meantime, stops every run up to it from counting, empties the recorded evidence, and clears reconciliation obligations at or below the reset point while preserving any obligations above it. It keeps `runs.jsonl` and never deletes the branch. It needs write access to the repository.

## Verify an observe pilot

For an unreleased build, record the full gh-aw commit SHA. Build the compiler from that commit, pass the same SHA to `compile --gh-aw-ref`, and check that the workflow lock uses `github/gh-aw/actions/setup@<SHA>` before running it. The flag pins the runtime actions; it does not change the compiler you invoked. For example, the [reviewed observe baseline](https://github.com/github/gh-aw/commit/7154b590ec1dda07344713fccefc8e2a4e2a3866) includes `model-routing.learning` and the writer fixes for pagination, deleted runs and active re-runs.

Record the first opted-in Actions run ID, not the first successful save. For an [eligible workflow](#which-workflows-take-part), an observation is due when the first attempt uses the default-branch workflow definition, runs the agent and is not a dry run. Check that its `push_routing_history` job succeeded and its `run` ID appears once in `runs.jsonl`. If the opening run owed no save (for example, a `pull_request` run using a PR ref), record why and check the first eligible attempt without moving the pilot boundary.

Record the history branch head when the last pilot writer finishes. Compare the eligible first attempts from Actions with the rows at that revision by run ID, checking for missing or duplicate rows. Use the writer result from attempt 1 even if the run was later re-run. If the snapshot does not cover the entire pilot, use [earlier branch revisions](#retention) to recover rows that rolled off, counting each appended row only once.

A row with `note: "unsaved"` accounts for a failed save; it does not recover the observation. If two eligible writers failed before the first successful save and history shows neither ID, report both missing IDs. Zero `unsaved` rows in history does not prove that every observation was saved.

## Limits

- Observe mode only records evidence; it never changes which model runs and makes no recommendation.
- A history belongs to one workflow file in one repository; renaming the file starts a new history. Reusable workflows (`on: workflow_call`) cannot take part, because each caller numbers its own runs.
- A run that ends before the agent job starts, or whose usage artifact was never published, is not recorded by its own save.
- A failed or cancelled run whose agent ran and whose `push_routing_history` job did not succeed (whether it failed, was cancelled, timed out, or was skipped) is charged as `unsaved` by a later save. The writer checks the workflow's default-branch and `pull_request_target` run listings, using creation-time windows to stay within GitHub Actions' 1,000-result limit per query. It can also look up a tracked in-progress run directly after it ages out of the listing window.
- The writer checks a backlog in chunks, starting near the earliest unchecked run. A run can have a later timestamp than its neighbours, so absence from one time window is not proof of deletion. If the complete listing is too large to check in one save, the writer can keep the missing run numbers in `unresolved_runs` and account for later runs it did find. For example, an unverified run 2 does not prevent a failed save at run 3 from being recorded. Malformed listings, gaps too large for the 200 unresolved entries, or too many runs created in the same second can still limit progress.
- Each save spends at most 64 requests inspecting newly covered runs, including first-attempt lookups, job pages and generation lookups, even if a concurrent write forces it to retry. When the budget runs out, the writer can still save its own result; the next save resumes from persisted coverage. If a failed save's workflow definition cannot be retrieved at its commit, the writer keeps that run unresolved rather than charging the current definition.
- When a history starts with earlier runs to check, `first_write_run` records that first writer's run number. Later definition edits do not move it. For an earlier `pull_request_target` failure, Actions reports the pull request's head revision, not the trusted revision of the workflow that executed. Without that evidence the run stays unresolved, including on subsequent saves, rather than being attributed to the current definition.
