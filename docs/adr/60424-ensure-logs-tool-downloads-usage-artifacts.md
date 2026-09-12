# ADR-60424: Ensure Logs Tool Downloads Usage Artifacts

**Date**: 2026-09-12
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

The PR fixes a bug where the `logs` MCP tool reported `TokenUsage == 0` for every run because its default artifact selection downloaded only the `info` artifact set and never fetched the compact `usage` artifact containing `token_usage.jsonl` and `agent_usage.json`. The PR description and diff show that downstream reports average token usage from per-run records, so omitting usage artifacts silently produced incorrect fleet analytics and even removed the `token_usage` key entirely because of `omitempty`. The implementation changes the logs tool schema defaults, normalizes explicit artifact selections, and adjusts JSON serialization so consumers can distinguish `0` from an absent field. The architectural question is how the logs tool should guarantee availability of per-run token metrics without forcing callers to understand internal artifact dependencies.

### Decision

We will make the `logs` MCP tool always include the compact `usage` artifact set in its effective artifact selection and always serialize `token_usage` and `aic` on each run record. We decided to default the tool to `info,usage` and to append `usage` to explicit artifact selections unless the caller already requested `usage` or `all`, because token metrics are part of the tool's contract and should not disappear due to an incomplete artifact list. This keeps report consumers aligned with the tool schema and restores correct token-usage analytics with minimal download overhead.

### Alternatives Considered

#### Alternative 1: Keep `usage` optional and require callers to request it explicitly

This matched the previous behavior where the tool could be invoked with only `info` or another narrow artifact subset. It was considered because it gives callers maximal control over artifact downloads. It was not chosen because the PR evidence shows callers and downstream reports treated `token_usage` as a normal part of run data, so leaving `usage` optional caused silent data corruption rather than an explicit opt-in trade-off.

#### Alternative 2: Infer token usage from other downloaded artifacts or omit the field when unavailable

Another option would be to keep existing artifact behavior and either derive token usage from heavier artifacts or continue omitting `token_usage` and `aic` when the data is missing. This was considered because it avoids modifying artifact defaults. It was not chosen because the PR description states the compact `usage` artifact is the authoritative, cheap source of those metrics, and omitting the fields made consumers interpret missing data as zero or fail to discover the metric at all.

#### Alternative 3: Change only JSON serialization so `token_usage: 0` is always present

The diff also removes `omitempty` from `token_usage` and `aic`, so one possible narrower decision would be to serialize zero values without changing artifact selection. This was considered because it improves schema discoverability. It was not chosen on its own because the root problem is absent usage data; always serializing `0` without downloading `usage` would preserve an incorrect metric rather than restore real token counts.

### Consequences

#### Positive
- Per-run `token_usage` and `aic` are reliably present, so fleet analytics and logs reports can aggregate real usage metrics again.
- The logs tool contract becomes safer for callers because explicit artifact selections still retain the compact data needed for token accounting.
- Regression tests now guard both default and explicit artifact-selection paths, reducing the chance of silently reintroducing zeroed token metrics.

#### Negative
- The logs tool now downloads the `usage` artifact even when a caller requested a narrower set, slightly reducing strict caller control over artifact selection.
- The implementation adds artifact-normalization logic and special handling for `all`/`usage`, increasing behavior complexity around schema defaults.
- Future artifact-set changes must preserve this implicit dependency or update the tool contract and tests accordingly.

#### Neutral
- The change does not alter the external CLI shape beyond artifact defaults and documented behavior; callers still pass artifact-set names in the same way.
- Run records with legitimately unavailable usage data now emit `token_usage: 0`, making missing-versus-zero semantics explicit at the JSON layer.
- The patch frames `usage` as a compact dependency of the logs tool rather than as an optional reporting enhancement.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
