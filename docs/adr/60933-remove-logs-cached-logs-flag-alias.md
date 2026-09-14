# ADR-60933: Remove `gh aw logs` `--cached-logs` Flag Alias

**Date**: 2026-09-14
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

[ADR-60702](60702-support-wildcard-cached-logs-files.md) introduced wildcard cached logs shards for `gh aw logs` and, as part of that change, added a `--cached-logs` alias alongside the existing `--cached-jsonl` flag. The alias created two spellings for the same input: both flags fed the same cached JSONL resolution path, help text had to describe both, and documentation examples drifted between them. Wildcard cache support does not depend on the alias, so the CLI surface carried a duplicate flag name without any capability of its own.

### Decision

We will remove the `--cached-logs` flag from the `logs` command and read cached log input only from `--cached-jsonl`. Wildcard cache prefixes such as `logs-*` remain fully supported, but exclusively through `--cached-jsonl`. Help text, examples, and CLI reference documentation are updated to show wildcard usage on `--cached-jsonl`, and flag-registration tests assert that `--cached-logs` is no longer registered.

This ADR amends only the neutral consequence in ADR-60702 stating that the CLI surface grows by one alias; ADR-60702's wildcard shard loading, unique shard naming, deterministic ordering, and date-range pruning decisions remain in force and are recorded there unchanged.

### Alternatives Considered

#### Alternative 1: Keep the alias and deprecate it gradually

The alias could remain registered but hidden or flagged as deprecated, with a warning pointing users to `--cached-jsonl`. This was considered because it avoids breaking any caller that already adopted the alias. It was not chosen because the alias shipped only very recently alongside the wildcard feature, so adoption is minimal, and a deprecation path would keep duplicate flag handling, help text, and tests alive for multiple releases.

#### Alternative 2: Keep `--cached-logs` and remove `--cached-jsonl` instead

The command could standardize on the newer alias name and drop the older flag. This was considered because `--cached-logs` reads more naturally for the logs command. It was not chosen because `--cached-jsonl` is the long-standing documented flag, is referenced by existing automation, and names the concrete cache format the flag accepts.

### Consequences

#### Positive
- The `logs` command exposes exactly one flag for cached JSONL input, so help text and documentation no longer describe the same input twice.
- Flag wiring and tests are simpler: a single option field feeds cached log resolution.
- Wildcard cache behavior is documented in one place, reducing the chance of examples drifting between flag names.

#### Negative
- Callers that adopted `--cached-logs` will fail with an unknown flag error and must switch to `--cached-jsonl`.
- The change is a breaking CLI change and requires a major changeset even though the removed surface is small.

#### Neutral
- Wildcard cache resolution, shard naming, merge ordering, and pruning behavior are unchanged.
- ADR-60702 remains the record of the wildcard cache design; this ADR narrows only its flag surface.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
