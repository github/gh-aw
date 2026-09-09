# ADR-57746: Fix Nested Import Dependency Order and Cycle Detection

**Date**: 2026-09-01
**Status**: Draft
**Deciders**: pelikhan, adr-writer agent

---

### Context

This pull request changes how `gh-aw` resolves and merges nested workflow imports in the parser and compiler pipeline. The PR body states that the existing breadth-first merge order allowed dependents to run before their prerequisites and that relative dependency edges from subdirectories were being lost, which prevented reliable cycle detection. The diff updates the parser’s topological ordering and cycle detection behavior, adds contract and regression tests for diamond graphs, sibling declaration order, scalar precedence, role invariance, and nested cycles, and regenerates compiled workflow manifests to reflect the new deterministic import order. Because import ordering affects compiled workflow behavior across many workflows, the resolution strategy should be recorded explicitly.

### Decision

We will preserve breadth-first discovery for import collection, but apply dependency-aware topological ordering when emitting imported workflow artifacts and merged step fields. Nested imports will retain their canonical full-path dependency edges so subdirectory relationships participate in cycle detection and ordering, and tie-breaks among simultaneously ready nodes will follow declaration/discovery order for deterministic first-wins scalar semantics. We chose this approach because it fixes prerequisite-before-dependent ordering bugs and restores actionable cycle diagnostics without giving up deterministic merge behavior.

### Alternatives Considered

#### Alternative 1: Keep Pure BFS Emission Order

Continue emitting imported files and merged fields in the order discovered by breadth-first traversal.

This was considered because BFS discovery is simple and already documented in the compilation process reference. It was not chosen because the PR evidence shows BFS emission let dependents appear before their prerequisites, which broke step ordering and scalar precedence in nested import graphs.

#### Alternative 2: Use Lexical or Path-Sorted Ordering After Discovery

Sort imported files alphabetically or by normalized path after collecting the import graph.

This was considered because lexical ordering is deterministic and easy to explain. It was not chosen because the tests in this PR explicitly show lexical order can violate dependency constraints in diamond and branch-shaped graphs, and it would not preserve declaration-order tie-breaks that current merge semantics depend on.

### Consequences

#### Positive
- Imported `steps`, `pre-agent-steps`, and `post-steps` now run in dependency order, so prerequisites appear before the workflows that depend on them.
- Cycle detection works across nested subdirectories using canonical full-path edges, producing more reliable and actionable diagnostics.
- Deterministic declaration-order tie-breaks preserve first-wins behavior for scalar fields such as `max-turns` while still honoring dependency constraints.

#### Negative
- Import processing logic becomes more complex because discovery order and emission order are now distinct concepts that must both remain correct.
- Existing generated workflow manifests change order in many files, creating broad lockfile churn even though the semantic change is localized.
- Documentation that previously described BFS processing as the effective ordering model now needs clarification so future contributors do not reintroduce the bug.

#### Neutral
- Breadth-first traversal remains the collection mechanism for discovering imports, but topological sort becomes the authoritative merge/emission order.
- The repository adds more contract-style tests around import ordering, scalar precedence, role invariance, and nested cycles.
- The user-visible change is primarily in deterministic ordering and diagnostics rather than in new workflow surface area.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
