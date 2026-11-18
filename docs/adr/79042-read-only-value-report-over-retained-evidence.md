# ADR-79042: Read-Only `gh aw value` Report Over Retained Evidence and Frozen Policy

**Date**: 2026-11-18
**Status**: Draft
**Deciders**: Unknown [TODO: verify] (PR author: mnkiefer)

---

### Context

gh-aw already records native delivery/adoption lifecycle data (`gh aw logs --adoption`) and primary `operational-value` grader results, but it has no single way to answer "what value did this workflow deliver?". Prior ADRs (ADR-55155, ADR-60682) moved grading to one-shot, per-run evaluators and removed historical replay and baseline machinery from the grading path. This PR adds a reporting layer on top: it combines retained delivery/adoption records, recorded grades, first-parent Git snapshots, and caller-supplied typed measurements into one report. The central concerns are avoiding hindsight bias (choosing metrics or windows after seeing outcomes) and avoiding over-claiming when evidence is missing.

### Decision

We will add an experimental, read-only `gh aw value WORKFLOW` command that reports only on evidence already retained locally or in Git. It does not refresh lifecycle evidence, execute graders, or dispatch workflows. Metric definitions select named Go evaluators (`json-number-v1`, `evidence-number-v1`, `schema-example-coverage-v1`) rather than executable code. Goal and window policy (versioned recipe) are frozen before cohort outcomes are examined. Missing evidence is reported as unavailable rather than zero, and only verified direct PR windows contribute to `attributed_value`. The primary driver is credibility: a report that cannot run code or fetch fresh data is reproducible and cannot be tuned after the fact.

### Alternatives Considered

#### Alternative 1: Extend grading to compute value over time inside the grader/conclusion path

Historical replay, baselines and maturation could be run as part of grading. This was considered because it would produce value figures automatically per run. It was not chosen because ADR-60682 deliberately removed this machinery in favour of one-shot evaluators, and replaying history in the graders job would reintroduce hindsight tuning and execution of user code.

#### Alternative 2: Let value definitions carry executable evaluators (scripts)

Each workflow could ship its own script to compute metrics for the report. This was considered for flexibility. It was not chosen because a read-only report that executes arbitrary code is harder to sandbox and verify; named Go evaluators with declarative config keep the surface small, at the cost of expressiveness.

#### Alternative 3: Report from live GitHub state (refresh dispositions and history on every call)

The command could fetch current object states and run workflows to gather observations. This was considered for freshness. It was not chosen because results would change between invocations and the command would need write/dispatch capability; retained exports keep reports reproducible.

### Consequences

#### Positive
- Reports are reproducible and side-effect free: the same retained logs, Git ref and recipe yield the same output.
- Freezing recipes and treating missing data as unavailable limits hindsight bias and over-claiming.
- Typed evidence input (`--evidence`) lets owners report outcomes that are not committed numbers without new collection jobs.

#### Negative
- Reports depend on a retained subset of runs and can be incomplete; users must run `gh aw logs` first.
- Adding new metric types requires a Go code change, since definitions cannot carry code.
- Git replay needs a complete local checkout and authenticated `gh` access for PR metadata, and the window policy has many knobs that add complexity.

#### Neutral
- The command is marked experimental, so its JSON shape and recipe format may change.
- The designer skill and the graders/operational-value specifications were updated to describe the new reporting boundary.
- Repository-wide trends (`weekly`) are intentionally separate from the attributed subtotal.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
