---
"gh-aw": patch
---

Bump the default gh-aw-firewall version to v0.28.23. This release enforces TLS
SNI allowlisting, fails Cloud Hypervisor preflight early when a trusted-artifact
root is `noexec`, propagates config fields consistently across all layers,
preserves explicit upstream proxy ports, and adds GCP WIF support for the
Gemini and Vertex API proxy adapters.
