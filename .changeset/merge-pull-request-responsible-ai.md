---
"gh-aw": minor
---

Always honor effective changes-requested reviews when merging pull requests, including without an approver allowlist. Add optional `required-check-apps` producer pinning and persist bounded, redacted identity/revision audit records for successful, blocked, and staged merges. Surface audit-write failures separately without losing completed merge outcomes, and expose only summary-safe diagnostics for blocked processing results.

Record structured producer and early-target rejection codes. Preserve audit-only artifacts in global staged and trial runs, require successful tracked-secret redaction before uploading merge audits, and never perform a merge in staged mode.
