---
"gh-aw": patch
---

Bump the default gh-aw-firewall version to v0.28.6 and refresh embedded container digest pins. This activates `filesystem.allowWrite` support for the Cloud Hypervisor microVM runtime: `sandbox.agent.runtime: cloud-hypervisor` workflows now seed `/workspace` and `/workspace/.awf-home` as writable paths by default (in addition to `/tmp/gh-aw/agent`) so the repo checkout and `HOME` remain writable, and the compiler emits a `mkdir -p` for `.awf-home` on the host before AWF starts. A new `AWFCloudHypervisorFilesystemAllowWriteMinVersion` (v0.28.6) gate is used for Cloud Hypervisor, since selective `allowWrite` was broken on real hosts before v0.28.6.

`filesystem.allowWrite` is now emitted **only** for the Cloud Hypervisor runtime. The Docker runtimes enforce the policy by narrowing AWF's own writable bind mounts to read-only — including its internal `/tmp/awf-init` control-plane mount nested under the narrowed `/tmp` bind — so any policy that does not cover `/tmp` prevents the agent container from starting. Workflows that declare `sandbox.agent.config.filesystem.allowWrite` on those runtimes now get a compiler warning instead of a silently dropped (or fatally applied) policy.
