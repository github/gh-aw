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

An opt-in repository profile MUST initialize its owned repository context before
creating the model session and verify its concrete native tool catalog before
step 5. A failed initialization MUST NOT send a model prompt.

### 3.3 Event Persistence

A complete implementation (Level 3) SHOULD serialize non-ephemeral session events to a JSON Lines stream compatible with downstream timeline rendering.

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
| `GH_AW_COPILOT_SDK_TOOL_CONFIG` | Yes | Compiler-owned tool and permission JSON | Version 1 for ordinary SDK workflows; version 2 for `go-repository`. Missing or inconsistent contracts MUST fail before session creation. |
| `GH_AW_MCP_CONFIG` | With MCP capability | Converted gateway MCP configuration | MUST name a regular file of at most 1 MiB. SDK execution stages it at `${RUNNER_TEMP}/gh-aw/mcp-config/copilot-sdk.json` before entering AWF. |
| `GITHUB_REPOSITORY`, `GITHUB_SHA` | With `go-repository` | Trusted checkout identity and publication baseline | The root checkout MUST match the current repository and starting SHA; `GITHUB_WORKSPACE` is also required for this profile. |

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

1. Standalone compiled workflows MUST obtain a nonempty permission allowlist
   from the validated compiler-owned tool contract.
2. Missing, malformed, or contradictory standalone contracts MUST fail closed;
   they MUST NOT fall back to unrestricted tools.
3. The driver MUST enforce the scoped allow rules below.

The reusable permission helper retains its legacy explicit allow-all and
missing/empty-configuration behavior for embedded callers. That API compatibility
does not authorize a standalone workflow to omit its compiler contract.

### 5.3 Scoped Allow Rules

When scoped rules are active, the implementation MUST evaluate requests as follows:

- `read`: MUST be denied unless `allowedTools` contains `read`.
- `write`: MUST be approved only when `allowedTools` contains `write`.
- `url`: MUST be approved only when `allowedTools` contains `web_fetch`.
- `custom-tool`: MUST be approved only when `allowedTools` contains the request tool name.
- `mcp`: MUST be approved when either:
  - `allowedTools` contains `<serverName>`, or
  - `allowedTools` contains `<serverName>(<rawMCPToolName>)`.
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

### 5.7 Go Repository Profile

`tools.profile: go` is an opt-in version 2 contract for the
bundled Copilot SDK driver inside AWF. It MUST preserve explicit `bash: false`
and `cli-proxy: false`, keep editing enabled, and register the exact custom
permission `go_repository`. Version 1 defaults remain unchanged when the
profile is omitted, except that `write_bash` MUST NOT be exposed through editing
when Bash is disabled.

Profile SDK dependencies are installed under
`${RUNNER_TEMP}/gh-aw/copilot-sdk`, outside the reviewed checkout and inside the
existing read-only AWF mount. The driver uses that installation for `NODE_PATH`
rather than installing dependencies into, or loading them from, the worktree.

The additional `profile` object contains `id: "go-repository"`, a trusted
`repositoryDefaultBranch`, and the canonical non-secret create-PR `policy`.
Repository-default metadata MUST NOT replace an explicit PR base or manufacture
a base when omitted. Runtime expression bindings are resolved after JSON
parsing, using only supported repository/branch metadata and `GH_AW_INPUT_*`
variables. Missing bindings, secret references, and unsupported policy fields
MUST fail before inference.

Native MCP configuration MUST be bound explicitly to the SDK session. Only
gateway HTTP/SSE definitions are accepted; subprocess servers and credential
logging are forbidden. The dedicated safe-outputs bundle MUST include all local
module dependencies; startup coverage must load that isolated bundle rather than
rely on files available only in the full source tree.
Compatibility coverage MUST also traverse the actual MCP gateway. A direct
backend connection does not cover gateway discovery, protocol negotiation, or
authentication. Stateful gateways must negotiate a legacy `initialize` handshake;
enabling sessionless transport is not a substitute for compatible negotiation.
The native SDK repository integration has separate direct and gateway modes.
Set `GH_AW_TEST_MCP_GATEWAY_BINARY` to an absolute gateway executable path to
enable the latter. It verifies authenticated routes, the native catalog,
Go/Git operations, safe-output recording, and zero model-provider requests.
Failure coverage MUST invoke `session.rpc.tools.execute`, not only direct
custom-tool handlers. Both modes exercise a passing Go test that writes an
ignored receipt into the projection, a nonzero Go test command with distinct
stdout/stderr diagnostics, blocked commits after both failures, and the repaired
validation/commit/publication sequence. These checks MUST NOT send a model
prompt. Per-operation timings diagnose cold-cache failures without extending
the fixture or operation deadlines.
The promptless native API emits `external_tool.requested` and
`external_tool.completed`, correlated by request ID; the completion notification
does not contain an outcome. Failure status and diagnostic content are asserted
from the actual `tools.execute` result. Model-driven `tool.execution_complete`
error persistence is covered separately by driver event tests, not fabricated
from promptless completion notifications.
Both modes own their Go build and module caches under the fixture scratch
directory; they must also pass when the host's configured caches do not exist.
The initial GOROOT probe uses the installed local toolchain from the isolated
fixture home, not the enclosing compiler checkout's toolchain selection.
An absent binary explicitly skips that integration locally; an invalid or
failing configured binary MUST fail without falling back to the direct mode.
Catalog initialization MAY temporarily select MCP tools
for metadata discovery, but MUST replace that selection with concrete,
source-qualified names before inference. The driver MUST verify the resulting
catalog and fence late initialization continuations after timeout or cancellation.
It MUST NOT expose general shell aliases, generic task/subagent tools, or a
model-facing MCP wildcard. Deferred tool search is disabled; at most 128
approved tools are preloaded.

