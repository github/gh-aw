// @ts-check
/// <reference types="@actions/github-script" />

const childProcess = require("child_process");
const { randomBytes } = require("crypto");
const fs = require("fs");
const { buildWorkflowCallId } = require("./aw_context.cjs");
const path = require("path");
const { nowMs } = require("./performance_now.cjs");
const { buildWorkflowRunUrl } = require("./workflow_metadata_helpers.cjs");
const { readExperimentAssignments, EXPERIMENT_ASSIGNMENTS_PATH } = require("./experiment_helpers.cjs");
const { parseJsonlContent } = require("./jsonl_helpers.cjs");
const { countSteeringEventsInApiProxyJsonl } = require("./steering_helpers.cjs");
const { resolveAICreditsFailureState } = require("./ai_credits_context.cjs");

/**
 * send_otlp_span.cjs
 *
 * Sends a single OTLP (OpenTelemetry Protocol) trace span to the configured
 * HTTP/JSON endpoint.  Used by actions/setup to instrument each job execution
 * with basic telemetry.
 *
 * Design constraints:
 * - No-op when OTEL_EXPORTER_OTLP_ENDPOINT is not set (zero overhead).
 * - Errors are non-fatal: export failures must never break the workflow.
 * - No third-party dependencies: uses only Node built-ins + native fetch.
 */

// ---------------------------------------------------------------------------
// OTel GenAI engine-to-system mapping
// ---------------------------------------------------------------------------

/**
 * Maps gh-aw internal engine IDs to the OTel GenAI semantic-convention
 * `gen_ai.system` values expected by Grafana, Datadog, Honeycomb, and Sentry.
 * Unknown engines fall back to the engine ID as-is.
 *
 * Uses Object.create(null) to avoid prototype-pollution risks from keys like
 * "constructor" or "__proto__" returning unexpected non-string values.
 * @type {Record<string, string>}
 */
const ENGINE_TO_SYSTEM_MAP = Object.assign(Object.create(null), {
  copilot: "github_models",
  claude: "anthropic",
  codex: "openai",
  gemini: "google_vertex_ai",
});

// ---------------------------------------------------------------------------
// Low-level helpers
// ---------------------------------------------------------------------------

/**
 * Generate a random 16-byte trace ID encoded as a 32-character hex string.
 * @returns {string}
 */
function generateTraceId() {
  return randomBytes(16).toString("hex");
}

/**
 * Generate a random 8-byte span ID encoded as a 16-character hex string.
 * @returns {string}
 */
function generateSpanId() {
  return randomBytes(8).toString("hex");
}

/**
 * Convert a Unix timestamp in milliseconds to a nanosecond string suitable for
 * OTLP's `startTimeUnixNano` / `endTimeUnixNano` fields.
 *
 * BigInt arithmetic avoids floating-point precision loss for large timestamps.
 *
 * @param {number} ms - milliseconds since Unix epoch
 * @returns {string} nanoseconds since Unix epoch as a decimal string
 */
function toNanoString(ms) {
  return (BigInt(Math.floor(ms)) * 1_000_000n).toString();
}

/**
 * Build a single OTLP attribute object in the key-value format expected by the
 * OTLP/HTTP JSON wire format.
 *
 * @param {string} key
 * @param {string | number | boolean} value
 * @returns {{ key: string, value: object }}
 */
function buildAttr(key, value) {
  if (typeof value === "boolean") {
    return { key, value: { boolValue: value } };
  }
  if (typeof value === "number") {
    if (Number.isFinite(value)) {
      if (Number.isInteger(value)) {
        return { key, value: { intValue: value } };
      }
      return { key, value: { doubleValue: value } };
    }
    return { key, value: { stringValue: String(value) } };
  }
  return { key, value: { stringValue: String(value) } };
}

/**
 * Build an OTLP key-value attribute whose wire type is always `doubleValue`.
 * Use for OTel attributes declared as `double` in the observability spec
 * (e.g. `gh-aw.aic`) so that Sentry EAP infers the field schema as float
 * rather than int — enabling sum()/avg()/percentile() rollups even when the
 * value is 0, which JavaScript treats as an integer and `buildAttr` would
 * encode as `intValue`, potentially creating a type mismatch across spans.
 *
 * @param {string} key
 * @param {number} value
 * @returns {{ key: string, value: { doubleValue: number } }}
 */
function buildDoubleAttr(key, value) {
  return { key, value: { doubleValue: typeof value === "number" && Number.isFinite(value) ? value : 0 } };
}

/**
 * Build an OTLP key-value attribute with an array of string values.
 * Used for OTel attributes whose type is `string[]`, such as
 * `gen_ai.response.finish_reasons`.
 *
 * @param {string} key
 * @param {string[]} values
 * @returns {{ key: string, value: { arrayValue: { values: Array<{ stringValue: string }> } } }}
 */
function buildArrayAttr(key, values) {
  return { key, value: { arrayValue: { values: values.map(v => ({ stringValue: String(v) })) } } };
}

/**
 * Build the workflow-call identifier for the current run when enough GitHub
 * context is available.
 *
 * @param {string} runId
 * @param {string} runAttempt
 * @param {string} [workflowRef]
 * @returns {string}
 */
function buildCurrentWorkflowCallId(runId, runAttempt, workflowRef = process.env.GH_AW_CURRENT_WORKFLOW_REF || process.env.GITHUB_WORKFLOW_REF || "") {
  return buildWorkflowCallId(runId, runAttempt, workflowRef);
}

/**
 * Parse a strict boolean env var.
 * @param {string | undefined} value
 * @returns {boolean | undefined}
 */
function parseBooleanEnv(value) {
  if (value === "true") return true;
  if (value === "false") return false;
  return undefined;
}

/**
 * Resolve the job name for conclusion spans.
 *
 * Normally this comes from INPUT_JOB_NAME (job-name action input), but some
 * deployment paths can miss that env var in the post step. In that case, fall
 * back to parsing the conclusion span name ("gh-aw.<job>.conclusion").
 *
 * @param {string} spanName
 * @returns {string}
 */
function resolveConclusionJobName(spanName) {
  const inputJobName = (process.env.INPUT_JOB_NAME || "").trim();
  if (inputJobName) {
    return inputJobName;
  }
  const match = /^gh-aw\.([^.]+)\.conclusion$/.exec(spanName);
  return match ? match[1] : "";
}

/**
 * Parse setup-time aw_context passed via environment before aw_info.json exists.
 *
 * @param {string | undefined} raw
 * @returns {Record<string, unknown>}
 */
function parseSetupAwContext(raw) {
  if (typeof raw !== "string" || raw.trim() === "") {
    return {};
  }
  try {
    const parsed = JSON.parse(raw);
    return parsed && typeof parsed === "object" && !Array.isArray(parsed) ? parsed : {};
  } catch {
    return {};
  }
}

/**
 * @param {unknown} value
 * @returns {string}
 */
function readContextString(value) {
  return typeof value === "string" ? value.trim() : "";
}

/**
 * Resolve the engine identifier from aw_info.json, aw_context propagation, or env vars.
 *
 * Priority:
 * 1. `awInfo.engine_id` – set by generate_aw_info.cjs on the agent job
 * 2. `awInfo.context.engine_id` – propagated via aw_context for auxiliary / dispatched jobs
 * 3. `process.env.GH_AW_INFO_ENGINE_ID` – workflow-injected env var fallback
 *
 * @param {any} awInfo
 * @returns {string}
 */
function resolveEngineId(awInfo) {
  return readContextString(awInfo.engine_id) || readContextString(awInfo.context?.engine_id) || process.env.GH_AW_INFO_ENGINE_ID || "";
}

/**
 * Resolve live episode correlation attributes directly from runtime context.
 *
 * Prefer the canonical lineage fields propagated in aw_context: episode_id for
 * the full automation session, hop_id for the current workflow invocation, and
 * parent_hop_id for the immediate caller. Legacy workflow_call_id is accepted
 * only as a compatibility fallback when the canonical fields are absent. For
 * standalone runs we fall back to the current run's run_id-run_attempt pair so
 * every live span is still queryable as a bounded execution unit.
 *
 * @param {any} awInfo
 * @param {string} runId
 * @param {string} runAttempt
 * @returns {Array<{key: string, value: object}>}
 */
function buildEpisodeAttributesFromContext(awInfo, runId, runAttempt) {
  const currentHopId = buildCurrentWorkflowCallId(runId, runAttempt);
  const inheritedHopId = readContextString(awInfo.context?.hop_id) || readContextString(awInfo.context?.workflow_call_id);
  const episodeId = readContextString(awInfo.context?.episode_id) || inheritedHopId || currentHopId;
  const parentHopId = readContextString(awInfo.context?.parent_hop_id) || (inheritedHopId && inheritedHopId !== currentHopId ? inheritedHopId : "");
  const originEvent = readContextString(awInfo.context?.origin_event) || readContextString(awInfo.context?.event_type);
  const rootRepo = readContextString(awInfo.context?.root_repo) || readContextString(awInfo.context?.repo);
  const rootWorkflowId = readContextString(awInfo.context?.root_workflow_id) || readContextString(awInfo.context?.workflow_id);

  if (!episodeId) {
    return [];
  }

  const attributes = [buildAttr("gh-aw.episode.id", episodeId), buildAttr("gh-aw.episode.kind", parentHopId ? "workflow_call" : "run")];

  if (currentHopId) {
    attributes.push(buildAttr("gh-aw.hop.id", currentHopId));
    attributes.push(buildAttr("gh-aw.workflow_call.id", currentHopId));
  }
  if (parentHopId) {
    attributes.push(buildAttr("gh-aw.hop.parent_id", parentHopId));
    attributes.push(buildAttr("gh-aw.workflow_call.parent_id", parentHopId));
  }
  if (originEvent) {
    attributes.push(buildAttr("gh-aw.origin.event", originEvent));
  }
  if (rootRepo) {
    attributes.push(buildAttr("gh-aw.root.repo", rootRepo));
  }
  if (rootWorkflowId) {
    attributes.push(buildAttr("gh-aw.root.workflow_id", rootWorkflowId));
  }

  return attributes;
}

// ---------------------------------------------------------------------------
// OTLP SpanKind constants
// ---------------------------------------------------------------------------

/** OTLP SpanKind: span represents an internal operation (default for job lifecycle spans). */
const SPAN_KIND_INTERNAL = 1;
/** OTLP SpanKind: span covers server-side handling of a remote network request. */
const SPAN_KIND_SERVER = 2;
/** OTLP SpanKind: span represents an outbound remote call. */
const SPAN_KIND_CLIENT = 3;
/** OTLP SpanKind: span represents a message producer (e.g. message queue publish). */
const SPAN_KIND_PRODUCER = 4;
/** OTLP SpanKind: span represents a message consumer (e.g. message queue subscriber). */
const SPAN_KIND_CONSUMER = 5;

// ---------------------------------------------------------------------------
// OTLP payload builder
// ---------------------------------------------------------------------------

/**
 * @typedef {Object} OTLPSpanOptions
 * @property {string} traceId           - 32-char hex trace ID
 * @property {string} spanId            - 16-char hex span ID
 * @property {string} [parentSpanId]    - 16-char hex parent span ID; omitted for root spans
 * @property {string} spanName          - Human-readable span name
 * @property {number} startMs           - Span start time (ms since epoch)
 * @property {number} endMs             - Span end time (ms since epoch)
 * @property {string} serviceName       - Value for the service.name resource attribute
 * @property {string} [scopeVersion]    - gh-aw version string (e.g. from GH_AW_INFO_VERSION)
 * @property {Array<{key: string, value: object}>} attributes - Span attributes
 * @property {Array<{key: string, value: object}>} [resourceAttributes] - Extra resource attributes (e.g. github.repository, github.run_id)
 * @property {number} [statusCode]      - OTLP status code: 0=UNSET, 1=OK, 2=ERROR (defaults to 1)
 * @property {string} [statusMessage]   - Human-readable status message (included when statusCode is 2)
 * @property {number} [kind]            - OTLP SpanKind: use SPAN_KIND_* constants. Defaults to SPAN_KIND_INTERNAL (1).
 * @property {Array<{timeUnixNano: string, name: string, attributes: Array<{key: string, value: object}>}>} [events] - Span events following the OTel events spec (e.g. exception events).
 */

/**
 * @typedef {Object} OTLPSpanRecordOptions
 * @property {string} traceId
 * @property {string} spanId
 * @property {string} [parentSpanId]
 * @property {string} spanName
 * @property {number} startMs
 * @property {number} endMs
 * @property {Array<{key: string, value: object}>} attributes
 * @property {number} [statusCode]
 * @property {string} [statusMessage]
 * @property {number} [kind]
 * @property {Array<{timeUnixNano: string, name: string, attributes: Array<{key: string, value: object}>}>} [events]
 */

/**
 * Build the OTLP span object nested under `scopeSpans[].spans[]`.
 *
 * @param {OTLPSpanRecordOptions} opts
 * @returns {any}
 */
function buildOTLPSpan({ traceId, spanId, parentSpanId, spanName, startMs, endMs, attributes, statusCode, statusMessage, kind = SPAN_KIND_INTERNAL, events }) {
  const code = typeof statusCode === "number" ? statusCode : 1; // STATUS_CODE_OK
  /** @type {{ code: number, message?: string }} */
  const status = { code };
  if (statusMessage) {
    status.message = statusMessage;
  }
  return {
    traceId,
    spanId,
    ...(parentSpanId ? { parentSpanId } : {}),
    name: spanName,
    kind,
    startTimeUnixNano: toNanoString(startMs),
    endTimeUnixNano: toNanoString(endMs),
    status,
    attributes,
    ...(events && events.length > 0 ? { events } : {}),
  };
}

/**
 * Build resource attributes for an OTLP traces payload.
 *
 * @param {string} serviceName
 * @param {string | undefined} scopeVersion
 * @param {Array<{key: string, value: object}> | undefined} resourceAttributes
 * @returns {Array<{key: string, value: object}>}
 */
function buildOTLPResourceAttributes(serviceName, scopeVersion, resourceAttributes) {
  const baseResourceAttrs = [buildAttr("service.name", serviceName)];
  if (scopeVersion && scopeVersion !== "unknown") {
    baseResourceAttrs.push(buildAttr("service.version", scopeVersion));
  }
  return resourceAttributes ? [...baseResourceAttrs, ...resourceAttributes] : baseResourceAttrs;
}

/**
 * Build the standard GitHub Actions resource attributes shared by all OTLP spans
 * (setup, conclusion, and tool spans).  Centralises the attribute list so that
 * future additions propagate to every span type automatically.
 *
 * @param {{
 *   repository: string,
 *   runId: string,
 *   eventName?: string,
 *   ref?: string,
 *   refName?: string,
 *   headRef?: string,
 *   sha?: string,
 *   job?: string,
 *   workflowRef?: string,
 *   actorId?: string,
 *   runnerOs?: string,
 *   runnerArch?: string,
 *   runnerName?: string,
 *   runnerEnvironment?: string,
 *   awfVersion?: string,
 *   awmgVersion?: string,
 *   staged: boolean,
 *   runAttempt?: string,
 * }} ctx
 * @returns {Array<{key: string, value: object}>}
 */
