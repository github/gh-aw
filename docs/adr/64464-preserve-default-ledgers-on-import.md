# ADR-64464: Preserve default ledgers on import

**Date**: 2026-09-30
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

This pull request changes how gh-aw merges `tools.ledger` configuration when one workflow imports another. The PR description and tests show a specific failure mode: combining the concise single-ledger form with named ledgers could silently drop the consuming workflow's default ledger, and the inverse import direction had the same risk. The affected code lives in workflow frontmatter merging and compilation, where imported workflow configuration must be combined deterministically before ledger schemas and persistence jobs are generated. The architectural decision is how imported and local ledger definitions should be normalized so both default and named ledgers survive composition.

### Decision

We will normalize the concise single-ledger form to a named `default` ledger whenever it is merged with a named-ledger map from another workflow. We will apply this normalization during tool merging for the `ledger` key so imported and local configurations preserve both the default ledger and any explicitly named ledgers regardless of merge direction. We chose this because the PR evidence shows that import composition, compilation output, and ledger persistence behavior all depend on retaining both configurations instead of letting one shape overwrite the other.

### Alternatives Considered

#### Alternative 1: Keep the existing merge behavior

This was realistic because the prior merge logic already handled recursive map merges for tool configuration. It was not chosen because the PR evidence shows that the old behavior could silently discard either the local default ledger or the imported default ledger when the other side used named ledgers, producing incorrect compiled workflow state.

#### Alternative 2: Require all workflows to declare ledgers only in the named form

This was considered because a single canonical shape would simplify merge behavior and reduce normalization logic. It was not chosen because the current feature supports a concise single-ledger form, and the PR adds targeted normalization so existing workflows can continue using that form while still composing correctly with named ledgers.

### Consequences

#### Positive
- Imported workflows preserve both default and named ledger definitions instead of losing one during frontmatter merging.
- Ledger compilation remains consistent across both import directions, including generated persistence job output.
- Tests now cover merge-level and compile-level cases for this cross-workflow configuration shape.

#### Negative
- Tool merging gains ledger-specific normalization logic that future contributors must understand and maintain.
- The code must continue distinguishing between concise single-ledger configuration and named-ledger maps, which adds shape-detection complexity.

#### Neutral
- The change is localized to frontmatter merge behavior and ledger-focused tests rather than introducing a new user-facing ledger capability.
- Existing concise single-ledger workflows keep their current authoring syntax unless they are merged with named ledgers.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
