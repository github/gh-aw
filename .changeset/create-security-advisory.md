---
"gh-aw": minor
---

Add the `create-security-advisory` safe output for creating draft repository security advisories and `add-security-advisory-comment` for commenting on existing advisories. Require an external token or GitHub App with advisory-write access instead of `GITHUB_TOKEN`, and support staged previews without credentials.
