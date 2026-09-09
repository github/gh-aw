---
title: Agent Runtime Selection
description: Choose and configure Docker, gVisor, Docker sbx, Cloud Hypervisor, or ARC DinD for an agentic workflow, with runner requirements and troubleshooting guidance.
sidebar:
  order: 1340
---

Agentic workflows use AWF (Agent Workflow Firewall) to run the agent in an isolated environment. The environment can use the runner's standard Docker runtime, gVisor, Docker sbx, or preview Cloud Hypervisor mode. ARC DinD is a runner topology that changes how the standard Docker environment is reached; it is not another value of `sandbox.agent.runtime`. The `gvisor` and `docker-sbx` runtimes are deprecated and will be removed in a future release; use `docker` instead.

Use this page when selecting a runtime, writing workflow frontmatter, provisioning a runner, or diagnosing a runtime setup failure.

## Runtime and topology fields

These similarly named fields control different layers:

| Field | Purpose | Values covered here |
| --- | --- | --- |
| `sandbox.agent.runtime` | Selects the isolation backend for the main agent | `docker`, `docker-sudo-iptables`, `gvisor` (deprecated), `docker-sbx` (deprecated), `cloud-hypervisor`, or omitted for Docker |
| `sandbox.agent.runtime-install` | Controls whether gh-aw installs and prepares gVisor or Docker sbx | `true` by default; `false` for a pre-provisioned runner |
| `runner.topology` | Describes how the runner reaches Docker | `arc-dind`, or omitted for a local Docker daemon |
| `runtimes` | Installs language toolchains such as Node.js, Python, and Go | Unrelated to agent isolation |

## Choose a runtime

| Choice | Isolation boundary | Runner requirements | Main tradeoff |
| --- | --- | --- | --- |
| Docker | Linux namespaces, cgroups, and the host kernel | Linux and a usable Docker daemon | Fastest and most compatible, but the agent shares the host kernel |
| gVisor | A `runsc` user-space kernel between the agent and host kernel | Local Docker daemon, `sudo`, systemd, and access to gVisor downloads | Stronger kernel isolation with syscall compatibility and performance overhead |
| Docker sbx | A KVM-backed microVM for the agent | KVM, nested virtualization, `sudo`, apt, Docker Hub credentials, and local Docker | Strongest boundary here, but has the most setup cost and platform constraints |
| Cloud Hypervisor (preview) | A KVM-backed microVM for the agent | GitHub-hosted Ubuntu x86_64 runner with `/dev/kvm` and AWF release asset download access | Preview-only path with strict host requirements and release-asset provisioning |
| ARC DinD | Standard Docker agent container in a DinD sidecar | ARC or equivalent Kubernetes runner with a privileged DinD sidecar and shared work volume | Supports Kubernetes runner fleets, but adds split-filesystem and daemon-connectivity complexity |

Apply this selection order:

1. Use **ARC DinD** when the runner is an ARC pod or another Kubernetes runner whose Docker daemon is a DinD sidecar. Do not combine it with gVisor or Docker sbx.
2. Otherwise, use **Docker sbx** when the user requires a hardware-virtualized boundary and the runner exposes working KVM.
3. Use **Cloud Hypervisor (preview)** only when the runtime must be Cloud Hypervisor and the runner is GitHub-hosted Ubuntu x86_64 with `/dev/kvm`.
4. Otherwise, use **gVisor** when untrusted agent code warrants a smaller host-kernel attack surface and the workload is compatible with `runsc`.
5. Use the default **Docker** runtime when compatibility, startup time, or runner portability is more important than an additional kernel or VM boundary.

If the user's requirement is unclear, prefer Docker. Do not select a stronger runtime until the runner prerequisites are known to be available.

## Requirements shared by all choices

The main agent job requires a Linux runner. macOS and Windows runners are not supported. The runner must have enough CPU, memory, and disk for the agent, AWF, the MCP gateway, proxy containers, and any configured MCP servers.

Docker must be reachable by the runner user. On a conventional runner, this normally means that `/var/run/docker.sock` exists and the runner user can access it. Verify the baseline before investigating a specialized runtime:

