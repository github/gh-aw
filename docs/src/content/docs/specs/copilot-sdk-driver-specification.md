---
title: Copilot SDK Driver Specification
description: Formal W3C-style specification for Copilot SDK driver configuration, environment variables, permission checks, and logging requirements
sidebar:
  order: 1365
---

# Copilot SDK Driver Specification

**Version**: 1.0.2  
**Status**: Draft Specification  
**Latest Version**: [copilot-sdk-driver-specification](/gh-aw/specs/copilot-sdk-driver-specification/)  
**Editor**: GitHub Agentic Workflows Team

---

## Abstract

This specification defines the normative behavior of a Copilot SDK driver that runs an agent session against a Copilot SDK endpoint and emits session telemetry. The specification is language agnostic and focuses on environment variable contracts, permission-checking policy, required logging behavior, and connection-token propagation between the harness-managed sidecar and the driver. Non-normative examples use TypeScript. Conforming implementations provide deterministic permission enforcement, auditable diagnostics, and interoperable runtime behavior across host environments.

## Status of This Document

This section describes the status of this document at the time of publication. This is a draft specification and may be updated, replaced, or made obsolete by other documents at any time.

This document is governed by the GitHub Agentic Workflows project specifications process.

## Table of Contents

1. [Introduction](#1-introduction)
2. [Conformance](#2-conformance)
3. [Driver Execution Model](#3-driver-execution-model)
4. [Configuration and Environment Variables](#4-configuration-and-environment-variables)
5. [Permission Checking Model](#5-permission-checking-model)
6. [Logging Requirements](#6-logging-requirements)
7. [Error Handling and Exit Behavior](#7-error-handling-and-exit-behavior)
8. [Compliance Testing](#8-compliance-testing)
9. [Appendices](#9-appendices)
10. [References](#10-references)
11. [Sync Notes](#sync-notes)
12. [Change Log](#change-log)

---

## 1. Introduction

### 1.1 Purpose

The Copilot SDK driver provides a host-independent contract for starting a Copilot SDK client session, sending a prompt, handling tool permissions, and producing structured operational logs.

### 1.2 Scope

This specification covers:

- Driver configuration inputs and runtime environment variables
- Permission request evaluation and deny/allow behavior
- Required lifecycle and policy-denial logging
- Required error handling and process exit semantics

This specification does NOT cover:

- Copilot model quality or response content guarantees
- Host workflow compiler internals
- SDK transport protocol internals beyond driver-facing inputs

### 1.3 Design Goals

A conforming implementation MUST:

- Remain language agnostic in externally visible behavior
- Provide explicit and testable permission decisions
- Produce consistent, audit-friendly logs for runtime and policy events
- Fail fast on missing required configuration in standalone mode

---

## 2. Conformance

### 2.1 Conformance Classes

A **conforming driver implementation** satisfies all MUST, REQUIRED, and SHALL requirements in this specification.

A **partially conforming driver implementation** satisfies all MUST requirements in Sections 4, 5, and 7 but MAY omit optional diagnostics in Section 6.

### 2.2 Requirements Notation

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be interpreted as described in [RFC 2119](https://www.ietf.org/rfc/rfc2119.txt).

### 2.3 Compliance Levels

Implementations MUST support:

- **Level 1 (Required)**: Required environment variables, session startup, prompt dispatch, and exit behavior
- **Level 2 (Standard)**: Permission checking model and denial diagnostics
- **Level 3 (Complete)**: Full lifecycle logging and structured event serialization interoperability

---

## 3. Driver Execution Model

### 3.1 Runtime Mode

For extension integrations, a driver implementation MUST support **standalone mode** (executable entry point that reads configuration from environment variables). Embedded callable APIs are out of scope for this specification.

### 3.2 Session Lifecycle

A conforming implementation MUST execute the following sequence:

1. Resolve runtime configuration.
2. Start SDK client connection.
3. Create a session.
4. Register event handlers.
5. Send prompt and await completion.
6. Return success/failure result.
7. Perform best-effort cleanup of stream/session/client resources.

### 3.3 Event Persistence

A complete implementation (Level 3) SHOULD serialize non-ephemeral session events to a JSON Lines stream compatible with downstream timeline rendering.

The built-in driver retains native event envelopes and payloads, including user
messages, reasoning, errors, tool outcomes, and unknown non-ephemeral extensions.
It also captures the ephemeral `assistant.usage`, `assistant.message_delta`,
`assistant.reasoning_delta`, `model.call_failure`, and workflow lifecycle observations needed to
recover accounting and interrupted conversations. Other ephemeral events are
not persisted. Canonical conversion happens in the Copilot session adapter;
missing timestamps, tool outcomes, and metrics are not synthesized by the driver.
Default summaries continue to omit user prompts.

Bootstrap and collection retain independently parseable native observations
from malformed JSONL and record an explicit collection warning. A usable partial
native trace is not replayed together with fallback stdio. When fallback is
needed, exact identified copies are deduplicated within their observed session;
conflicting payloads and independent retry or child observations remain available.

### 3.4 Harness Connection Token Flow

When SDK mode is enabled (`COPILOT_SDK_URI` is set), the harness MUST generate a per-run `COPILOT_CONNECTION_TOKEN` and MUST pass the same token value to both:

1. The harness-managed Copilot sidecar process
2. The SDK driver subprocess environment

The SDK driver MUST treat `COPILOT_CONNECTION_TOKEN` as a required input and MUST fail fast with non-zero exit when it is missing.

The harness MUST NOT propagate platform authentication secrets such as `GITHUB_TOKEN`, `COPILOT_GITHUB_TOKEN`, or `GH_TOKEN` into the SDK driver subprocess environment. Driver processes run in a secret-isolated environment and MUST NOT rely on or attempt to read platform authentication tokens.

---

## 4. Configuration and Environment Variables

### 4.1 Standalone Environment Variables

In standalone mode, the implementation MUST enforce the following contract:

| Variable                      | Required | Description                                               | Default / Validation                                                                                         |
| ----------------------------- | -------- | --------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------ |
| `GH_AW_PROMPT`                | Yes      | Path to prompt file                                       | MUST exist and be readable                                                                                   |
| `COPILOT_SDK_URI`             | Yes      | SDK endpoint URI                                          | MUST be non-empty                                                                                            |
| `COPILOT_CONNECTION_TOKEN`    | Yes      | Per-run shared token generated by the harness in SDK mode | MUST be non-empty in the driver environment                                                                  |
| `COPILOT_MODEL`               | Yes      | Model to use (e.g. `gpt-4o`, `claude-sonnet-4`)          | MUST be non-empty; implementations MUST fail fast when unset                                                 |
| `COPILOT_SDK_SEND_TIMEOUT_MS` | No       | Send timeout in milliseconds                              | Input SHOULD be a positive integer; gh-aw typically sets this from workflow `timeout-minutes`; default `600000`; implementations MUST fall back on invalid values |
| `GH_AW_MAX_TOOL_DENIALS`      | No       | Maximum repeated tool denials before aborting inference | Input SHOULD be a positive integer; default `5`; implementations MUST fall back on invalid values |
| `COPILOT_SDK_LOG_LEVEL`       | No       | SDK client log level                                      | gh-aw may set this for driver runtime logging; valid values: `none`, `error`, `warning`, `info`, `debug`, `all`; invalid values MUST fall back to `warning` |
| `GITHUB_WORKSPACE`            | No       | Working directory hint                                    | SHOULD be used when present                                                                                  |

> **Note**: Platform authentication tokens (`GITHUB_TOKEN`, `COPILOT_GITHUB_TOKEN`, `GH_TOKEN`) are NOT available in the SDK driver subprocess environment. Driver implementations MUST NOT reference or depend on these variables.

### 4.2 Connection Token Requirement and Token Isolation Policy

`COPILOT_CONNECTION_TOKEN` is a harness-generated per-run secret used by the SDK driver to authenticate to the harness-managed sidecar session.

In SDK mode, a conforming implementation:

- MUST generate a token value with sufficient entropy for local authentication.
- MUST propagate the same token to sidecar and driver processes for a given run.
- MUST require the token in the driver process environment before creating `RuntimeConnection`.
- MUST NOT log the raw token value.

The SDK driver subprocess runs in a secret-isolated environment. As a result:

- `COPILOT_CONNECTION_TOKEN` is the **only** authentication token a driver MUST use.
- A driver SHOULD NOT attempt to read `GITHUB_TOKEN`, `COPILOT_GITHUB_TOKEN`, or `GH_TOKEN` from its environment.
- A driver MUST NOT use a GitHub platform token as a substitute or supplement for `COPILOT_CONNECTION_TOKEN`.
- If a driver encounters an absent `GITHUB_TOKEN` or `COPILOT_GITHUB_TOKEN`, it MUST NOT treat this as an error condition. The absence of these variables is expected and normal.

### 4.3 Timeout Environment Variable

`COPILOT_SDK_SEND_TIMEOUT_MS` controls maximum send wait duration for prompt completion in standalone mode.

When gh-aw hosts the driver, it generally derives and injects this variable from workflow `timeout-minutes` (exposed to the harness as `GH_AW_TIMEOUT_MINUTES`) so SDK completion stays below the step timeout budget.

A conforming driver implementation MUST treat `COPILOT_SDK_SEND_TIMEOUT_MS` as milliseconds and MUST apply the default value (`600000`) when unset, non-numeric, or non-positive.

### 4.4 Log-Level Environment Variable

`COPILOT_SDK_LOG_LEVEL` is the host-provided SDK client log level control. A conforming driver implementation MUST use the provided value when it is one of `none`, `error`, `warning`, `info`, `debug`, or `all`; otherwise it MUST fall back to `warning`.

### 4.5 Tool-Denials Guardrail Environment Variable

`GH_AW_MAX_TOOL_DENIALS` controls the catastrophic tool-denials
guardrail in SDK mode. A conforming driver implementation MUST count
repeated tool refusals (permission denials), and MUST stop
inference once the configured threshold is reached. When unset,
non-numeric, or non-positive, implementations MUST apply the default
value (`5`).

This counting requirement tightens the specification to match the existing gh-aw reference implementation in `actions/setup/js/copilot_sdk_session.cjs` and the current `copilot_sdk_driver.test.cjs` guardrail coverage. It does not change gh-aw runtime behavior, but implementations that previously treated denial counting as optional would need to align with this clarified conformance requirement.

### 4.6 TypeScript Example (Non-Normative)

Prerequisite: install [`@github/copilot-sdk`](https://www.npmjs.com/package/@github/copilot-sdk) in the runtime where this example executes.

```ts
import { readFile } from "node:fs/promises";
import { CopilotClient, RuntimeConnection } from "@github/copilot-sdk";

const promptPath = process.env.GH_AW_PROMPT;
const sdkUri = process.env.COPILOT_SDK_URI;
const connectionToken = process.env.COPILOT_CONNECTION_TOKEN;
const model = process.env.COPILOT_MODEL;
if (!promptPath || !sdkUri || !connectionToken || !model) {
  throw new Error("Missing required standalone environment variables.");
}

const rawTimeoutValue = process.env.COPILOT_SDK_SEND_TIMEOUT_MS;
const sendTimeoutMs = rawTimeoutValue && /^[1-9]\d*$/.test(rawTimeoutValue) ? Number(rawTimeoutValue) : 600000;
const allowedLogLevels = new Set(["none", "error", "warning", "info", "debug", "all"]);
const rawLogLevel = process.env.COPILOT_SDK_LOG_LEVEL || "warning";
const logLevel = allowedLogLevels.has(rawLogLevel) ? rawLogLevel : "warning";
const workingDirectory = process.env.GITHUB_WORKSPACE || process.cwd();
const prompt = await readFile(promptPath, "utf8");

const client = new CopilotClient({
  connection: RuntimeConnection.forUri(sdkUri, {
    connectionToken,
  }),
  workingDirectory,
  logLevel,
});

await client.start();
try {
  const session = await client.createSession({ model });
  const response = await session.sendAndWait({ prompt }, { timeoutMs: sendTimeoutMs });
  console.log(response);
  await session.disconnect();
} finally {
  await client.stop();
}
```

---

## 5. Permission Checking Model

### 5.1 Permission Configuration

The effective permission configuration supports:

- `allowAllTools` (boolean)
- `allowedTools` (string array)

In scoped allowlist mode, `read` is treated as a configured permission and MUST be denied unless explicitly allowed.

### 5.2 Handler Resolution

The driver MUST always configure an `onPermissionRequest` handler when creating
an SDK session. The handler MUST consume the effective permission configuration
input and resolve as follows:

1. If `allowAllTools` is `true`, the driver MUST approve all permission requests.
2. If effective permission configuration is absent, the driver MUST treat the
   session as unrestricted and approve all permission requests.
3. If `allowedTools` is empty after normalization, the driver MUST treat the
   session as unrestricted and approve all permission requests.
4. Otherwise, the driver MUST enforce the scoped allow rules below.

### 5.3 Scoped Allow Rules

When scoped rules are active, the implementation MUST evaluate requests as follows:

- `read`: MUST be denied unless `allowedTools` contains `read`.
- `write`: MUST be approved only when `allowedTools` contains `write`.
- `url`: MUST be approved only when `allowedTools` contains `web_fetch`.
- `custom-tool`: MUST be approved only when `allowedTools` contains the request tool name.
- `mcp`: MUST be approved when either:
  - `allowedTools` contains `<serverName>`, or
  - `allowedTools` contains `<serverName>(<toolName>)`.
- `shell`: MUST be approved when at least one condition is true:
  - `allowedTools` contains `shell`.
  - A `shell(<rule>)` entry matches the request command identifier.
  - A `shell(<full command text>)` entry exactly matches full command text.
- Unknown kinds MUST be rejected.

### 5.4 Shell Rule Semantics

For `shell(<rule>)` entries:

- Rules ending with `:*` MUST perform prefix matching against command identifiers.
- Rules without spaces SHOULD be treated as identifier matches.
- Rules containing spaces MUST be treated as exact full-command matches.

### 5.5 Rejection Contract

On rejection, the handler MUST return a reject decision with feedback indicating that invocation is not allowed by workflow tool permissions.

### 5.6 TypeScript Example (Non-Normative)

```ts
type PermissionConfig = {
  allowAllTools?: boolean;
  allowedTools?: string[];
};

export function canUseWriteTool(config: PermissionConfig): boolean {
  if (config.allowAllTools) return true;
  return (config.allowedTools ?? []).includes("write");
}
```

---

## 6. Logging Requirements

### 6.1 Log Channels

A conforming implementation MUST support:

- Driver logger output for runtime lifecycle events.
- Permission-denial diagnostics for policy rejections.

### 6.2 Required Lifecycle Logs

The driver logger MUST emit log entries for:

1. Connection attempt start
2. Client start confirmation
3. Session creation with session identifier
4. Prompt dispatch start
5. Completion summary (including output presence and duration)
6. Runtime error summary on failure

### 6.3 Permission-Denial Logs

When a permission request is denied, the implementation MUST:

1. Log a denial entry with a compact request summary.
2. Emit denial diagnostics to optional secondary loggers when configured.

### 6.4 Standalone Error Logs

In standalone mode, missing required environment variables or unreadable prompt input MUST be reported to standard error with a driver-specific prefix.

### 6.5 Structured Event Serialization Schema

All structured log entries emitted by a conforming implementation MUST conform to the following field schema. Implementations MUST include all required fields and MAY include additional fields.

#### 6.5.1 Lifecycle Event Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `event` | string | MUST | Event type identifier (e.g., `"connection.start"`, `"session.created"`, `"prompt.dispatched"`, `"session.completed"`, `"error"`) |
| `timestamp` | string (ISO 8601) | MUST | UTC timestamp of the event |
| `sessionId` | string | MUST when available | Session identifier assigned at session creation; MUST be omitted before session is established |
| `durationMs` | number | MUST for completion events | Elapsed time in milliseconds from prompt dispatch to completion |
| `hasOutput` | boolean | MUST for completion events | Indicates whether the session produced a non-empty output |
| `level` | string | MUST | Log severity level: one of `"info"`, `"warning"`, `"error"` |
| `message` | string | MUST | Human-readable description of the event |

#### 6.5.2 Policy-Denial Log Entry Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `event` | string | MUST | MUST be `"permission.denied"` |
| `timestamp` | string (ISO 8601) | MUST | UTC timestamp of the denial |
| `sessionId` | string | MUST | Session identifier in which the denial occurred |
| `requestKind` | string | MUST | Permission request kind: one of `"read"`, `"write"`, `"url"`, `"shell"`, `"mcp"`, `"custom-tool"` |
| `requestSummary` | string | MUST | Compact human-readable summary of the denied request (MUST NOT include secret values or raw token content) |
| `level` | string | MUST | MUST be `"warning"` or `"error"` |

---

## 7. Error Handling and Exit Behavior

### 7.1 Standalone Validation Failures

In standalone mode, the implementation MUST exit with a non-zero status when:

- Any required environment variable is missing
- Prompt file read fails
- Unhandled runtime exception occurs

### 7.2 Session Result Mapping

The session result object SHOULD include:

- Exit code
- Output text
- Output-presence indicator
- Duration in milliseconds

### 7.3 Cleanup

The implementation MUST perform best-effort cleanup of event streams, session handles, and client handles regardless of success or failure.

---

## 8. Compliance Testing

### 8.1 Test Suite Requirements

Implementations MUST provide automated tests for all Level 1 and Level 2 requirements.

#### 8.1.1 Configuration Tests

- **T-CSD-001**: Missing `GH_AW_PROMPT` fails with non-zero exit.
- **T-CSD-002**: Missing `COPILOT_SDK_URI` fails with non-zero exit.
- **T-CSD-003**: Missing `COPILOT_CONNECTION_TOKEN` fails with non-zero exit.
- **T-CSD-004**: Invalid log level falls back to `warning`.
- **T-CSD-005**: Unset `COPILOT_SDK_SEND_TIMEOUT_MS` falls back to default `600000`.
- **T-CSD-006**: Non-numeric or non-positive `COPILOT_SDK_SEND_TIMEOUT_MS` falls back to default `600000`.
- **T-CSD-007**: In SDK mode, harness and driver receive the same non-empty `COPILOT_CONNECTION_TOKEN`, and token values are not logged.
- **T-CSD-008**: Driver does not read, require, or error on absence of `GITHUB_TOKEN`, `COPILOT_GITHUB_TOKEN`, or `GH_TOKEN`.

#### 8.1.2 Permission Tests

- **T-CSD-101**: Absent `permissionConfig` defers to SDK default policy.
- **T-CSD-102**: `allowAllTools=true` approves all requests.
- **T-CSD-103**: Scoped allowlist denies `read` by default when `allowedTools` does not include `read`.
- **T-CSD-104**: Scoped allowlist approves `read` when `allowedTools` includes `read`.
- **T-CSD-105**: Scoped allowlist approves `write` only when `allowedTools` includes `write`.
- **T-CSD-106**: Scoped allowlist approves `url` only when `allowedTools` includes `web_fetch`.
- **T-CSD-107**: Scoped allowlist approves `custom-tool` only when `allowedTools` includes the requested tool name.
- **T-CSD-108**: Scoped allowlist approves `mcp` by server entry (`<serverName>`) and by server-tool entry (`<serverName>(<toolName>)`).
- **T-CSD-109**: Scoped allowlist enforces shell matching for `shell`, `shell(<rule>)`, and exact full-command entries.
- **T-CSD-110**: Unknown request kinds are rejected.
- **T-CSD-111**: Rejected requests include policy feedback and denial logs.

#### 8.1.3 Logging Tests

- **T-CSD-201**: Lifecycle logs include connection, session, prompt, completion, and failure markers.
- **T-CSD-202**: Permission denial logs include compact request summary.

### 8.2 Compliance Checklist

| Requirement                                   | Test ID              | Level | Status      |
| --------------------------------------------- | -------------------- | ----- | ----------- |
| Required standalone variables enforced        | T-CSD-001..003       | 1     | Required    |
| Connection token generation and propagation   | T-CSD-007            | 1     | Required    |
| Token isolation (no GitHub platform tokens)   | T-CSD-008            | 1     | Required    |
| Log-level and timeout fallback behavior       | T-CSD-004..006       | 1     | Required    |
| Default permission delegation                 | T-CSD-101            | 2     | Required    |
| Allow-all permission behavior                 | T-CSD-102            | 2     | Required    |
| Scoped `read` default-deny and explicit allow | T-CSD-103..104       | 2     | Required    |
| Scoped write/url/custom-tool enforcement      | T-CSD-105..107       | 2     | Required    |
| Scoped MCP/shell enforcement                  | T-CSD-108..109       | 2     | Required    |
| Unknown-kind rejection                        | T-CSD-110            | 2     | Required    |
| Permission denial diagnostics                 | T-CSD-111, T-CSD-202 | 2     | Required    |
| Lifecycle logging coverage                    | T-CSD-201            | 3     | Recommended |

---

## 9. Appendices

### Appendix A: Permission Rule Examples

- `shell` authorizes all shell requests.
- `shell(git:*)` authorizes shell commands whose identifier begins with `git`.
- `github(get_file_contents)` authorizes only one MCP tool on one MCP server.
- `github` authorizes all tools on the `github` MCP server.
- `web_fetch` authorizes URL requests.
- `write` authorizes file write requests.

### Appendix B: Error Conditions

| Condition                            | Required Behavior                           |
| ------------------------------------ | ------------------------------------------- |
| Missing required standalone variable | Log error and exit non-zero                 |
| Prompt file unreadable               | Log error and exit non-zero                 |
| Permission denied                    | Reject request and log denial summary       |
| Session runtime error                | Return failure result and log error summary |

### Appendix C: Security Considerations

#### Glossary

**Non-ephemeral events**: Events that persist beyond the lifetime of a single session or request and are written to durable storage (for example, audit logs, telemetry sinks, or artifact uploads). Ephemeral events, by contrast, exist only in process memory or transient I/O buffers and are discarded when the driver process exits.

#### Requirements

A conforming implementation SHOULD:

- Treat connection tokens as secrets and avoid logging raw token values.
- Apply least-privilege permission rules and avoid broad allow-all configurations unless operationally justified.
- Preserve auditable denial logs for policy and incident review.
- Restrict event persistence to non-ephemeral events and the explicit accounting,
  conversation-delta, and workflow lifecycle exceptions in Section 3.3; avoid
  writing other sensitive transient state to durable storage.
- Not attempt to read, fall back to, or check for platform authentication tokens (`GITHUB_TOKEN`, `COPILOT_GITHUB_TOKEN`, `GH_TOKEN`). These tokens are not present in the driver subprocess environment by design. Attempting to use them would fail silently or cause unexpected behavior.

---

## 10. References

### Normative References

- **[RFC 2119]** Key words for use in RFCs to Indicate Requirement Levels. https://www.ietf.org/rfc/rfc2119.txt

### Informative References

- **[Copilot SDK (npm)]** https://www.npmjs.com/package/@github/copilot-sdk
- **[Copilot SDK Repository]** https://github.com/github/copilot-sdk
- **[Copilot SDK Driver Source]** `actions/setup/js/copilot_sdk_driver.cjs`
- **[Copilot Harness Source]** `actions/setup/js/copilot_harness.cjs`
- **[Environment Variables Reference]** [Environment Variables](/gh-aw/reference/environment-variables/)

---

<a id="sync-notes"></a>
## Sync Notes

The canonical gh-aw implementation for this specification is centered in:

- `actions/setup/js/copilot_sdk_driver.cjs`
- `actions/setup/js/copilot_sdk_session.cjs`
- `actions/setup/js/copilot_harness.cjs`
- `actions/setup/js/copilot_sdk_driver.test.cjs`

This specification MUST be revalidated whenever any of the following occurs:

1. The standalone environment-variable contract changes.
2. The permission-request handling or tool-denial guardrail semantics change.
3. Harness token propagation or secret-isolation behavior changes.

---

<a id="change-log"></a>
## Change Log

### Version 1.0.2 (Draft Specification)

- Added token isolation policy to Section 3.4: harness MUST NOT propagate `GITHUB_TOKEN`, `COPILOT_GITHUB_TOKEN`, or `GH_TOKEN` to driver subprocesses.
- Expanded Section 4.2 to "Connection Token Requirement and Token Isolation Policy": drivers SHOULD NOT read platform authentication tokens; `COPILOT_CONNECTION_TOKEN` is the sole authentication token for driver use; absence of platform tokens MUST NOT be treated as an error.
- Added normative note to Section 4.1 table confirming platform authentication tokens are not available in the driver environment.
- Added compliance test T-CSD-008 for token isolation verification.
- Updated Appendix C (Security Considerations) with token isolation guidance.

### Version 1.0.1 (Draft Specification)

- Added normative connection-token flow requirements based on harness SDK mode behavior.
- Clarified that `COPILOT_CONNECTION_TOKEN` is harness-generated and required in the driver environment.
- Added compliance test coverage for token propagation and non-disclosure in logs.

### Version 1.0.0 (Draft Specification)

- Added initial formal specification for Copilot SDK driver behavior.
- Defined language-agnostic configuration and environment variable contract.
- Formalized permission checking semantics and deny behavior.
- Formalized required runtime and policy logging requirements.
