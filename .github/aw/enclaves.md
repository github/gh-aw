---
description: Private-repository enclaves (preview) — finite-disclosure access to approved private repos via the MCP gateway.
---

# Private Repository Enclaves

Use these instructions when a workflow needs bounded, auditable access to a private repository other than the one the workflow runs in.

## What it is

- The top-level `enclaves:` array (1-2 entries) enables finite-disclosure access to approved private repositories through the compiler-launched MCP gateway.
- Each entry is either a **script enclave** (`script:` + `repos:`) registering `enclave_run_script`, or an **agent enclave** (`agent:` + static `repos:` or dynamic `dynamic:`) registering `enclave_run_agent`.
- Omit `enclaves:` entirely to disable the feature — this is the default.
- This is a preview feature gated on `github/gh-aw-firewall#6992`; an older pinned AWF version will not provide the enclave server.

## Prerequisites

- Enclaves require AWF network isolation, which every supported `sandbox.agent.runtime` profile provides, so the compiler launches the MCP gateway in bridge mode and AWF can attach it to the isolated topology.
- Each `repos:` entry needs `repo:` (`owner/name`) and `sensitivity:` (`public`, `trusted`, `internal`, `confidential`, or `sealed`).
- Choose `trusted` only for repositories whose content is approved for unrestricted return to the primary agent without confidentiality accounting, and where the enclave may return free-form strings in a declared response schema. Do not select it merely to obtain string output. All other sensitivities are finite-schema-only; do not recommend free-form string schemas for them.

## Example

```yaml
sandbox:
  agent:
    id: awf
enclaves:
  - script:
    repos:
      - repo: octo-org/private-service
        sensitivity: confidential
    timeout: 45
  - agent:
      model: gpt-5
    repos:
      - repo: octo-org/private-service
        sensitivity: confidential
    timeout: 180
```

## Rules

- Each enclave type (`script`, `agent`) can appear at most once.
- If the same repository appears in both entries, its `sensitivity` must match — the information budget is shared across executor types.
- AWF fixes the script enclave's network and interpreter, and the agent enclave's network, internally; do not attempt to override these in workflow frontmatter.
- A fresh masked capability is generated per workflow run and passed only to the MCP gateway and AWF, never to the primary agent environment.
- `timeout:` per enclave entry is capped at 4,740 seconds (AWF reserves the final 60 seconds of its 4,800-second finite-disclosure bucket for cleanup). The gateway itself enforces a 4,860-second tool timeout (4,800s AWF bucket + 60s transport allowance) — treat this as an enforcement bound, not a wall-clock guarantee.

## Agent GitHub tool configuration

Prefer this configuration shape for new workflows:

```yaml
sandbox:
  mcp:
    version: v0.4.17
enclaves:
  - agent:
      model: gpt-5
      tools:
        github:
          allowed: [list_issues, issue_read]
          allowed-repos: [octo-org/private-service]
          min-integrity: none
    repos:
      - repo: octo-org/private-service
        sensitivity: confidential
```

- `allowed` is required and currently supports only `list_issues` and `issue_read`.
- `allowed-repos` is optional. When omitted, the enclave identity inherits all repositories declared in the enclave's `repos:` list. When set, each entry must also appear in that list.
- `min-integrity` is optional and defaults to `approved`.
- Unsupported tools and out-of-scope repositories fail closed at compile time.
- GraphQL, search, writes, and all other GitHub tools remain denied.
- Minimum versions are AWF `v0.28.9` and mcpg `v0.4.15`; trusted repositories additionally require AWF `v0.28.14`.

## Dynamic agent repository policies

Use `dynamic:` on agent entries when the primary agent should select one admitted repository at invocation time without enumerating every repository in frontmatter:

