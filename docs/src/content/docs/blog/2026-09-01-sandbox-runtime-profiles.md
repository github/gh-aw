---
title: "Sandbox Security Options Are Now Runtime Profiles"
description: "gh-aw replaces sandbox.agent.legacy-security and sandbox.agent.sudo with explicit sandbox runtime profiles."
authors:
  - copilot
  - pelikhan
date: 2026-09-01
metadata:
  seoDescription: "Migrate removed gh-aw sandbox.agent.legacy-security and sudo settings to sandbox.agent.runtime profiles with gh aw fix."
---

Sandbox security behavior is now selected through `sandbox.agent.runtime`. The separate `sandbox.agent.legacy-security` and `sandbox.agent.sudo` settings have been removed, making each runtime an explicit security and topology profile.

The default `docker` profile runs AWF without sudo and isolates network access. Workflows that need the previous privileged iptables behavior can select `docker-sudo-iptables`:

```aw wrap
sandbox:
  agent:
    runtime: docker-sudo-iptables
```

This profile runs AWF with sudo, uses iptables-based networking, and permits host and GitHub Actions service access. It is required for `sandbox.agent.allow-host-ports` and for connecting to published `services:` ports. Other profiles retain their own isolation guarantees: `gvisor` adds kernel-level isolation, while `docker-sbx` and `cloud-hypervisor` use virtual-machine boundaries.

## Migrate existing workflows

Run the fixer to update workflow frontmatter:

```bash
gh aw fix --write
```

The `sandbox-runtime-profiles` codemod rewrites this configuration:

```aw wrap
sandbox:
  agent:
    legacy-security: enable
```

to:

```aw wrap
sandbox:
  agent:
    runtime: docker-sudo-iptables
```

The codemod also removes obsolete `sudo` settings and preserves compatible runtime choices. When a combination cannot be migrated without changing its security intent, the fixer reports an actionable error instead of selecting a profile silently.

After migration, compile the workflow and review the generated lock file:

```bash
gh aw compile
```

See the [sandbox configuration reference](/gh-aw/reference/sandbox/) and [agent runtime reference](/gh-aw/reference/agent-runtimes/) for the behavior and constraints of each profile.
