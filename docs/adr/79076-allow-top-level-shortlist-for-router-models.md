# ADR-79076: Allow a Top-Level Shortlist for Router Models

**Date**: 2026-11-18
**Status**: Draft
**Deciders**: rabeckett (PR author and author of issue #78928); [TODO: verify other deciders and the parent issue #78927 owners]

---

### Context

A workflow selects a cost-oriented router level with `model: router`, `router/economy`, `router/balanced` or `router/robust`. The only way to limit which models the router may choose for the main agent is the nested `engine.model-routing.allowed-models` block. That block requires `goal` and `mode`, so an author who wants a shortlist must configure the engine twice, once for the model and once for routing (issue #78928). `models.allowed` and `models.blocked` cannot do this job because they are request policies that apply to every model request, not only the main agent's. The shortlist must therefore be scoped to the main agent, and an older AWF firewall that cannot separate router candidates from other requests must not silently restrict all requests.

### Decision

We will accept a top-level `model-routing.allowed-models` list, valid only with a literal main-agent router value. The list narrows only the main agent's router candidates, and it compiles to the same routing configuration as the nested `engine.model-routing.allowed-models` form. The primary driver is configuration simplicity without widening the request policy: an author sets the level and its shortlist as two top-level keys. Ambiguous combinations (a fixed or expression-valued model, an implicit default, duplicate declarations, mixing with the nested block, or a firewall too old to restrict only router candidates) fail compilation rather than discarding or widening the list.

### Alternatives Considered

#### Alternative 1: Keep the nested `engine.model-routing` block

This is the status quo and remains supported. It already produces the same selection when combined with `goal: cost` and `mode: economy`. It was not chosen as the only path because it forces an engine block and a second routing configuration beside a `model: router/*` value that already selects the mode. This was a close call, since the nested form needs no new syntax. The new form is still limited to cost-oriented routing, so the nested form remains the only way to express `goal: cost-speed`.

#### Alternative 2: Narrow the router with `models.allowed` or `models.blocked`

These fields already restrict model choice, and reusing them would need no new key. This was rejected because they are request policies. A list written there would also restrict sub-agents and other model requests, which the issue requires to stay unchanged. The shortlist is only meaningful for the main agent's router choices.

#### Alternative 3: Union shortlists across imports, as the nested form does

The nested form combines `allowed-models` lists across imported workflows that route the same engine. Applying the same rule to the top-level key would allow a shared import and the main workflow to each contribute a list. This was rejected: a second, separately authored list would silently widen the first, so the implementation rejects more than one top-level declaration. This is the main deliberate difference between the two forms.

### Consequences

#### Positive

- An author can write `model: router/economy` and a shortlist once, without an engine block, `goal` or `mode`.
- The top-level form compiles to the same AWF proxy configuration and lock model configuration as the nested equivalent. Tests compare both for the local, imported and mixed cases.
- Other model requests (sub-agents and similar) keep their current policy. Tests assert that the full proxy configuration is identical to the nested form.
- Ambiguous inputs fail at compile time, including a fixed model, an implicit default, an expression-valued model, duplicate declarations, an empty list, and firewalls older than v0.28.33. None of these silently discards or widens the list.

#### Negative

- Two syntaxes now express the same feature. The nested form stays supported, so docs and tests must cover both and explain which to choose.
- The two forms disagree on imports. The top-level form errors on a second declaration, while the nested form unions lists. An author who combines shared imports may hit a compile error they did not expect.
- The top-level form requires AWF v0.28.33 or later. Workflows pinned to an older firewall fail compilation rather than running with a shortlist.
- The top-level block does not accept `goal` or `mode`, so a speed-oriented shortlist (`goal: cost-speed`) still needs the nested block.
- Shortlist names are not checked against current availability. A shortlist can compile and still leave no eligible model at run time.

#### Neutral

- A new top-level `model-routing` key reuses the nested `allowed-models` schema through a `$ref`. Imported `model-routing` blocks are validated at import time.
- The import accumulator stores shortlists as a slice (`RouterShortlists`) rather than one value, so a conflict can be detected.
- Documentation changes span the frontmatter reference, the full frontmatter reference, the model-routing reference, the model alias specification and the engine syntax guide. The top-level key is labelled experimental.
- Workflow migrations and any deprecation of the nested form are separate follow-up steps, as the issue states.
- The smoke workflow was compiled locally with both forms and the firewall output compared. It was not dispatched.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
