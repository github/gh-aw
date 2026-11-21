# ADR-26322: Step-Scoped Env Injection for DIFC Proxy Routing

**Date**: 2026-04-15
**Status**: Accepted
**Deciders**: lpcox, Copilot
**Amended**: 2026-11-21 (PR #79924): restrict the pre-agent proxy's host listener to loopback in both Docker network modes.

---

## Part 1 — Narrative (Human-Friendly)

### Context

The DIFC proxy (`awmg-proxy`) intercepts pre-agent `gh` CLI calls through integrity filtering when guard policies are configured. To route `gh` CLI through the proxy, several env vars must be set on each step that uses the `gh` CLI: `GH_HOST`, `GH_REPO`, `GITHUB_API_URL`, `GITHUB_GRAPHQL_URL`, and `NODE_EXTRA_CA_CERTS`. Previously, `start_difc_proxy.sh` wrote these vars to `$GITHUB_ENV`, which applies them as job-level state for the entire remainder of the job. On GitHub Enterprise (GHE) runners, `configure_gh_for_ghe.sh` runs earlier in the job and sets `GH_HOST` to the enterprise hostname in `$GITHUB_ENV`; the proxy's subsequent write overwrote that value and left the job without a valid GHE host after the proxy stopped. This required fragile save/restore logic in the shell scripts and a dedicated `Set GH_REPO` workaround step, both of which had to be replicated for every new `gh` CLI variable added.

### Decision

We will inject DIFC proxy routing env vars as **step-level `env:` blocks** on each custom step, rather than writing them to `$GITHUB_ENV`. A new compiler function, `injectProxyEnvIntoCustomSteps()`, uses YAML parse-and-reserialize (via `goccy/go-yaml`) to merge proxy vars into each step's existing `env:` block while preserving user-defined vars (e.g. `GH_TOKEN`). Step-level env overrides `$GITHUB_ENV` for that step only, so the GHE host and other job-level env values are left intact for non-proxied steps (including post-stop cleanup). The shell scripts `start_difc_proxy.sh` and `stop_difc_proxy.sh` are simplified to contain only container lifecycle logic; all env-routing responsibility moves to the compiler.

### Alternatives Considered

#### Alternative 1: Save/Restore `$GITHUB_ENV` in Shell Scripts

Before starting the proxy, save the existing `GH_HOST` (and related vars) to `GH_AW_ORIGINAL_*` placeholders in `$GITHUB_ENV`; restore them in `stop_difc_proxy.sh`. This was the prior implementation. It was rejected because the restore logic was timing-sensitive and silently failed on some GHES runner configurations, leaving the job with a corrupted `GH_HOST` after the proxy stopped. Every new proxy variable required a matching save/restore pair, creating ongoing maintenance burden.

#### Alternative 2: Standalone Env-Override Step + Cleanup Step

Emit a compiler-generated step just before the custom steps block that writes proxy vars to `$GITHUB_ENV`, and a matching cleanup step after. This has the same root problem as Alternative 1: `$GITHUB_ENV` is global, so the cleanup must accurately restore all prior values or non-proxied steps break. It also introduces two extra visible steps per workflow invocation and does not compose cleanly with post-failure cleanup (the cleanup step runs only if prior steps succeed, unless wrapped with `if: always()`).

#### Alternative 3: Wrapper Script That Sets Env Per-Invocation

Wrap each custom step's `run:` block in a shell preamble that sets and unsets the proxy vars for that one invocation. This avoids `$GITHUB_ENV` mutation but requires rewriting `run:` content, which breaks `uses:` steps (action steps do not have a `run:` field) and is incompatible with multi-line scripts that rely on shell inheritance.

### Consequences

#### Positive
- GHE host values set by `configure_gh_for_ghe.sh` are preserved for all non-proxied steps (post-stop steps, MCP gateway setup, secret redaction, artifact upload).
- Eliminates the standalone `Set GH_REPO` workaround step and all save/restore shell logic.
- The proxy's env scope is automatically limited to the steps that need it; no cleanup required.
- Adding new proxy vars in future requires only updating `proxyEnvVars()` in Go, not shell scripts.

#### Negative
- The compiler must parse custom steps YAML at compile time using `goccy/go-yaml`; a parse failure silently falls back to unmodified steps (proxy env not injected), which could cause runtime failures that are hard to diagnose.
- Compiled workflow lock files become slightly larger because each custom step now carries explicit `env:` entries for five proxy vars.
- The `injectProxyEnvIntoCustomSteps()` abstraction must be maintained and kept consistent if the custom-steps YAML structure changes (e.g. if steps are wrapped in an outer document key other than `steps:`).

#### Neutral
- `start_difc_proxy.sh` and `stop_difc_proxy.sh` are now purely container lifecycle scripts with no env-management responsibility; teams maintaining these scripts no longer need to understand the env-routing design.
- Four existing workflow lock files were recompiled to apply the new step-level `env:` blocks; the diff is mechanical and does not represent a behavioral change for github.com-hosted runners.

---

## Part 2 — Normative Specification (RFC 2119)

> The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**, **SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **MAY**, and **OPTIONAL** in this section are to be interpreted as described in [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119).

### Proxy Env Injection

1. When the DIFC proxy is active for a main-job workflow, implementations **MUST** inject proxy routing env vars (`GH_HOST`, `GH_REPO`, `GITHUB_API_URL`, `GITHUB_GRAPHQL_URL`, `NODE_EXTRA_CA_CERTS`) as step-level `env:` blocks on each custom step.
2. Implementations **MUST NOT** write proxy routing env vars to `$GITHUB_ENV` (the runner's persistent env file) for the purpose of routing custom steps through the proxy.
3. Implementations **MUST** preserve existing step-level env vars that are not proxy routing vars (e.g. `GH_TOKEN`) when injecting proxy vars. Proxy routing vars (`GH_HOST`, `GH_REPO`, `GITHUB_API_URL`, `GITHUB_GRAPHQL_URL`, `NODE_EXTRA_CA_CERTS`) **MUST** take precedence over any user-defined values for those same keys; user-provided values for these keys are intentionally overwritten to guarantee correct proxy routing.
4. If YAML parsing of the custom steps string fails, implementations **SHOULD** log the error and return the original custom steps string unchanged rather than aborting compilation.

### Shell Script Responsibilities

1. `start_difc_proxy.sh` **MUST** be responsible only for starting the proxy container, performing a health check, installing the CA certificate, and adding the `proxy` git remote.
2. `start_difc_proxy.sh` **MUST NOT** write any env vars to `$GITHUB_ENV`.
3. `stop_difc_proxy.sh` **MUST** be responsible only for stopping the proxy container and removing the CA certificate.
4. `stop_difc_proxy.sh` **MUST NOT** read from or write to `$GITHUB_ENV` for the purpose of restoring or clearing proxy routing vars.

The pre-agent proxy serves runner-side clients. It listens on `127.0.0.1` in host-network mode. In bridge mode Docker publishes the port on host loopback and forwards requests to the container's bridge address.

### Conformance

An implementation is considered conformant with this ADR if it satisfies all **MUST** and **MUST NOT** requirements above. Specifically: proxy routing vars appear only as step-level `env:` blocks on custom steps (never in `$GITHUB_ENV` writes within the proxy scripts); proxy routing vars (`GH_HOST`, `GH_REPO`, `GITHUB_API_URL`, `GITHUB_GRAPHQL_URL`, `NODE_EXTRA_CA_CERTS`) take precedence and overwrite any user-defined values for those keys; and all other step env vars are preserved. Failure to meet any **MUST** or **MUST NOT** requirement constitutes non-conformance.

---