```bash
docker version
docker info
docker compose version
docker run --rm hello-world
```

The runner also needs outbound HTTPS access to GitHub, the selected AI provider, `ghcr.io`, and the domains required by setup steps and `network.allowed`. See [Self-Hosted Runners](/gh-aw/reference/self-hosted-runners/) for the complete runner baseline.

Compile after changing frontmatter:

```bash
gh aw compile
```

Compilation catches unsupported field values and known incompatible combinations before a workflow runs.

## Docker

Docker is the default. Leave `sandbox.agent.runtime` unset:

```aw wrap
---
on: issues
sandbox:
  agent:
    id: awf
---

Investigate this issue.
```

The entire `sandbox` block may be omitted when its defaults are sufficient. AWF still provides network isolation and proxy enforcement; "Docker" does not mean that the agent runs without a sandbox.

### Docker runner requirements

A conventional Docker runner needs:

- A Linux Docker Engine that the runner user can access.
- Docker Compose support.
- A daemon on the same filesystem as the runner, unless `runner.topology: arc-dind` is configured.
- Outbound access to pull the pinned AWF, proxy, gateway, and MCP images.

For a self-hosted runner, avoid a remote TCP Docker daemon unless the environment is intentionally configured as a split-daemon topology. Bind-mount source paths are resolved by the daemon, not by the client, so a remote daemon that cannot see the runner workspace causes missing-workspace and mount errors.

### Docker tradeoffs

Docker has the lowest startup overhead and the broadest compatibility with build tools, debuggers, filesystem operations, and syscalls. The agent container shares the runner host's kernel, so a kernel vulnerability presents a larger escape surface than gVisor or Docker sbx.

### Docker troubleshooting

**`permission denied` while connecting to Docker:** Confirm the socket path and permissions with `docker context show`, `printf '%s\n' "${DOCKER_HOST:-<unset>}"`, and `stat /var/run/docker.sock`. Add the runner user to the correct socket group as part of runner provisioning rather than changing permissions in workflow steps.

**`Cannot connect to the Docker daemon`:** Confirm the daemon is running and that `DOCKER_HOST` points to the intended endpoint. If `DOCKER_HOST` is a `tcp://` address for a DinD sidecar, configure `runner.topology: arc-dind`.

**The agent sees an empty workspace:** The Docker daemon cannot resolve the runner-side bind-mount source. Use a local daemon, or configure the runner as ARC DinD with a shared work volume.

**Image pull failures:** Check registry reachability, rate limits, proxy configuration, and any repository-level `container_pins` substitutions.

## gVisor

gVisor runs only the agent container under the `runsc` OCI runtime. AWF's infrastructure containers continue to use standard Docker.

```aw wrap
---
on: issues
sandbox:
  agent:
    id: awf
    runtime: gvisor
---

Investigate this issue.
```

### gVisor runner requirements

The generated setup step:

1. Detects `x86_64` or `aarch64` with `uname -m`.
2. Downloads pinned `runsc` and `containerd-shim-runsc-v1` binaries and their SHA-512 files from `storage.googleapis.com/gvisor`.
3. Verifies both checksums.
4. Uses `sudo` to install the binaries under `/usr/local/bin`.
5. Runs `sudo runsc install` and `sudo systemctl restart docker`.
6. Verifies the runtime with `docker run --rm --runtime=runsc hello-world`.

The runner therefore needs:

- A supported Linux architecture and a Docker Engine managed by systemd.
- Passwordless, non-interactive `sudo` for the runner user.
- Permission to modify Docker's runtime configuration and restart Docker.
- Outbound HTTPS access to `storage.googleapis.com/gvisor` and the registry serving `hello-world`.
- AWF `v0.27.30` or newer. The repository default is newer; this matters when `firewall.version` or `sandbox.agent.version` is pinned.

> [!IMPORTANT]
> Host-level `sudo` is required by the generated gVisor installation step, but the agent itself keeps running rootless under AWF network isolation. The compiler derives these privileges from `sandbox.agent.runtime: gvisor`; there is no separate `sudo` field to set.

