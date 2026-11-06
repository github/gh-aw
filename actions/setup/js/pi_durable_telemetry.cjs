// @ts-check
"use strict";

const { randomBytes } = require("node:crypto");
const fs = require("node:fs");
const path = require("node:path");
const { iterateJsonl } = require("./jsonl_helpers.cjs");

const DURABLE_OTEL_FILE = "/tmp/gh-aw/agent/durable-otel.jsonl";

/**
 * @typedef {{traceId: string, id: string, attributes: Record<string, unknown>}} DurableLink
 * @typedef {{name: string, attributes: Record<string, unknown>, at: number}} DurableEvent
 * @typedef {{traceId: string, id: string, parentId?: string, name: string, startedMs: number, endedMs: number, attributes: Record<string, unknown>, events: DurableEvent[], links: DurableLink[], status: string}} DurableSpan
 */

/** Host-owned implementation of the official pi-telemetry TelemetryContext. */
function createDurableTelemetry(options = {}) {
  const pending = new Set();
  const contexts = new WeakMap();
  const traceparent = /^00-([a-f0-9]{32})-([a-f0-9]{16})-0[01]$/.exec(process.env.TRACEPARENT || "");
  const configuredTrace = process.env.GITHUB_AW_OTEL_TRACE_ID || traceparent?.[1] || "";
  const configuredParent = process.env.GITHUB_AW_OTEL_PARENT_SPAN_ID || traceparent?.[2] || "";
  const traceId = /^[a-f0-9]{32}$/.test(configuredTrace) && !/^0+$/.test(configuredTrace) ? configuredTrace : randomBytes(16).toString("hex");
  const parentId = /^[a-f0-9]{16}$/.test(configuredParent) && !/^0+$/.test(configuredParent) ? configuredParent : undefined;
  const exportSpan =
    options.exportSpan ||
    (async record => {
      const otlp = require("./send_otlp_span.cjs");
      const raw = otlp.buildOTLPPayload({
        traceId: record.traceId,
        spanId: record.id,
        parentSpanId: record.parentId,
        spanName: record.name,
        startMs: record.startedMs,
        endMs: record.endedMs,
        serviceName: process.env.OTEL_SERVICE_NAME || "gh-aw",
        attributes: Object.entries(record.attributes)
          .filter(([, value]) => value !== undefined)
          .map(([key, value]) => otlp.buildAttr(key, value)),
        statusCode: record.status === "error" ? 2 : 1,
        events: record.events.map(event => ({
          name: event.name,
          timeUnixNano: otlp.toNanoString(event.at),
          attributes: Object.entries(event.attributes)
            .filter(([, value]) => value !== undefined)
            .map(([key, value]) => otlp.buildAttr(key, value)),
        })),
      });
      raw.resourceSpans[0].scopeSpans[0].spans[0].links = record.links.map(link => ({
        traceId: link.traceId,
        spanId: link.id,
      }));
      const payload = otlp.sanitizeOTLPPayload(raw);
      const journal = options.journalFile || DURABLE_OTEL_FILE;
      fs.mkdirSync(path.dirname(journal), { recursive: true });
      if (fs.existsSync(journal) && !fs.lstatSync(journal).isFile()) throw new Error("Durable telemetry journal must be a regular file");
      fs.appendFileSync(journal, JSON.stringify(payload) + "\n", { mode: 0o600 });
    });
  const propagation = {
    inject(context) {
      const current = contexts.get(context);
      return current?.id ? { traceparent: `00-${current.traceId}-${current.id}-01` } : undefined;
    },
    extract(carrier) {
      const match = /^00-([a-f0-9]{32})-([a-f0-9]{16})-0[01]$/.exec(carrier?.traceparent || "");
      return match && !/^0+$/.test(match[1]) && !/^0+$/.test(match[2]) ? makeContext(match[1], match[2]) : root;
    },
  };
  function makeContext(trace, parent) {
    const context = {
      propagation,
      async startSpan(spanOptions, callback) {
        /** @type {DurableSpan} */
        const record = { traceId: trace, id: randomBytes(8).toString("hex"), parentId: parent, name: spanOptions.name, startedMs: Date.now(), endedMs: 0, attributes: { ...spanOptions.attributes }, events: [], links: [], status: "ok" };
        const addLink = link => {
          const current = contexts.get(link.context);
          if (current?.id) record.links.push({ ...current, attributes: link.attributes || {} });
        };
        for (const link of spanOptions.links || []) addLink(link);
        const span = Object.assign(makeContext(trace, record.id), {
          addLink,
          addEvent(name, attributes = {}) {
            record.events.push({ name, attributes, at: Date.now() });
          },
          setAttributes(attributes) {
            Object.assign(record.attributes, attributes);
          },
          // Deliberately omit arbitrary diagnostic text from telemetry.
          setStatus(status) {
            record.status = status.status;
          },
        });
        contexts.set(span, { traceId: trace, id: record.id });
        try {
          return await callback(span);
        } catch (error) {
          record.status = "error";
          throw error;
        } finally {
          record.endedMs = Date.now();
          const sending = Promise.resolve()
            .then(() => exportSpan(record))
            .catch(() => {
              process.stderr.write("[pi-durable] Telemetry export failed\n");
            });
          pending.add(sending);
          void sending.finally(() => pending.delete(sending));
        }
      },
    };
    contexts.set(context, { traceId: trace, id: parent });
    return context;
  }
  const root = makeContext(traceId, parentId);
  return { telemetry: root, flush: () => Promise.all([...pending]), trace: { traceId, ...(parentId ? { parentId } : {}) } };
}

/** Export on the trusted host; OTLP credentials never enter the harness. */
async function exportDurableTelemetry() {
  if (!fs.existsSync(DURABLE_OTEL_FILE)) return;
  if (!fs.lstatSync(DURABLE_OTEL_FILE).isFile()) throw new Error("Durable telemetry journal must be a regular file");
  const otlp = require("./send_otlp_span.cjs");
  const endpoints = otlp.parseOTLPEndpoints();
  for (const payload of iterateJsonl(fs.readFileSync(DURABLE_OTEL_FILE, "utf8"), {
    onError(error) {
      throw error;
    },
  })) {
    const sanitized = otlp.sanitizeOTLPPayload(payload);
    otlp.appendToOTLPJSONL(sanitized);
    await otlp.sendOTLPToAllEndpoints(endpoints, sanitized, { skipJSONL: true });
  }
}

module.exports = { createDurableTelemetry, exportDurableTelemetry };
