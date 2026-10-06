# ADR-66289: Route Claude Engine Inference Through GitHub Copilot via Model Prefix

**Date**: 2026-04-27
**Status**: Draft
**Deciders**: pelikhan (PR #66289 author), gh-aw maintainers

---

### Context

The Codex engine already resolved a GitHub-hosted inference provider from a `copilot/` model prefix, but the Claude engine did not: `ClaudeEngine.ResolveLLMProvider` always defaulted to Anthropic unless `engine.provider` was set explicitly. Worse, workflows that *did* set the GitHub provider explicitly had the Copilot credential injected as `ANTHROPIC_API_KEY`, which caused AWF to provision an Anthropic proxy target instead of the Copilot adapter — the wrong credential reaching the wrong proxy. Users asking for `engine: claude` with `model: copilot/claude-haiku-4.5` and `permissions: copilot-requests: write` therefore could not run Claude Code against Copilot-hosted Anthropic models without an Anthropic API key. The fix had to preserve explicit provider overrides, keep raw credentials out of the agent container, and fail closed rather than silently falling back to direct Anthropic access.

### Decision

We will infer the GitHub (Copilot) LLM provider for the Claude engine from the `copilot/` model prefix, using a shared helper `resolveEngineLLMProviderFromModel` that both Claude and Codex call so prefix-vs-explicit-override precedence lives in exactly one place. When that provider is active, we will pass the GitHub credential only as `COPILOT_GITHUB_TOKEN` for AWF's Copilot adapter, export a placeholder `ANTHROPIC_API_KEY` **inside** the sandbox so the Anthropic provider is never provisioned, strip the `copilot/` prefix before handing the model ID to the Claude CLI, and require the agent sandbox plus a matching Copilot endpoint from AWF `/reflect` — erroring out when either is missing. A manually dispatched canary workflow (`smoke-claude-copilot`) will exercise native Messages streaming and an MCP tool round-trip, with model fallback disabled so unsupported CAPI requests cannot pass silently.

### Alternatives Considered

#### Alternative 1: Require an explicit `engine.provider: github` for Claude Copilot routing

Keep `ResolveLLMProvider` returning Anthropic by default and make users set the provider explicitly alongside the model. This is the smallest change and is maximally unambiguous, but it diverges from the Codex engine's established `copilot/` prefix behaviour, forcing users to learn two different configuration idioms for the same intent. It was rejected for consistency; the explicit override is still honoured and still takes precedence over the prefix.

#### Alternative 2: Continue supplying the Copilot token as `ANTHROPIC_API_KEY`

The pre-existing behaviour reused the Anthropic credential slot for the Copilot token, which requires no new env var plumbing. It was rejected because AWF selects its proxy target from the credential shape: an Anthropic-looking key provisions an Anthropic provider, so requests would be routed to `api.anthropic.com` with a Copilot token, failing opaquely and leaking a GitHub credential to the wrong endpoint. Credential isolation (`COPILOT_GITHUB_TOKEN` + in-sandbox placeholder) was judged the primary driver.

#### Alternative 3: Allow Copilot inference without the agent sandbox

Permitting sandbox-less runs would broaden where the feature works. It was rejected because the routing depends on AWF `/reflect` endpoint resolution; without the sandbox there is no proxy to reflect, and the harness would fall back to direct Anthropic calls with a placeholder key. `validateSandboxConfig` now rejects this combination at compile time instead.

### Consequences

#### Positive
- `engine: claude` with `model: copilot/...` works with Actions-token (`permissions: copilot-requests: write`) or PAT auth, with no Anthropic API key required.
- Provider-prefix resolution is shared between Claude and Codex, removing duplicated precedence logic and reducing drift between engines.
- Credentials are isolated: the real GitHub token never appears as `ANTHROPIC_API_KEY`, and AWF excludes secret env vars from the container.
- Failure modes are explicit: missing Copilot endpoint, missing sandbox, or an unexpected dynamic model now raise actionable errors instead of silently routing elsewhere.

#### Negative
- Claude + Copilot is hard-coupled to the agent sandbox and AWF `/reflect`; disabling the sandbox is now a compile-time error for these workflows.
- The placeholder-`ANTHROPIC_API_KEY` export is a non-obvious indirection that future maintainers can easily break when touching Claude command assembly or threat-detection path setup.
- Live CAPI inference has not been exercised in this PR; account/model access, beta headers, endpoint shape, and request-schema compatibility remain unverified until the canary is dispatched.
- Regenerated lock files and a new 1.5k-line smoke lock increase review surface and future merge-conflict likelihood.

#### Neutral
- Behaviour changes for existing explicit GitHub-provider Claude workflows (env var names change), so their lock files were regenerated.
- `normalizeClaudeModel` is enforced in the JS harness as well as the compiler, duplicating the prefix rule across Go and Node by design (defence in depth for dynamically selected models via `GH_AW_LLM_PROVIDER_EXPLICIT`).
- Inline and external threat-detection jobs inherit the same credential export path, keeping detection consistent with the main agent step.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
