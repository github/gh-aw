# ADR-73704: Record Not-Evaluated Evals Runs

**Date**: 2026-11-01
**Status**: Proposed
**Deciders**: gh-aw maintainers

---

### Context

BinEval evaluates agentic workflow outcomes by parsing a judge's response into `YES`, `NO`, and `UNKNOWN` rows in `evals.jsonl`. In prior runs, AWF pre-flight failures could still allow `UNKNOWN` rows to reach artifacts, summaries, and durable eval state branches, making a judge that never ran indistinguishable from a judge that answered `UNKNOWN`. The pipeline spans execution evidence, parsing, artifact publication, state-branch persistence, and downstream consumers, so the distinction must survive each boundary. The system must preserve genuine `UNKNOWN` responses while preventing failed or incomplete evaluations from being counted as answers.

### Decision

We will treat successful judge execution and response detection as prerequisites for publishing eval answer rows. The parse step will use the raw execution outcome and validated execution evidence for the current run, together with engine-specific response detection; when the judge was not started, failed, produced no assistant response, or lacks usable execution evidence, it will write a run-level `evals_status.json` marker with a fixed reason code and no `evals.jsonl` rows. Response detection is engine-aware: structured logs yield only assistant message text, and streamed delta fragments of one message (for example Gemini `stream-json` chunks) are concatenated verbatim, including whitespace-only fragments, with `\n` used only between complete messages. Successful runs will continue to publish `YES`, `NO`, and genuine `UNKNOWN` answers as before, while the marker will be propagated through artifacts, usage data, summaries, state-branch handling, and consumers. The shared marker reader used by the step-summary and state-branch consumers accepts a marker only when `status` is `not_evaluated`, `reason_code` is one of the fixed reason codes, and `reason` is a string; any other marker is treated as malformed and ignored, so those consumers fall back to `evals.jsonl` when it is present.

### Alternatives Considered

#### Alternative 1: Continue recording failures as `UNKNOWN`

This would preserve the existing file shape and require fewer changes to parsers and consumers. It was rejected because it conflates infrastructure or execution failures with a valid judge answer, contaminates summaries and durable state, and can cause downstream systems to treat an unevaluated run as a real result.

#### Alternative 2: Infer execution solely from the step conclusion or exit code

This would avoid introducing a marker and could reuse existing workflow metadata. It was rejected because `continue-on-error` can rewrite the step conclusion, and an exit-code file does not reliably establish that the current judge started or returned a response; run-scoped execution evidence and the raw outcome provide a stronger signal.

#### Alternative 3: Suppress all output when evaluation is incomplete

This would prevent false answer rows with a small implementation. It was rejected because silently missing output is not diagnosable and cannot distinguish a judge that did not run from an evaluation whose result was intentionally unavailable; an explicit marker preserves that information for operators and consumers.

### Consequences

#### Positive
- Failed, skipped, cancelled, or never-started judges are no longer represented as `UNKNOWN` answers, preventing inaccurate summaries, artifacts, durable state, and experiment inputs.
- Operators and downstream consumers receive a stable run-level reason and execution metadata, while genuine `UNKNOWN` responses remain available when a judge actually answered.

#### Negative
- A new marker schema and propagation path must be maintained across parsers, generated workflows, artifacts, usage uploads, state-branch logic, CLI lookups, and consumers.
- Consumers must handle marker-only runs and the absence of `evals.jsonl`, and some previously recorded runs may need to be interpreted differently because they were execution failures rather than valid answers.
- New reason codes must be added to the fixed list before a marker using them is recognized.

#### Neutral
- Generated workflow lock files and usage upload lists change to carry `evals_status.json`; marker-only runs do not create or update an eval state branch.
- The parser is now engine-aware and depends on matching run identity fields in execution evidence, so future eval engines must provide compatible outcome and response-detection inputs.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Proposed to Accepted.*
