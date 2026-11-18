#!/usr/bin/env bash
set +o histexpand

# Stop DIFC proxy for pre-agent gh CLI steps
# This script stops the awmg proxy container and removes the proxy CA certificate
# from the system trust store (if it was installed).
# The proxy must be stopped before the MCP gateway starts to avoid double-filtering traffic.
#
# This script does NOT modify $GITHUB_ENV. The proxy routing env vars (GH_HOST,
# GITHUB_API_URL, etc.) are injected as step-level env by the compiler and are
# never written to $GITHUB_ENV, so no restore/clear is needed here.

set -e

docker rm -f awmg-proxy 2>/dev/null || true
git remote remove proxy 2>/dev/null || true

echo "DIFC proxy stopped"
