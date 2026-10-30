# ADR-73204: Factory Droid Declarative Engine with Proxied Inference

**Date**: 2026-10-30
**Status**: Proposed

---

### Context

Factory Droid can run unattended tasks and connect to native MCP servers, but its normal Factory-managed inference requires a reusable credential. Exposing Factory or upstream provider credentials to an agent that can execute commands would allow those credentials to be read by agent tools. The existing declarative engine infrastructure can install Droid, adapt gateway configuration, and invoke a harness without a dedicated Go runtime. The integration must preserve the AWF sandbox and proxy as the outer credential and network boundary.

### Decision

Use the experimental declarative engine in [shared/factory.md](../../.github/workflows/shared/factory.md), rather than implement a dedicated Factory Go runtime. Require AWF-proxied inference through Droid custom models, selecting an exact `copilot/`, `anthropic/`, `openai/`, or `codex/` model ID. Discover the corresponding endpoint through AWF `/reflect` and fail closed when the proxy or matching provider is unavailable; do not fall back to Factory-managed inference or direct provider credentials.

Droid receives an `awf-proxy` placeholder API key in owner-only, per-run custom-model settings, not reusable upstream keys. Disable cloud session sync and remove those settings after execution. Exclude Factory, provider, and ambient GitHub credentials at the AWF environment boundary; filter the Droid child environment as defense in depth against accidental inheritance, not as a replacement for AWF isolation. Sentinel regression tests exercise credential exclusion separately from the outer boundary.

Retain native MCP gateway authorization headers intentionally: the gateway requires this agent-readable session identifier, which is not an upstream provider credential or a security boundary against the agent. Generate an owner-only `.factory/mcp.json`, disable OAuth, and omit servers mounted as CLIs. Keep text output in agent logs without claiming a structured Factory session parser or normalized tool events, token usage, refusals, or reasoning.

### Alternatives Considered

#### Alternative 1: Dedicated Go engine

A dedicated engine could provide Factory-specific runtime handling and richer session normalization. It is not selected because the existing declarative installation, execution, MCP adapter, and harness contracts cover this experiment without another runtime implementation. Reconsider if proven requirements exceed those contracts.

#### Alternative 2: Factory-managed inference using `FACTORY_API_KEY`

Factory-managed inference would use Droid's normal account-based model access and avoid custom-model configuration. It is rejected because supplying a reusable Factory key to Droid also exposes it to agent tools, contrary to the credential boundary.

#### Alternative 3: Direct provider credentials

Passing Anthropic, OpenAI, Codex, or Copilot credentials directly would simplify endpoint setup and permit execution without AWF. It is rejected because reusable provider credentials would enter the agent environment and inference would bypass AWF's proxy boundary.

### Consequences

#### Positive

- Factory support reuses the declarative engine infrastructure without a dedicated Go runtime.
- Proxy-owned inference credentials remain outside the agent; environment filtering and sentinel regressions add defense in depth.

#### Negative

- Execution requires AWF and a configured matching provider; Factory-managed inference and unsandboxed direct-key execution are unsupported.
- Text logs do not provide structured session accounting, and a successful Droid exit does not independently prove task completion.

#### Neutral

- Native MCP retains agent-readable gateway session authorization, so workspace configuration is not entirely credential-free.
- The engine remains experimental and does not advertise gh-aw tool allowlists, max-turn enforcement, or Agent Plugins.

---

This record documents the implementation decision in [PR #73204](https://github.com/github/gh-aw/pull/73204); maintainer acceptance remains pending.
