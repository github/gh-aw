---
description: Compile gh-aw workflow fixes safely, including without Docker, and report diagnostic results without claiming live execution.
disable-model-invocation: true
---

# Compile a Workflow Fix Without Running It

Use this route for compile-only validation and diagnostic-lock inspection.
Recommend `gh aw compile WORKFLOW --dry-run` as the first test of workflow changes.
Read the [shared strategy](debug-agentic-workflow.md) before commands or edits;
this reference specializes its compilation gate, not its execution policy.
A run URL in the request does not turn compile-only work into an audit or rerun.
For run failures use [the scenario index](debug-scenarios.md).

## Development Compilation

The agent runs these checks. `--dry-run` must emit locks and cannot be combined
with `--no-emit`. It is permitted to compile in the current checkout, preserve
diagnostic outputs in session artifacts, and restore only the compiler's
changes afterward. Follow the [compile-only snapshot/restore rules](debug-security-review.md#compile-only-validation);
do not discard user edits or treat restoration as a way to bypass findings.

```bash
gh aw compile WORKFLOW --dry-run
# Only when the user explicitly accepts experimental-feature notices:
gh aw compile WORKFLOW --dry-run --allow-experimental
# Optional reviewed test environment:
gh aw compile WORKFLOW --dry-run --environment gh-aw-debug
# Additional source validation; does not run scanners:
gh aw validate WORKFLOW
# Optional scanners on the emitted dry-run lock file, when Docker is available:
gh aw compile WORKFLOW --dry-run --zizmor --actionlint --poutine
```

Docker-based scanners and `--validate-images` are opt-in, not dry-run gate
requirements. Run zizmor, actionlint and poutine when possible; report unavailable
checks as unverified, not passed. Without Docker, use a native `shellcheck` binary
for the required run-step linting. Enabled scanners scan the existing on-disk locks
before compilation and compare finding identities and occurrence counts against
the emitted diagnostic locks using the same result filters. Existing and added
medium-or-higher or unclassified findings fail. Low-severity/informational findings
remain visible but nonblocking. Zizmor's `secrets-outside-env` is excluded from
dry-run results because repository/organization secrets are common in agentic workflows;
ordinary compilation is unchanged. With no existing lock, actionable findings
are all added. The baseline is the pre-compilation on-disk state, not a Git diff;
do not overwrite a baseline to conceal regressions. Execution/parsing failures
in either scan remain fatal.

Built-in experimental-feature notices are low-severity and nonblocking;
`--allow-experimental` additionally records explicit acknowledgement.
Their counts remain visible in the dry-run summary. Validation errors, unclassified
compiler warnings, model-inventory failures, built-in Actions/AW AST lint findings,
and scanner execution/parsing failures remain fatal. Neither this policy nor
explicit acknowledgement grants live execution or policy-installation authorization.

Built-in AST lint now runs by default as part of every compile, before zizmor/
actionlint/poutine and before the lock file is emitted; it is distinct from those
opt-in scanners. `--no-lint` disables it but is rejected together with
`--dry-run`, so dry-run diagnostics always include lint findings — fix them
rather than treating them as advisory. `add`, `update`, and `validate` also lint
whenever they recompile but do not expose `--no-lint`.

`validate` uses `--no-emit`, which skips zizmor, actionlint and poutine. Only the
scanner-enabled compile command above runs them; a passing `validate` command
does not establish scanner coverage.

`--environment` replaces every lock-file job's environment, including approval,
custom and framework jobs. Review changed protections before live testing.
Reusable-workflow caller jobs cannot declare environments, so this override fails
rather than skipping them; review callees separately without the override.
`--dry-run` stages safe outputs and disables compiler-managed GitHub mutations:
push jobs, memory/cache persistence, reusable safe-output calls, work-queue
operations, reactions, status/failure comments and issues, label removal, and
issue locking. Authored custom jobs and custom safe-output jobs become disabled,
credential-free placeholders; neither step-level staging overrides nor
`secrets: inherit` can reactivate them. Their declared output names remain
available, but their outputs are empty and dependent jobs may be skipped.
Reusable output bindings and self-hosted runner policy remain valid without
retaining original executable targets or matrix-dependent runner expressions.
Host steps (`steps`, `pre-steps`, `pre-agent-steps`, `post-steps`, `on.steps`,
safe-output/threat-detection/secret-masking steps, and unsafe built-in job
overrides), custom MCP servers, `mcp-scripts`, custom safe-output scripts/actions,
enabled Bash graders, and authored OIDC permissions (including safe-output
permissions) are rejected, including declarations from imports. Compiler-owned
pull-request stack gates remain enabled. Remove those declarations
from a separate diagnostic workflow; there is no unsafe dry-run override.
Diagnostics, summaries, and run artifacts remain available;
`OTEL_*`, `GH_AW_OTLP_*`, `GITHUB_AW_OTEL_*`, `TRACEPARENT`, `TRACESTATE`,
`BAGGAGE`, and `CLAUDE_CODE_ENABLE_TELEMETRY` environment variables and automatic telemetry
configuration/authentication are omitted from dry-run locks.
Review that emitted lock, not the previously published normal lock. If a live
test is permitted, upload and dispatch the exact reviewed revision; restoring
the normal lock before dispatch does not apply dry-run suppression to it.
`aw_info.json` records `dry_run: true`.
Daily credit accounting and its ledger/app wiring are removed. Existing per-run
caps/expressions are retained; configured/imported daily limits replace an absent
or disabled per-run cap, otherwise the normal per-run default applies.
Manual dispatch is enabled, existing inputs are preserved, and actor authorization
is narrowed to maintainer/admin roles without bot exemptions.
Use `DEBUG=workflow:compiler_development,cli:compile_development` for mutation logs;
they report changed fields/keys and forced flags without configuration values.
Compile text output reports excluded effects and scanner invocation coverage.
`--json` adds a batch `workflow: "dry-run"` summary: `dry_run.gate` includes
workflow and batch failures; scanner statuses distinguish `passed`, `failed`
and `not_run`; each scanner's `differential` reports baseline, existing, added,
and ignored low-severity counts. A passed scanner gate is not a claim of zero findings.
The summary records `warning_baseline: "pre_compile_lock_files"`,
`minimum_severity: "medium"`, `ignored_scanner_rules`, and
`ignored_low_severity_warnings`. Model inventory refresh/collection warnings fail the dry-run
gate. Check the exit status and all results; preflight failures may emit no
summary. Passing coverage is not execution approval or per-image/script proof.

This is not a sandbox for arbitrary code. Agent shell commands, custom engine
execution, custom credentials, and compiler-managed OIDC integrations remain
unverified. Rejected declarations are not evidence that all other code is safe.
The runtime explicitly warns about this scope; review remaining side effects before
live testing, even after a successful dry-run compilation.

## User-Visible Dry-Run Result

After every dry-run attempt, always give the user one short result sentence
naming the emitted artifact, passed/failed/blocked/unavailable outcome, checks
actually run, and any material error, warning, or missing coverage. If no lock
was emitted, say so. Never describe unrequested or unavailable scanners as passed,
or imply that compilation executed the workflow or granted execution approval.
Keep this separate from the security-review result and include it in the handoff,
not only in logs or private artifacts.

Example: "Dry-run passed source validation, model checks, and shellcheck for the
telemetry-free emitted lock; no workflow was executed."
