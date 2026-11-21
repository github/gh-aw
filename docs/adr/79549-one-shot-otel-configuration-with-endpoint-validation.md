# ADR-79549: One-Shot OTEL Configuration With Authenticated Endpoint Validation

**Date**: 2026-11-20
**Status**: Draft
**Deciders**: Unknown (draft generated from PR #79549; author to confirm)

---

### Context

Workflows read OTLP defaults from Actions variables and secrets (`OTEL_EXPORTER_OTLP_ENDPOINT`, headers, `OTEL_EXPORTER_OTLP_PROTOCOL`, `GH_AW_OTEL_METRICS`). Setting these across many repositories or organizations required several `gh aw env update` or `gh secret set` calls. Credentials could leak through argv or shell history, and a wrong endpoint or header was only found when a workflow later failed to export. GitHub has no enterprise-level Actions secrets API, and writes across targets cannot be made transactional.

### Decision

We will add a dedicated `gh aw env otel` subcommand that configures one OTLP endpoint for repeated `--repo`, `--org`, and `--enterprise` targets. It sends an authenticated, empty traces export to `/v1/traces` before any write, with a 10-second deadline, a 64 KiB response limit, and no redirects. It stores the endpoint and headers as encrypted Actions secrets and the protocol and metrics flag as variables. Headers come from hidden interactive input or `--headers-stdin` (up to 48 KiB), never argv, and enterprise targets are expanded to their visible organizations through GraphQL. The primary driver is safe, repeatable bulk setup that fails early on bad credentials.

This command uses the existing scalar settings without compiler, runtime, or generated-workflow changes. Updates temporarily disable the endpoint, save credentials and variables, then enable the validated endpoint last. Organization setup rejects partial repository secret overrides and changes to existing secret visibility before writing.

### Alternatives Considered

#### Alternative 1: Keep using `gh aw env update` and `gh secret set`

This needs no new code and reuses existing commands. It was not chosen because it cannot validate the endpoint before saving. It also needs one invocation per target and per setting, and it encourages passing header credentials as arguments. This was the status quo for the OTEL defaults and was a reasonable baseline.

#### Alternative 2: Save settings without validating the endpoint

Skipping the probe would remove network behavior and the SSRF and response-handling surface the probe adds. It was not chosen because a typo or bad credential would be written to many targets and only found at runtime. The empty-export probe is the cheapest check that exercises both route and authentication.

#### Alternative 3: Use enterprise-level secrets and variables directly

A single enterprise-scope write would be simpler for users. It was not possible because GitHub does not provide an enterprise-level Actions secrets API, so enterprise setup fans out to organization-level settings.

### Consequences

#### Positive
- One command, or one stdin-fed non-interactive call, configures many repositories, organizations, or enterprise members consistently.
- Bad endpoints and rejected credentials are caught before any write, and the saved headers are the same normalized headers the probe used.
- Header values stay out of argv and shell history, and raw collector bodies and network errors are not echoed.

#### Negative
- Writes are nontransactional. A mid-run failure identifies the target and can leave export disabled there; rerunning completes the configuration. Earlier targets may already be saved.
- The probe checks route and credential acceptance, not span ingestion or connectivity from GitHub runners, so it can give false confidence.
- Enterprise expansion covers only the organizations visible to the caller. Organizations hidden from enumeration or added later are not configured.
- Organization setup requires access to inspect affected repository secrets and, outside selected visibility, verify the complete repository inventory.

#### Neutral
- All targets share a single default endpoint, and an endpoint secret takes precedence over an endpoint variable, including across scopes.
- Sentry endpoint headers are normalized to `x-sentry-auth` before testing and saving.
- The command changes only the four OTEL settings and preserves unrelated defaults.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
