---
title: Compiler Enterprise Environment Controls
description: Enterprise environment variables injected and managed by the compiler for default guardrails and model overrides
sidebar:
  order: 655
---

Use these variables to set organization- or repository-wide defaults and compiler policy without editing individual workflow frontmatter files.

In this enterprise controls reference, OTLP defaults are the scope-sensitive exception: `GH_AW_DEFAULT_OTLP_ENDPOINT` can be a GitHub Actions variable at repository, organization, or enterprise scope, but the `GH_AW_DEFAULT_OTLP_ENDPOINT` and `GH_AW_DEFAULT_OTLP_HEADERS` secrets are limited to repository or organization scope.

## Enterprise Control Variables

| Variable | Source | Purpose | Applies when |
| --- | --- | --- | --- |
| `GH_AW_DEFAULT_ARTIFACT_RETENTION_DAYS` | GitHub Actions `vars.*` at runtime | Retention period for all gh-aw artifacts | `artifact_retention_days` is not set in `.github/workflows/aw.json` |
| `GH_AW_DEFAULT_MAX_AI_CREDITS` | GitHub Actions `vars.*` at runtime | Default AWF `apiProxy.maxAiCredits` budget | `max-ai-credits` is not set in frontmatter or any imported workflow |
| `GH_AW_DEFAULT_MAX_TURN_CACHE_MISSES` | Compiler process environment | Default AWF `apiProxy.maxCacheMisses` guardrail | `max-turn-cache-misses` is not set in frontmatter or any imported workflow |
| `GH_AW_DEFAULT_DETECTION_MAX_AI_CREDITS` | GitHub Actions `vars.*` at runtime | Default threat-detection AWF `apiProxy.maxAiCredits` budget | `safe-outputs.threat-detection.max-ai-credits` is not set |
| `GH_AW_DEFAULT_MAX_DAILY_AI_CREDITS` | GitHub Actions `vars.*` at runtime | Default `max-daily-ai-credits` guardrail threshold | `max-daily-ai-credits` is not set in frontmatter or any imported workflow |
| `GH_AW_DEFAULT_ADMISSION_URL` | GitHub Actions repository/organization `vars.*` at runtime | HTTPS `/api/admission` endpoint; empty disables admission | Emitted when `on.admission: true` or aw.json `"admission": true` enables admission jobs |
| `GH_AW_DEFAULT_ADMISSION_AUDIENCE` | GitHub Actions repository/organization `vars.*` at runtime | Admission OIDC audience | Defaults to the effective admission endpoint |
| `GH_AW_DEFAULT_ADMISSION_ON_OUTAGE` | GitHub Actions repository/organization `vars.*` at runtime | Admission outage policy, `ignore` or `block` | Defaults to `ignore`; only transient failures can fail open |
| `GH_AW_DEFAULT_MAX_TURNS` | Compiler process environment | Default top-level `max-turns` | `max-turns` is not set in frontmatter and the selected engine supports max-turns |
| `GH_AW_DEFAULT_TIMEOUT_MINUTES` | GitHub Actions `vars.*` at runtime | Default `agentic_execution` step `timeout-minutes` (20 minutes) | `timeout-minutes` is not set in frontmatter |
| `GH_AW_DEFAULT_AGENT_JOB_TIMEOUT_MINUTES` | GitHub Actions `vars.*` at runtime | Default generated `agent` job `timeout-minutes` (60 minutes) | `jobs.agent.timeout-minutes` is not set in frontmatter |
| `GH_AW_DEFAULT_DETECTION_JOB_TIMEOUT_MINUTES` | GitHub Actions `vars.*` at runtime | Default generated `detection` job `timeout-minutes` (10 minutes) | `jobs.detection.timeout-minutes` is not set in frontmatter |
| `GH_AW_DEFAULT_DETECTION_MODEL` | Compiler process environment | Default threat-detection model | `safe-outputs.threat-detection.engine.model` is not set |
| `GH_AW_DEFAULT_UTC` | Compiler process environment | Default project home UTC offset for rendered CLI timestamps | `utc` is not set in `.github/workflows/aw.json` |
| `GH_AW_DEFAULT_MODEL_COPILOT` | GitHub Actions `vars.*` at runtime | Default fallback model for Copilot | `GH_AW_MODEL_AGENT_COPILOT` / `GH_AW_MODEL_DETECTION_COPILOT` is unset |
| `GH_AW_DEFAULT_MODEL_CLAUDE` | GitHub Actions `vars.*` at runtime | Default fallback model for Claude | `GH_AW_MODEL_AGENT_CLAUDE` / `GH_AW_MODEL_DETECTION_CLAUDE` is unset |
| `GH_AW_DEFAULT_MODEL_CODEX` | GitHub Actions `vars.*` at runtime | Default fallback model for Codex | `GH_AW_MODEL_AGENT_CODEX` / `GH_AW_MODEL_DETECTION_CODEX` is unset |
| `GH_AW_DEFAULT_OTLP_ENDPOINT` | GitHub Actions `secrets.*` or `vars.*` at runtime | Default OTLP exporter endpoint | `observability.otlp.endpoint` is not set in frontmatter or any imported workflow |
| `GH_AW_DEFAULT_OTLP_HEADERS` | GitHub Actions `secrets.*` at runtime | Default OTLP exporter headers for `GH_AW_DEFAULT_OTLP_ENDPOINT` | `observability.otlp.endpoint` is not set in frontmatter or any imported workflow |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | GitHub Actions `vars.*` at runtime | OTLP/HTTP encoding, `http/json` or `http/protobuf` | Workflow `env` does not explicitly define the variable; JavaScript traces-specific protocol takes precedence |
| `GH_AW_OTLP_LOGS_ENABLED` | GitHub Actions `vars.*` at runtime | Opt in to compact unified-session OTLP logs (`true`); unset or `false` disables export | The conclusion job has a published unified session and configured OTLP endpoints |
| `GH_AW_OTLP_LOGS_MAX_BYTES` | GitHub Actions `vars.*` at runtime | Maximum serialized OTLP session-log request size in bytes (default `65536`) | Session-log export is enabled |
| `GH_AW_REQUIRE_SELF_HOSTED_RUNNERS` | Compiler process environment | Fail compilation unless every generated job selects a self-hosted runner | Set to `true` in a trusted compile environment to enforce self-hosted-only runners |

