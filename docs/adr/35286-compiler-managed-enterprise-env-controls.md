# ADR-35286: Compiler-Managed Enterprise Env Controls with GH_AW_DEFAULT_* Override Chain

**Date**: 2026-05-27
**Status**: Draft
**Deciders**: PR author (pelikhan), reviewers of PR #35286

---

## Part 1 — Narrative (Human-Friendly)

### Context

Enterprise administrators need to set organization- or repository-wide defaults for engine model selection and the `max-ai-credits` AWF guardrail without editing every individual workflow's frontmatter. Before this change, model fallback expressions were assembled ad-hoc with `fmt.Sprintf("${{ vars.%s || '%s' }}", ...)` scattered across the engine-specific files (`claude_engine.go`, `codex_engine.go`, `copilot_engine_execution.go`, `compiler_yaml.go`), and `max-ai-credits` was resolved at compile time directly against a hard-coded constant. There was no central enumeration of compiler-managed enterprise variables, and the only override path available to admins was the per-engine `GH_AW_MODEL_AGENT_*` / `GH_AW_MODEL_DETECTION_*` knobs, which still require touching workflow frontmatter to reach a useful default. The team wanted a second-tier "enterprise default" layer plus a single Go-side helper that knows about it for both YAML expression generation and Go-side compile-time resolution.

### Decision

We will introduce a dedicated `pkg/workflow/compilerenv` package as the single source of truth for compiler-managed enterprise environment variables, and add a `GH_AW_DEFAULT_*` override chain between the existing `GH_AW_MODEL_*` primary variables and the built-in engine fallbacks. The package exposes the variable names (`GH_AW_DEFAULT_MAX_AI_CREDITS`, `GH_AW_DEFAULT_MODEL_COPILOT`, `GH_AW_DEFAULT_MODEL_CLAUDE`, `GH_AW_DEFAULT_MODEL_CODEX`), an `EnterpriseVariables()` enumeration, a Go-side `ResolveDefaultMaxAIC` reader for compile-time consumers, and `BuildModelOverrideExpression` / `BuildModelOverrideExpressionEmptyFallback` builders for generated YAML expressions. All call sites that previously built the legacy two-tier expression are migrated to use these builders so the precedence chain is uniformly `frontmatter → GH_AW_MODEL_* → GH_AW_DEFAULT_MODEL_* → built-in fallback`.

### Alternatives Considered

#### Alternative 1: Per-engine inline override chain (no shared package)

Keep the existing pattern of inline `fmt.Sprintf` expressions, and add the `GH_AW_DEFAULT_MODEL_*` term in-line at each call site (Claude, Codex, Copilot, `compiler_yaml.go`, `notify_comment.go`, `awf_config_build.go`). This was rejected because the override chain is a cross-cutting policy: scattering it across N files makes it easy to drift (one site forgetting the default tier), and adding a new enterprise knob would require touching every site again. Centralizing the knowledge in `compilerenv` keeps the override chain consistent and makes future additions a one-file change.

#### Alternative 2: YAML-only enterprise overrides (no Go-side resolver)

Implement the override chain purely as a `vars.*` expression injected into generated workflow YAML, and resolve everything at GitHub Actions runtime. This was rejected because `max-ai-credits` is also consumed at compile time inside the Go binary — `BuildAWFConfigJSON` (`pkg/workflow/awf_config_build.go`) and `buildConclusionJob` (`pkg/workflow/notify_comment.go`) need the numeric value to emit into the AWF config JSON and into the failure-reporting env block. A YAML-only solution would leave those compile-time paths unable to honor the enterprise default, so `ResolveDefaultMaxAIC` (a Go-side `os.Getenv` reader) is required.

#### Alternative 3: Config-file-based enterprise overrides (e.g. `.gh-aw-enterprise.yml`)

Store enterprise defaults in a checked-in or repo-configured YAML file rather than environment variables. This was rejected because GitHub Actions `vars.*` and process env vars give administrators a uniform, well-understood path that already supports org-level, repo-level, and environment-level scoping, with built-in precedence. Adding a new file format would require new storage, parsing, scoping rules, and CI infrastructure for no functional gain.

### Consequences

#### Positive
- Single source of truth (`pkg/workflow/compilerenv`) for compiler-managed enterprise variable names, descriptions, and expression builders.
- Enterprise admins can set org-wide defaults for model selection and `max-ai-credits` via `gh variable set --org ...` without editing any workflow frontmatter or repo files.
- The override chain (`primary → enterprise default → built-in`) is now uniform across Copilot, Claude, and Codex engines and across both YAML-expression and Go-side compile-time consumers.
- Future enterprise knobs can be added by appending one entry to `EnterpriseVariables()` plus the matching builder/resolver — no scatter-edit required.

