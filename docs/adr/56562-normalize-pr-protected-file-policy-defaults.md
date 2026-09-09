# ADR-56562: Normalize PR Protected-File Policy Defaults

**Date**: 2026-08-28
**Status**: Draft
**Deciders**: dsyme, copilot-swe-agent

---

### Context

The safe-output pull request handlers exposed inconsistent public defaults for protected-file enforcement. `create-pull-request` documented and emitted `request_review`, while related docs and policy names were otherwise hyphenated, and `push-to-pull-request-branch` defaulted to a stricter blocked behavior. The current protected-file set also treated `CHANGELOG.md` as protected, which blocked routine changelog-only release updates even though those edits are common and lower risk than changes to manifests, CI, or agent instruction files. This PR updates parser validation, handler config generation, runtime checks, docs, and tests together, so the repository is making an explicit API-shape and default-policy decision rather than a local bug fix in one file.

### Decision

We will canonicalize the public protected-files policy value for safe-output PR handlers to `request-review`, while continuing to accept the legacy underscore alias for backward compatibility. We will apply that same default to both `create-pull-request` and `push-to-pull-request-branch`, and we will exclude `CHANGELOG.md` from the default protected-file set so routine changelog edits do not require protected-file escalation. The implementation normalizes accepted input during parsing and config building so downstream runtime logic and documentation use one canonical hyphenated form.

### Alternatives Considered

#### Alternative 1: Keep `request_review` as the Public Canonical Value

The repository could have kept the underscore form as the official external API and only updated isolated docs or tests for consistency.

This was considered because it minimizes visible API change. It was not chosen because the diff shows the project wants an idiomatic hyphenated YAML surface, and leaving the underscore form canonical would preserve the mismatch between documentation, user expectations, and the normalized policy language used elsewhere.

#### Alternative 2: Keep Different Defaults Per Handler and Continue Protecting `CHANGELOG.md`

The repository could have left `push-to-pull-request-branch` stricter than `create-pull-request` and continued treating `CHANGELOG.md` like other protected top-level project files.

This was considered because stricter defaults can reduce accidental changes to sensitive files. It was not chosen because the PR evidence shows those differences are causing unnecessary friction for realistic changelog updates and making two closely related safe-output handlers behave inconsistently for the same policy concept.

### Consequences

#### Positive
- The public YAML and schema surface becomes consistent around one canonical policy spelling: `request-review`.
- Both PR-related safe-output handlers now share the same default behavior, reducing surprise for workflow authors.
- Routine changelog-only PRs no longer trigger protected-file handling by default, which better matches common release workflows.

#### Negative
- The code must carry backward-compatibility normalization logic for legacy underscore inputs, adding maintenance surface.
- Existing tests, golden files, schema descriptions, and documentation all need coordinated updates when the canonical public value changes.
- Excluding `CHANGELOG.md` from the default protected-file set slightly reduces the default review friction for a file that still affects project communication.

#### Neutral
- Runtime checks still map protected-file modifications to the existing internal `request_review` action, so the external naming change does not require a full downstream behavior redesign.
- Existing configurations using `request_review` remain supported through normalization rather than hard failure.
- The change touches both compiler output and generated golden fixtures, so future regressions are more likely to be caught by snapshot-style tests.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
