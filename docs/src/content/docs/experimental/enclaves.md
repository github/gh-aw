---
title: Private repository enclaves
description: Configure unified AWF script and agent enclaves through the trusted MCP gateway.
---

The top-level `enclaves` array enables finite-disclosure access to approved private repositories. The compiler registers `enclave_run_script` or `enclave_run_agent` from the keyed entries present on the `awf-enclave` MCP route. Omit the array to disable enclaves.

Enclaves require AWF network isolation, which every supported `sandbox.agent.runtime` profile provides, so the compiler launches mcpg in bridge mode and AWF can attach it to the isolated topology.

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

Each `repos` entry uses `public`, `trusted`, `internal`, `confidential`, or `sealed` sensitivity. `trusted` is unmetered and permits free-form string values inside an otherwise strict structured response schema, but is appropriate only when repository content is approved for unrestricted return to the primary agent without confidentiality accounting. Do not select it merely to obtain string output. All other sensitivities are finite-schema-only. Each type can appear at most once. When the same repository appears in both entries, its sensitivity must match because its information budget is shared across executor types. AWF fixes the script enclave network and interpreter and the agent enclave network internally; workflows cannot override those security invariants.

The generated gateway upstream uses a fresh masked capability for each workflow run. That capability is passed only to mcpg and AWF and is excluded from the primary agent environment. The gateway allows 120 seconds for the AWF-owned HTTP upstream to become available. It enforces a 4,860-second tool timeout, covering AWF's maximum 4,800-second finite-disclosure timing bucket plus a 60-second transport allowance. Executor timeouts are capped at 4,740 seconds because AWF reserves 60 seconds in the final bucket for processing and cleanup. The gateway timeout is an enforcement bound, not an absolute AWF wall-clock guarantee under pathological host cleanup or scheduler stalls.

This compiler contract depends on the unified enclave implementation from `github/gh-aw-firewall#6992`. Until that change is available in an AWF release, pinning an older AWF version will not provide the enclave server.

## GitHub tool access from agent enclaves

Prefer `agent.tools.github` for new workflows:

```yaml
sandbox:
  agent:
    id: awf
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
    timeout: 180
```

- `allowed` is required and currently accepts only `list_issues` and `issue_read`.
- `allowed-repos` is optional. If omitted, the enclave identity inherits the enclave's `repos` list. If set, every entry must also appear in that list.
- `min-integrity` is optional and defaults to `approved`.
- GraphQL, search, writes, and every other GitHub tool remain denied.
- The minimum supported versions are AWF `v0.28.9` (or `v0.28.14` when using `trusted`) and mcpg `v0.4.15`.

## Dynamic agent repository policies

Agent enclaves can admit one repository per invocation at runtime without listing every repository in frontmatter. Dynamic entries use the same `awf-enclave` MCP backend, but replace static `repos` with a closed compiler-owned policy envelope:

```yaml
sandbox:
  agent:
    id: awf
    version: v0.28.14
  mcp:
    version: v0.4.17
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

- Dynamic mode is supported only for `agent` entries. `script` entries must use static `repos`.
- An entry must declare either non-empty static `repos` or `dynamic`, never both.
- `allowed-owners` and `allowed-repositories` use canonical lowercase ASCII selectors. Repository selectors must already match `owner/repo`; the compiler does not trim, case-fold, URL-decode, or normalize them.
- `github-policy` must be `github-repository-read-v1`, which exposes only `list_issues` and `issue_read` through per-invocation delegated identities.
- Dynamic entries require finite resource limits, total quotas, audit labels, an absolute `expires-at` timestamp, AWF `v0.28.14` or newer, and mcpg `v0.4.17` or newer (the first mcpg release that accepts the delegation controller's atomic bootstrap configuration; see `github/gh-aw-mcpg#12605`). `expires-at` is an upper bound checked into the workflow file; the compiler resolves the effective envelope expiry at workflow setup time as `min(expires-at, job-start + enclave timeout)`, so a fixed, checked-in timestamp never needs to be a short-lived compile-time value.
- The primary and enclave agents do not receive repository credentials or the delegation-control capability. The compiler starts mcpg's `github-repository-delegation-v1` controller with five settings as one atomic configuration (`MCP_GATEWAY_DELEGATION_ENVELOPE`, `MCP_GATEWAY_DELEGATION_CONTROL_KEY`, `MCP_GATEWAY_DELEGATION_STATE_PATH`, `MCP_GATEWAY_DELEGATION_GENERATION`, `MCP_GATEWAY_DELEGATION_CONTROL_LISTEN`) and hands AWF an explicit, host-private control endpoint (`AWF_ENCLAVE_GITHUB_DELEGATION_CONTROL_ENDPOINT`) and capability so AWF can request short-lived mcpg identities for admitted repositories. That endpoint is distinct from the executor-facing `AWF_ENCLAVE_MCP_GATEWAY_ENDPOINT` data plane and is excluded from the primary agent's environment.

## Deprecated `issues-read-v1` profile

Agent enclaves can still opt into the legacy profile during migration:

```yaml
enclaves:
  - agent:
      model: gpt-5
      github:
        cli: issues-read-v1
    repos:
      - repo: octo-org/private-service
        sensitivity: confidential
    timeout: 180
```

`issues-read-v1` is the only accepted `agent.github.cli` value, and it is deprecated in favor of `agent.tools.github`. Script enclaves cannot configure `github`. The first profile version accepts at most one repository whose sensitivity is neither `public` nor `trusted`; additional assigned repositories may declare `sensitivity: public` or `trusted`.

The profile permits only `list_issues` and `issue_read` through the GitHub MCP
server. GraphQL, search, writes, and every other GitHub tool are denied.

The compiler configures a shared mcpg gateway with separate primary and enclave
identities. The enclave identity can access only those tools and the union of
repositories declared for the enclave. AWF stages that identity privately and
connects the enclave directly to `/mcp/github`; the enclave has no `gh`
executable or GitHub token. Neither the primary agent nor the enclave receives
the PAT or the other identity.

Provide `GH_AW_GITHUB_MCP_SERVER_TOKEN` or `GH_AW_GITHUB_TOKEN` with read access
to the assigned repository's Issues. The fallback `GITHUB_TOKEN` can only
access repositories that token can already read (typically just the current
repository in Actions).

The minimum supported versions are AWF `v0.28.9` (or `v0.28.14` when using `trusted`) and mcpg `v0.4.15`. The
compiler does not fall back to older versions.
