import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { once } from "node:events";
import { createServer } from "node:http";
import { createRequire } from "node:module";
import path from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

// CI installs the reference SDK outside the checkout; runtime dependencies stay unchanged.
assert.ok(process.env.GH_AW_OTEL_SDK_DIR, "Set GH_AW_OTEL_SDK_DIR to the isolated SDK installation");
const sdkRequire = createRequire(path.join(process.env.GH_AW_OTEL_SDK_DIR, "package.json"));
const require = createRequire(import.meta.url);
const { encodeOTLPTraceRequest } = require("../otlp_protobuf.cjs");
const { buildOTLPPayload } = require("../send_otlp_span.cjs");
const { BasicTracerProvider, InMemorySpanExporter, SimpleSpanProcessor } = sdkRequire("@opentelemetry/sdk-trace-base");
const { resourceFromAttributes } = sdkRequire("@opentelemetry/resources");
const { ROOT_CONTEXT, SpanKind, SpanStatusCode, TraceFlags, createTraceState, trace } = sdkRequire("@opentelemetry/api");
const { JsonTraceSerializer, ProtobufTraceSerializer } = sdkRequire("@opentelemetry/otlp-transformer");
// Generated protocol types from the pinned SDK provide an independent wire oracle.
const { ExportTraceServiceRequest } = sdkRequire("@opentelemetry/otlp-transformer/build/src/generated/root").opentelemetry.proto.collector.trace.v1;

function fromProtoJson(value) {
  if (Array.isArray(value)) return value.map(fromProtoJson);
  if (!value || typeof value !== "object") return value;
  return Object.fromEntries(Object.entries(value).map(([key, item]) => [key, ["traceId", "spanId", "parentSpanId"].includes(key) ? Buffer.from(item, "base64").toString("hex") : fromProtoJson(item)]));
}

function toProtoJson(value) {
  if (Array.isArray(value)) return value.map(toProtoJson);
  if (!value || typeof value !== "object") return value;
  return Object.fromEntries(Object.entries(value).map(([key, item]) => [key, ["traceId", "spanId", "parentSpanId"].includes(key) ? Buffer.from(item, "hex").toString("base64") : toProtoJson(item)]));
}

function decode(wire) {
  return ExportTraceServiceRequest.toObject(ExportTraceServiceRequest.decode(wire), { longs: String, bytes: String });
}

function compare(payload, referenceWire) {
  const actual = encodeOTLPTraceRequest(payload);
  assert.deepEqual(decode(actual), decode(referenceWire));
  assert.deepEqual(actual, Buffer.from(referenceWire), "wire bytes must match the SDK encoder exactly");
}

