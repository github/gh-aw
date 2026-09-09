---
title: "MicroVM Support Is Consolidating on Cloud Hypervisor"
description: "gh-aw is consolidating its specialized sandbox runtimes on the Cloud Hypervisor microVM implementation."
authors:
  - copilot
date: 2026-09-05
metadata:
  seoDescription: "Learn why gh-aw deprecated the gVisor and Docker sbx runtimes and is consolidating microVM support on Cloud Hypervisor."
---

GitHub Agentic Workflows is consolidating its specialized sandbox runtime support on `cloud-hypervisor`. The `gvisor` and `docker-sbx` runtime options are deprecated and will be removed in a future release.

`docker-sbx` introduced a KVM-backed microVM boundary, while `gvisor` provided a user-space kernel between the agent container and host kernel. Maintaining both paths alongside Cloud Hypervisor created separate installation, compatibility, and troubleshooting surfaces. Consolidating on one microVM implementation makes the stronger isolation path more consistent and easier to evolve.

## What changes

The default `docker` runtime remains available and continues to run AWF with network isolation and proxy enforcement. For workflows that require a hardware-virtualized boundary, `cloud-hypervisor` is now the supported direction:

```aw wrap
---
on: issues
sandbox:
  agent:
    runtime: cloud-hypervisor
---

Investigate this issue.
```

Cloud Hypervisor support is currently in preview and requires a GitHub-hosted Ubuntu x86_64 runner with `/dev/kvm`. The compiler adds the required host checks and provisions digest-pinned runtime assets.

## Migrating existing workflows

Review workflows that explicitly set `runtime: gvisor` or `runtime: docker-sbx`. Select `cloud-hypervisor` when the workflow runs on an eligible GitHub-hosted runner and needs a microVM boundary. Otherwise, remove the runtime setting to use the default Docker profile.

Compile each updated workflow and review the generated lock file:

```bash
gh aw compile
```

The deprecated values remain documented during the transition, but new workflows should use either the default Docker runtime or `cloud-hypervisor`. See [Agent Runtime Selection](/gh-aw/reference/agent-runtimes/) for requirements and tradeoffs.
