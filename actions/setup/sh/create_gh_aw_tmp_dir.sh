#!/usr/bin/env bash
set +o histexpand
set -euo pipefail

# Reclaim stale root-owned /tmp/gh-aw/sandbox/firewall before creating it fresh.
# A previous AWF run can leave this directory (or its subdirectories) owned by root
# (Docker containers run as root, e.g. Squid runs as UID 13 inside a container, writing
# files to the host-mounted volume).
# setup.sh removes the entire /tmp/gh-aw tree using sudo, but if that cleanup partially
# failed (e.g. sudo was unavailable), the stale directory persists here.
# This targeted cleanup is defense-in-depth: it removes the specific directory tree that
# AWF's writeConfigs step will try to populate, so AWF can always create it fresh as the
# runner user.
# Note: the writability check covers both the firewall root AND its subdirectories (logs/,
# audit/) because Docker/Squid may own only the subdirectories when the parent was
# pre-created by the runner user on a prior clean run.
if [ -d /tmp/gh-aw/sandbox/firewall ] && \
   { [ ! -w /tmp/gh-aw/sandbox/firewall ] || \
     { [ -d /tmp/gh-aw/sandbox/firewall/logs ] && [ ! -w /tmp/gh-aw/sandbox/firewall/logs ]; } || \
     { [ -d /tmp/gh-aw/sandbox/firewall/audit ] && [ ! -w /tmp/gh-aw/sandbox/firewall/audit ]; }; }; then
  echo "Pre-flight: /tmp/gh-aw/sandbox/firewall is not writable (likely root-owned from prior AWF run); reclaiming with sudo"
  if command -v sudo >/dev/null 2>&1; then
    sudo -n rm -rf /tmp/gh-aw/sandbox/firewall 2>/dev/null || echo "::warning::sudo rm failed for /tmp/gh-aw/sandbox/firewall; AWF may fail with EACCES"
  else
    echo "::warning::sudo unavailable; cannot reclaim /tmp/gh-aw/sandbox/firewall; AWF may fail with EACCES"
  fi
fi

# Reclaim stale root-owned validation markers before the agent starts. Agents can
# run in containers as root while this post-agent validation runs as the runner
# user, so pre-creating this directory prevents marker writes from failing.
if [ -d /tmp/gh-aw/memory-validation ] && [ ! -w /tmp/gh-aw/memory-validation ]; then
  echo "Pre-flight: /tmp/gh-aw/memory-validation is not writable (likely root-owned from prior AWF run); reclaiming with sudo"
  if command -v sudo >/dev/null 2>&1; then
    sudo -n rm -rf /tmp/gh-aw/memory-validation 2>/dev/null || echo "::warning::sudo rm failed for /tmp/gh-aw/memory-validation; AWF may fail with EACCES"
  else
    echo "::warning::sudo unavailable; cannot reclaim /tmp/gh-aw/memory-validation; AWF may fail with EACCES"
  fi
fi

mkdir -p /tmp/gh-aw/agent
mkdir -p /tmp/gh-aw/memory-validation
mkdir -p /tmp/gh-aw/sandbox/agent/logs
# Pre-create the firewall sandbox dirs as the runner user (uid=1001) before AWF starts.
# If the stale directory was successfully removed above, these create fresh dirs.
# If removal failed and the dirs still exist as root-owned, mkdir -p will emit EACCES here
# rather than deep inside AWF, surfacing the problem at an earlier step.
mkdir -p /tmp/gh-aw/sandbox/firewall/logs
mkdir -p /tmp/gh-aw/sandbox/firewall/audit
echo "Created /tmp/gh-aw/agent directory for agentic workflow temporary files"
