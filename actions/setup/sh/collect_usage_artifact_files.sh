#!/usr/bin/env bash
set +o histexpand
set -euo pipefail

# Collect usage artifact files into /tmp/gh-aw/usage/ for upload.
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

ensure_real_directory /tmp/gh-aw
ensure_real_directory /tmp/gh-aw/usage
ensure_real_directory /tmp/gh-aw/usage/agent
ensure_real_directory /tmp/gh-aw/usage/detection
ensure_real_directory /tmp/gh-aw/usage/evals
ensure_real_directory /tmp/gh-aw/usage/experiment
ensure_real_directory /tmp/gh-aw/usage/activity

echo "Usage artifact source file status:"
for file in \
  /tmp/gh-aw/aw_info.json \
  /tmp/gh-aw/runtime/aw_info.json \
  /tmp/gh-aw/agent/aw_info.json \
  /tmp/gh-aw/aw-info.jsonl \
  /tmp/gh-aw/agent_usage.json \
  /tmp/gh-aw/agent_usage.jsonl \
  /tmp/gh-aw/agent/harness_retry.json \
  /tmp/gh-aw/threat-detection/detection_harness_retry.json \
  /tmp/gh-aw/detection_usage.jsonl \
  /tmp/gh-aw/threat-detection/detection_usage.jsonl \
  /tmp/gh-aw/evals/evals.jsonl \
  /tmp/gh-aw/evals/evals_status.json \
  /tmp/gh-aw/evals/evals_token_usage.jsonl \
  /tmp/gh-aw/evals/execution.json \
  /tmp/gh-aw/evals/evals/execution.json \
  /tmp/gh-aw/experiments/state.jsonl \
  /tmp/gh-aw/experiments/state.json \
  /tmp/gh-aw/experiments/assignments.json \
  /tmp/gh-aw/github_rate_limits.jsonl \
  /tmp/gh-aw/safe-output-items.jsonl \
  /tmp/gh-aw/sandbox/firewall-audit-logs/api-proxy-logs/token-usage.jsonl \
  /tmp/gh-aw/sandbox/firewall-audit-logs/api-proxy-logs/model-routing.jsonl \
  /tmp/gh-aw/sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl \
  /tmp/gh-aw/sandbox/firewall/logs/api-proxy-logs/model-routing.jsonl \
  /tmp/gh-aw/sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl \
  /tmp/gh-aw/sandbox/firewall/audit/api-proxy-logs/model-routing.jsonl \
  /tmp/gh-aw/threat-detection/sandbox/firewall-audit-logs/api-proxy-logs/token-usage.jsonl \
  /tmp/gh-aw/threat-detection/sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl \
  /tmp/gh-aw/threat-detection/sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl; do
  if [ -f "$file" ] && ! path_has_symlink_component "$file"; then echo "FOUND: $file"; else echo "MISSING: $file"; fi
done

if [ -f /tmp/gh-aw/aw_info.json ]; then safe_copy /tmp/gh-aw/aw_info.json /tmp/gh-aw/usage/aw_info.json || true; fi
if [ -f /tmp/gh-aw/runtime/aw_info.json ]; then safe_copy /tmp/gh-aw/runtime/aw_info.json /tmp/gh-aw/usage/aw_info.json || true; fi
if [ -f /tmp/gh-aw/agent/aw_info.json ]; then safe_copy /tmp/gh-aw/agent/aw_info.json /tmp/gh-aw/usage/aw_info.json || true; fi
if [ -f /tmp/gh-aw/aw-info.jsonl ]; then safe_copy /tmp/gh-aw/aw-info.jsonl /tmp/gh-aw/usage/aw-info.jsonl || true; fi
if [ -f /tmp/gh-aw/agent_usage.json ]; then safe_copy /tmp/gh-aw/agent_usage.json /tmp/gh-aw/usage/agent_usage.json || true; fi
if [ -f /tmp/gh-aw/agent_usage.jsonl ]; then safe_copy /tmp/gh-aw/agent_usage.jsonl /tmp/gh-aw/usage/agent_usage.jsonl || true; fi
if [ -f /tmp/gh-aw/threat-detection/detection_usage.jsonl ]; then
  safe_copy /tmp/gh-aw/threat-detection/detection_usage.jsonl /tmp/gh-aw/usage/detection_usage.jsonl || true
elif [ -f /tmp/gh-aw/detection_usage.jsonl ]; then
  safe_copy /tmp/gh-aw/detection_usage.jsonl /tmp/gh-aw/usage/detection_usage.jsonl || true
fi
if [ -f /tmp/gh-aw/agent_execution.json ]; then safe_copy /tmp/gh-aw/agent_execution.json /tmp/gh-aw/usage/agent/execution.json || true; fi
if [ -f /tmp/gh-aw/agent/harness_retry.json ]; then safe_copy /tmp/gh-aw/agent/harness_retry.json /tmp/gh-aw/usage/agent/harness_retry.json || true; fi
if [ -f /tmp/gh-aw/agent/durable-session.jsonl ]; then
  if ! safe_copy /tmp/gh-aw/agent/durable-session.jsonl /tmp/gh-aw/usage/agent/durable-session.jsonl 2>/dev/null; then
    echo "::warning::Durable session metadata could not be copied; continuing usage evidence collection."
  fi
