"""Compare the dependency-free JavaScript encoder with the official OTel SDK.

Run with opentelemetry-exporter-otlp-proto-http==1.38.0 installed (CI only).
No Python or OpenTelemetry packages are required by the JavaScript runtime.
"""

import base64
from http.server import BaseHTTPRequestHandler, HTTPServer
import json
import os
from pathlib import Path
import random
import subprocess
from threading import Thread
import unittest

from google.protobuf.json_format import MessageToDict
from opentelemetry.exporter.otlp.proto.common.trace_encoder import encode_spans
from opentelemetry.proto.collector.trace.v1.trace_service_pb2 import (
    ExportTraceServiceRequest,
)
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import Event, ReadableSpan
from opentelemetry.sdk.util.instrumentation import InstrumentationScope
from opentelemetry.trace import (
    Link,
    SpanContext,
    SpanKind,
    Status,
    StatusCode,
    TraceFlags,
    TraceState,
)

RUNTIME = Path(__file__).resolve().parents[1]


def otlp_json(value):
    """OTLP JSON uses hex IDs, unlike generic protobuf JSON's base64 bytes."""
    if isinstance(value, list):
        return [otlp_json(item) for item in value]
    if isinstance(value, dict):
        return {
            key: base64.b64decode(item).hex()
            if key in ("traceId", "spanId", "parentSpanId")
            else otlp_json(item)
            for key, item in value.items()
        }
    return value


def javascript_encode(payload):
    return subprocess.run(
        [
            "node",
            "-e",
            'const { encodeOTLPTraceRequest } = require("./otlp_protobuf.cjs");'
            'const payload = JSON.parse(require("node:fs").readFileSync(0, "utf8"));'
            "process.stdout.write(encodeOTLPTraceRequest(payload));",
        ],
        cwd=RUNTIME,
        input=json.dumps(payload).encode(),
        capture_output=True,
        check=True,
    ).stdout


