# ADR-56353: Always Configure OTLP from Default Variables and Secrets

**Date**: 2026-08-27
**Status**: Draft
**Deciders**: Unknown (automated draft from PR #56353 — review and finalize)
**Revision note**: Renamed from `56353-always-configure-otlp-from-enterprise-defaults.md` after review clarified that GitHub Actions secrets are repository- or organization-scoped while variables can still be enterprise-scoped. The ADR remains Draft until a human reviewer finalizes the automated draft.

---

### Context

Agentic workflows in gh-aw previously required per-workflow opt-in for OTLP telemetry via an `observability.otlp` frontmatter block (typically pulled in through a shared import). Repository or organization administrators wanting to enable telemetry for all workflows had to modify every workflow individually, creating adoption friction and making centralized observability governance impractical. The compiler already has a `compilerenv` default-env infrastructure for injecting organization- and enterprise-level variable values; OTLP configuration was not wired into it. GitHub Actions makes repository- or organization-level secrets and organization- or enterprise-level variables available as `secrets.*` / `vars.*` expressions, which are the appropriate mechanism for centralized defaults.

### Decision

We will make the compiler always emit OTLP environment variables (`OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_HEADERS`, `GH_AW_OTLP_ENDPOINTS`, and `GH_AW_OTLP_IF_MISSING`) in compiled workflow YAML, drawing from repository or organization defaults — `GH_AW_DEFAULT_OTLP_ENDPOINT` (secret preferred, with a variable fallback) and `GH_AW_DEFAULT_OTLP_HEADERS` (secret). Existing `observability.otlp` frontmatter wins on precedence so already-configured workflows are unaffected. When neither endpoint value is set, `GH_AW_OTLP_IF_MISSING: ignore` causes the runtime to silently drop endpoints whose URL is empty, making the default path a no-op. The compiler also skips injecting any OTLP-related env key that the workflow's own `env:` block already defines, avoiding duplicate YAML mapping keys. A half-configured default (endpoint set, headers empty) is rejected in two ways: `parseOTLPEndpoints()` — the single choke point used by every span emitter (job-setup, conclusion, outcome, and MCP gateway) — drops such an endpoint before any network call regardless of which job runs it, and a runtime bash guard (`check_otlp_default_credentials.sh`) additionally fails the agent job so the misconfiguration is visible.

### Alternatives Considered

#### Alternative 1: Continue requiring per-workflow opt-in via frontmatter

Each workflow that needs telemetry explicitly includes `observability.otlp` frontmatter or imports a shared snippet that does so. This was rejected because it does not solve the centralized governance problem — a repository or organization administrator cannot enable telemetry for all workflows from one place, and new workflows start without telemetry by default unless contributors remember to add the frontmatter.

#### Alternative 2: Org-level variable read by the existing opt-in path (no compiler change)

Introduce an org-level variable that the existing `observability.otlp` resolution checks at runtime, without changing the compiler to emit OTLP env vars by default. This was rejected because it still requires an active code path in the frontmatter resolution — workflows with no `observability.otlp` frontmatter at all would not check the variable. A compiler-level default is the only way to guarantee every compiled workflow has the env vars present.

### Consequences

#### Positive
- Repository or organization administrators can configure OTLP once at the repository or organization level and all agentic workflows inherit it automatically with no per-workflow changes.
- Workflows without explicit OTLP frontmatter do not break when defaults are absent — `GH_AW_OTLP_IF_MISSING: ignore` makes the empty-URL endpoint a silent no-op at runtime.
- Existing workflows with explicit `observability.otlp` frontmatter are unaffected; their configuration takes precedence and the new env vars are simply overwritten.

#### Negative
- Every compiled workflow now declares `GH_AW_DEFAULT_OTLP_ENDPOINT` and `GH_AW_DEFAULT_OTLP_HEADERS` in its manifest as referenced secrets, even when OTLP is not actually used, increasing manifest noise and slightly expanding the secret-reference surface area.
- The runtime credential guard fails hard (job error) when an endpoint is set without headers, which may be too strict for repositories or organizations running unauthenticated internal collectors; this strictness is scoped only to the default path, not to explicit frontmatter-configured endpoints.

#### Neutral
- The collector domain is not known at compile time (it is an expression-valued variable), so no firewall allowlist entry is added automatically; repository or organization administrators must add their collector to `network.allowed` themselves, which is documented but requires a manual step.
- `GH_AW_DEFAULT_OTLP_ENDPOINT` and `GH_AW_DEFAULT_OTLP_HEADERS` are registered as compiler-internal in `safe_update_enforcement.go` so that safe-update does not flag every recompile as introducing new secret references.
- The credential-emptiness check that drops a half-configured default endpoint is centralized in `parseOTLPEndpoints()` (`actions/setup/js/send_otlp_span.cjs`) rather than only in the agent job's bash guard, so no job ordering can result in one unauthenticated export attempt slipping through before validation runs. The env-key injection in `injectOTLPConfig` (`pkg/workflow/observability_otlp.go`) also checks the workflow's own `env:` block for every OTLP-related key it emits, not only `OTEL_SERVICE_NAME`, to avoid duplicate YAML mapping keys.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
