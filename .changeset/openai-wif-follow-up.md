---
"gh-aw": patch
---

Require AWF v0.29.11 or newer for OpenAI WIF, validate effective detector proxy images, preserve keyless authentication and OIDC permissions for same-engine and environment-configured threat detection, and avoid granting agent egress to the OpenAI token-exchange host. Reject OIDC-authenticated detectors on AWF versions that cannot exclude Actions token-request credentials.