Set `runtime-install: false` when the runner image already contains a working, Docker-registered `runsc` runtime:

```aw wrap
---
sandbox:
  agent:
    id: awf
    runtime: gvisor
    runtime-install: false
---
```

This skips the generated download, checksum, installation, Docker registration, restart, and smoke-test step. The runner no longer needs workflow-time `sudo`, systemd, or access to the gVisor download host, but `docker info` must already list `runsc`. If any imported workflow sets `runtime-install: false`, false wins during import merging.

gVisor cannot be combined with `runner.topology: arc-dind`. The generated installer must register `runsc` with the same Docker daemon that starts the agent and restart that daemon through systemd. An ARC runner cannot perform those operations against its DinD sidecar.

### gVisor tradeoffs

gVisor substantially reduces the host-kernel syscall surface exposed to agent code. It does not require KVM and is lighter than a microVM.

The user-space kernel adds CPU, syscall, filesystem, and network overhead. Some low-level workloads can fail because they depend on an unsupported syscall, privileged operation, device, kernel module, eBPF behavior, unusual `/proc` or `/sys` semantics, or exact host-kernel behavior. Prefer Docker for kernel-sensitive build and test workloads unless the stronger boundary is required.

### gVisor troubleshooting

**Download or checksum failure:** Confirm access to `storage.googleapis.com/gvisor`, inspect proxy or TLS interception, and verify that the runner architecture is reported as `x86_64` or `aarch64`.

**`sudo` prompts or fails:** Provision passwordless `sudo` for the runner service account. No frontmatter field can grant host permissions or fix the installer.

**`systemctl: command not found` or Docker is not a systemd service:** The runner image is incompatible with the generated installer. Use a conventional systemd-based runner, pre-provision and maintain gVisor outside the workflow only if the generated setup remains compatible, or select Docker.

**`unknown or invalid runtime name: runsc`:** Run `docker info` and confirm `runsc` appears in the runtimes map. Check the output of `runsc install`, confirm Docker was restarted rather than reloaded, and confirm the workflow is not using a different or remote Docker daemon.

**The runtime installs but the agent still uses Docker:** Remove an AWF pin older than `v0.27.30`, or update `firewall.version` or `sandbox.agent.version` to `v0.27.30` or newer and recompile.

**Tests fail only under gVisor:** Treat this as a compatibility issue if the failure involves syscalls, devices, namespaces, tracing, eBPF, or kernel-specific filesystem behavior. Reproduce with `docker run --runtime=runsc ...` and compare it with the same image under standard Docker.

## Docker sbx

Docker sbx runs the agent in a KVM-backed microVM. The MCP gateway, Squid proxy, API proxy, and other infrastructure containers remain on the Docker host.

```aw wrap
---
on: issues
sandbox:
  agent:
    id: awf
    runtime: docker-sbx
---

Investigate this issue.
```

Add these Actions secrets to the repository or organization:

| Secret | Purpose |
| --- | --- |
| `DOCKER_USERNAME` | Docker Hub account used by both the Docker and sbx CLIs |
| `DOCKER_PAT` | Docker Hub personal access token used to pull the sandbox template |

`DOCKER_PAT` is required for Docker sbx, including when `runtime-install: false`, because the compiled workflow refreshes sbx credentials immediately before agent execution. Use a Docker Hub personal access token rather than a password, and make sure it can pull `docker/sandbox-templates:shell-docker`.

### Docker sbx runner requirements

The runner needs:

- Linux with the KVM module loaded and `/dev/kvm` exposed.
- Nested virtualization when the runner itself is a virtual machine.
- Passwordless, non-interactive `sudo`.
- An apt-based distribution on which the official Docker repository and `docker-sbx` package can be installed.
- Docker Engine and the Docker CLI.
- Docker Hub credentials with access to `docker/sandbox-templates:shell-docker`.
- Outbound HTTPS access to `get.docker.com`, Docker's apt repository, and Docker Hub.
- AWF `v0.27.30` or newer.

