---
description: Repository overrides for workflow authoring, validation, and investigation.
---

# Repository Instructions

Apply to workflow creation, editing, review, diagnosis, debugging, and upgrades.
These rules override conflicting upstream guidance.

## Repository Rules

- After any `.github/workflows/*.md` edit, run one-shot `gh aw compile`; never `--watch`.
- Follow [debug-agentic-workflow.md](debug-agentic-workflow.md) for diagnosis,
  patching, mode selection, and live gates. Logs/audits do not authorize live runs.
- For ruleset-required workflows, keep workflow/job names stable; set
  `inlined-imports: true` when imports exist.
- Explain Copilot authentication accurately: `permissions: { copilot-requests: write }`
  uses `${{ github.token }}` for inference, without a PAT or `COPILOT_GITHUB_TOKEN`.
- Before inventing workflow/shared-component/tool/safe-output patterns, inspect
  `https://github.com/gm3dmo/the-power` through GitHub APIs or `gh` for prior art.

## Skipping Unnecessary Agent Invocations

Use experimental top-level `skip-if-decision` when skipping requires semantic evidence from
task or progress files. Prefer deterministic trigger/activation checks when
ordinary conditions suffice; decisions cannot override retry limits, budgets,
deadlines, or nonrecoverable errors.

Lists accept at most 64 questions. Nonempty lists emit a compile-time experimental warning. Configuration and
behavior may change without deprecation.

```yaml
skip-if-decision:
  - Is there no actionable work remaining in @context/task.md?
  - Has @reports/progress.md established that further execution is unnecessary?
```

Write each question as a skip condition, not a request to execute the agent.
Populate referenced files through checkout, restored memory, or pre-agent setup
before the first invocation, and keep them current for subsequent invocations.
Runtime `@file` references resolve relative to `engine.cwd` or the workspace;
only workspace and `/tmp/gh-aw/agent` files are eligible. Sensitive paths,
symlinks, hardlinks, oversized context, and Actions expressions are unsupported.

The shared harness evaluates questions inside AWF before every otherwise
eligible initial, retry, resume, or continuation invocation, including
declarative engines. Any `deny` skips; otherwise `allow` or all `unknown`
preserves the existing execution path. Missing files, API/model unavailability,
timeouts, and malformed responses abstain without decision retries or workflow
failure. An absent or empty list preserves existing behavior.

Questions are batched through the existing AWF proxy, using
`gpt-6-luna-decisions` on Copilot or `gpt-6-luna` on OpenAI. Do not add direct
provider calls, extra credentials, host-side decision steps, or decision code
to detection, evals, or conclusion. Decision requests retain normal proxy
budget and usage accounting; a skipped agent is not zero total model usage.
Inspect typed `harness.decision` unified-session events or `gh aw audit --json`
under `retries.decisions` and `retries.avoided_invocations` for outcomes,
sanitized errors, latency, and avoided invocations.

## Indexing and Search

Follow [index-search.md](index-search.md) when integrating existing indexes or
search tools, including MCP.

## Session Graders

Ten fast built-ins run by default in conclusion after session collection.
Configure them with `graders.<builtin-id>.threshold`, `enabled`, or data-only
`config`; expensive built-ins require opt-in.

For custom outcomes, use `graders.<distinct-id>.script` with a synchronous
JavaScript body over immutable `session`, `config`, and `helpers` inputs.
Reuse `helpers.createSessionLookups(session)` for stats and event/tool lookups.
Return a finite number, `null` for unavailable evidence, or an object with
`value`; do not emit legacy metric arrays. The script limit is 256 KiB.

For repository-owned Bash compatibility, use `graders.operational-value.run`
with a confined `.sh` path. Read session JSONL from `$1` / `GH_AW_SESSION_FILE`
and JSON config from `GH_AW_GRADER_CONFIG`; retain `[{id,value}, ...]` output.
Remove file writes and live API/checkout reads; compile/lint rejects detected
writes. This trusted shell path is not yet microVM-isolated.

The compiler owns the isolated downstream `graders` job for both custom forms.
Never override built-in implementations, customize `jobs.graders`, or inject
user code or steps into `jobs.conclusion`.
Collect required evidence before session publication; graders cannot fetch it.
Use [operational-value-designer](../skills/operational-value-designer/SKILL.md)
for executable examples and evidence/fixture design.

Audit optionally retrieves registered custom results from the `graders`
artifact; `logs --graders` and `--artifacts graders` include both it and `usage`.
Custom results never replace conclusion's published session.

## Repository-Wide Debugging and Optimization

General "debug/optimize agentic workflows" requests start with failure clustering,
not a request to choose a workflow. Supplied workflow/run/failure scope takes
precedence. Follow [analyzing-runs.md](analyzing-runs.md) for collection, selection,
cached evidence, audits, and uncertainty reporting. Do not dispatch workflows for evidence.
Use single-run `audit --search` for BM25 evidence retrieval from the actual
unified session. Repetition grouping activates automatically when useful; do not
configure Drain3. Grouping never changes ranking and keeps structured outcomes
separate. These optional run-local groups are distinct from
fleet failure clusters and `audit --group` finding codes; relevance/anomaly scores
never replace full-stream findings or establish threat absence.
