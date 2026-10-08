---
title: Agent Runtime Selection
description: Choose and configure Docker, Cloud Hypervisor, NVX, or ARC DinD for an agentic workflow.
sidebar:
  order: 1340
---

Agentic workflows use AWF (Agent Workflow Firewall) to isolate the agent. The supported agent runtime profiles are Docker, privileged Docker with legacy iptables networking, preview Cloud Hypervisor, and preview NVX. ARC DinD is a runner topology, not a value of `sandbox.agent.runtime`.

## Runtime and topology fields

| Field | Purpose | Supported values |
| --- | --- | --- |
| `sandbox.agent.runtime` | Selects the isolation backend for the main agent | `docker`, `docker-sudo-iptables`, `cloud-hypervisor`, `nvx`, or omitted for Docker |
| `runner.topology` | Describes how the runner reaches Docker | `arc-dind`, or omitted for a local Docker daemon |
| `runtimes` | Installs language toolchains such as Node.js, Python, and Go | Unrelated to agent isolation |

## Choose a runtime

| Choice | Isolation boundary | Runner requirements | Main tradeoff |
| --- | --- | --- | --- |
| Docker | Linux namespaces, cgroups, and the host kernel | Linux and a usable Docker daemon | Broad compatibility; the agent shares the host kernel |
| Docker with legacy iptables | Docker with privileged AWF networking and host/service access | Linux, Docker, and host privileges | Supports host ports and Actions services but increases AWF privileges |
| Cloud Hypervisor (preview) | A KVM-backed microVM for the agent | GitHub-hosted Ubuntu x86_64 runner with `/dev/kvm` | Stronger isolation with strict host requirements |
| NVX (preview) | A one-shot NVX microVM with a workload-specific guest layer | Linux x86_64, effective root for AWF, read/write `/dev/kvm`, cgroup v2 (`cpu`, `memory`, `pids`), and seccomp `kill_process` | Strong isolation; requires trusted, attested AWF assets and explicit pre-agent provisioning |
| ARC DinD | Standard Docker agent container in a DinD sidecar | ARC or equivalent Kubernetes runner with a privileged DinD sidecar and shared work volume | Supports Kubernetes runner fleets but adds split-filesystem complexity |

Use `docker-sudo-iptables` only when the workflow requires `sandbox.agent.allow-host-ports` or access to published GitHub Actions `services:` ports. Use `cloud-hypervisor` or `nvx` only when the runner satisfies the selected preview runtime's requirements. Otherwise, omit `sandbox.agent.runtime` to use Docker.

## Requirements shared by all choices

The main agent job requires a Linux runner with enough CPU, memory, and disk for the agent, AWF, the MCP Gateway, proxy containers, and configured MCP servers.

Docker must be reachable by the runner user. Verify the baseline before investigating a specialized topology:

```bash
docker version
docker info
docker compose version
docker run --rm hello-world
```

The runner also needs outbound HTTPS access to GitHub, the selected AI provider, `ghcr.io`, and the domains required by setup steps and `network.allowed`.

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

The entire `sandbox` block may be omitted when its defaults are sufficient. AWF still provides network isolation and proxy enforcement.

### Docker with host and service access

Use `docker-sudo-iptables` when the agent must reach explicitly allowed host ports or published GitHub Actions service ports:

```aw wrap
---
sandbox:
  agent:
    id: awf
    runtime: docker-sudo-iptables
    allow-host-ports: [8080]
---
```

This profile runs AWF with the host privileges required for legacy iptables networking. Other profiles reject `allow-host-ports` and published service connectivity.

## Cloud Hypervisor (preview)

Cloud Hypervisor runs the agent in a KVM-backed microVM. It is supported only on GitHub-hosted Ubuntu x86_64 runners with `/dev/kvm`.

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

The compiler verifies the host environment, downloads and checksum-verifies the Cloud Hypervisor bundle from the pinned AWF release, grants the runner user scoped access to `/dev/kvm`, and starts AWF with the preview runtime flags. The default guest size is 2 vCPUs and 4096 MiB.

