#!/usr/bin/env bash
set +o histexpand
set -euo pipefail
umask 077

version="${GH_AW_AWF_VERSION:?GH_AW_AWF_VERSION is required}"
[[ "${version}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([+-][0-9A-Za-z.-]+)?$ ]] || {
  echo "::error::invalid AWF release tag: ${version}"
  exit 1
}

setup_dir="${RUNNER_TEMP}/gh-aw/cloud-hypervisor-enclave-setup"
mkdir -p "${setup_dir}"
manifest_name=cloud-hypervisor-enclave-rootfs-x86_64.manifest.json
bundle_name=cloud-hypervisor-enclave-rootfs-x86_64.manifest.sigstore.jsonl
gh release download "${version}" \
  --repo github/gh-aw-firewall \
  --dir "${setup_dir}" \
  --clobber \
  --pattern setup-cloud-hypervisor-enclave-artifacts.sh \
  --pattern "${manifest_name}" \
  --pattern "${bundle_name}"

# The installer is not separately attested. Bind its bytes to the source commit
# in the release-attested enclave manifest before executing any downloaded code.
gh attestation verify "${setup_dir}/${manifest_name}" \
  --repo github/gh-aw-firewall \
  --bundle "${setup_dir}/${bundle_name}" \
  --signer-workflow github/gh-aw-firewall/.github/workflows/release.yml \
  --deny-self-hosted-runners
source_commit="$(jq -er --arg tag "${version}" \
  '.release | select(.repository == "github/gh-aw-firewall" and .tag == $tag) | .sourceCommit' \
  "${setup_dir}/${manifest_name}")"
[[ "${source_commit}" =~ ^[0-9a-f]{40}$ ]] || {
  echo "::error::invalid enclave artifact source commit"
  exit 1
}
curl -fsSL \
  "https://raw.githubusercontent.com/github/gh-aw-firewall/${source_commit}/guest/cloud-hypervisor/setup-enclave-artifacts.sh" \
  -o "${setup_dir}/source-installer.sh"
cmp "${setup_dir}/source-installer.sh" "${setup_dir}/setup-cloud-hypervisor-enclave-artifacts.sh"

# The release installer verifies the enclave manifest and both rootfs attestations
# and exports the host-only artifact paths through GITHUB_ENV.
bash "${setup_dir}/setup-cloud-hypervisor-enclave-artifacts.sh" \
  "${version}" "${RUNNER_TEMP}/gh-aw/cloud-hypervisor-enclaves"
