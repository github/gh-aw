#!/usr/bin/env bash
set +o histexpand
set -euo pipefail

# Collect usage artifact files into the private runtime's usage/ for upload.
# Copies aw_info, agent/detection usage JSONL, evals, A/B experiment state,
# rate limits, token-usage logs, and model-routing logs from the firewall sandbox directories,
# so the audit command can mine experiments and evals from the usage artifact alone.
#
# Token-usage files are copied in ascending priority order so the last source
# wins:
#   firewall-audit-logs/ (legacy)  →  firewall/audit/ (AWF audit)  →  firewall/logs/ (authoritative)
# The authoritative agent source is copied even when empty so its presence is
# preserved; missing agent accounting is not synthesized below.

path_has_symlink_component() {
  local path="$1"
  while [ "$path" != "/" ]; do
    if [ -L "$path" ]; then return 0; fi
    path="$(dirname "$path")"
  done
  return 1
}

ensure_real_directory() {
  local dir="$1"
  if path_has_symlink_component "$dir"; then
    echo "::error::Refusing to use a symlinked usage-artifact directory: $dir" >&2
    return 1
  fi
  mkdir -p "$dir"
  if path_has_symlink_component "$dir" || [ ! -d "$dir" ]; then
    echo "::error::Usage-artifact path is not a real directory: $dir" >&2
    return 1
  fi
}

safe_copy() {
  local source="$1"
  local destination="$2"
  local destination_dir
  local staged_file

  if [ ! -f "$source" ] || path_has_symlink_component "$source"; then
    return 1
  fi
  destination_dir="$(dirname "$destination")"
  if [ ! -d "$destination_dir" ] || path_has_symlink_component "$destination_dir" || [ -L "$destination" ]; then
    return 1
  fi
  staged_file="$(mktemp "${destination_dir}/.usage-copy.XXXXXX")" || return 1
  if ! cp -- "$source" "$staged_file"; then
    rm -f -- "$staged_file"
    return 1
  fi
  if [ -L "$destination" ]; then
    rm -f -- "$staged_file"
    return 1
  fi
  mv -f -- "$staged_file" "$destination"
}

RUNTIME="${GH_AW_RUNTIME_DIR:?}"
ensure_real_directory "$RUNTIME"
for directory in usage usage/agent usage/detection usage/evals usage/experiment usage/activity; do
  ensure_real_directory "$RUNTIME/$directory"
done

echo "Usage artifact source file status:"
for file in \
  "$RUNTIME/aw_info.json" \
  "$RUNTIME/runtime/aw_info.json" \
  "$RUNTIME/agent/aw_info.json" \
  "$RUNTIME/aw-info.jsonl" \
  "$RUNTIME/agent_usage.json" \
  "$RUNTIME/agent_usage.jsonl" \
  "$RUNTIME/agent/harness_retry.json" \
  "$RUNTIME/threat-detection/detection_harness_retry.json" \
  "$RUNTIME/detection_usage.jsonl" \
  "$RUNTIME/threat-detection/detection_usage.jsonl" \
  "$RUNTIME/evals/evals.jsonl" \
  "$RUNTIME/evals/evals_status.json" \
  "$RUNTIME/evals/evals_token_usage.jsonl" \
  "$RUNTIME/evals/execution.json" \
  "$RUNTIME/evals/evals/execution.json" \
  "$RUNTIME/experiments/state.jsonl" \
  "$RUNTIME/experiments/state.json" \
  "$RUNTIME/experiments/assignments.json" \
  "$RUNTIME/github_rate_limits.jsonl" \
  "$RUNTIME/safe-output-items.jsonl" \
  "$RUNTIME/sandbox/firewall-audit-logs/api-proxy-logs/token-usage.jsonl" \
  "$RUNTIME/sandbox/firewall-audit-logs/api-proxy-logs/model-routing.jsonl" \
  "$RUNTIME/sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl" \
  "$RUNTIME/sandbox/firewall/logs/api-proxy-logs/model-routing.jsonl" \
  "$RUNTIME/sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl" \
  "$RUNTIME/sandbox/firewall/audit/api-proxy-logs/model-routing.jsonl" \
  "$RUNTIME/threat-detection/sandbox/firewall-audit-logs/api-proxy-logs/token-usage.jsonl" \
  "$RUNTIME/threat-detection/sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl" \
  "$RUNTIME/threat-detection/sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl"; do
  if [ -f "$file" ] && ! path_has_symlink_component "$file"; then echo "FOUND: $file"; else echo "MISSING: $file"; fi
done

if [ -f "$RUNTIME/aw_info.json" ]; then safe_copy "$RUNTIME/aw_info.json" "$RUNTIME/usage/aw_info.json" || true; fi
if [ -f "$RUNTIME/runtime/aw_info.json" ]; then safe_copy "$RUNTIME/runtime/aw_info.json" "$RUNTIME/usage/aw_info.json" || true; fi
# Agent-controlled metadata must not establish or override runtime attribution.
for file in aw-info.jsonl agent_usage.json agent_usage.jsonl github_rate_limits.jsonl; do
  if [ -f "$RUNTIME/$file" ]; then safe_copy "$RUNTIME/$file" "$RUNTIME/usage/$file" || true; fi
