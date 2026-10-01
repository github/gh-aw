# ADR-64646: Add run-wide tool-call budget for Copilot SDK

**Date**: 2026-10-01
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

This pull request adds a new top-level `max-tool-calls` workflow setting and threads it through parsing, validation, Copilot SDK tool configuration, runtime enforcement, and documentation. The PR description states that existing limits did not provide an enforceable run-wide ceiling spanning local tools, MCP calls, concurrent dispatches, failed calls, permission denials, and SDK subagents. The diff shows that enforcement must happen before tool execution in the built-in Copilot SDK harness so concurrent calls cannot overspend the budget and rejected calls are still counted. The visible constraint in the PR is that this control must be rejected for unsupported engines and for custom Copilot drivers or harnesses that cannot guarantee the same aggregate enforcement semantics.

### Decision

We will introduce a top-level `max-tool-calls` setting that applies a single aggregate tool-dispatch budget to Copilot SDK workflow runs when using the built-in SDK driver and harness. We will enforce that budget through a synchronous pre-tool hook that debits every attempted dispatch before execution, shares the count across parent and subagent calls, and emits explicit debit and exhaustion events. We chose this because the PR evidence shows the required architectural property is a single enforceable budget across all tool categories, including concurrent and denied calls, and that property is only available in the built-in Copilot SDK integration path.

### Alternatives Considered

#### Alternative 1: Keep using only per-tool or per-permission limits

This was realistic because the repository already supports controls such as `tools.github.allowed[].max-calls` and `max-tool-denials`, and extending documentation around those limits would have been cheaper than adding a new end-to-end setting. It was not chosen because the PR description and tests explicitly require one run-wide budget that covers local tools, MCP calls, failed calls, permission denials, and subagents, which existing per-tool and denial-based controls do not enforce.

#### Alternative 2: Support `max-tool-calls` for all engines and custom Copilot drivers immediately

This was considered because a uniform top-level limit across every engine would be simpler for authors to understand. It was not chosen because the implementation evidence in this PR depends on the built-in Copilot SDK pre-tool hook, and the new validation code and docs explicitly state that other engines and custom driver or harness overrides do not expose an enforceable aggregate interception point.

### Consequences

#### Positive
- Workflow authors get a single explicit run-wide tool budget that covers primary-agent and Copilot SDK subagent dispatches.
- The budget is reserved before execution, so concurrent calls, failed executions, and permission denials all consume the same enforceable limit.
- Validation and documentation make unsupported configurations fail closed instead of silently accepting a limit that cannot actually be enforced.

#### Negative
- `max-tool-calls` is only available on the built-in Copilot SDK path, so workflows using other engines or custom Copilot drivers and harnesses cannot adopt the feature.
- The change adds another cross-layer configuration field that must stay aligned across schema validation, import processing, engine config parsing, runtime JS hooks, tests, and documentation.

#### Neutral
- The feature is additive and distinct from existing per-tool MCP limits such as `tools.github.allowed[].max-calls`, which remain in place for narrower use cases.
- The runtime now emits budget debit and exhaustion diagnostics, increasing observability without changing the underlying tool implementations themselves.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
