#!/bin/sh

#
# configure_git_credentials.sh - Configure Git identity, safe directory, and remote authentication
#
# Sets up Git global configuration for use in GitHub Actions workflows and the gh-aw-node
# container. Always configures git identity and safe.directory trust. Configures the remote
# URL for authentication when credentials are provided; silently skips auth when any required
# credential variable (repository, GITHUB_SERVER_URL, GITHUB_TOKEN) is absent,
# or when the current directory has no checkout/origin of its own.
#
# When a checkout manifest is present, every cross-repository checkout subdirectory it
# records is also trusted as a safe.directory so that safe-outputs handlers can run git
# inside those subdirectories without hitting "dubious ownership" errors.
#
# Required environment variables:
#   GITHUB_WORKSPACE     - Workspace directory path (for safe.directory)
#
# Optional environment variables:
#   RUNNER_TEMP              - Runner temp dir; used to locate the checkout manifest
#   GH_AW_CHECKOUT_MANIFEST  - Explicit path to the checkout manifest (overrides default)
#   GH_AW_GIT_CREDENTIALS_DEBUG - Set to true for privacy-preserving debug events;
#                                also enabled by RUNNER_DEBUG=1
#
# Optional environment variables for remote authentication:
#   GH_AW_GIT_REPOSITORY - Expected repository slug (e.g., "org/repo"); takes precedence
#                         over GITHUB_REPOSITORY, which Actions reserves for the workflow repo
#   GITHUB_REPOSITORY    - Legacy fallback for previously compiled workflows
#   GITHUB_SERVER_URL    - GitHub server URL (with or without https:// prefix)
#   GITHUB_TOKEN         - Authentication token; falls back to GIT_TOKEN
#
# Exit codes:
#   0 - Success
#   1 - Error

# External tracing can print expanded credentials before stderr redaction applies.
set +x
set +v
set -eu
export GIT_TRACE=0 GIT_TRACE_SETUP=0 GIT_TRACE_PACKET=0 GIT_TRACE_PACK_ACCESS=0
export GIT_TRACE_PACKFILE=0 GIT_TRACE_PERFORMANCE=0 GIT_TRACE_SHALLOW=0
export GIT_TRACE_CURL=0 GIT_TRACE2=0 GIT_TRACE2_EVENT=0 GIT_TRACE2_PERF=0
unset GIT_CURL_VERBOSE GIT_TRACE2_ENV_VARS GIT_TRACE2_CONFIG_PARAMS

configure_git_global() {
  if ! git config --global "$@" 2>/dev/null; then
    echo "Git global configuration failed; configuration details are omitted to protect credentials." >&2
    exit 1
  fi
}

# Configure git identity
configure_git_global user.email "github-actions[bot]@users.noreply.github.com"
configure_git_global user.name "github-actions[bot]"
configure_git_global am.keepcr true

# Trust the workspace directory to avoid "dubious ownership" errors
# when the repository is owned by a different user (e.g., in mounted containers)
if [ -n "${GITHUB_WORKSPACE:-}" ]; then
  configure_git_global --add safe.directory "${GITHUB_WORKSPACE}"
fi

# Trust cross-repository checkout directories recorded in the checkout manifest.
# Cross-repo checkouts live in subdirectories of the workspace (e.g.
# "${GITHUB_WORKSPACE}/github"), each a separate git repository whose top-level is
# not GITHUB_WORKSPACE. The safe-outputs handlers run git inside these
# subdirectories, so without trusting them git aborts with "dubious ownership"
# (surfacing as errors such as "Failed to pin branch").
MANIFEST_PATH="${GH_AW_CHECKOUT_MANIFEST:-}"
if [ -z "${MANIFEST_PATH}" ] && [ -n "${RUNNER_TEMP:-}" ]; then
  MANIFEST_PATH="${RUNNER_TEMP}/gh-aw/safeoutputs/checkout-manifest.json"
