---
description: Consumer reference for OpenTelemetry signals, attributes, export, and query semantics in GitHub Agentic Workflows.
---

# OpenTelemetry reference

This guide describes telemetry available to downstream collectors and dashboards. Telemetry is optional and conditional on workflow configuration and available run data. An absent span or attribute means unknown or unavailable, not zero.

## Signals and span placement

gh-aw exports traces over OTLP/HTTP. Built-in spans identify job setup and conclusion; agent execution can have a dedicated agent span. Other spans may describe admission checks, work-queue operations, or tool calls. Span names and attributes are useful for grouping, but are not a promise that every workflow or engine emits every span.

Conclusion spans can include `exception` events. When grader or evaluation data is available, spans can include `grader.result` and `eval.result` events. Session logs are an opt-in signal using the `gh-aw.session` scope; they are not included by default. gh-aw does not currently emit metrics. Metric names mentioned in semantic-convention proposals are not evidence of an available metric stream.

## Identity and measurements

Resource attributes identify the service and workflow run, for example `service.name`, `service.version`, `github.repository`, `github.run_id`, `github.run_attempt`, and `github.workflow_ref`. Span attributes describe the job and measurements. Common families include:

| Family | Examples |
|---|---|
| Workflow identity and outcome | `gh-aw.workflow.name`, `gh-aw.job.name`, `gh-aw.run.status`, `gh-aw.agent.conclusion` |
| Model usage | `gen_ai.system`, `gen_ai.request.model`, `gen_ai.response.model`, `gen_ai.usage.input_tokens`, `gen_ai.usage.output_tokens` |
| Cost and budget | `gh-aw.aic`, `gh-aw.max_ai_credits`, `gh-aw.max_ai_credits_exceeded` |
| Failures and export health | `gh-aw.error_count`, `exception`, `gh-aw.otlp.export_errors` |
| Routing, firewall, and tools | `gh-aw.model_routing.*`, `gh-aw.firewall.*`, `gh-aw.mcp.*` |
| Graders and evaluations | `gh-aw.graders.*`, `gh-aw.evals.*`, `grader.result`, `eval.result` |
| Work queue | `gh-aw.work_queue.*` |

See the [OpenTelemetry attribute reference](../../docs/src/content/docs/reference/open-telemetry-attributes.mdx) for attribute descriptions and availability.
For work-queue operation names and instrumentation availability, see [OpenTelemetry queue health](../../docs/src/content/docs/reference/work-queue.md#open-telemetry-queue-health).

AI credits (`gh-aw.aic`) and token usage belong to the span that owns the usage: usually the dedicated agent span, or the executing job's conclusion span when no dedicated agent span is emitted. A run can have other conclusion spans that do not own that usage. Deduplicate records by trace ID and span ID before aggregating mirrored data. Explicit numeric zero is a measured value; missing cost or token data is not zero.

Work-queue attributes are point-in-time snapshots on operation spans. They are not counters and should not be summed across operations. Queue identifiers are hashed; treat remaining high-cardinality values as trace data rather than default metric dimensions.

## Export and local artifacts

The configured OTLP protocol can be `http/json` or `http/protobuf`, selected with `OTEL_EXPORTER_OTLP_TRACES_PROTOCOL` or `OTEL_EXPORTER_OTLP_LOGS_PROTOCOL`. Traces use `/v1/traces`; logs use `/v1/logs`. JSON and protobuf are supported for network export.

When the helper telemetry artifact is uploaded, `otel.jsonl` contains one raw OTLP/HTTP JSON fragment per line, with `resourceSpans` for traces. It is not a flattened event stream. Session logs are exported to the configured endpoint and are not included in this local trace mirror. Some engine-native spans export directly to the configured backend and may not appear in the helper artifact, so the artifact is not a complete trace inventory.

Durable engine spans, when available, are included in the exported trace stream and local trace mirror. Export failures can be represented by `gh-aw.otlp.export_errors` and `gh-aw.otlp.export_error_details`; those attributes describe observed export problems, not necessarily missing local records.

## Encoding and privacy

OTLP JSON spans are under `resourceSpans[].scopeSpans[].spans[]`; log records are under `resourceLogs[].scopeLogs[].logRecords[]`. Resource attributes and span attributes are separate. Numeric attributes can be integers or doubles; integer values in session log bodies are string-encoded, while resource attribute integers remain numeric. Span timestamps in the local mirror are decimal nanosecond strings.

Telemetry is redacted before export where sensitive-looking attribute keys are recognized. Redaction is not a reason to include credentials or private data in custom attributes. Do not send secrets, exporter headers, prompts, tool arguments, or tool results as telemetry. Treat identifiers and free-form errors as potentially sensitive and high-cardinality.

## Query recipes

The following examples operate on the helper `otel.jsonl` mirror:

```bash
F=otel.jsonl
# Span durations in seconds
jq -r '.resourceSpans[]?.scopeSpans[]?.spans[]? | [.name, ((.endTimeUnixNano|tonumber)-(.startTimeUnixNano|tonumber))/1e9] | @tsv' "$F"
# AI credits on usage-owning spans, deduplicated by trace and span ID
jq -s '[.[].resourceSpans[]?.scopeSpans[]?.spans[]? | {k:(.traceId+"/"+.spanId), a:((.attributes[]?|select(.key=="gh-aw.aic")|.value.doubleValue) // null)} | select(.a!=null)] | unique_by(.k) | map(.a) | add' "$F"
# Token attributes wherever usage is owned
jq -r '.resourceSpans[]?.scopeSpans[]?.spans[]? | .attributes[]? | select(.key|startswith("gen_ai.usage.")) | [.key,(.value.intValue // .value.doubleValue)]|@tsv' "$F"
# Error spans
jq -r '.resourceSpans[]?.scopeSpans[]?.spans[]? | select(.status.code==2) | [.name,.status.message//""]|@tsv' "$F"
```

An empty result does not establish zero usage. For backend queries, filter resource identity at the resource level and job or measurement fields at the span level. Follow trace/span identity when combining backend exports with local mirrors.
