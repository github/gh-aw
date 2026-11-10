#!/usr/bin/env bash
set +o histexpand

# Sourced by installers from the trusted setup action, never from the download origin.
# Review new pins against authenticated release provenance before committing them.
# Add pins with scripts/update-release-pins.sh using an independently trusted manifest SHA256.
release_staging_identity() {
  stat -c '%d:%i:%u' "$1" 2>/dev/null || stat -f '%d:%i:%u' "$1"
}

cleanup_release_staging() {
  if [ ! -e "$TEMP_DIR" ] && [ ! -L "$TEMP_DIR" ]; then
    return 0
  fi
  # Keep the ownership evidence in the installer process, not in mutable staging.
  if [ -L "$TEMP_DIR" ] || [ ! -d "$TEMP_DIR" ] || [ ! -O "$TEMP_DIR" ] ||
    [ "$(release_staging_identity "$TEMP_DIR")" != "$GH_AW_RELEASE_STAGING_ID" ]; then
    echo "Warning: refusing to clean replaced installer staging directory: $TEMP_DIR" >&2
    return 0
  fi
  if ! rm -rf -- "$TEMP_DIR"; then
    echo "Warning: unable to reclaim owned installer staging directory without elevated permissions: $TEMP_DIR" >&2
  fi
}

create_release_staging() {
  local prefix="$1" parent
  if [[ ! "$prefix" =~ ^[a-z][a-z0-9-]*$ ]]; then
    echo "ERROR: Invalid release staging prefix" >&2
    return 1
  fi
  parent="${RUNNER_TEMP:-${TMPDIR:-/tmp}}"
  if command -v cygpath >/dev/null 2>&1; then
    parent="$(cygpath -u "$parent")" || return 1
  fi
  if ! parent="$(cd "$parent" && pwd -P)"; then
    if [[ -n "${RUNNER_TEMP:-}" ]]; then
      return 1
    fi
    parent="$(cd /tmp && pwd -P)" || return 1
  fi
  if ! TEMP_DIR="$(umask 077; mktemp -d "${parent}/${prefix}-install-XXXXXX")"; then
    if [[ -z "${RUNNER_TEMP:-}" && "$parent" != "$(cd /tmp && pwd -P)" ]]; then
      parent="$(cd /tmp && pwd -P)" || return 1
      TEMP_DIR="$(umask 077; mktemp -d "${parent}/${prefix}-install-XXXXXX")" || {
        echo "ERROR: Could not allocate release staging under $parent" >&2
        return 1
      }
    else
      echo "ERROR: Could not allocate release staging under $parent" >&2
      return 1
    fi
  fi
  readonly TEMP_DIR
  if ! GH_AW_RELEASE_STAGING_ID="$(release_staging_identity "$TEMP_DIR")"; then
    echo "Warning: could not record ownership; leaving installer staging directory untouched: $TEMP_DIR" >&2
    return 1
  fi
  readonly GH_AW_RELEASE_STAGING_ID
  trap cleanup_release_staging EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM
  trap 'exit 129' HUP
}

