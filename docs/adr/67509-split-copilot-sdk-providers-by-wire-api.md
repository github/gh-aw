# ADR-67509: Split Copilot SDK Providers by Wire API and Qualify Every Routed Model

**Date**: 2026-10-10
**Status**: Draft
**Deciders**: SivaKesava1 (PR author); gh-aw maintainers (review pending)

---

### Context

The Copilot SDK engine configured a single provider per Copilot endpoint and passed bare model names
(for example `gpt-5.6-luna`) to routed SDK sessions. Two problems followed. First, bare model names
bypassed BYOK inference routing under organization billing, so spend attribution and proxy routing
were wrong. Second, one provider carries one wire API, but Copilot exposes both the Responses API
(`/responses`) and Chat Completions (`/chat/completions`) and different model families support
different ones — so a GPT main agent and a Claude sub-agent could not coexist in the same session.
Separately, declared sub-agents (inline and repository agent files) were parsed but never handed to
the SDK, so delegation silently ran on the main agent's model. Smoke coverage asserted on model-name
replies, which a model can hallucinate, so none of this was reliably caught.

### Decision

We will split each Copilot endpoint into two sibling SDK providers, `copilot-responses` and
`copilot-completions`, derived from `/reflect` endpoint capabilities, and route each model to the
provider matching its wire API (`inferWireApiForModel`, with explicit `wire_api`/`wireApi` from the
model catalog winning over the GPT-5+ `responses` heuristic). Every model identifier that reaches the
SDK — routed, fallback, reflected, and sub-agent — is qualified as `<provider>/<model>` so BYOK
inference is used; explicitly configured API selections are preserved rather than overridden.
Declared agents are loaded as SDK `customAgents` carrying name, description, prompt, and tools, with
concrete models qualified and aliases inheriting the session model (unknown agent models warn, then
inherit). Non-Copilot endpoints and CLI mode are deliberately left unchanged. Observability captures
`subagent.selected` and `model.call_final_result` including BYOK status, and `pkg/cli` normalizes
qualified identities (`copilot-(responses|completions)` stripped) so audit spend attribution still
groups by real model. Smoke tests now require evidence of completed delegation on the expected model
and proxy endpoint instead of model-name replies.

### Alternatives Considered

#### Alternative 1: Keep one provider per endpoint and pick a single wire API

Continue with a provider-wide wire API, choosing `responses` or `completions` per run. This is the
smallest change and keeps the provider list short. Rejected because it structurally prevents
cross-family sessions: a Claude sub-agent delegated from a GPT main agent needs a different wire API
in the same session, which is the primary defect this PR fixes.

#### Alternative 2: Qualify models only at the top-level session, not for sub-agents and fallbacks

Qualify the main session model and leave sub-agent, fallback, and reflected models bare, relying on
server-side defaults. Tempting because it touches far fewer call sites. Rejected because the bare
paths are exactly where BYOK routing was being bypassed; partial qualification would leave spend
attribution wrong for delegated work and make failures intermittent and hard to diagnose.

#### Alternative 3: Resolve wire API dynamically per request instead of per provider

Let the driver choose the HTTP path at call time rather than modelling two providers. Rejected
because the SDK's provider abstraction owns `wireApi`; per-request override would fork the SDK's
transport handling and lose the SDK-native `customAgents` routing we rely on.

### Consequences

#### Positive

- Cross-family sessions work: a GPT main agent on `/responses` can delegate to a Claude sub-agent on
  `/chat/completions` within one session.
- Routed, fallback, reflected, and sub-agent calls all use BYOK inference, so org-billed runs route
  through the intended proxy and attribute spend correctly.
- Declared inline and repository sub-agents are actually honored by the SDK, with their prompts and
  tool sets intact.
- Smoke and audit assertions are grounded in delegation/proxy evidence rather than model self-reports,
  which a model can fake.

#### Negative

- The provider list roughly doubles for every Copilot endpoint, making reflected configuration and
  debug output noisier and harder to read.
- Wire-API inference adds a heuristic (`GPT-5+ → responses`) that will need maintenance as new model
  families ship; a wrong inference produces a routing failure rather than a graceful degradation.
- Qualified identifiers (`copilot-responses/gpt-5.6-luna`) leak into logs, session records, and
  fixtures, requiring normalization in `pkg/cli` and in any downstream consumer that matched on bare
  model names.
- Behaviour now diverges between SDK mode and CLI mode, which stays on the old path — two code paths
  to reason about until CLI mode is migrated.

#### Neutral

- Non-Copilot endpoints (including Anthropic-typed providers, where `wireApi` is ignored) are
  unaffected by this change.
- Unknown sub-agent models warn and inherit the session model rather than failing the run, trading
  strictness for resilience.
- `docs/src/content/docs/reference/engines.md` documents the new provider naming, so the qualified
  form is now part of the user-facing contract.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
