---
description: Choose and configure agent runtimes for GitHub Agentic Workflows.
disable-model-invocation: true
---

# Agent Runtime Instructions

Use these instructions when creating or updating workflows that mention Docker, Cloud Hypervisor, ARC DinD, or self-hosted runners.

## Runtime fields

- Omit `sandbox.agent.runtime` for the default Docker agent runtime.
- Set `sandbox.agent.runtime: docker-sudo-iptables` only when the workflow needs `sandbox.agent.allow-host-ports` or published GitHub Actions service ports.
- Set `sandbox.agent.runtime: cloud-hypervisor` only for the preview microVM runtime on a GitHub-hosted Ubuntu x86_64 runner with `/dev/kvm`.
- Set `runner.topology: arc-dind` for ARC or equivalent Kubernetes runners that use a Docker-in-Docker sidecar. This is a runner topology, not an agent runtime.

## Compatibility

- Do not combine `runner.topology: arc-dind` with `sandbox.agent.runtime: cloud-hypervisor`.
- ARC DinD workflows must be rootless: do not add `sudo`, `apt-get install`, or other host package bootstrap steps.
- Cloud Hypervisor requires `RUNNER_ENVIRONMENT=github-hosted`, Ubuntu Linux x86_64, and `/dev/kvm`; it is not supported on self-hosted or ARC DinD runners.

## Cloud Hypervisor guidance (preview)

- The compiler emits host preflight and release-asset provisioning steps that download and checksum-verify the pinned Cloud Hypervisor bundle before AWF starts.
- AWF launches with the host privileges required to create the VM but keeps strict network isolation. The guest defaults to 2 vCPUs and 4096 MiB.
- Not supported: `tools.github.mode: gh-proxy`, integrity reactions, `sandbox.agent.allow-host-ports`, GitHub Actions services with published ports, and enclaves.
- If the runner does not meet the preview requirements, omit `sandbox.agent.runtime` to use Docker.

## ARC DinD guidance

- Use `runner.topology: arc-dind` when `DOCKER_HOST` points to a DinD sidecar such as `tcp://localhost:2375` or `tcp://dind:2375`.
- Ensure the runner container and DinD sidecar share `/home/runner/_work`.
- Use a daemon-visible tool cache path such as `/tmp/gh-aw/tool-cache`, not `/opt/hostedtoolcache`.
- If the Docker socket is bind-mounted at a nonstandard path, set `GH_AW_DOCKER_SOCK_PATH`. Set `GH_AW_DOCKER_SOCK_GID` only when group detection with `stat` fails.