The compiler generates fail-fast KVM and secret checks, installs `docker-sbx`, changes `/dev/kvm` permissions, starts the sbx daemon, authenticates both CLIs, initializes an allow-all sbx policy, pulls the template, and runs a create/exec/remove smoke test. It refreshes sbx credentials again immediately before AWF starts the agent.

These installation steps require passwordless host `sudo`; the compiler derives that requirement from `sandbox.agent.runtime: docker-sbx`. AWF itself still runs rootless with network isolation enabled for Docker sbx.

Set `runtime-install: false` when the runner image already has Docker sbx, a working sbx daemon and policy, KVM access, and the required template:

```aw wrap
---
sandbox:
  agent:
    id: awf
    runtime: docker-sbx
    runtime-install: false
---
```

This skips the generated KVM check, secret check, package installation, daemon setup, template pull, and pre-flight smoke test. The runner must already satisfy those checks; gh-aw does not verify them when installation is disabled. The credential-refresh step still runs immediately before agent execution, so `DOCKER_USERNAME` and `DOCKER_PAT` remain required. If any imported workflow sets `runtime-install: false`, false wins during import merging.

Docker sbx cannot be combined with `runner.topology: arc-dind`. ARC DinD normally does not expose nested KVM, and the sbx daemon must run on the runner host rather than inside the DinD sidecar.

### Docker sbx tradeoffs

Docker sbx provides the strongest isolation boundary among these choices because agent code runs behind a hardware-virtualized guest kernel. It is appropriate when agent code is highly untrusted and the runner platform supports KVM.

It has the highest cold-start cost, consumes more memory and disk, requires Docker Hub credentials, changes `/dev/kvm` permissions for the job, and adds an sbx daemon and template lifecycle. The microVM boundary can also expose path, networking, CLI installation, and terminal behavior differences. For example, gh-aw omits TTY mode for Docker sbx because sbx TTY execution can terminate long-running sessions prematurely.

### Docker sbx troubleshooting

**`KVM kernel module is not loaded` or `/dev/kvm is missing`:** The runner does not provide hardware virtualization. Enable nested virtualization and pass `/dev/kvm` through to the runner, or select gVisor or Docker. Frontmatter cannot add KVM capability. With `runtime-install: false`, this generated check is skipped, but sbx execution still fails if KVM is unavailable.

**Permission denied for `/dev/kvm`:** With runtime installation enabled, confirm the runner can execute passwordless `sudo` and that its security policy permits the generated `chmod 666 /dev/kvm`. With `runtime-install: false`, gh-aw does not run that `chmod`, so provision the runner image or pod so the runner user can access `/dev/kvm` before the workflow starts. If neither access model is acceptable, do not use Docker sbx.

**`DOCKER_PAT` or `DOCKER_USERNAME` is empty:** Define both Actions secrets in the scope available to the workflow. Secrets are not passed to workflows triggered from untrusted forks, so Docker sbx is unsuitable for such runs unless the trigger and credential model are changed safely.

**`Unable to locate package docker-sbx`:** Confirm the runner is apt-based, `curl https://get.docker.com` can configure the Docker repository, and the package is available for the runner architecture and distribution.

**The sbx daemon does not become ready:** Inspect `/tmp/sbx-daemon.log`, then run `sbx daemon status`. Check stale daemon processes, policy state, KVM access, and whether the runner permits the daemon to create its required resources. With `runtime-install: false`, gh-aw does not start or initialize the daemon; runner provisioning must do so.

**`user is not authenticated to Docker`:** Confirm the PAT is valid for Docker Hub and both `docker login` and `sbx login` succeed. Current compiled workflows refresh sbx credentials immediately before execution; upgrade gh-aw and recompile if the refresh step is absent from the lock file.

**Template pull fails:** Test `docker pull docker/sandbox-templates:shell-docker` with the same credentials. Check Docker Hub access, account entitlements, proxy behavior, and rate limits.

**Pre-flight smoke test fails:** The generated test creates `test-sandbox-direct`, executes `uname -a`, and removes it. Inspect the first failing `sbx create`, `sbx exec`, or `sbx stop` command before investigating AWF because the failure is below the workflow firewall layer.