create_awf_runtime_tmp() {
  local runtime_parent runtime_dir runtime_id
  if [[ -z "${GITHUB_ENV:-}" ]]; then
    return 0
  fi
  if [[ -n "${GH_AW_RUNTIME_TMP_DIR:-}" ]]; then
    if [[ ! -d "$GH_AW_RUNTIME_TMP_DIR" || -L "$GH_AW_RUNTIME_TMP_DIR" || ! -O "$GH_AW_RUNTIME_TMP_DIR" ||
      "$(release_staging_identity "$GH_AW_RUNTIME_TMP_DIR")" != "${GH_AW_RUNTIME_TMP_ID:-}" ]]; then
      echo "ERROR: Existing AWF runtime temp directory is not owned by this invocation" >&2
      return 1
    fi
    export TMPDIR="$GH_AW_RUNTIME_TMP_DIR"
    return 0
  fi
  runtime_parent="$(cd /tmp && pwd -P)" || return 1
  if ! runtime_dir="$(umask 077; mktemp -d "${runtime_parent}/gh-aw-runtime-XXXXXX")"; then
    echo "ERROR: Could not allocate private AWF runtime temp directory under $runtime_parent" >&2
    return 1
  fi
  runtime_id="$(release_staging_identity "$runtime_dir")" || {
    rm -rf -- "$runtime_dir"
    return 1
  }
  if ! printf 'GH_AW_RUNTIME_TMP_DIR=%s\nGH_AW_RUNTIME_TMP_ID=%s\nTMPDIR=%s\n' \
    "$runtime_dir" "$runtime_id" "$runtime_dir" >>"$GITHUB_ENV"; then
    rm -rf -- "$runtime_dir"
    echo "ERROR: Could not persist private AWF runtime temp directory" >&2
    return 1
  fi
  export GH_AW_RUNTIME_TMP_DIR="$runtime_dir"
  export GH_AW_RUNTIME_TMP_ID="$runtime_id"
  export TMPDIR="$runtime_dir"
}

cleanup_awf_runtime_tmp() {
  local runtime_dir="${GH_AW_RUNTIME_TMP_DIR:-}" runtime_id="${GH_AW_RUNTIME_TMP_ID:-}" runtime_parent
  if [[ -z "$runtime_dir" ]]; then
    return 0
  fi
  runtime_parent="$(cd /tmp && pwd -P)" || return 0
  if [[ "${runtime_dir%/*}" != "$runtime_parent" || "${runtime_dir##*/}" != gh-aw-runtime-* ]]; then
    echo "Warning: refusing to clean an AWF runtime temp path outside its private temp root: $runtime_dir" >&2
    return 0
  fi
  if [[ ! -e "$runtime_dir" && ! -L "$runtime_dir" ]]; then
    return 0
  fi
  if [[ -L "$runtime_dir" || ! -d "$runtime_dir" || ! -O "$runtime_dir" ||
    "$(release_staging_identity "$runtime_dir")" != "$runtime_id" ]]; then
    echo "Warning: refusing to clean a replaced or unowned AWF runtime temp directory: $runtime_dir" >&2
    return 0
  fi
  if ! rm -rf -- "$runtime_dir"; then
    echo "Warning: unable to reclaim owned AWF runtime temp directory without elevated permissions: $runtime_dir" >&2
  fi
}

