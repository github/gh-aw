# ADR-79076: Allow a Top-Level Shortlist for Router Models

**Date**: 2026-11-18
**Status**: Accepted
**Deciders**: rabeckett (PR author and author of [#78928](https://github.com/github/gh-aw/issues/78928))

---

### Context

A workflow selects a cost-oriented router level with `model: router`, `router/economy`, `router/balanced` or `router/robust`. The nested `engine.model-routing.allowed-models` form requires authors to express that choice as `goal` and `mode` instead. `models.allowed` and `models.blocked` cannot substitute for a shortlist because they constrain other model requests too. The new form needs to preserve the same distinction between main-agent candidates and global request policy.

### Decision

We will accept a top-level `model-routing.allowed-models` list, valid only with a literal main-agent router value. The list narrows only the main agent's router candidates, and it compiles to the same routing configuration as the nested `engine.model-routing.allowed-models` form. The primary driver is configuration simplicity without widening the request policy: an author sets the level and its shortlist as two top-level keys. Ambiguous combinations (a fixed or expression-valued model, an implicit default, duplicate declarations, mixing with the nested block, or a firewall too old to restrict only router candidates) fail compilation rather than discarding or widening the list.

### Alternatives Considered

#### Alternative 1: Keep the nested `engine.model-routing` block

This remains supported and produces the same selection for an equivalent cost-oriented goal, mode and shortlist. Keeping it as the only option would require authors to replace their `model: router/*` selector with an engine block just to add a shortlist. The nested form remains the only way to express `goal: cost-speed`.

#### Alternative 2: Narrow the router with `models.allowed` or `models.blocked`

These fields already restrict model choice, and reusing them would need no new key. This was rejected because they are request policies. A list written there would also restrict sub-agents and other model requests, which the issue requires to stay unchanged. The shortlist is only meaningful for the main agent's router choices.

#### Alternative 3: Union shortlists across imports, as the nested form does

The nested form combines `allowed-models` lists across imported workflows that route the same engine. Applying the same rule to the top-level key would allow a shared import and the main workflow to each contribute a list. This was rejected: a second, separately authored list would silently widen the first, so the implementation rejects more than one top-level declaration. This is the main deliberate difference between the two forms.

### Consequences

#### Positive

- An author can write `model: router/economy` and a shortlist once, without an engine block, `goal` or `mode`.
- The top-level form compiles to the same AWF proxy configuration as the nested equivalent. Compiler tests compare local and imported shortlists, including the policy for other model requests. Lock audit metadata also records the explicit router selector.
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
