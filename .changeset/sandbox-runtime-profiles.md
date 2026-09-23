---
"gh-aw": minor
---

Collapse the sandbox security options into `sandbox.agent.runtime` profiles.

- `sandbox.agent.runtime` is now the single selector for the supported sandbox security and topology profiles: `docker` (default), `docker-sudo-iptables`, and `cloud-hypervisor`. Omitting `runtime` keeps the secure Docker default.
- **Breaking:** `sandbox.agent.sudo` and `sandbox.agent.legacy-security` are removed from the schema and the compiler model. The compiler now derives AWF privileges, network isolation, and runtime setup privileges from the selected profile.
- `sandbox.agent.allow-host-ports` and automatic connectivity to GitHub Actions `services:` require `runtime: docker-sudo-iptables`. Invalid combinations now fail at compile time.
- `gh aw fix` codemod (`sandbox-runtime-profiles`) migrates unambiguous configurations and reports an actionable error for mixed profiles.
