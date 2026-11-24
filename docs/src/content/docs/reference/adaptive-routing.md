---
title: Adaptive Routing
description: Record which model and effort each routed run used, whether it completed its task, and what it cost, without changing which model runs.
sidebar:
  order: 606
---

[Model routing](/gh-aw/reference/model-routing/) chooses a model and reasoning effort for each run from the task in front of it. Adaptive routing adds what the workflow itself has learned: which model and effort each run used, whether the run completed its task, and what it cost. With `adaptive-routing: observe`, the workflow keeps running the router's ordinary pick exactly as before, and gh-aw records every eligible run so that maintainers can see the evidence before anything changes.

For example, if the router's pick for an issue triage workflow moves from model B to model A, observe mode keeps running A as today and records each run. A week later, `gh aw routing status` might show B with 40 runs, all successful, at 28 credits a run, and A with 9 runs, one of them a failure, at 12 credits a run. That is the evidence a later live mode would act on. Observe mode makes no recommendation of its own.

## Turn it on

Add `adaptive-routing: observe` to a routed workflow that uses [repo-memory](/gh-aw/reference/repo-memory/):

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
tools:
  repo-memory: true
adaptive-routing: observe
safe-outputs:
  add-comment:
---
Read the new issue and post one suggested next step.
```

To turn it on for every eligible workflow in a repository, set the default in `.github/workflows/aw.json` and recompile:

```json title=".github/workflows/aw.json"
{
  "adaptive_routing": "observe"
}
```

A workflow's own value wins, including `adaptive-routing: false`, then the first import that sets it, then the repository default. `true` is reserved for live adaptive routing, which is not available yet, and fails to compile.

## Which workflows take part

A workflow takes part when its main agent's model is chosen by AWF model routing on a Copilot, Claude, or Codex engine: `model: router`, a fixed level such as `router/balanced`, the [default routing](/gh-aw/reference/model-routing/#default-routing-for-workflows-without-a-model) for workflows without a model, or a model expression that resolves to a router value. It must also enable `tools.repo-memory`, and it must not be a work-queue worker, a reusable workflow (`on: workflow_call`), or run [experiments](/gh-aw/reference/experiments/). A workflow that sets `adaptive-routing: observe` itself, or through an import, fails to compile with the reason when it does not qualify; the repository default skips such workflows silently.

At run time, gh-aw records only the first attempt of a run whose workflow definition comes from the default branch, so re-runs and pull request runs are not recorded. Every first attempt is recorded once the agent job has run, whether the run succeeded or failed.

## What is recorded

After the run's `conclusion` job has published its unified session, the `push_routing_history` job reads three things from it: the routing verdict (whether the run's result counts as evidence about the model and effort AWF selected, and if not, why), the model and effort AWF selected, and the run's cost as AWF priced it. It appends one line per run to `runs.jsonl`:

```json
{"run": "44156334376", "number": 812, "attempt": 1, "at": "2026-11-19T17:02:40Z", "generation": "<definition hash>", "scope": "copilot/cost/shared", "mode": "observe", "role": "observe", "pair": "github-copilot/model-a@high", "outcome": "failure", "credits": 12.0, "counted": true, "note": null}
```

A run counts as a success or a failure only when the verdict says its result was about the selected model. Otherwise its line explains why it did not count, for example `infrastructure`, `cancelled` or `rate_limited`, or `no_agent` when AWF did not route the run or its agent made no request. If a run's own save fails, the next save finds the run in the workflow's run list and records it as `unsaved`.

`state.json` keeps, for each model and effort, its recent outcomes with their credits and token counts, its average cost, current streak and a summary of its success rate. It also tracks verified reconciliation coverage (`reconciled_through` and `unresolved_runs`) independently of `runs.jsonl`'s 500-line retention window and `last_run`, so an older run that ages out of `runs.jsonl` is never charged twice and an incomplete or failed run-list lookup never advances coverage past a gap. Providers report a request's input count either with or without its cached tokens. When AWF's record does not say which, Anthropic counts are treated as excluding cached tokens, OpenAI counts as including them, and for Copilot (or other providers) an input count smaller than the cached count is taken to exclude it and any other to include it, so a Copilot run's token counts are close but not always exact, while its credits are what AWF priced. Editing the workflow starts a new generation: earlier failures are forgiven and successes are kept, a run of the older definition that finishes late does not count against the new one, and reconciliation continues from the previous verified floor so a failed save on the first run of a new definition is not dropped. Records are shared across routing levels unless the workflow chooses its level at run time, in which case each level keeps its own.

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

## Limits

- Observe mode only records evidence; it never changes which model runs and makes no recommendation.
- A history belongs to one workflow file in one repository; renaming the file starts a new history. Reusable workflows (`on: workflow_call`) cannot take part, because each caller numbers its own runs.
- A run that ends before the agent job starts, or whose usage artifact was never published, is not recorded by its own save.
- A failed or cancelled run whose agent ran and whose `push_routing_history` job did not succeed (whether it failed, was cancelled, timed out, or was skipped) is charged as `unsaved` by a later save as verified reconciliation coverage advances across the workflow's default-branch and `pull_request_target` run listings (and via direct lookup for tracked in-progress runs that age past the listing window).
