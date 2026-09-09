# ADR-59720: Expose JSON schemas for CLI output

**Date**: 2026-09-09
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

`gh aw audit --json` and `gh aw logs --json` already emit structured data, but the repository did not provide a supported way for users or downstream tooling to discover those payload shapes. This PR adds a `json-schema` CLI command, checked-in schema artifacts, Makefile regeneration, tests, and documentation centered on the audit and logs outputs. The change also keeps the Go output types as the source of truth by generating schemas directly from `AuditData` and `LogsData`. The repository needs an explicit decision on whether structured CLI outputs should expose versioned machine-readable schemas as part of the developer-facing interface.

### Decision

We will expose JSON schemas for the structured `audit` and `logs` CLI outputs through a new `gh aw json-schema <schema>` command and commit regenerated schema artifacts under `schemas/`. We will generate those schemas directly from the existing Go output types and share the same serialization path between CLI output and checked-in artifacts so the published schemas stay deterministic and consistent. We will also regenerate the artifacts during `make recompile` so schema freshness becomes part of the normal repository regeneration flow.

### Alternatives Considered

#### Alternative 1: Keep structured JSON output undocumented and schema-less

The project could continue emitting `audit --json` and `logs --json` without publishing schemas. This was considered because it avoids adding a new command, generated files, and regeneration logic. It was not chosen because downstream automation would still need to reverse-engineer payloads, and the PR explicitly adds tests and docs to make the output contract inspectable.

#### Alternative 2: Hand-maintain static schema files separately from the Go types

Another option would be to write `audit.schema.json` and `logs.schema.json` manually and update them when the output types change. This was considered because it could avoid adding schema-generation entry points to the CLI. It was not chosen because the PR evidence shows a stronger preference for using `GenerateOutputSchema[...]()` and shared marshaling so JSON tags and type definitions remain the single source of truth.

#### Alternative 3: Expose schemas only in repository files, without a CLI command

The project could check in generated schema artifacts but omit a user-facing command. This was considered because consumers could read the committed files directly from the repository. It was not chosen because the PR intentionally adds `gh aw json-schema audit` and `gh aw json-schema logs`, making schema discovery available from the installed CLI and not only from the source tree.

### Consequences

#### Positive
- Downstream tools gain a supported machine-readable contract for `gh aw audit --json` and `gh aw logs --json`.
- Generating schemas from `AuditData` and `LogsData` reduces drift between implementation and published schema.
- Deterministic CLI output and checked-in artifacts make schema changes easier to test and review.

#### Negative
- The repository now carries large generated schema artifacts that must be regenerated when output types change.
- `make recompile` takes on additional responsibility, so schema generation failures can block broader regeneration workflows.
- Exposing schemas makes output-shape changes more visible and may increase compatibility expectations for future changes.

#### Neutral
- A new `json-schema` command is added to the CLI utilities group.
- Tests now verify command registration, argument handling, schema validity, determinism, and artifact freshness.
- `.prettierignore` is extended to exclude the generated schema files from formatting.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