MCP permission requests in this profile MUST join the SDK's canonical wire name
to the verified catalog's `mcpServerName` and `mcpToolName`. For example, the
permission request `github-get_file_contents` is checked against the grant
`github(get_file_contents)`. Prefix stripping or guessed aliases MUST NOT
authorize another raw tool. Native inspection uses `view`, `grep`, and `glob`;
`rg` is an SDK selector, not the callable search name.

| `go_repository` action | Effect |
| --- | --- |
| `status`, `diff` | Inspect the checkout and cumulative review changes. |
| `prepare_branch` | Create one new allowed branch without overwriting an existing branch. Only this action accepts `branch`. |
| `format` | Format changed, publication-eligible Go files. |
| `readiness` | Compile the projected repository's tests without selecting tests. |
| `validate` | Check formatting and run `go test -count=1 ./...`, `go vet ./...`, and `go build` against the exact projected publication tree. |
| `commit` | Record the validated candidate locally with a fixed message/identity; retrying pending index synchronization does not create another commit. |

Formatting validation covers the whole projected repository. If an unchanged
baseline file is unformatted, the review is blocked until that prerequisite is
fixed through an authorized change; `format` MUST NOT expand the publication
scope to repair unrelated or excluded files.

These operations MUST NOT accept model-supplied executables, arguments, working
directories, or environment overrides. Execution remains inside AWF, not a
runner-host MCP script. Go downloads, automatic toolchain switching, repository
hooks/filters, and remote Git operations are disabled. Child environments
exclude SDK/provider credentials and Actions command-file variables. Commands
have bounded time, output, cancellation, and owned-process cleanup; build and
validation copies use owned temporary storage outside the checkout.

Ordinary errors from an owned `go_repository` operation MUST return a first-class
SDK `ToolResultObject`, not a thrown exception that the native pipeline can reduce
to a generic failure. The failure object has this shape:

```ts
{ resultType: "failure", textResultForLlm: diagnostic, error: diagnostic }
```

`error` is a string, not an object. The envelope MUST be explicitly typed against
the SDK contract. JSON-stringifying that envelope, returning `{ success: false }`,
or using MCP `isError` is not equivalent. Successful operations retain their
existing JSON-string response shape. Inactive-runtime, malformed-input, and
concurrent-call preflight guards remain rejections.

Diagnostics MUST NOT determine outcome, cancellation, timeout, or permission
status by inspecting message text. Nonzero exits remain failures even if their
output says `PASS`; words such as `denied` or `timeout` do not create a permission
denial or timeout. Command failures preserve the command/exit status and labeled
excerpts of both nonempty stdout and stderr.

The final **serialized SDK failure envelope** MUST fit within **8192 UTF-8 bytes**,
including JSON escape expansion, both copies of the diagnostic, and truncation
markers. Mutation summaries display at most **20 paths**, with at most **256
rendered UTF-8 bytes per path**, including any path truncation marker. Truncation
MUST be explicit and reserve space for both nonempty process streams.
Redaction applies to complete, already-bounded inputs before excerpt selection,
including masks declared after their first use, in the other stream, or outside
the displayed excerpt. The implementation reuses the built-in credential
patterns and Actions `add-mask` helpers, masks Authorization values, URL userinfo
and supported credential query fields, and accepts additional exact-match
authentication masks only from values the caller already holds. It MUST NOT
discover platform secrets or read credential files. This is not a guarantee of
arbitrary-secret detection.
Controls, terminal escape sequences, bidi/invisible formatting controls, and
dangerous log/markup framing are rendered literally. Stack traces, causes and
`AggregateError.errors` are not expanded. A formatter or redactor exception
returns a fixed, prebounded, non-sensitive failure envelope, never raw detail.
These pure helpers MUST load in the separately copied actions runtime with its
existing dependencies, without adding an `@actions/core` startup dependency.

Exclusions MUST be applied before the existing allowlist/protection checks.
Request-review and fallback policies MUST NOT become unconditional edit denials.
Projection MUST exclude pre-existing ignored files and unpublished edits.
This exclusion does **not** permit validation-generated ignored files: any
tracked change or untracked addition in the projected checkout MUST reject
validation, including an ignored receipt written by a passing test.
Integrity checks retain NUL-delimited `git diff --name-only -z` and
`git ls-files --others -z` output, without an `--exclude-standard` exemption.
Diagnostics distinguish tracked changes from untracked additions, using only
validated, deduplicated, sorted relative paths. Unsafe, incomplete or excessive
lists remain failures with a details-withheld explanation; the implementation
MUST NOT read those files or sanitize an invalid path into an accepted path.