# Each entry binds repository, immutable release version, asset name, and SHA256.
# The optional fourth argument is a literal pin from trusted workflow frontmatter.
release_artifact_sha256() {
  local repository="$1" version="$2" asset="$3" digest="${4:-}"
  version="v${version#v}"
  if [ -z "$digest" ]; then
    digest=$(awk -v repository="$repository" -v version="$version" -v asset="$asset" '
    $1 == repository && $2 == version && $3 == asset { count++; digest = $4 }
    END { if (count == 1) print digest; else exit 1 }
  ' <<'PINS'
github/gh-aw-firewall v0.28.62 checksums.txt 0b03f6ea5b638356a799fa541ade4fd7902a6ac8d7c3f0252ee09d8259a4ce8f
github/copilot-cli v1.0.98 SHA256SUMS.txt 372f0c8d3d50076455cd19ef080dc59dc2a32401d8b2d4f77ce190c8ab13bc59
github/copilot-cli v1.0.21 SHA256SUMS.txt fd9b2fb92a39aeb2a41815a6aa0bf3853307c812b3451a45f5a2217be003987d
github/gh-aw-firewall v0.28.70 checksums.txt 9a35879a2f405fa7a1aa275a67b96d01eb0d0c6c8fd850637a1ad8ad55b500da
github/copilot-cli v1.0.101 SHA256SUMS.txt 018625eac68670d66ede904b9b82cdcd6ab8f87ba20e6e1fbdf0b62c8295d5be
github/gh-aw-firewall v0.29.0 checksums.txt 2d49727b3b65630b5d0ba5375168687a852afac319d05c911d4769c43fa2ed61
github/gh-aw-firewall v0.29.5 checksums.txt df77f6dd216030f6c604839136ccfed07b922690594db518a83ff3bcc555e788
github/copilot-cli v1.0.103 SHA256SUMS.txt 26cd3e4b32cde498e4961fb4d48e7b85d7859fc69af1a645a19201fdd577bdb2
github/gh-aw-firewall v0.29.6 checksums.txt b8c184739fe435513e91b8736417d12d3901205f63bc9587e656a3cffb4c59f1
github/gh-aw-firewall v0.29.7 checksums.txt 10896644ecf2f7305299458d61ad5194cba825dff537d63a5c194b80f5746bd5
github/gh-aw-firewall v0.29.10 checksums.txt a60c61e67aad4810e9a4f52ae26d3b2a58a7cf190aff66aeb338238b7b0e1467
github/gh-aw-firewall v0.29.11 checksums.txt d520f7716e3ee942490e1823b0f6c73b5933172961115bf702d22850d8a6f527
PINS
    ) || {
      local config_field="engine"
      if [ "$repository" = github/gh-aw-firewall ]; then
        config_field="sandbox.agent"
      fi
      echo "ERROR: No reviewed SHA256 pin for ${repository} ${version} ${asset}; refusing to install an unverified release." >&2
      echo "Fix: run 'gh extension upgrade gh-aw', then 'gh aw compile <workflow.md>' and commit the regenerated .lock.yml. Ensure the workflow uses a setup action revision containing this release pin." >&2
      echo "For an intentional release override, set ${config_field}.version to a literal release version and ${config_field}.checksums-sha256 to the independently authenticated SHA256 of ${asset} (not the CLI archive)." >&2
      echo "Maintainers: add the reviewed pin with 'bash scripts/update-release-pins.sh ${repository} ${version} ${asset} <trusted-manifest-sha256>'; do not trust a digest solely because it was downloaded alongside the executable." >&2
      return 1
    }
  fi
  if [[ ! "$digest" =~ ^[0-9a-f]{64}$ ]]; then
    echo "ERROR: Invalid reviewed SHA256 pin for ${repository} ${version} ${asset}" >&2
    return 1
  fi
  printf '%s\n' "$digest"
}

verify_release_digest() {
  local file="$1" expected="$2" actual
  if [[ ! "$expected" =~ ^[0-9a-f]{64}$ ]]; then
    echo "ERROR: A trusted lowercase SHA256 digest is required" >&2
    return 1
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    actual=$(sha256sum "$file") || return 1
  elif command -v shasum >/dev/null 2>&1; then
    actual=$(shasum -a 256 "$file") || return 1
  else
    echo "ERROR: No sha256sum or shasum found" >&2
    return 1
  fi
  actual="${actual%% *}"
  if [ "$actual" != "$expected" ]; then
    echo "ERROR: Trusted SHA256 verification failed for $(basename "$file"); refusing installation" >&2
    return 1
  fi
}

# Exit status for a verification deferred by GitHub API quota exhaustion. It is
# never success: the release stays unverified and nothing is installed.
RELEASE_QUOTA_EXHAUSTED_STATUS=75
RELEASE_QUOTA_WAITED_SECONDS=0

release_quota_iso_time() {
  local epoch="$1"
  [[ "$epoch" =~ ^[0-9]{1,12}$ ]] || { printf 'unknown'; return 0; }
  date -u -d "@${epoch}" '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null || date -u -r "$epoch" '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null || printf '%s' "$epoch"
}

