# ADR-66954: Strip Telemetry Environment Variables from Dry-Run Locks via Post-Emission YAML Rewriting

**Date**: 2026-10-08
**Status**: Draft
**Deciders**: pelikhan (PR author). Maintainer acceptance is not implied by this record.

---

### Context

`gh aw compile --dry-run` produces diagnostic lock files that are retained locally for inspection and shared in review evidence, but are never dispatched. Until now those locks carried the same telemetry export configuration as production locks: `OTEL_*` and `GH_AW_OTLP_*` variables appear across workflow-level, job-level, step-level, container, and service `env` mappings, and are also derived from engine defaults, enterprise defaults, imported configuration, and GitHub OIDC telemetry authentication. A dry-run artifact therefore advertised exporter endpoints and could cause telemetry-only credentials to be collected into generated secret declarations and manifests, even though no export can ever occur from a lock that is not run.

The constraints are that ordinary (non-dry-run) compilation must be byte-for-byte unaffected; network permissions, local diagnostics, and unrelated environment values must survive untouched; custom executable script contents (which may configure their own exporters) must not be rewritten; and the filter must survive reusable-workflow body regeneration, YAML anchors/aliases, multiline scalars, and empty `env` maps. The compiler is also reused across invocations, so dry-run suppression must not leak into subsequent compilations.

### Decision

We will suppress telemetry configuration in dry-run mode by rewriting the emitted YAML after generation, using a node-aware filter (`removeDryRunTelemetryEnv` in `pkg/workflow/compiler_development_telemetry.go`) that walks the parsed document, locates only `env` mapping nodes, and splices line-range edits back into the original text. Operating on line ranges rather than re-serialising the whole document preserves formatting, comments, anchors, and executable scalar contents exactly. The filter runs before secret collection and again after reusable-workflow body regeneration, and the dry-run job data separately clears automatic OTLP export/authentication configuration so no telemetry credential survives into secret declarations or manifests.

### Alternatives Considered

#### Alternative 1: Suppress telemetry at configuration-construction time

Never populate `OTEL_*` / `GH_AW_OTLP_*` in the first place when the dry-run flag is set, by threading a dry-run condition through every site that contributes telemetry environment values. This is conceptually cleaner and avoids a post-processing pass. It was rejected because telemetry values enter from many independent sources — engine defaults, enterprise defaults, explicit and imported user configuration, OIDC authentication, pre/post-step environments — so the condition would have to be duplicated at every contributor, and any new contributor added later would silently reintroduce the leak. The single post-emission filter is a chokepoint that cannot be bypassed by a new producer.

#### Alternative 2: Full YAML unmarshal/marshal round-trip with keys removed

Parse the emitted lock, delete the offending keys from the tree, and re-serialise with `yaml.Marshal`. This is the shortest implementation, but a round-trip normalises formatting, drops or expands anchors and aliases, and can rewrite multiline and executable script scalars. Lock files are reviewed as text and compared against non-dry-run output, so an unrelated reformat would defeat the purpose of the diagnostic artifact. Rejected in favour of surgical line-range edits over the original content.

#### Alternative 3: Leave dry-run locks unchanged and rely on them not being executed

Since a dry-run lock is never dispatched, the exporter configuration is inert. Rejected because the lock is still a retained, shareable artifact: it discloses endpoints and pulls telemetry-only credentials into generated secret declarations and manifests, which is unnecessary exposure for an artifact with no execution path.

### Consequences

#### Positive
- Dry-run diagnostic locks no longer disclose telemetry endpoints or pull telemetry-only credentials into secret declarations and manifests.
- A single chokepoint governs suppression, so telemetry added by a future contributor site is filtered automatically rather than requiring a new dry-run guard.
- Formatting, comments, anchors/aliases, and executable script bodies in the emitted lock are preserved, keeping dry-run output diffable against normal compilation.

#### Negative
- The compiler now parses and rewrites its own emitted YAML, adding a post-emission pass and a second failure mode (`cannot remove dry-run telemetry environment variables`, overlapping-edit errors) that is only reachable in dry-run.
- Suppression is keyed on variable-name prefixes (`OTEL_`, `GH_AW_OTLP_`), so telemetry configured under a different name is not removed, and an unrelated variable sharing those prefixes would be removed incorrectly.
- The filter must be invoked at two points (before secret collection and after reusable-workflow body regeneration); a future emission path that bypasses both would reintroduce the leak without a compile-time error.

#### Neutral
- Non-dry-run compilation output is unchanged, so production locks still carry full telemetry configuration.
- Custom executable scripts that configure their own exporters remain outside the compiler's env suppression by design.
- This PR also reassigns workflow security preflight, compilation/scanner runs, evidence collection, and diagnostic cleanup to the agent in `.github/aw/` and `.github/skills/review-agentic-workflows/`; genuine live-authorization gates are unchanged. That is a process change accompanying, but separable from, the decision recorded here.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
