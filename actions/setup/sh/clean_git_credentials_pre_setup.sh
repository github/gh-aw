#!/usr/bin/env bash
# clean_git_credentials_pre_setup.sh - Checkout-time credential cleanup fallback
#
# Used only before Setup Scripts has copied runtime helpers to
# ${RUNNER_TEMP}/gh-aw/actions. Unlike clean_git_credentials.sh, this script runs
# from a pre-setup helper path (runner temp bundle or repository workspace).
set -euo pipefail
# Disable history expansion so values containing "!" are handled safely.
set +o histexpand

git_configs_processed=0
search_roots=("${GITHUB_WORKSPACE:?}")
if [[ -n "${GH_AW_RUNTIME_DIR:-}" ]]; then search_roots+=("$GH_AW_RUNTIME_DIR"); fi
while IFS= read -r git_config; do
  git config --file "${git_config}" --remove-section credential 2>/dev/null || true
  git config --file "${git_config}" --unset-all http.extraheader 2>/dev/null || true
  git config --file "${git_config}" --get-regexp '^http\..*\.extraheader$' 2>/dev/null | while read -r key _; do
    git config --file "${git_config}" --unset-all "${key}" || true
  done || true
  git_configs_processed=$((git_configs_processed + 1))
done < <(find "${search_roots[@]}" -maxdepth 15 -type f -name "config" \( -path "*/.git/config" -o -path "*/.git/modules/*/config" \) 2>/dev/null | sort -u)

if [ "${git_configs_processed}" -eq 0 ]; then
  echo "No git config files found for cleanup"
fi
