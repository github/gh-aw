---
title: Harbor
description: Reference for exporting recorded gh-aw sessions and grader evidence to Harbor.
sidebar:
  order: 299
---

`gh aw` exports recorded workflow evidence for inspection with Harbor. The
export is not a runnable Harbor task, an agent replay, or an evaluation of
whether the workflow succeeded.

## CLI commands

| Command | Behavior |
| --- | --- |
| `gh aw sessions download RUN --atif` | Write one ATIF JSON document to standard output. Use `-o FILE` to write it to a file. |
| `gh aw logs WORKFLOW` | Retain downloaded unified sessions and automatically write a sibling `.atif` file when a session is available. |
| `gh aw audit RUN` | Retain downloaded unified sessions and automatically write a sibling `.atif` file when a session is available. |
| `gh aw logs --harbor --output DIR` | Export the selected runs as a portable Harbor job in `DIR/harbor/`. |
| `gh aw graders run --logs-dir DIR` | Regrade downloaded GH-AW logs offline and emit a JSON collection. |

`sessions download --atif` selects ATIF instead of JSONL; it does not create a
second output file. Do not combine it with `--format markdown`. In `logs` and
`audit` output, `aw_session.atif` is a sibling of `aw_session.jsonl`. These
commands do not fetch or reconstruct sessions specifically for ATIF: missing
sessions are skipped, while invalid available sessions return an error.

The `--harbor` option uses the normal `logs` run selection, including workflow
targets, `--stdin`, and applicable filters. It requests session, experiment,
agent, and recorded grader artifacts without requiring every run to have
grader results. The default export does not execute graders. Use
`--additional-grader` to explicitly request additional grading.

