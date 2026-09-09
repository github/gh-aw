# ADR-59270: Cache audit output for logs

**Date**: 2026-09-07
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

`gh aw logs` already downloads and processes workflow run data into per-run cache directories, and the standalone audit command can build a structured audit report from that local data. This PR adds a new `--audit` mode to the logs command so users can persist `audit.json` beside downloaded runs, reuse it when run status and conclusion are unchanged, and expose the resulting path in logs output. The implementation also refactors audit construction so a local-data-only path can build reports without extra GitHub API calls. The codebase needs an explicit decision on whether audit reports should be treated as first-class cached artifacts of the logs subsystem rather than generated only on demand.

### Decision

We will make audit reports a cacheable output of `gh aw logs` by adding an `--audit` flag that writes `audit.json` into each run cache directory and reuses that file when the cached run state still matches the current workflow run. We will generate those audit files exclusively from already-downloaded run data so logs workflows can produce audit output without additional GitHub API calls. We will also surface the cached audit path in logs JSON output so downstream tooling can discover and reuse the generated report.

### Alternatives Considered

#### Alternative 1: Keep audit generation only in the standalone audit command

The project could continue requiring users to run a separate audit command after downloading logs. This was considered because it keeps `gh aw logs` focused on download and report preparation responsibilities. It was not chosen because the PR explicitly adds `--audit` to discovery, multi-target, and stdin log flows, showing that users benefit from generating reusable audit output during the logs pipeline rather than as a separate step.

#### Alternative 2: Regenerate audit output every time instead of caching it

Another option would be to always rebuild `audit.json` from local run data whenever logs output is rendered. This was considered because it avoids cache validation logic and ensures the report always reflects the latest renderer behavior. It was not chosen because the diff adds run-state checks on run ID, status, and conclusion specifically to reuse existing audit output when still valid, reducing repeated work across repeated logs and audit invocations.

#### Alternative 3: Build logs audit output by making fresh GitHub API calls

The command could generate audit reports by reusing existing rendered-audit paths that fetch or enrich data from GitHub during report construction. This was considered because the standalone audit flow already had code paths that add richer outcome summaries with contextual data. It was not chosen because the PR description and the `buildLocalAuditData` refactor both emphasize producing audit output from already-downloaded data without additional GitHub API calls.

### Consequences

#### Positive
- Users can generate per-run `audit.json` files directly from `gh aw logs`, including discovery, multi-target, and stdin-based workflows.
- Reusing cached audit output when run state is unchanged reduces repeated audit work across subsequent logs and audit operations.
- Downstream tools can consume the new `audit_path` field in logs JSON and summary output to locate structured audit reports without recomputing them.

#### Negative
- The logs subsystem now owns additional cache lifecycle logic, including audit file validation, regeneration, and write failures.
- Cached audit output can become another compatibility surface that must stay in sync with evolving audit schemas and report expectations.
- Adding the `--audit` flag across multiple logs execution paths increases implementation and test surface area.

#### Neutral
- `audit.json` becomes a standard per-run cache artifact alongside other logs-derived files.
- Audit construction is split into local-data and rendered-output paths, which clarifies responsibilities but introduces another internal abstraction boundary.
- Logs output now conditionally includes `audit_path`, so consumers may need to handle its absence when audit generation was not requested.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