Use `gh aw env get` and `gh aw env update` to manage `GH_AW_DEFAULT_*` and `OTEL_EXPORTER_OTLP_PROTOCOL`
variables in batch. These commands operate on GitHub Actions variables and support repo, org, or enterprise scope, so `default_otlp_endpoint` can still be managed as an enterprise variable. Set OTLP secrets separately with `gh secret set`; those secrets are limited to repository or organization scope. The defaults file uses
`default_`-prefixed keys such as `default_max_ai_credits`, `default_max_turn_cache_misses`, `default_detection_max_ai_credits`, `default_max_daily_ai_credits`, `default_timeout_minutes`, `default_agent_job_timeout_minutes`, `default_detection_job_timeout_minutes`,
`default_model_copilot`, `default_otlp_endpoint`, `default_otlp_protocol`, `default_artifact_retention_days`, and `default_utc`. For the endpoint, `gh aw env update --scope ent` writes the `GH_AW_DEFAULT_OTLP_ENDPOINT` variable, not an endpoint secret. To mask the endpoint value, set `GH_AW_DEFAULT_OTLP_ENDPOINT` with
`gh secret set` at repository or organization scope instead. `GH_AW_DEFAULT_OTLP_HEADERS` is always a secret and must also be set with `gh secret set` at repository or organization scope. If both endpoint values exist, clearing only one leaves the other effective through the fallback expression; clear both the endpoint secret and variable to disable OTLP export.

