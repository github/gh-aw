---
title: Enterprise Configuration
description: Deployment and runner applicability for GitHub Agentic Workflows, including GHEC, GHES, ARC/DinD, component prerequisites, and support boundaries.
sidebar:
  order: 51
---

# Enterprise Configuration

This page covers configuration options specific to GitHub Enterprise Server (GHES) and GitHub Enterprise Cloud (GHEC) deployments.

> [!IMPORTANT]
> **Component compatibility is not end-to-end product support.** This is the consolidated applicability reference for the gh-aw stack. GHES endpoint routing, artifact compatibility mode, and ARC/DinD execution do not establish official GitHub Agentic Workflows availability on GHES or certify a server release or ARC chart. Combinations without an explicit owner-confirmed commitment remain **unconfirmed**.

## Deployment and runner applicability

Choose the GitHub deployment and runner topology independently. GHEC organizations on `github.com` are not the same deployment as GHEC with data residency on `*.ghe.com`; neither is GHES. ARC describes runner infrastructure, not a GitHub deployment or an AI engine.

| GitHub deployment | Implemented compatibility | Evidence and product boundary |
| --- | --- | --- |
| `github.com`, including GHEC organizations | Default GitHub and Copilot endpoint configuration | [Published product requirements](https://docs.github.com/en/copilot/concepts/agent-capabilities/agent-powered-workflows-and-operations/about-github-agentic-workflows#requirements), quickstart, and hosted Linux CI environment. This does not promise every engine, feature, or runner combination. |
| GHEC with data residency (`*.ghe.com`) | Tenant-specific endpoint routing and host/domain configuration | Endpoint/configuration tests and [tenant troubleshooting](/gh-aw/troubleshooting/debug-ghe/), not a live tenant validation matrix. End-to-end availability for the selected engine and services is **unconfirmed** here. |
| GHES (custom hostname) | CLI host configuration, artifact/cache compatibility mode, and enterprise endpoint routing | Compiler and endpoint tests, not a live GHES release matrix. End-to-end availability and a minimum GHES release are **unconfirmed**. |

| Runner topology | Component prerequisites | Evidence and limitations |
| --- | --- | --- |
| GitHub-hosted Linux | Docker/Compose and compatible Linux tooling | gh-aw and AWF hosted CI; this is not enterprise-tenant or GHES validation. GitHub-hosted runners are not available for GHES. |
| Self-hosted Linux, shared runner/daemon filesystem | Docker access; daemon-visible workspace, tool, and output paths | Configuration path exists; hosted CI does not certify custom images, persistent state, or security policies. Validate the actual runner and selected engine. |
| Self-hosted ARC/DinD, split runner/daemon filesystems | Privileged DinD sidecar, shared work volume, accessible Docker socket, `DOCKER_HOST`, and sysroot staging | Implemented split-filesystem path. AWF live split-DinD checks are **not Kubernetes ARC** validation. End-to-end ARC deployment/chart combinations remain **unconfirmed**; no minimum chart version is established here. |

For official runner availability, see [GitHub-hosted runners](https://docs.github.com/en/actions/concepts/runners/github-hosted-runners). For firewall-specific applicability, see [AWF environment and runner applicability](https://github.com/github/gh-aw-firewall/blob/main/docs/compatibility.md#environment-and-runner-applicability), [enterprise routing](https://github.com/github/gh-aw-firewall/blob/main/docs/enterprise-configuration.md), and [ARC/DinD configuration](https://github.com/github/gh-aw-firewall/blob/main/docs/arc-dind.md). AWF links back to this reference for stack-level applicability.

### End-to-end availability and owner confirmation

An end-to-end statement must cover the **gh-aw compiler, selected engine and inference provider, MCP gateway and configured MCP servers, AWF, Actions runner, and dependent GitHub services**: Actions workflow syntax, tokens/permissions, artifacts, caches, APIs, and any enabled Copilot services. A passing compiler test or accepted AWF schema establishes none of the other components' availability.

GitHub's [published GHEC product requirements](https://docs.github.com/en/enterprise-cloud@latest/copilot/concepts/agent-capabilities/agent-powered-workflows-and-operations/about-github-agentic-workflows#requirements) require Actions, an AI-engine account, and an authenticated CLI; they do not provide a GHES/ARC component-version certification. The evidence linked here does not include an owner-confirmed release-by-release support commitment for GHES, data-residency tenants, or Kubernetes ARC. Those combinations remain unconfirmed, rather than being declared supported or unsupported from configuration examples. Selecting Claude, Codex, Pi, or a custom engine does not by itself resolve deployment/service compatibility. Consult [engine contracts](/gh-aw/reference/engines/) and [authentication](/gh-aw/reference/auth/) for the selected provider's requirements.

The gh-aw/Next and GitHub Docs owners need to confirm the deployment × topology × engine combinations, exact component versions, product availability, limitations, and validation evidence before a support commitment is published. [gh-aw#75716](https://github.com/github/gh-aw/issues/75716) records this coordination request; the [AWF owner request](https://github.com/github/gh-aw-firewall/issues/11245#issuecomment-6710853719) requests the corresponding upstream clarification, not a support certification.

**Navigation follow-up:** gh-aw/Next and Docs owners should evaluate product/version self-service navigation, including whether a product/version selector would help readers distinguish GHEC, GHES, and component versions. That decision is pending and does not block this clarification, commit to a selector implementation, or commit to migration to `docs.github.com`.

### Component versions and validation evidence

Record the compiler version, selected engine version/provider/model, MCP gateway and server image digests, AWF version/images, Actions runner version, Docker/Compose versions, GitHub deployment/release, and (for ARC) chart, Kubernetes, and pod configuration when validating a deployment. The generated `.lock.yml` records the compiled pins; upgrading the CLI alone does not update an already compiled workflow.

| Component or feature | Prerequisite and scope | Evidence |
| --- | --- | --- |
| GHES artifact actions | Compatibility mode selects `upload-artifact@v3.2.2` and `download-artifact@v3.1.0`. Their Node.js 24 runtime requires Actions Runner `2.327.1+`; this is an action-runtime requirement, not a GHES product floor. | [Pin selection](https://github.com/github/gh-aw/blob/main/pkg/actionpins/resolve.go), [generated artifact tests](https://github.com/github/gh-aw/blob/main/pkg/workflow/ghes_artifact_pins_integration_test.go), and the [upload](https://github.com/actions/upload-artifact/tree/v3.2.2) / [download](https://github.com/actions/download-artifact/tree/v3.1.0) action requirements. |
| GHES cache syntax | Compatibility mode omits compiler-generated `cache-mode`; the server must independently support the remaining Actions syntax and cache actions. | [Cache-mode compiler tests](https://github.com/github/gh-aw/blob/main/pkg/workflow/jobs_cache_mode_test.go) and the GitHub.com-only announcement below. |
| ARC/DinD fixes | The walkthrough's historical feature floors are gh-aw `v0.82.8` and AWF `v0.27.22`. The narrower compiler startup gate requires AWF `v0.27.20+` for sysroot/chroot mount fixes; neither is certification of every newer release or an ARC/GHES combination. | [ARC required versions](/gh-aw/reference/arc-dind-copilot-agent/#7-required-versions) and [compiler validation](https://github.com/github/gh-aw/blob/main/pkg/workflow/runner_topology_validation.go). |
| AWF and MCP gateway pins | This revision defaults to AWF `v0.29.7` and MCP gateway `v0.5.4`. Match documentation/schema to the version in your lock file, including overrides. | [Compiler version constants](https://github.com/github/gh-aw/blob/main/pkg/constants/version_constants.go). |
| Docker and Compose | AWF documents Docker Engine `20.10+` and Compose `v2.0+` installation baselines. Docker must be accessible to the runner and gateway; these baselines are not an exact-version CI matrix. | [AWF version prerequisites](https://github.com/github/gh-aw-firewall/blob/main/docs/compatibility.md#version-specific-prerequisites-and-optional-runtimes). |
| Copilot engine | Requires applicable licensing, policy authorization, inference credentials, and connectivity to the deployment's actual Copilot endpoint. Runner-registration credentials do not authorize inference. | [Copilot authentication](/gh-aw/reference/auth/#copilot_github_token) and [GHES prerequisites](/gh-aw/troubleshooting/common-issues/#copilot-engine-prerequisites-on-ghes). |
| Optional runtimes | Cloud Hypervisor is preview and limited to GitHub-hosted Ubuntu x86_64 KVM runners, not self-hosted/ARC. NVX is preview, requires AWF `v0.28.49+`, explicit provisioning/attestation and Linux x86_64 KVM, and rejects ARC/DinD. | [Runtime restrictions](/gh-aw/reference/agent-runtimes/) and [AWF optional runtimes](https://github.com/github/gh-aw-firewall/blob/main/docs/compatibility.md#version-specific-prerequisites-and-optional-runtimes). |

The compiler's [artifact integration tests](https://github.com/github/gh-aw/blob/main/pkg/workflow/ghes_artifact_pins_integration_test.go) generate and inspect YAML; they do not run on a GHES appliance. AWF's [CI matrix](https://github.com/github/gh-aw-firewall/blob/main/docs/compatibility.md#ci-test-matrix) distinguishes hosted Linux build/unit/live Docker checks from the [provider-free live split-DinD harness](https://github.com/github/gh-aw-firewall/blob/main/.github/workflows/test-docker-harness-observation.yml). These are configured checks, not a claim that every run passed. They do not establish live GHES, data-residency tenant, or Kubernetes ARC validation.

### ARC filesystem and AWF configuration scope

For compiled workflows, `runner.topology: arc-dind` enables compiler-managed staging; runtime handling also probes a `tcp://` `DOCKER_HOST`. The runner and daemon must share the work root at compatible absolute paths. Workspace, tool cache, home/output state, and mounts must be daemon-visible; the runner's private `/usr`, `/lib`, `/bin`, and `/sbin` require sysroot staging. Rootless runner installation does not remove the DinD sidecar's `privileged: true` requirement. See the [ARC walkthrough](/gh-aw/reference/arc-dind-copilot-agent/) and [self-hosted host isolation checklist](/gh-aw/reference/self-hosted-runners/#host-isolation-checklist).

The pinned AWF [published schema](https://github.com/github/gh-aw-firewall/blob/v0.29.7/docs/awf-config.schema.json) and [source schema](https://github.com/github/gh-aw-firewall/blob/v0.29.7/src/awf-config-schema.json) describe standalone AWF JSON, not gh-aw frontmatter. The current AWF applicability notices were merged after `v0.29.7`; links to AWF `main` provide current scope guidance, while these pinned schemas describe the compiler's default release.

| Standalone AWF field | Meaning at the pinned release |
| --- | --- |
| `runner.topology` | `standard` or `arc-dind`; ARC enables sysroot staging and tool-cache warnings, not every other setting. |
| `runner.sysrootImage` | Optional ARC sysroot image override; defaults to the release-tagged `build-tools` image staged into a named volume. |
| `container.dockerHost` | Docker daemon URI; uses `DOCKER_HOST` when omitted. |
| `container.dockerHostPathPrefix` | Bind-source path translation for layouts that need it; intentionally omitted by gh-aw's shared-work ARC path. |
| `network.isolation` | Docker-network egress isolation without host sudo/`NET_ADMIN`; the pinned schema labels the field experimental. |

gh-aw generates topology/isolation configuration and runtime Docker-endpoint arguments from frontmatter and environment; it intentionally omits path-prefix translation in its [ARC config builder](https://github.com/github/gh-aw/blob/main/pkg/workflow/awf_config_build.go). Standalone AWF users must configure their own daemon-visible paths, staging/sysroot, Docker endpoint, and isolation as described in [AWF ARC/DinD configuration](https://github.com/github/gh-aw-firewall/blob/main/docs/arc-dind.md). Do not copy AWF JSON fields into workflow frontmatter or infer product support from schema acceptance. AWF schema runtime options such as gVisor or Docker Sandboxes are not current gh-aw frontmatter runtime options.

For human sponsorship, independent code review, and evidence requirements, see
[Human Review and Compliance Patterns](/gh-aw/reference/compliance-patterns/).

## GitHub Enterprise Server (GHES) Compatibility

### GHES Compatibility Mode

GHES instances running versions that predate `@actions/artifact` v2.0.0 support cannot use `actions/upload-artifact@v4+` or `actions/download-artifact@v4+`. Attempting to run compiled workflows on these instances produces a `GHESNotSupportedError`.

gh-aw includes a GHES compatibility mode toggle (`aw.json` `ghes` or `gh aw compile --ghes`) for GHES releases that require the v3 artifact backend. Compatibility mode emits `upload-artifact@v3.2.2` and `download-artifact@v3.1.0`; default GitHub.com compilation continues to use the latest artifact actions.

Compatibility mode also omits compiler-generated workflow and job `cache-mode` fields, including dry-run fields. The [GitHub announcement](https://github.blog/changelog/2026-09-10-control-github-actions-cache-access-with-cache-mode/) establishes availability on GitHub.com, not a supported GHES release. Enable compatibility mode when the target server does not support this syntax, then recompile the workflows. This is the existing combined GHES switch, not an independent cache-only option or automatic server-version negotiation.

> [!WARNING]
> Omitting `cache-mode` removes the scoped-token cache restriction. Detection-enabled cache-memory restores, publisher event/validation gates, and event-conditioned dependency-cache actions remain. Dry-run still uses restore-only compiler-managed cache actions and omits the cache-memory publisher; it cannot constrain arbitrary cache writers through the scoped token on unsupported servers. See [cache access boundaries](/gh-aw/reference/cache-memory/#cache-access-boundaries).

The earlier “GHES 3.21.x and earlier / Actions Runner 2.327.1+” guidance refers to the v3 artifact-backend path and the actions' Node.js 24 runner requirement only. The `3.21.x` boundary has not been independently confirmed by a versioned product commitment; upstream [artifact-action GHES guidance](https://github.com/actions/upload-artifact#ghes-support) currently says v4+ is not supported on GHES. It is not a tested GHES release matrix, a minimum GHES release, or an end-to-end support commitment. Determine artifact-backend and workflow-syntax compatibility from your server's versioned Actions documentation; do not assume a later GHES release supports the v4 backend. Keep compatibility mode enabled when that backend or `cache-mode` syntax is unavailable.

#### Enable via `aw.json` (recommended)

Set `ghes: true` in `.github/workflows/aw.json` to apply GHES compatibility to every workflow compiled in the repository:

```json
{
  "ghes": true
}
```

#### Auto-detection with `gh aw init`

Running `gh aw init` inside a GHES repository automatically detects the deployment and writes `ghes: true` to `.github/workflows/aw.json`. No manual configuration is required.

#### Enable via CLI flag

Pass `--ghes` to `gh aw compile` for a one-off compilation without modifying `aw.json`:

```bash
gh aw compile --ghes my-workflow.md
```

> [!NOTE]
> The `--ghes` flag only affects the current compilation. Use `aw.json` to apply GHES compatibility permanently across all workflows in the repository.

## GitHub Enterprise Server CLI Setup

For `gh` CLI configuration, host authentication, and `GH_HOST` setup on GHES, see [GitHub Enterprise Server Support](/gh-aw/setup/cli/#github-enterprise-server-support) in the CLI reference.

## Centralized Activation Admission

Admission jobs are omitted by default. Enable both `admission` and `conclusion_admission` by setting `on.admission: true` in workflow frontmatter, or `"admission": true` in `.github/workflows/aw.json` for all workflows. Either setting enables the jobs; `on.admission: false` does not override a repository opt-in. Recompile after changing either setting.

```aw
on:
  workflow_dispatch:
  admission: true
```

Configure an OIDC-authenticated admission authority using organization or repository Actions variables: `GH_AW_DEFAULT_ADMISSION_URL`, `GH_AW_DEFAULT_ADMISSION_AUDIENCE`, and `GH_AW_DEFAULT_ADMISSION_ON_OUTAGE`. For opted-in workflows, the endpoint enables admission at runtime; an empty endpoint skips both jobs. Variables alone do not generate admission jobs. For enterprise-wide rollout, provision organization variables in each enrolled organization. Deployments must explicitly configure their issuer, accepted claims, and trusted workflow identities on the authority.

Approved admission supersedes daily-AIC validation and supplies the primary and optional component allowances used by the existing runtime credit limits. Omitted optional allowances retain their configured component limits. Disabled admission and ignored transient outages retain daily validation and existing limits. The default outage policy is `ignore`; select and protect `block` when availability must not bypass admission. Variables remain cooperative defaults, not mandatory organization policy. Reusable workflow callers must grant `id-token: write` to the caller job; see [enterprise environment controls](/gh-aw/reference/compiler-enterprise-environment-controls/) for the permission envelope and rollout requirements. See also the [admission service specification](/gh-aw/specs/admission-service-specification/).

The same configuration enables an isolated conclusion-admission job to post to `/api/conclusion` on the authority's origin. The ordinary conclusion job first publishes the full unified-session artifact, finishes any upload retry, and passes only bounded compact metadata and artifact IDs to the callback job. The callback obtains its own fresh OIDC token for the same audience; unrelated conclusion and safe-output steps do not receive this permission. The compact profile excludes agent conversation and tool content and is bounded to 128 records/16 KiB JSONL; the whole request is capped at 64 KiB. The authority can resolve the unchanged full session through authenticated, invocation-verified artifact reads. Failed artifact publication does not suppress compact delivery, but the request explicitly carries no IDs if neither upload produced one. Endpoint enrollment covers both compact delivery and protected full-session retrieval. Delivery failures warn without changing completed workflow results, even with outage policy `block`; the authority remains responsible for secure, deduplicated processing and retention.

## OIDC Token Broker

> [!WARNING]
> Token broker authentication is experimental. Configuration and behavior may change without deprecation. Compiling a workflow that enables either capability emits a warning.

> [!WARNING]
> Brokered Copilot inference is supported in threat-detection and evals jobs. Main-agent inference remains blocked pending a renewable credential provider in AWF. Enable task and request capabilities independently; task-only adoption does not change the workflow's inference authentication.

Enterprise administrators can distribute a broker endpoint through an organization Actions variable named `GH_AW_TOKEN_BROKER_URL`, with access limited to enrolled repositories. A repository Actions variable can override the organization value. An Actions secret with the same name is also supported; a nonempty variable takes precedence over the secret. For enterprise-wide deployment, provision the setting in each organization rather than assuming an enterprise Actions-variable scope.

Workflows must explicitly opt in:

```aw
token-broker:
  copilot-task: true
```

The compiler emits runtime references to the variable and secret, not the endpoint value. Setting the variable alone does not enable the feature. After opting in and recompiling, changing the endpoint does not require recompilation.

The broker contract defines independent authorization for `copilot-request` (Copilot inference) and `copilot-task` (coding-agent task creation and issue assignment). Enable either capability or both. Brokered Copilot inference is supported in threat-detection and evals jobs; each exchange identifies the gh-aw inference provider ID (currently `github`). Main agent inference still requires a renewable AWF credential provider. Do not combine `copilot-request` with `permissions.copilot-requests: write`. The broker validates GitHub's signature, issuer, expiration, audience, repository, workflow identity, and run claims against its enrollment policy; the audience is the configured broker URL. Broker-enabled consumer jobs run an unconditional post-step that requests invalidation scoped to the verified job identity. Restrict runner egress and protect the workflow and configuration that select this destination.

Broker-enabled jobs require `id-token: write`. Reusable workflow callers must grant that permission to the caller job. The runner must reach both the GitHub OIDC endpoint and the broker over HTTPS. Broker failures, including a missing endpoint, must not select a PAT, GitHub App token, or `GITHUB_TOKEN` as a fallback. Unrelated GitHub operations retain their existing authentication.

See the [Token Broker Specification](/gh-aw/specs/token-broker-specification/) for the server contract, identity validation, authorization, expiration, and conformance requirements. Admission remains a separate opt-in: the admission outage policy does not apply to credential exchange.

## Copilot Engine on GHES

For Copilot-specific prerequisites, licensing requirements, and firewall configuration on GHES, see [Copilot Engine Prerequisites on GHES](/gh-aw/troubleshooting/common-issues/#copilot-engine-prerequisites-on-ghes).
