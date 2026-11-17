#!/usr/bin/env bash
# clean_git_credentials_checkout.sh - Checkout-time git credential cleanup orchestration
set -euo pipefail
set +o histexpand

if [ -x "${RUNNER_TEMP}/gh-aw/actions/clean_git_credentials.sh" ]; then
  echo "Using shared clean_git_credentials.sh from setup action"
  bash "${RUNNER_TEMP}/gh-aw/actions/clean_git_credentials.sh"
  exit 0
fi

if [ -x "${RUNNER_TEMP}/gh-aw/actions/clean_git_credentials_pre_setup.sh" ]; then
  echo "Using pre-setup clean_git_credentials helper from runner temp bundle"
  bash "${RUNNER_TEMP}/gh-aw/actions/clean_git_credentials_pre_setup.sh"
  exit 0
fi

echo "WARNING: Git credential cleanup helper unavailable. Running inline fallback."

cleaned_configs=0
search_roots=("${GITHUB_WORKSPACE:?}")
if [[ -n "${GH_AW_RUNTIME_DIR:-}" ]]; then search_roots+=("$GH_AW_RUNTIME_DIR"); fi
while IFS= read -r git_config; do
  git config --file "${git_config}" --remove-section credential 2>/dev/null || true
  sed -i '/^\[credential /,/^\[/{ /^\[credential /d; /^\[/!d; }' "${git_config}" 2>/dev/null || true
  git config --file "${git_config}" --unset-all http.extraheader 2>/dev/null || true
  git config --file "${git_config}" --get-regexp '^http\..*\.extraheader$' 2>/dev/null | while read -r key _; do
    git config --file "${git_config}" --unset-all "${key}" || true
  done || true
  git config --file "${git_config}" --get-regexp '^remote\..*\.url$' 2>/dev/null | while read -r key url; do
    clean_url=$(echo "${url}" | sed -E 's|(https?://)([^@]+@)?(.*)|\1\3|')
    if [ "${url}" != "${clean_url}" ]; then
      git config --file "${git_config}" "${key}" "${clean_url}"
    fi
  done || true
  cleaned_configs=$((cleaned_configs + 1))
done < <(find "${search_roots[@]}" -maxdepth 15 -type f -name "config" \( -path "*/.git/config" -o -path "*/.git/modules/*/config" \) 2>/dev/null | sort -u)

if [ "${cleaned_configs}" -eq 0 ]; then
  echo "No git config files found for checkout cleanup fallback"
fi