**The CLI is missing inside the microVM:** Upgrade gh-aw and recompile. Docker sbx requires engine CLIs to be staged under `${RUNNER_TEMP}/gh-aw/engine-cli`, which is visible to the microVM.

## Cloud Hypervisor (preview)

Cloud Hypervisor runs the agent in AWF's preview microVM runtime:

```aw wrap
---
on: issues
sandbox:
  agent:
    id: awf
    runtime: cloud-hypervisor
---

Investigate this issue.
```

Preview scope is intentionally narrow:

- GitHub-hosted runners only (`RUNNER_ENVIRONMENT=github-hosted`).
- Ubuntu Linux x86_64 only (`RUNNER_OS=Linux`, `RUNNER_ARCH=X64`, `ImageOS=ubuntu*`).
- `/dev/kvm` must be present, and the host must have `gh`, `rsync`, and Docker (with a running Docker Engine), a usable cgroup v2 hierarchy, and a kernel that reports Landlock LSM support.
- `runner.topology: arc-dind` is not supported.
- `tools.github.mode: gh-proxy` and the `integrity-reactions` feature are not supported: the CLI proxy sidecar is not attached to the isolated topology.
- `sandbox.agent.allow-host-ports` and GitHub Actions `services:` with published ports are not supported: host access requires `sandbox.agent.runtime: docker-sudo-iptables`.
- `enclaves` configuration is not supported.

The compiler grants only the runner user read/write access to `/dev/kvm`, then emits host preflight and release-asset provisioning steps before AWF runs. Host preflight fails closed unless `/dev/kvm`, `gh`, `rsync`, Docker, a usable cgroup v2 hierarchy, and Landlock LSM support are all present. Provisioning downloads the Cloud Hypervisor guest archive together with its attested `manifest.json` and Sigstore `manifest.sigstore.jsonl` bundle from the pinned `gh-aw-firewall` release, validates the release identity, artifact names, and bundle structure, and feeds AWF the manifest path, bundle path, and exact release tag so AWF can perform the authoritative offline Sigstore/provenance verification for the Cloud Hypervisor binary, `virtiofsd`, kernel, rootfs, and supervisor.

AWF launches with host privileges required to create the VM, but the runtime remains in strict network-isolation mode. The guest defaults to 2 vCPUs and 4096 MiB of memory. Its trusted topology attachment is limited to the MCP gateway on TCP 8080; the CLI proxy is not attached.

> [!IMPORTANT]
> This runtime is preview-only. Keep expectations aligned with AWF preview support and prefer Docker sbx or gVisor when Cloud Hypervisor host constraints are not guaranteed.

## ARC with Docker-in-Docker

ARC DinD describes a split-daemon runner: the GitHub Actions runner is one container and Docker runs in a privileged sidecar. The agent still uses standard Docker, so omit `sandbox.agent.runtime`.

```aw wrap
---
on: issues
runs-on: arc-runner-set
runner:
  topology: arc-dind
---

Investigate this issue.
```

Do not set `runtime: gvisor`, `runtime: docker-sbx`, or `runtime: cloud-hypervisor` in this configuration. Omit `runtime` or set `runtime: docker`; both select the same Docker profile.

### ARC DinD runner requirements

The ARC or equivalent Kubernetes pod needs:

- `containerMode.type="dind"` or an equivalent privileged Docker sidecar. Kubernetes container mode is not supported.
- A shared `/home/runner/_work` volume between the runner and DinD sidecar.
- `DOCKER_HOST` set to the sidecar's `tcp://` endpoint.
- An unprivileged runner container; only the DinD sidecar needs `privileged: true`.
- No `sudo`, `apt install`, or other root-requiring commands in `steps`, `pre-steps`, `pre-agent-steps`, or `post-steps`.
- AWF `v0.27.20` or newer.
- A tool cache on a daemon-visible shared path, such as `/tmp/gh-aw/tool-cache`, rather than `/opt/hostedtoolcache`.