#### Negative
- Generated workflow YAML expressions become longer and visually noisier (`${{ vars.X || vars.Y || 'z' }}` instead of `${{ vars.X || 'z' }}`), which slightly hurts golden-file readability.
- Adds a new tier to the precedence chain, increasing the number of variables users and admins must understand when debugging "why did this workflow pick model M?".
- All golden test files asserting on the legacy two-tier expression shape had to be regenerated; any out-of-tree consumer that parses the generated env-var expressions will break.

#### Neutral
- New package introduces an import edge from `claude_engine.go`, `codex_engine.go`, `copilot_engine_execution.go`, `compiler_yaml.go`, `compiler_yaml_lookups.go`, `awf_config_build.go`, and `notify_comment.go` into `pkg/workflow/compilerenv`.
- `GH_AW_INFO_MODEL` (run-info metadata) now follows the same override chain as the engine model env vars, so surfaced metadata matches effective model selection.
- The `EngineConfig.GetMaxAIC()` accessor is bypassed at the two compile-time sites that now go through `ResolveDefaultMaxAIC` plus a direct field check on `EngineConfig.MaxAIC`; the accessor still exists for callers that don't need the enterprise default tier.

---

## Part 2 — Normative Specification (RFC 2119)

> The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**, **SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **MAY**, and **OPTIONAL** in this section are to be interpreted as described in [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119).

### Enterprise Variable Registry

1. The package `pkg/workflow/compilerenv` **MUST** be the single source of truth for the names of compiler-managed enterprise environment variables.
2. `compilerenv.EnterpriseVariables()` **MUST** return every compiler-managed enterprise variable currently recognized by the compiler, each with a human-readable description.
3. New compiler-managed enterprise environment variables **MUST** be declared as exported `const` identifiers inside `pkg/workflow/compilerenv` and added to `EnterpriseVariables()` in the same change.
4. Code outside `pkg/workflow/compilerenv` **MUST NOT** hard-code the literal string of an enterprise variable name (e.g. `"GH_AW_DEFAULT_MODEL_COPILOT"`); it **MUST** reference the exported constant from `compilerenv` instead.

### Model Override Chain

1. Generated workflow YAML for engine model environment variables (Copilot `COPILOT_MODEL`, Claude `GH_AW_MODEL_AGENT_CLAUDE` / `GH_AW_MODEL_DETECTION_CLAUDE`, Codex `GH_AW_MODEL_AGENT_CODEX` / `GH_AW_MODEL_DETECTION_CODEX`) **MUST** follow the precedence: workflow frontmatter `engine.model` → `vars.GH_AW_MODEL_*` → `vars.GH_AW_DEFAULT_MODEL_<ENGINE>` → built-in compiler fallback.
2. Generated YAML for these model variables **MUST** be produced by `compilerenv.BuildModelOverrideExpression` when a non-empty built-in fallback exists, or `compilerenv.BuildModelOverrideExpressionEmptyFallback` when the fallback is the empty string.
3. Compiler code **MUST NOT** emit the legacy two-tier expression `${{ vars.<PRIMARY> || '<fallback>' }}` for the engine model env vars listed above.
4. `GH_AW_INFO_MODEL` **MUST** be generated using the same override chain as the corresponding engine model env var so that surfaced run metadata matches effective model selection.

### Max-AI-Credits Override

1. Compile-time consumers of the AWF `apiProxy.maxAIC` default (currently `pkg/workflow/awf_config_build.go` and `pkg/workflow/notify_comment.go`) **MUST** resolve the default through `compilerenv.ResolveDefaultMaxAIC(constants.DefaultMaxAIC)`.
2. When workflow frontmatter sets `max-ai-credits` to a non-zero value, that value **MUST** take precedence over the `GH_AW_DEFAULT_MAX_AI_CREDITS` env var override.
3. When `GH_AW_DEFAULT_MAX_AI_CREDITS` is unset, empty, or not parseable as a base-10 `int64`, the resolver **MUST** return the supplied fallback unchanged.
4. The resolver **MUST NOT** panic, log a fatal error, or fail compilation for an invalid value; it **MUST** fall back silently to the supplied default.

### Package Boundary

1. `pkg/workflow/compilerenv` **MUST NOT** import any other package in `pkg/workflow/...` (it is a leaf utility).
2. `pkg/workflow/compilerenv` **SHOULD NOT** depend on any package outside the Go standard library except where strictly required for variable identifier reuse (none today).
3. New enterprise variable resolvers added to `compilerenv` **SHOULD** follow the same shape as `ResolveDefaultMaxAIC`: read via `os.Getenv`, trim whitespace, parse with explicit error handling, and return the supplied fallback on any failure.

### Documentation

1. The reference page `docs/src/content/docs/reference/compiler-enterprise-environment-controls.md` **MUST** list every variable returned by `compilerenv.EnterpriseVariables()`.
2. The environment variables index (`docs/src/content/docs/reference/environment-variables.md`) **MUST** link to the compiler-enterprise reference page.

### Conformance

An implementation is considered conformant with this ADR if it satisfies all **MUST** and **MUST NOT** requirements above. Failure to meet any **MUST** or **MUST NOT** requirement constitutes non-conformance.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
