---
"gh-aw": minor
---

Add `enclaves[].agent.tools.github` as the preferred enclave GitHub frontmatter
shape, aligning enclave agent tool allowlists and guard-policy fields with the
primary agent's `tools.github` configuration while keeping the legacy
`enclaves[].agent.github.cli: issues-read-v1` profile available during
migration.
