---
title: Self-Hosted Runners
description: How to configure and run agentic workflows on self-hosted runners, ARC/Kubernetes, and GHES environments.
sidebar:
  order: 810
---

Use the `runs-on` frontmatter field to target a self-hosted runner instead of the default `ubuntu-latest`.

Runners must be Linux with Docker support. macOS and Windows are not supported.

Self-hosted runners may require `sudo` depending on the selected engine and configuration. For the default GitHub Copilot engine, there are two distinct sudo considerations:

- **AWF (Agentic Workflow Firewall)**: Runs rootless in the default network-isolation mode. Egress is enforced via Docker network topology — an internal Docker network (`awf-net`) with no internet route and a dual-homed Squid proxy as the sole egress path. No `sudo` and no `NET_ADMIN` are required on the runner for AWF in this mode. Container-level `iptables`, Squid proxy ACLs, and capability drops provide defense in depth, all managed inside the Docker daemon's domain.

- **Copilot CLI install**: The `install_copilot_cli.sh` script runs as the runner user. By default it escalates via `sudo` for file operations (fixing `.copilot` directory ownership, cleaning stale chroot directories, and installing the Copilot binary to `/usr/local/bin`). Pass `--rootless` to the script to install to `~/.local/bin` without `sudo`, which is required on ARC pods with `allowPrivilegeEscalation: false`.

## ARC with Docker-in-Docker (DinD)

For a complete ARC DinD setup walkthrough for GitHub Copilot coding agent, see [How to run GitHub Copilot coding agent on ARC with Docker-in-Docker](/gh-aw/reference/arc-dind-copilot-agent/).

Actions Runner Controller (ARC) deployments that use a Docker-in-Docker sidecar split the runner container and the Docker daemon container across separate filesystems.

Set `runner.topology: arc-dind` in workflow frontmatter for this environment.
Compiled workflows emit a runtime probe that inspects `DOCKER_HOST`.
Any `tcp://` endpoint (for example `tcp://localhost:2375`, `tcp://dind:2375`, or `tcp://172.30.0.5:2375`) is treated as ARC DinD, so ensure `DOCKER_HOST` points to the DinD daemon for that runner pod.

With ARC DinD handling enabled, AWF receives `--docker-host`, shared-work sysroot staging is applied, and chroot config patching is enabled. The runtime no longer uses `--docker-host-path-prefix`.

The topology applies to jobs that run on the ARC runner. The threat-detection job runs on `ubuntu-latest` unless `safe-outputs.threat-detection.runs-on` selects another runner, so it only inherits `runner.topology: arc-dind` when that override is set.

### Docker socket override for split-daemon topologies

When `DOCKER_HOST` is a TCP address (e.g., `tcp://localhost:2375`) and the Docker socket is mounted via a bind mount at a non-standard path, the MCP gateway needs explicit configuration to find the socket and determine its group ID.

Set these environment variables at the runner level (e.g., in your ARC runner pod spec or Kubernetes deployment):

- **`GH_AW_DOCKER_SOCK_PATH`** — absolute path to the bind-mounted Docker socket (e.g., `/dind-sock/docker.sock`)
- **`GH_AW_DOCKER_SOCK_GID`** — numeric group ID of the Docker socket (e.g., `999`)

**Example runner pod configuration:**

```yaml
env:
  - name: GH_AW_DOCKER_SOCK_PATH
    value: /dind-sock/docker.sock
  - name: GH_AW_DOCKER_SOCK_GID
    value: "999"
```

When both overrides are set, the MCP gateway will mount the specified socket path and add the specified group to the container without attempting automatic detection. If either variable is omitted, the gateway falls back to auto-detection from `DOCKER_HOST` and `stat`, and will fail with an actionable error if resolution fails.

## runs-on formats

**String** — single runner label:

```aw
---
on: issues
runs-on: self-hosted
---
```

**Array** — runner must have *all* listed labels (logical AND):

```aw
---
on: issues
runs-on: [self-hosted, linux, x64]
---
```

**Object** — named runner group, optionally filtered by labels:

```aw
---
on: issues
runs-on:
  group: my-runner-group
  labels: [linux, x64]
---
```

The string, array, and object forms are supported by the top-level `runs-on`, `runs-on-slim`, `safe-outputs.runs-on`, `safe-outputs.threat-detection.runs-on`, and custom `safe-outputs.jobs.<job>.runs-on` fields.

## Sharing configuration via imports