fi
if [ -f /tmp/gh-aw/threat-detection/detection_harness_retry.json ]; then safe_copy /tmp/gh-aw/threat-detection/detection_harness_retry.json /tmp/gh-aw/usage/detection/harness_retry.json || true; fi
if [ -f /tmp/gh-aw/threat-detection/execution.json ]; then safe_copy /tmp/gh-aw/threat-detection/execution.json /tmp/gh-aw/usage/detection/execution.json || true; fi
# The evals artifact is rooted at /tmp/gh-aw/ in the evals job, so its execution evidence
# lands under evals/evals/ when downloaded into /tmp/gh-aw/evals/.
if [ -f /tmp/gh-aw/evals/evals/execution.json ]; then
  safe_copy /tmp/gh-aw/evals/evals/execution.json /tmp/gh-aw/usage/evals/execution.json || true
elif [ -f /tmp/gh-aw/evals/execution.json ]; then
  safe_copy /tmp/gh-aw/evals/execution.json /tmp/gh-aw/usage/evals/execution.json || true
fi
if [ -f /tmp/gh-aw/evals/evals.jsonl ]; then safe_copy /tmp/gh-aw/evals/evals.jsonl /tmp/gh-aw/usage/evals.jsonl || true; fi
# The not-evaluated marker is run-level diagnostic evidence, kept beside the evals
# execution evidence rather than as an answer file.
if [ -f /tmp/gh-aw/evals/evals_status.json ]; then
  safe_copy /tmp/gh-aw/evals/evals_status.json /tmp/gh-aw/usage/evals/evals_status.json || true
fi
if [ -f /tmp/gh-aw/evals/evals_token_usage.jsonl ]; then
  safe_copy /tmp/gh-aw/evals/evals_token_usage.jsonl /tmp/gh-aw/usage/evals/token_usage.jsonl
fi
for file in state.jsonl state.json assignments.json; do
  if [ -f "/tmp/gh-aw/experiments/$file" ]; then safe_copy "/tmp/gh-aw/experiments/$file" "/tmp/gh-aw/usage/experiment/$file" || true; fi
done
if [ -f /tmp/gh-aw/github_rate_limits.jsonl ]; then safe_copy /tmp/gh-aw/github_rate_limits.jsonl /tmp/gh-aw/usage/github_rate_limits.jsonl || true; fi

# Agent token usage (ascending priority — authoritative source wins even when empty).
if [ -s /tmp/gh-aw/sandbox/firewall-audit-logs/api-proxy-logs/token-usage.jsonl ]; then safe_copy /tmp/gh-aw/sandbox/firewall-audit-logs/api-proxy-logs/token-usage.jsonl /tmp/gh-aw/usage/agent/token_usage.jsonl || true; fi
if [ -s /tmp/gh-aw/sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl ]; then safe_copy /tmp/gh-aw/sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl /tmp/gh-aw/usage/agent/token_usage.jsonl || true; fi
if [ -f /tmp/gh-aw/sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl ]; then safe_copy /tmp/gh-aw/sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl /tmp/gh-aw/usage/agent/token_usage.jsonl || true; fi

# Agent model routing (ascending priority — authoritative source wins even when empty).
if [ -f /tmp/gh-aw/sandbox/firewall-audit-logs/api-proxy-logs/model-routing.jsonl ]; then safe_copy /tmp/gh-aw/sandbox/firewall-audit-logs/api-proxy-logs/model-routing.jsonl /tmp/gh-aw/usage/agent/model-routing.jsonl || true; fi
if [ -f /tmp/gh-aw/sandbox/firewall/audit/api-proxy-logs/model-routing.jsonl ]; then safe_copy /tmp/gh-aw/sandbox/firewall/audit/api-proxy-logs/model-routing.jsonl /tmp/gh-aw/usage/agent/model-routing.jsonl || true; fi
if [ -f /tmp/gh-aw/sandbox/firewall/logs/api-proxy-logs/model-routing.jsonl ]; then safe_copy /tmp/gh-aw/sandbox/firewall/logs/api-proxy-logs/model-routing.jsonl /tmp/gh-aw/usage/agent/model-routing.jsonl || true; fi

# Detection token usage (ascending priority — last non-empty source wins).
if [ -s /tmp/gh-aw/threat-detection/sandbox/firewall-audit-logs/api-proxy-logs/token-usage.jsonl ]; then safe_copy /tmp/gh-aw/threat-detection/sandbox/firewall-audit-logs/api-proxy-logs/token-usage.jsonl /tmp/gh-aw/usage/detection/token_usage.jsonl || true; fi
if [ -s /tmp/gh-aw/threat-detection/sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl ]; then safe_copy /tmp/gh-aw/threat-detection/sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl /tmp/gh-aw/usage/detection/token_usage.jsonl || true; fi
if [ -s /tmp/gh-aw/threat-detection/sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl ]; then safe_copy /tmp/gh-aw/threat-detection/sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl /tmp/gh-aw/usage/detection/token_usage.jsonl || true; fi

if [ ! -e /tmp/gh-aw/usage/detection/token_usage.jsonl ] && [ ! -L /tmp/gh-aw/usage/detection/token_usage.jsonl ]; then
  : > /tmp/gh-aw/usage/detection/token_usage.jsonl
fi

if ! command -v node >/dev/null 2>&1; then
  echo "::warning::Node was not found on PATH; usage summaries will run via actions/github-script."
fi

find /tmp/gh-aw/usage -type f -print | sort
