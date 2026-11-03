---
"gh-aw": minor
---

Add dependency-free OTLP/HTTP protobuf encoding for JavaScript spans through `OTEL_EXPORTER_OTLP_PROTOCOL` or `OTEL_EXPORTER_OTLP_TRACES_PROTOCOL`. Manage the standard protocol Actions variable with `gh aw env get/update` using `default_otlp_protocol`. Preserve full Azure Monitor traces endpoint paths and the existing JSON export default and JSONL mirror. Compare protobuf output with an isolated, CI-only Node.js OpenTelemetry SDK installation in its own job, without changing repository npm dependencies.
