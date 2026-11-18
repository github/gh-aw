# ADR-78930: Disable implicit routing without choosing a model

**Date**: 2026-11-18
**Status**: Proposed

### Context

[ADR-79076](79076-allow-top-level-shortlist-for-router-models.md) moved main-agent shortlists to the top level. Omitting `model` is not a replacement for `engine.model-routing: false`: eligible workflows would start routing, and pinning a model instead would change how runtime overrides work.

### Decision

Use top-level `model-routing: false` to disable implicit routing without pinning a model, including from a shared import. Do not introduce a model name such as `router/off`. Reject an opt-out combined with a router selector or shortlist instead of silently ignoring either declaration.

Warn on the old form while preserving its behavior. A speed objective has no equivalent cost selector, so its warning must ask the author to choose rather than prescribe a mechanical rewrite.

### Consequences

Existing locks need no runtime change. Warnings are emitted on recompilation. Removing `cost-speed`, changing the firewall contract, and retiring the producer's tables remain separate release steps under [#78930](https://github.com/github/gh-aw/issues/78930).
