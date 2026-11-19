// @ts-check
"use strict";

const { performance } = require("node:perf_hooks");
const { AdmissionError, FAILURE, readAdmissionConfig, buildAdmissionIdentity, parseAdmissionDecision, readBoundedBody } = require("./admission_protocol.cjs");
const { withRetry, sleep } = require("./error_recovery.cjs");

const OUTAGE = "ERR_ADMISSION: Admission service unavailable; continuing without a service decision.";

/**
 * @template T
 * @param {(signal: AbortSignal) => Promise<T>} operation
 * @param {number} deadline
 * @param {() => number} now
 * @returns {Promise<T>}
 */
async function withAdmissionTimeout(operation, deadline, now) {
  try {
    return await withRetry(
      async signal => {
        if (!signal) throw new AdmissionError();
        try {
          return await operation(signal);
        } catch (error) {
          throw error instanceof AdmissionError ? error : new AdmissionError(true);
        }
      },
      { maxRetries: 0, timeoutMs: 10000, deadline, now, timeoutError: () => new AdmissionError(true), shouldRetry: () => false },
      "workflow admission request"
    );
  } catch (error) {
    const original = error instanceof Error && "originalError" in error ? error.originalError : error;
    throw original instanceof AdmissionError ? original : new AdmissionError();
  }
}

/** @param {Response} response */
function discard(response) {
  void response.body?.cancel().catch(() => {});
}

/** @param {number} status */
function isTransient(status) {
  return status === 408 || status === 429 || (status >= 500 && status <= 599);
}

/**
 * @typedef {{traceId: string, parentSpanId: string | undefined, operationSpanId: string, attempts: Array<{spanId: string, startMs: number, endMs: number}>, startMs: number, endMs: number, endpoints: ReturnType<typeof import("./send_otlp_span.cjs").parseOTLPEndpoints>}} AdmissionTrace
 * @typedef {{result: "skipped"} | ({result: "ignored"} & AdmissionTrace) | ({result: "admitted"} & ReturnType<typeof parseAdmissionDecision> & AdmissionTrace)} AdmissionResult
 */

/**
 * @template T
 * @param {{config: NonNullable<ReturnType<typeof readAdmissionConfig>>, body: string, parseResponse: (response: Response) => Promise<T>, acceptedStatuses?: number[], env?: NodeJS.ProcessEnv, fetchImpl?: typeof fetch, now?: () => number, mask?: (token: string) => void, log?: (message: string) => void}} options
 * @returns {Promise<{decision: T | undefined} & AdmissionTrace>}
 */
