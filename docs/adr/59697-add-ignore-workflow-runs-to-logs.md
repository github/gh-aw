# ADR-59697: Add ignore-workflow-runs to logs

**Date**: 2026-09-09
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

The `gh aw logs` command collects workflow runs and currently lets users constrain results with workflow, date, ref, and run ID filters, but it cannot explicitly skip known unwanted runs while still returning the requested count of useful results. This PR adds a new exclusion input that accepts workflow run IDs in plain numeric form or qualified `slug/ID` form, applies the exclusion before artifact processing, and preserves it in continuation state for resumed downloads. The implementation evidence shows a need to omit specific runs without shrinking the effective result set or forcing users to narrow broader filters. The repository needs an explicit decision on whether run exclusion should be treated as a first-class logs query parameter across CLI parsing, pagination, orchestration, and reporting.

### Decision

We will add an `--ignore-workflow-runs` option to `gh aw logs` that accepts positive workflow run database IDs, including qualified `slug/ID` inputs, normalizes them to unique numeric IDs, and excludes matching runs from collection. We will apply the ignore list before downstream artifact processing and persist it through orchestration and continuation payloads so resumed or paginated log downloads keep honoring the same exclusion set. We chose this approach because it gives users precise control over unwanted runs without changing the requested result count or overloading existing date, ref, or before/after run filters.

### Alternatives Considered

#### Alternative 1: Require users to refine existing filters such as `--ref`, `--before-run-id`, or `--after-run-id`

The project could continue relying on broader query filters and ask users to manually narrow the candidate run set. This was considered because it avoids adding a new option and reuses existing filtering semantics. It was not chosen because the PR evidence explicitly addresses the need to skip specific known runs while still preserving the overall count and broader search scope.

#### Alternative 2: Support exclusion only for raw numeric run IDs

Another option would be to accept only integer run IDs and reject qualified `slug/ID` inputs. This was considered because it would simplify parsing and validation. It was not chosen because the PR explicitly supports both numeric IDs and qualified values, which improves usability when users copy workflow run references from repository-qualified contexts.

#### Alternative 3: Exclude runs only in the final output after artifacts are processed

The implementation could defer exclusion until after run data is downloaded and assembled. This was considered because it would minimize changes to earlier pagination and orchestration code paths. It was not chosen because the PR description and code both indicate exclusions should happen before artifact processing so ignored runs do not consume result slots or processing work.

### Consequences

#### Positive
- Users can omit known irrelevant or problematic workflow runs without reducing the requested number of collected results.
- The same exclusion behavior is preserved across pagination and continuation data, making resumed downloads more predictable.
- Supporting both numeric and `slug/ID` inputs reduces friction when specifying runs from copied GitHub references.

#### Negative
- The logs query path becomes more complex because ignore-list parsing, deduplication, filtering, and continuation serialization must all stay in sync.
- Invalid or non-positive run identifiers now introduce an additional user-facing validation failure mode.
- Exclusion behavior must be covered in multiple test layers to avoid regressions across CLI and orchestration flows.

#### Neutral
- `LogsDownloadOptions`, pagination options, and continuation payloads gain an `IgnoreWorkflowRuns` field.
- Run filtering semantics now include explicit exclusions in addition to inclusive workflow, date, ref, and run ID bounds.
- The implementation uses run database IDs as the stable identifier for exclusion across query and resume boundaries.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
