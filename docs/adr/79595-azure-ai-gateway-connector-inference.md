# ADR-79595: Model Azure AI Gateway as Connector Inference Profiles

**Date**: 2026-11-22
**Status**: Draft
**Deciders**: Unknown (draft generated from PR #79595; PR author pelikhan to confirm)

---

### Context

Workflows run on a fixed set of runtime engines, and the Copilot engine can already be pointed at an OpenAI-compatible endpoint through `COPILOT_PROVIDER_*` variables in `engine.env`. Teams that route model traffic through Azure AI Gateway (Responses and Chat Completions profiles, authenticated with Entra federation or a gateway key) have had to hand-write those variables and secrets. Nothing in that path checks that the endpoint is approved, that the requested model is granted, or that the gateway key stays out of reader and effect plans. Adding a new runtime engine per gateway would widen the engine matrix and duplicate authentication and network-approval logic that the connector layer already owns. Non-negotiable constraints: inference credentials must never reach readers or effects, importing a connector must not change engine routing, and request bodies must not be author-defined templates or scripts.

### Decision

We will model Azure AI Gateway as a connector-declared inference profile. A profile names a fixed origin, a fixed absolute SDK base path, a protocol (`openai-responses` or `openai-chat-completions`), an inference-only bearer credential, and an exact model resource collection. A workflow opts in with `engine.inference: {connector, profile, model}`, and the compiler rejects unknown profiles, ungranted or dynamic wire aliases, and native-versus-imported conflicts before any credential is referenced. The primary driver is keeping the `copilot` engine as the only runtime while making credential isolation and model grants enforceable at compile time.

### Alternatives Considered

#### Alternative 1: Add a dedicated `azure-ai-gateway` runtime engine

This would give users one engine identifier that handles the gateway URL, authentication, and protocol internally. It was considered because it is the simplest thing for users to discover. It was not chosen because it adds a fourth runtime to the engine matrix, duplicates endpoint, credential, and approval handling that connectors already provide, and the PR goal explicitly excludes a new runtime engine. This was a close call on discoverability, and the trade-off would be worth revisiting if inference profiles grow beyond Copilot.

#### Alternative 2: Keep configuring BYOK through `engine.env`

The existing pattern works today with no compiler change: set `COPILOT_PROVIDER_BASE_URL`, `COPILOT_PROVIDER_WIRE_API`, and a gateway-key secret directly. It was considered as the zero-new-surface option. It was not chosen as the primary path because any `engine.env` value can name any host, nothing checks the requested model against a grant, and nothing prevents the key from reaching readers or effects. The PR keeps this path as a documented direct option, so this is a partial rejection rather than a removal.

#### Alternative 3: Allow connector-defined request templates or inference scripts

This would support future providers and protocols without new compiler work, including model discovery and arbitrary request bodies. It was rejected outright. The connector specification forbids request templates, scripts, model discovery, and inference-as-reader POSTs, because they would turn connector declarations into an executable request surface and widen exfiltration risk. This was not a close call.

### Consequences

#### Positive
- Unknown profiles, ungranted or dynamic model aliases, and native/imported override conflicts fail at compile time, before any credential is referenced.
- Inference credentials are excluded from reader and effect plans, so a tool cannot reach the gateway key by declaring a reader.
- Importing a connector selects nothing by itself. Only an explicit `engine.inference` selection changes routing.
- A bounded TLA+ model checks selection, phase separation, and terminal authentication denial, including a witness that staged writes do not disable inference.

#### Negative
- The feature is experimental and supports Copilot only. Other engines get no inference-profile support until they are designed separately.
- It adds specification surface (connector specification 1.2.0 with C-INF-001 and C-INF-002 conformance procedures, plus new parser, compiler, and connector modules) that maintainers must keep in sync.
- Compilation and the symbolic model do not establish Entra trust, provider authorization, protocol compatibility at runtime, pricing, or output quality. [TODO: verify] Azure Entra trust and asset RBAC have not been reviewed against a real deployment.
- Azure HTTP MCP federation requires MCP gateway v0.5.12 or newer, `strict: false`, and a reviewed custom gateway build. It depends on upstream gh-aw-mcpg#16956.

#### Neutral
- Model grants bind the compiled wire alias, not the provider-side authority of the gateway key. A key with broader provider rights than the grant is still broader than the grant.
- Entra federation for HTTP MCP servers is a separate, preparatory decision in the same PR. It keeps the existing direct GitHub OIDC behavior and can be revisited independently of the inference profile decision.
- Validation is offline: setup guidance, a source-only golden fixture (`pkg/cli/workflows/azure-ai-gateway-golden.md`), and compile-time checks. Executing even a staged consumer sends prompts to the gateway and incurs model charges.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