function buildGitHubActionsResourceAttributes({
  repository,
  runId,
  eventName = "",
  ref = "",
  refName = "",
  headRef = "",
  sha = "",
  job = "",
  workflowRef = "",
  actorId = "",
  runnerOs = "",
  runnerArch = "",
  runnerName = "",
  runnerEnvironment = "",
  awfVersion = "",
  awmgVersion = "",
  staged,
  runAttempt = "1",
}) {
  const resourceAttributes = parseOTELResourceAttributes(process.env.OTEL_RESOURCE_ATTRIBUTES || "");
  const upsertResourceAttr = attr => {
    const existingIdx = resourceAttributes.findIndex(existing => existing.key === attr.key);
    if (existingIdx >= 0) {
      resourceAttributes[existingIdx] = attr;
    } else {
      resourceAttributes.push(attr);
    }
  };

  upsertResourceAttr(buildAttr("github.repository", repository));
  upsertResourceAttr(buildAttr("github.run_id", runId));
  upsertResourceAttr(buildAttr("github.run_attempt", runAttempt));
  const runUrlAttr = buildGitHubActionsRunUrlAttribute(repository, runId);
  if (runUrlAttr) {
    upsertResourceAttr(runUrlAttr);
  }
  if (eventName) {
    upsertResourceAttr(buildAttr("github.event_name", eventName));
  }
  if (ref) {
    upsertResourceAttr(buildAttr("github.ref", ref));
  }
  if (refName) {
    upsertResourceAttr(buildAttr("github.ref_name", refName));
  }
  if (headRef) {
    upsertResourceAttr(buildAttr("github.head_ref", headRef));
  }
  if (sha) {
    upsertResourceAttr(buildAttr("github.sha", sha));
  }
  if (job) {
    upsertResourceAttr(buildAttr("github.job", job));
  }
  if (workflowRef) {
    upsertResourceAttr(buildAttr("github.workflow_ref", workflowRef));
  }
  if (actorId) {
    upsertResourceAttr(buildAttr("github.actor_id", actorId));
  }
  if (runnerOs) {
    upsertResourceAttr(buildAttr("runner.os", runnerOs));
  }
  if (runnerArch) {
    upsertResourceAttr(buildAttr("runner.arch", runnerArch));
  }
  if (runnerName) {
    upsertResourceAttr(buildAttr("runner.name", runnerName));
  }
  if (runnerEnvironment) {
    upsertResourceAttr(buildAttr("runner.environment", runnerEnvironment));
  }
  if (awfVersion) {
    upsertResourceAttr(buildAttr("gh-aw.awf.version", awfVersion));
  }
  if (awmgVersion) {
    upsertResourceAttr(buildAttr("gh-aw.awmg.version", awmgVersion));
  }
  upsertResourceAttr(buildAttr("deployment.environment", staged ? "staging" : "production"));
  return resourceAttributes;
}

/**
 * Parse an `OTEL_RESOURCE_ATTRIBUTES` value into OTLP resource attributes.
 *
 * Supports OpenTelemetry percent-encoded values and legacy gh-aw backslash
 * escapes for commas, equals signs, and backslashes (`\,`, `\=`, `\\`).
 *
 * @param {string} raw
 * @returns {Array<{key: string, value: object}>}
 */
function parseOTELResourceAttributes(raw) {
  if (!raw || !raw.trim()) return [];

  /** @type {Array<{key: string, value: object}>} */
  const attributes = [];
  let key = "";
  let value = "";
  let seenEquals = false;
  let escaped = false;
  const decodeComponent = component => {
    try {
      return decodeURIComponent(component);
    } catch {
      return component;
    }
  };

  const pushCurrent = () => {
    const trimmedKey = key.trim();
    if (trimmedKey) {
      attributes.push(buildAttr(decodeComponent(trimmedKey), decodeComponent(value.trim())));
    }
    key = "";
    value = "";
    seenEquals = false;
  };

  for (const char of raw) {
    if (escaped) {
      if (seenEquals) {
        value += char;
      } else {
        key += char;
      }
      escaped = false;
      continue;
    }
    if (char === "\\") {
      escaped = true;
      continue;
    }
    if (char === ",") {
      pushCurrent();
      continue;
    }
    if (char === "=" && !seenEquals) {
      seenEquals = true;
      continue;
    }
    if (seenEquals) {
      value += char;
    } else {
      key += char;
    }
  }
  if (escaped) {
    if (seenEquals) {
      value += "\\";
    } else {
      key += "\\";
    }
  }
  pushCurrent();

  return attributes;
}

/**
 * Build github.actions.run_url attribute when repository and run ID are available.
 *
 * @param {string} repository
 * @param {string} runId
 * @returns {{ key: string, value: object } | null}
 */
function buildGitHubActionsRunUrlAttribute(repository, runId) {
  if (!repository || !runId || !repository.includes("/")) {
    return null;
  }
  const [owner, repo] = repository.split("/");
  return buildAttr("github.actions.run_url", buildWorkflowRunUrl({ runId }, { owner, repo }));
}

/**
 * Wrap one or more OTLP span objects in a single traces payload.
 *
 * @param {{
 *   serviceName: string,
 *   scopeVersion?: string,
 *   resourceAttributes?: Array<{key: string, value: object}>,
 *   spans: object[]
 * }} opts
 * @returns {any}
 */
function buildOTLPBatchPayload({ serviceName, scopeVersion, resourceAttributes, spans }) {
  return {
    resourceSpans: [
      {
        resource: {
          attributes: buildOTLPResourceAttributes(serviceName, scopeVersion, resourceAttributes),
        },
        scopeSpans: [
          {
            scope: { name: "gh-aw", version: scopeVersion || "unknown" },
            spans,
          },
        ],
      },
    ],
  };
}

/**
 * Split a large span set into chunked OTLP payloads so high-volume exporters
 * can amortize HTTP request overhead without creating oversized requests.
 *
 * @param {{
 *   serviceName: string,
 *   scopeVersion?: string,
 *   resourceAttributes?: Array<{key: string, value: object}>,
 *   spans: object[],
 *   maxSpansPerPayload?: number
 * }} opts
 * @returns {any[]}
 */
function buildOTLPBatchPayloads({ serviceName, scopeVersion, resourceAttributes, spans, maxSpansPerPayload = 100 }) {
  const normalizedMax = Number.isInteger(maxSpansPerPayload) && maxSpansPerPayload > 0 ? maxSpansPerPayload : 100;
  const payloads = [];
  for (let index = 0; index < spans.length; index += normalizedMax) {
    payloads.push(
      buildOTLPBatchPayload({
        serviceName,
        scopeVersion,
        resourceAttributes,
        spans: spans.slice(index, index + normalizedMax),
      })
    );
  }
  return payloads;
}

/**
 * Build an OTLP/HTTP JSON traces payload wrapping a single span.
 *
 * @param {OTLPSpanOptions} opts
 * @returns {any} - Ready to be serialised as JSON and POSTed to `/v1/traces`
 */
function buildOTLPPayload({ traceId, spanId, parentSpanId, spanName, startMs, endMs, serviceName, scopeVersion, attributes, resourceAttributes, statusCode, statusMessage, kind = SPAN_KIND_INTERNAL, events }) {
  return buildOTLPBatchPayload({
    serviceName,
    scopeVersion,
    resourceAttributes,
    spans: [buildOTLPSpan({ traceId, spanId, parentSpanId, spanName, startMs, endMs, attributes, statusCode, statusMessage, kind, events })],
  });
}

// ---------------------------------------------------------------------------
// Local JSONL mirror
// ---------------------------------------------------------------------------

/**
 * Path to the OTLP telemetry mirror file.
 * Every OTLP span payload is also appended here as a JSON line so that it can
 * be inspected via GitHub Actions artifacts without needing a live collector.
 * @type {string}
 */
const OTEL_JSONL_PATH = "/tmp/gh-aw/otel.jsonl";

/**
 * Append an OTLP payload as a single JSON line to the local telemetry mirror
 * file.  Creates the `/tmp/gh-aw` directory if it does not already exist.
 * Errors are silently swallowed — mirror failures must never break the workflow.
 *
 * @param {any} payload - OTLP traces payload
 * @returns {void}
 */
function appendToOTLPJSONL(payload) {
  try {
    fs.mkdirSync("/tmp/gh-aw", { recursive: true });
    fs.appendFileSync(OTEL_JSONL_PATH, JSON.stringify(payload) + "\n");
  } catch {
    // Mirror failures are non-fatal; do not propagate.
  }
}

// ---------------------------------------------------------------------------
// Experiment assignments
// ---------------------------------------------------------------------------
// readExperimentAssignments and EXPERIMENT_ASSIGNMENTS_PATH are imported from
// experiment_helpers.cjs above.

/**
 * Build OTLP span attributes for the active experiment assignments.
 *
 * Adds one `gh-aw.experiment.<name>` attribute per experiment (carrying the
 * selected variant string) and a single `gh-aw.experiments` attribute with a
 * compact JSON string of only the valid emitted assignments (key-sorted for
 * determinism), which enables simple substring searches in backends that do
 * not support per-attribute filtering.
 *
 * Invalid assignments (non-string or empty-string variants) are skipped for
 * both the per-experiment attributes and the aggregated JSON.
 *
 * Returns an empty array when no assignments are available.
 *
 * @param {Record<string, string> | null} assignments
 * @returns {Array<{key: string, value: object}>}
 */
function buildExperimentAttributes(assignments) {
  if (!assignments || typeof assignments !== "object") return [];
  const names = Object.keys(assignments).sort();
  if (names.length === 0) return [];
  const attrs = [];
  /** @type {Record<string, string>} */
  const validAssignments = {};
  for (const name of names) {
    const variant = assignments[name];
    if (typeof variant === "string" && variant) {
      attrs.push(buildAttr(`gh-aw.experiment.${name}`, variant));
      validAssignments[name] = variant;
    }
  }
  if (attrs.length > 0) {
    attrs.push(buildAttr("gh-aw.experiments", JSON.stringify(validAssignments)));
  }
  return attrs;
}

/**
 * Build summary attributes and per-result events from valid deterministic grader output.
 * Free-form grader messages, details, and errors are intentionally excluded because
 * custom graders may derive them from trace content containing sensitive values.
 *
 * @param {any} graderOutput
 * @param {number} eventTimeMs
 * @returns {{attributes: Array<{key: string, value: object}>, events: Array<{timeUnixNano: string, name: string, attributes: Array<{key: string, value: object}>}>}}
 */
function buildGraderTelemetry(graderOutput, eventTimeMs) {
  if (!graderOutput || typeof graderOutput !== "object" || !Array.isArray(graderOutput.results) || graderOutput.results.length === 0) {
    return { attributes: [], events: [] };
  }

  const results = graderOutput.results.filter(result => result && typeof result === "object" && typeof result.id === "string" && result.id);
  if (results.length === 0) {
    return { attributes: [], events: [] };
  }

  const countByStatus = status => results.filter(result => result.status === status).length;
  const attributes = [
    buildAttr("gh-aw.graders.count", results.length),
    buildAttr("gh-aw.graders.passed", countByStatus("pass")),
    buildAttr("gh-aw.graders.failed", countByStatus("fail")),
    buildAttr("gh-aw.graders.errors", countByStatus("error")),
    buildAttr("gh-aw.graders.unavailable", countByStatus("unavailable")),
    buildAttr("gh-aw.graders.other", results.length - countByStatus("pass") - countByStatus("fail") - countByStatus("error") - countByStatus("unavailable")),
  ];
  const timeUnixNano = toNanoString(eventTimeMs);
  const events = results.map(result => {
    const resultAttributes = [buildAttr("gh-aw.grader.id", result.id)];
    if (typeof result.name === "string" && result.name) resultAttributes.push(buildAttr("gh-aw.grader.name", result.name));
    if (typeof result.status === "string" && result.status) resultAttributes.push(buildAttr("gh-aw.grader.status", result.status));
    if (typeof result.source === "string" && result.source) resultAttributes.push(buildAttr("gh-aw.grader.source", result.source));
    if (typeof result.unit === "string" && result.unit) resultAttributes.push(buildAttr("gh-aw.grader.unit", result.unit));
    if (typeof result.value === "number" && Number.isFinite(result.value)) resultAttributes.push(buildDoubleAttr("gh-aw.grader.value", result.value));
    if (typeof result.passed === "boolean") resultAttributes.push(buildAttr("gh-aw.grader.passed", result.passed));
    if (typeof result.severity === "string" && result.severity) resultAttributes.push(buildAttr("gh-aw.grader.severity", result.severity));
    if (typeof result.baselineValue === "number" && Number.isFinite(result.baselineValue)) resultAttributes.push(buildDoubleAttr("gh-aw.grader.baseline_value", result.baselineValue));
    if (typeof result.deltaFromBaseline === "number" && Number.isFinite(result.deltaFromBaseline)) resultAttributes.push(buildDoubleAttr("gh-aw.grader.delta_from_baseline", result.deltaFromBaseline));
    return { timeUnixNano, name: "grader.result", attributes: resultAttributes };
  });

  return { attributes, events };
}

/**
 * Build summary attributes and per-result events from BinEval JSONL records.
 * Only bounded, structured fields are included; free-form questions are excluded.
 *
 * @param {any[]} evalResults
 * @param {number} eventTimeMs
 * @returns {{attributes: Array<{key: string, value: object}>, events: Array<{timeUnixNano: string, name: string, attributes: Array<{key: string, value: object}>}>}}
 */
function buildEvalTelemetry(evalResults, eventTimeMs) {
  if (!Array.isArray(evalResults)) {
    return { attributes: [], events: [] };
  }
  const results = evalResults.filter(result => result && typeof result === "object" && typeof result.id === "string" && result.id);
  if (results.length === 0) {
    return { attributes: [], events: [] };
  }

  const normalizedAnswers = results.map(result => {
    const answer = typeof result.answer === "string" ? result.answer.trim().toUpperCase() : "UNKNOWN";
    return answer === "YES" || answer === "NO" ? answer : "UNKNOWN";
  });
  const countAnswer = answer => normalizedAnswers.filter(value => value === answer).length;
  const attributes = [buildAttr("gh-aw.evals.count", results.length), buildAttr("gh-aw.evals.yes", countAnswer("YES")), buildAttr("gh-aw.evals.no", countAnswer("NO")), buildAttr("gh-aw.evals.unknown", countAnswer("UNKNOWN"))];
  const timeUnixNano = toNanoString(eventTimeMs);
  const events = results.map((result, index) => {
    const resultAttributes = [buildAttr("gh-aw.eval.id", result.id), buildAttr("gh-aw.eval.answer", normalizedAnswers[index])];
    if (typeof result.model === "string" && result.model) {
      resultAttributes.push(buildAttr("gh-aw.eval.model", result.model));
    }
    return { timeUnixNano, name: "eval.result", attributes: resultAttributes };
  });

  return { attributes, events };
}

// ---------------------------------------------------------------------------
// Custom OTLP attributes (GH_AW_OTLP_ATTRIBUTES)
// ---------------------------------------------------------------------------

/**
 * Parse the GH_AW_OTLP_ATTRIBUTES environment variable into a plain object.
 * The variable is a JSON-encoded `Record<string, string>` injected by the
 * gh-aw compiler from the `observability.otlp.attributes` frontmatter field.
 * Returns null when the variable is absent, empty, or not valid JSON.
 *
 * @returns {Record<string, string> | null}
 */
