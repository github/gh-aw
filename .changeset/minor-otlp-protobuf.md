---
"gh-aw": minor
---

Add dependency-free OTLP/HTTP protobuf encoding for JavaScript spans through `OTEL_EXPORTER_OTLP_PROTOCOL` or `OTEL_EXPORTER_OTLP_TRACES_PROTOCOL`. Preserve full Azure Monitor traces endpoint paths and the existing JSON export default and JSONL mirror. Compare protobuf output with the official OpenTelemetry SDK in CI without adding npm dependencies.