# Read the quota headers of the same attestation endpoint with the same
# credential as the failed verification. Only validated header values are kept.
release_quota_probe() {
  local file="$1" repository="$2" digest response line name value
  RELEASE_QUOTA_STATUS="" RELEASE_QUOTA_RESOURCE="unknown" RELEASE_QUOTA_LIMIT="" RELEASE_QUOTA_REMAINING="" RELEASE_QUOTA_USED="" RELEASE_QUOTA_RESET="" RELEASE_QUOTA_RETRY_AFTER=""
  if command -v sha256sum >/dev/null 2>&1; then
    digest=$(sha256sum "$file") || return 1
  else
    digest=$(shasum -a 256 "$file") || return 1
  fi
  digest="${digest%% *}"
  [[ "$digest" =~ ^[0-9a-f]{64}$ ]] || return 1
  response=$(gh api --hostname github.com --include "repos/${repository}/attestations/sha256:${digest}" 2>/dev/null) || true
  while IFS= read -r line; do
    line="${line%$'\r'}"
    [ -z "$line" ] && [ -n "$RELEASE_QUOTA_STATUS" ] && break
    if [[ "$line" =~ ^HTTP/[0-9.]+\ ([0-9]{3}) ]]; then
      RELEASE_QUOTA_STATUS="${BASH_REMATCH[1]}"
      continue
    fi
    name="${line%%:*}"
    value="${line#*:}"
    value="${value#"${value%%[![:space:]]*}"}"
    case "${name,,}" in
      x-ratelimit-resource) [[ "$value" =~ ^[a-z_]{1,64}$ ]] && RELEASE_QUOTA_RESOURCE="$value" ;;
      x-ratelimit-limit) [[ "$value" =~ ^[0-9]{1,16}$ ]] && RELEASE_QUOTA_LIMIT="$value" ;;
      x-ratelimit-remaining) [[ "$value" =~ ^[0-9]{1,16}$ ]] && RELEASE_QUOTA_REMAINING="$value" ;;
      x-ratelimit-used) [[ "$value" =~ ^[0-9]{1,16}$ ]] && RELEASE_QUOTA_USED="$value" ;;
      x-ratelimit-reset) [[ "$value" =~ ^[0-9]{1,12}$ ]] && RELEASE_QUOTA_RESET="$value" ;;
      retry-after) [[ "$value" =~ ^[0-9]{1,8}$ ]] && RELEASE_QUOTA_RETRY_AFTER="$value" ;;
    esac
  done <<< "$response"
  return 0
}

report_release_quota_blocked() {
  local file="$1" reason="$2" reset_iso details
  reset_iso=$(release_quota_iso_time "$RELEASE_QUOTA_RESET")
  details="resource=${RELEASE_QUOTA_RESOURCE}; limit=${RELEASE_QUOTA_LIMIT:-unknown}; remaining=${RELEASE_QUOTA_REMAINING:-unknown}; used=${RELEASE_QUOTA_USED:-unknown}; reset=${reset_iso}; reason=${reason}"
  echo "ERROR: GitHub API quota exhausted during release attestation verification for $(basename "$file") (HTTP ${RELEASE_QUOTA_STATUS:-unknown}; ${details}). The release was not verified or installed." >&2
  if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
    {
      echo "### GitHub API quota exhausted"
      echo ""
      echo "| Field | Value |"
      echo "| --- | --- |"
      echo "| Stage | Release attestation verification ($(basename "$file")) |"
      echo "| HTTP status | ${RELEASE_QUOTA_STATUS:-unknown} |"
      echo "| Resource | ${RELEASE_QUOTA_RESOURCE} |"
      echo "| Limit | ${RELEASE_QUOTA_LIMIT:-unknown} |"
      echo "| Remaining | ${RELEASE_QUOTA_REMAINING:-unknown} |"
      echo "| Used | ${RELEASE_QUOTA_USED:-unknown} |"
      echo "| Reset | ${reset_iso} |"
      echo "| Reason | ${reason} |"
      echo ""
      echo "Attestation verification is required and was not bypassed; nothing was installed. Dependent execution is blocked until a later run after the reset."
      echo ""
    } >> "$GITHUB_STEP_SUMMARY" || true
  fi
  if [ -n "${GITHUB_ENV:-}" ]; then
    printf 'GH_AW_GITHUB_API_QUOTA_BLOCKED=%s\n' "$details" >> "$GITHUB_ENV" || true
  fi
}