`runs-on` must be set in each workflow — it is not merged from imports. Other settings like `network` and `tools` can be shared:

```aw title=".github/workflows/shared/runner-config.md"
---
network:
  allowed:
    - defaults
    - private-registry.example.com
tools:
  bash: {}
---
```

```aw
---
on: issues
imports:
  - shared/runner-config.md
runs-on: [self-hosted, linux, x64]
---

Triage this issue.
```

## Configuring the detection job runner

When [threat detection](/gh-aw/reference/threat-detection/) is enabled, the detection job runs on the agent job's runner by default. Override it with `safe-outputs.threat-detection.runs-on`:

```aw
---
on: issues
runs-on: [self-hosted, linux, x64]
safe-outputs:
  create-issue: {}
  threat-detection:
    runs-on: [self-hosted, linux, x64]
---
```

This is useful when your self-hosted runner lacks outbound internet access for AI detection, or when you want to run the detection job on a cheaper runner.

## Configuring the framework job runner

Framework jobs — activation, pre-activation, safe-outputs, unlock, APM, update_cache_memory, and push_repo_memory — default to `ubuntu-slim`. Use `runs-on-slim:` to override all of them at once:

```aw
---
on: issues
runs-on: [self-hosted, linux, x64]
runs-on-slim: [self-hosted, linux, x64]
safe-outputs:
  runs-on: [self-hosted, linux, x64]
  create-issue: {}
---
```

> [!NOTE]
> `runs-on` controls only the main agent job. `runs-on-slim` controls all framework/generated jobs. `safe-outputs.runs-on` still takes precedence over `runs-on-slim` for safe-output jobs specifically.
> `runs-on-slim` accepts the same string, array, or runner-group object forms as `runs-on`.

## Configuring the maintenance workflow runner

The generated `agentics-maintenance.yml` workflow defaults to `ubuntu-slim` for all its jobs. To use a self-hosted runner for maintenance jobs, set `runs_on` in `.github/workflows/aw.json`:

**Single label:**

```json
{
  "maintenance": {
    "runs_on": "self-hosted"
  }
}
```

**Multiple labels** (runner must match all):

```json
{
  "maintenance": {
    "runs_on": ["self-hosted", "linux", "x64"]
  }
}
```

This setting applies to every job in `agentics-maintenance.yml` (close-expired-entities, cleanup-cache-memory, run_operation, apply_safe_outputs, create_labels, validate_workflows, and activity_report). Re-run `gh aw compile` after changing `aw.json` to regenerate the workflow.

> [!NOTE]
> `aw.json` is separate from individual workflow frontmatter. It provides repository-level settings for generated infrastructure workflows.

## Action and container substitutions (`aw.json`)

Enterprises running in private clouds or air-gapped environments can redirect action and container image references to internal mirrors using `action_pins` and `container_pins` in `.github/workflows/aw.json`. These substitutions are applied at compile time and baked into the generated `.lock.yml` files, so workflows never reference unreachable public registries at runtime.

### Action substitutions (`action_pins`)

`action_pins` maps `owner/repo@ref` source references to replacement `owner/repo@ref` values before pin resolution. The rest of the resolution pipeline (cache → GitHub API → embedded pins) operates on the mapped target.

```json title=".github/workflows/aw.json"
{
  "action_pins": {
    "actions/checkout@v4": "acme-corp/checkout-mirror@v4",
    "actions/setup-node@v4": "acme-corp/setup-node-mirror@v4"
  }
}
```

Keys and values must use the `owner/repo@ref` format. Each source version must be mapped individually — wildcard and prefix matching are not supported. A console message is emitted once per applied mapping during compilation.

### Container image substitutions (`container_pins`)

`container_pins` maps source container image references to replacement image targets. The mapping is applied before digest-pin resolution, so a privately mirrored image can be used in place of the public source. Each value is an object with separate `image` and `digest` fields for independent validation:

```json title=".github/workflows/aw.json"
{
  "container_pins": {
    "ghcr.io/github/gh-aw-firewall:0.27.22": {
      "image": "registry.acme.com/gh-aw-firewall:0.27.22",
      "digest": "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
    },
    "node:lts-alpine": {
      "image": "registry.acme.com/node:lts-alpine",
      "digest": "sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
    }
  }
}
```

Keys are source image references as they appear in compiled workflows. `image` must be a valid image reference without a digest, and `digest` must be a full `sha256:<64 lowercase hex characters>` digest.

### AWF infrastructure image overrides (`sandbox.agent.images`)