```yaml
sandbox:
  agent:
    id: awf
    version: v0.28.14
  mcp:
    version: v0.4.19
enclaves:
  - agent:
      model: gpt-5
      max-task-bytes: 4096
      max-model-requests: 8
      max-model-tokens: 1024
    dynamic:
      allowed-owners: [octo-org]
      sensitivity: confidential
      github-policy: github-repository-read-v1
      max-repositories: 4
      quotas:
        max-invocations: 8
        max-output-bytes: 32768
        max-execution-seconds: 900
      audit-labels: [dynamic-enclave]
      expires-at: "2026-09-06T00:32:00Z"
    timeout: 120
    memory-limit: 512m
    cpu-limit: "1"
    pids-limit: 128
    tmpfs-limit: 64m
    max-output-bytes: 8192
    max-invocations: 8
```

- Dynamic mode is agent-only; scripts remain static seed-backed and must declare `repos`.
- Each entry declares either non-empty static `repos` or `dynamic`, never both.
- Declare `allowed-owners` or `allowed-repositories` using the ADR 0001 canonical lowercase ASCII selector form. The compiler does not trim, case-fold, URL-decode, or otherwise normalize dynamic selectors.
- `github-policy` must be `github-repository-read-v1`, the closed policy containing only `list_issues` and `issue_read`.
- Dynamic entries require fixed sensitivity, finite resource limits, total quotas, audit labels, an absolute `expires-at` timestamp, AWF `v0.28.14` or newer, and mcpg `v0.4.19` or newer. `expires-at` is an upper bound; the compiler resolves the effective envelope expiry at workflow setup time as `min(expires-at, job-start + workflow timeout)`. Each delegated identity remains independently bounded by its configured `max_identity_ttl`.
- The compiler emits the dynamic policy envelope and starts mcpg's `github-repository-delegation-v1` controller, then hands AWF a host-private delegation control endpoint. The delegation-control capability is AWF-only and is excluded from primary and enclave agent environments.

## Deprecated legacy profile

The legacy profile remains supported during migration:

```yaml
enclaves:
  - agent:
      model: gpt-5
      github:
        cli: issues-read-v1
    repos:
      - repo: octo-org/private-service
        sensitivity: confidential
```

- `enclaves[].agent.github.cli: issues-read-v1` is deprecated. Migrate to `enclaves[].agent.tools.github`.
- `issues-read-v1` permits only the `list_issues` and `issue_read` GitHub MCP
  tools. GraphQL, search, writes, and all other GitHub tools fail closed.
- V1 allows at most one repository whose sensitivity is neither `public` nor `trusted` in the agent entry; `trusted` is public-equivalent for this limit.
- `trusted` repositories are public-equivalent for this limit, so multiple `trusted` and `public` repositories are allowed.
- The compiler generates separate primary and enclave identities for one shared
  mcpg gateway. The enclave identity is restricted to the GitHub server, those
  two tools, and the union of repositories declared in its trusted entry.
- AWF privately stages the enclave identity and connects the enclave directly
  to `/mcp/github`; the enclave has no `gh` executable or GitHub token.
- The primary agent receives neither the enclave identity nor the gateway
  configuration.
- Minimum versions are AWF `v0.28.9` and mcpg `v0.4.15`; trusted repositories additionally require AWF `v0.28.14`.

For a trusted repository, an `enclave_run_agent` response schema may contain strings while remaining structured and strict:

```json
{
  "type": "object",
  "fields": {
    "should_dispatch": { "type": "boolean" },
    "title": { "type": "string" },
    "problem": { "type": "string" },
    "root_cause": { "type": "string" },
    "proposed_solution": { "type": "string" }
  }
}
```

Responses must conform exactly to the declared schema: fields are required, extra properties are rejected, floats, `$ref`, recursion, regex schemas, and untagged unions are unsupported. Output remains subject to AWF's configured limit and the global 8 KiB ceiling.

See also: [agent-runtime-instructions.md](agent-runtime-instructions.md) for `sandbox.agent` fields, and [network.md](network.md) for network isolation defaults.
