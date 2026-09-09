# ADR-58796: Add repository label bootstrap actions

**Date**: 2026-09-05
**Status**: Draft
**Deciders**: gh-aw maintainers

---

## Context

Package manifests in `gh-aw` can already declare repository bootstrap actions such as variables, secrets, GitHub Apps, and handoff steps, but they cannot currently declare repository labels required by installed workflows. This PR adds a new declarative `repo-label` action to the manifest schema, parser, and bootstrap runner so installed packages can describe label requirements directly in `aw.yml`. The implementation must validate the new action consistently, apply it idempotently during bootstrap, and document the behavior for package authors. The change also needs tests that prove labels are created when missing, updated when metadata changes, and left untouched when the declared state already matches.

## Decision

We will extend the `aw.yml` `config` array with a `repo-label` action that requires `name`, `description`, and a six-character hexadecimal `color`, and validate that contract in both the manifest parser and JSON schema. We will implement bootstrap-time repository label reconciliation through the GitHub REST API so `gh aw add-wizard` can create missing labels, update changed descriptions or colors, and do nothing when the live label already matches the manifest. We will also document `config` and `repo-label` in the package manifest reference and specification so the new bootstrap capability is explicit and discoverable for package authors.

## Alternatives Considered

### Keep label setup manual outside `aw.yml`

The project could continue requiring repository maintainers to create labels manually after installing a package. This was a realistic option because labels already exist as a GitHub feature independent of `gh-aw`, and avoiding automation would keep bootstrap behavior narrower. It was not chosen because the PR evidence shows installed workflows may require specific labels, and manual setup is not declarative, repeatable, or idempotent.

### Introduce a generic repository mutation action instead of `repo-label`

Another option would be to add a more generic config action for arbitrary repository metadata changes, with labels as one subtype among many. This was considered implicitly by the existing pattern of typed bootstrap actions and would provide a broader abstraction. It was not chosen because the diff is tightly scoped to labels, and a narrower `repo-label` action keeps validation, docs, and bootstrap semantics simple while solving the immediate problem.

### Support labels only in documentation and examples first

The maintainers could have documented a label convention without adding parser, schema, and runner support yet. This would reduce implementation work in the short term. It was rejected because the PR clearly aims to make label requirements machine-readable and automatically enforced during bootstrap, which documentation alone cannot provide.

## Consequences

### Positive

- Package manifests can now declare required repository labels in a first-class, validated format instead of relying on out-of-band setup.
- Bootstrap becomes idempotent for labels by reconciling create, update, and no-op cases against the current repository state.
- Schema, parser, runtime, and documentation stay aligned around the same `repo-label` contract, reducing ambiguity for package authors.

### Negative

- Bootstrap now depends on additional GitHub REST label operations, which increases implementation surface area and error paths in repository setup.
- The manifest model becomes broader, adding another experimental action type that maintainers must preserve and test over time.

### Neutral

- The implementation introduces a dedicated label reconciliation helper and test stubs alongside the existing repository mutation helpers.
- `repo-label` remains scoped to name, description, and color; broader label lifecycle concerns such as deletion or renaming policies are still outside this ADR.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