done
if [ -f "$RUNTIME/threat-detection/detection_usage.jsonl" ]; then
  safe_copy "$RUNTIME/threat-detection/detection_usage.jsonl" "$RUNTIME/usage/detection_usage.jsonl" || true
elif [ -f "$RUNTIME/detection_usage.jsonl" ]; then
  safe_copy "$RUNTIME/detection_usage.jsonl" "$RUNTIME/usage/detection_usage.jsonl" || true
fi
if [ -f "$RUNTIME/agent_execution.json" ]; then safe_copy "$RUNTIME/agent_execution.json" "$RUNTIME/usage/agent/execution.json" || true; fi
if [ -f "$RUNTIME/agent/harness_retry.json" ]; then safe_copy "$RUNTIME/agent/harness_retry.json" "$RUNTIME/usage/agent/harness_retry.json" || true; fi
if [ -f "$RUNTIME/agent/durable-session.jsonl" ]; then
  if ! safe_copy "$RUNTIME/agent/durable-session.jsonl" "$RUNTIME/usage/agent/durable-session.jsonl" 2>/dev/null; then
    echo "::warning::Durable session metadata could not be copied; continuing usage evidence collection."
  fi
fi
if [ -f "$RUNTIME/threat-detection/detection_harness_retry.json" ]; then safe_copy "$RUNTIME/threat-detection/detection_harness_retry.json" "$RUNTIME/usage/detection/harness_retry.json" || true; fi
if [ -f "$RUNTIME/threat-detection/execution.json" ]; then safe_copy "$RUNTIME/threat-detection/execution.json" "$RUNTIME/usage/detection/execution.json" || true; fi
# Downloaded evals artifacts can contain a nested evals/ directory.
if [ -f "$RUNTIME/evals/evals/execution.json" ]; then
  safe_copy "$RUNTIME/evals/evals/execution.json" "$RUNTIME/usage/evals/execution.json" || true
elif [ -f "$RUNTIME/evals/execution.json" ]; then
  safe_copy "$RUNTIME/evals/execution.json" "$RUNTIME/usage/evals/execution.json" || true
fi
if [ -f "$RUNTIME/evals/evals.jsonl" ]; then safe_copy "$RUNTIME/evals/evals.jsonl" "$RUNTIME/usage/evals.jsonl" || true; fi
# The not-evaluated marker is run-level diagnostic evidence, kept beside the evals
# execution evidence rather than as an answer file.
if [ -f "$RUNTIME/evals/evals_status.json" ]; then
  safe_copy "$RUNTIME/evals/evals_status.json" "$RUNTIME/usage/evals/evals_status.json" || true
fi
if [ -f "$RUNTIME/evals/evals_token_usage.jsonl" ]; then
  safe_copy "$RUNTIME/evals/evals_token_usage.jsonl" "$RUNTIME/usage/evals/token_usage.jsonl"
fi
for file in state.jsonl state.json assignments.json; do
  if [ -f "$RUNTIME/experiments/$file" ]; then safe_copy "$RUNTIME/experiments/$file" "$RUNTIME/usage/experiment/$file" || true; fi
done

# Agent token usage (ascending priority — authoritative source wins even when empty).
for layout in firewall-audit-logs firewall/audit firewall/logs; do
  source="$RUNTIME/sandbox/$layout/api-proxy-logs/token-usage.jsonl"
  if [ -s "$source" ] || { [ "$layout" = firewall/logs ] && [ -f "$source" ]; }; then
    safe_copy "$source" "$RUNTIME/usage/agent/token_usage.jsonl" || true
  fi
done

# Agent model routing (ascending priority — authoritative source wins even when empty).
for layout in firewall-audit-logs firewall/audit firewall/logs; do
  source="$RUNTIME/sandbox/$layout/api-proxy-logs/model-routing.jsonl"
  if [ -f "$source" ]; then safe_copy "$source" "$RUNTIME/usage/agent/model-routing.jsonl" || true; fi
done

# Detection token usage (ascending priority — last non-empty source wins).
for layout in firewall-audit-logs firewall/audit firewall/logs; do
  source="$RUNTIME/threat-detection/sandbox/$layout/api-proxy-logs/token-usage.jsonl"
  if [ -s "$source" ]; then safe_copy "$source" "$RUNTIME/usage/detection/token_usage.jsonl" || true; fi
done

if [ ! -e "$RUNTIME/usage/detection/token_usage.jsonl" ] && [ ! -L "$RUNTIME/usage/detection/token_usage.jsonl" ]; then
  : > "$RUNTIME/usage/detection/token_usage.jsonl"
fi

if ! command -v node >/dev/null 2>&1; then
  echo "::warning::Node was not found on PATH; usage summaries will run via actions/github-script."
fi

find "$RUNTIME/usage" -type f -print | sort