async function requestAuthority({ config, body, parseResponse, acceptedStatuses = [200], env = process.env, fetchImpl = fetch, now = () => performance.now(), mask = () => {}, log = () => {} }) {
  const requestUrl = env.ACTIONS_ID_TOKEN_REQUEST_URL;
  const bootstrapToken = env.ACTIONS_ID_TOKEN_REQUEST_TOKEN;
  if (!requestUrl || !bootstrapToken) throw new AdmissionError();
  let oidcUrl;
  try {
    oidcUrl = new URL(requestUrl);
  } catch {
    throw new AdmissionError();
  }
  if (oidcUrl.protocol !== "https:" || oidcUrl.username || oidcUrl.password || oidcUrl.hash) throw new AdmissionError();
  oidcUrl.searchParams.set("audience", config.audience);

  const { generateTraceId, generateSpanId, isValidTraceId, isValidSpanId, parseOTLPEndpoints, buildTraceparent } = require("./send_otlp_span.cjs");
  const endpoints = parseOTLPEndpoints();
  const existingTraceId = env.GITHUB_AW_OTEL_TRACE_ID || "";
  const traceId = isValidTraceId(existingTraceId) ? existingTraceId : generateTraceId();
  const parentSpanId = isValidTraceId(env.GITHUB_AW_OTEL_TRACE_ID || "") && isValidSpanId(env.GITHUB_AW_OTEL_PARENT_SPAN_ID || "") ? env.GITHUB_AW_OTEL_PARENT_SPAN_ID : undefined;
  const operationSpanId = generateSpanId();
  const startMs = Date.now();
  const deadline = now() + 60000;
  let token = "";
  let expiresAt = 0;
  /** @type {Array<{spanId: string, startMs: number, endMs: number}>} */
  const attempts = [];
  /** @type {T | undefined} */
  let decision;
  try {
    decision = await withRetry(
      async () => {
        try {
          if (!token || expiresAt <= Date.now() + 10000) {
            log("Admission: acquiring or renewing OIDC credentials (issuer, audience and tokens withheld).");
            token = await withAdmissionTimeout(
              async signal => {
                const response = await fetchImpl(oidcUrl, { headers: { authorization: `bearer ${bootstrapToken}`, accept: "application/json" }, redirect: "manual", signal });
                if (response.status !== 200) {
                  discard(response);
                  throw new AdmissionError(isTransient(response.status));
                }
                let result;
                try {
                  result = JSON.parse(await readBoundedBody(response, 65536));
                } catch (error) {
                  if (error instanceof AdmissionError) throw error;
                  if (error instanceof SyntaxError) throw new AdmissionError();
                  throw new AdmissionError(true);
                }
                if (typeof result?.value !== "string" || !/^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$/.test(result.value)) throw new AdmissionError();
                try {
                  mask(result.value);
                } catch {
                  throw new AdmissionError();
                }
                try {
                  const payload = JSON.parse(Buffer.from(result.value.split(".")[1], "base64url").toString("utf8"));
                  if (!Number.isFinite(payload.exp) || payload.exp * 1000 <= Date.now()) throw new AdmissionError();
                  expiresAt = payload.exp * 1000;
                } catch {
                  throw new AdmissionError();
                }
                return result.value;
              },
              deadline,
              now
            );
            log("Admission: OIDC credentials validated and masked.");
          } else {
            log("Admission: reusing valid in-memory OIDC credentials.");
          }
          log("Admission: starting service request within the remaining deadline.");
          const spanId = generateSpanId();
          const attemptStartMs = Date.now();
          try {
            return await withAdmissionTimeout(
              async signal => {
                /** @type {Record<string, string>} */
                const headers = { authorization: `bearer ${token}`, "content-type": "application/json", "cache-control": "no-store" };
                headers.traceparent = buildTraceparent(traceId, spanId);
                const response = await fetchImpl(config.endpoint, { method: "POST", headers, body, redirect: "manual", signal });
                if (!acceptedStatuses.includes(response.status)) {
                  discard(response);
                  throw new AdmissionError(isTransient(response.status));
                }
                return await parseResponse(response);
              },
              deadline,
              now
            );
          } finally {
            attempts.push({ spanId, startMs: attemptStartMs, endMs: Date.now() });
          }
        } catch (error) {
          log(error instanceof AdmissionError && !error.transient ? "Admission: non-retryable failure; activation will be blocked." : "Admission: transient failure; retry and deadline policy applies.");
          throw error instanceof AdmissionError ? error : new AdmissionError(true);
        }
      },
      {
        maxRetries: 2,
        initialDelayMs: 125,
        backoffMultiplier: 2,
        maxDelayMs: 1000,
        jitterMs: 250,
        shouldRetry: error => error instanceof AdmissionError && error.transient && now() < deadline,
        sleep: delay => withAdmissionTimeout(signal => sleep(delay, signal), deadline, now),
      },
      "workflow admission"
    );
  } catch (error) {
    const original = error instanceof Error && "originalError" in error ? error.originalError : error;
    if (!(original instanceof AdmissionError) || !original.transient) throw new AdmissionError();
  } finally {
    token = "";
  }
  return { decision, traceId, parentSpanId, operationSpanId, attempts, startMs, endMs: Date.now(), endpoints };
}

/**
 * @param {{env?: NodeJS.ProcessEnv, fetchImpl?: typeof fetch, now?: () => number, mask?: (token: string) => void, log?: (message: string) => void}} [options]
 * @returns {Promise<AdmissionResult>}
 */
async function checkAdmission(options = {}) {
  const { env = process.env, log = () => {} } = options;
  log("Admission: checking configuration (configuration values withheld).");
  const config = readAdmissionConfig(env);
  if (!config) {
    log("Admission: disabled; retaining existing budget and daily-validation policy.");
    return { result: "skipped" };
  }
  const body = buildAdmissionIdentity(env);
  log("Admission: request identity validated (identity and workflow hash withheld).");
  const { decision, ...traceContext } = await requestAuthority({
    ...options,
    config,
    body,
    parseResponse: async response => {
      if (!/^application\/json(?:\s*;\s*charset\s*=\s*(?:"utf-8"|utf-8))?\s*$/i.test(response.headers.get("content-type") || "")) {
        discard(response);
        throw new AdmissionError();
      }
      const result = parseAdmissionDecision(await readBoundedBody(response, 4096));
      log("Admission: complete response validated; primary allowance supplied (values withheld).");
      log(result.threatDetectionMaxAiCredits === undefined ? "Admission: detection allowance absent; retaining component policy." : "Admission: detection allowance supplied (value withheld).");
      log(result.evalsMaxAiCredits === undefined ? "Admission: evals allowance absent; retaining component policy." : "Admission: evals allowance supplied (value withheld).");
      return result;
    },
  });
  if (!decision) {
    if (config.onOutage === "block") {
      log("Admission: service unavailable; outage policy blocks activation.");
      throw new AdmissionError();
    }
    log("Admission: service unavailable; outage policy retains existing budgets and daily validation.");
    return { result: "ignored", ...traceContext };
  }
  log("Admission: approved; validated allowances selected for runtime enforcement; daily validation bypassed.");
  return { result: "admitted", ...decision, ...traceContext };
}

