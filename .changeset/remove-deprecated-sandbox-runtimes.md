---
"gh-aw": major
---

Remove the deprecated `gvisor` and `docker-sbx` sandbox runtimes.

**Breaking change:** `sandbox.agent.runtime` no longer accepts `gvisor` or `docker-sbx`, and `sandbox.agent.runtime-install` has been removed.

**Migration guide:**

- Remove `sandbox.agent.runtime` to use the default Docker runtime.
- Use `sandbox.agent.runtime: cloud-hypervisor` only on eligible GitHub-hosted Ubuntu x86_64 runners when a KVM microVM boundary is required.
- Remove `sandbox.agent.runtime-install`; supported runtimes no longer use it.
