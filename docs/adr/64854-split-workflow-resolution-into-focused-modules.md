# ADR-64854: Split workflow resolution into focused modules

**Date**: 2026-10-01
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

`pkg/cli/add_workflow_resolution.go` had grown to 1,214 lines and combined several responsibilities: repository-package resolution, local package handling, package asset discovery, package spec construction, and wildcard-related behavior. The PR description states that the goal is to split this implementation into five files while preserving the public API and existing behavior. The diff adds dedicated test coverage for dispatch parsing, local package discovery, asset discovery, and package spec generation, indicating that maintainability and targeted verification are the main constraints alongside backward compatibility.

### Decision

We will decompose workflow-resolution logic into several focused `pkg/cli` modules, each owning a distinct concern such as local package resolution, package asset discovery, package spec generation, and wildcard handling. We will keep the existing external behavior and public APIs unchanged, and reinforce that refactor with focused tests for the extracted helpers. We chose this to reduce the maintenance cost and cognitive load of a single oversized source file without introducing user-visible behavior changes.

### Alternatives Considered

#### Alternative 1: Keep the monolithic `add_workflow_resolution.go` file

This would avoid file churn and preserve all existing code locality in one place. It was not chosen because the PR evidence explicitly identifies the 1,214-line file as mixing multiple concerns, which makes review, navigation, and future changes harder.

#### Alternative 2: Redesign workflow resolution behavior while splitting the code

This would use the refactor as an opportunity to alter package-resolution semantics or public interfaces. It was not chosen because the PR description says public APIs and behavior are unchanged, and the added tests focus on preserving existing behavior rather than introducing a new model.

### Consequences

#### Positive
- Each workflow-resolution concern now has a smaller, more focused implementation unit that is easier to read and modify.
- Focused tests around extracted helpers improve confidence that the refactor preserves existing behavior.
- Future changes to local packages, package assets, or package specs can be made with less risk of unrelated edits in one large file.

#### Negative
- Related resolution logic is now spread across multiple files, so understanding the full control flow may require more cross-file navigation.
- The refactor increases the number of internal helper boundaries that must remain consistent as behavior evolves.
- Reviewers and maintainers must ensure future changes do not reintroduce duplication across the new modules.

#### Neutral
- The change is primarily internal to `pkg/cli`; the stated external CLI behavior and public APIs remain the same.
- Existing workflow-resolution capabilities still exist, but are reorganized behind narrower helper functions and files.
- Test coverage expands in areas touched by the extraction, especially around local package and wildcard behavior.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