function parseOTLPCustomAttributes() {
  const raw = process.env.GH_AW_OTLP_ATTRIBUTES;
  if (!raw) return null;
  try {
    const parsed = JSON.parse(raw);
    if (parsed === null || typeof parsed !== "object" || Array.isArray(parsed)) return null;
    return /** @type {Record<string, string>} */ parsed;
  } catch {
    return null;
  }
}

/**
 * Build additional OTLP attribute objects from the GH_AW_OTLP_ATTRIBUTES
 * environment variable.
 *
 * Attribute values are used as-is (use GitHub Actions expressions like
 * `${{ vars.MY_VALUE }}` in workflow frontmatter for dynamic values).
 * Attributes whose value is an empty string are omitted.  When no custom
 * attributes are configured, an empty array is returned.
 *
 * @returns {Array<{key: string, value: object}>}
 */
function buildCustomOTLPAttributes() {
  const customDefs = parseOTLPCustomAttributes();
  if (!customDefs) return [];

  const result = [];
  for (const [key, value] of Object.entries(customDefs)) {
    if (typeof key !== "string" || !key || typeof value !== "string") continue;
    if (value !== "") {
      result.push(buildAttr(key, value));
    }
  }
  return result;
}

// ---------------------------------------------------------------------------
// HTTP transport
// ---------------------------------------------------------------------------

/**
 * Parse an `OTEL_EXPORTER_OTLP_HEADERS` value into a plain object suitable for
 * merging into a `Headers` / `fetch` `headers` option.
 *
 * The value follows the OpenTelemetry specification:
 *   key=value[,key=value...]
 * where individual keys and values may be percent-encoded.
 * Empty pairs (from leading/trailing/consecutive commas) are silently skipped.
 *
 * @param {string} raw - Raw header string (e.g. "Authorization=Bearer tok,X-Tenant=acme")
 * @returns {Record<string, string>} Parsed headers object
 */
function parseOTLPHeaders(raw) {
  if (!raw || !raw.trim()) return {};
  /** @type {Record<string, string>} */
  const result = {};
  for (const pair of raw.split(",")) {
    const eqIdx = pair.indexOf("=");
    if (eqIdx <= 0) continue; // skip malformed pairs (no =) or empty keys (= at start)
    try {
      // Decode before trimming so percent-encoded whitespace (%20) at edges is preserved correctly.
      const key = decodeURIComponent(pair.slice(0, eqIdx)).trim();
      const value = decodeURIComponent(pair.slice(eqIdx + 1)).trim();
      if (key) result[key] = value;
    } catch {
      // Ignore only the malformed pair so other configured headers remain usable.
    }
  }
  return result;
}

const EMPTY_OTLP_AUTHORIZATION_SCHEMES = new Set([
  "api-key",
  "apikey",
  "basic",
  "bearer",
  "concealed",
  "digest",
  "dpop",
  "dsn",
  "gnap",
  "hoba",
  "mutual",
  "negotiate",
  "oauth",
  "privatetoken",
  "scram-sha-1",
  "scram-sha-256",
  "sentry",
  "vapid",
]);

/**
 * @param {string} raw
 * @returns {boolean}
 */
function hasEmptyOTLPAuthorizationHeader(raw) {
  const headers = parseOTLPHeaders(raw);
  return Object.entries(headers).some(([key, value]) => {
    const normalizedKey = key.toLowerCase();
    return (normalizedKey === "authorization" || normalizedKey === "x-sentry-auth") && (value === "" || EMPTY_OTLP_AUTHORIZATION_SCHEMES.has(value.toLowerCase()));
  });
}

/**
 * Determine whether OTLP export should use a proxy-aware transport.
 * Native Node fetch does not honor proxy environment variables by default.
 *
 * @param {string} endpoint
 * @returns {boolean}
 */
function hasProxyConfigured(endpoint) {
  const isHTTPS = /^https:/i.test(endpoint);
  if (isHTTPS) {
    return Boolean(process.env.HTTPS_PROXY || process.env.https_proxy || process.env.ALL_PROXY || process.env.all_proxy);
  }
  return Boolean(process.env.HTTP_PROXY || process.env.http_proxy || process.env.HTTPS_PROXY || process.env.https_proxy || process.env.ALL_PROXY || process.env.all_proxy);
}

/**
 * Send OTLP through curl so standard proxy environment variables are honored.
 *
 * @param {string} url
 * @param {Record<string, string>} headers
 * @param {string} body
 * @returns {{ ok: boolean, status: number, statusText: string }}
 */
function sendOTLPViaCurl(url, headers, body) {
  /** @type {string[]} */
  const args = ["--silent", "--show-error", "--location", "--output", "/dev/null", "--write-out", "%{http_code}", "--request", "POST", "--data-binary", "@-"];

  for (const [key, value] of Object.entries(headers)) {
    args.push("--header", `${key}: ${value}`);
  }
  args.push(url);

  const result = childProcess.spawnSync("curl", args, {
    encoding: "utf8",
    input: body,
    maxBuffer: 1024 * 1024,
    timeout: 15_000,
  });

  if (result.error) {
    throw result.error;
  }

  const status = Number.parseInt((result.stdout || "").trim(), 10);
  if (Number.isInteger(status)) {
    return {
      ok: status >= 200 && status < 300,
      status,
      statusText: result.status === 0 ? "OK" : (result.stderr || "curl failed").trim() || "curl failed",
    };
  }

  return {
    ok: false,
    status: 0,
    statusText: (result.stderr || "curl failed").trim() || "curl failed",
  };
}

/**
 * Regular expression matching attribute key fragments that indicate the value
 * is sensitive and should be redacted before the payload is sent over the
 * wire.  The pattern is case-insensitive.  Word-boundary anchors (`\b`) are
 * used for `key` so that generic infrastructure keys like `sort_key` or
 * `cache_key` (where "key" is preceded by an underscore, a word character)
 * are **not** over-redacted, while dot-separated forms like `app.key` and
 * standalone `key` attributes are still caught.
 * @type {RegExp}
 */
const SENSITIVE_ATTR_KEY_RE = /token|secret|password|passwd|\bkey\b|auth|credential|api[_-]?key|access[_-]?key/i;

/**
 * Maximum length (in characters) allowed for a string attribute value.
 * Values that exceed this limit are truncated to avoid sending unexpectedly
 * large payloads to the OTLP collector.
 * @type {number}
 */
const MAX_ATTR_VALUE_LENGTH = 1024;

/**
 * Redaction placeholder substituted for sensitive attribute values.
 * @type {string}
 */
const REDACTED = "[REDACTED]";

const FETCH_TIMEOUT_MS = 120_000;

/**
 * Sanitize an array of OTLP key-value attributes in-place (shallowly cloned).
 *
 * For each attribute:
 * - If the key matches {@link SENSITIVE_ATTR_KEY_RE} the string value is
 *   replaced with {@link REDACTED}.
 * - String values longer than {@link MAX_ATTR_VALUE_LENGTH} are truncated.
 *
 * @param {Array<{key: string, value: object}>} attrs
 * @returns {Array<{key: string, value: object}>}
 */
function sanitizeAttrs(attrs) {
  if (!Array.isArray(attrs)) return attrs;
  return attrs.map(attr => {
    if (!attr || typeof attr.key !== "string") return attr;
    const isSensitive = SENSITIVE_ATTR_KEY_RE.test(attr.key);
    const val = attr.value;
    if (typeof val !== "object" || val === null) return attr;
    if (isSensitive && "stringValue" in val) {
      return { key: attr.key, value: { stringValue: REDACTED } };
    }
    if (!isSensitive && "stringValue" in val && typeof val.stringValue === "string" && val.stringValue.length > MAX_ATTR_VALUE_LENGTH) {
      return { key: attr.key, value: { stringValue: val.stringValue.slice(0, MAX_ATTR_VALUE_LENGTH) } };
    }
    return attr;
  });
}

/**
 * Sanitize an OTLP traces payload before sending it over the wire.
 *
 * Walks the `resourceSpans[].resource.attributes`,
 * `resourceSpans[].scopeSpans[].spans[].attributes`, and
 * `resourceSpans[].scopeSpans[].spans[].events[].attributes` arrays and applies
 * {@link sanitizeAttrs} to each, redacting values for sensitive keys and
 * truncating excessively long string values.
 *
 * The original payload object is not mutated; a shallow-clone is returned.
 *
 * @param {any} payload - OTLP traces payload produced by {@link buildOTLPPayload}
 * @returns {any} Sanitized payload suitable for serialisation
 */
function sanitizeOTLPPayload(payload) {
  if (!payload || !Array.isArray(payload.resourceSpans)) return payload;
  return {
    ...payload,
    resourceSpans: payload.resourceSpans.map(rs => ({
      ...rs,
      resource: rs.resource ? { ...rs.resource, attributes: sanitizeAttrs(rs.resource.attributes) } : rs.resource,
      scopeSpans: Array.isArray(rs.scopeSpans)
        ? rs.scopeSpans.map(ss => ({
            ...ss,
            spans: Array.isArray(ss.spans)
              ? ss.spans.map(span => ({
                  ...span,
                  attributes: sanitizeAttrs(span.attributes),
                  events: Array.isArray(span.events) ? span.events.map(ev => ({ ...ev, attributes: sanitizeAttrs(ev.attributes) })) : span.events,
                }))
              : ss.spans,
          }))
        : rs.scopeSpans,
    })),
  };
}

/**
 * Sanitize persisted OTLP export failure reasons before they are mirrored into
 * JSONL artifacts or exported on conclusion spans.
 *
 * @param {string} reason
 * @returns {string}
 */
