---
title: Checkout Behavior Specification
description: Formal specification of checkout behavior across activation, agent, and safe_outputs jobs in gh-aw workflows
sidebar:
  order: 1365
---

# Checkout Behavior Specification

**Version**: 1.2.0<br>
**Status**: Working Draft  
**Publication Date**: 2026-09-23<br>
**Editor**: GitHub Agentic Workflows Team  
**This Version**: [checkout-behavior-specification](/gh-aw/specs/checkout-behavior-specification/)  
**Latest Published Version**: This document

---

## Abstract

This specification defines normative checkout behavior in GitHub Agentic Workflows for activation, agent, and `safe_outputs` jobs. It specifies credential and token precedence, `github-token` and `github-app` resolution, trial mode behavior, side-repo targeting, sparse/shallow/fetch semantics, symlink handling during sparse activation checkout, submodule cleanup semantics, and checkout-manifest behavior used by safe output handlers.

## Status of This Document

This is a working draft and may change. It describes behavior mined from the current implementation in `pkg/workflow/` and `actions/setup/js/`.

## Table of Contents

1. [Introduction](#1-introduction)
2. [Conformance](#2-conformance)
3. [Checkout Model](#3-checkout-model)
4. [Authentication and Token Resolution](#4-authentication-and-token-resolution)
5. [Sparse, Shallow, Fetch, Symlink, and Submodule Behavior](#5-sparse-shallow-fetch-symlink-and-submodule-behavior)
6. [Trial Mode and Side-Repo Behavior](#6-trial-mode-and-side-repo-behavior)
7. [Compliance Testing](#7-compliance-testing)
8. [References](#8-references)
9. [Change Log](#9-change-log)

---

## 1. Introduction

### 1.1 Purpose

This document defines exactly how checkout is compiled and executed for:

- Activation job sparse checkouts
- Agent job checkouts
- `safe_outputs` job PR/push checkouts

### 1.2 Scope

This specification covers:

- `checkout:` parsing and merge behavior
- `actions/checkout` step generation
- Checkout credential persistence/cleanup behavior
- Token and GitHub App precedence across checkout and `safe_outputs`
- Sparse/shallow/fetch mechanics
- Symlink handling for activation sparse checkout
- Submodule credential cleanup behavior
- Trial mode and side-repo derivation behavior
- Checkout-manifest generation requirements for safe_outputs handler lookup

---

## 2. Conformance

### 2.1 Requirements Notation

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be interpreted as described in [RFC 2119](https://www.ietf.org/rfc/rfc2119.txt).

### 2.2 Conformance Classes

- **C1 (Compiler conformance)**: emits checkout YAML conforming to Sections 3–6.
- **C2 (Runtime conformance)**: JS/sh handlers honor manifest safety, fetch behavior, and credential boundaries in Sections 4–6.

---

## 3. Checkout Model

### 3.1 Checkout Entry Parsing

`checkout:` MUST accept either a single object or an array of objects.  
Each entry MAY define: `repository`, `ref`, `path`, `github-token` (or legacy `token`), `github-app` (or deprecated alias `app`), `safe-outputs-github-app`, `fetch-depth`, `fetch`, `sparse-checkout`, `submodules`, `lfs`, `current`, `wiki`, and `force-clean-git-credentials`.

`github-token` and `github-app` MUST be mutually exclusive per entry.

`safe-outputs-github-app` applies only to safe_outputs git auth/token resolution. It MUST NOT change agent/activation checkout authentication behavior.

### 3.2 Entry Merge Rules

Entries with the same `(repository, path, wiki)` key MUST merge with these rules:

- `fetch-depth`: deepest wins (`0` wins over all)
- `ref`: first non-empty wins
- auth (`github-token` vs `github-app`): first auth wins
- safe_outputs auth (`safe-outputs-github-app`): first non-empty wins
- sparse patterns: union
- fetch refs: union
- `lfs`: OR
- `submodules`: first non-empty wins
- `force-clean-git-credentials`: OR

### 3.3 Job-specific Checkout Generation

- **Activation job** MUST use sparse checkout for `.github` and `.agents`, with `persist-credentials: false`.
- **Agent job** MUST generate default checkout plus additional checkouts from `CheckoutManager`, with `persist-credentials: false` by default.
- **safe_outputs job** MUST reuse the same checkout generators but set keep-credentials mode for push/fetch use and inject a `Configure Git credentials` step.

### 3.4 Checkout Manifest

For non-default cross-repo checkouts, the compiler MUST emit a checkout manifest step.  
Runtime lookup (`find_repo_checkout`) MUST prefer manifest paths and MUST reject manifest paths that are absolute or escape workspace roots (see §7 test T-CHK-014 for compliance test coverage of this requirement).

Checkout-manifest generation MUST include enough per-checkout auth metadata to resolve default branches and safe_outputs repo targeting without relying on implicit defaults.  
The manifest (or manifest-construction env) MUST NOT persist resolved token values to disk.

### 3.5 Workspace Git-Scan Fallback

When no usable manifest entry exists for a requested `owner/repo` target, runtime lookup MAY fall back to scanning `$GITHUB_WORKSPACE` for git repositories and matching them by `remote.origin.url`. This fallback exists because repositories cloned by a `steps:` entry or a manual `actions/checkout` are absent from the manifest.

The fallback is an agent-influenced trust boundary: the agent can write anywhere under `$GITHUB_WORKSPACE`, while the safe-outputs MCP server that performs the lookup holds the GitHub token. Implementations MUST therefore satisfy all of the following:

- **Manifest precedence**: the scan MUST run only after manifest lookup fails to yield an existing, in-workspace path.
- **Scan confinement**: the scan MUST NOT descend outside `$GITHUB_WORKSPACE` and MUST NOT follow symbolic links out of it.
- **Scoped ownership trust**: the safe-outputs MCP server container runs under a uid that differs from the runner user owning workspace clones, so `git config --get` would otherwise fail git's dubious-ownership check and silently report a missing remote. The implementation MUST scope the `safe.directory` override to that single read-only invocation (for example via `GIT_CONFIG_COUNT`/`GIT_CONFIG_KEY_*`/`GIT_CONFIG_VALUE_*` on the child process environment) and MUST NOT mutate the process environment or grant process-wide trust to scanned directories.
- **Read-only discovery**: discovery MUST use only git commands that cannot execute repository-supplied programs (no hooks, pagers, filesystem monitors, or credential helpers).
- **Remote host constraint**: a scanned repository MUST be bound to an `owner/repo` slug only when its `remote.origin.url` host matches the host of `GITHUB_SERVER_URL` or `github.com`; remotes on other hosts MUST be ignored.
- **Deferred durable trust**: durable `safe.directory` trust MUST be granted only to the resolved checkout directory, after cross-repository allowlist validation, at the point where git write operations are performed.
- **Non-disclosing failure**: the not-found error MUST name the requested slug and the supported remediation (`checkout:` entry or workspace clone) without returning scanned filesystem paths to the agent.

See also Threat T7 and requirements RCR1–RCR7 in the [Safe Outputs MCP Gateway Specification](/gh-aw/specs/safe-outputs-specification/).

---

## 4. Authentication and Token Resolution

### 4.0 Compiler Error Message Requirements

When `github-token` and `github-app` are both specified on the same checkout entry, the compiler MUST reject the workflow with an error. The normative error message format is:

```
Checkout entry for '<repository>' specifies both 'github-token' and 'github-app'. These fields are mutually exclusive. Remove one of them.
```

Where `<repository>` is the `repository` field value from the checkout entry (or `"(default)"` when `repository` is unset). The error MUST be emitted as a compiler validation error (not a warning) and MUST halt compilation.

### 4.1 Checkout Entry Auth

For each checkout step, token resolution MUST be:

1. Entry `github-app` minted token (with fallback behavior if `ignore-if-missing: true`)
2. Entry `github-token`
3. Default token fallback when required by context

Top-level `github-app` fallback MUST be applied to checkout entries only when that entry has neither `github-app` nor `github-token`.

### 4.2 Activation Job Token

Activation token precedence MUST be:

1. `on.github-app` minted token; when `ignore-if-missing: true`, this resolves to `steps.activation-app-token.outputs.token || secrets.GITHUB_TOKEN`
2. `on.github-token`
3. `secrets.GITHUB_TOKEN`

### 4.3 safe_outputs PR Checkout Token

`safe-outputs.github-app` and `safe-outputs.github-token` MUST NOT be used as checkout tokens in the safe_outputs job. They govern safe_outputs operations (PR creation, push) only. Using a safe_outputs-scoped app token for checkout is unsafe when that token is scoped to a target organization that differs from the workflow repository's organization.

`resolvePRCheckoutToken` precedence MUST be:

1. Checkout target `safe-outputs-github-app` minted token (with fallback chain when `ignore-if-missing: true`)
2. `${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}`

When safe_outputs checkout retention is enabled, checkouts without explicit entry tokens MUST persist the resolved PR checkout token so local git credentials match push/fetch token usage.

### 4.4 Credential Lifecycle Boundary

- Agent job checkouts MUST remove persisted credentials by default (`persist-credentials: false`).
- safe_outputs PR checkout path MUST retain credentials (`persist-credentials: true`) and run explicit git remote credential configuration for root and sub-repo checkouts.
- Agent guidance MUST state that authenticated `git fetch/pull/push` is unavailable after checkout unless refs were already fetched.

### 4.5 Credential-less Safe-outputs MCP Context

`generate_git_patch` and `generate_git_bundle` in the safe-outputs MCP server context MUST fail softly when required refs are unavailable and private-repo fetch cannot authenticate, with actionable error text directing users to add refs via `checkout.fetch`.

---

## 5. Sparse, Shallow, Fetch, Symlink, and Submodule Behavior

### 5.1 Shallow and Additional Fetch

- Default shallow behavior MUST be `fetch-depth: 1` when unset.
- Additional `fetch:` refs MUST emit a follow-up `git fetch` step per checkout entry.
- Follow-up fetch MUST mirror effective depth (omit `--depth` only when effective depth is `0`).
- Follow-up fetch MUST inject credentials via command-level `http.extraheader` and MUST NOT persist credentials to git config.

### 5.2 Sparse Checkout and Partial-clone Repair

When sparse checkout is configured, compiler-generated checkout steps MUST emit:

- `filter: 'blob:limit=1073741824'`
- a post-checkout step clearing:
  - `remote.origin.promisor`
  - `remote.origin.partialclonefilter`

This avoids blobless partial-clone behavior that would require later authenticated lazy fetches in agent contexts.

### 5.3 Activation Symlink Handling

Activation sparse checkout path expansion MUST detect symlinks for known `.github` subpaths and include resolved in-repo targets.  
Resolved targets escaping repository root MUST be rejected.

### 5.4 Submodules and Credential Cleanup

`submodules` MUST accept string or boolean and compile to string form accepted by `actions/checkout`.  
When `force-clean-git-credentials: true` is set (and keep-credentials-for-push is not enabled), checkout MUST keep credentials long enough for checkout internals and then run cleanup that removes credential sections/headers from:

- `.git/config`
- `.git/modules/**/config`

---

## 6. Trial Mode and Side-Repo Behavior

### 6.1 Trial Mode Checkout

In trial mode, default checkout generation MUST:

- Override repository to `trialLogicalRepoSlug` when provided
- Emit token via default GitHub token chain (`getEffectiveGitHubToken("")`)
- Keep additional fetch-step behavior aligned with non-trial mode

For safe-output environment variables, trial mode SHOULD set `GH_AW_TARGET_REPO_SLUG` to the logical trial repo when no explicit target repo is configured.

### 6.2 Side-Repo Target Derivation

Side-repo maintenance target detection MUST use checkout entries marked `current: true` with static repository slugs.  
Expression-based repositories MUST be excluded.  
Auth selection for repeated targets MUST preserve first-seen auth, except that empty auth MAY be upgraded by a later non-empty auth.

Effective side-repo token precedence MUST be:

1. side-repo target checkout `github-token`
2. side-repo target checkout `github-app` minted token reference
3. `${{ secrets.GH_AW_GITHUB_TOKEN }}`

### 6.3 `push_to_pull_request_branch` Side-Repo Checkout Resolution

The `push_to_pull_request_branch` safe-output handler MUST resolve the target checkout directory from one of three sources, evaluated in priority order:

1. `patch_workspace_path` configuration (explicit path; resolved via `resolvePatchWorkspacePath`).
2. `entry.repo` field or `target-repo` workflow config entry — triggers `findRepoCheckout` lookup.
3. `GH_AW_TARGET_REPO_SLUG` environment variable — used as a side-repo checkout hint **only when** its value (case-insensitively) differs from `GITHUB_REPOSITORY`. When the env var is set but matches `GITHUB_REPOSITORY`, it MUST be ignored and the handler MUST log a debug message explaining the passthrough.

When source 2 or 3 triggers a `findRepoCheckout` lookup, all subsequent git operations for that handler invocation (branch detection, patch generation, bundle generation) MUST operate under the resolved checkout directory (`repo_cwd`) rather than the workspace root.

When `GH_AW_TARGET_REPO_SLUG` is used as a side-repo hint (source 3), the implementation SHOULD emit an informational debug log identifying the resolved `owner/repo`.

When `GH_AW_TARGET_REPO_SLUG` is set but equals `GITHUB_REPOSITORY`, the implementation MUST emit a debug log stating that the variable is not being used as a side-repo hint and explaining why.

**Rationale**: `GH_AW_TARGET_REPO_SLUG` may be globally set in side-repo maintenance workflows for cross-repo operations other than `push_to_pull_request_branch`. Using it unconditionally as a checkout hint would misdirect branch and patch detection when the push target is the current (workflow-host) repository.

---

## 7. Compliance Testing

### 7.1 Required Tests

- **T-CHK-001**: `checkout.github-token` and `checkout.github-app` mutual exclusivity validation
- **T-CHK-002**: Merge behavior by `(repository,path,wiki)` key
- **T-CHK-003**: Agent job emits `persist-credentials: false` by default
- **T-CHK-004**: safe_outputs PR checkout path retains credentials and configures git remotes
- **T-CHK-005**: Sparse checkout emits blob-limit filter and partial-clone reset step
- **T-CHK-006**: Additional `fetch:` emits depth-mirrored fetch with `http.extraheader`
- **T-CHK-007**: Manifest path resolution rejects absolute/out-of-workspace paths
- **T-CHK-008**: Trial mode repository/token override behavior
- **T-CHK-009**: Side-repo target extraction and auth precedence
- **T-CHK-010**: `force-clean-git-credentials` cleanup covers `.git/modules/**/config`
- **T-CHK-011**: `checkout.safe-outputs-github-app` is the sole supported safe_outputs auth override per checkout entry
- **T-CHK-012**: safe_outputs checkout token MUST NOT use `safe-outputs.github-app` or `safe-outputs.github-token`; only `safe-outputs-github-app` (per entry) or `GITHUB_TOKEN` are permitted
- **T-CHK-013**: Checkout-manifest generation includes safe_outputs auth metadata without persisting resolved tokens
- **T-CHK-014**: Checkout-manifest path resolution MUST reject paths that are absolute (e.g., `/etc/passwd`) or escape the workspace root (e.g., `../../sensitive`); rejected paths MUST produce an error and MUST NOT be used for checkout or file lookup
- **T-CHK-015**: `push_to_pull_request_branch` uses side-repo checkout from `GH_AW_TARGET_REPO_SLUG` only when it differs from `GITHUB_REPOSITORY`; emits debug log and ignores it when they match
- **T-CHK-016**: Workspace git-scan fallback reads `remote.origin.url` with a per-invocation `safe.directory` override (process environment unchanged), and ignores scanned repositories whose remote host is neither `GITHUB_SERVER_URL`'s host nor `github.com`

### 7.2 Compliance Checklist

| Requirement | Test ID | Level | Status |
|---|---|---|---|
| Checkout parse + merge semantics | T-CHK-001, T-CHK-002 | C1 | Required |
| Agent vs safe_outputs credential lifecycle | T-CHK-003, T-CHK-004 | C1/C2 | Required |
| Sparse/shallow/fetch semantics | T-CHK-005, T-CHK-006 | C1 | Required |
| Manifest path safety | T-CHK-007 | C2 | Required |
| Trial and side-repo behavior | T-CHK-008, T-CHK-009 | C1 | Required |
| Submodule cleanup behavior | T-CHK-010 | C1/C2 | Required |
| Checkout-level safe_outputs auth field | T-CHK-011, T-CHK-012 | C1/C2 | Required |
| Checkout-manifest generation requirements | T-CHK-013 | C1/C2 | Required |
| Checkout-manifest path-escape rejection | T-CHK-014 | C2 | Required |
| `push_to_pull_request_branch` side-repo cwd resolution | T-CHK-015 | C2 | Required |
| Workspace git-scan fallback trust scoping and host constraint | T-CHK-016 | C2 | Required |

### 7.3 Safeguards

The following MUST-level norms govern credential and token safety during checkout and `safe_outputs` operations:

1. **Token write-to-disk prohibition**: `safe-outputs-github-app` minted tokens MUST NOT be written to disk at any point before the `safe_outputs` job begins its push/PR operations. Token values MUST be passed only as environment variables or via GitHub Actions secret interpolation at the step level. Any intermediate file, log entry, or env export that materializes the token value to disk MUST be treated as a security violation.

2. **Manifest path rejection**: Checkout-manifest paths that are absolute or that escape the workspace root MUST be rejected before any file I/O is performed against them. The rejection MUST produce an actionable error message (see §3.4 and T-CHK-014).

3. **Credential cleanup**: When `force-clean-git-credentials: true` is active and `keep-credentials-for-push` is not, all credential-bearing git config sections MUST be removed from `.git/config` and `.git/modules/**/config` before the agent step completes.

4. **Scan trust scoping**: The workspace git-scan fallback MUST NOT grant process-wide git ownership trust to scanned directories, and MUST NOT bind a scanned directory to an `owner/repo` slug when its remote host is neither the host of `GITHUB_SERVER_URL` nor `github.com` (see §3.5 and T-CHK-016).

---

## 8. References

### Normative References

- [RFC 2119](https://www.ietf.org/rfc/rfc2119.txt)
- `pkg/workflow/checkout_manager.go`
- `pkg/workflow/checkout_config_parser.go`
- `pkg/workflow/checkout_step_generator.go`
- `pkg/workflow/compiler_yaml_main_job.go`
- `pkg/workflow/compiler_safe_outputs_steps.go`
- `pkg/workflow/safe_outputs_app_config.go`
- `pkg/workflow/github_token.go`
- `actions/setup/js/generate_git_patch.cjs`
- `actions/setup/js/generate_git_bundle.cjs`
- `actions/setup/js/find_repo_checkout.cjs`
- `actions/setup/sh/configure_git_credentials.sh`
- `actions/setup/sh/clean_git_credentials.sh`

### Informative References

- `/gh-aw/reference/checkout/`
- `/gh-aw/reference/safe-outputs-pull-requests/`
- `/gh-aw/docs/sparseness.md`

---

## 9. Change Log

### Version 1.2.0 (Working Draft)

- Added §3.5: workspace git-scan fallback requirements covering manifest precedence, scan confinement, per-invocation `safe.directory` scoping, read-only discovery, remote host constraint, deferred durable trust, and non-disclosing failure messages.
- Added T-CHK-016 to §7.1 and the §7.2 compliance checklist, and a scan trust-scoping safeguard to §7.3.

### Version 1.1.0 (Working Draft)

- Added §6.3: `push_to_pull_request_branch` side-repo checkout resolution requirements, covering `GH_AW_TARGET_REPO_SLUG` passthrough guard, debug-logging obligation, and `repo_cwd` scoping of all git operations.
- Added T-CHK-015 to §7.1 and §7.2 compliance checklist.

### Version 1.0.0 (Working Draft)

- Initial specification for checkout behavior in activation, agent, and safe_outputs jobs
- Added normative credential precedence and lifecycle requirements
- Added sparse/shallow/fetch, symlink, submodule, trial mode, and side-repo requirements
