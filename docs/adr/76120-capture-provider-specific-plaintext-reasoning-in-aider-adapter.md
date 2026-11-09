# ADR-76120: Capture provider-specific plaintext reasoning in the Aider adapter

**Date**: 2026-11-08
**Status**: Draft

## Context

[ADR-67395](67395-capture-aider-native-session-evidence-via-runtime-hooks.md),
also Draft, describes native Aider response capture using `reasoning_content`
and `reasoning`. LiteLLM completion messages can instead expose plaintext in
`provider_specific_fields.reasoning_text`. Without an adapter fallback, that
text does not become a canonical `assistant.reasoning` event.

This record documents the narrow refinement in
[PR #76120](https://github.com/github/gh-aw/pull/76120); it does not replace
ADR-67395 or imply acceptance of either decision.

## Decision

In `.github/workflows/shared/aider.md`, read `reasoning_content` first and
`reasoning` only when the first value is absent or null. Only when both values
are absent or null, use `provider_specific_fields.reasoning_text` if it is a
string.

Preserve existing precedence and explicit empty strings, including an empty
fallback string. Do not coerce non-string fallback values or promote
`reasoning_opaque` into plaintext reasoning events. Preserve the existing
native message metadata and `partial: true` propagation for length-limited
responses. Keep extraction in the Aider adapter; shared archival and RPC
normalization are unchanged.

## Alternatives

These are tradeoffs for review, not claims of prior evaluation or approval.

- **Leave extraction unchanged.** Avoids another LiteLLM field dependency but
  misses plaintext available only in the provider-specific field.
- **Promote opaque reasoning.** Does not establish plaintext and would
  misrepresent opaque data as readable reasoning.
- **Normalize in shared archival or RPC code.** Broadens an adapter-specific
  compatibility change into shared consumers instead of emitting the existing
  canonical event at capture time.

## Consequences

Available fallback plaintext reaches existing reasoning consumers without a
new event type or changed top-level precedence. The adapter gains a dependency
on a LiteLLM-specific field; generated workflow locks must stay synchronized
with the shared source.

This fallback cannot recover completions that never reach the response hook.
The reported unexplained completion gap and broader baseline test failures
remain separate issues. No post-fix live Actions validation is established by
this record, and it contains no raw reasoning payloads.