Cloud Hypervisor is incompatible with:

- `runner.topology: arc-dind`
- self-hosted, non-Ubuntu, or non-x86_64 runners
- `tools.github.mode: gh-proxy`
- `sandbox.agent.allow-host-ports`
- GitHub Actions services with published ports
- enclaves

If the runner does not meet these requirements, omit the runtime to use Docker.

## NVX (preview)

NVX runs the agent in AWF's fail-closed, one-shot microVM runtime. It requires AWF `v0.28.49` or newer and a Linux x86_64 host with effective UID 0 for AWF, read/write access to `/dev/kvm`, cgroup v2 `cpu`, `memory`, and `pids` controllers, and seccomp `kill_process` support. Generated preflight checks these requirements and fails the job with an actionable diagnostic; NVX never falls back to Docker or Cloud Hypervisor.

Set only the runtime to use secure NVX defaults:

```aw wrap
---
on:
  workflow_dispatch:
  # A custom provisioning job must be listed in on.needs.
  needs: [build_nvx_artifacts]
sandbox:
  agent:
    id: awf
    runtime: nvx
---

Run the workload in the pre-provisioned NVX guest.
```

The defaults expect the workload-specific guest layer and the five trusted assets under `${{ runner.temp }}/nvx/` (`guest-layer`, `openvmm`, `vmlinux`, `initramfs.cpio.gz`, and the manifest and Sigstore bundle). Download and extract the trusted assets from the same pinned AWF release used by the workflow, then build the guest layer before the generated preflight step. The manifest and bundle are verified offline with `gh attestation verify`; hashes alone are not accepted. Setup stages the verified files root-owned and read-only, and removes the staging directory after agent execution. Install these host packages before pre-agent setup: `acl bubblewrap e2fsprogs erofs-utils jq nftables uidmap`. The required host tools are resolved only from `/usr/sbin`, `/usr/bin`, `/sbin`, or `/bin`.

Optional `sandbox.agent.nvx` overrides support alternate paths, mount policy, resource limits, and working directory. Preview mode, strict network isolation, and the API proxy always remain enabled.

For migration from the firewall repository's manual smoke workflows, keep each workflow's custom artifact-building job and workload-specific layer build, declare the artifact job under `on.needs`, and replace the manual AWF NVX command with the declarative runtime above. The Copilot smoke layer must still contain its pinned Copilot CLI; the build-test layer must still contain its pinned Node.js and Go toolchains. Download the workflow-attested AWF assets and restore the paths before pre-agent setup finishes. Do not pass provider credentials into the guest; the required API proxy keeps real credentials in its sidecar.

NVX rejects TTY/interactive execution, `runner.topology: arc-dind`, host or service port access, extra host mounts, Docker-in-Docker options, custom AWF arguments, DIFC proxy features, custom network subnets, DNS-over-HTTPS, and primary-agent enclaves. Do not disable preview, strict network isolation, or the API proxy. `workspace-and-tool-cache` fails if the runner tool cache is absent.

## ARC with Docker-in-Docker

Set `runner.topology: arc-dind` when the runner reaches Docker through a DinD sidecar:

```aw wrap
---
runner:
  topology: arc-dind
  labels: [self-hosted, linux, arc]
---
```

The runner and DinD sidecar must share `/home/runner/_work`. `DOCKER_HOST` must point to the sidecar, and the sidecar must be privileged. The compiler stages binaries and configuration into daemon-visible paths and installs Docker Compose when needed.

Do not combine ARC DinD with `cloud-hypervisor` or `nvx`. Use the default Docker profile.

## Troubleshooting

Debug runtime failures in this order:

1. Confirm the runner operating system and architecture.
2. Verify Docker access with `docker version`, `docker info`, and `docker compose version`.
3. Verify `DOCKER_HOST` and shared-volume visibility for ARC DinD.
4. Verify `/dev/kvm` and the host requirements for the selected microVM runtime.
5. Inspect the generated setup step that failed.
6. Recompile with the latest compatible gh-aw version.

Compilation rejects unsupported runtime values and known incompatible combinations before a workflow runs.
