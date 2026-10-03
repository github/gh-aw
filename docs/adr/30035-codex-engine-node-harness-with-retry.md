# ADR-30035: Codex Engine Node.js Harness with Retry Logic

**Date**: 2026-05-04
**Status**: Draft
**Deciders**: Unknown

---

## Part 1 — Narrative (Human-Friendly)

### Context

The system runs multiple AI agent engines (Claude, Copilot, Codex) as subprocesses inside GitHub Actions workflows. The Codex Node.js harness provides prompt delivery, diagnostics, and bounded retries around `codex exec`. Transient API and transport failures require recovery without replaying task outputs. Codex supports exact-session recovery through `codex exec resume <SESSION_ID>`; the absence of a `--continue` flag does not prevent continuation.

### Decision

`codex_harness.cjs` reads `--prompt-file` and delivers its contents through stdin with the positional prompt `-`, avoiding shell quoting and operating-system argv limits. Recoverable failures use up to three retries with exponential backoff (5 s → 10 s → 20 s, capped at 60 s).

When JSONL reports a valid thread UUID and persistence is enabled, retries use that exact thread with a continuation instruction, not the original prompt. The harness never uses `--last`. Before retrying, it checks for staged task outputs: if any exist, it preserves them and stops rather than risking duplicate actions. Missing, invalid, ephemeral, or explicitly selected sessions do not qualify for automatic resume.

The shared process runner owns the subprocess group, forwards cancellation, escalates termination, and bounds stdio draining. A forced shutdown completes group escalation even when the CLI exits and closes stdio before its descendants; ordinary successful closure does not signal a reaped group. It decodes stdout and stderr independently so diagnostics cannot corrupt JSONL failure classification. Codex retains bounded classifier tails while forwarding complete log streams and retaining recent structured terminal errors. Its soft deadline also preempts running attempts to preserve structured diagnostics before the Actions hard timeout.

Runtime model prefixes must match the already-configured provider unless `GH_AW_LLM_PROVIDER_EXPLICIT=1` declares an explicit override. An explicit override strips a conflicting OpenAI/Copilot prefix and logs the override without changing credentials or endpoints. Native budget errors and all known terminal proxy guard rejections stop retries. Unknown native session items remain available as snapshots, and successful retry results update the aggregate outcome without erasing historical errors or token usage.

### Alternatives Considered

#### Alternative 1: Inline Bash Retry Loop

A `while` / `until` retry loop could be added directly to the shell command already embedded in each `.lock.yml`. This avoids adding a new file but would be duplicated across many workflow files, is difficult to unit-test, and the retry-detection logic (parsing OpenAI error patterns from output) becomes fragile in a one-liner. This approach also does not address the `$INSTRUCTION` shell-variable injection, which has a size limit and quoting hazards for large prompts.

#### Alternative 2: Upstream Fix in the Codex CLI

Relying solely on Codex's internal stream retries does not cover process failures, safe-output replay prevention, cancellation, or downstream diagnostics. The harness complements internal retry behavior and uses the native exact-session resume subcommand where eligible.

#### Alternative 3: Shell Wrapper Script (Plain `.sh`)

A POSIX shell script could implement the retry loop without requiring Node.js. However, the existing harness infrastructure is Node.js-based (process runner, AWF reflect helpers, structured logging to stderr). Introducing a Bash harness for one engine would create an inconsistency, lose the shared `process_runner.cjs` utilities, and make it harder to write fast, isolated unit tests.

### Consequences

#### Positive
- Transient rate-limit and server errors are automatically retried, improving overall workflow success rates for Codex-based agents.
- Prompt delivery switches from shell-variable injection (`$INSTRUCTION`) to file-based (`--prompt-file`), removing shell quoting hazards and size limitations for large prompts.
- Consistent harness pattern across all three agent engines simplifies future maintenance and onboarding.
- Targeted tests cover stdin delivery, exact-session resume, staged-output preservation, structured failure classification, cancellation, process-group cleanup, model arguments, and session normalization.

#### Negative
- Runs without an eligible persisted thread restart from scratch only when no task output has been staged. Recovery cannot guarantee completion when session storage or infrastructure is unavailable.
- A failed attempt that already staged task output stops with a nonzero exit rather than replaying work. Outputs are retained; stopping is not itself evidence that the entire requested task succeeded.
- Node.js must be present in the execution environment; the harness detects it via `GH_AW_NODE_BIN` or falls back to `command -v node`, adding a soft dependency that could fail silently on unusual runners.

#### Neutral
- Every compiled `.lock.yml` that uses the Codex engine is regenerated to switch from the direct `codex exec "$INSTRUCTION"` invocation to the harness-wrapped form; this is a mechanical change with no behavioral difference beyond the retry wrapper.
- The `--prompt-file` flag is a harness-only argument stripped before passing the remaining args to `codex exec`.

---

## Part 2 — Normative Specification (RFC 2119)

> The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**, **SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **MAY**, and **OPTIONAL** in this section are to be interpreted as described in [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119).

### Harness Script

1. The Codex engine **MUST** invoke `codex exec` through `codex_harness.cjs` rather than calling it directly from a shell command.
2. The harness **MUST** accept a readable `--prompt-file <path>`, deliver its contents through stdin, and pass `-` as the positional prompt to `codex exec`.
3. The `--prompt-file` flag **MUST NOT** be forwarded to the `codex exec` subprocess.
4. The harness **MUST** bound retries to the configured limit (3 by default). Cancellation, terminal proxy guards, deterministic configuration failures, and failures after staged task output **MUST NOT** be retried.
5. The harness **MUST** apply exponential backoff between retries, starting at 5 seconds, doubling on each attempt, and capping at 60 seconds.
6. The harness **MUST NOT** retry a run that produced no output before failing, as this indicates an unrecoverable error (e.g., authentication failure) rather than a transient one.
7. The harness **SHOULD** emit structured diagnostic log lines prefixed with `[codex-harness]` to stderr so they are distinguishable in aggregated logs.
8. Eligible retries **MUST** target the observed thread UUID explicitly, **MUST NOT** use `--last`, and **MUST NOT** resend the original task instructions.
9. The harness **MUST** preserve staged outputs and **MUST NOT** discard them to enable replay.
10. Process termination **MUST** target the owned subprocess group and bound the grace period and stdio drain. Cancellation **MUST NOT** be suppressed as a successful completion.
11. stdout and stderr **MUST** be decoded independently for structured classification while the full streams remain available in logs.
12. Provider-prefixed runtime models **MUST** agree with the configured provider unless an explicit provider override supersedes an OpenAI/Copilot prefix. Such overrides **MUST** be logged without secrets; normalization **MUST NOT** switch authentication.

### Engine Interface

1. `CodexEngine` **MUST** implement the `HarnessProvider` interface by returning `"codex_harness.cjs"` from `GetHarnessScriptName()`.
2. `CodexEngine.GetExecutionSteps()` **MUST** construct the execution command using `nodeRuntimeResolutionCommand` and the harness script name, consistent with the pattern used by other engines that implement `HarnessProvider`.
3. Compiled workflow lock files **MUST** reflect the harness invocation pattern and **MUST NOT** use the legacy `INSTRUCTION="$(cat ...)"` shell-variable injection for Codex.

### Conformance

An implementation is considered conformant with this ADR if it satisfies all **MUST** and **MUST NOT** requirements above. Failure to meet any **MUST** or **MUST NOT** requirement constitutes non-conformance.

---

*This is a DRAFT ADR generated by the [Design Decision Gate](https://github.com/github/gh-aw/actions/runs/25295059484) workflow. The PR author must review, complete, and finalize this document before the PR can merge.*