class ProtobufCompatibility(unittest.TestCase):
    def compare(self, expected):
        payload = otlp_json(MessageToDict(expected, use_integers_for_enums=True))
        wire = javascript_encode(payload)
        actual = ExportTraceServiceRequest.FromString(wire)
        self.assertEqual(actual, expected)
        self.assertEqual(
            wire,
            expected.SerializeToString(deterministic=True),
        )

    def test_sdk_spans(self):
        rng = random.Random(4318)
        spans = []
        for index in range(150):
            context = SpanContext(
                rng.getrandbits(128) or 1,
                rng.getrandbits(64) or 1,
                is_remote=False,
                trace_flags=TraceFlags(index % 2),
                trace_state=TraceState([("vendor", "state")]),
            )
            parent = SpanContext(
                context.trace_id,
                rng.getrandbits(64) or 1,
                is_remote=index % 2 == 0,
                trace_flags=context.trace_flags,
            )
            attributes = {
                "text": "UTF-8: café 日本語 \u0000 " + "x" * index,
                "negative": -(2**63) + index,
                "positive": 2**63 - 1 - index,
                "integer": index,
                "fraction": -0.125 * index,
                "bool": index % 2 == 0,
                "empty": "",
                "strings": ("", "value"),
                "integers": (0, -1, 128),
                "doubles": (0.0, -0.5),
                "booleans": (True, False),
            }
            spans.append(
                ReadableSpan(
                    name=f"gh-aw.sdk.{index}",
                    context=context,
                    parent=parent if index % 3 else None,
                    resource=Resource(
                        {"service.name": "gh-aw", "group": index % 2},
                        schema_url="https://opentelemetry.io/schemas/1.30.0",
                    ),
                    instrumentation_scope=InstrumentationScope(
                        "gh-aw", str(index % 3),
                        schema_url="https://opentelemetry.io/schemas/1.30.0",
                        attributes={"scope.attribute": index % 3},
                    ),
                    attributes=attributes,
                    kind=list(SpanKind)[index % len(SpanKind)],
                    status=Status(
                        list(StatusCode)[index % len(StatusCode)],
                        "failure" if index % len(StatusCode) == 2 else None,
                    ),
                    start_time=1700000000000000001 + index,
                    end_time=1700000000123456789 + index,
                    events=[
                        Event("exception", {"exception.message": "oops"}, 1700000000000000123),
                        Event("empty", {}, 1700000000000000456),
                    ],
                    links=[Link(parent, {"link.attribute": "value"})],
                )
            )
        self.compare(encode_spans(spans))

    def test_anyvalue_and_wire_boundaries(self):
        expected = ExportTraceServiceRequest()
        resource = expected.resource_spans.add()
        scope = resource.scope_spans.add()
        span = scope.spans.add()
        span.trace_id = bytes.fromhex("ff" * 16)
        span.span_id = bytes.fromhex("ff" * 8)
        span.start_time_unix_nano = 2**64 - 1
        span.end_time_unix_nano = 0
        span.flags = 2**32 - 1
        span.dropped_attributes_count = 128
        span.dropped_events_count = 16384
        span.dropped_links_count = 2**32 - 1
        resource.resource.dropped_attributes_count = 300
        scope.scope.dropped_attributes_count = 128
        event = span.events.add()
        event.dropped_attributes_count = 128
        link = span.links.add()
        link.dropped_attributes_count = 128
        link.flags = 2**32 - 1
        for name, value in (("min", -(2**63)), ("max", 2**63 - 1), ("zero", 0)):
            attribute = span.attributes.add(key=name)
            attribute.value.int_value = value
        span.attributes.add(key="false").value.bool_value = False
        span.attributes.add(key="empty").value.string_value = ""
        span.attributes.add(key="bytes").value.bytes_value = bytes(range(256))
        nested = span.attributes.add(key="nested").value.kvlist_value.values.add(key="array")
        nested.value.array_value.values.add().int_value = -1
        nested.value.array_value.values.add().string_value = "nested"
        nested.value.array_value.values.add().kvlist_value.values.add(key="leaf").value.double_value = -1.5
        self.compare(expected)

    def test_empty_request(self):
        self.compare(ExportTraceServiceRequest())

    def test_actual_runtime_payload(self):
        result = subprocess.run(
            [
                "node", "-e",
                'const { buildOTLPPayload } = require("./send_otlp_span.cjs");'
                'process.stdout.write(JSON.stringify(buildOTLPPayload({'
                'traceId:"0123456789abcdef0123456789abcdef",spanId:"0123456789abcdef",'
                'spanName:"gh-aw.sdk",serviceName:"gh-aw",scopeVersion:"test",'
                'startMs:1700000000000,endMs:1700000000123,'
                'attributes:[{key:"items",value:{intValue:128}}]})));',
            ],
            cwd=RUNTIME, capture_output=True, check=True,
        )
        payload = json.loads(result.stdout)
        actual = ExportTraceServiceRequest.FromString(javascript_encode(payload))
        span = actual.resource_spans[0].scope_spans[0].spans[0]
        self.assertEqual(span.trace_id.hex(), "0123456789abcdef0123456789abcdef")
        self.assertEqual(span.start_time_unix_nano, 1700000000000000000)
        self.assertEqual(span.end_time_unix_nano, 1700000000123000000)
        self.assertEqual(span.attributes[0].value.int_value, 128)
        self.compare(actual)

    def test_http_and_curl_transport(self):
        requests = []

        class Collector(BaseHTTPRequestHandler):
            def do_POST(self):
                requests.append((
                    self.path,
                    self.headers.get("Content-Type"),
                    self.rfile.read(int(self.headers["Content-Length"])),
                ))
                self.send_response(200)
                self.end_headers()

            def log_message(self, *_args):
                pass

        with HTTPServer(("127.0.0.1", 0), Collector) as collector:
            worker = Thread(target=collector.serve_forever)
            worker.start()
            try:
                for proxy in ("", "http://127.0.0.1:1"):
                    env = {
                        key: value for key, value in os.environ.items()
                        if key.lower() not in (
                            "http_proxy", "https_proxy", "all_proxy",
                            "otel_exporter_otlp_protocol",
                            "otel_exporter_otlp_traces_protocol",
                            "otel_exporter_otlp_headers",
                        )
                    }
                    env.update(
                        OTEL_EXPORTER_OTLP_PROTOCOL="http/protobuf",
                        HTTP_PROXY=proxy,
                        NO_PROXY="127.0.0.1",
                        no_proxy="127.0.0.1",
                    )
                    subprocess.run(
                        [
                            "node", "-e",
                            'const { sendOTLPSpan } = require("./send_otlp_span.cjs");'
                            f'const url = "http://127.0.0.1:{collector.server_port}'
                            '/datacollectionRules/dcr-test/streams/Microsoft-OTLP-Traces/otlp/v1/traces?test=1";'
                            'sendOTLPSpan(url, {resourceSpans:[{scopeSpans:[{spans:[{'
                            'name:"binary\\u0000span",startTimeUnixNano:"1700000000000000001",'
                            'attributes:[{key:"secret",value:{stringValue:"do-not-export"}}]'
                            '}]}]}]}, {skipJSONL:true,maxRetries:0});',
                        ],
                        cwd=RUNTIME, env=env, capture_output=True, check=True, timeout=15,
                    )
            finally:
                collector.shutdown()
                worker.join()
        self.assertEqual(len(requests), 2)
        for path, content_type, body in requests:
            self.assertEqual(path, "/datacollectionRules/dcr-test/streams/Microsoft-OTLP-Traces/otlp/v1/traces?test=1")
            self.assertEqual(content_type, "application/x-protobuf")
            span = ExportTraceServiceRequest.FromString(body).resource_spans[0].scope_spans[0].spans[0]
            self.assertEqual(span.name, "binary\u0000span")
            self.assertEqual(span.start_time_unix_nano, 1700000000000000001)
            self.assertEqual(span.attributes[0].value.string_value, "[REDACTED]")


if __name__ == "__main__":
    unittest.main()