test("matches the Node.js SDK for 150 spans across resources and scopes", async () => {
  const allSpans = [];
  for (let group = 0; group < 2; group++) {
    const exporter = new InMemorySpanExporter();
    const provider = new BasicTracerProvider({
      resource: resourceFromAttributes({ "service.name": "gh-aw", group }, { schemaUrl: "https://opentelemetry.io/schemas/1.30.0" }),
      spanProcessors: [new SimpleSpanProcessor(exporter)],
    });
    try {
      for (let index = 0; index < 75; index++) {
        const parent = {
          traceId: "0123456789abcdef0123456789abcdef",
          spanId: (index + 1).toString(16).padStart(16, "0"),
          traceFlags: TraceFlags.SAMPLED,
          isRemote: index % 2 === 0,
          traceState: createTraceState("vendor=state"),
        };
        const tracer = provider.getTracer("gh-aw", String(index % 3), { schemaUrl: "https://opentelemetry.io/schemas/1.30.0" });
        const span = tracer.startSpan(
          `gh-aw.sdk.${index}`,
          {
            kind: [SpanKind.INTERNAL, SpanKind.SERVER, SpanKind.CLIENT, SpanKind.PRODUCER, SpanKind.CONSUMER][index % 5],
            startTime: [1700000000, index + 1],
            attributes: {
              text: "UTF-8: café 日本語 \u0000 " + "x".repeat(index * 3),
              negative: -Number.MAX_SAFE_INTEGER + index,
              positive: Number.MAX_SAFE_INTEGER - index,
              integer: index,
              fraction: -0.125 * index,
              bool: index % 2 === 0,
              empty: "",
              strings: ["", "value"],
              integers: [0, -1, 128],
              doubles: [0.0, -0.5],
              booleans: [true, false],
            },
            links: [{ context: parent, attributes: { "link.attribute": "value" } }],
          },
          index % 3 ? trace.setSpanContext(ROOT_CONTEXT, parent) : ROOT_CONTEXT
        );
        span.setStatus({ code: [SpanStatusCode.UNSET, SpanStatusCode.OK, SpanStatusCode.ERROR][index % 3], ...(index % 3 === 2 ? { message: "failure" } : {}) });
        span.addEvent("exception", { "exception.message": "oops" }, [1700000000, 123]);
        span.addEvent("empty", {}, [1700000000, 456]);
        span.end([1700000000, 123456789 + index]);
      }
      await provider.forceFlush();
      allSpans.push(...exporter.getFinishedSpans());
    } finally {
      await provider.shutdown();
    }
  }
  assert.equal(allSpans.length, 150);
  const payload = JSON.parse(Buffer.from(JsonTraceSerializer.serializeRequest(allSpans)).toString("utf8"));
  for (const resourceSpans of payload.resourceSpans) {
    // The pinned SDK duplicates schemaUrl inside Resource, outside the OTLP schema.
    assert.equal(resourceSpans.resource.schemaUrl, resourceSpans.schemaUrl);
    delete resourceSpans.resource.schemaUrl;
  }
  compare(payload, ProtobufTraceSerializer.serializeRequest(allSpans));
});

test("matches SDK protocol types for 64-bit boundaries and nested AnyValue", () => {
  const payload = {
    resourceSpans: [
      {
        resource: { attributes: [], droppedAttributesCount: 300 },
        scopeSpans: [
          {
            scope: { droppedAttributesCount: 128 },
            spans: [
              {
                traceId: "ff".repeat(16),
                spanId: "ff".repeat(8),
                startTimeUnixNano: "18446744073709551615",
                endTimeUnixNano: "0",
                flags: 0xffffffff,
                droppedAttributesCount: 128,
                droppedEventsCount: 16384,
                droppedLinksCount: 0xffffffff,
                events: [{ droppedAttributesCount: 128 }],
                links: [{ droppedAttributesCount: 128, flags: 0xffffffff }],
                attributes: [
                  { key: "min", value: { intValue: "-9223372036854775808" } },
                  { key: "max", value: { intValue: "9223372036854775807" } },
                  { key: "zero", value: { intValue: 0 } },
                  { key: "false", value: { boolValue: false } },
                  { key: "empty", value: { stringValue: "" } },
                  { key: "bytes", value: { bytesValue: Buffer.from(Array.from({ length: 256 }, (_, i) => i)).toString("base64") } },
                  {
                    key: "nested",
                    value: {
                      kvlistValue: {
                        values: [
                          {
                            key: "array",
                            value: { arrayValue: { values: [{ intValue: -1 }, { stringValue: "nested" }, { kvlistValue: { values: [{ key: "leaf", value: { doubleValue: -1.5 } }] } }] } },
                          },
                        ],
                      },
                    },
                  },
                ],
              },
            ],
          },
        ],
      },
    ],
  };
  const expected = ExportTraceServiceRequest.fromObject(toProtoJson(payload));
  compare(payload, ExportTraceServiceRequest.encode(expected).finish());
});

test("matches the SDK for an empty request", () => {
  compare({ resourceSpans: [] }, ProtobufTraceSerializer.serializeRequest([]));
});

for (const token of ["NaN", "Infinity", "-Infinity"]) {
  test(`matches SDK protocol types for OTLP JSON double token ${token}`, () => {
    const payload = {
      resourceSpans: [{ resource: { attributes: [{ key: "nonfinite", value: { doubleValue: token } }] } }],
    };
    const expected = ExportTraceServiceRequest.fromObject(toProtoJson(payload));
    compare(payload, ExportTraceServiceRequest.encode(expected).finish());
    const value = decode(encodeOTLPTraceRequest(payload)).resourceSpans[0].resource.attributes[0].value.doubleValue;
    assert.ok(Object.is(value, Number(token)));
    assert.equal(payload.resourceSpans[0].resource.attributes[0].value.doubleValue, token);
  });
}