fi
if [ -n "${GITHUB_WORKSPACE:-}" ] && [ -n "${MANIFEST_PATH}" ] && [ -f "${MANIFEST_PATH}" ] && command -v node >/dev/null 2>&1; then
  GH_AW_MANIFEST_PATH="${MANIFEST_PATH}" node -e '
    const fs = require("fs");
    const path = require("path");
    const ws = process.env.GITHUB_WORKSPACE || "";
    try {
      const manifest = JSON.parse(fs.readFileSync(process.env.GH_AW_MANIFEST_PATH, "utf8"));
      if (manifest && typeof manifest === "object") {
        const seen = new Set();
        for (const entry of Object.values(manifest)) {
          if (!entry || typeof entry !== "object") continue;
          const p = typeof entry.path === "string" ? entry.path : "";
          if (!p) continue;
          if (/[\r\n\0]/.test(p)) continue;
          // Only trust paths that resolve to a location inside the workspace,
          // guarding against path traversal in a malformed/hostile manifest.
          const resolved = path.resolve(ws, p);
          if (/[\r\n\0]/.test(resolved)) continue;
          const rel = path.relative(ws, resolved);
          if (rel === "" || rel.startsWith("..") || path.isAbsolute(rel)) continue;
          if (seen.has(resolved)) continue;
          seen.add(resolved);
          process.stdout.write(resolved + "\n");
        }
      }
    } catch (_e) {
      /* ignore missing or malformed manifest */
    }
  ' 2>/dev/null | while IFS= read -r dir; do
    [ -n "${dir}" ] && configure_git_global --add safe.directory "${dir}"
  done
fi

# Configure remote URL authentication when all required credentials are present.
# Silently skips when any variable is absent (e.g., inside the safeoutputs container
# where GITHUB_SERVER_URL is intentionally not exposed).
REPO="${GH_AW_GIT_REPOSITORY-${GITHUB_REPOSITORY:-}}"
URL="${GITHUB_SERVER_URL:-}"
TOKEN="${GITHUB_TOKEN:-${GIT_TOKEN:-}}"

# Callers supply fixed events only, never repository, path, URL, or token values.
git_credentials_debug() {
  if [ "${RUNNER_DEBUG:-}" = "1" ] || [ "${GH_AW_GIT_CREDENTIALS_DEBUG:-}" = "true" ]; then
    printf 'workflow:git_credentials %s\n' "$1" >&2
  fi
}

if [ "${GH_AW_GIT_REPOSITORY+x}" = x ]; then
  git_credentials_debug "identity_source=explicit"
else
  git_credentials_debug "identity_source=legacy"
fi

# Do not discover a parent checkout when this directory has no checkout of its own.
# Git worktrees and submodules use a .git file rather than a directory.
if [ ! -d .git ] && [ ! -f .git ]; then
  git_credentials_debug "authentication_skipped=no_checkout"
  echo "Git configured with standard GitHub Actions identity" >&2
  exit 0
fi

if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  git_credentials_debug "checkout_configuration=unreadable"
  echo "Git checkout configuration could not be read; check repository ownership, safe.directory, and Git configuration. Remote URLs are omitted to protect credentials." >&2
  exit 1
fi
if ORIGINAL_URLS=$(git config --get-all remote.origin.url 2>/dev/null); then
  :
else
  CONFIG_STATUS=$?
  if [ "${CONFIG_STATUS}" -ne 1 ]; then
    git_credentials_debug "origin_configuration=unreadable"
    echo "Git checkout configuration could not be read; remote URL omitted to protect credentials." >&2
    exit 1
  fi
  git_credentials_debug "authentication_skipped=no_origin"
  echo "Git configured with standard GitHub Actions identity" >&2
  exit 0
fi

if [ "${GH_AW_GIT_REPOSITORY+x}" = x ] && [ -z "${REPO}" ]; then
  git_credentials_debug "identity_target=empty"
  echo "Git checkout identity target is empty; set a repository slug." >&2
  exit 1
fi

# Parse only unambiguous GitHub HTTPS/SSH URLs using the shell and standard
# utilities. Identity checks must also work in jobs that do not install Node.
url_matches_repository() {
  REMOTE_URL="$1"
  case "${REMOTE_URL}" in
    *[[:space:][:cntrl:]]*|*\\*|*'?'*|*'#'*) return 1 ;;
    https://*|ssh://*)
      REMOTE_PART="${REMOTE_URL#*://}"
      case "${REMOTE_PART}" in */*) ;; *) return 1 ;; esac
      REMOTE_HOST="${REMOTE_PART%%/*}"
      REMOTE_HOST="${REMOTE_HOST##*@}"
      REMOTE_REPO="${REMOTE_PART#*/}"
      case "${REMOTE_URL}" in https://*) REMOTE_HOST="${REMOTE_HOST%:443}" ;; esac
      ;;
    git@*:*)
      REMOTE_PART="${REMOTE_URL#git@}"
      REMOTE_HOST="${REMOTE_PART%%:*}"
      REMOTE_REPO="${REMOTE_PART#*:}"
      ;;
    *) return 1 ;;
  esac
  REMOTE_REPO="${REMOTE_REPO%/}"
  REMOTE_REPO="${REMOTE_REPO%.git}"
  REMOTE_ID=$(printf '%s/%s' "${REMOTE_HOST}" "${REMOTE_REPO}" | LC_ALL=C tr '[:upper:]' '[:lower:]')
  [ "${REMOTE_ID}" = "${EXPECTED_ID}" ]
}

