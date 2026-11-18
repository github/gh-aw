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

if [[ "${RUNNER_ENVIRONMENT:-}" == "self-hosted" ]]; then
  # shellcheck source=assert_docker_lease.sh
  source "$(dirname "${BASH_SOURCE[0]}")/assert_docker_lease.sh" awmg-proxy
fi

docker rm -f awmg-proxy 2>/dev/null || true
git remote remove proxy 2>/dev/null || true

# Remove the DIFC proxy CA certificate from the system trust store if present,
# to avoid permanently expanding the trusted root set on persistent/self-hosted runners.
DIFC_PROXY_CA_CERT="/usr/local/share/ca-certificates/awmg-proxy-difc.crt"
if [ -f "$DIFC_PROXY_CA_CERT" ]; then
  rm -f "$DIFC_PROXY_CA_CERT" || true
  if command -v update-ca-certificates >/dev/null 2>&1; then
    sudo update-ca-certificates || true
  fi
fi

echo "DIFC proxy stopped"
