---
title: gh-aw OpenTelemetry Observability Specification
description: Formal W3C-style specification for observability contract for GitHub Agentic Workflows using OpenTelemetry traces, metrics, logs, and OTLP export.
version: 0.6.0
status: Working Draft
date: 2026-06-18
last_updated: 2026-11-09
editors:
  - GitHub gh-aw Team
---

# gh-aw OpenTelemetry Observability Specification

**Version**: 0.6.0
**Status**: Working Draft  
**Publication Date**: June 18, 2026
**Latest Version**: https://github.com/github/gh-aw/blob/main/specs/otel-observability-spec.md  
**Previous Version**: 0.5.0
**Editors**: GitHub gh-aw Team

---

## Abstract

This specification defines the compatibility-first OpenTelemetry observability contract for GitHub Agentic Workflows (`gh-aw`). It specifies the existing `observability.otlp` configuration surface, OTLP export behavior, trace-context compatibility variables, resource identity, built-in span attributes, local telemetry mirrors, security controls, and conformance obligations. Version 0.4.0 preserves the contract already used by workflows, dashboards, and artifacts, and treats additional OpenTelemetry alignment as additive evolution rather than a replacement standard.

## Status of This Document

This document is a **Working Draft** maintained by the GitHub `gh-aw` Team. It may be changed, replaced, or made obsolete by subsequent versions.

Version 0.6.0 clarifies fail-closed safeguards and required export, fan-out, and shutdown ordering without changing the shipped compatibility contract.

Sections explicitly marked **Informative** are non-normative. All other sections are normative unless stated otherwise.

Implementation mapping, validation requirements, and compatibility test expectations are defined in this specification so version 0.6.0 has one authoritative contract.

Implementations claiming conformance MUST identify the version and conformance classes they implement. A conformance claim for version 0.6.0 is a compatibility claim: it MUST preserve externally observable behavior documented by this specification unless a later specification explicitly defines a migration period and compatibility alias.

## Table of Contents

