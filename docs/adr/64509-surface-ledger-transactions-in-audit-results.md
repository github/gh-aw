# ADR-64509: Surface ledger transactions in audit results

**Date**: 2026-09-30
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

This pull request changes how gh-aw reports activity from the conclusion job’s usage summary and the `gh aw audit` pipeline. The PR description states that conclusion usage payloads previously omitted ledger activity, which left audit results without a transaction count even when repo-memory ledger mutations had been recorded. The diff shows changes in both the JavaScript usage-summary generator and the Go audit/reporting pipeline, plus tests and documentation, so the architectural question is whether ledger transaction counts should be promoted to a first-class usage and audit field. The non-negotiable constraint visible in the PR is that recorded `ledger_mutation` manifest entries must be counted, while queued `ledger_append` safe outputs must not be counted because they are persisted later in a separate job.

### Decision

We will add a `ledger.transactions_added` field to the usage activity summary and carry that field through cached summaries, processed run models, JSON audit output, and console audit rendering. We will derive the count only from recorded `ledger_mutation` entries in the downloaded safe-outputs manifest, treating a present manifest with no such entries as zero and a missing manifest as absent data. We chose this because the PR evidence shows audit consumers need a consistent, explicit ledger activity metric and because counting queued `ledger_append` outputs would misstate work that has not yet been persisted.

### Alternatives Considered

#### Alternative 1: Keep ledger activity implicit in the safe-outputs manifest only

This was realistic because the manifest already contains per-type item counts, including ledger-related entries, and existing tooling could inspect that lower-level artifact directly. It was not chosen because the PR description and diff show that audit JSON, console output, and cached summaries currently need a direct ledger transaction count, and forcing every consumer to re-interpret raw safe-output items would leave the missing-field problem unresolved.

#### Alternative 2: Count both `ledger_mutation` and queued `ledger_append` items as transactions added

This was considered because both item types are ledger-related and a broader count might appear to better reflect total intended ledger work. It was not chosen because the documentation and new tests in this PR explicitly distinguish recorded repo-memory mutations from queued appends that are persisted later, so combining them would over-report completed ledger transactions.

### Consequences

#### Positive
- Audit JSON and console output expose ledger transaction counts directly instead of requiring consumers to inspect raw safe-output manifests.
- Cached run summaries and rebuilt audit views preserve the same ledger activity data path as fresh runs.
- The implementation distinguishes missing manifest data from a real zero count, improving observability semantics.

#### Negative
- The usage-summary and audit pipelines now carry another cross-language schema field that must stay synchronized between JavaScript generators, Go models, renderers, and tests.
- Future changes to ledger safe-output semantics will need to preserve the `ledger_mutation` versus `ledger_append` distinction to avoid regressions.

#### Neutral
- The change extends existing usage and audit schemas rather than introducing a new ledger subsystem or persistence mechanism.
- Documentation and tests now encode the rule that ledger activity is additive and optional, so older runs may continue omitting the field.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
