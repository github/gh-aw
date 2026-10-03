---
name: Engine Conformance Dispatcher
description: Queue and dispatch bounded engine conformance checks, prioritizing recent engine upgrades.
intent: Detect engine configuration regressions without running every conformance suite every day.
on:
  schedule: daily
  workflow_dispatch:
permissions:
  contents: read
  actions: read
  copilot-requests: write
concurrency:
  group: engine-conformance-dispatcher
  cancel-in-progress: false
strict: true
timeout-minutes: 10
max-turns: 20
max-ai-credits: 2
tools:
  cache-memory: true
  edit:
  github:
    mode: gh-proxy
    toolsets: [actions, repos]
  bash:
    - "cat"
    - "mkdir"
safe-outputs:
  dispatch-workflow:
    workflows:
      - engine-conformance-aider
      - engine-conformance-claude
      - engine-conformance-codex
      - engine-conformance-copilot
      - engine-conformance-crush
      - engine-conformance-cursor
      - engine-conformance-deepseek-harness
      - engine-conformance-gemini
      - engine-conformance-goose
      - engine-conformance-kiro
      - engine-conformance-opencode
      - engine-conformance-pi
      - engine-conformance-pydantic-ai
    max: 3
    target-ref: ${{ github.event.repository.default_branch }}
  noop:
    report-as-issue: false
---

# Engine conformance dispatcher

Dispatch at most three conformance workers per run. The workers are the
`engine-conformance-<id>` workflows in the allowlist above; never run the probes
in this dispatcher. Do not dispatch any other workflow.

Maintain a work queue in
`/tmp/gh-aw/cache-memory/engine-conformance/queue.json`. It is a JSON object
with `pending` (an array of engine IDs). A missing or invalid file is a cold
start: initialize the full worker list. Cache writes can race safe-output
dispatches; the cache is only a scheduling hint, never a record that a worker
ran or that a dispatch succeeded. Use the GitHub Actions run history as the
source of truth for previous dispatches, successes, failures, and cooldowns.

1. Read the latest commits on the default branch from the last seven days.
   Examine changed file paths in relevant commits. Mark an engine as upgraded
   when its conformance workflow, its engine implementation or setup, or its
   engine-specific shared configuration changed. Changes to
   `.github/workflows/shared/engine-conformance.md` affect all workers and
   should queue all 13 for testing over successive days. Match only the 13
   allowlisted IDs for engine-specific changes; unrelated changes are not
   evidence of an upgrade.
2. Read the queue file and list recent runs of the allowlisted worker workflows
   on the default branch (including queued and in-progress runs). Exclude
   workers with queued or in-progress runs. A worker that failed since its last
   upgrade is eligible for a retry, but do not dispatch the same worker more
   than once within 24 hours unless it was upgraded again. Compute this
   cooldown from the *observed run timestamps*, not from the cache. If the
   Actions history is unavailable, stop with `noop` rather than risk duplicates.
3. Form the queue with upgraded engines first, then workers with a recent
   failure, then the remaining workers ordered by oldest observed run
   (workers without a run first). Preserve the previous queue order to break
   ties; deduplicate IDs and drop unknown IDs. Recent upgrades not yet tested
   on their current configuration take precedence over the ordinary rotation.
4. Dispatch up to three eligible workers from the front of the queue using
   `dispatch_workflow` with the exact allowlisted workflow name. Do not
   dispatch an engine that already has a successful run after its latest
   upgrade, unless it is due for the ordinary rotation (at least seven days
   since its most recent observed run). On days without upgrades or failures, use at most
   one slot for the oldest worker due for rotation; it is fine to dispatch
   none. The daily dispatcher does not imply daily execution of each worker.
5. Write the queue order to the cache, keeping selected workers in `pending`
   until a subsequent dispatcher observes their Actions runs. A missing run
   leaves its worker at the front for the next dispatch attempt. An observed
   run moves its worker behind those still waiting; only a successful run
   counts as a passing test. If no workers qualify, call `noop` and keep the
   queue for future runs.

Keep all reads bounded to the seven-day upgrade window and recent worker runs.
If a source is incomplete, prefer the rotation over guessing an upgrade.
Do not install tools, change repository files, or publish other resources.
