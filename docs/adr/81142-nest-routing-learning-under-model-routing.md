# ADR-81142: Nest Routing Learning Under model-routing

**Date**: 2026-11-25
**Status**: Draft
**Deciders**: rabeckett (PR author)
**Related**: [PR #81142](https://github.com/github/gh-aw/pull/81142), [PR #80583](https://github.com/github/gh-aw/pull/80583), [ADR-80583](80583-adaptive-routing-observe-history.md)

## Context

[ADR-80583](80583-adaptive-routing-observe-history.md) added observe mode for adaptive routing. Observe mode records each eligible routed run in a per-workflow history without changing which model runs. Its setting was a top-level workflow key, `adaptive-routing: observe`, with a repository default `adaptive_routing` in `.github/workflows/aw.json`.

That key is unreleased. It did not appear in v1.4.0, the latest published prerelease.

Forces:

- Model routing decides which model runs. Learning decides only whether a workflow records what happened. A top-level key beside `model:` reads as a second routing choice.
- `model-routing` already holds routing settings (`allowed-models`) and follows an import precedence order. Learning needs the same order: the workflow's value, then the first import that sets it, then the repository default.
- Only one spelling should ship, so the unreleased key should not survive as an alias.
- Ordinary Markdown imports may contain unrelated frontmatter. If a removed `adaptive-routing: false` opt-out were ignored, the repository default could start recording a workflow that its author meant to exclude.

## Decision

1. Declare learning as `learning` under the top-level `model-routing` block: `model-routing: {learning: observe}`. Only `observe` and `false` are accepted. `true` and other values fail to compile.
2. The repository default moves to `.github/workflows/aw.json` as `"model_routing": {"learning": "observe"}`. The default is off.
3. Precedence: the workflow's own `learning` value (including `false`), then the first import that sets it, then the repository default.
4. Learning does not enable model routing, and it does not count as a model choice. A `model-routing` block that sets only `learning` is valid. It does not trigger connector-inference conflicts or suppress default routing. `allowed-models` keeps its existing meaning.
5. A removed top-level `adaptive-routing` key in an ordinary Markdown import fails compilation with guidance. It is not ignored.
6. The history format, the `push_routing_history` writer job, and `gh aw routing status` and `reset` are unchanged.

## Alternatives Considered

1. **Keep the top-level `adaptive-routing: observe` key from ADR-80583.** Rejected. Learning would stay outside the `model-routing` block that controls routing, and the two routing-related keys would need their own precedence rules.
2. **Accept both spellings during a transition.** Rejected. The top-level key never shipped in a published release, so there is no compatibility window to protect. Two spellings would also need a precedence rule between them.
3. **Treat `learning` as a routing declaration that enables routing.** Rejected. Learning observes a choice made elsewhere. Enabling routing from it would change which model runs for workflows that did not opt into routing. The compiler therefore checks for a real model choice (`declaresTopLevelRoutingChoice`) and does not treat a learning-only block as one.

## Consequences

Positive:

- Routing and learning are declared in one block, and the documented example reads as a property of routing.
- One spelling ships, and the repository default uses the same nesting.
- Learning uses the same import precedence as the other `model-routing` settings.
- A removed key in a Markdown import fails the build instead of silently enabling the repository default.

Negative:

- The change is breaking for any workflow that adopted `adaptive-routing` from main after ADR-80583. Those workflows must move to `model-routing.learning`.
- The `model-routing` schema no longer requires `allowed-models`. The compiler must therefore distinguish a learning-only block from a shortlist, which adds a rule for readers to learn.
- The import check for the removed key is a special case. It can be deleted once no supported workflow uses the key.
- Live routing, if it is added later, will need its own decision. `learning` accepts only `observe` and `false`.

*Drafted by the Design Decision Gate from PR #81142 evidence. The PR author should review and complete it.*
