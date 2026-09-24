# ADR-63032: Add grouped audit findings mode

**Date**: 2026-09-23
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

`gh aw audit` already supports two output shapes: a detailed single-run report and a base-versus-comparison diff for multiple runs. The PR description and diff show a new operator need: when several workflow runs contain the same audit finding code, reviewers want a compact summary where each `[run, code]` pair appears once with an occurrence count and one representative finding instead of reading a full diff or scanning repeated findings manually. The change touches CLI flag parsing, audit execution flow, output rendering, JSON schema generation, tests, and end-user documentation. The decision is whether grouped aggregation should become a first-class audit mode rather than requiring downstream consumers to post-process per-run audit output themselves.

### Decision

We will add a first-class grouped mode to `gh aw audit` behind a new `--group` flag that aggregates findings by run ID and stable audit finding code. In this mode, audit will process each requested run independently, load the generated audit data, collapse repeated findings into grouped entries with `occurrences` and `representative_entry`, and render the grouped result in pretty, markdown, and JSON formats with matching schema support. We chose this because the PR evidence shows grouping is an intentional review workflow distinct from multi-run diffing, and building it into the CLI provides a stable, documented output contract instead of forcing each consumer to reconstruct the same aggregation logic.

### Alternatives Considered

#### Alternative 1: Keep only diff mode and require users to interpret repeated findings manually

This was a realistic option because multi-run diff output already exists and avoids introducing another audit mode. It was not chosen because the PR description explicitly calls for each `[run, audit code]` pair to appear once with counts, which diff mode does not provide and would leave repeated findings noisy and harder to summarize across runs.

#### Alternative 2: Emit only raw per-run audit JSON and require external post-processing

This was viable because the current audit pipeline already writes per-run audit artifacts that another script or consumer could aggregate after the fact. It was not chosen because the diff adds grouped report types, rendering, and schema validation specifically to make grouped output a supported CLI contract, reducing duplicated downstream logic and making grouped summaries available in pretty and markdown formats as well as JSON.

### Consequences

#### Positive
- Reviewers can summarize repeated audit findings across multiple runs with one stable grouped output mode instead of reading only detailed reports or diffs.
- The grouped output contract is documented and schema-backed, so automation can rely on `runs_analyzed`, grouped `entries`, occurrence counts, and representative findings.
- Grouped mode remains separate from diff mode, preserving existing audit workflows while adding a clearer aggregation path for multi-run analysis.

#### Negative
- The audit command surface and execution flow become more complex because argument dispatch, rendering, and schema generation now support an additional output mode.
- Grouped mode depends on reading persisted per-run audit data back from disk, which adds another coupling point between audit generation and grouped rendering.
- Tests and future maintenance must keep grouped pretty, markdown, JSON, and schema outputs aligned as audit finding structures evolve.

#### Neutral
- Existing single-run and diff behaviors remain in place; grouping is opt-in via `--group`.
- The grouped report uses one representative finding per `[run, code]` pair, so detailed per-occurrence context still lives in the underlying audit artifacts rather than the grouped summary.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