1. [Introduction](#1-introduction)
2. [Conformance](#2-conformance)
3. [Terminology](#3-terminology)
4. [Observability Architecture](#4-observability-architecture)
5. [Configuration Model](#5-configuration-model)
6. [Runtime Environment and Export](#6-runtime-environment-and-export)
7. [Context Propagation](#7-context-propagation)
8. [Resource and Instrumentation Identity](#8-resource-and-instrumentation-identity)
9. [Trace Model](#9-trace-model)
10. [Span and Event Contracts](#10-span-and-event-contracts)
11. [Metrics Contract](#11-metrics-contract)
12. [Logs Contract](#12-logs-contract)
13. [Outcome Evaluation](#13-outcome-evaluation)
14. [Local Mirrors and Artifacts](#14-local-mirrors-and-artifacts)
15. [Security and Privacy](#15-security-and-privacy)
16. [Reliability and Failure Handling](#16-reliability-and-failure-handling)
17. [Compliance Testing](#17-compliance-testing)
18. [References](#18-references)
19. [Change Log](#19-change-log)

---

## 1. Introduction

### 1.1 Purpose

This specification ensures that `gh-aw` observability is interoperable, testable, safe by default, and aligned with OpenTelemetry conventions.

A conforming implementation enables operators to answer the following questions:

- **Metrics**: Is workflow or AI behavior unhealthy, slow, expensive, or unreliable at aggregate scale?
- **Traces**: Which job, model request, tool execution, gateway operation, or external dependency caused a specific run to fail or become slow?
- **Logs and events**: What exact diagnostic, policy, exception, or exporter condition occurred?

### 1.2 Scope

This specification covers:

- `observability.otlp` workflow configuration;
- endpoint and header normalization;
- direct-export and Collector-mediated export modes;
- W3C Trace Context propagation across jobs, child workflows, containers, HTTP, JSON-RPC, and MCP;
- CI/CD, GenAI, MCP, HTTP, RPC, and repository-specific telemetry attributes;
- trace, metric, log, and local-mirror contracts;
- outcome evaluation and long-lived correlation;
- security, privacy, cardinality, and reliability requirements.

This specification does not define vendor-specific dashboards, retention policies, model-quality algorithms, backend pricing formulas, or a requirement to use a particular OpenTelemetry backend.

### 1.3 Design Goals

The design goals are:

1. **Standards first**: Reuse applicable OpenTelemetry conventions before defining custom attributes.
2. **Compatibility first**: Preserve the telemetry fields, files, and environment variables already documented for users.
3. **Additive standards alignment**: Add standard OpenTelemetry aliases when useful, without replacing shipped `gh-aw` fields or GenAI attributes.
4. **Correct temporal containment**: New parent-child relationships SHOULD contain the operations represented by their child spans.
5. **Safe defaults**: Do not capture prompts, responses, credentials, or unbounded payloads by default.
6. **Graceful degradation**: Observability failure SHOULD NOT fail useful workflow execution.
7. **Portable correlation**: Support W3C `traceparent` while preserving `GITHUB_AW_OTEL_TRACE_ID` and `GITHUB_AW_OTEL_PARENT_SPAN_ID` as stable compatibility variables.
8. **Bounded cardinality**: Unique run, commit, item, or user identifiers MUST NOT be metric dimensions by default.

---

## 2. Conformance

### 2.1 Requirements Notation

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be interpreted as described in [RFC 2119](https://www.ietf.org/rfc/rfc2119.txt).

### 2.2 Conformance Classes

This specification defines the following conformance classes:

| Class | Responsibility |
|---|---|
| **Compiler** | Parses workflow observability configuration, validates it, and emits the runtime contract. |
| **Runtime Instrumentation** | Creates resources, built-in spans, optional metrics/logs/events/links, and local mirrors. |
| **Exporter** | Sends telemetry through direct OTLP export or an OpenTelemetry Collector and implements retry and fan-out behavior. |
| **Gateway Instrumentation** | Propagates context and instruments MCP, JSON-RPC, HTTP, proxy, and backend operations. |
| **Artifact Producer** | Persists local telemetry mirrors and associated manifests as workflow artifacts. |
| **Validator** | Verifies emitted telemetry and implementation behavior against this specification. |

An implementation MAY conform to one or more classes. It MUST state each claimed class.

### 2.3 Compliance Levels

| Level | Name | Requirements |
|---|---|---|
| **Level 1** | Stable Configuration and Export | Compiler configuration, validation, endpoint/header normalization, compatibility runtime variables, direct OTLP export, and secret-safe setup in Sections 5 and 6. |
| **Level 2** | Compatible Correlation | Level 1 plus resource identity, stable trace IDs, W3C `TRACEPARENT` compatibility, built-in setup/conclusion/agent spans, MCP gateway correlation, local mirrors, and artifact safety. |
| **Level 3** | Extended Observability | Level 2 plus optional OpenTelemetry-native root/job spans, metrics, structured logs, outcome links, Collector mode, and other additive records. |

A claim of a compliance level MUST satisfy every MUST and MUST NOT requirement applicable to the implementation's conformance classes at that level. Level 3 features MUST be additive: they MUST NOT remove or rename Level 1 or Level 2 fields, files, environment variables, or span attributes.

An implementation that does not satisfy every applicable requirement MAY describe itself as **partially implemented**, but MUST NOT claim conformance at that level.

### 2.4 Extension Conformance

Repository-specific attributes and instruments MAY be added. An extension:

- MUST NOT change the meaning of a standard OpenTelemetry attribute;
- MUST NOT reuse a standard name with an incompatible type;
- MUST use a documented namespace;
- MUST document stability and cardinality expectations;
- MUST NOT claim to be an OpenTelemetry standard unless accepted by the relevant OpenTelemetry specification process.

---

## 3. Terminology

| Term | Definition |
|---|---|
| **Workflow run** | One execution of a GitHub Actions workflow. |
| **Pipeline root span** | The real root span representing the complete workflow run. |
| **Job span** | A child span representing one GitHub Actions job for its complete measured lifetime. |
| **Agent invocation** | Execution of an AI agent or agent framework, potentially containing several model and tool operations. |
| **Model operation** | One logical request to a generative model, including automatic retries observed by the caller. |
| **Tool operation** | Execution of one tool, function, command, API operation, or MCP method. |
| **OTLP entry** | A normalized endpoint record containing a URL and optional exporter-only headers. |
| **Primary endpoint** | The first endpoint after normalization, used for single-endpoint compatibility. |
| **Collector mode** | Export through a local or remote OpenTelemetry Collector. |
| **Direct mode** | Export directly from `gh-aw` instrumentation to a vendor OTLP endpoint. |
| **Telemetry mirror** | A local JSON Lines representation of telemetry records produced independently of remote export success. |
| **Span link** | A non-parental relationship from one span to another span context, used for delayed or asynchronous correlation. |
| **Sensitive content** | Prompts, model output, tool arguments, tool results, source snippets, credentials, personal data, private repository data, or equivalent content. |
| **High-cardinality value** | A value with a large or unbounded number of distinct values, including run IDs, commit SHAs, item URLs, user IDs, and arbitrary text. |

---

## 4. Observability Architecture

### 4.1 Signal Responsibilities

A conforming Level 3 implementation MUST support the following signal model:

| Signal | Purpose |
|---|---|
| **Traces** | Explain the structure and latency of one workflow run. |
| **Metrics** | Monitor aggregate behavior across runs. |
| **Logs** | Preserve detailed diagnostics and audit-relevant events. |

A metric MUST NOT be represented only as a fleet-summary span when the value is intended for aggregation across multiple runs.

### 4.2 Component Model

Collector mode is RECOMMENDED for production because it separates exporter credentials from agent execution and provides centralized batching, retry, filtering, and fan-out.

Direct mode is part of the stable compatibility contract and MUST remain available for existing workflows.

### 4.3 Trust Boundaries

The compiler, telemetry helper, and Collector are trusted observability components.

Agent commands, generated code, tool processes, checked-out repository content, and externally supplied issue or pull-request content MUST be treated as potentially untrusted.

Exporter credentials MUST be made available only to trusted observability components. They MUST NOT be exposed to an untrusted agent process unless no supported isolation mechanism exists and the deployment explicitly accepts that risk.

### 4.4 Time Model

All timestamps MUST use Unix epoch nanoseconds in OTLP records.

Custom duration metrics defined by this specification MUST use seconds (`s`).

Clock comparisons across jobs SHOULD tolerate runner clock skew. Implementations SHOULD derive job duration from timestamps captured on the same runner whenever possible.

---

## 5. Configuration Model

### 5.1 Frontmatter Declaration

A workflow MAY declare an `observability.otlp` object.

When `observability.otlp` is absent:

- the compiler MUST NOT configure remote OTLP export;
- the runtime MAY still produce local telemetry mirrors;
- the runtime MAY still propagate trace context for local correlation.

The stable object contains `endpoint`, `headers`, `if-missing`, `attributes`, `resource-attributes`, and `github-app`. Future fields such as `mode`, `signals`, or `capture-content` MAY be added only after they are implemented, documented, and accepted by the frontmatter schema. Until then, a strict schema MAY reject those future fields.

### 5.2 Endpoint Forms

The `endpoint` field MUST accept exactly these forms:

1. a URL string;
2. an object containing `url` and optional `headers`;
3. an ordered array of objects containing `url` and optional `headers`.

The compiler MUST normalize accepted forms into an ordered list of OTLP entries.

An entry without a non-empty URL MUST be discarded with a diagnostic.

If no valid entry remains, remote export MUST be treated as disabled and `if-missing` behavior MUST apply.

### 5.3 Headers

Header declarations MAY be a map from header name to string value or a comma-separated `key=value` string compatible with the selected exporter.

Map-form headers MUST be serialized deterministically by ascending header name.

Top-level `headers` MUST apply only to the string endpoint form. Object and array forms MUST use per-entry headers.

Headers MUST be classified as secrets unless explicitly documented otherwise.

Headers MUST NOT be included in generated gateway JSON, job summaries, logs, span attributes, metric attributes, or telemetry mirrors.

For compatibility with existing multi-endpoint fan-out, trusted exporter variables such as `OTEL_EXPORTER_OTLP_HEADERS`, `GH_AW_OTLP_ALL_HEADERS`, or `GH_AW_OTLP_ENDPOINTS` MAY carry endpoint-local header material. When they do, implementations MUST mask the values before diagnostics and MUST NOT pass them to untrusted agent commands, generated code, or backend tool processes unless explicitly documented by that runtime boundary.

### 5.4 Sentry Compatibility

For a statically identifiable Sentry OTLP endpoint, the compiler MAY rewrite an `Authorization` header to `x-sentry-auth` when required by the supported Sentry ingestion contract.

Such rewriting MUST be documented as vendor-specific compatibility behavior and MUST NOT change headers for non-Sentry endpoints.

### 5.5 Missing-Value Policy

`if-missing` MUST accept `error`, `warn`, and `ignore`. The default MUST be `error`.

Invalid values MUST cause compile-time validation failure when statically known. If an invalid value can only be detected at runtime, it MUST be treated as `error` and a structured diagnostic MUST be emitted.

The policy applies to required exporter setup values, not to ordinary telemetry delivery failure after valid setup.

### 5.6 Reserved Extension Fields

`mode`, `signals`, and `capture-content` are reserved extension fields. They are not part of the Level 1 or Level 2 compatibility contract in version 0.4.0.

If an implementation adds `mode`, it SHOULD accept `collector`, `direct`, and `auto`, and direct export MUST remain available for existing workflows.

If an implementation adds `signals`, it SHOULD allow a subset of `traces`, `metrics`, and `logs`, but traces MUST remain the default for compatibility with current workflows.

If an implementation adds `capture-content`, its default MUST be `none`. `metadata` MAY record sizes, counts, MIME types, hashes, and classification labels, but MUST NOT record raw prompts, model responses, tool arguments, or tool results. `full` MUST require explicit opt-in and MUST be rejected unless the implementation has an active redaction policy and the workflow is authorized to export sensitive content.

### 5.7 Static Endpoint Allowlisting

When an endpoint URL is statically resolvable, the compiler MUST extract its hostname and add it to the network allowlist required by the workflow sandbox.

Expressions such as `${{ secrets.OTLP_ENDPOINT }}` MUST NOT produce a compile-time hostname allowlist entry.

### 5.8 Default Fallback

When a workflow declares no `observability.otlp` endpoint in its own frontmatter or in any imported workflow, the compiler MUST fall back to a repository- or organization-level default endpoint expressed as `${{ secrets.GH_AW_DEFAULT_OTLP_ENDPOINT || vars.GH_AW_DEFAULT_OTLP_ENDPOINT }}` with headers `${{ secrets.GH_AW_DEFAULT_OTLP_HEADERS }}`. The secret takes precedence when both endpoint values are configured; implementations MUST require clearing both endpoint values to disable export when both are present.

An explicit `observability.otlp` endpoint from frontmatter or an import MUST take precedence over the default fallback; the fallback MUST only apply when no endpoint entry can be resolved from frontmatter or imports.

The compiler MUST set the effective `if-missing` policy to `ignore` for the default fallback path when the workflow has not explicitly configured `if-missing`. This makes an unset `GH_AW_DEFAULT_OTLP_ENDPOINT` a silent no-op instead of a compile- or runtime-time failure.

A default fallback endpoint that resolves to a non-empty URL without its configured headers MUST fail the default-credentials check with a diagnostic identifying the missing headers secret. When the effective `if-missing` policy is `ignore`, every runtime exporter MUST also omit the endpoint before any network call, including exporters in jobs that do not execute that check. The complete fail-closed behavior and ordering are specified in §16.6.

The compiler MUST NOT emit an env-block mapping key that the workflow's own `env:` block already defines. When a user-defined key already exists (for example `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_SERVICE_NAME`, `OTEL_RESOURCE_ATTRIBUTES`, `OTEL_EXPORTER_OTLP_HEADERS`, `GH_AW_OTLP_ALL_HEADERS`, `GH_AW_OTLP_ENDPOINTS`, `GH_AW_OTLP_IF_MISSING`, or `GH_AW_OTLP_ATTRIBUTES`), the compiler MUST skip injecting that key rather than emit a duplicate mapping key.

---

## 6. Runtime Environment and Export

### 6.1 Stable Runtime Variables

When observability is enabled, the compiler MUST make the following non-secret variables available to trusted runtime instrumentation:

| Variable | Requirement |
|---|---|
| `OTEL_SERVICE_NAME` | MUST be `gh-aw.<sanitized-workflow-id>` or `gh-aw` when no identifier is available. |
| `OTEL_RESOURCE_ATTRIBUTES` | SHOULD contain stable gh-aw resource attributes when OTLP is configured. |
| `GITHUB_AW_OTEL_TRACE_ID` | MUST contain the active gh-aw trace ID when a trace has been created. |
| `GITHUB_AW_OTEL_PARENT_SPAN_ID` | MUST contain the active setup or parent span ID when available. |
| `TRACEPARENT` | SHOULD be emitted or forwarded where child tools can consume W3C Trace Context. |
| `GH_AW_OTLP_ENDPOINTS` | SHOULD contain a compact JSON array for multi-endpoint fan-out when more than one endpoint or endpoint-local header set is configured. |
| `GH_AW_OTLP_IF_MISSING` | SHOULD contain the resolved policy when runtime setup needs it; MUST be `ignore` when the endpoint was resolved through the default fallback (§5.8) and the workflow did not explicitly configure `if-missing`. |

Future variables such as `GH_AW_OTLP_MODE`, `GH_AW_OTEL_SIGNALS`, and `GH_AW_OTEL_CAPTURE_CONTENT` MAY be added only as additive extensions.

### 6.2 OTLP Export Compatibility Variables

The trusted exporter process MAY receive `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_HEADERS`, and signal-specific OTLP endpoint or header variables.

The first normalized endpoint MUST remain the primary endpoint for compatibility variables.

Compatibility variables containing credentials MUST NOT be injected into the environment of the agent command, arbitrary shell steps, generated code, or untrusted tool processes.

The JavaScript traces exporter MUST honor a non-empty `OTEL_EXPORTER_OTLP_TRACES_PROTOCOL` before `OTEL_EXPORTER_OTLP_PROTOCOL`. When neither is set, it MUST preserve `http/json` as the compatibility default. `http/protobuf` MUST encode the existing sanitized trace payload as a binary OTLP `ExportTraceServiceRequest` with `Content-Type: application/x-protobuf`, without runtime package dependencies. The selected protocol applies to every endpoint in the JavaScript fan-out. Unsupported protocols or encoding failures MUST produce an exporter diagnostic without a JSON fallback. The local JSONL mirror MUST remain raw OTLP JSON for both protocols.

`gh aw env get/update` MUST expose `default_otlp_protocol`, mapped to the standard `OTEL_EXPORTER_OTLP_PROTOCOL` Actions variable, with `http/json` and `http/protobuf` as the accepted non-null values. Null or omission MUST delete the variable at the selected scope, consistently with other managed defaults. Unless workflow `env` explicitly defines the protocol, the compiler MUST inject `${{ vars.OTEL_EXPORTER_OTLP_PROTOCOL }}` so variable changes resolve at runtime rather than compilation.

The JavaScript exporter MUST preserve complete endpoint paths ending in `/v1/traces`, including Azure Monitor DCE/DCR traces paths and query parameters. Base endpoints MUST gain `/v1/traces` before any query parameters.

### 6.3 Collector Mode (Optional)

Collector mode is RECOMMENDED for deployments that want centralized batching, retry, redaction, and credential isolation, but direct export is part of the stable compatibility contract. In Collector mode:

1. Application instrumentation MUST export to the configured Collector endpoint.
2. Vendor credentials SHOULD be held by the Collector rather than the workflow-wide environment.
3. Multi-endpoint fan-out SHOULD be performed by Collector exporters.
4. The Collector SHOULD enable batching and queued retry.
5. The Collector SHOULD apply redaction and attribute filtering before external export.

### 6.4 Direct Mode

In direct mode:

1. Exporter headers MUST be scoped to the trusted exporter helper.
2. Each configured endpoint MUST be attempted independently.
3. A failure at one endpoint MUST NOT suppress an attempt to another endpoint.
4. Export retry MUST be bounded by duration and attempt count.
5. Export failure MUST be reported through a structured diagnostic and `gh_aw.otlp.export.failures` when metrics are enabled.
6. Endpoint filtering and missing-credential safeguards MUST follow §16.6 before any endpoint is passed to the exporter.

### 6.5 Gateway Export

The MCP gateway SHOULD export to the local Collector rather than directly to an external vendor.

The gateway configuration MUST NOT embed exporter authentication headers.

When direct gateway export is explicitly configured, credentials MUST be scoped to the gateway process and MUST NOT be inherited by backend tool processes.

### 6.6 Flush and Shutdown

At job completion, instrumentation MUST follow the ordered finalization sequence in §16.5 and attempt to flush buffered telemetry within a bounded timeout. Failure to flush MUST NOT replace the workflow's functional result. It MUST produce an exporter diagnostic and SHOULD increment the exporter-failure metric.

---

## 7. Context Propagation

### 7.1 Required Compatibility Context

A conforming Level 2 or Level 3 implementation MUST preserve the gh-aw compatibility context variables `GITHUB_AW_OTEL_TRACE_ID` and `GITHUB_AW_OTEL_PARENT_SPAN_ID`.

It SHOULD also support W3C Trace Context by injecting or forwarding `traceparent` when present and valid. It MAY support `tracestate` and W3C Baggage through `baggage`.

### 7.2 Environment Carriers

Across GitHub Actions steps and jobs, the stable gh-aw carriers are `GITHUB_AW_OTEL_TRACE_ID` and `GITHUB_AW_OTEL_PARENT_SPAN_ID`.

The implementation SHOULD emit `TRACEPARENT` where child tools can consume W3C Trace Context. It MAY also emit `TRACESTATE` when non-empty and `BAGGAGE` when non-empty.

When both gh-aw carriers and W3C carriers are present, they MUST describe the same active trace context. New W3C carriers MUST NOT replace or suppress the gh-aw carriers in version 0.4.0.

### 7.3 Workflow Root Context

The workflow orchestration layer SHOULD create or resolve a valid root `SpanContext` before additive full-duration job spans are created.

When a root context is created for an additive pipeline root span, it SHOULD contain a 16-byte trace ID, an 8-byte span ID for the pipeline root span, trace flags, and optional trace state.

The corresponding pipeline root span MAY be exported as an additive Level 3 feature. A trace that uses the existing gh-aw setup/conclusion span model is still conforming at Level 1 or Level 2.

### 7.4 Cross-Job Propagation

Each job MUST receive enough context to correlate built-in spans for the workflow run.

A full-duration job span MAY be created as a child of a pipeline root span as an additive Level 3 feature.

The compiler SHOULD propagate `TRACEPARENT` when possible and MUST preserve the existing `GITHUB_AW_OTEL_TRACE_ID` and `GITHUB_AW_OTEL_PARENT_SPAN_ID` propagation path.

### 7.5 Intra-Job Propagation

After an additive full-duration job span is started, downstream steps that create child operations SHOULD receive the current job span context.

A setup child span MUST NOT be used as the parent of the entire job unless the setup child span actually remains open for the entire job duration.

### 7.6 Child Workflows and Dispatch

A dispatched or reusable child workflow SHOULD continue the trace when the parent invocation causally waits for or coordinates the child.

The parent SHOULD pass `traceparent`, and MAY pass `tracestate` and `baggage`, through the supported workflow-call context.

If the child is asynchronous and the parent operation does not remain active, the child workflow root SHOULD start a new trace with a span link to the parent context.

### 7.7 HTTP, JSON-RPC, and MCP

W3C Trace Context SHOULD be injected into HTTP requests when transport permits.

MCP over Streamable HTTP SHOULD use the HTTP carrier.

MCP over stdio or another non-HTTP transport SHOULD use a documented protocol carrier or process-level context handoff that preserves the complete span context.

An MCP client span and MCP server span SHOULD have a valid parent-child or link relationship according to the active transport conventions.

### 7.8 Invalid Context

An invalid incoming `traceparent` MUST be ignored and MUST NOT be partially reused.

The implementation SHOULD emit a structured warning without including the full invalid header value.

---

## 8. Resource and Instrumentation Identity

### 8.1 Required Resource Attributes

Level 1 and Level 2 `gh-aw` telemetry MUST preserve the stable gh-aw resource attributes already emitted by the compiler and JavaScript helpers. Standard OpenTelemetry resource attributes MAY be added as aliases.

Stable resource attributes include:

| Attribute | Type | Requirement |
|---|---|---|
| `service.name` | string | REQUIRED. `gh-aw.<workflow-id>` or `gh-aw`. |
| `service.version` | string | REQUIRED when the CLI version or commit is known. |
| `gh-aw.workflow.name` | string | REQUIRED when the workflow name is known; otherwise SHOULD be `unknown`. |
| `gh-aw.repository` | string | REQUIRED when repository identity is available. |
| `gh-aw.run.id` | string | REQUIRED for traces and logs. MUST be opt-in for metrics. |
| `github.run_id` | string | REQUIRED when GitHub run ID is available. |
| `gh-aw.engine.id` | string | RECOMMENDED when an engine ID is known. |

Additive standard aliases MAY include `cicd.pipeline.name`, `cicd.pipeline.run.id`, `cicd.pipeline.run.url.full`, `vcs.repository.url.full`, and `deployment.environment.name`.

### 8.2 GitHub Compatibility Attributes

Custom GitHub attributes MAY be emitted for compatibility, including repository, run ID, run attempt, run URL, event name, ref, SHA, workflow ref, actor ID, runner OS, runner architecture, runner name, and runner environment.

Where a standard OpenTelemetry attribute exists, the standard attribute MAY be emitted as an alias. The existing `gh-aw.*` or `github.*` attribute MUST remain available during version 0.4.x.

### 8.3 Metric Resource Cardinality

A metric provider MUST NOT attach the per-run `cicd.pipeline.run` entity or equivalent unique run attributes by default.

Workflow run IDs, job run IDs, trace IDs, span IDs, commit SHAs, pull-request or issue numbers, actor IDs, item URLs, and conversation IDs MUST NOT be metric resource attributes or metric dimensions by default.

### 8.4 Instrumentation Scope

Telemetry emitted by the core runtime MUST use instrumentation scope name `gh-aw` and scope version equal to the `gh-aw` version or commit identifier.

Gateway telemetry SHOULD use a gateway-specific scope such as `gh-aw-mcpg`.

---

## 9. Trace Model

### 9.1 General Rule

One GitHub Actions workflow run SHOULD be represented as one trace when the run is synchronously coordinated as one execution.

The trace MAY contain a recorded pipeline root span as an additive Level 3 feature. It is conforming for Level 1 and Level 2 implementations to use the existing setup, conclusion, agent, and custom-span model without a separate recorded root span.

### 9.2 Compatibility Hierarchy and Additive Root Model

The stable compatibility model includes built-in setup and conclusion spans for jobs, optional agent spans, MCP gateway spans when configured, and custom spans emitted through `otlp.cjs` or `send_otlp_span.cjs`.

An implementation MAY add the following OpenTelemetry-native hierarchy as a Level 3 extension:

```text
RUN <workflow>                                  SERVER
├── <job: activation>                          INTERNAL
│   ├── gh-aw.job.setup                        INTERNAL
│   ├── activation work                        INTERNAL
│   └── gh-aw.job.finalize                     INTERNAL
├── <job: agent>                               INTERNAL
│   ├── gh-aw.job.setup                        INTERNAL
│   ├── invoke_agent <agent>                   INTERNAL
│   │   ├── chat <model>                       CLIENT
│   │   └── execute_tool <tool>                INTERNAL or CLIENT
│   └── gh-aw.job.finalize                     INTERNAL
└── <job: safe_outputs>                        INTERNAL
    ├── apply safe outputs                     INTERNAL
    └── gh-aw.job.finalize                     INTERNAL
```

Setup and finalization MAY remain represented by the existing setup and conclusion spans. They MAY be represented as span events on future job spans when their duration is negligible or not independently useful.

A finalization span or event SHOULD occur after the operations it summarizes. New finalization spans or events MUST NOT be the parent of an earlier agent or model operation.

### 9.3 Pipeline Root Span

A pipeline root span, when emitted, SHOULD cover the workflow-run interval known to the implementation, SHOULD use span kind `SERVER`, SHOULD be named `RUN <pipeline-name>` when the name is low cardinality, and SHOULD set `cicd.pipeline.result` when the result is known.

An additive pipeline root span SHOULD set `ERROR` status and `error.type` when the pipeline fails due to an error. It SHOULD leave status `UNSET` on success rather than setting `OK` solely to restate success.

### 9.4 Job Spans

Each additive GitHub Actions job span SHOULD cover the measured job lifetime, SHOULD use span kind `INTERNAL`, and SHOULD be a child of the pipeline root span or an explicitly documented orchestration span.

Each additive job span SHOULD include `cicd.pipeline.task.name`, `cicd.pipeline.task.run.id`, `cicd.pipeline.task.run.result` when known, and `cicd.pipeline.task.run.url.full` when available.

Each additive job span SHOULD set `ERROR` status and `error.type` when the task fails due to an error.

### 9.5 Agent Invocation Spans

An in-process agent invocation, when emitted as a new dedicated span:

- SHOULD use `gen_ai.operation.name = "invoke_agent"`;
- SHOULD use span kind `INTERNAL`;
- SHOULD be named `invoke_agent <agent-name>` when a low-cardinality name is available;
- SHOULD include `gen_ai.agent.name` and `gen_ai.agent.version` when available;
- SHOULD contain child spans for model, retrieval, planning, memory, and tool operations.

A remote hosted-agent invocation SHOULD use span kind `CLIENT` according to the applicable GenAI agent semantic convention.

Existing gh-aw built-in agent spans MAY continue to use `gen_ai.operation.name = "chat"` for compatibility with deployed dashboards. A future dedicated `invoke_agent` span MUST be additive and MUST NOT remove the existing documented attributes without a migration plan.

### 9.6 Model Operation Spans

Each additive logical model operation span SHOULD be a separate child span of the agent or workflow operation, SHOULD use span kind `CLIENT`, and SHOULD set the applicable `gen_ai.operation.name`, such as `chat`, `generate_content`, `embeddings`, or `text_completion`.

Each model operation span SHOULD set `gen_ai.provider.name` when the operation calls a GenAI provider, SHOULD include `gen_ai.request.model` when known, SHOULD include `gen_ai.response.model` when returned, and SHOULD include input and output token attributes when provided by the provider.

For compatibility, built-in gh-aw spans MAY continue to emit `gen_ai.system` and `gen_ai.usage.total_tokens`. New standard aliases such as `gen_ai.provider.name` MAY be emitted in addition to, not instead of, the compatibility attributes.

### 9.7 Tool and MCP Spans

A tool executed directly in the agent process SHOULD use span name `execute_tool <tool-name>`, `gen_ai.operation.name = "execute_tool"`, `gen_ai.tool.name`, and span kind `INTERNAL`.

An MCP client tool call SHOULD use `mcp.method.name`, `gen_ai.operation.name = "execute_tool"`, `gen_ai.tool.name` when known, and span kind `CLIENT`.

The corresponding MCP server span SHOULD use span kind `SERVER`.

When an existing GenAI tool span can be reliably enriched with MCP attributes, instrumentation SHOULD avoid creating a duplicate logical tool span.

### 9.8 HTTP and Backend Spans

HTTP client and server operations SHOULD follow applicable OpenTelemetry HTTP semantic conventions.

A gateway backend invocation SHOULD use `CLIENT` span kind when it represents an outbound request.

Internal policy evaluation, routing, filtering, and DIFC processing SHOULD use `INTERNAL` spans or events.

### 9.9 Span Names and Links

Span names MUST be low cardinality and MUST NOT contain run IDs, commit SHAs, issue or pull-request numbers, user input, prompt text, URLs with unbounded paths or query strings, or error messages.

Span links SHOULD be used instead of parent-child relationships when outcome evaluation occurs substantially after the originating run, an asynchronous child workflow starts after the parent span has ended, one operation is causally related to several originating operations, or preserving the original parent would create false temporal containment.

---

## 10. Span and Event Contracts

### 10.1 Pipeline Root Attributes

| Attribute | Type | Requirement |
|---|---|---|
| `cicd.pipeline.result` | string | RECOMMENDED when known for additive pipeline root spans. |
| `cicd.pipeline.action.name` | string | RECOMMENDED; use `RUN`. |
| `gh-aw.run.attempt` | int | RECOMMENDED. |
| `gh-aw.run.actor` | string | OPTIONAL; MUST NOT be a metric dimension. |
| `gh-aw.event_name` | string | RECOMMENDED when known. |
| `gh-aw.staged` | boolean | RECOMMENDED when applicable. |

### 10.2 Job Attributes

| Attribute | Type | Requirement |
|---|---|---|
| `cicd.pipeline.task.name` | string | RECOMMENDED for additive full-duration job spans. |
| `cicd.pipeline.task.run.id` | string | RECOMMENDED for additive full-duration job spans. |
| `cicd.pipeline.task.run.result` | string | RECOMMENDED when known. |
| `cicd.pipeline.task.run.url.full` | string | RECOMMENDED when available. |
| `gh-aw.job.name` | string | REQUIRED when a built-in gh-aw job span knows the job name. |
| `gh-aw.error.count` | int | RECOMMENDED at job completion. |
| `gh-aw.warning.count` | int | RECOMMENDED at job completion. |
| `gh-aw.output.item_count` | int | RECOMMENDED at job completion. |
| `gh-aw.engine.id` | string | RECOMMENDED for agent jobs. |

### 10.3 Model Attributes

| Attribute | Type | Requirement |
|---|---|---|
| `gen_ai.operation.name` | string | REQUIRED. |
| `gen_ai.system` | string | REQUIRED on built-in gh-aw agent spans when the engine/provider mapping is known. |
| `gen_ai.provider.name` | string | RECOMMENDED additive alias when a provider is known. |
| `gen_ai.request.model` | string | CONDITIONALLY REQUIRED when available. |
| `gen_ai.response.model` | string | RECOMMENDED when returned. |
| `gen_ai.response.finish_reasons` | string[] | RECOMMENDED when returned. |
| `gen_ai.usage.input_tokens` | int | RECOMMENDED when returned. |
| `gen_ai.usage.output_tokens` | int | RECOMMENDED when returned. |
| `gen_ai.usage.cache_read.input_tokens` | int | RECOMMENDED when applicable. |
| `gen_ai.usage.cache_creation.input_tokens` | int | RECOMMENDED when applicable. |
| `gen_ai.usage.total_tokens` | int | RECOMMENDED compatibility attribute for built-in gh-aw spans when input or output token counts are available. |
| `gen_ai.usage.reasoning.output_tokens` | int | RECOMMENDED when applicable. |

### 10.4 Tool and MCP Attributes

| Attribute | Type | Requirement |
|---|---|---|
| `gen_ai.operation.name` | string | REQUIRED for tool spans; use `execute_tool`. |
| `gen_ai.tool.name` | string | REQUIRED when known. |
| `mcp.method.name` | string | REQUIRED on MCP spans. |
| `mcp.session.id` | string | RECOMMENDED when present; MUST NOT be a metric dimension. |
| `mcp.protocol.version` | string | RECOMMENDED when known. |
| `jsonrpc.request.id` | string | OPTIONAL; MUST NOT be captured when omitted or null. |
| `rpc.response.status_code` | int | RECOMMENDED when present. |
| `http.request.method` | string | REQUIRED on HTTP spans when applicable. |
| `http.response.status_code` | int | CONDITIONALLY REQUIRED when a response is received. |

Tool arguments and results are opt-in sensitive content and MUST follow Section 15.

### 10.5 Error Recording

When an operation ends in error, the span MUST set status `ERROR`, `error.type` MUST contain a predictable low-cardinality type, and an exception event SHOULD be recorded when an exception object is available.

Exception messages MUST be redacted before export. Stack traces MAY be recorded in logs or exception events when authorized.

Successful operations SHOULD leave span status `UNSET` unless an applicable semantic convention requires otherwise.

### 10.6 Lifecycle Events

The following span events MAY be used on job or agent spans: `gh_aw.job.setup.completed`, `gh_aw.job.finalization.started`, `gh_aw.agent.retry`, `gh_aw.model.retry`, `gh_aw.policy.decision`, and `gh_aw.export.failure`.

Event attributes MUST comply with the same privacy and cardinality rules as span attributes.

### 10.7 Content Attributes

`gen_ai.input.messages`, `gen_ai.output.messages`, `gen_ai.system_instructions`, `gen_ai.tool.call.arguments`, and `gen_ai.tool.call.result` MUST NOT be emitted by default.

When full content capture is enabled, content attributes MUST use the schemas required by the applicable GenAI convention, MUST be redacted before export, SHOULD be truncated according to a documented limit, MUST NOT contain credentials, and MUST NOT be copied into metric attributes.

### 10.8 Eval Results

Completed BinEval records MUST produce `eval.result` events on
`gh-aw.evals.conclusion`, with `gh-aw.eval.id`, normalized `gh-aw.eval.answer`
(`YES`, `NO`, or `UNKNOWN`), and the model when available. Summary attributes
MUST include `gh-aw.evals.count`, `yes`, `no`, and `unknown` under the
`gh-aw.evals.` prefix.

A valid not-evaluated marker MUST instead emit `gh-aw.evals.status =
not_evaluated` and the bounded `gh-aw.evals.reason_code`, both on the span and
an `eval.status` event. When available, the marker's configured Decisions model
ID MUST be included as `gh-aw.eval.model` on both the span and status event.
It MUST NOT fabricate answer records or unknown-answer
counts. Free-form questions, reasons, and error text MUST NOT be included in
these result events.

---

## 11. Metrics Contract

### 11.1 General Requirements

Metrics SHOULD be used for aggregate monitoring across runs when a Level 3 implementation adds metrics.

Metric dimensions MUST be bounded and operationally useful.

An implementation MUST NOT use trace IDs, span IDs, run IDs, job IDs, commit SHAs, actor IDs, issue numbers, pull-request numbers, conversation IDs, raw model responses, error messages, or URLs as metric dimensions by default.

Derived rates such as acceptance rate, waste rate, failure rate, and zero-touch rate SHOULD be computed by the backend from counters rather than emitted as per-run span attributes or gauges.

### 11.2 Standard Metrics

A Level 3 implementation SHOULD emit applicable standard CI/CD metrics, including `cicd.pipeline.run.duration`, `cicd.pipeline.run.active`, `cicd.pipeline.run.errors`, and `cicd.worker.count` when worker inventory is observable.

A Level 3 implementation SHOULD emit `gen_ai.client.operation.duration` for instrumented model-client operations.

A Level 3 implementation SHOULD emit `gen_ai.client.token.usage` when input or output token counts are available. `gen_ai.client.token.usage` MUST distinguish input and output tokens with `gen_ai.token.type`.

### 11.3 gh-aw Metrics

A Level 3 implementation SHOULD emit the following custom metrics:

| Metric | Instrument | Unit | Description |
|---|---|---|---|
| `gh_aw.agent.turns` | Histogram | `{turn}` | Agent turns per completed agent invocation. |
| `gh_aw.workflow.output.items` | Counter | `{item}` | Safe output items produced. |
| `gh_aw.outcome.evaluations` | Counter | `{evaluation}` | Outcome evaluations grouped by bounded result and item type. |
| `gh_aw.outcome.resolution.duration` | Histogram | `s` | Time from item creation to terminal resolution. |
| `gh_aw.outcome.zero_touch` | Counter | `{item}` | Accepted items requiring no recorded human interaction. |
| `gh_aw.otlp.export.failures` | Counter | `{failure}` | Failed export attempts grouped by endpoint class and failure class. |
| `gh_aw.ai.credits.usage` | Counter | `{credit}` | AI credits consumed when a stable credit definition exists. |

### 11.4 Metric Dimensions and Exemplars

Custom metric dimensions MAY include workflow name, job category, engine ID, GenAI provider, requested model, operation name, pipeline result, outcome result, safe-output type, deployment environment, error type, and endpoint class.

Values MUST be normalized and bounded.

Metric implementations SHOULD attach exemplars containing trace and span context when supported by the SDK and backend.

---

## 12. Logs Contract

### 12.1 Structured Logs

Exported logs, when added as a Level 3 feature, SHOULD be structured OpenTelemetry log records or records that can be losslessly transformed into them.

Each log SHOULD include timestamp, observed timestamp when applicable, severity, event name or stable body template, resource attributes, instrumentation scope, trace ID and span ID when a current span exists, and bounded structured attributes.

### 12.2 Required Log Categories

A Level 3 implementation SHOULD produce structured logs for exporter failures and retries, invalid or missing trace context, model-client errors, malformed model or tool output, policy and guardrail decisions, MCP protocol errors, artifact and mirror write failures, and outcome-evaluation diagnostics.

Recommended event names include `gh_aw.export.failure`, `gh_aw.export.retry`, `gh_aw.context.invalid`, `gh_aw.agent.error`, `gh_aw.model.error`, `gh_aw.tool.error`, `gh_aw.policy.decision`, `gh_aw.outcome.evaluation`, and `gh_aw.mirror.write_failure`.

### 12.3 Correlation

A log emitted during a traced operation SHOULD include the active trace ID and span ID when the logging API supports correlation.

A standalone log related to a previous workflow run SHOULD include the source run ID as a log attribute and MAY include a span link representation supported by the backend. It MUST NOT fabricate an active parent-child relationship.

### 12.4 Sensitive Logs

Log bodies and attributes MUST NOT contain OTLP headers, tokens, credentials, complete prompts or responses without explicit authorization, complete tool arguments or results without explicit authorization, unredacted private source code, or unbounded stack traces or payloads.

Log messages SHOULD use stable templates, with variable data placed in structured attributes after redaction.

### 12.5 Opt-in Compact Unified Session Logs

The compiler MUST inject `GH_AW_OTLP_LOGS_ENABLED` and
`GH_AW_OTLP_LOGS_MAX_BYTES` from their same-named GitHub Actions variables.
Session-log export MUST remain disabled unless the enable value is exactly
`true`. An unset byte limit MUST default to 65536; a configured limit MUST be a
positive safe integer. Invalid configuration MUST warn without exporting.

The conclusion job MUST export only the persisted, publication-redacted compact
unified session, with at most 128 events and 16 KiB of projected JSONL.
Export MUST additionally omit repository, workflow/run/session/agent identity,
refs, SHAs, memory identity, branches and filesystem paths. Current secret and
exporter-header masks MUST be applied. Operational metadata and trace
correlation MAY remain; this profile does not guarantee irreversible anonymity.
Full session artifacts MUST remain unchanged.

Each retained event MUST be an OTLP structured log body under instrumentation
scope `gh-aw.session`, with `gh-aw.session.event_type` as a log attribute.
Valid current trace/span IDs SHOULD be included. Export MUST use the existing
endpoint list and isolated per-endpoint headers, posting to `/v1/logs` instead
of `/v1/traces`. A trailing signal path MUST be replaced, not duplicated.

The serialized UTF-8 JSON request MUST NOT exceed `GH_AW_OTLP_LOGS_MAX_BYTES`
per endpoint. Whole low-priority records MUST be omitted to meet the limit,
using the same retention ranking as the compact serializer: format/workflow
metadata, final outcomes and promoted failures, guardrail/grader results,
retry diagnostics, per-step usage, then successful memory updates.
Equal-priority records MUST prefer earlier evidence, and retained records MUST
remain in source order. The source serializer MUST consider late essential
records within its scan budget even after compact output limits are reached.
Resource attributes MUST report compact version, included/omitted event counts,
oversized source lines, scan truncation and overall truncation, including
size-induced omissions. When size or scan limits discard evidence, the exporter
MUST issue a GitHub Actions warning explicitly identifying the export as partial.
If no record fits, the exporter MUST warn and skip the
request. Export failures MUST NOT fail the workflow, and unchanged credentials
MUST NOT be retried after authorization failures.

---

## 13. Outcome Evaluation

### 13.1 Separate Evaluation Execution

Outcome evaluation runs through `gh aw logs --outcomes` after the source workflow. The command returns JSON and optional JSONL; it does not emit outcome spans. A downstream exporter that converts these records to OpenTelemetry SHOULD create its own trace.

It MUST NOT extend the original workflow trace across hours or days when doing so would create false temporal containment.

### 13.2 Source Correlation

Each outcome-evaluation span SHOULD contain a span link to the originating workflow root context when that context was persisted.

When a full span context is unavailable, the evaluation span SHOULD include `gh-aw.outcome.source_run_id`, `gh-aw.outcome.source_workflow`, and `gh-aw.outcome.repo`.

For compatibility with existing processors, implementations MAY additionally emit `gh-aw.outcome.repository` as an alias. New implementations SHOULD prefer `gh-aw.outcome.repo` for consistency with `specs/safe-output-outcome-evaluation.md`.

### 13.3 Evaluation Span and Metrics

The span SHOULD be named `gh-aw.outcome.evaluate` and use `INTERNAL` span kind.

It SHOULD include `gh-aw.outcome.type`, `gh-aw.outcome.result`, `gh-aw.outcome.source_run_id`, `gh-aw.outcome.source_workflow`, and `gh-aw.outcome.repo`.

`gh-aw.outcome.result` SHOULD use the canonical outcome taxonomy defined in `specs/safe-output-outcome-evaluation.md` (`accepted`, `rejected`, `ignored`, `pending`, `lifecycle`, `lifecycle_close`).

URLs and item identifiers MUST NOT be metric dimensions.

The implementation MAY emit aggregate outcome values as metrics described in Section 11.3.

A daily or fleet summary span MAY exist as a traceable batch-processing operation. If metrics are implemented, aggregate counts and rates SHOULD be emitted as metrics rather than relying only on summary spans.

---

## 14. Local Mirrors and Artifacts

### 14.1 Mirror Requirement

A Level 2 Runtime Instrumentation implementation MUST preserve the local telemetry mirror behavior for built-in gh-aw JavaScript span exporters.

The default path MUST be `/tmp/gh-aw/otel.jsonl`.

### 14.2 Mirror Record Format

Each line of `/tmp/gh-aw/otel.jsonl` MUST remain one complete JSON object containing the raw OTLP/HTTP JSON export fragment currently emitted by gh-aw helpers, including `resourceSpans` for trace payloads.

A versioned envelope for traces, metrics, logs, or diagnostics MAY be added only as an additive companion format. It MUST either use a separate file or support both the raw OTLP/JSON line format and the envelope format during a documented migration period.

### 14.3 Write and Artifact Safety

Mirror writes MUST occur before remote export success is assumed.

A remote export failure MUST NOT delete or truncate previously written mirror data.

Mirror-write failure MUST produce a structured diagnostic but SHOULD NOT fail the functional workflow.

The mirror writer MUST create parent directories with restrictive permissions, MUST use append-safe writes, SHOULD tolerate concurrent writers, MUST NOT write exporter credentials, MUST apply the same content-capture and redaction policy as remote export, and SHOULD rotate or bound file size.

When telemetry artifacts are uploaded, the artifact SHOULD contain `otel.jsonl` and no secret headers or credentials. As of the removal in PR #32280, gh-aw does not produce a runtime-specific companion mirror file for Copilot CLI spans; Copilot CLI is expected to export its spans directly to the configured OTLP backend using the injected `OTEL_EXPORTER_OTLP_ENDPOINT`/`OTEL_EXPORTER_OTLP_HEADERS`/`OTEL_RESOURCE_ATTRIBUTES` environment variables (see ADR-34450). A manifest containing schema version, signal counts, byte sizes, and redaction mode MAY be added as an optional companion file.

---

## 15. Security and Privacy

### 15.1 Secret Handling

Exporter headers and credentials MUST be masked before any diagnostic output; MUST NOT appear in telemetry records, artifacts, generated gateway JSON, or job summaries; SHOULD be short-lived and least-privilege; and SHOULD be held by a Collector or trusted exporter helper rather than workflow-global environment variables.

In direct-export mode (`observability.otlp.mode: direct`), implementations MUST treat `OTEL_EXPORTER_OTLP_HEADERS` and equivalent header sources as secret material, MUST redact header values before writing any mirror/artifact/diagnostic record, and MUST NOT copy raw header values or credential-bearing header keys into span attributes, span events, logs, or metric attributes.

### 15.2 Content Defaults and Redaction

Raw prompts, model responses, system instructions, retrieved documents, source code, tool arguments, and tool results MUST NOT be captured by default.

Hashes, lengths, counts, content types, and classification labels MAY be captured when they cannot be used to reconstruct sensitive content.

A Level 3 implementation MUST provide a redaction stage before remote export and artifact upload. Redaction MUST cover known secret formats, authorization headers, bearer tokens, GitHub tokens, private keys, passwords, connection strings, and configured repository-specific patterns.

Direct personal identifiers and other PII MUST NOT be captured by default. When an identifier is operationally necessary and authorized, it MUST be validated and bounded, MUST be redacted before remote export and from mirrors, artifacts, and diagnostics according to the applicable privacy rules, and MUST NOT be a metric dimension by default. See safeguard **OTEL-SG-02** and test **T-OT-013** in §17.1.2.

### 15.3 Attribute Limits

The implementation MUST define and enforce limits for attribute count per record, attribute value length, event count per span, link count per span, log-body size, captured content size, and local mirror size.

Truncation SHOULD be indicated with a boolean or count attribute that does not reveal the removed content.

### 15.4 User, Repository, and Untrusted Values

User IDs, actor names, repository names, and item identifiers MAY be recorded in traces and logs when operationally necessary and authorized. They MUST NOT be metric dimensions by default.

Untrusted input MUST NOT control span names, metric names, metric dimension keys, exporter endpoints without policy validation, exporter headers, resource attribute keys, or log event names.

Untrusted values MAY be recorded only after validation, redaction, and bounded-length enforcement.

---

## 16. Reliability and Failure Handling

### 16.1 Non-Fatal Observability

Once valid configuration has been established, telemetry export failure SHOULD NOT change a successful functional workflow into a failed workflow.

`if-missing: error` applies to missing mandatory setup values, not transient delivery failure.

Exporter, mirror, and bounded-flush failures MUST NOT replace an already-recorded functional result or be reported as successful delivery. See safeguard **OTEL-SG-01** and test **T-OT-012** in §17.1.2.

### 16.2 Retry and Queueing

Transient failures SHOULD use exponential backoff with jitter.

Retry MUST be bounded by maximum attempts, maximum elapsed time, and workflow shutdown deadline.

Permanent failures MUST NOT be retried indefinitely.

Collector mode SHOULD use a sending queue when implemented. Queue overflow SHOULD be observable through Collector or `gh-aw` internal telemetry.

### 16.3 Partial Fan-Out Failure

For N configured endpoints, the exporter MUST attempt each eligible endpoint independently.

Partial success MUST be represented in diagnostics and metrics without discarding successful deliveries.

### 16.4 Sampling

Sampling decisions SHOULD be propagated through W3C trace flags when W3C Trace Context is present.

Child instrumentation MUST honor the parent sampling decision unless an explicitly documented OpenTelemetry sampling policy applies.

Metrics and critical security logs SHOULD remain available even when traces are sampled out.

### 16.5 Shutdown Ordering

Job finalization MUST use this sequence:

1. Finish functional work and record the workflow result.
2. Record result attributes and events, then end built-in child spans.
3. Sanitize and append eligible local mirror records before attempting remote delivery.
4. Flush each eligible endpoint within the bounded retry and shutdown deadlines.
5. Emit bounded exporter diagnostics, including partial fan-out and flush failures.

When an additive pipeline root span is emitted, it SHOULD end only after the workflow result and all known job results are available. An exporter failure MUST NOT replace the functional result.

### 16.6 Fail-Closed Safeguards

The following requirements apply to every runtime span emitter that consumes `GH_AW_OTLP_ENDPOINTS`, including setup, conclusion, outcome, and MCP gateway spans:

| Condition | Required behavior |
|---|---|
| Default endpoint is absent and `if-missing` resolves to `ignore` | Treat OTLP as unconfigured; do not make a network request. |
| Default endpoint has a non-empty URL but its headers secret is empty | The default-credentials check MUST fail the workflow job with a diagnostic identifying the missing headers secret. Every runtime emitter MUST also omit the endpoint before network fan-out when the effective `if-missing` policy is `ignore`. |
| Endpoint configuration is invalid or contains an empty URL | Exclude the entry before export; do not interpret an invalid entry as successful delivery. |
| Payload is ready for export | Sanitize it and append the eligible local mirror record before remote delivery. |
| Multiple endpoints are configured | Attempt each eligible endpoint independently; a failure at one endpoint MUST NOT suppress another endpoint's attempt or erase successful delivery. |
| Export or bounded shutdown flush fails | Record a bounded diagnostic, do not report the failed delivery as successful, preserve prior mirror data, and do not fail already-completed functional work. |
| Headers contain credentials | Keep credentials scoped to trusted exporters and out of mirrors, artifacts, diagnostics, and untrusted processes. |

Endpoint resolution and credential filtering MUST complete before fan-out. Eligible endpoint requests MAY run concurrently; retries remain bounded per endpoint. The ordered export/fan-out/finalization contract is in §6.4 and §16.5.

The CLI outcome evaluator currently emits JSON/JSONL and has no OTLP exporter. OTLP unavailability therefore MUST NOT change its classification or suppress its output; any downstream exporter is responsible for preserving those records on export failure. See the [Safe Output Outcome Evaluation Specification](safe-output-outcome-evaluation.md#otlp-backend-unavailability).

---

### Safeguards

| Safeguard | Normative behavior | Compliance test |
|---|---|---|
| **OTEL-SG-01 — Exporter failure** | After valid setup, exporter, mirror, or bounded-flush failure MUST preserve the functional result, produce a bounded diagnostic, and MUST NOT claim failed delivery succeeded. | [§17.1.2, T-OT-012](#1712-concrete-span-attribute-contract-cases) |
| **OTEL-SG-02 — PII redaction** | PII MUST be excluded by default; authorized identifiers MUST be validated, bounded, and redacted before remote export, persistence, or diagnostics. | [§17.1.2, T-OT-013](#1712-concrete-span-attribute-contract-cases) |

### Sync Notes

Sections 15.2 and 16.1/16.6 define the privacy and delivery safeguards; §17.1.A maps existing implementation tests, while §17.1.2 defines their conformance test IDs. Changes to content capture, redaction, mirrors, exporter retries, fan-out, or shutdown behavior MUST update the corresponding test mapping. Revalidate the shipped compatibility contract with `make validate-otel-contract`.

---

## 17. Compliance Testing

A conforming implementation MUST provide automated tests for every applicable REQUIRED compatibility behavior.

Tests MUST validate semantic output rather than only source-code structure.

OTLP tests SHOULD decode exported payloads and assert resource, scope, span, attribute, status, event, and context fields that are implemented by the claimed conformance level.

### 17.1 Required Compatibility Tests

Level 1 and Level 2 compatibility validation MUST cover the following behaviors:

| Area | Required coverage |
|---|---|
| Frontmatter schema | `observability.otlp.endpoint`, `headers`, `if-missing`, `attributes`, `resource-attributes`, and `github-app` are accepted or rejected according to Section 5. |
| Compiler environment | The compiler emits `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_HEADERS` when configured, `OTEL_SERVICE_NAME`, `OTEL_RESOURCE_ATTRIBUTES`, `GH_AW_OTLP_ENDPOINTS`, `GITHUB_AW_OTEL_TRACE_ID`, `GITHUB_AW_OTEL_PARENT_SPAN_ID`, and `TRACEPARENT` according to Sections 6 and 7. |
| Header secrecy | OTLP headers are masked before diagnostics and are absent from generated gateway JSON, job summaries, telemetry records, and artifacts. |
| Default fallback credentials | A default endpoint without its headers secret fails the credential check, and runtime exporters do not send it unauthenticated when the effective `if-missing` policy is `ignore`. |
| Endpoint fan-out | Multiple endpoints are preserved in declaration order, first endpoint compatibility is retained, and one endpoint failure does not suppress attempts to other endpoints. |
| Local mirror | `/tmp/gh-aw/otel.jsonl` remains raw OTLP/HTTP JSON lines with `resourceSpans`; it is not replaced by an envelope-only format. |
| Built-in spans | Setup, conclusion, and built-in agent spans preserve shipped names and stable `gh-aw.*`, `github.*`, and GenAI attributes. |
| GenAI compatibility | Built-in gh-aw spans continue to emit `gen_ai.system` and `gen_ai.usage.total_tokens` when the underlying values are available. |
| Privacy defaults | Raw prompts, model responses, system instructions, source content, tool arguments, and tool results are not captured by default. |
| Non-fatal export | Telemetry export and mirror failures do not replace the workflow's functional result after valid setup. |

### 17.1.A Required Coverage Map (current tests)

The following table maps each required-coverage row above to concrete tests in the
current repository.

| Required coverage area | Named tests |
|---|---|
| Frontmatter schema | `pkg/parser/schema_test.go`: `TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_OTLPGitHubAppImplicitOIDC`, `TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_OTLPCustomAttributes`, `TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_OTLPResourceAttributes`, `TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_OTLPGitHubAppPermissionsRejected` |
| Compiler environment | `pkg/workflow/otel_observability_formal_test.go`: `TestFormal_EndpointFormNormalization`, `TestFormal_IfMissingPolicyValidation`, `TestFormal_ServiceNameFormation`; `pkg/workflow/observability_otlp_test.go`: `TestInjectOTLPConfig`, `TestInjectOTLPConfig_MultipleEndpoints`, `TestInjectOTLPConfig_OTLPHeadersField`, `TestInjectOTLPConfig_CustomAttributes` |
| Header secrecy | `pkg/workflow/otel_observability_formal_test.go`: `TestFormal_SentryAuthHeaderRewrite`; `pkg/workflow/observability_otlp_mask_script_test.go`: `TestMaskOTLPHeadersScript`, `TestMaskOTLPAttributesScript` |
| Default fallback credentials | `pkg/workflow/observability_otlp_default_credentials_script_test.go`: `TestCheckOTLPDefaultCredentialsScript` (including `endpoint without headers fails`); `actions/setup/js/send_otlp_span.test.cjs`: `it("drops an endpoint with a URL but no headers to avoid unauthenticated export", ...)` |
| Endpoint fan-out | `pkg/workflow/otel_observability_formal_test.go`: `TestFormal_FanOutPreservesDeclarationOrder`; `actions/setup/js/send_otlp_span.test.cjs`: `it("continues to other endpoints when one fails", ...)` |
| Local mirror | `pkg/workflow/otel_observability_formal_test.go`: `TestFormal_MirrorPathConstant`; `actions/setup/js/otel_contract.test.cjs`: `it("preserves customer-facing raw OTLP JSONL and GenAI compatibility attributes", ...)` |
| Built-in spans | `actions/setup/js/otel_contract.test.cjs`: `it("preserves customer-facing raw OTLP JSONL and GenAI compatibility attributes", ...)` |
| GenAI compatibility | `actions/setup/js/otel_contract.test.cjs`: `it("preserves customer-facing raw OTLP JSONL and GenAI compatibility attributes", ...)` |
| Privacy defaults | `pkg/workflow/otel_observability_formal_test.go`: `TestFormal_SecretRefResourceAttributeRejected`; `actions/setup/js/send_otlp_span.test.cjs`: `it("redacts span attribute values whose keys match sensitive patterns", ...)`, `it("redacts sensitive keys in span event attributes", ...)` |
| Non-fatal export | `actions/setup/js/send_otlp_span.test.cjs`: `it("warns (does not throw) when server returns non-2xx status on all retries", ...)`, `it("warns (does not throw) when fetch rejects on all retries", ...)`, `it("does not throw when appendFileSync fails", ...)` |

The repository enforcement entry point for these checks is `make validate-otel-contract`. This target MUST remain focused on the customer-facing compatibility contract rather than all possible OTEL-related tests.

### 17.1.1 Test ID Stubs: Level 1 Compliance

The following test IDs are stubs for Level 1 (Stable Configuration and Export) compliance tests. Implementations MUST provide tests that correspond to each stub before claiming Level 1 conformance.

| Test ID | Area | Description |
|---------|------|-------------|
| **T-OT-001** | Compiler config | Compiler accepts `observability.otlp.endpoint` with a valid HTTPS URL and emits `OTEL_EXPORTER_OTLP_ENDPOINT` in the generated workflow environment. |
| **T-OT-002** | Compiler config | Compiler rejects `observability.otlp.endpoint` set to a non-HTTPS URL when `if-missing: block` is configured, producing a descriptive validation error. |
| **T-OT-003** | Endpoint normalization | When multiple endpoints are declared, the compiler preserves them in declaration order and retains the first endpoint in the `OTEL_EXPORTER_OTLP_ENDPOINT` variable for backward compatibility. |
| **T-OT-004** | OTLP export | The runtime JavaScript helper defaults to valid OTLP/HTTP JSON and supports binary OTLP/HTTP protobuf selected through the standard protocol environment variables. Both POST to the configured traces endpoint and recognize a successful 200 response as accepted. Protobuf conformance is compared with the official OpenTelemetry SDK in CI. |
| **T-OT-005** | Trace context | The compiler injects `GITHUB_AW_OTEL_TRACE_ID`, `GITHUB_AW_OTEL_PARENT_SPAN_ID`, and `TRACEPARENT` into the generated workflow environment when OTLP observability is enabled. |
| **T-OT-006** | Local mirrors | The runtime helper writes each exported span as a raw OTLP/HTTP JSON line with a `resourceSpans` key to `/tmp/gh-aw/otel.jsonl`; the file format MUST NOT be an envelope-only summary. |
| **T-OT-007** | Compiler config | `observability.otlp.headers` entries are emitted as `OTEL_EXPORTER_OTLP_HEADERS` in `key=value,key=value` format and are masked in diagnostics, job summaries, and artifacts. |

### 17.1.2 Concrete Span-Attribute Contract Cases

At minimum, the automated compliance suite SHOULD include the following
attribute-level checks mapped directly to Section 10 requirements:

| Test ID | Attribute requirement | Expected value | Verification method |
|---------|------------------------|----------------|---------------------|
| **T-OT-008** | §10.2 `gh-aw.job.name` on built-in job spans | The setup span includes `gh-aw.job.name` equal to the GitHub Actions job name | Decode `/tmp/gh-aw/otel.jsonl` or captured OTLP payloads and assert the setup span attribute value exactly matches the workflow job name |
| **T-OT-009** | §10.3 `gen_ai.system` on built-in agent spans | A known engine emits a non-empty normalized provider/system value | Run a workflow with a built-in agent span, decode exported spans, and assert `gen_ai.system` is present whenever the engine mapping is known |
| **T-OT-010** | §13.3 `gh-aw.outcome.type` on optional downstream outcome-evaluation spans | Exported outcome spans preserve the safe-output type being evaluated | Feed `.outcomes[].items[]` from `gh aw logs --outcomes --json` to the downstream exporter under test and assert `gh-aw.outcome.type` matches the evaluated manifest item type; not a built-in CLI export |
| **T-OT-011** | §17 Backward-compatibility: v0.3.0 fields present in v0.4.0 export | All span attribute keys that were present in v0.3.0 (`gh-aw.*`, `github.*`, `gen_ai.system`, `gen_ai.usage.total_tokens`, `OTEL_EXPORTER_OTLP_ENDPOINT`, built-in setup/conclusion span names) **MUST** also be present in a v0.4.0 export at the same semantic positions | Produce a v0.4.0 compliant export from the current implementation; compare the set of attribute keys against the v0.3.0 attribute inventory (defined in Version 0.3.0 change-log entry in Section 19); assert no v0.3.0 attribute has been removed or renamed; assert no built-in span name has changed |
| **T-OT-012** | §16.1 Exporter failure preserves functional result | Non-2xx responses, exhausted network retries, mirror-write failures, and bounded flush failures do not replace the completed workflow result or report delivery success | Exercise failures in each exporter and mirror path; assert bounded diagnostics and unchanged functional result |
| **T-OT-013** | §15.2 PII defaults and redaction | PII is absent by default; authorized identifiers are bounded and redacted before remote export and from mirrors, artifacts, and diagnostics and are not metric dimensions by default | Capture representative direct identifiers and configured PII patterns; inspect exported payloads, local mirrors, artifacts, diagnostics, and metric dimensions |


### 17.2 Optional Extension Tests

An implementation claiming Level 3 MUST add automated tests for every Level 3 feature it enables, including any OpenTelemetry-native root spans, full-duration job spans, metrics, structured logs, outcome span links, Collector mode, or versioned mirror companion files.

Level 3 extension tests MUST prove that the extension is additive. They MUST NOT require removal or renaming of Level 1 or Level 2 fields, files, environment variables, span names, or compatibility attributes.

### 17.3 Implementation Map

The following implementation areas are authoritative for version 0.4.0 compatibility:

| Contract area | Implementation and tests |
|---|---|
| Frontmatter schema | `pkg/parser/schemas/main_workflow_schema.json`, `pkg/parser/schema_test.go` |
| Compiler normalization and env injection | `pkg/workflow/observability_otlp.go`, `pkg/workflow/observability_otlp_test.go`, `pkg/workflow/safe_output_helpers_test.go` |
| Gateway credential scoping | `pkg/workflow/mcp_renderer.go`, `pkg/workflow/mcp_setup_generator.go`, `pkg/workflow/mcp_renderer_test.go` |
| MCP access-control fixture contract | `specs/github-mcp-access-control-compliance/README.md` |
| Replace-label fixture contract | `specs/replace-label-compliance/README.md` |
| Runtime setup/conclusion spans and JSONL mirror | `actions/setup/js/send_otlp_span.cjs`, `actions/setup/js/send_otlp_span.test.cjs`, `actions/setup/js/otel_contract.test.cjs` |
| Header and attribute masking | `actions/setup/sh/mask_otlp_headers.sh`, `actions/setup/sh/mask_otlp_attributes.sh`, `pkg/workflow/observability_otlp_mask_script_test.go` |
| Local validation target | `Makefile` target `validate-otel-contract` |

Any change that alters a listed compatibility surface MUST update the corresponding tests in the same change.

Outcome-evaluation span contracts SHOULD stay aligned with the governance and attribution
schema defined in `specs/intent-attribution-agent-governance.md`, especially when intent
context is added to outcome spans or links.

---

## 18. References

### 18.1 Normative References

- **[RFC 2119]** S. Bradner. *Key words for use in RFCs to Indicate Requirement Levels*. March 1997. https://www.ietf.org/rfc/rfc2119.txt
- **[W3C Trace Context Level 2]** W3C Distributed Tracing Working Group. *Trace Context Level 2*. https://www.w3.org/TR/trace-context-2/
- **[OpenTelemetry Specification]** OpenTelemetry Authors. https://opentelemetry.io/docs/specs/otel/
- **[OTLP]** OpenTelemetry Authors. *OpenTelemetry Protocol Specification*. https://opentelemetry.io/docs/specs/otlp/
- **[OpenTelemetry Semantic Conventions]** OpenTelemetry Authors. https://opentelemetry.io/docs/specs/semconv/
- **[CI/CD Spans]** OpenTelemetry Authors. *Semantic conventions for CI/CD spans*. https://opentelemetry.io/docs/specs/semconv/cicd/cicd-spans/
- **[CI/CD Metrics]** OpenTelemetry Authors. *Semantic conventions for CI/CD metrics*. https://opentelemetry.io/docs/specs/semconv/cicd/cicd-metrics/
- **[CI/CD Resources]** OpenTelemetry Authors. *CI/CD resource semantic conventions*. https://opentelemetry.io/docs/specs/semconv/resource/cicd/
- **[GenAI Spans]** OpenTelemetry GenAI SIG. *Semantic conventions for generative AI spans*. https://github.com/open-telemetry/semantic-conventions-genai/blob/main/docs/gen-ai/gen-ai-spans.md
- **[GenAI Agent Spans]** OpenTelemetry GenAI SIG. *Semantic conventions for GenAI agent and framework spans*. https://github.com/open-telemetry/semantic-conventions-genai/blob/main/docs/gen-ai/gen-ai-agent-spans.md
- **[GenAI Metrics]** OpenTelemetry GenAI SIG. *Semantic conventions for generative AI metrics*. https://github.com/open-telemetry/semantic-conventions-genai/blob/main/docs/gen-ai/gen-ai-metrics.md
- **[MCP Semantic Conventions]** OpenTelemetry GenAI SIG. *Semantic conventions for Model Context Protocol*. https://github.com/open-telemetry/semantic-conventions-genai/blob/main/docs/gen-ai/mcp.md

### 18.2 Informative References

- **[Collector Resiliency]** OpenTelemetry Authors. https://opentelemetry.io/docs/collector/resiliency/
- **[Collector Internal Telemetry]** OpenTelemetry Authors. https://opentelemetry.io/docs/collector/internal-telemetry/
- `docs/src/content/docs/reference/open-telemetry.mdx`
- `docs/src/content/docs/reference/frontmatter.md`
- `docs/src/content/docs/reference/mcp-gateway.md`
- `specs/safe-output-outcome-evaluation.md`

---

## 19. Change Log

### Unreleased

- **Added**: A normative requirement (§10.8) that completed BinEval records MUST produce `eval.result` events on `gh-aw.evals.conclusion`; when a not-evaluated status marker is present it takes precedence and answers are not emitted.
- **Added**: Explicit PII and exporter-failure safeguards in §§15–16, with conformance test IDs T-OT-012 and T-OT-013 and revalidation guidance.

### Version 0.6.0 (Working Draft, November 5, 2026)

- **Clarified**: Consolidated fail-closed endpoint, credential, mirror, fan-out, and shutdown safeguards in §16.6 and added a testable five-step finalization order in §16.5.
- **Added**: Cross-reference to the safe-output outcome evaluation contract, clarifying that the CLI emits JSON/JSONL and does not currently export outcome spans.

### Version 0.5.0 (Working Draft, August 27, 2026)

- **Added**: §5.8 Default Fallback — the compiler falls back to `${{ secrets.GH_AW_DEFAULT_OTLP_ENDPOINT || vars.GH_AW_DEFAULT_OTLP_ENDPOINT }}` / `${{ secrets.GH_AW_DEFAULT_OTLP_HEADERS }}` when no `observability.otlp` endpoint is configured in frontmatter or an import, forcing `if-missing: ignore` so an unset default is a silent no-op.
- **Added**: A normative requirement (§5.8, §6.4) that a default endpoint resolved with a URL but empty headers MUST be dropped by every span-emitting runtime path (job-setup, conclusion, outcome, MCP gateway), not only by whichever job performs credential validation, so no job ordering can allow unauthenticated export.
- **Added**: A normative requirement (§5.8) that the compiler MUST NOT emit a duplicate env-block mapping key when the workflow already defines an OTLP-related key (`OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_SERVICE_NAME`, `OTEL_RESOURCE_ATTRIBUTES`, `OTEL_EXPORTER_OTLP_HEADERS`, `GH_AW_OTLP_ALL_HEADERS`, `GH_AW_OTLP_ENDPOINTS`, `GH_AW_OTLP_IF_MISSING`, `GH_AW_OTLP_ATTRIBUTES`).
- **Clarified**: §6.1 table entry for `GH_AW_OTLP_IF_MISSING` now cross-references the default fallback.

### Version 0.4.0 (Working Draft, June 18, 2026)

- **Removed** (documentation correction, August 2026): References to a `copilot-otel.jsonl` local mirror/companion artifact. PR #32280 removed the file-export pipeline that produced it (`COPILOT_OTEL_FILE_EXPORTER_PATH` injection, artifact inclusion, and forwarding script); Copilot CLI spans are now expected to be exported directly to the configured OTLP backend and queried there.
- **Sync follow-up**: Keep the legacy local-mirror artifact name only as this historical note; repo-wide searches for that name SHOULD continue to return this changelog entry as the sole live reference after PR #32280.

- **Changed**: Reframed 0.4.0 as a non-breaking compatibility revision rather than a replacement telemetry standard.
- **Preserved**: `observability.otlp`, direct OTLP export, `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_HEADERS`, `GITHUB_AW_OTEL_TRACE_ID`, `GITHUB_AW_OTEL_PARENT_SPAN_ID`, `TRACEPARENT` compatibility, built-in setup/conclusion spans, `gen_ai.system`, `gen_ai.usage.total_tokens`, and raw OTLP JSONL mirror behavior.
- **Clarified**: Standard OpenTelemetry attributes such as `gen_ai.provider.name` and CI/CD attributes may be emitted as additive aliases, not replacements for existing fields.
- **Clarified**: Pipeline root spans, full-duration job spans, Collector mode, metrics, structured logs, and outcome links are future or Level 3 extensions unless already implemented.
- **Clarified**: A versioned mirror envelope may be added only as an additive format; `/tmp/gh-aw/otel.jsonl` remains raw OTLP/JSON lines for compatibility.
- **Added**: Metric cardinality, privacy, redaction, and secret-handling guidance while preserving existing artifacts and query surfaces.
- **Added**: Inlined compatibility validation requirements, optional extension tests, and the implementation map so this document is self-contained.
- **Added**: Consolidated fail-closed safeguards for export, endpoint fan-out, and shutdown handling.
- **See also**: `specs/safe-output-outcome-evaluation.md` (Change Log, Version 1.0.1) for aligned outcome-taxonomy and provenance rules.

### Version 0.3.0 (Working Draft, June 15, 2026)

- Consolidated OTLP behavior and `cicd.automation.*` conventions into one repository specification.
- Added MCP gateway and API proxy spans.
- Added initial trace, attribute, resource, propagation, outcome, and conformance sections.

### Version 0.2.0 (Working Draft)

- Added the initial trace model, span attributes, outcome spans, resource attributes, and trace-ID propagation.

### Version 0.1.0 (Working Draft)

- Defined the initial `observability.otlp` compiler and runtime contract.

---

## License

Copyright (c) 2026 GitHub, Inc.
This specification is provided under the MIT License.
