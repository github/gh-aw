#!/usr/bin/env bash
set +o histexpand

#
# mask_otlp_headers.sh - Mask OTEL_EXPORTER_OTLP_HEADERS from GitHub Actions logs
#
# Issues the ::add-mask:: workflow command for OTEL_EXPORTER_OTLP_HEADERS so that
# authentication tokens in the header value do not leak into GitHub Actions runner
# logs (including debug/step-debug logs).
#
# When GH_AW_OTLP_ALL_HEADERS is set (multi-endpoint configuration), the same
# masking is applied to all endpoint headers combined in that variable.
#
# Up to three levels of masking are applied to each headers string:
#   1. The entire comma-separated header pairs string.
#   2. Each individual header value extracted from the pairs, so that a token
#      appearing without its header name prefix is also redacted.
#   3. For Authorization-style "Bearer <token>" credentials, the raw token after
#      stripping the "Bearer " scheme prefix, so it is masked even when it appears
#      without the scheme (e.g. in downstream tool logs).
# Values shorter than MIN_MASK_LENGTH are skipped at all levels.
#
# Mixed quoting ('::add-mask::' followed by "$VAR") is used so the directive prefix
# is treated as a literal string while the variable values are expanded at runtime.
#
# Exit codes:
#   0 - Success (variables may be empty, which is a no-op)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/mask_otlp_common.sh"

# emit_mask emits ::add-mask:: only for non-empty values at or above MIN_MASK_LENGTH.
emit_mask() {
  local _value="${1:-}"
  [ -z "$_value" ] && return
  [ "${#_value}" -lt "$MIN_MASK_LENGTH" ] && return
  echo '::add-mask::'"$_value"
}

# mask_headers masks all values in a comma-separated key=value headers string.
mask_headers() {
  local _headers="$1"
  local -a _pairs
  local _pair _val _no_bearer
  [ -z "$_headers" ] && return

  # Level 1: mask the entire comma-separated headers string.
  emit_mask "$_headers"

  # Levels 2 & 3: split on commas, extract each value, and mask it individually.
  # For "Bearer <token>" values, also mask the raw token without the scheme prefix.
  # Use mapfile rather than a pipeline so the loop completion doesn't trip errexit.
  mapfile -t _pairs < <(printf '%s' "$_headers" | tr ',' '\n')
  for _pair in "${_pairs[@]}"; do
    _val="${_pair#*=}"
    emit_mask "$_val"
    _no_bearer="${_val#Bearer }"
    if [ "$_no_bearer" != "$_val" ]; then
      emit_mask "$_no_bearer"
    fi
  done
}

# Mask primary single-endpoint headers (backward compat).
mask_headers "${OTEL_EXPORTER_OTLP_HEADERS:-}"

# Mask all-endpoints combined headers when set (multi-endpoint configuration).
# GH_AW_OTLP_ALL_HEADERS contains comma-joined headers from all endpoints,
# ensuring tokens from additional endpoints are also redacted.
if [ -n "${GH_AW_OTLP_ALL_HEADERS:-}" ]; then
  mask_headers "$GH_AW_OTLP_ALL_HEADERS"
fi