Use `container_pins` for general container substitutions, and use `sandbox.agent.images` when replacing AWF infrastructure roles themselves. This is the supported path for self-hosted and air-gapped setups that must mirror and approve AWF sidecar images with explicit digests.

```aw wrap
sandbox:
  agent:
    version: v0.28.8
    images:
      squid: registry.example.com/approved/squid:v0.28.8@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
      agent: registry.example.com/approved/agent:v0.28.8@sha256:1123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
      apiProxy: registry.example.com/approved/api-proxy:v0.28.8@sha256:2123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
```

> [!NOTE]
> The digest values in this example are placeholders. Replace them with the exact digests from your approved registry images.

When this manifest is set, AWF uses these references as authoritative runtime role images. Keep values literal and digest-pinned; expressions are rejected at compile time. For role requirements and constraints (for example `cliProxy` with `tools.github.mode: gh-proxy`, or `buildTools` with `runner.topology: arc-dind`), see [Sandbox custom infrastructure images](/gh-aw/reference/sandbox/#custom-infrastructure-images-sandboxagentimages).

### Combined example

```json title=".github/workflows/aw.json"
{
  "action_pins": {
    "actions/checkout@v4": "acme-corp/checkout-mirror@v4"
  },
  "container_pins": {
    "ghcr.io/github/gh-aw-firewall:0.27.22": {
      "image": "registry.acme.com/gh-aw-firewall:0.27.22",
      "digest": "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
    }
  }
}
```

Re-run `gh aw compile` after modifying `aw.json` to regenerate all affected lock files.

> [!NOTE]
> Neither `action_pins` nor `container_pins` is supported in individual workflow frontmatter. Both are repository-level settings in `aw.json` that apply across all workflows in the repository.

## Learn More

- [Frontmatter](/gh-aw/reference/frontmatter/#run-configuration-run-name-runs-on-runs-on-slim-timeout-minutes) — `runs-on` and `runs-on-slim` syntax reference
- [Threat Detection](/gh-aw/reference/threat-detection/) — detection job configuration
- [Network Access](/gh-aw/reference/network/) — configuring outbound network permissions
- [Sandbox](/gh-aw/reference/sandbox/) — container and Docker requirements
- [Ephemerals](/gh-aw/reference/ephemerals/#maintenance-configuration) — full `aw.json` maintenance configuration reference
- [Enterprise Configuration](/gh-aw/reference/enterprise-configuration/) — custom API endpoints for GHEC/GHES

## GitHub Official Documentation

These links point to the official GitHub documentation for self-hosted runners and related infrastructure:

### Self-hosted runners

- [About self-hosted runners](https://docs.github.com/en/actions/hosting-your-own-runners/managing-self-hosted-runners/about-self-hosted-runners) — overview, system requirements, and supported architectures
- [Adding self-hosted runners](https://docs.github.com/en/actions/hosting-your-own-runners/managing-self-hosted-runners/adding-self-hosted-runners) — step-by-step setup for repository, organization, or enterprise
- [Using self-hosted runners in a workflow](https://docs.github.com/en/actions/hosting-your-own-runners/managing-self-hosted-runners/using-self-hosted-runners-in-a-workflow) — how to target self-hosted runners with `runs-on`
- [Configuring the self-hosted runner application as a service](https://docs.github.com/en/actions/hosting-your-own-runners/managing-self-hosted-runners/configuring-the-self-hosted-runner-application-as-a-service) — running the runner as a background service
- [Monitoring and troubleshooting self-hosted runners](https://docs.github.com/en/actions/hosting-your-own-runners/managing-self-hosted-runners/monitoring-and-troubleshooting-self-hosted-runners) — runner logs, diagnostics, and connectivity checks

### Labels and runner groups

- [Using labels with self-hosted runners](https://docs.github.com/en/actions/hosting-your-own-runners/managing-self-hosted-runners/using-labels-with-self-hosted-runners) — default and custom labels for routing jobs
- [Managing access to self-hosted runners using groups](https://docs.github.com/en/actions/hosting-your-own-runners/managing-self-hosted-runners/managing-access-to-self-hosted-runners-using-groups) — runner groups for organization and enterprise

### Actions Runner Controller (ARC)

- [About Actions Runner Controller](https://docs.github.com/en/actions/hosting-your-own-runners/managing-self-hosted-runners-with-actions-runner-controller/about-actions-runner-controller) — overview of the Kubernetes-based autoscaling solution
- [Quickstart for Actions Runner Controller](https://docs.github.com/en/actions/hosting-your-own-runners/managing-self-hosted-runners-with-actions-runner-controller/quickstart-for-actions-runner-controller) — getting started with ARC on Kubernetes
- [Deploying runner scale sets with Actions Runner Controller](https://docs.github.com/en/actions/hosting-your-own-runners/managing-self-hosted-runners-with-actions-runner-controller/deploying-runner-scale-sets-with-actions-runner-controller) — Helm-based deployment of runner scale sets
- [Authenticating to the GitHub API for Actions Runner Controller](https://docs.github.com/en/actions/hosting-your-own-runners/managing-self-hosted-runners-with-actions-runner-controller/authenticating-to-the-github-api) — PAT and GitHub App authentication for ARC

### Security

- [Security hardening for GitHub Actions](https://docs.github.com/en/actions/security-guides/security-hardening-for-github-actions) — best practices for secrets, permissions, and self-hosted runner security
- [About security hardening with OpenID Connect](https://docs.github.com/en/actions/deployment/security-hardening-your-deployments/about-security-hardening-with-openid-connect) — using OIDC tokens instead of long-lived credentials

## Runner environment requirements

Self-hosted runners must meet these requirements for agentic workflows to run reliably.

### Docker

A working Docker daemon is required. The MCP gateway and sandbox run as containers.

- **Unix socket**: Docker must be accessible via a Unix socket (typically `/var/run/docker.sock`). If `DOCKER_HOST` is unset, the gateway mounts `/var/run/docker.sock`. If `DOCKER_HOST` is `unix://...` or a bare absolute path, the gateway mounts that socket path. Other schemes (for example `tcp://...`) are ignored for mounts and default back to `/var/run/docker.sock`.
- **Docker group**: The runner user must be in the `docker` group, or the socket must be world-readable.
- **ARC/Kubernetes**: Docker-in-Docker (DinD) is **required** for ARC. Set `containerMode.type="dind"` in your ARC Helm configuration. The `containerMode.type="kubernetes"` mode is not supported. The dind sidecar must share the Docker socket via an `emptyDir` volume, and the gateway retries the socket check for up to 10 seconds to handle startup race conditions. See [How to run GitHub Copilot coding agent on ARC with Docker-in-Docker](/gh-aw/reference/arc-dind-copilot-agent/) for the complete setup guide, and [ARC (Actions Runner Controller)](#arc-actions-runner-controller) below for pod security details.
- **Split-daemon override**: On ARC or other split-daemon topologies where the socket path or group ID cannot be auto-detected, set `GH_AW_DOCKER_SOCK_PATH` and `GH_AW_DOCKER_SOCK_GID` environment variables at the runner level. See [Docker socket override for split-daemon topologies](#docker-socket-override-for-split-daemon-topologies) for details.

### Node.js

Node.js is required for gh-aw framework scripts (`start_safe_outputs_server.sh`, `start_mcp_scripts_server.sh`, and related scripts) that invoke `node` directly.

**Standard GitHub-hosted runners** (`ubuntu-latest`, `ubuntu-22.04`, `ubuntu-24.04`, etc.) have Node.js pre-installed — no additional configuration is required.

**Self-hosted and GPU runners** may not have Node.js on `PATH`. To prevent cryptic mid-run failures, the compiler automatically emits an [`actions/setup-node`](https://github.com/actions/setup-node) step (Node.js 24) at the start of the agent job whenever a non-standard runner is detected. This step runs before any agent or framework scripts and fails fast with a clear error if Node.js cannot be installed.

To pin a specific Node.js version, use `runtimes:` in your workflow frontmatter:

```aw
---
on: issues
runs-on: self-hosted
runtimes:
  node:
    version: '22'
---
```

> [!NOTE]
> The automatic Node.js setup is emitted only for non-standard runners. If your self-hosted runner already has Node.js installed and on `PATH`, the `actions/setup-node` step still runs but is a no-op when the requested version is already present.

### Filesystem

- **Use `RUNNER_TEMP` for transient state.** Put sandbox state, tool downloads, and intermediate outputs in `$RUNNER_TEMP`, which is cleaned between jobs. On shared runners, avoid writing arbitrary workflow data to `/tmp` because it can persist across jobs.
- **No root assumption.** Tool installs, file operations, and sandbox setup should run as the unprivileged runner user. The Copilot CLI install script escalates via `sudo` for specific operations; see the sudo requirements above.
- **No global installs.** Do not install packages to `/usr/local/`, `/opt/hostedtoolcache/`, or other system-wide paths. These may be read-only, shared across runners, or bind-mounted read-only inside the sandbox. Use job-scoped writable locations instead.
- **No hardcoded `HOME` paths.** The runner's home directory may not be `/home/runner`. Use `$HOME` or `$RUNNER_TEMP` instead of hardcoded paths.

### Post-job cleanup

Self-hosted runners persist between jobs. Agentic workflows should clean up after themselves:

- Files written to `$RUNNER_TEMP` are automatically cleaned.
- Docker containers on the `awf-net` bridge are stopped and removed by the sandbox teardown.
- If your workflow creates files outside `$RUNNER_TEMP` (e.g. in `$GITHUB_WORKSPACE`), the runner's built-in workspace cleanup handles this.

### Network

Self-hosted runners need outbound HTTPS access to:

- `api.githubcopilot.com` (or your enterprise Copilot endpoint)
- `github.com` (or your GHES instance)
- `ghcr.io` (to pull the MCP gateway container image)
- Any domains listed in your workflow's `network.allowed` configuration

## GHES (GitHub Enterprise Server)

Agentic workflows can run on GHES with some additional configuration.

### GHES compatibility mode

GHES does not support the `@actions/artifact` v2.0.0+ backend used by `upload-artifact@v4+` and `download-artifact@v4+`. Compiled workflows use the latest artifact action versions by default, which fail on GHES with `GHESNotSupportedError`.

Enable GHES compatibility mode in `.github/workflows/aw.json` to compile with GHES mode explicitly enabled:

```json
{
  "ghes": true
}
```

Or compile with `--ghes` for one-off workflow generation:

```bash
gh aw compile --ghes my-workflow.md
```

Compatibility mode emits `upload-artifact@v3.2.2` and `download-artifact@v3.1.0`, which use the artifact backend supported by GHES. Default GitHub.com compilation continues to use the latest artifact actions.

This path supports GHES 3.21.x and earlier when the workflow runs on Actions Runner 2.327.1 or later, which is required by the Node.js 24 runtime used by these pinned artifact actions. Keep compatibility mode enabled on later releases until the instance supports the v4 artifact backend.

### API endpoint

GHES instances need the `api-target` engine configuration. See [Enterprise Configuration](/gh-aw/reference/enterprise-configuration/) for full setup instructions.

```aw
---
engine:
  id: copilot
  api-target: api.enterprise.githubcopilot.com
network:
  allowed:
    - defaults
    - github.company.com
    - api.enterprise.githubcopilot.com
---
```

## ARC (Actions Runner Controller)

GitHub Copilot coding agent **requires** Docker-in-Docker (DinD) mode on ARC. Set `containerMode.type="dind"` in your ARC Helm configuration. The `containerMode.type="kubernetes"` mode is not supported.

Set `runner.topology: arc-dind` in workflow frontmatter to enable ARC DinD split-filesystem handling. See the [ARC with Docker-in-Docker (DinD)](#arc-with-docker-in-docker-dind) section above and the [ARC DinD setup guide](/gh-aw/reference/arc-dind-copilot-agent/) for a complete walkthrough.

### Docker-in-Docker (dind) sidecar

The MCP gateway:

1. Resolves the Docker socket path from `DOCKER_HOST` (supports `unix://` paths and bare absolute paths)
2. Auto-detects the socket's group ID for correct permissions
3. Retries the socket check for up to 10 seconds to handle the race condition where the gateway starts before `dockerd`

### Pod security

The dind sidecar requires `privileged: true` so `dockerd` can run. The runner container does **not** need `privileged: true` or `NET_ADMIN`.

In network-isolation mode (the default for `topology: arc-dind`), AWF enforces egress via Docker network topology — an internal Docker network with no internet route and a dual-homed Squid proxy. All network enforcement happens inside the Docker daemon's domain (the dind sidecar). The runner container only issues Docker API commands via the socket; it never manipulates host `iptables` or network namespaces.

> [!NOTE]
> If your cluster enforces `allowPrivilegeEscalation: false` or `no-new-privileges` on the runner container, pass `--rootless` to the Copilot CLI install script so it installs to `~/.local/bin` without `sudo`. See [Pod security and rootless install](/gh-aw/reference/arc-dind-copilot-agent/#pod-security-and-rootless-install) in the ARC DinD guide.
