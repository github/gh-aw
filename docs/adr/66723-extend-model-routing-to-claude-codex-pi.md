# ADR-66723: Extend Model Routing to Claude, Codex, and pi Engines

**Date**: 2026-10-08
**Status**: Draft
**Deciders**: pelikhan [TODO: verify full decider list]

---

### Context

`engine.model-routing` lets AWF's API proxy pick a Copilot model and reasoning effort once per run, before the agent starts, instead of pinning a fixed model. Until now the compiler rejected routing for every engine except `copilot` (`validateModelRouting` returned a validation error for any other `engine.id`), even though the Claude, Codex, and pi harnesses can all run against GitHub Copilot inference. That restriction meant workflows on those engines could not benefit from cost- or cost-speed-oriented routing, and it forced authors to switch engines purely to get routing. The complication is that the three engines do not share one wire API: Claude Code speaks the native Messages API, Codex speaks the Responses API, and pi can speak either, and each supports a different subset of reasoning-effort levels. This PR addresses #66714.

### Decision

We will allow `engine.model-routing` on the `copilot`, `claude`, `codex`, and `pi` engines whenever the engine resolves to GitHub Copilot inference, and we will enforce API compatibility at two layers rather than one. At compile time, `validateModelRouting` accepts the four engines, rejects non-GitHub providers, and rejects literal `allowed-models` candidates incompatible with the engine's API family (`claude-*` for Claude, `gpt-*` for Codex, any model for Copilot and pi); fixed-model and fixed-effort overrides, plus incompatible sub-agent models, produce warnings instead of errors. At runtime, a new shared `actions/setup/js/awf_model_routing.cjs` module resolves the `/reflect` selection and verifies the selected wire model is advertised by a configured endpoint. For Claude and Codex, AWF's selected endpoint is advisory: complete `/reflect` `routing_models.supported_endpoints` metadata must show that the model supports the engine's API, which becomes the effective endpoint. Pi resolves the API from its model catalog and verifies the corresponding endpoint against the same metadata. The original AWF endpoint is retained for diagnostics. Routed effort maps onto engine-specific settings, and unsupported selections fail closed rather than being clamped or silently substituted. Threat-detection jobs intentionally remain unrouted.

### Alternatives Considered

#### Alternative 1: Compile-time validation only, trusting the router at runtime

The compiler already knows the engine and its candidate list, so one option was to validate `allowed-models` and the provider at compile time and let each harness pass the routed selection straight through to the CLI. This was seriously considered because it avoids the new runtime module and its duplication of compatibility knowledge. It was rejected because `allowed-models` may contain GitHub Actions expressions that the compiler cannot resolve, and because the router can return a model or endpoint the engine cannot actually use; without a runtime check those cases surface as opaque upstream HTTP 400s instead of a clear failure.

#### Alternative 2: Normalize routed effort per engine (clamp or substitute)

Rather than failing when the router returns an effort an engine does not support (for example `none` or `minimal` on Claude), `mapAWFRoutingEffort` could clamp to the nearest supported level. This is attractive because it keeps runs alive and makes routing configuration engine-agnostic. It was rejected (except for the narrow, lossless `none` → `off` mapping on pi) because silently changing the reasoning effort changes both cost and output quality in ways the author never asked for and cannot observe, which undermines the `cost` / `cost-speed` goal the router was asked to optimize.

#### Alternative 3: Keep routing Copilot-only and document the limitation

The status quo — reject routing on other engines and tell authors to use the Copilot engine — required no code change and no new failure modes. It was rejected because the engine choice encodes agent behavior (tooling, harness, sub-agent model), not inference routing, so forcing an engine switch to obtain routing couples two unrelated decisions and blocks routing adoption for existing Claude, Codex, and pi workflows.

### Consequences

#### Positive

- Claude, Codex, and pi workflows on GitHub Copilot inference can use `engine.model-routing` without changing engines.
- Incompatible literal candidates (for example a `gpt-*` model under Claude) are caught at compile time with an actionable validation error instead of failing mid-run.
- Runtime verification of the selected wire model and engine-compatible endpoint turns previously opaque upstream 400s into explicit, readable failures, and non-Copilot harnesses log the effective endpoint and any differing AWF selection.
- Effort semantics stay faithful: an unsupported routed effort fails rather than quietly degrading or inflating cost.

#### Negative

- API-compatibility knowledge is now duplicated in two places — `modelRoutingEngineSupportsModel` in Go and `mapAWFRoutingEffort` / `resolveAWFModelRoutingSelection` in JavaScript — so new engines or model families must be updated in both or they will drift.
- AWF still ranks model-and-effort arms without an engine-specific effort filter. Fail-closed effort mapping therefore makes routed Claude and Codex runs abort when the router picks an effort outside their supported set; resolving this requires an AWF configuration capability so unsupported arms can be removed before ranking.
- The model-family prefix checks (`claude-`, `gpt-`) are heuristics over model IDs; a future Copilot model that does not follow these naming conventions will be misclassified.
- The compiler can say nothing about expression-valued `allowed-models`, so those configurations get runtime-only protection and a weaker authoring experience.

#### Neutral

- Fixed `engine.model`, `MODEL`-bearing `engine.env` entries, and pi's `settings.defaultThinkingLevel` are suppressed under routing and only produce warnings, so existing configs keep compiling but behave differently.
- Threat detection continues to use its own configured engine and model; the previously documented "threat detection fails under routing" limitation is replaced by an explicit "threat detection is not routed" statement.
- Candidate normalization now also strips a `copilot/` prefix in addition to `github-copilot/`, widening accepted spellings of the same model ID.
- Routing still requires the AWF firewall for all four engines; this change does not relax that requirement.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
