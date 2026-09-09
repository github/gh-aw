---
title: Safe-Outputs Samples
description: Replay a fixed set of safe-outputs payloads in place of the agentic step for deterministic end-to-end tests.
sidebar:
  order: 8
---

:::caution[Experimental — hidden]
The `samples:` field and the `gh aw compile --use-samples` flag are an internal, undocumented testing feature. The shape of `samples:` entries, the set of recognized sidecar fields, and the replay driver behavior may change at any time without notice. The `--use-samples` flag is hidden from `gh aw compile --help` on purpose.
:::

`samples:` is an optional per-handler list under any safe-output block that declares a fixed set of payloads the workflow should "produce" instead of running the agent. When `gh aw compile` is invoked with the hidden `--use-samples` flag, the compiled workflow's agentic `Execute coding agent` step is replaced with a deterministic driver that feeds each sample to the safe-outputs MCP server via real `tools/call` JSON-RPC. This lets end-to-end tests exercise safe-output handlers, the safe-outputs MCP server, and the post-processing jobs without any non-determinism from a real LLM.

## When to use it

Use samples replay only to make CI for `gh-aw` itself deterministic — for example, smoke-testing a new safe-output handler, validating MCP routing changes, or exercising the post-processing jobs that consume `outputs.jsonl`. Workflows shipped to users should never rely on `samples:`; remove the field before publishing.

## Declaring samples

Add a `samples:` list to any enabled safe-output handler. Each entry is a JSON object whose keys must conform to the corresponding MCP tool's `inputSchema` (the same schema the agent's `tools/call` payload would have to satisfy).

```aw wrap
---
on:
  workflow_dispatch:
permissions: read-all
engine:
  id: claude
safe-outputs:
  create-issue:
    samples:
      - title: "Sample issue from gh-aw samples"
        body: "Body emitted by the deterministic replay driver."
        labels: [automated, sample]
---

Trivial workflow whose only job is to be compiled with --use-samples.
```

Compile with the hidden flag:

```bash
gh aw compile --use-samples .github/workflows/my-workflow.md
```

Alternatively, a single workflow can opt itself in with the `features.samples: true` flag, which has exactly the same effect as `--use-samples` but applies only to that workflow. Use it for workflows that are designed around `samples:` (such as this repository's own smoke tests) so a plain `gh aw compile` — and therefore `make recompile` — keeps producing the samples-mode lock file:

```yaml
features:
  samples: true
```

The generated `.lock.yml` replaces the agentic step with:

```yaml
- name: Replay safe-outputs samples (deterministic)
  id: agentic_execution
  env:
    GH_AW_SAMPLES: |
      [{"tool":"create_issue","arguments":{...}}]
  run: |
    node "${{ runner.temp }}/gh-aw/actions/apply_samples.cjs"
```

## Sidecar fields

Some samples carry data that is not part of the MCP tool's `tools/call` arguments but is instead pre-staged on disk by the replay driver. These "sidecar" fields are stripped from the sample before schema validation and consumed by the driver:

| Handler | Sidecar field | Used for |
|---|---|---|
| `create-pull-request` | `patch` | Applied to the working tree with `git apply` before the `tools/call` so the resulting PR has a real diff. |
| `push-to-pull-request-branch` | `patch` | Same as above, for an existing PR branch. |

Example:

```aw wrap
---
on:
  workflow_dispatch:
permissions: read-all
engine:
  id: claude
safe-outputs:
  create-pull-request:
    samples:
      - title: "Sample PR from gh-aw"
        body: "Created by the samples replay driver."
        branch: "feat/sample-pr"
        patch: |
          diff --git a/sample.txt b/sample.txt
          new file mode 100644
          --- /dev/null
          +++ b/sample.txt
          @@ -0,0 +1 @@
          +hello from gh-aw samples
---

Trivial workflow exercising create-pull-request via --use-samples.
```

## Compile-time validation

Even without `--use-samples`, every `samples:` entry on every enabled handler is validated at compile time against the corresponding MCP tool's `inputSchema`. Sidecar keys are stripped first. Any `${{ ... }}` runtime expression inside a sample string is substituted with a schema-aware placeholder for validation only (e.g. an enum value for `severity`, `true` for a boolean, `aw_sample` for a generic `aw_*` temporary-id pattern); the original expression is preserved verbatim in the emitted YAML.

When samples replay is enabled, the compiler also warns about each enabled non-builtin handler that supports `samples:` but declares no entries. Those handlers are never called by the replay driver, so the run succeeds without performing the configured operation — add a `samples:` list to each one you expect the suite to exercise.

## Interaction with other features

- **Threat detection is force-disabled** under `--use-samples` (and under `features.samples: true`). The replay driver bypasses the agent entirely, so threat scanning over the (nonexistent) prompt and tool log would always produce a clean signal and add noise to the deterministic baseline. Setting `safe-outputs.threat-detection: true` explicitly is overridden with a warning.
- **`staged:`** is honored normally — `staged: true` handlers still emit step-summary stubs instead of GitHub API calls.
- **All other safe-output features** (`max:`, `github-token:`, `github-app:`, `normalize-closing-keywords:`) behave identically to a real agentic run.

## Frontmatter reference

| Field | Type | Description |
|---|---|---|
| `samples` | `array` | Per-handler list of MCP `tools/call` argument objects. Each entry is validated against the tool's `inputSchema` at compile time. |
| `features.samples` | `boolean` | Workflow-level opt-in to samples replay, equivalent to compiling that workflow with `--use-samples`. |

Recognized sidecar keys (stripped before schema validation; consumed by the replay driver only):

| Handler | Sidecar | Driver action |
|---|---|---|
| `create-pull-request` | `patch` | `git apply` before `tools/call` |
| `push-to-pull-request-branch` | `patch` | `git apply` before `tools/call` |
