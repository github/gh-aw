# ADR-73052: Native Anthropic and OpenAI Model Routing for Claude and Codex

**Date**: 2026-10-29
**Status**: Proposed
**Deciders**: the gh-aw maintainers

---

### Context

`engine.model-routing` lets AWF's router pick a model and reasoning effort per run. Until now gh-aw accepted routing only on GitHub Copilot inference. A Claude or Codex workflow had to set `engine.model-provider: github` to route, which moved its inference credential and bill from the workflow's own Anthropic or OpenAI key to Copilot.

AWF v0.28.28 and later (including gh-aw's default v0.29.0) support provider-scoped routing through `apiProxy.routing.provider: copilot | openai | anthropic`. Native routing discovers models with the provider's own key and runs the classifier and the selected model through that provider's authenticated adapter. It uses only the provider's native HTTPS endpoint, and it never switches credentials or translates between providers. gh-aw emitted no routing provider, so AWF defaulted to Copilot, and every candidate was qualified with `github-copilot/` (issue #73051).

### Decision

gh-aw resolves one **effective routing provider** per workflow and uses it consistently in validation, AWF config, credentials, and harness startup:

- Copilot and pi: `github` only, unchanged.
- Claude: `github`, or native `anthropic` when `model-provider` is omitted or `anthropic`.
- Codex: `github`, or native `openai` when `model-provider` is omitted or `openai`.

For native routing only, the compiler:

- emits `apiProxy.routing.provider`. Copilot routing still omits the field, so existing locks stay byte-identical.
- emits bare native candidates: `claude-*` or `gpt-*` by default, exact IDs unchanged (dash-form Anthropic IDs such as `claude-sonnet-5-5`), and aliases expanded from the native namespace of the shared alias table, limited to the engine family.
- applies policy rules for the native provider, plus bare rules. A `github-copilot/` rule never authorizes a native model.
- rejects cross-provider combinations (Claude on OpenAI, Codex on Anthropic). It also rejects targets that move inference off the native HTTPS endpoint: a custom `engine.api-target`, a custom `ANTHROPIC_BASE_URL` or `OPENAI_BASE_URL`, a `sandbox.agent.targets` gateway, or `engine.auth` for another cloud such as Azure.

The shared harness check in `awf_model_routing.cjs` is now provider-aware. The selected provider must equal the engine's provider, and a configured `/reflect` endpoint for that provider must list the wire model with complete routing metadata. The endpoint must be compatible and the effort supported; native protocol names map to paths. A missing native endpoint fails closed; there is no fallback to another provider that lists the same model. Claude and Codex reuse their existing base-URL resolution and effort handling. Copilot's endpoint-override path is unchanged.

### Alternatives Considered

#### Alternative 1: Keep Copilot-only routing

Claude and Codex workflows that want routing would continue to set `model-provider: github`. This needs no new code, but it forces a change of inference credential and bill to enable a feature that AWF already supports natively. It was rejected.

#### Alternative 2: Relax only the provider check

Accepting `anthropic` and `openai` in the existing validator without emitting `routing.provider`, or without validating the target, would let AWF default to Copilot routing on a native-key workflow. It would also let routing silently run through custom gateways that AWF native routing doesn't support. It was rejected because the effective target, not the provider label, decides where inference goes.

#### Alternative 3: Derive native candidates by stripping `github-copilot/`

This would produce Copilot model names, such as dot-form `claude-sonnet-5.5`, that don't exist on Anthropic. It would also apply Copilot-only overrides to native requests. It was rejected in favor of expanding aliases from the native namespace.

### Consequences

#### Positive

- Claude and Codex can route while keeping their provider API key and provider billing; the key stays in AWF's API-proxy sidecar.
- Copilot routing and all existing compiled locks are unchanged.
- Cross-provider fallback is impossible both at compile time and in the harness.

#### Negative

- Native candidates must have AWF-maintained routing metadata and router benchmark data. Today that excludes, for example, the Haiku models and `gpt-5.4-mini`, so some natural shortlists route nowhere.
- Native routing can't be combined with custom gateways, BYOK endpoints, or Azure OpenAI.
- Smoke coverage needs native provider keys, and native-specific evidence checks (N1–N3) add to the smoke assertion helper.

#### Neutral

- The unified-session `model.config` event and `aw_info.model_config` record `routing.provider` for native routes only.