check_repository_identity() {
  case "${REPO}" in
    *[!a-zA-Z0-9_./-]*|/*|*/|*/*/*|"" ) return 1 ;;
    */*) ;;
    *) return 1 ;;
  esac
  SERVER_HOST="${URL#https://}"
  SERVER_HOST="${SERVER_HOST%/}"
  case "${SERVER_HOST}" in
    ""|*[!a-zA-Z0-9.:-]*|:*|*: ) return 1 ;;
  esac
  EXPECTED_ID=$(printf '%s/%s' "${SERVER_HOST%:443}" "${REPO}" | LC_ALL=C tr '[:upper:]' '[:lower:]')
  for MODE in fetch push; do
    if [ "${MODE}" = push ]; then
      git_credentials_debug "effective_urls_check=push"
      REMOTE_URLS=$(git remote get-url --push --all origin 2>/dev/null) || return 1
    else
      git_credentials_debug "effective_urls_check=fetch"
      REMOTE_URLS=$(git remote get-url --all origin 2>/dev/null) || return 1
    fi
    [ -n "${REMOTE_URLS}" ] || return 1
    printf '%s\n' "${REMOTE_URLS}" | while IFS= read -r REMOTE_URL; do
      url_matches_repository "${REMOTE_URL}" || exit 1
    done || return 1
  done
}

# Never print remotes or raw git errors: either may contain credentials.
verify_repository_identity() {
  if [ "$1" = before ]; then
    git_credentials_debug "identity_check=before"
  else
    git_credentials_debug "identity_check=after"
  fi
  if ! check_repository_identity; then
    git_credentials_debug "identity_result=rejected"
    echo "Git checkout identity verification failed ${1} credential configuration; check checkout repository and origin configuration. Remote URLs are omitted to protect credentials." >&2
    return 1
  fi
  git_credentials_debug "identity_result=verified"
  echo "Git checkout identity verified ${1} credential configuration" >&2
}

# Explicit compiler targets are checked even without a token. Legacy callers
# retain the identity-only, no-checkout behavior when authentication is skipped.
if [ -n "${URL}" ] && { [ "${GH_AW_GIT_REPOSITORY+x}" = x ] || { [ -n "${REPO}" ] && [ -n "${TOKEN}" ]; }; }; then
  verify_repository_identity before || exit 1
fi

if [ -n "${REPO}" ] && [ -n "${URL}" ] && [ -n "${TOKEN}" ]; then
  git_credentials_debug "authentication=installing"
  URL_STRIPPED="${URL#https://}"
  URL_STRIPPED="${URL_STRIPPED%/}"
  ORIGINAL_URL="${ORIGINAL_URLS%%"
"*}"
  if ! git remote set-url origin "https://x-access-token:${TOKEN}@${URL_STRIPPED}/${REPO}.git" 2>/dev/null; then
    git_credentials_debug "authentication=write_failed"
    echo "Git remote authentication configuration failed; remote URL omitted to protect credentials." >&2
    exit 1
  fi
  if ! verify_repository_identity after; then
    git_credentials_debug "rollback=restoring"
    # Restore the raw value, not its potentially rewritten effective value.
    if ! git remote set-url origin "${ORIGINAL_URL}" 2>/dev/null; then
      if git config --unset-all remote.origin.url 2>/dev/null; then
        git_credentials_debug "rollback=credential_url_removed"
        echo "Git remote restoration failed; authentication URL removed." >&2
      else
        git_credentials_debug "rollback=cleanup_failed"
        echo "Git remote restoration and authentication URL removal failed; credentials may remain in Git configuration." >&2
      fi
    else
      git_credentials_debug "rollback=restored"
    fi
    exit 1
  fi
  git_credentials_debug "authentication=installed"
else
  git_credentials_debug "authentication_skipped=missing_credentials"
fi

echo "Git configured with standard GitHub Actions identity" >&2
