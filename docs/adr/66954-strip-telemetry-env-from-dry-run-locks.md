# ADR-66954: Strip Telemetry Environment Variables from Dry-Run Locks via Post-Emission YAML Rewriting

**Date**: 2026-10-08
**Status**: Draft
**Deciders**: pelikhan (PR author). Maintainer acceptance is not implied by this record.

---

### Context

`gh aw compile --dry-run` produces diagnostic lock files for inspection and review. Compilation itself never dispatches them; a separately authorized live test may execute the exact reviewed diagnostic. These locks previously carried the same telemetry export configuration as production locks: `OTEL_*` and `GH_AW_OTLP_*` variables appear across workflow-level, job-level, step-level, container, and service `env` mappings, and are also derived from engine defaults, enterprise defaults, imported configuration, and GitHub OIDC telemetry authentication. A dry-run artifact therefore advertised exporter endpoints and could collect telemetry-only credentials into generated secret declarations and manifests, allowing an authorized diagnostic run to inherit unintended export destinations.

The constraints are that ordinary (non-dry-run) compilation must be byte-for-byte unaffected; network permissions, local diagnostics, and unrelated environment values must survive untouched; custom executable script contents (which may configure their own exporters) must not be rewritten; and the filter must survive reusable-workflow body regeneration, YAML anchors/aliases, multiline scalars, and empty `env` maps. The compiler is also reused across invocations, so dry-run suppression must not leak into subsequent compilations.

### Decision

Suppress telemetry configuration in dry-run mode using early cloned workflow-data preparation and a node-aware emitted-YAML filter (`removeDryRunTelemetryEnv` in `pkg/workflow/compiler_development_telemetry.go`). The filter locates `env` mappings and splices line-range replacements into the original text rather than re-serializing the entire document. Content outside replaced mappings, including executable scripts, stays untouched. Replaced mappings may lose comments, ordering, or alias syntax; anchors used by later aliases are retained. Decode retained values as strings to preserve scalar text and reapply colon-space quoting so custom headers remain valid YAML. Filtering runs before secret collection and again after reusable-workflow body regeneration; dry-run job data also clears automatic OTLP export/authentication configuration.

Before generating jobs and headers, prepare a cloned dry-run workflow environment
and remove its telemetry environment-source entries. This prevents telemetry-only
masking steps and stale header entries from being generated. Retain the final
output filter to cover environment values contributed by engines and custom jobs.
Ordinary compilation remains unchanged. The emitted diagnostic lock and the
previously published normal lock are distinct artifacts; restoring the latter
does not transfer telemetry suppression or a review verdict to it.

The diagnostic also omits daily credit accounting and preserves or derives a
per-run cap from configured daily limits. It adds manual dispatch with
maintainer/admin actor checks and no bot exemption. These controls permit bounded
diagnostic execution without authorizing it. DEBUG-gated mutation logs name fields
and keys without printing configuration values.

### Alternatives Considered

#### Alternative 1: Suppress telemetry at configuration-construction time

Never populate `OTEL_*` / `GH_AW_OTLP_*` in the first place when the dry-run flag is set, by threading a dry-run condition through every site that contributes telemetry environment values. This is conceptually cleaner and avoids a post-processing pass. It was rejected because telemetry values enter from many independent sources — engine defaults, enterprise defaults, explicit and imported user configuration, OIDC authentication, pre/post-step environments — so the condition would have to be duplicated at every contributor, and any new contributor added later would silently reintroduce the leak. The single post-emission filter is a chokepoint that cannot be bypassed by a new producer.

#### Alternative 2: Full YAML unmarshal/marshal round-trip with keys removed

Parse the emitted lock, delete the offending keys from the tree, and re-serialise with `yaml.Marshal`. This is the shortest implementation, but a round-trip normalises formatting, drops or expands anchors and aliases, and can rewrite multiline and executable script scalars. Lock files are reviewed as text and compared against non-dry-run output, so an unrelated reformat would defeat the purpose of the diagnostic artifact. Rejected in favour of surgical line-range edits over the original content.

#### Alternative 3: Leave dry-run locks unchanged and rely on them not being executed

Compilation does not dispatch the lock, but a separately authorized diagnostic run may execute it. Leaving exporters intact unnecessarily retains destinations and telemetry-only credential references in shareable artifacts and live diagnostic configuration.

### Consequences

#### Positive
- Compiler-managed telemetry environment/configuration and its telemetry-only credential references are omitted from diagnostic locks.
- A single chokepoint governs suppression, so telemetry added by a future contributor site is filtered automatically rather than requiring a new dry-run guard.
- Content outside rewritten environment mappings, including executable scripts, is preserved; retained environment values keep their scalar text and valid quoting.

#### Negative
- The compiler now parses and rewrites its own emitted YAML, adding a post-emission pass and a second failure mode (`cannot remove dry-run telemetry environment variables`, overlapping-edit errors) that is only reachable in dry-run.
- Suppression is keyed on variable-name prefixes (`OTEL_`, `GH_AW_OTLP_`), so telemetry configured under a different name is not removed, and an unrelated variable sharing those prefixes would be removed incorrectly.
- The filter must be invoked at two points (before secret collection and after reusable-workflow body regeneration); a future emission path that bypasses both would reintroduce the leak without a compile-time error.

#### Neutral
- Non-dry-run compilation output is unchanged, so production locks still carry full telemetry configuration.
- Custom executable scripts that configure their own exporters remain outside the compiler's env suppression by design.
- The agent owns technical preflight, independent review, compilation/scanners, evidence collection, and cleanup. Human authorization may be explicitly session-scoped within recorded bounds, but compiler security warnings invalidate that grant until fresh authorization is obtained.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