`default_otlp_protocol` is the defaults-file key for the standard `OTEL_EXPORTER_OTLP_PROTOCOL` Actions variable (not a `GH_AW_DEFAULT_*` variable). It accepts `http/json` or `http/protobuf`; null or omission deletes the variable at the selected scope. Recompile existing workflows to add the runtime variable lookup. Explicit workflow `env` takes precedence. See [OpenTelemetry](/gh-aw/reference/open-telemetry/#protobuf-encoding-and-azure-monitor) for details.

`GH_AW_REQUIRE_SELF_HOSTED_RUNNERS` is a compile-time enforcement setting, not a `GH_AW_DEFAULT_*` default, and is not managed by `gh aw env`. The same policy can be enabled persistently with `require_self_hosted_runners: true` in `.github/workflows/aw.json`, or for a single invocation with `gh aw compile --require-self-hosted-runners`.

Admission jobs are omitted by default. Enable them with workflow frontmatter `on.admission: true` or `"admission": true` in `.github/workflows/aw.json`, then recompile. Either setting enables both jobs; workflow-level `false` does not override repository-level `true`. Admission defaults use `default_admission_url`, `default_admission_audience`, and `default_admission_on_outage` in managed YAML. Provision organization variables in each enrolled organization for enterprise-wide rollout; do not assume an enterprise Actions-variable scope for admission. Repository variables override organization values, so admission is cooperative rather than locked policy. Subsequent variable changes are runtime-only for opted-in workflows.

A valid approval supersedes daily-AIC validation. Its primary and optional detection/evals allowances set the corresponding existing runtime credit limits; omitted optional allowances retain their existing component limits. An empty endpoint skips the admission job and its token acquisition; an ignored outage retains daily validation and existing limits. Denial and malformed responses block activation. Admission identity and credentials stay within the compiler-owned admission job, while activation and downstream jobs consume only validated outputs. The admission and isolated conclusion-admission callback jobs have job-scoped `id-token: write`, including when the endpoint is empty, because GitHub Actions permissions cannot depend on runtime variables. The ordinary conclusion job receives this permission only when separately configured OTLP OIDC requires it. Activation and agent do not gain this permission for admission. Review existing OIDC relying-party policies accordingly.

The endpoint also enables the isolated conclusion-admission callback job to post a compact unified session and usage artifact IDs to the same origin's `/api/conclusion`, after artifact upload and any upload retry have finished. The ordinary conclusion job passes only the compact metadata and artifact IDs through job outputs; it does not receive callback OIDC access unless OTLP OIDC is separately configured. The reusable compact profile excludes agent conversation, tool payloads and unbounded nested data; its JSONL is limited to 128 records/16 KiB and the complete request to 64 KiB. The authority resolves the unchanged full session through authenticated, invocation-bound artifact reads. Failed artifact publication does not suppress compact delivery, but full-session availability remains explicitly unknown without an artifact ID. The callback reconstructs invocation identity and obtains its own fresh OIDC token using the same effective audience. Conclusion delivery is best-effort under both outage policies: failures warn without changing completed workflow results or delaying prior publication attempts. Endpoint enrollment therefore covers compact delivery and protected full-session retrieval as well as admission metadata.

When calling a compiled workflow through `workflow_call`, add `id-token: write` to the caller job's permission envelope and preserve any other permissions required by the reusable workflow. The callee's admission endpoint is evaluated at runtime, so the caller must grant this scope even when admission is currently disabled:

```yaml
jobs:
  call-worker:
    uses: ./.github/workflows/worker.lock.yml
    permissions:
      contents: read
      id-token: write
```

See the [admission contract](/gh-aw/specs/admission-service-specification/) for protocol and deployment requirements.

```yaml title="Required compile check"
jobs:
  compile:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - name: Compile workflows
        env:
          GH_AW_REQUIRE_SELF_HOSTED_RUNNERS: "true"
        run: gh aw compile
```

When enabled, compilation checks the generated workflow jobs, including framework jobs and jobs whose runner comes from a default. Each job must include the `self-hosted` label or select a runner group. Custom labels alone are not sufficient because the compiler cannot verify that they identify self-hosted runners. An organization can make this check mandatory with a centrally managed required workflow or ruleset check.

```bash
gh aw env update defaults.yml --scope org --org MY_ORG --visibility all
gh aw env update defaults.yml --scope ent --enterprise MY_ENT --visibility all
```

## Unified Session Logs

`GH_AW_OTLP_LOGS_ENABLED=true` opts in to content-minimized unified-session
export from the conclusion job, before cleanup. Set it as an organization,
enterprise, or repository Actions variable. Workflows need to be compiled with
this support once; subsequent variable changes take effect at runtime.
Unset or `false` disables export, and invalid values warn without exporting.
Like other Actions variables, this is cooperative configuration, not a locked
enterprise policy: repository variables and explicit workflow environment values
can override it.

`GH_AW_OTLP_LOGS_MAX_BYTES` accepts a positive integer byte limit (default
`65536`, or 64 KiB). It limits the complete UTF-8 JSON request, including the
OTLP envelope, for each vendor, not just the session body. Whole low-priority
records are omitted until the request fits, and the exported included/omitted
counts and truncation flag reflect those omissions. Invalid limits or limits
too small for one record warn without exporting. Increasing the limit does not
raise the compact profile's independent source/event limits.

Both the compact serializer and OTLP envelope use this retention order:

| Priority (highest first) | Records |
| --- | --- |
| 1 | Session format and workflow metadata |
| 2 | Final execution/session/job outcomes, plus failed, denied, exceeded, cancelled, or interrupted evidence from any retained event kind |
| 3 | Remaining guardrail decisions and grader results |
| 4 | Harness attempts/retries and engine reconnect diagnostics |
| 5 | Per-step usage and firewall token-usage observations |
| 6 | Successful memory updates |

Later essential records can replace earlier low-priority records even after
the compact byte/event limits are reached. Selection is deterministic:
equal-priority records prefer earlier observations, and retained records remain
in source order. Records are dropped whole, not text-truncated or merged;
unrelated accounting sources are never summed. A GitHub Actions warning
explicitly identifies a **partial session export** whenever output or source-scan
limits discard evidence, and exported truncation/count attributes preserve
that qualification for vendor queries. Routine privacy filtering alone does
not trigger a size-truncation warning.

Logs use the existing OTLP endpoint list and per-endpoint authentication,
including the enterprise default endpoint/header pair. No additional credentials
or agent network access are required. The exporter posts OTLP/HTTP JSON or protobuf to
`<endpoint>/v1/logs`, replacing a trailing `/v1/traces` or `/v1/logs` when present.
`OTEL_EXPORTER_OTLP_LOGS_PROTOCOL` overrides `OTEL_EXPORTER_OTLP_PROTOCOL` for
logs (`http/json` by default); `OTEL_EXPORTER_OTLP_TRACES_PROTOCOL` does not
affect logs. The byte limit is measured against the complete JSON representation
before encoding. For Azure Monitor traces endpoints, the logs URL also selects
the independent `Microsoft-OTLP-Logs` stream, retaining the DCE, DCR, and query
parameters. The vendor must support OTLP logs as well as traces at that base URL.
See [protobuf encoding and Azure Monitor](/gh-aw/reference/open-telemetry/#protobuf-encoding-and-azure-monitor).

Each retained event is a structured log body with `type`, `data`, optional
`timestamp`, and provenance. Filter on the `gh-aw.session.event_type` log
attribute or the `gh-aw.session` instrumentation scope. Records carry the
existing trace/span IDs when valid, so vendors can correlate session metadata
with traces. Resource attributes report the compact format version, included
and omitted event counts, oversized source lines, and truncation.

`timeUnixNano` contains the original event time when valid; timezone-qualified
ISO timestamps retain up to nanosecond precision. Numeric timestamps reuse the
source-normalized `provenance.timestampMs` clock when present, otherwise the
native millisecond timestamp. `observedTimeUnixNano` records export observation
time independently. Missing, invalid, negative or out-of-range event times leave
`timeUnixNano` unset; they are not replaced with export time. The body retains
the projected original timestamp and available normalized clock.

Use the following resource attributes to qualify vendor queries:

| Attribute | Meaning |
| --- | --- |
| `gh-aw.session.compact.version` | Compact profile version, currently `1`. |
| `gh-aw.session.included_events` | Records retained in this request. |
| `gh-aw.session.omitted_events` | Inspected records omitted by privacy/vocabulary filtering or compact/transport budgets; not a count of the unscanned population. |
| `gh-aw.session.oversized_lines` | Completed source lines discarded for exceeding the line limit. |
| `gh-aw.session.scan_truncated` | Source scanning stopped at its byte limit. |
| `gh-aw.session.truncated` | Output, line-size or scan limits discarded evidence; `false` does not certify full-session completeness. |
| `gh-aw.session.untimed_events` | Retained records without an exportable event timestamp. |

These attributes describe a request and repeat on its log rows. Do not sum them
per row or across mirrored endpoints. A truncated or scan-limited export is
partial evidence, not proof of no further activity. Event-time queries must
qualify untimed records separately rather than silently treating export time as
execution time. Uncorrelated records cannot establish run-level coverage.

The export reuses the [compact unified-session profile](/gh-aw/specs/unified-agent-session-specification/):
at most 128 events and 16 KiB of projected JSONL, with bounded source scanning.
Prompts, conversation, reasoning, tool arguments/results, unknown event types,
and unbounded nested data are excluded. Export additionally removes repository,
workflow/run/session/agent identifiers, refs, commit SHAs, memory identifiers,
branches, and filesystem paths. Current publication-secret and exporter-header
masks are applied before sending. Operational metadata such as model names,
job/grader IDs, timestamps, usage, outcomes, and trace correlation remains;
this is content minimization, not a guarantee of irreversible anonymity.
The full session artifact is unchanged. Export failures warn and do not change
workflow results; authorization failures are not retried.

## Artifact Retention

`artifact_retention_days` in `.github/workflows/aw.json` sets retention for all
artifacts uploaded by compiled gh-aw workflows, including logs, inter-job staging,
memory artifacts, safe-output uploads, custom `actions/upload-artifact` steps, and
maintenance artifacts:

```json title=".github/workflows/aw.json"
{
  "artifact_retention_days": 7
}
```

The field accepts an integer from 1 to 400 or a single-line GitHub Actions
expression that resolves to an integer in that range, such as
`"${{ vars.ARTIFACT_RETENTION_DAYS || '7' }}"`. Expressions must use contexts
available in every uploading job; prefer `vars` rather than job-specific `steps`
or `needs` outputs. Recompile workflows after changing `aw.json`.

Retention precedence is:

1. `artifact_retention_days` in `.github/workflows/aw.json`
2. `vars.GH_AW_DEFAULT_ARTIFACT_RETENTION_DAYS` at runtime
3. The existing per-artifact setting or built-in default

Both repository and enterprise settings override individual `retention-days`
values. Without either setting, existing retention is preserved, including
short-lived staging artifacts, the safe-output upload default of 30 days, and
GitHub's repository default for uploads without an explicit retention period.

Set `GH_AW_DEFAULT_ARTIFACT_RETENTION_DAYS` as a repository, organization, or
enterprise Actions variable, or manage it through `gh aw env` using
`default_artifact_retention_days: "7"` in the defaults file. The variable is
resolved at workflow runtime, so changing it does not require recompilation of
workflows already compiled with this support.

GitHub's repository, organization, and enterprise retention limits still apply
(public repositories allow up to 90 days; private repositories allow up to 400).
This setting does not change Actions cache eviction, git-backed memory, published
assets, or artifacts uploaded inside third-party actions.

## Project Timezone

By default, the CLI renders timestamps (table output, expiration footers, and the closing messages on expired issues, pull requests, and discussions) using the runner's local clock. Set a project home UTC offset so these times render consistently regardless of where the CLI runs.

Configure the offset per repository with the `utc` field in `.github/workflows/aw.json`:

```json
{
  "utc": "-08:00"
}
```

The value must be a numeric UTC offset in `+HH:MM` or `-HH:MM` form (for example `+00:00`, `+05:30`, or `-08:00`), within the range `-14:00` to `+14:00`. Named timezones and abbreviations are not accepted.

To set an organization- or enterprise-wide default, use the `GH_AW_DEFAULT_UTC` environment variable (or the `default_utc` key managed by `gh aw env`). The repository `aw.json` value takes precedence over this enterprise default.

When neither is configured, timestamp formatting is left unchanged and uses the runner's local time.

## Precedence

For model selection, precedence is:

1. `engine.model` in workflow frontmatter
2. `GH_AW_MODEL_AGENT_*` or `GH_AW_MODEL_DETECTION_*`
3. `GH_AW_DEFAULT_MODEL_*`
4. Built-in compiler fallback

For max AI credits, precedence is:

1. `max-ai-credits` in workflow frontmatter (compile-time literal)
2. `max-ai-credits` from imported shared workflows (compile-time, first-wins across imports)
3. `vars.GH_AW_DEFAULT_MAX_AI_CREDITS` GitHub Actions variable (action runtime)
4. Built-in constant default: `1000` AIC

The compiler emits `${{ vars.GH_AW_DEFAULT_MAX_AI_CREDITS || '1000' }}` in a runtime patch script when no frontmatter or imported value is set, so the organization variable is resolved at workflow run time by the GitHub Actions runner — not at compile time. A value of `-1` disables the AWF budget at runtime; with AWF releases after v0.29.11, budget notices continue unless `sandbox.agent.token-steering: false`. Positive values accept `K`/`M` suffixes such as `100M`.

For max turn cache misses, precedence is:

1. `max-turn-cache-misses` in workflow frontmatter
2. `GH_AW_DEFAULT_MAX_TURN_CACHE_MISSES`
3. Built-in constant default: `5`

The compiler emits `apiProxy.maxCacheMisses` directly in the AWF config JSON. When `max-turn-cache-misses` is omitted, the compiler reads `GH_AW_DEFAULT_MAX_TURN_CACHE_MISSES` from its process environment and falls back to `5` if the variable is unset or invalid.

For threat-detection max AI credits, precedence is:

1. `safe-outputs.threat-detection.max-ai-credits` in workflow frontmatter (compile-time literal)
2. `vars.GH_AW_DEFAULT_DETECTION_MAX_AI_CREDITS` GitHub Actions variable (action runtime)
3. Built-in constant default: `400` AIC

The compiler emits `${{ vars.GH_AW_DEFAULT_DETECTION_MAX_AI_CREDITS || '400' }}` for threat-detection runs when `safe-outputs.threat-detection.max-ai-credits` is unset, so the organization variable is resolved at workflow run time by the GitHub Actions runner — not at compile time. A value of `-1` disables the AWF budget for detection runs at runtime; with AWF releases after v0.29.11, budget notices continue against AWF's hard cap. Positive values accept `K`/`M` suffixes such as `100M`.

For daily AI credits workflow guardrails, precedence is:

1. `max-daily-ai-credits` in workflow frontmatter (compile-time literal)
2. `max-daily-ai-credits` from imported shared workflows (compile-time, first-wins across imports)
3. `vars.GH_AW_DEFAULT_MAX_DAILY_AI_CREDITS` GitHub Actions variable (action runtime)
4. Built-in constant default: `5000` AIC

The compiler emits `${{ vars.GH_AW_DEFAULT_MAX_DAILY_AI_CREDITS || '5000' }}` when no frontmatter or imported value is set, so the organization variable is resolved at workflow run time by the GitHub Actions runner — not at compile time. A value of `-1` in frontmatter explicitly disables the guardrail. Positive values accept `K`/`M` suffixes such as `100M`.

For the generated `agent` job timeout, precedence is:

1. `jobs.agent.timeout-minutes` in workflow frontmatter
2. `vars.GH_AW_DEFAULT_AGENT_JOB_TIMEOUT_MINUTES`
3. Built-in compiler default of 60 minutes

The generated `detection` job timeout follows the same chain with
`jobs.detection.timeout-minutes`, `vars.GH_AW_DEFAULT_DETECTION_JOB_TIMEOUT_MINUTES`,
and a built-in default of 10 minutes. The `agentic_execution` step timeout uses
top-level `timeout-minutes`, `vars.GH_AW_DEFAULT_TIMEOUT_MINUTES`, and a built-in
default of 20 minutes.

For OTLP observability, precedence is:

1. `observability.otlp` in workflow frontmatter
2. `observability.otlp` from imported shared workflows
3. `secrets.GH_AW_DEFAULT_OTLP_ENDPOINT`, falling back to `vars.GH_AW_DEFAULT_OTLP_ENDPOINT`, with `secrets.GH_AW_DEFAULT_OTLP_HEADERS` (action runtime)

The compiler always emits OTLP environment variables. When no endpoint is configured in frontmatter or an import, it emits
`${{ secrets.GH_AW_DEFAULT_OTLP_ENDPOINT || vars.GH_AW_DEFAULT_OTLP_ENDPOINT }}` and `${{ secrets.GH_AW_DEFAULT_OTLP_HEADERS }}`
so a repository or organization can enable telemetry for every agentic workflow without editing individual workflows. The
endpoint secret takes precedence over the variable; clear both the endpoint secret and variable to disable export. When both are unset, the endpoint resolves to an empty string and OTLP export
becomes a no-op; a configured endpoint without the matching headers secret is dropped by every span-emitting job
(setup, conclusion, outcome, and MCP gateway) instead of being exported unauthenticated, and the agent job additionally fails the
run so the misconfiguration is visible. If a workflow's own `env:` block already defines one of the OTLP variables, the compiler
skips injecting that variable rather than emitting a duplicate mapping key.

For detection engine selection, precedence is:

1. `safe-outputs.threat-detection.engine` in workflow frontmatter
2. Main workflow engine (`engine`)
3. Built-in compiler default

For detection model selection, precedence is:

1. `safe-outputs.threat-detection.engine.model` in workflow frontmatter
2. `GH_AW_DEFAULT_DETECTION_MODEL`
3. Engine-specific detection defaults

For project timezone (rendered CLI timestamps), precedence is:

1. `utc` in `.github/workflows/aw.json`
2. `GH_AW_DEFAULT_UTC`
3. The runner's local clock (formatting left unchanged)

## Example

Set an org-wide Codex model fallback:

```bash
gh variable set GH_AW_DEFAULT_MODEL_CODEX --org my-org --body "gpt-5.5"
```

Set an org-wide default max-ai-credits guardrail:

```bash
gh variable set GH_AW_DEFAULT_MAX_AI_CREDITS --org my-org --body "15M"
```

```bash
gh variable set GH_AW_DEFAULT_MAX_AI_CREDITS --org my-org --body "100M"
```

Set an org-wide default detection max-ai-credits guardrail:

```bash
gh variable set GH_AW_DEFAULT_DETECTION_MAX_AI_CREDITS --org my-org --body "750"
```

Set an org-wide default daily workflow AIC guardrail:

```bash
gh variable set GH_AW_DEFAULT_MAX_DAILY_AI_CREDITS --org my-org --body "15M"
```

Set an organization-wide GitHub Actions variable for the timeout and compiler process defaults for max-turns:

```bash
gh variable set GH_AW_DEFAULT_TIMEOUT_MINUTES --org my-org --body "30"
gh variable set GH_AW_DEFAULT_AGENT_JOB_TIMEOUT_MINUTES --org my-org --body "90"
gh variable set GH_AW_DEFAULT_DETECTION_JOB_TIMEOUT_MINUTES --org my-org --body "15"
export GH_AW_DEFAULT_MAX_TURNS=12
export GH_AW_DEFAULT_MAX_TURN_CACHE_MISSES=7
export GH_AW_DEFAULT_DETECTION_MODEL=gpt-5.5-mini
```

Set an org-wide default project timezone (Pacific Standard Time):

```bash
gh variable set GH_AW_DEFAULT_UTC --org my-org --body "-08:00"
```
