# ADR-73578: Isolate Universal-Consumer Copilot Credentials

**Date**: 2026-10-31
**Status**: Draft
**Deciders**: gh-aw maintainers and compiler/runtime maintainers

---

### Context

The `universal-llm-consumer` engine supports multiple providers while allowing
OpenAI-compatible clients to use a shared routing profile. Before this change,
a Copilot credential could be exposed both as `COPILOT_GITHUB_TOKEN` and as
`OPENAI_API_KEY`, allowing an unintended upstream adapter to interpret the
credential during AWF startup even when the gateway URL selected Copilot. The
compiler must support both secret-based Copilot authentication and
`copilot-requests: write`, while preserving direct and harness execution,
protected session capture, sandbox-disabled behavior, and other providers.
Existing compiled workflow locks also need a compatible migration path.

### Decision

We will keep universal-consumer Copilot credentials provider-scoped: the real
credential is emitted only as `COPILOT_GITHUB_TOKEN`, while the Copilot gateway
continues to be exposed through its `OPENAI_BASE_URL` alias. When AWF is
enabled, the compiler will carry a non-secret `COPILOT_DUMMY_BYOK` sentinel into
the sandbox and export it as `OPENAI_API_KEY` only in the child engine command,
after AWF credential discovery; it will not place that key in the runner step,
and it will not apply the sandbox-only placeholder to disabled or non-universal
execution. Existing locks must be recompiled with the fixed compiler.

### Alternatives Considered

#### Alternative 1: Continue aliasing the Copilot credential to OpenAI/Codex variables

This preserves compatibility with clients that inspect `OPENAI_API_KEY` during
startup and requires fewer generated-command changes. It was rejected because
a credential-shaped startup variable can enable an unintended upstream adapter,
and provider destination allowlists do not establish the credential's audience.

#### Alternative 2: Remove the OpenAI-compatible key entirely

Clients would be forced to understand `COPILOT_GITHUB_TOKEN` or a Copilot-only
credential contract. This was rejected because universal-consumer execution
intentionally supports OpenAI-compatible clients and would break those clients
without preserving their existing command interfaces.

#### Alternative 3: Put a placeholder in the runner environment before AWF starts

A non-secret placeholder could avoid exposing the real token while retaining an
OpenAI-shaped variable. It was rejected because the placeholder would still be
visible during AWF startup, which is the boundary at which credential discovery
and adapter selection must remain separate from client configuration.

### Consequences

#### Positive
- Copilot credentials cannot be mistaken for OpenAI or Codex credentials by an
  upstream adapter during AWF startup.
- OpenAI-compatible clients continue to receive the expected key shape after
  discovery, while the Copilot gateway alias and both Copilot authentication
  modes remain supported.
- Generated YAML, simulated startup/client environments, direct and harness
  execution, and protected capture have explicit regression coverage.

#### Negative
- Deployed workflows with existing lock files must be recompiled; previous
  runs and uploaded artifacts are not repaired retroactively.
- Credential placement now depends on the AWF boundary and generated shell
  command ordering, increasing compiler and maintenance complexity.
- The simulated startup test does not provide end-to-end AWF or live-provider
  verification.

#### Neutral
- Anthropic and OpenAI/Codex profiles, non-universal execution, and
  sandbox-disabled execution retain their existing credential behavior.
- The change adds no new frontmatter setting, permission grant, or lock-file
  schema; it documents the routing contract under CTR-017.
- Explicit author-owned credential overrides and upstream partial-key redaction
  remain separate boundaries outside this decision.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