# gh attestation verify is the required provenance check. On a genuine quota
# 403/429 (remaining=0 or Retry-After), wait until the server's own reset within
# GH_AW_GITHUB_API_QUOTA_MAX_WAIT_SECONDS (default 300, cumulative for this
# installer) and verify again. Never skip verification; other failures keep
# their original status.
verify_release_attestation() {
  local file="$1" repository="$2" output status attempt now deadline delay reason max_wait
  max_wait="${GH_AW_GITHUB_API_QUOTA_MAX_WAIT_SECONDS:-300}"
  [[ "$max_wait" =~ ^[0-9]{1,6}$ ]] || max_wait=300
  for attempt in 1 2 3; do
    status=0
    output=$(gh attestation verify "$file" --hostname github.com \
      --repo "$repository" \
      --signer-workflow "${repository}/.github/workflows/release.yml" \
      --cert-oidc-issuer https://token.actions.githubusercontent.com \
      --deny-self-hosted-runners 2>&1) || status=$?
    [ -n "$output" ] && printf '%s\n' "$output"
    [ "$status" -eq 0 ] && return 0
    if ! grep -qiE 'rate limit|HTTP 429' <<< "$output"; then
      return "$status"
    fi
    release_quota_probe "$file" "$repository" || return "$status"
    if [ "$RELEASE_QUOTA_STATUS" != 403 ] && [ "$RELEASE_QUOTA_STATUS" != 429 ]; then
      if [[ "$RELEASE_QUOTA_STATUS" =~ ^2 ]] && [ "$attempt" -lt 3 ]; then
        continue
      fi
      return "$status"
    fi
    if [ "$RELEASE_QUOTA_REMAINING" != 0 ] && [ -z "$RELEASE_QUOTA_RETRY_AFTER" ]; then
      # A rate-limit response without exhaustion metadata is not established quota exhaustion.
      return "$status"
    fi
    now=$(date +%s)
    deadline=""
    if [ "$RELEASE_QUOTA_REMAINING" = 0 ] && [ -n "$RELEASE_QUOTA_RESET" ] && [ "$RELEASE_QUOTA_RESET" -gt "$now" ]; then
      deadline="$RELEASE_QUOTA_RESET"
    fi
    if [ -n "$RELEASE_QUOTA_RETRY_AFTER" ] && { [ -z "$deadline" ] || [ $((now + RELEASE_QUOTA_RETRY_AFTER)) -gt "$deadline" ]; }; then
      deadline=$((now + RELEASE_QUOTA_RETRY_AFTER))
    fi
    reason=""
    if [ "$attempt" -ge 2 ]; then
      reason="renewed_exhaustion"
    elif [ -z "$deadline" ]; then
      reason="unknown_reset"
    else
      delay=$((deadline - now + 1))
      if [ $((RELEASE_QUOTA_WAITED_SECONDS + delay)) -gt "$max_wait" ]; then
        reason="reset_exceeds_budget"
      fi
    fi
    if [ -n "$reason" ]; then
      report_release_quota_blocked "$file" "$reason"
      return "$RELEASE_QUOTA_EXHAUSTED_STATUS"
    fi
    echo "GitHub API quota exhausted during attestation verification for $(basename "$file"); waiting ${delay}s for the server reset at $(release_quota_iso_time "$deadline") before verifying again." >&2
    sleep "$delay"
    RELEASE_QUOTA_WAITED_SECONDS=$((RELEASE_QUOTA_WAITED_SECONDS + delay))
  done
  return "$status"
}

verify_release_identity() {
  local checksums_file="$1" identity_file="$2" repository="$3" version="$4" digest="$5"
  verify_release_digest "$checksums_file" "$digest" || return 1
  verify_release_attestation "$identity_file" "$repository" || return
  if [ "$(cat "$identity_file")" != "${digest}  refs/tags/v${version#v}" ]; then
    echo "ERROR: Release identity does not bind checksums.txt to refs/tags/v${version#v}" >&2
    return 1
  fi
  verify_release_attestation "$checksums_file" "$repository"
}
