---
"gh-aw": patch
---

Add up to 64 experimental top-level `skip-if-decision` questions evaluated inside AWF before eligible
agent invocations, including retries, resumes, and continuations. Questions share
one Decisions request through the existing API proxy and resolve guarded runtime
file references in the agent sandbox. Any deny skips; allow or all unknown
preserves the deterministic execution path. Evaluation failures abstain without
additional retries. All built-in and declarative engines share the gate, including
command-based, custom-harness, and durable execution paths.

Confine runtime files to the workspace or `/tmp/gh-aw/agent`, with incremental
import bounds including metadata and escaping. Limit each serialized request
to 400,000 UTF-8 bytes, approximately 100,000 tokens; questions exceeding the
remaining budget abstain.

Nonempty lists emit an experimental feature warning; configuration and behavior
may change without deprecation.

Record outcomes, sanitized errors, latency, and avoided invocations in harness
artifacts and typed `harness.decision` unified-session events. Audit exposes
decision history and avoided invocations separately from executed attempts.
Show decision outcomes, latency, sanitized errors, and skipped invocation totals
in a bounded, collapsible Actions step-summary section, including initial skips
without an agent conversation.
See the [audit deny example](https://github.github.com/gh-aw/reference/audit/#invocation-skip-decisions).
