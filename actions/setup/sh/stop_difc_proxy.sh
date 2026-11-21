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

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HOST_STATE_FILE="${RUNNER_TEMP}/gh-aw/actions/proxy-host-state"
DIFC_PROXY_CA_CERT="/usr/local/share/ca-certificates/awmg-proxy-difc.crt"
OWNER="${GH_AW_DOCKER_OWNER:-}"
LEASE_VALIDATED=true
TEARDOWN_FAILED=false
HOST_CLEANUP_FAILED=false

if [[ "${RUNNER_ENVIRONMENT:-}" == "self-hosted" ]]; then
  GH_AW_NODE="${GH_AW_NODE:-${GH_AW_NODE_BIN:-$(command -v node 2>/dev/null || true)}}"
  if [[ -z "$GH_AW_NODE" || ! -x "$GH_AW_NODE" ]]; then
    echo "::error::Node.js is required to validate the shared-daemon lease." >&2
    LEASE_VALIDATED=false
  else
    LEASE_SCRIPT="${SCRIPT_DIR}/docker_lease.cjs"
    if [[ ! -f "$LEASE_SCRIPT" ]]; then
      LEASE_SCRIPT="${SCRIPT_DIR}/../js/docker_lease.cjs"
    fi
    if ! "$GH_AW_NODE" "$LEASE_SCRIPT" assert awmg-proxy; then
      LEASE_VALIDATED=false
    fi
  fi
fi

if [[ "${RUNNER_ENVIRONMENT:-}" == "self-hosted" && "$LEASE_VALIDATED" == "true" ]]; then
  if ! docker rm -f awmg-proxy 2>/dev/null; then
    if ! docker container ls --all --filter 'name=^/awmg-proxy$' --format '{{.ID}}' >/dev/null; then
      echo "::error::Unable to inspect Docker while stopping the DIFC proxy." >&2
      TEARDOWN_FAILED=true
    elif [[ -n "$(docker container ls --all --filter 'name=^/awmg-proxy$' --format '{{.ID}}')" ]]; then
      echo "::error::Unable to remove the owned DIFC proxy container." >&2
      TEARDOWN_FAILED=true
    fi
  fi
elif [[ "${RUNNER_ENVIRONMENT:-}" == "self-hosted" ]]; then
  TEARDOWN_FAILED=true
else
  docker rm -f awmg-proxy 2>/dev/null || true
  git remote remove proxy 2>/dev/null || true
  if [[ -f "$DIFC_PROXY_CA_CERT" ]]; then
    sudo rm -f "$DIFC_PROXY_CA_CERT" 2>/dev/null || true
    if command -v update-ca-certificates >/dev/null 2>&1; then
      sudo update-ca-certificates 2>/dev/null || true
    fi
  fi
fi

HOST_STATE_OWNED=false
if [[ -e "$HOST_STATE_FILE" || -L "$HOST_STATE_FILE" ]]; then
  if [[ -L "${RUNNER_TEMP}/gh-aw/actions" || -L "$HOST_STATE_FILE" || ! -f "$HOST_STATE_FILE" ]]; then
    echo "::error::Refusing to read an unsafe DIFC proxy host-state file." >&2
    TEARDOWN_FAILED=true
    HOST_CLEANUP_FAILED=true
  else
    IFS=$'\t' read -r state_owner ca_hash remote_added < "$HOST_STATE_FILE"
    if [[ "${RUNNER_ENVIRONMENT:-}" != "self-hosted" || "$state_owner" == "$OWNER" ]]; then
      HOST_STATE_OWNED=true
    else
      echo "::error::DIFC proxy host-state belongs to another invocation; leaving it untouched." >&2
      TEARDOWN_FAILED=true
      HOST_CLEANUP_FAILED=true
    fi
  fi
fi

if [[ "$HOST_STATE_OWNED" == "true" ]]; then
  if [[ "$remote_added" == "true" ]]; then
    expected_remote="https://localhost:18443/${GITHUB_REPOSITORY}.git"
    if [[ "$(git remote get-url proxy 2>/dev/null || true)" == "$expected_remote" ]]; then
      if ! git remote remove proxy; then
        HOST_CLEANUP_FAILED=true
        TEARDOWN_FAILED=true
      fi
    fi
  fi

  if [[ -n "$ca_hash" ]]; then
      CA_STATE_SAFE=true
      if [[ -f "$DIFC_PROXY_CA_CERT" ]]; then
        if command -v sha256sum >/dev/null 2>&1; then
          current_hash="$(sha256sum "$DIFC_PROXY_CA_CERT" | awk '{print $1}')"
        else
          current_hash="$(shasum -a 256 "$DIFC_PROXY_CA_CERT" | awk '{print $1}')"
        fi
        if [[ "$current_hash" == "$ca_hash" ]]; then
          if ! sudo rm -f "$DIFC_PROXY_CA_CERT"; then
            CA_STATE_SAFE=false
            HOST_CLEANUP_FAILED=true
          fi
        else
          echo "::error::DIFC proxy CA changed after installation; leaving the replacement certificate untouched." >&2
          CA_STATE_SAFE=false
        fi
      fi
      if [[ "$CA_STATE_SAFE" == "true" ]] && ! sudo update-ca-certificates; then
        HOST_CLEANUP_FAILED=true
        TEARDOWN_FAILED=true
      elif [[ "$CA_STATE_SAFE" == "false" ]]; then
        HOST_CLEANUP_FAILED=true
        TEARDOWN_FAILED=true
      fi
    fi

  if [[ "$HOST_CLEANUP_FAILED" == "false" ]]; then
    rm -f "$HOST_STATE_FILE"
  fi
fi

if [[ "$TEARDOWN_FAILED" == "true" ]]; then
  echo "::error::DIFC proxy teardown was incomplete; the shared-daemon lease must be retained." >&2
  exit 1
fi

echo "DIFC proxy stopped"
