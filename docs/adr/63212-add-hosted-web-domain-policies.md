# ADR-63212: Add hosted web domain policies

**Date**: 2026-09-24
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

Provider-hosted Claude and Codex web tools run outside AWF's network boundary, so the existing `network.allowed` workflow setting cannot limit where those hosted tools retrieve content. The PR description and diff show a new need for an explicit deny-by-default control surface that is compiled into AWF's trusted API proxy configuration instead of being conflated with sandbox egress policy. The change spans workflow frontmatter, validation rules, schema updates, generated workflow lock files, and documentation, which makes it an architectural policy change rather than a local implementation detail. The decision is how AWF should express and enforce retrieval-domain restrictions for hosted web capabilities provided by supported engines.

### Decision

We will add a dedicated `network.hosted-web` frontmatter policy for provider-hosted web access and compile it into `apiProxy.hostedWeb` engine-specific configuration for Claude and Codex. The object form enables hosted web by its presence, while `hosted-web: false` disables it. The policy will be deny-by-default when a workflow declares network restrictions for those engines without an explicit hosted-web policy, and it will validate only lowercase DNS hostnames plus optional usage limits rather than reusing `network.allowed`. We chose this because the PR evidence shows hosted web retrieval happens outside the sandbox network boundary, so it requires a separate trusted-proxy policy surface with independent domain allow/block lists.

### Alternatives Considered

#### Alternative 1: Reuse `network.allowed` for hosted web restrictions

This was a realistic option because `network.allowed` already defines outbound network policy in workflow frontmatter and would avoid adding another policy block. It was not chosen because the PR description explicitly states that provider-hosted web tools execute outside AWF's network boundary, so `network.allowed` cannot reliably constrain those retrieval destinations and would create a misleading security contract.

#### Alternative 2: Allow hosted web access without explicit per-workflow policy

This was viable because AWF could rely on engine defaults or global proxy behavior and avoid extra frontmatter and schema complexity. It was not chosen because the diff and PR description both emphasize deny-by-default behavior and explicit allow/block lists, which provide a clearer trust boundary and prevent silent expansion of hosted web reachability when workflows opt into network restrictions.

### Consequences

#### Positive
- AWF gains an explicit, documented policy surface for hosted web access that matches the real enforcement boundary of provider-hosted retrieval.
- Claude and Codex workflows can declare separate hosted-web allow/block lists and optional usage caps without weakening or overloading sandbox `network.allowed` semantics.
- Deny-by-default compilation when restrictions are declared without hosted-web policy reduces accidental exposure and makes the security model more predictable.

#### Negative
- Frontmatter, schema validation, and config compilation become more complex because hosted web policy is a second network-related control surface with engine-specific handling.
- Users must understand the distinction between sandbox egress controls and hosted web proxy controls, which increases documentation and onboarding burden.
- Generated workflow outputs and proxy configuration must stay synchronized with the new policy shape, increasing maintenance cost across compilation paths and tests.

#### Neutral
- Hosted-web domain lists remain independent from `network.allowed`, so workflows may now need to manage two related but distinct policy sections.
- Unsupported engines and malformed domains are rejected earlier during validation rather than being deferred to runtime behavior.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