/**
 * @param {typeof import("@actions/core")} core
 * @param {AdmissionResult | {result: "blocked"}} decision
 */
async function writeAdmissionSummary(core, decision) {
  if (!core.summary) return;
  const admitted = decision.result === "admitted";
  const supplied = "Supplied; selected for runtime enforcement (value withheld)";
  const fallback = decision.result === "blocked" ? "Not applied; activation blocked" : "Existing budget policy";
  const rows = [
    ["Decision", decision.result],
    ["Primary allowance", admitted ? supplied : fallback],
    ["Threat-detection allowance", admitted && decision.threatDetectionMaxAiCredits !== undefined ? supplied : fallback],
    ["Evals allowance", admitted && decision.evalsMaxAiCredits !== undefined ? supplied : fallback],
    ["Daily validation", admitted ? "Bypassed by admission approval" : decision.result === "blocked" ? "Activation blocked" : "Existing validation retained"],
  ];
  if ("attempts" in decision) rows.push(["Service attempts", String(decision.attempts.length)]);
  try {
    core.summary.addRaw("### Admission gate\n\n");
    core.summary.addDetails(
      "Admission decision and budget sources (values withheld)",
      "\n\n| Field | Result |\n| --- | --- |\n" + rows.map(([key, value]) => `| ${key} | ${value} |`).join("\n") + "\n\nIdentity, endpoints, tokens, policy values and response diagnostics are withheld.\n"
    );
    await core.summary.write();
  } catch {
    core.warning("ERR_ADMISSION: Unable to write admission step summary.");
  }
}

/** @param {typeof import("@actions/core")} core */
async function main(core) {
  try {
    const decision = await checkAdmission({ mask: token => core.setSecret(token), log: message => core.info?.(message) });
    if (decision.result === "ignored") core.warning(OUTAGE);
    if (decision.result === "admitted") {
      core.setOutput("max-ai-credits", String(decision.maxAiCredits));
      if (decision.threatDetectionMaxAiCredits !== undefined) core.setOutput("max-ai-credits-threat-detection", String(decision.threatDetectionMaxAiCredits));
      if (decision.evalsMaxAiCredits !== undefined) core.setOutput("max-ai-credits-evals", String(decision.evalsMaxAiCredits));
    }
    if (decision.result !== "skipped") {
      core.exportVariable("GITHUB_AW_OTEL_TRACE_ID", decision.traceId);
      core.setOutput("trace-id", decision.traceId);
      if (decision.endpoints.length) {
        const { buildOTLPPayload, buildGitHubActionsResourceAttributes, sendOTLPToAllEndpoints } = require("./send_otlp_span.cjs");
        const resourceAttributes = buildGitHubActionsResourceAttributes({ repository: process.env.GITHUB_REPOSITORY || "", runId: process.env.GITHUB_RUN_ID || "", workflowName: process.env.GITHUB_WORKFLOW || "", staged: false });
        for (const attempt of decision.attempts) {
          const payload = buildOTLPPayload({
            ...attempt,
            traceId: decision.traceId,
            parentSpanId: decision.operationSpanId,
            spanName: "gh-aw.admission.http",
            serviceName: "gh-aw",
            kind: 3,
            attributes: [],
            resourceAttributes,
          });
          await sendOTLPToAllEndpoints(decision.endpoints, payload, { maxRetries: 0 });
        }
        await sendOTLPToAllEndpoints(
          decision.endpoints,
          buildOTLPPayload({
            traceId: decision.traceId,
            spanId: decision.operationSpanId,
            parentSpanId: decision.parentSpanId,
            startMs: decision.startMs,
            endMs: decision.endMs,
            spanName: "gh-aw.admission",
            serviceName: "gh-aw",
            attributes: [],
            resourceAttributes,
          }),
          { maxRetries: 0 }
        );
      }
    }
    core.setOutput("result", decision.result);
    core.exportVariable("GH_AW_ADMISSION_RESULT", decision.result);
    await writeAdmissionSummary(core, decision);
  } catch {
    core.info?.("Admission: activation blocked; sensitive diagnostics withheld.");
    await writeAdmissionSummary(core, { result: "blocked" });
    core.setFailed(FAILURE);
  }
}

module.exports = { checkAdmission, requestAuthority, main, OUTAGE };
