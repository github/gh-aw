# ADR-57968: Import Bot Allowlists from `on.bots`

**Date**: 2026-09-02
**Status**: Draft
**Deciders**: pelikhan, adr-writer agent

---

### Context

This pull request fixes how shared workflows contribute bot allowlists during import processing. The PR description states that imports incorrectly read `bots` from the top level even though the supported frontmatter structure now stores bot allowlists under `on.bots`. The diff updates the import field extractor to read `bots` from the `on` section, updates schema coverage to reject unsupported top-level `bots`, and revises workflow and codemod tests around imported-only, merged, and overlapping allowlists. Because this changes how imported workflow activation metadata is interpreted and migrated, the behavior should be documented explicitly.

### Decision

We will treat `on.bots` as the canonical location for bot allowlists in shared and main workflows, and imported workflows will contribute their bot allowlists by reading from that nested field. We will merge imported and importing workflow allowlists by preserving first-seen order and removing duplicates. We will also update the legacy codemod so that when both top-level `bots` and `on.bots` exist, it consolidates them into a single `on.bots` entry instead of leaving conflicting representations.

### Alternatives Considered

#### Alternative 1: Continue Reading Imported Bots from Top-Level `bots`

Keep the existing importer behavior and continue extracting bot allowlists from a top-level `bots` field.

This was considered because it would avoid changing the importer and keep older expectations intact. It was not chosen because the PR evidence shows top-level `bots` is no longer a supported frontmatter field, so continuing to read it from imports would preserve incorrect behavior and make shared workflows inconsistent with the validated schema.

#### Alternative 2: Require Main Workflows to Duplicate Imported Bot Allowlists Manually

Do not merge bot allowlists from imported workflows and instead require each importing workflow to restate every allowed bot in its own frontmatter.

This was considered because it reduces implicit behavior in the importer. It was not chosen because the regression tests in this PR explicitly cover imported-only and combined allowlists, showing that shared workflows are expected to contribute bot activation metadata and that manual duplication would be repetitive and error-prone.

### Consequences

#### Positive
- Imported shared workflows now contribute bot allowlists from the supported `on.bots` field, matching the documented schema.
- Combined bot allowlists are deterministic because merge order is preserved and duplicates are removed.
- Legacy workflow migration becomes safer because the codemod consolidates dual representations into one canonical `on.bots` field.

#### Negative
- Import behavior now depends on nested-field extraction, which adds some implementation complexity in parser and codemod logic.
- Workflows or tests that still assume top-level `bots` is accepted must be updated to the canonical nested form.
- The codemod emits JSON-style inline bot arrays when merging, which may alter formatting compared with the original YAML layout.

#### Neutral
- Regression coverage now focuses on imported-only, merged, and overlapping allowlist cases.
- Schema validation and import extraction become more tightly coupled around the canonical `on.bots` structure.
- The change affects activation metadata handling rather than the compiled workflow steps themselves.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