Validation MUST bind unambiguously to the committed bytes and check cumulative
publication limits rather than resetting them after each local commit. A new full
validation attempt invalidates the previous candidate before checkout preflight.
A failed or cancelled readiness/full-validation check MUST NOT leave a reusable
validated candidate. A candidate becomes reusable only after all projected-tree
checks, temporary cleanup, and the final cancellation check succeed. The original
rejectable operation promise and cleanup metadata remain owned until settlement;
formatting its failure for the SDK MUST NOT turn that internal promise into a
successful cleanup result. Aborting an SDK invocation signal after ordinary
request completion MUST NOT invalidate an already successful validation.
Post-commit compare-and-swap bookkeeping and retryable index synchronization
remain intact.

The initial implementation supports a regular-file root checkout, at most
500 changed paths, 2 MiB per inspected file,
and an 8 MiB changed-content snapshot. It rejects symlinks and submodules in the
projected validation tree.

Publication remains a separate native safe-output declaration. The verified
names include `safeoutputs-create_pull_request` and `safeoutputs-noop`; a
successful local commit alone does not record a PR or complete the workflow.
This profile MUST NOT weaken host completion gates or discard previously
completed queued outputs after a later failure.

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

#### 6.5.3 Repository Tool Completion Errors

For a failed `go_repository` completion, the driver MUST retain a bounded,
sanitized `data.error.message` when the SDK supplies an error, including events
with no `data.result`. It MUST preserve explicit `success: false` and the existing
native result payload; adding diagnostic text MUST NOT synthesize success.
Reformatting a native diagnostic for logging MUST preserve independently
budgeted stdout/stderr sections rather than flattening and dropping the final
stream. Event formatting redacts the complete message before interpreting
display labels, retains at most 32 sections and 20 path summaries, and renders
section separators literally. Labels affect presentation only, never outcome
classification or path authorization.
Error codes, remediation objects, stacks and nested causes are not copied into
this diagnostic. The pending-call map and completion watchdog retain their
existing behavior.

Repository failure messages use the same redaction, literal rendering and
serialized-envelope budget as Section 5.7. They are written through the existing
JSONL event serializer, not independently printed as raw error or process-output
log lines. Embedded newlines, forged JSONL records, terminal controls, markup and
Actions command framing MUST NOT create additional log records or commands.
Ordinary operation failures MUST NOT increment the permission-denial counter or
trigger first-denial shutdown.

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

A repository tool profile MUST abort its owned work immediately during shutdown and MUST complete a bounded repository-cleanup stage before the remaining cleanup operations. Temporary-tree removal is asynchronous so it cannot block the driver's event loop.

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
- **T-CSD-203**: Repository error-only completion events preserve sanitized failure messages, explicit failure status, and pending-call/completed-output recovery behavior without counting permission denials.

#### 8.1.5 Repository Diagnostic Tests

Repository tests MUST cover final serialized-envelope boundaries and UTF-8/escape
expansion, late and cross-stream redaction, literal control/framing rendering,
bounded mutation summaries, and fixed failure fallback when formatting throws.
State regressions MUST cover failed/cancelled revalidation, cleanup failure after
successful Go stages, normal SDK request-signal completion, and commit/index-sync
recovery. Native direct/gateway regressions MUST verify the failures and repaired
sequence described in Section 5.7 without inference.

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
- Restrict event persistence to non-ephemeral events (as defined above) and avoid writing sensitive transient state to durable storage.
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
- `actions/setup/js/copilot_sdk_permissions.cjs`
- `actions/setup/js/copilot_sdk_repo_diagnostics.cjs`
- `actions/setup/js/copilot_sdk_repo_tools.cjs`
- `actions/setup/js/copilot_harness.cjs`
- `actions/setup/js/copilot_sdk_driver.test.cjs`
- `actions/setup/js/copilot_sdk_permissions.test.cjs`
- `actions/setup/js/copilot_sdk_repo_diagnostics.test.cjs`
- `actions/setup/js/copilot_sdk_repo_tools.test.cjs`
- `actions/setup/js/copilot_sdk_repository_integration.test.cjs`

This specification MUST be revalidated whenever any of the following occurs:

1. The standalone environment-variable contract changes.
2. The permission-request handling or tool-denial guardrail semantics change.
3. Harness token propagation or secret-isolation behavior changes.

---

<a id="change-log"></a>
## Change Log

### Version 1.0.3 (Draft Specification)

- Added Section 5.7 defining the opt-in `go-repository` tool profile: fixed native repository operations, no shell or generic task interface, and a compiler-owned native tool catalog.
- Defined native repository failure envelopes, bounded sanitized diagnostics, error-only event persistence, and cleanup-gated validation state without relaxing projection integrity.
- Required a bounded repository-cleanup stage that precedes the remaining driver cleanup operations.

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
