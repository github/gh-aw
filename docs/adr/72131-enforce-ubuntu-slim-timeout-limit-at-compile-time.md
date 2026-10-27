# ADR-72131: Enforce the ubuntu-slim Timeout Limit at Compile Time and Upgrade Default Runners

**Date**: 2026-10-25
**Status**: Draft
**Deciders**: pelikhan, Copilot (PR author) [TODO: verify]

---

### Context

The compiler uses `ubuntu-slim` as the default runner for many framework-generated jobs, such as safe-outputs and other built-in jobs. GitHub's `ubuntu-slim` runner supports job timeouts of at most 15 minutes. Users can raise a job's effective `timeout-minutes`, for example with `safe-outputs.timeout-minutes: 30`. Before this change, the compiler could emit a job that pairs `ubuntu-slim` with a timeout it cannot honor. The problem appeared only at runtime. Timeouts are also set in several places: job builders supply defaults, and job augmentation applies user overrides. So the effective timeout is known only after all jobs are built.

### Decision

We will add a compiler pass, `resolveJobRunnerTimeouts` in `pkg/workflow/runner_timeouts.go`, that runs after job augmentation and checks each job's effective literal `timeout-minutes` against the 15-minute `ubuntu-slim` limit. If a job uses the compiler's default framework runner, the compiler switches it to `ubuntu-latest`. This applies to built-in jobs other than the agent and detection jobs, and only when neither `runs-on-slim` nor `safe-outputs.runs-on` is set. If a user explicitly selects `ubuntu-slim` and sets a longer timeout, compilation fails with an actionable error. The check skips GitHub Actions expressions in `runs-on` or `timeout-minutes`, and it skips reusable-workflow (`uses:`) jobs.

### Alternatives Considered

#### Alternative 1: Always reject over-limit timeouts on ubuntu-slim, including compiler defaults

The compiler could raise an error whenever any `ubuntu-slim` job has a timeout above 15 minutes. That would force users to also set `runs-on` whenever they raise a timeout. We rejected this because the user never chose `ubuntu-slim` for default runners. Upgrading the runner automatically keeps the configuration simple, as in `safe-outputs.timeout-minutes: 30`, and still respects the user's intent.

#### Alternative 2: Silently clamp timeout-minutes to 15 on ubuntu-slim

The compiler could cap the timeout at the runner's limit instead of changing the runner or failing. We rejected this because it quietly overrides an explicit user setting, and long-running jobs would then time out at runtime with no warning at compile time.

#### Alternative 3: Use ubuntu-latest as the default runner for all framework jobs

Making `ubuntu-latest` the default everywhere would remove the limit entirely. We did not choose this because `ubuntu-slim` is cheaper and faster to start for the common case of short framework jobs. The upgrade happens only when the timeout requires it.

### Consequences

#### Positive
- Invalid runner and timeout combinations are caught at compile time with an actionable message instead of failing at runtime.
- Users can raise framework job timeouts without also changing runners, because the default runner is upgraded automatically.
- Explicit runner choices are never rewritten, so user overrides stay authoritative.

#### Negative
- The 15-minute limit is hardcoded (`ubuntuSlimMaxTimeoutMinutes`). If GitHub changes the `ubuntu-slim` limit, the compiler must be updated.
- Expression-valued `runs-on` and `timeout-minutes` are skipped, so some invalid combinations still surface only at runtime.
- Jobs upgraded to `ubuntu-latest` cost more to run and start more slowly. This happens silently, with only a debug log.

#### Neutral
- The pass must run after job augmentation. Future changes to the compiler's job pipeline must keep this ordering.
- Generated lock files and golden fixtures change wherever the effective runner changes.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
