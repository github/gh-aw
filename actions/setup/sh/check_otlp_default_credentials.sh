#!/usr/bin/env bash
set +o histexpand

#
# check_otlp_default_credentials.sh - Validate enterprise OTLP default configuration
#
# Emitted only for workflows whose OTLP endpoint comes from the enterprise default
# environment (secrets.GH_AW_DEFAULT_OTLP_ENDPOINT falling back to
# vars.GH_AW_DEFAULT_OTLP_ENDPOINT / secrets.GH_AW_DEFAULT_OTLP_HEADERS)
# instead of `observability.otlp` frontmatter.
#
# Behaviour:
#   - Endpoint empty  -> no-op, telemetry export is simply disabled.
#   - Endpoint set and headers set -> no-op.
#   - Endpoint set but headers empty -> fail, so a misconfigured organization or
#     enterprise default is reported instead of silently sending unauthenticated
#     telemetry.
#
# Secret values are never printed; only their presence is reported.
#
# Environment variables read:
#   OTEL_EXPORTER_OTLP_ENDPOINT - resolved endpoint (may be empty)
#   OTEL_EXPORTER_OTLP_HEADERS  - resolved exporter headers (may be empty)
#
# Exit codes:
#   0 - Configuration is usable (or telemetry is disabled)
#   1 - Endpoint configured without credentials

set -euo pipefail

ENDPOINT="${OTEL_EXPORTER_OTLP_ENDPOINT:-}"
HEADERS="${OTEL_EXPORTER_OTLP_HEADERS:-}"

if [ -z "$ENDPOINT" ]; then
  echo "OTLP telemetry is not configured (GH_AW_DEFAULT_OTLP_ENDPOINT is empty); skipping export."
  exit 0
fi

if [ -z "$HEADERS" ]; then
  echo '::error::'"OTLP telemetry endpoint is configured through GH_AW_DEFAULT_OTLP_ENDPOINT but secrets.GH_AW_DEFAULT_OTLP_HEADERS is empty. Set the headers secret at the repository, organization, or enterprise level, or clear the endpoint secret or variable to disable OTLP export."
  exit 1
fi

echo "OTLP telemetry endpoint and credentials are configured."
