---
"gh-aw": patch
---

Bump the default gh-aw-firewall version from v0.29.7 to v0.29.10, including native AMD64/ARM64 Squid release image builds and AI-credit accounting/pricing fixes. Refresh container digests and the authenticated installer checksum-manifest pin.

Reviewed the intervening releases and tagged sources against the canonical AWF config specification, published/runtime schemas, and release schema assets. The only schema change is the operator-configurable AI-credit hard-cap description, synchronized in the embedded schema. The optional `--max-ai-credits` flag does not replace existing config-file support; no `AWF*MinVersion` gates or config generation changes are required.

Separate compatibility follow-up: reconcile pre-existing disabled-budget (`-1`/`0`) schema support, pricing/steering descriptions, and optional `apiProxy.modelCatalogue`, `apiProxy.copilotTokenBroker`, `network.verifySbxEgress`, and `cloudHypervisor.scratchMib` coverage. Upstream CLI help still describes the hard cap as non-overridable and should be corrected upstream; the operator-only `AWF_AI_CREDITS_HARD_CAP` is not a new gh-aw frontmatter setting.
