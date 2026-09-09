# ADR-58267: Add aw.json Project Support to Add Command

**Date**: 2026-09-03
**Status**: Draft
**Deciders**: Unknown, adr-writer agent

---

### Context

This pull request changes `gh aw add` so repository packages can contribute `.github/workflows/aw.json` project settings in addition to workflows, resources, skills, and agents. The PR body and diff show that package-level project settings were previously ignored, which meant package authors could not ship repository configuration such as `utc`, nested `maintenance` options, or other `aw.json` defaults through the add flow. The implementation now discovers `aw.json` in local and remote packages, treats it as package content during resolution, merges it into the target repository instead of copying it verbatim, and validates the merged result before writing. Because this establishes how package-provided repository configuration is represented, merged, and excluded from package ownership tracking, the behavior should be documented as an explicit architectural decision.

### Decision

We will treat a package `.github/workflows/aw.json` file as a first-class package asset for `gh aw add` and merge its JSON object into the target repository's `aw.json` during installation. We will deep-merge nested objects so unspecified target settings are preserved, while package-provided scalar, array, and non-object values take precedence on key conflicts. We will validate the merged configuration before writing it and avoid recording the shared project config as package-owned state, so rollback and ownership tracking continue to apply only to package-managed files that are copied verbatim.

### Alternatives Considered

#### Alternative 1: Copy package aw.json into the target repository without merging

This would treat `aw.json` like any other package resource file and overwrite the target repository configuration directly. It was considered because it fits the existing resource-copying model and requires less custom logic. It was not chosen because the PR explicitly needs to preserve unspecified target settings, and full replacement would discard local configuration that the package did not intend to manage.

#### Alternative 2: Ignore package aw.json and require users to configure project settings manually

This would keep package installation limited to workflows, resources, skills, and agents, leaving repository-level configuration outside the package system. It was considered because it avoids introducing special handling for shared configuration files. It was not chosen because the PR evidence shows that packages already need to distribute project settings, and ignoring `aw.json` prevents package behavior from being applied consistently across repositories.

### Consequences

#### Positive
- Packages can now distribute repository-level `aw.json` settings through `gh aw add`, making package installation more complete.
- Existing target settings are preserved when package settings only define part of a nested object, reducing accidental configuration loss.
- Validation of the merged config prevents invalid package settings from being written into the target repository.

#### Negative
- `gh aw add` now has special-case logic for one package resource type instead of treating all resources uniformly.
- Merge semantics add behavioral complexity that maintainers must preserve across future `aw.json` schema changes.
- Package precedence over conflicting target values can still surprise users who expect local scalar or array settings to remain unchanged.

#### Neutral
- Package resolution now considers a package with only `aw.json` settings to be installable even if it contains no workflows or other assets.
- Ownership tracking intentionally excludes the merged project file because it is shared target configuration rather than a copied package-managed file.
- Tests now cover both local and remote package discovery plus merge-and-validate behavior for repository project settings.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
