# ADR-57812: Add Package Visibility Metadata

**Date**: 2026-09-02
**Status**: Draft
**Deciders**: pelikhan, adr-writer agent

---

### Context

This pull request extends the `aw.yml` package manifest so repository packages can declare installation visibility and maturity metadata. The diff adds `private` and `experimental` fields to the manifest schema, parses them in the CLI package manifest loader, updates resolution behavior so `gh aw add` refuses private packages and emits a warning for experimental packages, and documents the new fields in both the manifest reference and specification. Because these flags change the contract between package authors and package consumers, the installation policy should be captured explicitly for future maintainers.

### Decision

We will add two boolean manifest fields, `private` and `experimental`, both defaulting to `false`, and make `gh aw add` enforce them during repository package resolution. When `private` is `true`, package installation will fail; when `experimental` is `true`, installation will proceed with a warning. We chose this approach because it gives package authors an explicit, machine-readable way to prevent installation of internal packages while still signaling lower-stability packages without blocking use.

### Alternatives Considered

#### Alternative 1: Keep Package Visibility Out of the Manifest

Continue treating all repository packages as installable and rely on documentation or naming conventions to communicate whether a package is internal or unstable.

This was considered because it avoids new manifest surface area and keeps package resolution behavior simple. It was not chosen because the PR evidence shows a need for enforcement and warnings at install time, and documentation-only signaling would not prevent accidental installation of private packages.

#### Alternative 2: Use a Single Enum Status Field

Replace the two booleans with one status field such as `visibility: public|private|experimental` or a similar combined metadata value.

This was considered because a single field can centralize package state and may be easier to extend later. It was not chosen because the PR implements two independent booleans, which fit the current requirements directly, preserve default compatibility, and distinguish a hard installation block (`private`) from a soft warning (`experimental`) without introducing a broader state model.

### Consequences

#### Positive
- Package authors can explicitly mark packages that must never be installed via `gh aw add`.
- Consumers receive a clear warning before adding experimental packages, improving visibility into support and stability expectations.
- The JSON schema, parser, resolver, tests, and docs now share a consistent contract for package visibility metadata.

#### Negative
- The package manifest surface area grows, adding more fields that must be validated, documented, and maintained over time.
- Installation logic becomes slightly more complex because package resolution must now enforce blocking and warning behavior based on metadata.
- Future package lifecycle states may require additional design work if the two booleans are not sufficient.

#### Neutral
- Existing manifests remain compatible because both new fields default to `false` when omitted.
- Test coverage expands to cover default parsing, explicit booleans, schema rejection for invalid types, blocking behavior, and warning behavior.
- Documentation and specification references now include package visibility semantics alongside the rest of the manifest contract.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
