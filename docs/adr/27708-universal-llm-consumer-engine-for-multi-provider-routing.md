# ADR-27708: Universal LLM Consumer Engine for Multi-Provider Backend Routing

**Date**: 2026-04-21
**Status**: Draft
**Deciders**: pelikhan

---

## Part 1 — Narrative (Human-Friendly)

### Context

OpenCode and Crush are both "universal" LLM consumer agents: unlike the Copilot engine, they are not tied to a single provider and can route requests to Anthropic, OpenAI/Codex, or Copilot backends depending on the model specified. Prior to this change, each engine contained its own hard-coded secret selection and environment-variable injection logic that defaulted exclusively to Copilot/OpenAI-compatible routing. This prevented true BYOK (Bring Your Own Key) usage at the native provider API and caused duplicated, drift-prone logic across the two engines. The agentic workflow framework needed a way to route OpenCode and Crush directly to the Anthropic or OpenAI native APIs when users specify models from those providers via `engine.model`.

### Decision

We will introduce a `UniversalLLMConsumerEngine` struct that OpenCode and Crush both embed as their base type. This struct owns the shared logic for resolving the LLM backend (Copilot, Anthropic, or Codex/OpenAI) from the `engine.model` provider prefix (e.g., `anthropic/claude-sonnet-4`), and exposes unified methods for secret name derivation, secret validation step generation, and provider environment variable injection. We will also add a compiler validation step that requires `engine.model` to be set in `provider/model` format for all universal consumer engines.

### Alternatives Considered

#### Alternative 1: Keep Per-Engine Secret Logic, Add Provider Switch Inline

Each engine continues to own its own `GetRequiredSecretNames`, `GetSecretValidationStep`, and environment-building methods, with a new `switch` on the provider prefix added to each. This was rejected because it duplicates the provider-resolution logic in both `opencode_engine.go` and `crush_engine.go`, making it easy for them to drift out of sync when a new provider is added.

#### Alternative 2: A Standalone Provider Factory / Registry

A dedicated `LLMProviderRegistry` that maps provider strings to backend profiles and is injected into each engine. This would be more decoupled and unit-testable in isolation, but it introduces indirection (a new abstraction layer, new interface, registration pattern) that is not yet justified by the number of providers or engines. Embedding a `UniversalLLMConsumerEngine` struct keeps the shared logic co-located without a separate registration mechanism.

### Consequences

#### Positive
- Provider-to-backend routing logic is a single source of truth: adding a new supported provider (e.g., Gemini) requires a change in one place (`universal_llm_consumer_engine.go`) rather than two.
- Compile-time validation ensures that workflows using OpenCode or Crush always declare a valid `engine.model` in `provider/model` format, preventing silent misconfiguration.
- Native provider API routing (e.g., `ANTHROPIC_API_KEY` + `ANTHROPIC_BASE_URL`) is now correctly applied without requiring manual `engine.env` overrides.

#### Negative
- OpenCode and Crush are now structurally coupled: a bug or breaking change in `UniversalLLMConsumerEngine` will affect both engines simultaneously.
- The `engine.model` field becomes required for both engines, which is a breaking change for any existing workflow frontmatter that omits it.
- The Copilot authentication path remains in the shared base, meaning the shared logic must be kept aware of Copilot-specific permissions.

#### Neutral
- Compiled workflow lock files (`.lock.yml`) are regenerated to reflect the new secret names and environment variables, which will require safe-update approval gates for secrets like `ANTHROPIC_API_KEY`.
- The `CrushLLMGatewayPort` constant is no longer referenced directly in the Crush engine; gateway port selection is now driven by the backend profile returned from `getUniversalLLMBackendProfile`.

---

## Part 2 — Normative Specification (RFC 2119)

> The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**, **SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **MAY**, and **OPTIONAL** in this section are to be interpreted as described in [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119).

### Universal Consumer Engine Identification

1. An engine that supports multiple LLM providers via a user-supplied `engine.model` field **MUST** embed `UniversalLLMConsumerEngine` as its base struct instead of `BaseEngine` directly.
2. Engines that route exclusively through a single provider (e.g., the Copilot engine) **MUST NOT** embed `UniversalLLMConsumerEngine`.

### Model Field Requirements

1. Compiler validation **MUST** reject workflow frontmatter for universal consumer engines (OpenCode, Crush) when `engine.model` is absent or blank.
2. The `engine.model` value **MUST** use `provider/model` format (e.g., `anthropic/claude-sonnet-4`, `copilot/gpt-5`, `openai/gpt-4.1`).
3. The provider prefix **MUST** be one of the supported values: `copilot`, `anthropic`, `openai`, or `codex`. Any other prefix **MUST** produce a compile-time error.

### Backend Profile Resolution

1. The backend profile (secret names, environment variables, base URL env name, gateway port) **MUST** be derived exclusively from the resolved `UniversalLLMBackend` value and the `permissions.copilot-requests` setting.
2. Implementations **MUST NOT** hard-code provider-specific secret names or environment variables in individual engine files (`opencode_engine.go`, `crush_engine.go`); all such logic **MUST** live in `universal_llm_consumer_engine.go`.
3. When the resolved backend is `anthropic`, the execution environment **MUST** include `ANTHROPIC_API_KEY` and, when the firewall is enabled, **MUST** set `ANTHROPIC_BASE_URL` to the gateway's internal address.
4. When the resolved backend is `codex`/`openai`, the execution environment **MUST** include both `CODEX_API_KEY` and `OPENAI_API_KEY` (falling back to the same secret value), and **MUST** set `OPENAI_BASE_URL` when the firewall is enabled.
5. When the resolved backend is `copilot`, the engine **SHOULD** check `permissions.copilot-requests`; if set to `write`, **MUST** use `${{ github.token }}` and require no additional secret.

### Adding New Providers

1. New provider support **MUST** be added by extending the `switch` statement in `resolveUniversalLLMBackendFromModel` and adding a corresponding case in `getUniversalLLMBackendProfile`.
2. New providers **MUST NOT** be handled by overriding methods in individual engine structs.

### Conformance

An implementation is considered conformant with this ADR if it satisfies all **MUST** and **MUST NOT** requirements above. Specifically: universal consumer engines embed `UniversalLLMConsumerEngine`; compiler validation rejects missing or malformed `engine.model`; all provider-to-backend mapping lives in `universal_llm_consumer_engine.go`. Failure to meet any **MUST** or **MUST NOT** requirement constitutes non-conformance.

---

*This is a DRAFT ADR generated by the [Design Decision Gate](https://github.com/github/gh-aw/actions/runs/24751429896) workflow. The PR author must review, complete, and finalize this document before the PR can merge.*
