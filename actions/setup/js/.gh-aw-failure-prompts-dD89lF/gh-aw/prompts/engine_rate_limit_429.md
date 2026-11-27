> [!WARNING]
> **Engine Rate Limited (HTTP 429)**: The {engine_label} engine hit provider rate limits and could not complete this run.

This signal was detected from engine runtime logs/OTLP telemetry.

**What to do next**
- Retry the workflow after a short delay.
- If this keeps happening, reduce concurrent workflow volume or review provider quota/rate-limit policies.
