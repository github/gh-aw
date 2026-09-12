#!/usr/bin/env bash
set +o histexpand
set -euo pipefail

# Collect usage artifact files into /tmp/gh-aw/usage/ for upload.
# Copies aw_info, agent/detection usage JSONL, evals, grader results, rate limits,
# and token-usage logs from the firewall sandbox directories.
#
# Token-usage files are copied in ascending priority order so the last
# non-empty source wins:
#   firewall-audit-logs/ (legacy)  →  firewall/audit/ (AWF audit)  →  firewall/logs/ (authoritative)
# The -s check (non-empty) prevents an empty stub file from zeroing out
# valid data already written by a higher-priority source.

mkdir -p /tmp/gh-aw/usage/agent /tmp/gh-aw/usage/detection

echo "Usage artifact source file status:"
for file in \
  /tmp/gh-aw/aw_info.json \
  /tmp/gh-aw/aw-info.jsonl \
  /tmp/gh-aw/agent_usage.json \
  /tmp/gh-aw/agent_usage.jsonl \
  /tmp/gh-aw/detection_usage.jsonl \
  /tmp/gh-aw/threat-detection/detection_usage.jsonl \
  /tmp/gh-aw/evals/evals.jsonl \
  /tmp/gh-aw/agent/graders/grader_manifest.json \
  /tmp/gh-aw/agent/graders/grader_results.json \
  /tmp/gh-aw/github_rate_limits.jsonl \
  /tmp/gh-aw/safe-output-items.jsonl \
  /tmp/gh-aw/sandbox/firewall-audit-logs/api-proxy-logs/token-usage.jsonl \
  /tmp/gh-aw/sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl \
  /tmp/gh-aw/sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl \
  /tmp/gh-aw/threat-detection/sandbox/firewall-audit-logs/api-proxy-logs/token-usage.jsonl \
  /tmp/gh-aw/threat-detection/sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl \
  /tmp/gh-aw/threat-detection/sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl; do
  if [ -f "$file" ]; then echo "FOUND: $file"; else echo "MISSING: $file"; fi
done

if [ -f /tmp/gh-aw/aw_info.json ]; then cp /tmp/gh-aw/aw_info.json /tmp/gh-aw/usage/aw_info.json || true; fi
if [ -f /tmp/gh-aw/aw-info.jsonl ]; then cp /tmp/gh-aw/aw-info.jsonl /tmp/gh-aw/usage/aw-info.jsonl || true; fi
if [ -f /tmp/gh-aw/agent_usage.json ]; then cp /tmp/gh-aw/agent_usage.json /tmp/gh-aw/usage/agent_usage.json || true; fi
if [ -f /tmp/gh-aw/agent_usage.jsonl ]; then cp /tmp/gh-aw/agent_usage.jsonl /tmp/gh-aw/usage/agent_usage.jsonl || true; fi
if [ -f /tmp/gh-aw/threat-detection/detection_usage.jsonl ]; then
  cp /tmp/gh-aw/threat-detection/detection_usage.jsonl /tmp/gh-aw/usage/detection_usage.jsonl || true
elif [ -f /tmp/gh-aw/detection_usage.jsonl ]; then
  cp /tmp/gh-aw/detection_usage.jsonl /tmp/gh-aw/usage/detection_usage.jsonl || true
fi
if [ -f /tmp/gh-aw/agent_execution.json ]; then cp /tmp/gh-aw/agent_execution.json /tmp/gh-aw/usage/agent/execution.json || true; fi
if [ -f /tmp/gh-aw/threat-detection/execution.json ]; then cp /tmp/gh-aw/threat-detection/execution.json /tmp/gh-aw/usage/detection/execution.json || true; fi
if [ -f /tmp/gh-aw/evals/evals.jsonl ]; then cp /tmp/gh-aw/evals/evals.jsonl /tmp/gh-aw/usage/evals.jsonl || true; fi
if [ -f /tmp/gh-aw/evals/evals_token_usage.jsonl ]; then
  mkdir -p /tmp/gh-aw/usage/evals
  cp /tmp/gh-aw/evals/evals_token_usage.jsonl /tmp/gh-aw/usage/evals/token_usage.jsonl
fi
if [ -f /tmp/gh-aw/agent/graders/grader_manifest.json ]; then mkdir -p /tmp/gh-aw/usage/graders && cp /tmp/gh-aw/agent/graders/grader_manifest.json /tmp/gh-aw/usage/graders/grader_manifest.json || true; fi
if [ -f /tmp/gh-aw/agent/graders/grader_results.json ]; then mkdir -p /tmp/gh-aw/usage/graders && cp /tmp/gh-aw/agent/graders/grader_results.json /tmp/gh-aw/usage/graders/grader_results.json || true; fi
if [ -f /tmp/gh-aw/github_rate_limits.jsonl ]; then cp /tmp/gh-aw/github_rate_limits.jsonl /tmp/gh-aw/usage/github_rate_limits.jsonl || true; fi

# Agent token usage (ascending priority — last non-empty source wins).
if [ -s /tmp/gh-aw/sandbox/firewall-audit-logs/api-proxy-logs/token-usage.jsonl ]; then cp /tmp/gh-aw/sandbox/firewall-audit-logs/api-proxy-logs/token-usage.jsonl /tmp/gh-aw/usage/agent/token_usage.jsonl || true; fi
if [ -s /tmp/gh-aw/sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl ]; then cp /tmp/gh-aw/sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl /tmp/gh-aw/usage/agent/token_usage.jsonl || true; fi
if [ -s /tmp/gh-aw/sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl ]; then cp /tmp/gh-aw/sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl /tmp/gh-aw/usage/agent/token_usage.jsonl || true; fi

# Detection token usage (ascending priority — last non-empty source wins).
if [ -s /tmp/gh-aw/threat-detection/sandbox/firewall-audit-logs/api-proxy-logs/token-usage.jsonl ]; then cp /tmp/gh-aw/threat-detection/sandbox/firewall-audit-logs/api-proxy-logs/token-usage.jsonl /tmp/gh-aw/usage/detection/token_usage.jsonl || true; fi
if [ -s /tmp/gh-aw/threat-detection/sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl ]; then cp /tmp/gh-aw/threat-detection/sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl /tmp/gh-aw/usage/detection/token_usage.jsonl || true; fi
if [ -s /tmp/gh-aw/threat-detection/sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl ]; then cp /tmp/gh-aw/threat-detection/sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl /tmp/gh-aw/usage/detection/token_usage.jsonl || true; fi

[ -f /tmp/gh-aw/usage/agent/token_usage.jsonl ] || : > /tmp/gh-aw/usage/agent/token_usage.jsonl
[ -f /tmp/gh-aw/usage/detection/token_usage.jsonl ] || : > /tmp/gh-aw/usage/detection/token_usage.jsonl

mkdir -p /tmp/gh-aw/usage/activity
node "${RUNNER_TEMP}/gh-aw/actions/generate_usage_activity_summary.cjs"
find /tmp/gh-aw/usage -type f -print | sort