test("decodes actual gh-aw payloads with the SDK without losing timestamps or IDs", () => {
  const payload = buildOTLPPayload({
    traceId: "0123456789abcdef0123456789abcdef",
    spanId: "0123456789abcdef",
    spanName: "gh-aw.sdk",
    serviceName: "gh-aw",
    scopeVersion: "test",
    startMs: 1700000000000,
    endMs: 1700000000123,
    attributes: [{ key: "items", value: { intValue: 128 } }],
  });
  const expected = ExportTraceServiceRequest.fromObject(toProtoJson(payload));
  compare(payload, ExportTraceServiceRequest.encode(expected).finish());
  const span = fromProtoJson(decode(encodeOTLPTraceRequest(payload))).resourceSpans[0].scopeSpans[0].spans[0];
  assert.equal(span.traceId, payload.resourceSpans[0].scopeSpans[0].spans[0].traceId);
  assert.equal(span.startTimeUnixNano, "1700000000000000000");
  assert.equal(span.endTimeUnixNano, "1700000000123000000");
  assert.equal(span.attributes[0].value.intValue, "128");
});

test("sends SDK-decodable binary payloads through real HTTP and curl transports", async () => {
  const requests = [];
  const collector = createServer(async (request, response) => {
    const chunks = [];
    for await (const chunk of request) chunks.push(chunk);
    requests.push({ path: request.url, contentType: request.headers["content-type"], body: Buffer.concat(chunks) });
    response.writeHead(200).end();
  });
  collector.listen(0, "127.0.0.1");
  await once(collector, "listening");
  try {
    const endpoint = `http://127.0.0.1:${collector.address().port}/datacollectionRules/dcr-test/streams/Microsoft-OTLP-Traces/otlp/v1/traces?test=1`;
    for (const proxy of ["", "http://127.0.0.1:1"]) {
      const env = Object.fromEntries(
        Object.entries(process.env).filter(([key]) => !["http_proxy", "https_proxy", "all_proxy", "otel_exporter_otlp_protocol", "otel_exporter_otlp_traces_protocol", "otel_exporter_otlp_headers"].includes(key.toLowerCase()))
      );
      await promisify(execFile)(
        process.execPath,
        [
          "-e",
          'const { sendOTLPSpan } = require("./send_otlp_span.cjs");' +
            "sendOTLPSpan(process.argv[1], {resourceSpans:[{scopeSpans:[{spans:[{" +
            'name:"binary\\u0000span",startTimeUnixNano:"1700000000000000001",' +
            'attributes:[{key:"secret",value:{stringValue:"do-not-export"}}]' +
            "}]}]}]}, {skipJSONL:true,maxRetries:0});",
          endpoint,
        ],
        {
          cwd: fileURLToPath(new URL("../", import.meta.url)),
          env: { ...env, OTEL_EXPORTER_OTLP_PROTOCOL: "http/protobuf", HTTP_PROXY: proxy, NO_PROXY: "127.0.0.1", no_proxy: "127.0.0.1" },
          timeout: 15000,
        }
      );
    }
  } finally {
    await new Promise((resolve, reject) => collector.close(error => (error ? reject(error) : resolve())));
  }
  assert.equal(requests.length, 2);
  for (const request of requests) {
    assert.equal(request.path, "/datacollectionRules/dcr-test/streams/Microsoft-OTLP-Traces/otlp/v1/traces?test=1");
    assert.equal(request.contentType, "application/x-protobuf");
    const span = decode(request.body).resourceSpans[0].scopeSpans[0].spans[0];
    assert.equal(span.name, "binary\u0000span");
    assert.equal(span.startTimeUnixNano, "1700000000000000001");
    assert.equal(span.attributes[0].value.stringValue, "[REDACTED]");
  }
});
