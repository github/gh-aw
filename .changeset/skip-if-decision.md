---
"gh-aw": patch
---

Add top-level `skip-if-decision` questions evaluated inside AWF before eligible
agent invocations, including retries, resumes, and continuations. Questions share
one Decisions request through the existing API proxy and resolve guarded runtime
file references in the agent sandbox. Any deny skips; allow or all unknown
preserves the deterministic execution path. Evaluation failures abstain without
additional retries. All built-in and declarative engines share the gate, including
command-based, custom-harness, and durable execution paths.

Record outcomes, sanitized errors, latency, and avoided invocations in harness
artifacts and typed `harness.decision` unified-session events. Audit exposes
decision history and avoided invocations separately from executed attempts.
See the [audit deny example](https://github.github.com/gh-aw/reference/audit/#invocation-skip-decisions).