See the [`logs` CLI reference](/gh-aw/setup/cli/#logs) and
[`graders` CLI reference](/gh-aw/setup/cli/#graders) for the full command
options.

## Harbor export layout

The Harbor job is written beneath the selected output directory:

```text
DIR/
└── harbor/
    ├── config.json
    ├── result.json
    ├── gh-aw-export.json
    └── run-.../
        ├── config.json
        ├── result.json
        ├── agent/
        │   └── trajectory.json
        ├── verifier/
        │   └── reward.json
        └── gh-aw/
            ├── metadata.json
            ├── regrading.json
            └── evidence/
                └── ...
```

Files that depend on available evidence are omitted when that evidence is
missing. `agent/trajectory.json` is written only when a unified session exists.
`verifier/reward.json` and `verifier_result` are written only when eligible
numeric grader values exist. `gh-aw/regrading.json` is present when additional
graders were selected. The evidence directory contains selected recorded inputs,
including available unified JSONL sessions, grader results and manifests, run
metadata, and experiment state; it does not contain grader source scripts or a
task environment. Original download caches and logs reports remain outside
`harbor/`.

The root `config.json` and `result.json` use Harbor's native job format.
`gh-aw-export.json` identifies the export as `gh-aw-harbor` format version 1
and records the compatibility target, Harbor 0.25.0. Trial directories contain
native Harbor configuration and result files plus `gh-aw/metadata.json`, which
records source run metadata, evidence paths, session status, experiment
assignments, grader data, and reward-mapping provenance.

The trial's Harbor task path points to `gh-aw/evidence`, not to a runnable task.
The export leaves the task checksum empty and does not synthesize an agent
result, verifier result, or task-success reward from a GitHub conclusion.
Missing sessions remain missing. An invalid available session or an export
failure fails the export.

The exporter retains a single ATIF trajectory at
`agent/trajectory.json` when the source session is available. ATIF output uses
`ATIF-v1.8`. Evidence that cannot safely be represented as dialogue, including
source records and ambiguous tool observations, remains in schema-approved
extensions rather than being fabricated as steps. Embedded subagent
trajectories and copied media assets are not emitted. A valid ATIF trajectory
does not guarantee that a particular Harbor agent can load or replay it.

### Grader rewards

Recorded grader results are retained in the evidence directory. A result becomes
a Harbor reward only when it has a unique non-empty grader ID, `pass` or `fail`
status, and a finite JSON numeric `value`. The original numeric value is
preserved under `gh-aw.<grader-id>`. Boolean values, `passed` flags, errors,
unavailable results, missing values, and GitHub conclusions do not create
rewards. The exporter does not create an aggregate reward or infer task success.

The reward mapping in `gh-aw/metadata.json` points to the source result and
retains available grader direction, threshold, and unit. Raw grader artifacts
remain in `gh-aw/evidence/` for interpretation.

## ATIF session output

`sessions download --atif` and automatic `logs`/`audit` exports use the shared
converter to produce one formatted ATIF JSON document, not JSON Lines. The
schema version is `ATIF-v1.8`. Unified-session source records and provenance
that do not map directly to ATIF dialogue are retained in approved `extra`
fields. Unknown events, ambiguous tool correlations, and subagent evidence are
not converted into invented dialogue or invalid tool references. Structured
non-text content is retained as JSON text; the exporter does not copy media
assets or claim native multimodal replay.

ATIF retains prompts, reasoning, tool inputs and outputs, and source
extensions. Treat the output as private, untrusted evidence. It is not a
privacy-preserving Markdown summary.

## Offline regrading

`gh aw graders run --logs-dir DIR` reads a downloaded GH-AW logs directory or
logs summary collection. It reads unified sessions and recorded grader evidence;
it does not make network requests or modify the downloaded evidence. Harbor
bundles, including an exported `DIR/harbor/` directory, are not valid input.
When no unified session is available, the grader result is reported as
unavailable. Grader execution errors are reported per run; invalid selectors,
conflicting run identities, and unsafe source paths fail the collection.

Select an enabled grader positionally or with `--additional-grader`:

```bash
gh aw graders run --logs-dir ./my-logs \
  --additional-grader weekly-research:loops \
  --additional-grader weekly-research:goal-directness
```

The option is repeatable and accepts comma-separated
`WORKFLOW:GRADER` selectors. Selectors resolve against local workflow grader
definitions. Collection regrading supports built-in and isolated JavaScript
graders only; Bash graders are rejected. JavaScript grader code executes, so
select only trusted definitions. Single-session Bash replay is separate and is
not supported in collection mode.

The command emits a JSON document with format `gh-aw-regrading`, a version,
CLI version, and per-run original evidence, selected grader definitions,
results, and provenance. It keeps original grader artifacts separate from new
results, including implementation identities and session digests.

`gh aw logs --harbor --additional-grader WORKFLOW:GRADER` runs the same
collection grading over selected unified sessions before export. The option
can be repeated or comma-separated. Additional numeric `pass`/`fail` values
are exported under `gh-aw.regraded.<grader-id>` and recorded separately in
`gh-aw/regrading.json`; original rewards remain unchanged. A collision with an
existing reward key fails instead of overwriting it. Missing or non-numeric
results do not become rewards.

Embedded regrading JSON is recursively sanitized for recognized credential
patterns and local paths, including arbitrary string fields and array elements.
This pattern-based redaction is not general PII filtering or comprehensive secret
detection.

## Compatibility

Harbor export conformance is pinned to Harbor 0.25.0. The conformance test
validates the generated job, trial, and trajectory with Harbor's native models,
disk readers, and viewer endpoints. Go export and ATIF conversion do not require
Python or Harbor; the conformance test and Harbor's trajectory validator do.

For details about the shared session format, see the
[Unified Agent Session Specification](/gh-aw/specs/unified-agent-session-specification/).
For Harbor's trajectory schema, see the
[Harbor ATIF guide](https://docs.harborframework.com/agents/atif) and
[ATIF specification](https://github.com/harbor-framework/harbor/blob/main/rfcs/0001-trajectory-format.md).