The compiler uses `runner.topology: arc-dind` to enable sysroot staging, shared-volume paths, chroot identity, log relocation, network isolation, and tool-cache checks. At runtime, the generated workflow also inspects `DOCKER_HOST` and passes the Docker endpoint to AWF.

If a custom `copilot-setup-steps` job installs the Copilot CLI on a runner container with `allowPrivilegeEscalation: false`, invoke `install_copilot_cli.sh --rootless`. This installs the CLI under `~/.local/bin` instead of using `sudo`.

See [How to run GitHub Copilot coding agent on ARC with Docker-in-Docker](/gh-aw/reference/arc-dind-copilot-agent/) for the runner scale-set setup.

### ARC DinD tradeoffs

ARC DinD makes agentic workflows available to ephemeral Kubernetes runner fleets without granting privilege to the runner container. The privileged DinD sidecar remains a significant infrastructure trust boundary.

The separate runner and daemon filesystems make paths, tool caches, sockets, and logs more complex than on a conventional Docker host. DinD also adds image caching and storage overhead. Choose ARC DinD because the runner platform requires it, not as an isolation upgrade over Docker.

### ARC DinD troubleshooting

**Compilation rejects `sudo` or `apt-get install`:** Move system packages into the runner image or DinD image. ARC DinD workflows are validated as rootless and must not bootstrap host packages during the job.

**`Docker daemon is not accessible` from the MCP gateway:** If a Unix socket is mounted at a nonstandard path, set `GH_AW_DOCKER_SOCK_PATH` in the runner pod. The gateway derives the socket group ID with `stat` when possible; set `GH_AW_DOCKER_SOCK_GID` only when that detection fails or the socket is not visible during setup. See [Docker socket override for split-daemon topologies](/gh-aw/reference/self-hosted-runners/#docker-socket-override-for-split-daemon-topologies).

**The agent sees an empty workspace or mount source does not exist:** Confirm both containers share `/home/runner/_work`, `GITHUB_WORKSPACE` is under that volume, and `runner.topology: arc-dind` was present when the lock file was compiled.

**`RUNNER_TOOL_CACHE is under /opt` warning or setup tools are missing:** Set `RUNNER_TOOL_CACHE=/tmp/gh-aw/tool-cache` in the runner pod and re-run. `/opt/hostedtoolcache` is normally visible only to the runner container.

**`spawn /usr/local/bin/copilot ENOENT` or an engine CLI is missing:** Upgrade gh-aw and recompile. Current workflows stage activated engine binaries into `${RUNNER_TEMP}/gh-aw/bin`, which is visible to the DinD daemon.

**Proxy DNS failure such as `getaddrinfo EAI_AGAIN`:** Docker containers created by the DinD daemon do not automatically use Kubernetes service discovery. Make the proxy reachable by IP or configure DNS forwarding from the Docker network to cluster DNS.

**Logs are missing from `/tmp/gh-aw`:** ARC DinD writes logs under `$RUNNER_TEMP/gh-aw/sandbox/firewall/logs/` because `$RUNNER_TEMP` is on the shared work volume.

## Debug in dependency order

Runtime failures are easiest to isolate from the runner upward:

1. Verify the runner operating system, architecture, disk, memory, and required privilege.
2. Verify Docker independently with `docker version`, `docker info`, `docker compose version`, and `docker run --rm hello-world`.
3. Verify the specialized backend independently: `docker run --runtime=runsc ...` for gVisor, `sbx create` and `sbx exec` for Docker sbx, or Docker API access through `DOCKER_HOST` for ARC DinD.
4. Confirm the frontmatter uses the correct field and value, then run `gh aw compile`.
5. Inspect the generated lock file for the expected setup and pre-flight steps.
6. Inspect AWF logs. Conventional runners use `/tmp/gh-aw/sandbox/firewall/logs/`; ARC DinD uses `$RUNNER_TEMP/gh-aw/sandbox/firewall/logs/`.

Enable compiler diagnostics when generated configuration is unexpected:

```bash
DEBUG=workflow:* gh aw compile 2>debug.log
```

Do not compensate for a missing host capability with extra agent mounts or manual AWF arguments. Fix the runner prerequisite or select a compatible runtime.
