---
"gh-aw": patch
---

Upgrade gh-aw-firewall to v0.28.12. Cloud Hypervisor workflows now pass the release-pinned artifact manifest, Sigstore bundle, and release tag to AWF, which verifies all guest artifacts before VM startup and avoids self-matching KVM process probes.