function sanitizeOTLPExportErrorReason(reason) {
  return reason
    .trim()
    .replace(/\bhttps?:\/\/[^\s"'`<>]+/gi, REDACTED)
    .slice(0, MAX_ATTR_VALUE_LENGTH);
}

/**
 * @param {{ ok: boolean, status?: number, statusText?: string }} response
 * @returns {string}
 */
function getOTLPExportFailureReason(response) {
  const statusText = typeof response.statusText === "string" ? response.statusText.trim() : "";
  if (statusText && !(response.ok === false && /^ok$/i.test(statusText))) {
    return statusText;
  }
  const status = typeof response.status === "number" ? response.status : 0;
  return Number.isInteger(status) && status > 0 ? `HTTP ${status}` : "HTTP request failed";
}

/**
 * @param {{ status?: number }} response
 * @param {string} reason
 * @returns {string}
 */
function formatOTLPExportFailureMessage(response, reason) {
  if (reason.startsWith("HTTP ")) {
    return reason;
  }
  const status = typeof response.status === "number" ? response.status : 0;
  return Number.isInteger(status) && status > 0 ? `HTTP ${status} ${reason}` : reason;
}

// ---------------------------------------------------------------------------
// Multi-endpoint support
// ---------------------------------------------------------------------------

/**
 * @typedef {Object} OTLPEndpointEntry
 * @property {string} url      - OTLP base URL (e.g. https://traces.example.com:4317)
 * @property {string} [headers] - Per-endpoint headers in "key=value,key=value" format
 */

/**
 * @typedef {Object} OTLPExportErrorDetail
 * @property {string} host
 * @property {number} [status]
 * @property {string} reason
 */

/**
 * Normalizes GH_AW_OTLP_IF_MISSING to a supported mode. Mirrors the compiler's
 * getOTLPIfMissingMode (pkg/workflow/observability_otlp.go) and the equivalent
 * helper in start_mcp_gateway.cjs so every OTLP consumer applies the same
 * missing-configuration policy.
 * @param {string | undefined} value
 * @returns {"error" | "warn" | "ignore"}
 */
function getOTLPIfMissingMode(value) {
  const normalized = (value || "").trim().toLowerCase();
  if (normalized === "warn" || normalized === "ignore") {
    return normalized;
  }
  return "error";
}

/**
 * Resolve the list of configured OTLP endpoints for the current run.
 *
 * Reads `GH_AW_OTLP_ENDPOINTS` (JSON-encoded array produced by the gh-aw
 * compiler for all endpoint configurations, including single-endpoint setups).
 * Returns an empty array when no endpoint is configured, so callers can skip
 * the export step without additional checks.
 *
 * When `GH_AW_OTLP_IF_MISSING=ignore` (set only for the enterprise-default
 * endpoint/headers fallback — see resolveOTLPEndpointEntries in
 * pkg/workflow/observability_otlp.go), an endpoint with a URL but no headers is
 * dropped rather than exported unauthenticated. This is the single choke point
 * used by every span emitter (job-setup, conclusion, outcome, MCP gateway), so
 * the guard applies uniformly regardless of job ordering — a half-configured
 * default (endpoint set, headers secret unset) can never leak telemetry before
 * the agent job's check_otlp_default_credentials.sh step runs, because that
 * step only runs in one job while this function runs in all of them.
 *
 * @returns {OTLPEndpointEntry[]}
 */
function parseOTLPEndpoints() {
  const raw = process.env.GH_AW_OTLP_ENDPOINTS || "";
  if (!raw) return [];
  try {
    const parsed = JSON.parse(raw);
    if (Array.isArray(parsed) && parsed.length > 0) {
      const ignoreMissingCredentials = getOTLPIfMissingMode(process.env.GH_AW_OTLP_IF_MISSING) === "ignore";
      /** @type {OTLPEndpointEntry[]} */
      const valid = parsed
        .filter(e => e && typeof e.url === "string" && e.url.trim() !== "")
        .filter(e => {
          const hasHeaders = typeof e.headers === "string" && e.headers !== "";
          if (hasHeaders && hasEmptyOTLPAuthorizationHeader(e.headers)) {
            return false;
          }
          if (ignoreMissingCredentials && !hasHeaders) {
            // Enterprise-default endpoint configured without matching credentials:
            // treat as unconfigured instead of exporting unauthenticated telemetry.
            return false;
          }
          return true;
        })
        .map(e => ({
          url: e.url,
          ...(typeof e.headers === "string" && e.headers ? { headers: e.headers } : {}),
        }));
      return valid;
    }
  } catch {
    // Invalid JSON — ignored, no endpoints available.
  }
  return [];
}

/**
 * Send an OTLP payload to all configured endpoints concurrently.
 *
 * Uses `Promise.allSettled` so a failure on one endpoint never prevents
 * delivery to the others.  The local JSONL mirror is written once by the
 * caller before invoking this function (pass `skipJSONL: true`).
 *
 * @param {OTLPEndpointEntry[]} endpoints  - Resolved endpoint list from {@link parseOTLPEndpoints}
 * @param {any} payload                 - Serialisable OTLP JSON object
 * @param {{ maxRetries?: number, baseDelayMs?: number, skipJSONL?: boolean }} [opts]
 * @returns {Promise<void>}
 */
async function sendOTLPToAllEndpoints(endpoints, payload, opts = {}) {
  if (endpoints.length === 0) return;
  await Promise.allSettled(
    endpoints.map(ep =>
      sendOTLPSpan(ep.url, payload, {
        ...opts,
        // Pass per-endpoint headers so each collector receives only its own
        // credentials (not the merged set from a different endpoint).
        headersOverride: ep.headers !== undefined ? ep.headers : "",
      })
    )
  );
}

/**
 * POST an OTLP traces payload to `{endpoint}/v1/traces` with automatic retries.
 *
 * Failures are surfaced as `console.warn` messages and never thrown; OTLP
 * export failures must not break the workflow.  Uses exponential back-off
 * between attempts (100 ms, 200 ms) so the three total attempts finish in
 * well under a second in the typical success case.
 *
 * Reads `OTEL_EXPORTER_OTLP_HEADERS` from the environment and merges any
 * configured headers into every request, unless `headersOverride` is provided
 * (used for per-endpoint headers in the multi-endpoint case).
 *
 * @param {string} endpoint  - OTLP base URL (e.g. https://traces.example.com:4317)
 * @param {any} payload   - Serialisable OTLP JSON object
 * @param {{ maxRetries?: number, baseDelayMs?: number, skipJSONL?: boolean, headersOverride?: string }} [opts]
 * @returns {Promise<void>}
 */
async function sendOTLPSpan(endpoint, payload, { maxRetries = 2, baseDelayMs = 100, skipJSONL = false, headersOverride = undefined } = {}) {
  // Mirror payload locally so it survives even when the collector is unreachable.
  // Callers that already wrote the JSONL mirror pass skipJSONL: true to avoid a
  // duplicate line.
  if (!skipJSONL) {
    appendToOTLPJSONL(payload);
  }

  const url = endpoint.replace(/\/$/, "") + "/v1/traces";
  // Use headersOverride when explicitly provided (including empty string, which means
  // "this endpoint has no configured headers" in the multi-endpoint fan-out path).
  // Fall back to OTEL_EXPORTER_OTLP_HEADERS only when headersOverride is absent
  // (undefined), which is the legacy single-endpoint case.
  const rawHeaders = headersOverride !== undefined ? headersOverride : process.env.OTEL_EXPORTER_OTLP_HEADERS || "";
  const extraHeaders = parseOTLPHeaders(rawHeaders);
  const headers = { "Content-Type": "application/json", ...extraHeaders };
  const sanitizedBody = JSON.stringify(sanitizeOTLPPayload(payload));
  for (let attempt = 0; attempt <= maxRetries; attempt++) {
    if (attempt > 0) {
      await new Promise(resolve => setTimeout(resolve, baseDelayMs * 2 ** (attempt - 1)));
    }
    try {
      const response = hasProxyConfigured(endpoint)
        ? sendOTLPViaCurl(url, headers, sanitizedBody)
        : await fetch(url, {
            method: "POST",
            headers,
            body: sanitizedBody,
            signal: AbortSignal.timeout(FETCH_TIMEOUT_MS),
          });
      if (response.ok) {
        return;
      }
      const reason = getOTLPExportFailureReason(response);
      const msg = formatOTLPExportFailureMessage(response, reason);
      if (attempt < maxRetries) {
        console.warn(`OTLP export attempt ${attempt + 1}/${maxRetries + 1} failed: ${msg}, retrying…`);
      } else {
        console.warn(`OTLP export failed after ${maxRetries + 1} attempts: ${msg}`);
        recordOTLPExportError({
          endpoint,
          ...(Number.isInteger(response.status) && response.status > 0 ? { status: response.status } : {}),
          reason,
        });
      }
    } catch (err) {
      const msg = getErrorMessage(err);
      if (attempt < maxRetries) {
        console.warn(`OTLP export attempt ${attempt + 1}/${maxRetries + 1} error: ${msg}, retrying…`);
      } else {
        console.warn(`OTLP export error after ${maxRetries + 1} attempts: ${msg}`);
        recordOTLPExportError({ endpoint, reason: msg });
      }
    }
  }
}

// ---------------------------------------------------------------------------
// High-level: job setup span
// ---------------------------------------------------------------------------

/**
 * Regular expression that matches a valid OTLP trace ID: 32 lowercase hex characters.
 * @type {RegExp}
 */
const TRACE_ID_RE = /^[0-9a-f]{32}$/;

/**
 * Validate that a string is a well-formed OTLP trace ID (32 lowercase hex chars).
 * @param {string} id
 * @returns {boolean}
 */
function isValidTraceId(id) {
  return TRACE_ID_RE.test(id);
}

/**
 * Regular expression that matches a valid OTLP span ID: 16 lowercase hex characters.
 * @type {RegExp}
 */
const SPAN_ID_RE = /^[0-9a-f]{16}$/;

/**
 * Validate that a string is a well-formed OTLP span ID (16 lowercase hex chars).
 * @param {string} id
 * @returns {boolean}
 */
function isValidSpanId(id) {
  return SPAN_ID_RE.test(id);
}

/**
 * @typedef {Object} SendJobSetupSpanOptions
 * @property {number} [startMs]  - Override for the span start time (ms).  Defaults to `Date.now()`.
 * @property {string} [traceId] - Existing trace ID to reuse for cross-job correlation.
 *   When omitted the value is taken from the `INPUT_TRACE_ID` environment variable (the
 *   `trace-id` action input); if that is also absent the `otel_trace_id` field from
 *   `aw_info.context` is used (propagated from the parent workflow via `aw_context`);
 *   and if none of those are set a new random trace ID is generated.
 *   Pass the `trace-id` output of the activation job setup step to correlate all
 *   subsequent job spans under the same trace.
 * @property {string} [parentSpanId] - Parent span ID to use for setup-span nesting.
 *   When omitted the value is taken from the `INPUT_PARENT_SPAN_ID` environment variable
 *   (the `parent-span-id` action input); if that is also absent the
 *   `otel_parent_span_id` field from `aw_info.context` is used.
 */

/**
 * Send a `gh-aw.<jobName>.setup` span (or `gh-aw.job.setup` when no job name
 * is configured) to the configured OTLP endpoint.
 *
 * This is designed to be called from `actions/setup/index.js` immediately after
 * the setup script completes.  It always returns `{ traceId, spanId, parentSpanId }` so callers
 * can expose the trace ID as an action output and write both values to `$GITHUB_ENV`
 * for downstream step correlation — even when `OTEL_EXPORTER_OTLP_ENDPOINT` is not
 * set (no span is sent in that case).
 * Errors are swallowed so the workflow is never broken by tracing failures.
 *
 * Environment variables consumed:
 * - `OTEL_EXPORTER_OTLP_ENDPOINT` – collector endpoint (required to send anything)
 * - `OTEL_SERVICE_NAME`            – service name (defaults to "gh-aw")
 * - `INPUT_JOB_NAME`               – job name passed via the `job-name` action input
 * - `INPUT_TRACE_ID`               – optional trace ID passed via the `trace-id` action input
 * - `INPUT_PARENT_SPAN_ID`         – optional parent span ID passed via the `parent-span-id` action input
 * - `GH_AW_INFO_WORKFLOW_NAME`     – workflow name injected by the gh-aw compiler
 * - `GH_AW_INFO_ENGINE_ID`         – engine ID injected by the gh-aw compiler
 * - `GITHUB_RUN_ID`                – GitHub Actions run ID
 * - `GITHUB_ACTOR`                 – GitHub Actions actor (user / bot)
 * - `GITHUB_REPOSITORY`            – `owner/repo` string
 *
 * Runtime files read (optional):
 * - `/tmp/gh-aw/aw_info.json` – when present, `context.otel_trace_id` is used as a fallback
 *   trace ID so that dispatched child workflows share the parent's OTLP trace;
 *   `context.otel_parent_span_id` is used as the parent span ID so the child's setup span
 *   is properly nested under the parent's setup span in the trace hierarchy; and
 *   `context.item_type`, `context.item_number`, `context.trigger_label`, and `context.comment_id`
 *   are emitted as `gh-aw.trigger.item_type`, `gh-aw.trigger.item_number`, `gh-aw.trigger.label`,
 *   and `gh-aw.trigger.comment_id` attributes so every span can be linked back to the GitHub item
 *   (and specific comment) that triggered the workflow
 *
 * @param {SendJobSetupSpanOptions} [options]
 * @returns {Promise<{ traceId: string, spanId: string, parentSpanId: string }>} The trace/span IDs used and resolved parent span ID.
 */
async function sendJobSetupSpan(options = {}) {
  // Resolve the trace ID before the early-return so it is always available as
  // an action output regardless of whether OTLP is configured.
  // Priority: options.traceId > INPUT_TRACE_ID > aw_info.context.otel_trace_id > newly generated ID.
  // Invalid (wrong length, non-hex) values are silently discarded.

  // Validate options.traceId if supplied; callers may pass raw user input.
  const optionsTraceId = options.traceId && isValidTraceId(options.traceId) ? options.traceId : "";
  const optionsParentSpanId = options.parentSpanId && isValidSpanId(options.parentSpanId) ? options.parentSpanId : "";

  // Normalize INPUT_TRACE_ID to lowercase before validating: OTLP requires lowercase
  // hex, but trace IDs pasted from external tools may use uppercase characters.
  // Also handle INPUT_TRACE-ID (with hyphen) in case the runner preserves the original
  // input name hyphen instead of converting it to an underscore.
  const rawInputTraceId = (process.env.INPUT_TRACE_ID || process.env["INPUT_TRACE-ID"] || "").trim().toLowerCase();
  const inputTraceId = isValidTraceId(rawInputTraceId) ? rawInputTraceId : "";
  const rawInputParentSpanId = (process.env.INPUT_PARENT_SPAN_ID || process.env["INPUT_PARENT-SPAN-ID"] || "").trim().toLowerCase();
  const inputParentSpanId = isValidSpanId(rawInputParentSpanId) ? rawInputParentSpanId : "";

  // When this job was dispatched by a parent workflow, the parent's trace ID is
  // propagated via aw_context.otel_trace_id → aw_info.context.otel_trace_id so that
  // composite-action spans share a single trace with their caller.
  const awInfo = readJSONIfExists("/tmp/gh-aw/aw_info.json") || {};
  const setupAwContext = parseSetupAwContext(process.env.GH_AW_SETUP_AW_CONTEXT);
  if ((!awInfo.context || typeof awInfo.context !== "object") && Object.keys(setupAwContext).length > 0) {
    awInfo.context = setupAwContext;
  }
  const rawContextTraceId = typeof awInfo.context?.otel_trace_id === "string" ? awInfo.context.otel_trace_id.trim().toLowerCase() : "";
  const contextTraceId = isValidTraceId(rawContextTraceId) ? rawContextTraceId : "";
  // When this job was dispatched by a parent workflow, the parent's setup span ID is
  // propagated via aw_context.otel_parent_span_id → aw_info.context.otel_parent_span_id so
  // that the child's setup span is nested under the parent's setup span in the trace.
  const rawContextParentSpanId = typeof awInfo.context?.otel_parent_span_id === "string" ? awInfo.context.otel_parent_span_id.trim().toLowerCase() : "";
  const contextParentSpanId = isValidSpanId(rawContextParentSpanId) ? rawContextParentSpanId : "";
  const staged = awInfo.staged === true || process.env.GH_AW_INFO_STAGED === "true";
  const itemType = typeof awInfo.context?.item_type === "string" ? awInfo.context.item_type : "";
  const itemNumber = typeof awInfo.context?.item_number === "string" ? awInfo.context.item_number : "";
  const triggerLabel = typeof awInfo.context?.trigger_label === "string" ? awInfo.context.trigger_label : "";
  const commentId = typeof awInfo.context?.comment_id === "string" ? awInfo.context.comment_id : "";
  const frontmatterSource = (typeof awInfo.frontmatter_source === "string" ? awInfo.frontmatter_source : "") || process.env.GH_AW_INFO_FRONTMATTER_SOURCE || "";
  const frontmatterEmoji = (typeof awInfo.frontmatter_emoji === "string" ? awInfo.frontmatter_emoji : "") || process.env.GH_AW_INFO_FRONTMATTER_EMOJI || "";
  const awfVersion = (typeof awInfo.awf_version === "string" ? awInfo.awf_version : "") || process.env.GH_AW_INFO_AWF_VERSION || "";
  const awmgVersion = (typeof awInfo.awmg_version === "string" ? awInfo.awmg_version : "") || process.env.GH_AW_INFO_AWMG_VERSION || "";
  const bodyModified = typeof awInfo.body_modified === "boolean" ? awInfo.body_modified : parseBooleanEnv(process.env.GH_AW_INFO_BODY_MODIFIED);

  const traceId = optionsTraceId || inputTraceId || contextTraceId || generateTraceId();
  const parentSpanId = optionsParentSpanId || inputParentSpanId || contextParentSpanId || "";

  // Always generate a span ID so it can be written to GITHUB_ENV as
  // GITHUB_AW_OTEL_PARENT_SPAN_ID even when OTLP is not configured, allowing downstream
  // scripts to establish the correct parent span context.
  const spanId = generateSpanId();

  // Build the full payload unconditionally so the JSONL mirror is always written,
  // enabling artifact-based debugging even without a live OTLP collector.
  const startMs = options.startMs ?? nowMs();
  const endMs = nowMs();

  const serviceName = process.env.OTEL_SERVICE_NAME || "gh-aw";
  const jobName = process.env.INPUT_JOB_NAME || "";
  const workflowName = process.env.GH_AW_INFO_WORKFLOW_NAME || process.env.GH_AW_SETUP_WORKFLOW_NAME || process.env.GITHUB_WORKFLOW || "";
  const engineId = resolveEngineId(awInfo);
  const runId = process.env.GITHUB_RUN_ID || "";
  const runAttempt = process.env.GITHUB_RUN_ATTEMPT || "1";
  const actor = process.env.GITHUB_ACTOR || "";
  const repository = process.env.GITHUB_REPOSITORY || "";
  const eventName = process.env.GITHUB_EVENT_NAME || "";
  const ref = process.env.GITHUB_REF || "";
  const refName = process.env.GITHUB_REF_NAME || "";
  const headRef = process.env.GITHUB_HEAD_REF || "";
  const sha = process.env.GITHUB_SHA || "";
  const job = process.env.GITHUB_JOB || "";
  const workflowRef = process.env.GH_AW_CURRENT_WORKFLOW_REF || process.env.GITHUB_WORKFLOW_REF || "";
  const actorId = process.env.GITHUB_ACTOR_ID || "";
  const runnerOs = process.env.RUNNER_OS || "";
  const runnerArch = process.env.RUNNER_ARCH || "";
  const runnerName = process.env.RUNNER_NAME || "";
  const runnerEnvironment = process.env.RUNNER_ENVIRONMENT || "";
  const scopeVersion =
    (typeof awInfo.cli_version === "string" ? awInfo.cli_version : "") || process.env.GH_AW_INFO_CLI_VERSION || awInfo.agent_version || awInfo.version || process.env.GH_AW_INFO_VERSION || process.env.GITHUB_SHA || "unknown";

  const attributes = [
    buildAttr("gh-aw.job.name", jobName),
    buildAttr("gh-aw.workflow.name", workflowName),
    buildAttr("gh-aw.run.id", runId),
    buildAttr("gh-aw.run.attempt", runAttempt),
    buildAttr("gh-aw.run.actor", actor),
    buildAttr("gh-aw.repository", repository),
  ];
  const runUrlAttr = buildGitHubActionsRunUrlAttribute(repository, runId);
  if (runUrlAttr) {
    attributes.push(runUrlAttr);
  }
  if (scopeVersion !== "unknown") {
    attributes.push(buildAttr("gh-aw.cli.version", scopeVersion));
  }

  if (engineId) {
    const genAiSystem = ENGINE_TO_SYSTEM_MAP[engineId] || engineId;
    attributes.push(buildAttr("gen_ai.system", genAiSystem));
    attributes.push(buildAttr("gh-aw.engine.id", engineId));
  }
  if (eventName) {
    attributes.push(buildAttr("gh-aw.event_name", eventName));
  }
  // Deployment state: prefer the env var (set from github.event.deployment_status.state
  // in the compiled workflow), fall back to aw_context propagation via awInfo.
  const deploymentStateSetup =
    process.env.GH_AW_GITHUB_EVENT_DEPLOYMENT_STATUS_STATE || (typeof awInfo.deployment_state === "string" ? awInfo.deployment_state : "") || (typeof awInfo.context?.deployment_state === "string" ? awInfo.context.deployment_state : "");
  if (deploymentStateSetup) {
    attributes.push(buildAttr("gh-aw.deployment.state", deploymentStateSetup));
  }
  // Workflow run conclusion: from aw_info or aw_context propagation.
  const workflowRunConclusion = (typeof awInfo.workflow_run_conclusion === "string" ? awInfo.workflow_run_conclusion : "") || (typeof awInfo.context?.workflow_run_conclusion === "string" ? awInfo.context.workflow_run_conclusion : "");
  if (workflowRunConclusion) {
    attributes.push(buildAttr("gh-aw.workflow_run.conclusion", workflowRunConclusion));
  }
  attributes.push(buildAttr("gh-aw.staged", staged));
  if (itemType) attributes.push(buildAttr("gh-aw.trigger.item_type", itemType));
  if (itemNumber) attributes.push(buildAttr("gh-aw.trigger.item_number", itemNumber));
  if (triggerLabel) attributes.push(buildAttr("gh-aw.trigger.label", triggerLabel));
  if (commentId) attributes.push(buildAttr("gh-aw.trigger.comment_id", commentId));
  if (frontmatterSource) attributes.push(buildAttr("gh-aw.frontmatter.source", frontmatterSource));
  if (frontmatterEmoji) attributes.push(buildAttr("gh-aw.frontmatter.emoji", frontmatterEmoji));
  if (typeof bodyModified === "boolean") attributes.push(buildAttr("gh-aw.frontmatter.body_modified", bodyModified));

  // Include experiment assignments so each span can be correlated with the
  // A/B variant selected for this run (written by pick_experiment.cjs).
  const experimentAssignments = readExperimentAssignments();
  attributes.push(...buildExperimentAttributes(experimentAssignments));
  attributes.push(...buildEpisodeAttributesFromContext(awInfo, runId, runAttempt));
  // Append user-defined custom attributes from observability.otlp.attributes.
  attributes.push(...buildCustomOTLPAttributes());

  const resourceAttributes = buildGitHubActionsResourceAttributes({
    repository,
    runId,
    eventName,
    ref,
    refName,
    headRef,
    sha,
    job,
    workflowRef,
    actorId,
    runnerOs,
    runnerArch,
    runnerName,
    runnerEnvironment,
    awfVersion,
    awmgVersion,
    staged,
    runAttempt,
  });

  const payload = buildOTLPPayload({
    traceId,
    spanId,
    ...(parentSpanId ? { parentSpanId } : {}),
    spanName: jobName ? `gh-aw.${jobName}.setup` : "gh-aw.job.setup",
    startMs,
    endMs,
    serviceName,
    scopeVersion,
    attributes,
    resourceAttributes,
  });

  // Always mirror to JSONL — the artifact is useful even without a live collector.
  appendToOTLPJSONL(payload);

  const endpoints = parseOTLPEndpoints();
  if (endpoints.length === 0) {
    return { traceId, spanId, parentSpanId };
  }

  // Pass skipJSONL: true so sendOTLPToAllEndpoints/sendOTLPSpan don't double-write the mirror.
  await sendOTLPToAllEndpoints(endpoints, payload, { skipJSONL: true });
  return { traceId, spanId, parentSpanId };
}

// ---------------------------------------------------------------------------
// Utilities for conclusion span
// ---------------------------------------------------------------------------

/**
 * Safely read and parse a JSON file.  Returns `null` on any error (missing
 * file, invalid JSON, permission denied, etc.).
 *
 * @param {string} filePath - Absolute path to the JSON file
 * @returns {any | null}
 */
function readJSONIfExists(filePath) {
  try {
    return JSON.parse(fs.readFileSync(filePath, "utf8"));
  } catch {
    return null;
  }
}

/**
 * Safely read and parse a JSONL file. Returns an empty array on any error.
 *
 * @param {string} filePath - Absolute path to the JSONL file
 * @returns {any[]}
 */
function readJSONLIfExists(filePath) {
  try {
    return parseJsonlContent(fs.readFileSync(filePath, "utf8"));
  } catch {
    return [];
  }
}

/**
 * Path to the GitHub rate-limit JSONL log file.
 * Mirrors GITHUB_RATE_LIMITS_JSONL_PATH from constants.cjs without introducing
 * a runtime require() dependency on that module.
 * @type {string}
 */
const GITHUB_RATE_LIMITS_JSONL_PATH = "/tmp/gh-aw/github_rate_limits.jsonl";

/**
 * Path to the persisted OTLP export error counter.
 * @type {string}
 */
const OTLP_EXPORT_ERRORS_PATH = "/tmp/gh-aw/otlp-export-errors.count";

/**
 * Path to the persisted OTLP export failure detail log.
 * @type {string}
 */
const OTLP_EXPORT_ERROR_DETAILS_PATH = "/tmp/gh-aw/otlp-export-errors.jsonl";
const AGENT_EXECUTION_EXIT_CODE_PATH = "/tmp/gh-aw/agent_execution_exit_code.txt";

/**
 * Path to the failure categories file written by handle_agent_failure and read
 * by the OTLP conclusion span so it can record them as gh-aw.failure.categories.
 * Mirrors FAILURE_CATEGORIES_PATH from handle_agent_failure.cjs without
 * introducing a runtime require() dependency on that module.
 * @type {string}
 */
const FAILURE_CATEGORIES_PATH = "/tmp/gh-aw/failure_categories.json";

/**
 * Path to the agent stdio log file.
 * @type {string}
 */
const AGENT_STDIO_LOG_PATH = "/tmp/gh-aw/agent-stdio.log";
const API_PROXY_EVENT_LOG_PATHS = [
  "/tmp/gh-aw/sandbox/firewall/logs/api-proxy-logs/event-logs.jsonl",
  "/tmp/gh-aw/sandbox/firewall/logs/api-proxy-logs/events.jsonl",
  "/tmp/gh-aw/sandbox/firewall-audit-logs/api-proxy-logs/event-logs.jsonl",
  "/tmp/gh-aw/sandbox/firewall-audit-logs/api-proxy-logs/events.jsonl",
];
const PERMISSION_DENIED_RE = /\b(?:permissions?\s+denied(?!\s+counts?\b)|EACCES|EPERM)\b/i;

/**
 * @typedef {Object} RateLimitEntry
 * @property {string} [resource]   - GitHub rate-limit resource category (e.g. "core", "graphql")
 * @property {number} [limit]      - Total request quota for the window
 * @property {number} [remaining]  - Requests remaining in the current window
 * @property {number} [used]       - Requests consumed in the current window
 * @property {string} [reset]      - ISO 8601 timestamp when the window resets
 * @property {string} [operation]  - API operation that produced this entry
 */

/**
 * Read the last entry from the GitHub rate-limit JSONL log file.
 * Returns the parsed entry or `null` when the file is absent, empty, or
 * contains no valid JSON lines.  Errors are silently swallowed — this is
 * an observability enrichment and must never break the workflow.
 *
 * @returns {RateLimitEntry | null}
 */
function readLastRateLimitEntry() {
  try {
    const content = fs.readFileSync(GITHUB_RATE_LIMITS_JSONL_PATH, "utf8");
    const entries = parseJsonlContent(content);
    if (entries.length === 0) return null;
    const last = entries[entries.length - 1];
    // Rate-limit enrichment requires object entries (not primitive values or
    // arrays) so the expected fields can be extracted as attributes.
    return last && typeof last === "object" && !Array.isArray(last) ? last : null;
  } catch {
    return null;
  }
}

/**
 * Read the persisted OTLP export error count.
 *
 * @returns {number}
 */
function readOTLPExportErrorCount() {
  try {
    const raw = fs.readFileSync(OTLP_EXPORT_ERRORS_PATH, "utf8").trim();
    const parsed = parseInt(raw, 10);
    return Number.isInteger(parsed) && parsed >= 0 ? parsed : 0;
  } catch {
    return 0;
  }
}

/**
 * Read an integer value from a file.
 *
 * @param {string} filePath
 * @returns {number | null}
 */
function readIntegerIfExists(filePath) {
  try {
    const raw = fs.readFileSync(filePath, "utf8").trim();
    if (!raw) {
      return null;
    }
    const parsed = parseInt(raw, 10);
    return Number.isInteger(parsed) ? parsed : null;
  } catch {
    return null;
  }
}

/**
 * Extract a collector host label from an OTLP endpoint URL.
 *
 * @param {string} endpoint
 * @returns {string}
 */
function getOTLPExportErrorHost(endpoint) {
  try {
    const parsed = new URL(endpoint);
    return parsed.host || endpoint;
  } catch {
    return endpoint.replace(/^[a-z]+:\/\//i, "").replace(/\/.*$/, "") || endpoint;
  }
}

/**
 * @param {unknown} status
 * @returns {status is number}
 */
function isValidOTLPExportErrorStatus(status) {
  return typeof status === "number" && Number.isInteger(status) && status > 0;
}

/**
 * @param {unknown} entry
 * @returns {entry is OTLPExportErrorDetail}
 */
function isValidOTLPExportErrorDetail(entry) {
  if (entry === null || typeof entry !== "object" || Array.isArray(entry)) {
    return false;
  }
  const candidate = /** @type {Record<string, unknown>} */ entry;
  return typeof candidate["host"] === "string" && candidate["host"].trim() !== "" && typeof candidate["reason"] === "string" && candidate["reason"].trim() !== "";
}

/**
 * @param {OTLPExportErrorDetail} detail
 * @returns {OTLPExportErrorDetail}
 */
function normalizeOTLPExportErrorDetail(detail) {
  return {
    host: detail.host.trim(),
    ...(isValidOTLPExportErrorStatus(detail.status) ? { status: detail.status } : {}),
    reason: sanitizeOTLPExportErrorReason(detail.reason),
  };
}

/**
 * Read persisted OTLP export failure details.
 *
 * @returns {OTLPExportErrorDetail[]}
 */
function readOTLPExportErrorDetails() {
  try {
    const content = fs.readFileSync(OTLP_EXPORT_ERROR_DETAILS_PATH, "utf8");
    /** @type {OTLPExportErrorDetail[]} */
    const details = [];
    for (const parsed of parseJsonlContent(content)) {
      if (isValidOTLPExportErrorDetail(parsed)) {
        details.push(normalizeOTLPExportErrorDetail(parsed));
      }
    }
    return details;
  } catch {
    return [];
  }
}

/**
 * @param {OTLPExportErrorDetail} detail
 * @returns {string}
 */
function formatOTLPExportErrorDetail(detail) {
  return `${detail.host}${isValidOTLPExportErrorStatus(detail.status) ? ` status=${detail.status}` : ""} reason=${detail.reason}`;
}

/**
 * Format persisted OTLP export failure details for a conclusion span attribute.
 *
 * @returns {string}
 */
function formatOTLPExportErrorDetails() {
  const details = readOTLPExportErrorDetails();
  if (details.length === 0) {
    return "";
  }
  const formattedDetails = details.map(formatOTLPExportErrorDetail);
  let summary = "";

  for (let i = 0; i < formattedDetails.length; i++) {
    const entry = formattedDetails[i];
    const candidate = summary ? `${summary} | ${entry}` : entry;
    if (candidate.length <= MAX_ATTR_VALUE_LENGTH) {
      summary = candidate;
      continue;
    }

    const remaining = formattedDetails.length - i;
    const suffix = summary && remaining > 0 ? ` | … (+${remaining} more)` : "";
    if (summary && summary.length + suffix.length <= MAX_ATTR_VALUE_LENGTH) {
      return summary + suffix;
    }

    if (!summary) {
      const prefix = `${details[i].host}${isValidOTLPExportErrorStatus(details[i].status) ? ` status=${details[i].status}` : ""} reason=`;
      const available = Math.max(0, MAX_ATTR_VALUE_LENGTH - prefix.length - 1);
      const truncatedReason = available > 0 ? `${details[i].reason.slice(0, available)}…` : "…";
      return `${prefix}${truncatedReason}`;
    }

    return summary;
  }

  return summary;
}

/**
 * Persist one additional OTLP export failure.
 *
 * @param {{ endpoint?: string, status?: number, reason?: string }} [detail]
 * @returns {void}
 */
function recordOTLPExportError(detail = {}) {
  try {
    fs.mkdirSync("/tmp/gh-aw", { recursive: true });
    fs.writeFileSync(OTLP_EXPORT_ERRORS_PATH, String(readOTLPExportErrorCount() + 1));
  } catch {
    // Export-health tracking is best-effort only.
  }
  if (!detail.endpoint || typeof detail.reason !== "string" || detail.reason.trim() === "") {
    return;
  }
  try {
    fs.mkdirSync("/tmp/gh-aw", { recursive: true });
    fs.appendFileSync(
      OTLP_EXPORT_ERROR_DETAILS_PATH,
      JSON.stringify(
        normalizeOTLPExportErrorDetail({
          host: getOTLPExportErrorHost(detail.endpoint),
          ...(isValidOTLPExportErrorStatus(detail.status) ? { status: detail.status } : {}),
          reason: detail.reason,
        })
      ) + "\n"
    );
  } catch {
    // Export-health tracking is best-effort only.
  }
}

/**
 * Normalize agent output errors into a single message string.
 *
 * @param {unknown} errorEntry
 * @returns {string}
 */
function getErrorMessage(errorEntry) {
  if (typeof errorEntry === "string") {
    return errorEntry.slice(0, MAX_ATTR_VALUE_LENGTH);
  }
  if (!errorEntry || typeof errorEntry !== "object" || Array.isArray(errorEntry)) {
    return "";
  }

  const normalizedError = /** @type {Record<string, unknown>} */ errorEntry;
  const rawType = normalizedError["type"];
  const rawMessage = normalizedError["message"];
  const rawError = normalizedError["error"];
  const type = typeof rawType === "string" ? rawType.trim() : "";
  const message = typeof rawMessage === "string" ? rawMessage.trim() : typeof rawError === "string" ? rawError.trim() : "";

  if (type && message) {
    return `${type}:${message}`.slice(0, MAX_ATTR_VALUE_LENGTH);
  }
  return message.slice(0, MAX_ATTR_VALUE_LENGTH);
}

/**
 * @typedef {Object} AgentRuntimeMetrics
 * @property {number | undefined} turns
 * @property {string | undefined} stopReason
 * @property {string | undefined} resolvedModel
 * @property {{input_tokens?: number, output_tokens?: number, cache_read_tokens?: number, cache_write_tokens?: number, ai_credits?: number} | undefined} tokenUsage
 * @property {number} warningCount
 * @property {number} permissionDeniedCount
 * @property {number} steeringEventCount
 */

/**
 * Normalize a non-negative numeric counter or cost value.
 *
 * @param {unknown} rawValue
 * @returns {number | undefined}
 */
function normalizeNonNegativeNumber(rawValue) {
  if (typeof rawValue === "number" && Number.isFinite(rawValue) && rawValue >= 0) {
    return rawValue;
  }
  if (typeof rawValue === "string" && rawValue.trim()) {
    const parsed = Number(rawValue.trim());
    if (Number.isFinite(parsed) && parsed >= 0) {
      return parsed;
    }
  }
  return undefined;
}

/**
 * Normalize token usage counters from an engine result event usage block.
 *
 * @param {unknown} rawUsage
 * @returns {{input_tokens?: number, output_tokens?: number, cache_read_tokens?: number, cache_write_tokens?: number, ai_credits?: number} | undefined}
 */
function normalizeRuntimeTokenUsage(rawUsage) {
  if (!rawUsage || typeof rawUsage !== "object" || Array.isArray(rawUsage)) {
    return undefined;
  }

  /** @type {{input_tokens?: number, output_tokens?: number, cache_read_tokens?: number, cache_write_tokens?: number, cache_read_input_tokens?: number, cache_creation_input_tokens?: number, ai_credits?: number}} */
  const usage = rawUsage;
  /** @type {{input_tokens?: number, output_tokens?: number, cache_read_tokens?: number, cache_write_tokens?: number, ai_credits?: number}} */
  const normalized = {};

  const inputTokens = normalizeNonNegativeNumber(usage.input_tokens);
  if (typeof inputTokens === "number") {
    normalized.input_tokens = inputTokens;
  }
  const outputTokens = normalizeNonNegativeNumber(usage.output_tokens);
  if (typeof outputTokens === "number") {
    normalized.output_tokens = outputTokens;
  }

  const cacheReadTokens = normalizeNonNegativeNumber(usage.cache_read_tokens) ?? normalizeNonNegativeNumber(usage.cache_read_input_tokens);
  if (typeof cacheReadTokens === "number") {
    normalized.cache_read_tokens = cacheReadTokens;
  }

  const cacheWriteTokens = normalizeNonNegativeNumber(usage.cache_write_tokens) ?? normalizeNonNegativeNumber(usage.cache_creation_input_tokens);
  if (typeof cacheWriteTokens === "number") {
    normalized.cache_write_tokens = cacheWriteTokens;
  }

  const aiCredits = normalizeNonNegativeNumber(usage.ai_credits);
  if (typeof aiCredits === "number") {
    normalized.ai_credits = aiCredits;
  }

  return Object.keys(normalized).length > 0 ? normalized : undefined;
}

/**
 * Read steering-event count from the first available API proxy event-log JSONL.
 *
 * Uses first-available-wins semantics: the first non-empty file encountered is
 * the single source of truth for this run. Multiple paths cover runtime vs
 * audit sandbox layouts. If the file exists but has no steering events, 0 is
 * returned without checking later paths.
 *
 * @returns {number}
 */
function readApiProxySteeringEventCount() {
  for (const filePath of API_PROXY_EVENT_LOG_PATHS) {
    try {
      const stat = fs.statSync(filePath);
      if (!stat || stat.size <= 0) {
        continue;
      }
      const content = fs.readFileSync(filePath, "utf8");
      return countSteeringEventsInApiProxyJsonl(content);
    } catch {
      // Ignore missing/unreadable files and fall through to the next path.
    }
  }
  return 0;
}

/**
 * Read turns, token usage, and warning volume from agent-stdio.log.
 *
 * @returns {AgentRuntimeMetrics}
 */
function readAgentRuntimeMetrics() {
  /** @type {AgentRuntimeMetrics} */
  const metrics = { turns: undefined, stopReason: undefined, resolvedModel: undefined, tokenUsage: undefined, warningCount: 0, permissionDeniedCount: 0, steeringEventCount: readApiProxySteeringEventCount() };
  let fallbackPermissionDeniedCount = 0;
  let hasStructuredPermissionDeniedCount = false;

  try {
    const content = fs.readFileSync(AGENT_STDIO_LOG_PATH, "utf8");
    const lines = content.split("\n");

    const applyRuntimeEntry = parsed => {
      if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
        return;
      }

      // Engine logs normalize init events to either:
      // - { type: "system", subtype: "init", ... } (Claude/Copilot-style entries)
      // - { type: "init", ... } (some normalized parsers/custom engines)
      if ((parsed.type === "system" && parsed.subtype === "init") || parsed.type === "init") {
        if (typeof parsed.model === "string" && parsed.model.trim()) {
          metrics.resolvedModel = parsed.model.trim();
        }
      }

      if (parsed.type !== "result") {
        return;
      }

      if (typeof parsed.num_turns === "number" && parsed.num_turns >= 0) {
        metrics.turns = parsed.num_turns;
      }
      if (typeof parsed.stop_reason === "string" && parsed.stop_reason) {
        metrics.stopReason = parsed.stop_reason;
      }
      const tokenUsage = normalizeRuntimeTokenUsage(parsed.usage);
      if (tokenUsage) {
        metrics.tokenUsage = tokenUsage;
      }
      if (Array.isArray(parsed.permission_denials)) {
        metrics.permissionDeniedCount = Math.max(metrics.permissionDeniedCount, parsed.permission_denials.length);
        hasStructuredPermissionDeniedCount = true;
      } else if (typeof parsed.permission_denied_count === "number" && Number.isFinite(parsed.permission_denied_count) && parsed.permission_denied_count >= 0) {
        metrics.permissionDeniedCount = Math.max(metrics.permissionDeniedCount, parsed.permission_denied_count);
        hasStructuredPermissionDeniedCount = true;
      }
    };

    for (const rawLine of lines) {
      const line = rawLine.trim();
      if (!line) {
        continue;
      }

      if (/^(?:\[WARN\]|npm warn\b)/i.test(line)) {
        metrics.warningCount += 1;
      }
      if (PERMISSION_DENIED_RE.test(line)) {
        fallbackPermissionDeniedCount += 1;
      }

      const jsonObjectStart = line.indexOf("{");
      const jsonArrayStart = line.indexOf("[{");
      let jsonStart = -1;
      if (jsonObjectStart >= 0 && jsonArrayStart >= 0) {
        jsonStart = Math.min(jsonObjectStart, jsonArrayStart);
      } else if (jsonObjectStart >= 0) {
        jsonStart = jsonObjectStart;
      } else {
        jsonStart = jsonArrayStart;
      }
      if (jsonStart < 0) {
        continue;
      }

      try {
        const parsed = JSON.parse(line.slice(jsonStart));
        if (Array.isArray(parsed)) {
          for (const entry of parsed) {
            applyRuntimeEntry(entry);
          }
          continue;
        }
        applyRuntimeEntry(parsed);
      } catch {
        // Ignore non-JSON and truncated log lines.
      }
    }
  } catch {
    return metrics;
  }

  if (!hasStructuredPermissionDeniedCount) {
    metrics.permissionDeniedCount = fallbackPermissionDeniedCount;
  }

  return metrics;
}

// ---------------------------------------------------------------------------
// High-level: job conclusion span
// ---------------------------------------------------------------------------

/**
 * Send a conclusion span for a job to the configured OTLP endpoint.  Called
 * from the action post step so it runs at the end of every job that uses the
 * setup action.  The span carries workflow metadata read from `aw_info.json`
 * and AI credit totals from `GH_AW_AIC`.
 *
 * The span payload is always built and mirrored to the local JSONL file so
 * that it can be inspected via GitHub Actions artifacts without needing a live
 * collector.  The HTTP export to the OTLP endpoint is skipped when
 * `OTEL_EXPORTER_OTLP_ENDPOINT` is not set.  All errors are surfaced as
 * `console.warn` messages and never re-thrown.
 *
 * Environment variables consumed:
 * - `OTEL_EXPORTER_OTLP_ENDPOINT`  – collector endpoint
 * - `OTEL_SERVICE_NAME`             – service name (defaults to "gh-aw")
 * - `GH_AW_AIC`                     – total AI Credits for the run
 * - `GH_AW_AGENT_CONCLUSION`        – agent job result ("success", "failure", "timed_out",
 *                                     "cancelled", "skipped"); when "failure" or "timed_out"
 *                                     the span status is set to STATUS_CODE_ERROR (2)
 * - `GH_AW_DETECTION_CONCLUSION`   – threat-detection scan outcome ("success", "warning",
 *                                     "failure", "skipped"); emitted as
 *                                     `gh-aw.detection.conclusion` when present.
 *                                     Set via GITHUB_ENV by the detection job itself
 *                                     (parse_threat_detection_results.cjs) so the
 *                                     detection conclusion span reports the job's own
 *                                     result; also injected from
 *                                     needs.detection.outputs.* in downstream jobs.
 * - `GH_AW_DETECTION_REASON`       – machine-readable reason for the detection conclusion
 *                                     (e.g. "threat_detected", "agent_failure"); emitted as
 *                                     `gh-aw.detection.reason` when present
 * - `INPUT_JOB_NAME`               – job name; set automatically by GitHub Actions from the
 *                                     `job-name` action input
 * - `GITHUB_AW_OTEL_TRACE_ID`      – trace ID written to GITHUB_ENV by the setup step;
 *                                     enables 1-trace-per-run when present
 * - `GITHUB_AW_OTEL_PARENT_SPAN_ID` – setup span ID written to GITHUB_ENV by the setup step;
 *                                     links this span as a child of the job setup span
 * - `GITHUB_RUN_ID`                 – GitHub Actions run ID
 * - `GITHUB_ACTOR`                  – GitHub Actions actor
 * - `GITHUB_REPOSITORY`             – `owner/repo` string
 *
 * Runtime files read:
 * - `/tmp/gh-aw/aw_info.json`    – workflow/engine metadata written by the agent job;
 *                                   `context.item_type`, `context.item_number`, and
 *                                   `context.trigger_label` are emitted as
 *                                   `gh-aw.trigger.item_type`, `gh-aw.trigger.item_number`,
 *                                   and `gh-aw.trigger.label` attributes so every span can
 *                                   be linked back to the GitHub item that triggered the workflow
 * - `/tmp/gh-aw/agent_usage.json` – per-type token breakdown written by parse_token_usage.cjs;
 *                                    provides `input_tokens`, `output_tokens`,
 *                                    `cache_read_tokens`, and `cache_write_tokens` counters
 * - `/tmp/gh-aw/agent/graders/grader_results.json` – deterministic grader
 *                                    summary attributes and per-result span events
 *
 * @param {string} spanName - OTLP span name (e.g. `"gh-aw.job.conclusion"`)
 * @param {{ startMs?: number }} [options]
 * @returns {Promise<void>}
 */
async function sendJobConclusionSpan(spanName, options = {}) {
  const startMs = options.startMs ?? nowMs();
  const endMs = nowMs();

  // Read workflow metadata from aw_info.json (written by the agent job setup step).
  const awInfo = readJSONIfExists("/tmp/gh-aw/aw_info.json") || {};

  const serviceName = process.env.OTEL_SERVICE_NAME || "gh-aw";
  const version = (typeof awInfo.cli_version === "string" ? awInfo.cli_version : "") || process.env.GH_AW_INFO_CLI_VERSION || awInfo.agent_version || awInfo.version || process.env.GH_AW_INFO_VERSION || process.env.GITHUB_SHA || "unknown";

  // Prefer GITHUB_AW_OTEL_TRACE_ID (written to GITHUB_ENV by this job's setup step) so
  // all spans in the same job share one trace.  Fall back to aw_context.otel_trace_id
  // for cross-job correlation, then try the legacy workflow_call_id fallback.
  const envTraceId = (process.env.GITHUB_AW_OTEL_TRACE_ID || "").trim().toLowerCase();
  const inheritedTraceId = readContextString(awInfo.context?.otel_trace_id).toLowerCase();
  const awTraceId = typeof awInfo.context?.workflow_call_id === "string" ? awInfo.context.workflow_call_id.replace(/-/g, "") : "";
  let traceId = generateTraceId();
  if (isValidTraceId(envTraceId)) {
    traceId = envTraceId;
  } else if (isValidTraceId(inheritedTraceId)) {
    traceId = inheritedTraceId;
  } else if (awTraceId && isValidTraceId(awTraceId)) {
    traceId = awTraceId;
  }

  // Use GITHUB_AW_OTEL_PARENT_SPAN_ID (written to GITHUB_ENV by this job's setup step) so
  // conclusion spans are linked as children of the setup span (1 parent span per job).
  const rawParentSpanId = (process.env.GITHUB_AW_OTEL_PARENT_SPAN_ID || "").trim().toLowerCase();
  const parentSpanId = isValidSpanId(rawParentSpanId) ? rawParentSpanId : "";

  const workflowName = awInfo.workflow_name || process.env.GH_AW_INFO_WORKFLOW_NAME || process.env.GITHUB_WORKFLOW || "";
  const engineId = resolveEngineId(awInfo);
  const model = awInfo.model || "";
  const staged = awInfo.staged === true;
  const itemType = typeof awInfo.context?.item_type === "string" ? awInfo.context.item_type : "";
  const itemNumber = typeof awInfo.context?.item_number === "string" ? awInfo.context.item_number : "";
  const triggerLabel = typeof awInfo.context?.trigger_label === "string" ? awInfo.context.trigger_label : "";
  const commentId = typeof awInfo.context?.comment_id === "string" ? awInfo.context.comment_id : "";
  const frontmatterSource = (typeof awInfo.frontmatter_source === "string" ? awInfo.frontmatter_source : "") || process.env.GH_AW_INFO_FRONTMATTER_SOURCE || "";
  const frontmatterEmoji = (typeof awInfo.frontmatter_emoji === "string" ? awInfo.frontmatter_emoji : "") || process.env.GH_AW_INFO_FRONTMATTER_EMOJI || "";
  const awfVersion = (typeof awInfo.awf_version === "string" ? awInfo.awf_version : "") || process.env.GH_AW_INFO_AWF_VERSION || "";
  const awmgVersion = (typeof awInfo.awmg_version === "string" ? awInfo.awmg_version : "") || process.env.GH_AW_INFO_AWMG_VERSION || "";
  const bodyModified = typeof awInfo.body_modified === "boolean" ? awInfo.body_modified : parseBooleanEnv(process.env.GH_AW_INFO_BODY_MODIFIED);
  const trackerId = process.env.GH_AW_TRACKER_ID || awInfo.tracker_id || "";
  const jobName = resolveConclusionJobName(spanName);
  const jobEmitsOwnTokenUsage = jobName === "agent" || jobName === "detection" || (!!engineId && jobName === engineId);
  const runId = process.env.GITHUB_RUN_ID || "";
  const runAttempt = awInfo.run_attempt || process.env.GITHUB_RUN_ATTEMPT || "1";
  const actor = process.env.GITHUB_ACTOR || "";
  const repository = process.env.GITHUB_REPOSITORY || "";
  const eventName = process.env.GITHUB_EVENT_NAME || "";
  const ref = process.env.GITHUB_REF || "";
  const refName = process.env.GITHUB_REF_NAME || "";
  const headRef = process.env.GITHUB_HEAD_REF || "";
  const sha = process.env.GITHUB_SHA || "";
  const job = process.env.GITHUB_JOB || "";
  const workflowRef = process.env.GITHUB_WORKFLOW_REF || "";
  const actorId = process.env.GITHUB_ACTOR_ID || "";
  const runnerOs = process.env.RUNNER_OS || "";
  const runnerArch = process.env.RUNNER_ARCH || "";
  const runnerName = process.env.RUNNER_NAME || "";
  const runnerEnvironment = process.env.RUNNER_ENVIRONMENT || "";

  // Agent conclusion is passed to downstream jobs via GH_AW_AGENT_CONCLUSION.
  // Values: "success", "failure", "timed_out", "cancelled", "skipped".
  const agentConclusion = process.env.GH_AW_AGENT_CONCLUSION || "";

  // Detection conclusion and reason: set via GITHUB_ENV by parse_threat_detection_results.cjs
  // so the detection job's own span includes its result; also injected from
  // needs.detection.outputs.* in downstream jobs (conclusion, safe_outputs, etc.).
  const detectionConclusion = process.env.GH_AW_DETECTION_CONCLUSION || "";
  const detectionReason = process.env.GH_AW_DETECTION_REASON || "";
  const runtimeMetrics = readAgentRuntimeMetrics();
  // Read once and reuse for both gh-aw.aic and gen_ai.usage.* attributes.
  const agentUsageFilePath = "/tmp/gh-aw/agent_usage.json";
  const agentUsageRaw = readJSONIfExists(agentUsageFilePath);
  const agentUsageNormalized = normalizeRuntimeTokenUsage(agentUsageRaw);
  const agentUsage = agentUsageNormalized || runtimeMetrics.tokenUsage || {};
  // Mark the span as an error when the agent job failed, timed out, or was cancelled.
  const isAgentTimedOut = agentConclusion === "timed_out";
  const isAgentFailure = agentConclusion === "failure" || isAgentTimedOut;
  const isAgentCancelled = agentConclusion === "cancelled";
  const isAgentNonOK = isAgentFailure || isAgentCancelled;
  // STATUS_CODE_ERROR = 2, STATUS_CODE_OK = 1
  let statusCode = isAgentNonOK ? 2 : 1;
  let statusMessage;
  if (isAgentFailure) {
    statusMessage = `agent ${agentConclusion}`;
  } else if (isAgentCancelled) {
    statusMessage = "agent cancelled";
  }

  // Always read agent_output.json so output metrics are available on all outcomes.
  const rawAgentOutput = readJSONIfExists("/tmp/gh-aw/agent_output.json");
  const agentOutput = rawAgentOutput || {};
  // readJSONIfExists returns null when the file is absent OR unreadable (e.g. partial/corrupt write).
  const hasNoReadableAgentOutput = rawAgentOutput === null;
  const outputErrors = Array.isArray(agentOutput.errors) ? agentOutput.errors : [];
  const outputItems = Array.isArray(agentOutput.items) ? agentOutput.items : [];
  const errorMessages = outputErrors.map(getErrorMessage).filter(Boolean).slice(0, 5);
  const agentExecutionExitCode = readIntegerIfExists(AGENT_EXECUTION_EXIT_CODE_PATH);
  const hasAgentExecutionFailureExitCode = Number.isInteger(agentExecutionExitCode) && agentExecutionExitCode !== 0;
  const warningCount = runtimeMetrics.warningCount + (detectionConclusion === "warning" ? 1 : 0);
  const workflowRunConclusion = (typeof awInfo.workflow_run_conclusion === "string" ? awInfo.workflow_run_conclusion : "") || (typeof awInfo.context?.workflow_run_conclusion === "string" ? awInfo.context.workflow_run_conclusion : "");

  let runStatus = "success";
  const rawRunStatus = agentConclusion || workflowRunConclusion;
  if (rawRunStatus === "cancelled") {
    runStatus = "cancelled";
  } else if (rawRunStatus === "timed_out") {
    // Keep timeout distinguishable from generic failure so it can be queried separately.
    // OTLP status.code remains ERROR (2) — timeouts are still failures in the OTLP sense.
    runStatus = "timeout";
  } else if (rawRunStatus === "failure") {
    runStatus = "failure";
  }

  // When GH_AW_AGENT_CONCLUSION and workflowRunConclusion are both absent (e.g. in the
  // agent job's own post-step where needs.<job>.result is not yet visible), fall back to
  // observable failure evidence so gh-aw.run.status and status.code are accurate.
  if (!rawRunStatus && hasAgentExecutionFailureExitCode) {
    runStatus = "failure";
    statusCode = 2;
    statusMessage = `copilot cli exited with code ${agentExecutionExitCode}`;
  } else if (!rawRunStatus && outputErrors.length > 0) {
    runStatus = "failure";
    statusCode = 2;
    statusMessage = (errorMessages.length > 0 ? `errors detected: ${errorMessages[0]}` : "errors detected").slice(0, 256);
  }

  if (isAgentFailure && errorMessages.length > 0) {
    statusMessage = `agent ${agentConclusion}: ${errorMessages[0]}`.slice(0, 256);
  }

  const attributes = [buildAttr("gh-aw.workflow.name", workflowName), buildAttr("gh-aw.run.id", runId), buildAttr("gh-aw.run.attempt", runAttempt), buildAttr("gh-aw.run.actor", actor), buildAttr("gh-aw.repository", repository)];
  const runUrlAttr = buildGitHubActionsRunUrlAttribute(repository, runId);
  if (runUrlAttr) {
    attributes.push(runUrlAttr);
  }
  if (version !== "unknown") {
    attributes.push(buildAttr("gh-aw.cli.version", version));
  }
  attributes.push(buildAttr("gh-aw.run.status", runStatus));
  attributes.push(buildAttr("gh-aw.error_count", outputErrors.length));
  attributes.push(buildAttr("gh-aw.warning_count", warningCount));
  if (typeof agentExecutionExitCode === "number" && Number.isInteger(agentExecutionExitCode)) {
    attributes.push(buildAttr("gh-aw.agent.execution.exit_code", agentExecutionExitCode));
  }
  attributes.push(buildAttr("gh-aw.permission_denied_count", runtimeMetrics.permissionDeniedCount));
  attributes.push(buildAttr("gh-aw.steering_event_count", runtimeMetrics.steeringEventCount));
  attributes.push(buildAttr("gh-aw.action_minutes", Math.max(0, endMs - startMs) / 60000));

  // The compact usage summary is assembled by the conclusion job before its
  // setup action post-step emits this run-level span.
  if (jobName === "conclusion") {
    const usageActivity = readJSONIfExists("/tmp/gh-aw/usage/activity/summary.json");
    for (const [attributeName, sectionName, fieldName] of [
      ["gh-aw.integrity.filtered_events", "integrity", "total_filtered"],
      ["gh-aw.firewall.requests", "firewall", "total_requests"],
      ["gh-aw.firewall.allowed_requests", "firewall", "allowed_requests"],
      ["gh-aw.firewall.blocked_requests", "firewall", "blocked_requests"],
      ["gh-aw.mcp.tool_calls", "gateway", "total_calls"],
      ["gh-aw.mcp.failed_tool_calls", "gateway", "failed_calls"],
      ["gh-aw.mcp.input_bytes", "gateway", "total_input_size"],
      ["gh-aw.mcp.max_input_bytes", "gateway", "max_input_size"],
      ["gh-aw.mcp.output_bytes", "gateway", "total_output_size"],
      ["gh-aw.mcp.max_output_bytes", "gateway", "max_output_size"],
    ]) {
      const value = usageActivity?.[sectionName]?.[fieldName];
      if (typeof value === "number" && Number.isFinite(value) && value >= 0) {
        attributes.push(buildAttr(attributeName, value));
      }
    }
    const workingSet = usageActivity?.working_set;
    const measurementState = workingSet?.measurement_state;
    if (measurementState === "measured" || measurementState === "partial" || measurementState === "unavailable") {
      attributes.push(buildAttr("gh-aw.working_set.measurement_state", measurementState));
      if (measurementState !== "unavailable") {
        if (typeof workingSet.rebuild_factor === "number" && Number.isFinite(workingSet.rebuild_factor)) {
          attributes.push(buildDoubleAttr("gh-aw.working_set.rebuild_factor", workingSet.rebuild_factor));
        }
        for (const [attributeName, fieldName] of [
          ["gh-aw.working_set.cumulative_input_tokens", "cumulative_input_tokens"],
          ["gh-aw.working_set.peak_input_tokens", "peak_input_tokens"],
          ["gh-aw.working_set.rebuild_excess_tokens", "rebuild_excess_tokens"],
          ["gh-aw.working_set.invocations", "invocations"],
        ]) {
          const value = workingSet[fieldName];
          // Use the same finite-number acceptance as buildAttr's own encoding
          // logic (rather than Number.isSafeInteger) so that legitimate large
          // token counters from high-volume runs are still emitted -
          // buildAttr already falls back to doubleValue for values that
          // exceed safe-integer precision instead of dropping them.
          if (typeof value === "number" && Number.isFinite(value) && value >= 0) {
            attributes.push(buildAttr(attributeName, value));
          }
        }
      }
    }
  }

  if (jobName) attributes.push(buildAttr("gh-aw.job.name", jobName));
  if (engineId) {
    const genAiSystem = ENGINE_TO_SYSTEM_MAP[engineId] || engineId;
    attributes.push(buildAttr("gen_ai.system", genAiSystem));
    attributes.push(buildAttr("gh-aw.engine.id", engineId));
  }
  if (model) attributes.push(buildAttr("gen_ai.request.model", model));
  if (trackerId) attributes.push(buildAttr("gh-aw.tracker.id", trackerId));
  if (eventName) attributes.push(buildAttr("gh-aw.event_name", eventName));
  // Deployment state: prefer the env var (set from github.event.deployment_status.state
  // in the compiled workflow), fall back to aw_info.deployment_state or aw_context propagation.
  const deploymentStateConclusion =
    process.env.GH_AW_GITHUB_EVENT_DEPLOYMENT_STATUS_STATE || (typeof awInfo.deployment_state === "string" ? awInfo.deployment_state : "") || (typeof awInfo.context?.deployment_state === "string" ? awInfo.context.deployment_state : "");
  if (deploymentStateConclusion) {
    attributes.push(buildAttr("gh-aw.deployment.state", deploymentStateConclusion));
  }
  if (workflowRunConclusion) {
    attributes.push(buildAttr("gh-aw.workflow_run.conclusion", workflowRunConclusion));
  }
  attributes.push(buildAttr("gh-aw.staged", staged));
  if (itemType) attributes.push(buildAttr("gh-aw.trigger.item_type", itemType));
  if (itemNumber) attributes.push(buildAttr("gh-aw.trigger.item_number", itemNumber));
  if (triggerLabel) attributes.push(buildAttr("gh-aw.trigger.label", triggerLabel));
  if (commentId) attributes.push(buildAttr("gh-aw.trigger.comment_id", commentId));
  if (frontmatterSource) attributes.push(buildAttr("gh-aw.frontmatter.source", frontmatterSource));
  if (frontmatterEmoji) attributes.push(buildAttr("gh-aw.frontmatter.emoji", frontmatterEmoji));
  if (typeof bodyModified === "boolean") attributes.push(buildAttr("gh-aw.frontmatter.body_modified", bodyModified));
  attributes.push(...buildEpisodeAttributesFromContext(awInfo, runId, runAttempt));
  // GH_AW_AIC may be propagated to downstream jobs via workflow outputs, so gate it
  // behind jobEmitsOwnTokenUsage to prevent non-owning jobs from re-emitting it.
  // Ranked AIC sources: env var → non-zero file → engine metrics → file (may be zero).
  // When the firewall proxy writes ai_credits=0 to agent_usage.json, the engine result
  // event (from agent-stdio.log) is tried next so its non-zero value is not lost.
  // For jobs that own token usage (agent, detection, engine), gh-aw.aic is ALWAYS
  // emitted as a numeric attribute — defaulting to 0 when no data is available.
  // This guarantees Sentry EAP infers the field as numeric (not string) so that
  // sum()/avg()/percentile() aggregations work without manual schema configuration,
  // and Tempo indexes it so { span."gh-aw.aic" > 0 } is queryable immediately.
  const aiCreditsFromEnv = normalizeNonNegativeNumber(process.env.GH_AW_AIC);
  const aiCreditsFromFile = agentUsage.ai_credits;
  const aiCreditsFromMetrics = runtimeMetrics.tokenUsage?.ai_credits;
  const aiCredits = jobEmitsOwnTokenUsage ? (aiCreditsFromEnv ?? ((aiCreditsFromFile ?? 0) > 0 ? aiCreditsFromFile : (aiCreditsFromMetrics ?? aiCreditsFromFile ?? 0))) : undefined;
  if (typeof aiCredits === "number") {
    // Always encode gh-aw.aic as doubleValue (not intValue) so that Sentry EAP
    // infers the field schema as float on first emission, enabling sum()/avg()/
    // percentile() aggregations even when the value is 0 (an integer in JS).
    attributes.push(buildDoubleAttr("gh-aw.aic", aiCredits));
  }
  if (typeof runtimeMetrics.turns === "number") {
    attributes.push(buildAttr("gh-aw.turns", runtimeMetrics.turns));
  }
  if (jobName === "agent") {
    // Emit OTel GenAI semantic attributes on agent conclusion spans even when the
    // dedicated agent sub-span is missing, so LLM activity remains discoverable.
    attributes.push(buildAttr("gen_ai.operation.name", "chat"));
    if (workflowName) attributes.push(buildAttr("gen_ai.workflow.name", workflowName));
    if (runtimeMetrics.resolvedModel) attributes.push(buildAttr("gen_ai.response.model", runtimeMetrics.resolvedModel));
    // Use the engine-reported stop_reason when available; fall back to "timeout" when
    // the agent was killed before it could write a result entry to agent-stdio.log;
    // use "unknown" as a sentinel for engines (e.g. copilot, codex) that do not emit
    // a result entry at all, so that gen_ai.response.finish_reasons is always present
    // and length-truncation is always queryable in Sentry/dashboards.
    const effectiveStopReason = runtimeMetrics.stopReason || (isAgentTimedOut ? "timeout" : "unknown");
    attributes.push(buildArrayAttr("gen_ai.response.finish_reasons", [effectiveStopReason]));
  }

  if (agentConclusion) {
    attributes.push(buildAttr("gh-aw.agent.conclusion", agentConclusion));
  }
  if (detectionConclusion) {
    attributes.push(buildAttr("gh-aw.detection.conclusion", detectionConclusion));
  }
  if (detectionReason) {
    attributes.push(buildAttr("gh-aw.detection.reason", detectionReason));
  }
  // Read failure categories written by handle_agent_failure so the reason/type
  // of each failure issue is captured as a queryable OTLP attribute.
  const rawFailureCategories = readJSONIfExists(FAILURE_CATEGORIES_PATH);
  const failureCategories = Array.isArray(rawFailureCategories) ? rawFailureCategories.filter(c => typeof c === "string" && c) : [];
  if (failureCategories.length > 0) {
    attributes.push(buildArrayAttr("gh-aw.failure.categories", failureCategories));
  }
  attributes.push(buildAttr("gh-aw.otlp.export_errors", readOTLPExportErrorCount()));
  const otlpExportErrorDetails = formatOTLPExportErrorDetails();
  if (otlpExportErrorDetails) {
    attributes.push(buildAttr("gh-aw.otlp.export_error_details", otlpExportErrorDetails));
  }
  if (errorMessages.length > 0) {
    attributes.push(buildAttr("gh-aw.error.count", outputErrors.length));
    attributes.push(buildAttr("gh-aw.error.messages", errorMessages.join(" | ")));
  }
  attributes.push(buildAttr("gh-aw.output.item_count", outputItems.length));
  const rawItemTypes = outputItems.map(i => (i && typeof i.type === "string" ? i.type : "")).filter(Boolean);
  const itemTypes = [...new Set(rawItemTypes)].sort();
  if (itemTypes.length > 0) {
    attributes.push(buildAttr("gh-aw.output.item_types", itemTypes.join(",")));
  }

  // Enrich span with the most recent GitHub API rate-limit snapshot for post-run
  // observability.  Reads the last entry from github_rate_limits.jsonl so that
  // rate-limit headroom at conclusion time is visible in the OTLP span without
  // requiring a live collector to parse the artifact separately.
  const lastRateLimit = readLastRateLimitEntry();
  if (lastRateLimit) {
    if (typeof lastRateLimit.remaining === "number") {
      attributes.push(buildAttr("gh-aw.github.rate_limit.remaining", lastRateLimit.remaining));
    }
    if (typeof lastRateLimit.limit === "number") {
      attributes.push(buildAttr("gh-aw.github.rate_limit.limit", lastRateLimit.limit));
    }
    if (typeof lastRateLimit.used === "number") {
      attributes.push(buildAttr("gh-aw.github.rate_limit.used", lastRateLimit.used));
    }
    if (lastRateLimit.resource) {
      attributes.push(buildAttr("gh-aw.github.rate_limit.resource", String(lastRateLimit.resource)));
    }
    if (lastRateLimit.reset) {
      attributes.push(buildAttr("gh-aw.github.rate_limit.reset", String(lastRateLimit.reset)));
    }
  }

  // Include experiment assignments so each span can be correlated with the
  // A/B variant selected for this run (written by pick_experiment.cjs).
  const conclusionExperimentAssignments = readExperimentAssignments();
  attributes.push(...buildExperimentAttributes(conclusionExperimentAssignments));

  // Append user-defined custom attributes from observability.otlp.attributes.
  attributes.push(...buildCustomOTLPAttributes());

  // Enrich conclusion span with outcome evaluation fleet metrics when available.
  // Written by the outcome-collector workflow's pre-agent step.
  const outcomeSummary = readJSONIfExists("/tmp/gh-aw/outcome-summary.json");
  if (outcomeSummary && typeof outcomeSummary.total_outcomes === "number" && outcomeSummary.total_outcomes > 0) {
    attributes.push(buildAttr("gh-aw.outcome.total", outcomeSummary.total_outcomes));
    attributes.push(buildAttr("gh-aw.outcome.accepted", outcomeSummary.accepted || 0));
    attributes.push(buildAttr("gh-aw.outcome.rejected", outcomeSummary.rejected || 0));
    attributes.push(buildAttr("gh-aw.outcome.pending", outcomeSummary.pending || 0));
    attributes.push(buildAttr("gh-aw.outcome.ignored", outcomeSummary.ignored || 0));
    attributes.push(buildAttr("gh-aw.outcome.runs_checked", outcomeSummary.runs_checked || 0));
    if (typeof outcomeSummary.acceptance_rate === "number") {
      attributes.push(buildAttr("gh-aw.outcome.acceptance_rate", outcomeSummary.acceptance_rate));
    }
    if (typeof outcomeSummary.waste_rate === "number") {
      attributes.push(buildAttr("gh-aw.outcome.waste_rate", outcomeSummary.waste_rate));
    }
  }

  const graderTelemetry = jobName === "agent" ? buildGraderTelemetry(readJSONIfExists("/tmp/gh-aw/agent/graders/grader_results.json"), endMs) : { attributes: [], events: [] };
  const evalTelemetry = jobName === "evals" ? buildEvalTelemetry(readJSONLIfExists("/tmp/gh-aw/evals.jsonl"), endMs) : { attributes: [], events: [] };

  const resourceAttributes = buildGitHubActionsResourceAttributes({
    repository,
    runId,
    eventName,
    ref,
    refName,
    headRef,
    sha,
    job,
    workflowRef,
    actorId,
    runnerOs,
    runnerArch,
    runnerName,
    runnerEnvironment,
    awfVersion,
    awmgVersion,
    staged,
    runAttempt,
  });
  // OpenTelemetry semantic convention for exceptions.  Each event has
  // name="exception" with "exception.type" and "exception.message" attributes,
  // making individual errors queryable and classifiable in backends like
  // Grafana Tempo, Honeycomb, and Datadog.
  const buildSpanEvents = eventTimeMs => {
    const shouldEmitSyntheticException = hasNoReadableAgentOutput && isAgentNonOK;
    if (outputErrors.length === 0) {
      if (shouldEmitSyntheticException) {
        let exceptionType = "gh-aw.AgentFailed";
        if (isAgentTimedOut) {
          exceptionType = "gh-aw.AgentTimedOut";
        } else if (isAgentCancelled) {
          exceptionType = "gh-aw.AgentCancelled";
        }
        const exceptionMessage = (statusMessage || `agent ${agentConclusion}`).slice(0, MAX_ATTR_VALUE_LENGTH);
        return [{ timeUnixNano: toNanoString(eventTimeMs), name: "exception", attributes: [buildAttr("exception.type", exceptionType), buildAttr("exception.message", exceptionMessage)] }];
      }
      return [];
    }
    const errorTimeNano = toNanoString(eventTimeMs);
    return outputErrors
      .map(getErrorMessage)
      .filter(Boolean)
      .map(msg => {
        // Extract colon-prefixed type when available ("push_to_pull_request_branch:...")
        const colonIdx = msg.indexOf(":");
        const prefix = msg.slice(0, colonIdx);
        const hasValidPrefix = colonIdx > 0 && colonIdx < 64 && /^[a-z_][a-z0-9_.]*$/i.test(prefix);
        const exceptionType = hasValidPrefix ? `gh-aw.${prefix.toLowerCase()}` : "gh-aw.AgentError";
        const exceptionMessage = (hasValidPrefix ? msg.slice(colonIdx + 1).trim() : msg).slice(0, MAX_ATTR_VALUE_LENGTH);
        return {
          timeUnixNano: errorTimeNano,
          name: "exception",
          attributes: [buildAttr("exception.type", exceptionType), buildAttr("exception.message", exceptionMessage)],
        };
      });
  };

  const spanEvents = [...buildSpanEvents(endMs), ...graderTelemetry.events, ...evalTelemetry.events];

  // Prefer the timestamp written at the very beginning of the Execute Agent CLI step
  // (captures true step start on the host, before the AWF container launches) so the
  // dedicated agent span excludes pre-agent overhead such as workspace audit and CLI
  // proxy startup. Fall back to options.startMs (end of setup step) when the file is
  // absent — e.g. on older compiled workflows or during local development.
  const agentStartMs = (() => {
    try {
      const raw = fs.readFileSync("/tmp/gh-aw/agent_cli_start_ms.txt", "utf8").trim();
      const ms = Number(raw);
      return Number.isFinite(ms) && ms > 0 ? ms : options.startMs;
    } catch {
      return options.startMs;
    }
  })();
  /** @type {any} */
  let agentEndMs = null;
  try {
    agentEndMs = fs.statSync("/tmp/gh-aw/agent_output.json").mtimeMs;
  } catch {
    // agent_output.json may be absent for agent failures and cancellations,
    // including timed-out or manually-cancelled runs where the process was
    // killed before writing output. Fall back to nowMs() so we still emit
    // the dedicated agent span for these cases.
    if ((isAgentFailure || isAgentCancelled) && jobName === "agent" && typeof agentStartMs === "number" && agentStartMs > 0) {
      agentEndMs = nowMs();
    }
  }

  // Read agent token-usage counters and build per-category breakdown attributes.
  // These are attached exclusively to the dedicated agent span (when one is emitted)
  // to avoid double-counting in backends that sum gen_ai.usage.* across all spans.
  // When no agent span is emitted the attributes fall through to the conclusion span
  // so a single query is still sufficient for observability.
  const usageAttrs = [];
  if (typeof agentUsage.input_tokens === "number" && agentUsage.input_tokens > 0) {
    usageAttrs.push(buildAttr("gen_ai.usage.input_tokens", agentUsage.input_tokens));
  }
  if (typeof agentUsage.output_tokens === "number" && agentUsage.output_tokens > 0) {
    usageAttrs.push(buildAttr("gen_ai.usage.output_tokens", agentUsage.output_tokens));
  }
  if (typeof agentUsage.cache_read_tokens === "number" && agentUsage.cache_read_tokens > 0) {
    usageAttrs.push(buildAttr("gen_ai.usage.cache_read.input_tokens", agentUsage.cache_read_tokens));
  }
  if (typeof agentUsage.cache_write_tokens === "number" && agentUsage.cache_write_tokens > 0) {
    usageAttrs.push(buildAttr("gen_ai.usage.cache_creation.input_tokens", agentUsage.cache_write_tokens));
  }
  // Emit gen_ai.usage.total_tokens as the sum of input and output so OTel GenAI
  // semantic-convention aggregations (e.g. sum(gen_ai.usage.total_tokens)) work
  // without per-backend normalization. Cache tokens are excluded from the total
  // to match the OpenTelemetry GenAI spec definition of "total tokens consumed".
  const totalTokens = (typeof agentUsage.input_tokens === "number" ? agentUsage.input_tokens : 0) + (typeof agentUsage.output_tokens === "number" ? agentUsage.output_tokens : 0);
  if (totalTokens > 0) {
    usageAttrs.push(buildAttr("gen_ai.usage.total_tokens", totalTokens));
  }

  const endpoints = parseOTLPEndpoints();
  const conclusionSpanId = generateSpanId();
  const hasDedicatedAgentSpan = jobName === "agent" && typeof agentStartMs === "number" && agentStartMs > 0 && typeof agentEndMs === "number" && agentEndMs > agentStartMs;
  if (hasDedicatedAgentSpan && typeof agentEndMs === "number") {
    const agentSpanEndMs = agentEndMs;
    const agentSpanEvents = buildSpanEvents(agentSpanEndMs);

    // Build OTel GenAI semantic convention attributes for the dedicated agent span.
    // These follow the OpenTelemetry GenAI specification and enable out-of-the-box
    // LLM dashboards in Grafana, Datadog, and Honeycomb without custom mappings.
    // Token-usage attributes are included here (and only here) to prevent
    // double-counting with the conclusion span.
    const agentAttributes = [...attributes, ...usageAttrs];
    // gen_ai.operation.name / gen_ai.workflow.name / gen_ai.response.finish_reasons
    // are already included via the shared attributes list above.

    const agentPayload = buildOTLPPayload({
      traceId,
      spanId: generateSpanId(),
      parentSpanId: conclusionSpanId,
      spanName: jobName ? `gh-aw.${jobName}.agent` : "gh-aw.job.agent",
      startMs: agentStartMs,
      endMs: agentSpanEndMs,
      serviceName,
      scopeVersion: version,
      attributes: agentAttributes,
      resourceAttributes,
      statusCode,
      statusMessage,
      events: agentSpanEvents,
      kind: SPAN_KIND_CLIENT,
    });
    appendToOTLPJSONL(agentPayload);
    if (endpoints.length > 0) {
      await sendOTLPToAllEndpoints(endpoints, agentPayload, { skipJSONL: true });
    }
  }

  // Grader results are run-level outcomes. They belong only on the agent job's
  // conclusion span, rather than its dedicated child span or downstream jobs
  // which may have downloaded the agent artifact.
  attributes.push(...graderTelemetry.attributes);
  attributes.push(...evalTelemetry.attributes);

  // Only attach token-usage attributes to jobs that actually executed model usage.
  // Most downstream jobs (conclusion, safe_outputs) may have agent_usage.json on
  // disk via artifact download but must NOT emit token data — otherwise every
  // sum(gen_ai.usage.*) query is inflated by the number of downstream jobs.
  if (!hasDedicatedAgentSpan && jobEmitsOwnTokenUsage) {
    attributes.push(...usageAttrs);
  }

  const { maxAICredits, aiCreditsRateLimitError, maxAICreditsExceeded } = resolveAICreditsFailureState({ logProvenance: false });
  const maxAICreditsValue = normalizeNonNegativeNumber(maxAICredits);
  if (typeof maxAICreditsValue === "number") {
    attributes.push(buildAttr("gh-aw.max_ai_credits", maxAICreditsValue));
  }
  if (typeof maxAICreditsExceeded === "boolean") {
    attributes.push(buildAttr("gh-aw.max_ai_credits_exceeded", maxAICreditsExceeded));
  }
  if (typeof aiCreditsRateLimitError === "boolean") {
    attributes.push(buildAttr("gh-aw.ai_credits_rate_limit_error", aiCreditsRateLimitError));
  }

  const payload = buildOTLPPayload({
    traceId,
    spanId: conclusionSpanId,
    ...(parentSpanId ? { parentSpanId } : {}),
    spanName,
    startMs,
    endMs,
    serviceName,
    scopeVersion: version,
    attributes,
    resourceAttributes,
    statusCode,
    statusMessage,
    events: spanEvents,
  });

  // Always mirror to JSONL — the artifact is useful even without a live collector.
  appendToOTLPJSONL(payload);

  if (endpoints.length === 0) {
    return;
  }

  // Pass skipJSONL: true so sendOTLPToAllEndpoints/sendOTLPSpan don't double-write the mirror.
  await sendOTLPToAllEndpoints(endpoints, payload, { skipJSONL: true });
}

module.exports = {
  SPAN_KIND_INTERNAL,
  SPAN_KIND_SERVER,
  SPAN_KIND_CLIENT,
  SPAN_KIND_PRODUCER,
  SPAN_KIND_CONSUMER,
  isValidTraceId,
  isValidSpanId,
  generateTraceId,
  generateSpanId,
  toNanoString,
  buildAttr,
  buildDoubleAttr,
  buildArrayAttr,
  buildGitHubActionsResourceAttributes,
  buildOTLPSpan,
  buildOTLPBatchPayload,
  buildOTLPBatchPayloads,
  buildOTLPPayload,
  sanitizeOTLPPayload,
  parseOTLPHeaders,
  parseOTLPEndpoints,
  sendOTLPSpan,
  sendOTLPToAllEndpoints,
  hasProxyConfigured,
  readJSONIfExists,
  readLastRateLimitEntry,
  buildCurrentWorkflowCallId,
  buildEpisodeAttributesFromContext,
  resolveEngineId,
  GITHUB_RATE_LIMITS_JSONL_PATH,
  FAILURE_CATEGORIES_PATH,
  sendJobSetupSpan,
  sendJobConclusionSpan,
  OTEL_JSONL_PATH,
  appendToOTLPJSONL,
  buildExperimentAttributes,
  buildGraderTelemetry,
  buildEvalTelemetry,
  parseOTLPCustomAttributes,
  buildCustomOTLPAttributes,
};
