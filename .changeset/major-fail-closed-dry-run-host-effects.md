---
"gh-aw": major
---

Harden dry-run compilation against retained custom execution and credentials.

**Breaking change:** `compile --dry-run` accepts authored `steps` and `post-steps`
but omits their code and credentials from emitted locks; separate dispatch cannot
execute them. It rejects `pre-steps`, `pre-agent-steps`, `on.steps`,
custom MCP servers and scripts, custom safe-output scripts, enabled Bash graders,
and authored OIDC permissions, including imported declarations. Custom jobs and
custom safe-output jobs become disabled, credential-free placeholders. Existing
medium-or-higher and unclassified scanner findings fail the gate instead of being
accepted solely because they appear in the baseline.

Migration: use a separate diagnostic workflow without unsupported declarations,
review omitted setup/cleanup steps, skipped dependencies and empty custom-job
outputs, and resolve actionable scanner findings before live testing. Normal
compilation is unchanged. This does
not make agent shell commands, custom engines, or credentials a hosted sandbox.

Preserve trigger comments and reusable-workflow telemetry secret contracts during
dry-run rewriting, and render disabled calls without actionlint constant conditions.
