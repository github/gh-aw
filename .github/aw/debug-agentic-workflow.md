---
description: Read-only run-log and audit triage supporting the shared local-first debugging strategy.
disable-model-invocation: true
---

# Workflow Evidence Triage

Use this companion to [local-debug.md](local-debug.md) for existing-run evidence.
Follow its mode selection and execution policy if not already loaded. A workflow
start returning `403 Forbidden` ends active debugging and returns to diagnosing
and patching. This evidence reference does not authorize uploads or new runs.

## Collect Existing Evidence

Use the workflow name or run URL already supplied. For a run URL, extract its ID
and audit it before requesting more context:

```bash
gh aw audit RUN_ID --json
gh aw logs WORKFLOW --json
gh aw status
```

Start with the compact audit. Fetch targeted evidence only when needed:

```bash
gh aw audit RUN_ID --artifacts usage,github-api,mcp,agent
```

Read the actual downloaded artifact names rather than assuming a fixed layout.
Use cached evidence when available. For an in-progress run, use a bounded
monitoring deadline; do not dispatch another run or poll indefinitely.

## Identify the First Failing Boundary

Separate the primary failure from cascades. Classify each failed message by
tool/action, HTTP status, job, and time: a workflows-permission 403 and a
`Bad credentials` 401 need different fixes.

In Codespaces, distinguish token-source/SAML failures from missing permissions.
Follow the shared strategy's 403 credential triage; a logged-in CLI or repository
role does not prove that its active token is SSO-authorized.

Check tool-name/schema mismatches, prompt inputs, token source/lifetime,
effective job permissions, firewall denials, and safe-output validation.
For GitHub MCP, inspect both the DIFC source policy and the `safeoutputs` write
sink policy. Never retrieve secret values or infer valid credentials from mocks.

For underperformance, distinguish missing tools, prompt ambiguity, timeout
pressure, and token/sub-agent fan-out. Compare token usage, cache behavior,
cost, and output quality; lower cost does not justify a quality regression.

Load syntax, safe-output, engine-runtime, campaign, or experiment references
only for the mechanism under investigation. If a workflow itself collects logs,
it needs `actions: read` and CLI installation before invoking `gh aw`.

## Fix and Report

Use the shared strategy for a regression-backed patch and development compilation.
In diagnosis/patching mode, hand off the patch and unmet hosted checks; do not
claim to have completed an active debug loop. In debug mode, follow the separately
human-validated iteration gates. Report mode, cause/uncertainty, change, and
evidence. A passing compile neither authorizes a rerun nor overrides a dispatch
denial or an explicit host restriction.
