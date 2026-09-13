# ADR-60531: Share Rate-Limit State Across Multi-Target Logs Downloads

**Date**: 2026-09-13
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

The PR changes `gh aw logs` when it downloads logs for multiple workflow targets under a configured `--max-github-api-rate-limit`. The existing behavior let each target enforce the API ceiling independently, which meant one target could detect the limit while other queued or in-flight targets kept working until they hit the same condition themselves or waited for reset. The diff introduces shared orchestration state across multi-target downloads, preserves single-target wait-for-reset behavior, and adds continuation data for targets interrupted by a shared rate-limit stop. The architectural question is how rate-limit enforcement should behave when several targets share one overall download operation.

### Decision

We will enforce `--max-github-api-rate-limit` through shared rate-limit state across multi-target `gh aw logs` downloads, cancel remaining target work as soon as the shared ceiling is reached, and reuse that reached state without additional GitHub API checks. We decided that multi-target downloads should stop promptly rather than wait for reset target-by-target, while single-target downloads should keep the existing wait-for-reset behavior. We will also emit resumable continuation data for interrupted targets so users can continue after the API window resets.

### Alternatives Considered

#### Alternative 1: Keep per-target independent rate-limit checks

This preserves the prior behavior where each target checks the configured limit separately and may wait for reset on its own. It was considered because it is simpler and reuses the existing single-target logic directly. It was not chosen because the PR evidence shows that independent checks leave queued work blocked behind a limit already reached elsewhere in the same command and allow unnecessary extra API checks after the shared ceiling is known.

#### Alternative 2: Wait for reset once, then resume remaining multi-target work automatically

Another option would be to coordinate targets under shared state but pause the whole multi-target operation until the reset time, then continue automatically. It was considered because it preserves completeness for one command invocation. It was not chosen because the diff explicitly adds immediate cancellation for queued and in-flight work in multi-target mode, and the updated CLI guidance distinguishes single-target waiting from multi-target stopping.

#### Alternative 3: Fail the command without continuation data when the shared ceiling is reached

The command could stop all remaining work immediately and return only an error. It was considered because it minimizes new continuation logic. It was not chosen because the diff adds rate-limit-specific continuation messages and preserves cursor options for interrupted targets, indicating resumability is a required part of the new behavior.

### Consequences

#### Positive
- Multi-target downloads stop promptly once the shared API ceiling is reached, avoiding wasted work and redundant rate-limit checks.
- Queued targets retain continuation parameters, making partial multi-target results resumable after the rate limit resets.
- Single-target downloads keep their existing wait-for-reset semantics, limiting behavioral change to the concurrent multi-target case.

#### Negative
- The logs download orchestration now depends on additional shared mutable state and cancellation coordination, increasing implementation complexity.
- Multi-target commands may now stop earlier than before, so users who expected automatic waiting across all targets must re-run with continuation data.
- Rate-limit handling now has distinct single-target and multi-target semantics, which raises the documentation and testing burden.

#### Neutral
- The change threads shared rate-limit state through orchestration, batch processing, and artifact download helpers without changing the public flag name.
- Tests now cover queue clearing, state reuse, and continuation behavior for rate-limit stops.
- CLI and MCP help text are updated to describe the different behaviors for one target versus multiple targets.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
