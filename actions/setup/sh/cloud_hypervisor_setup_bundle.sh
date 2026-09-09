#!/usr/bin/env bash
set +o histexpand

# cloud_hypervisor_setup_bundle.sh - Download, verify, and unpack AWF's
# cloud-hypervisor guest bundle for the requested AWF version.
#
# Outputs (GITHUB_OUTPUT):
#   binary_path, virtiofsd_path, kernel_path, rootfs_path, supervisor_path
#   manifest_path, manifest_bundle_path, release_tag

set -euo pipefail

if [[ -z "${GH_AW_AWF_VERSION:-}" ]]; then
  echo "::error::GH_AW_AWF_VERSION is required"
  exit 1
fi

version="${GH_AW_AWF_VERSION}"
if [[ "${version,,}" == "latest" ]]; then
  # GitHub has no "releases/download/latest/<asset>" URL: only
  # "releases/latest/download/<asset>" resolves the latest release, and it
  # does not reveal the resolved tag. Resolve the concrete release tag up
  # front so every subsequent step (asset URLs, manifest release-tag
  # comparison) operates on a single explicit, verifiable version.
  echo "::group::Resolve latest cloud-hypervisor release tag"
  auth_header=()
  if [[ -n "${GH_TOKEN:-}" ]]; then
    auth_header=(-H "Authorization: token ${GH_TOKEN}")
  elif [[ -n "${GITHUB_TOKEN:-}" ]]; then
    auth_header=(-H "Authorization: token ${GITHUB_TOKEN}")
  fi
  if ! latest_release_json="$(curl -fsSL "${auth_header[@]}" -H "Accept: application/vnd.github+json" \
    "https://api.github.com/repos/github/gh-aw-firewall/releases/latest")"; then
    echo "::error::failed to resolve the latest gh-aw-firewall release tag: could not reach the GitHub releases API"
    exit 1
  fi
  version="$(jq -r '.tag_name // empty' <<<"${latest_release_json}")"
  if [[ -z "${version}" ]]; then
    echo "::error::failed to resolve the latest gh-aw-firewall release tag: no tag_name in API response"
    exit 1
  fi
  echo "resolved latest release tag: ${version}"
  echo "::endgroup::"
elif [[ "${version}" != v* ]]; then
  version="v${version}"
fi

asset_base_url="https://github.com/github/gh-aw-firewall/releases/download/${version}"
asset_name="cloud-hypervisor-test-x86_64.tar.gz"
manifest_name="cloud-hypervisor-test-x86_64.manifest.json"
manifest_bundle_name="cloud-hypervisor-test-x86_64.manifest.sigstore.jsonl"

bundle_root="${RUNNER_TEMP}/gh-aw/cloud-hypervisor/${version}"
extract_dir="${bundle_root}/bundle"
mkdir -p "${bundle_root}" "${extract_dir}"

echo "::group::Download cloud-hypervisor bundle (${version})"
curl -fsSL -o "${bundle_root}/${asset_name}" "${asset_base_url}/${asset_name}"
curl -fsSL -o "${bundle_root}/${manifest_name}" "${asset_base_url}/${manifest_name}"
curl -fsSL -o "${bundle_root}/${manifest_bundle_name}" "${asset_base_url}/${manifest_bundle_name}"
echo "downloaded release assets"
echo "::endgroup::"

echo "::group::Validate cloud-hypervisor bundle archive structure"
archive_path="${bundle_root}/${asset_name}"
if tar -tzf "${archive_path}" | grep -E '(^/|(^|/)\.\.(/|$))' >/dev/null; then
  echo "::error::cloud-hypervisor bundle contains unsafe archive paths"
  exit 1
fi
archive_table="$(tar -tvzf "${archive_path}")"
if [[ -z "${archive_table}" ]]; then
  echo "::error::cloud-hypervisor bundle archive is empty"
  exit 1
fi
if grep -Eq '^[lh]' <<<"${archive_table}"; then
  echo "::error::cloud-hypervisor bundle must not include symbolic or hard links"
  exit 1
fi
echo "archive structure validated"
echo "::endgroup::"

echo "::group::Extract cloud-hypervisor bundle"
tar --no-same-owner --no-same-permissions -xzf "${archive_path}" -C "${extract_dir}"
echo "bundle extracted to ${extract_dir}"
echo "::endgroup::"

resolve_path() {
  local rel="$1"
  if [[ -z "${rel}" ]]; then
    return 1
  fi

  local cleaned="${rel#./}"
  local candidate
  for candidate in \
    "${extract_dir}/${cleaned}" \
    "${bundle_root}/${cleaned}"; do
    if [[ -f "${candidate}" ]]; then
      realpath "${candidate}"
      return 0
    fi
  done

  local found
  found="$(find "${extract_dir}" -type f -name "$(basename "${cleaned}")" | head -n1 || true)"
  if [[ -n "${found}" ]]; then
    realpath "${found}"
    return 0
  fi

  return 1
}

validate_extracted_file() {
  local file="$1"
  if [[ -z "${file}" || ! -f "${file}" || -L "${file}" ]]; then
    echo "::error::invalid extracted cloud-hypervisor bundle file: ${file}"
    exit 1
  fi
  local real_file real_extract_dir
  real_file="$(realpath "${file}")"
  real_extract_dir="$(realpath "${extract_dir}")"
  if [[ "${real_file}" != "${real_extract_dir}"/* ]]; then
    echo "::error::extracted bundle file is outside expected directory: ${file}"
    exit 1
  fi
}

# Artifact names are fixed by the gh-aw-firewall cloud-hypervisor release contract.
binary_rel="cloud-hypervisor"
kernel_rel="vmlinux.bin"
rootfs_rel="rootfs.ext4"
supervisor_rel="awf-supervisor"
virtiofsd_rel="virtiofsd"

binary_path="$(resolve_path "${binary_rel}" || true)"
kernel_path="$(resolve_path "${kernel_rel}" || true)"
rootfs_path="$(resolve_path "${rootfs_rel}" || true)"
supervisor_path="$(resolve_path "${supervisor_rel}" || true)"
virtiofsd_path="$(resolve_path "${virtiofsd_rel}" || true)"

if [[ -z "${binary_path}" || -z "${kernel_path}" || -z "${rootfs_path}" || -z "${supervisor_path}" || -z "${virtiofsd_path}" ]]; then
  echo "::error::failed to resolve one or more cloud-hypervisor artifact files after extraction"
  exit 1
fi

validate_extracted_file "${binary_path}"
validate_extracted_file "${kernel_path}"
validate_extracted_file "${rootfs_path}"
validate_extracted_file "${supervisor_path}"
validate_extracted_file "${virtiofsd_path}"

if [[ "$(dirname "${binary_path}")" != "$(dirname "${virtiofsd_path}")" ]]; then
  echo "::error::virtiofsd must be colocated with the cloud-hypervisor binary"
  exit 1
fi

manifest_path="${bundle_root}/${manifest_name}"
manifest_bundle_path="${bundle_root}/${manifest_bundle_name}"
if [[ ! -f "${manifest_path}" || -L "${manifest_path}" ]] || ! jq -e '
  .schemaVersion == 1
  and .release.repository == "github/gh-aw-firewall"
  and .release.workflow == "github/gh-aw-firewall/.github/workflows/release.yml"
  and .release.tag == $releaseTag
  and (.release.sourceCommit | type == "string" and test("^[0-9a-f]{40}$"))
  and .architecture == "x86_64"
  and (.artifacts.cloudHypervisor.file == "cloud-hypervisor")
  and (.artifacts.virtiofsd.file == "virtiofsd")
  and (.artifacts.kernel.file == "vmlinux.bin")
  and (.artifacts.rootfs.file == "rootfs.ext4")
  and (.artifacts.supervisor.file == "awf-supervisor")
  and ([.artifacts[] | .sha256] | all(type == "string" and test("^[0-9A-Fa-f]{64}$")))
' --arg releaseTag "${version}" "${manifest_path}" >/dev/null; then
 echo "::error::${manifest_name} does not match the cloud-hypervisor release bundle contract"
 exit 1
fi

if [[ ! -f "${manifest_bundle_path}" || -L "${manifest_bundle_path}" ]] || ! jq -e -s '
 length > 0 and all(.[]; type == "object" and .mediaType == "application/vnd.dev.sigstore.bundle.v0.3+json" and .verificationMaterial != null)
' "${manifest_bundle_path}" >/dev/null; then
 echo "::error::${manifest_bundle_name} is missing or malformed"
 exit 1
fi

chmod 0755 "${binary_path}" "${supervisor_path}" "${virtiofsd_path}"
chmod 0444 "${manifest_path}" "${manifest_bundle_path}"

if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
  {
    echo "binary_path=${binary_path}"
    echo "kernel_path=${kernel_path}"
    echo "rootfs_path=${rootfs_path}"
    echo "supervisor_path=${supervisor_path}"
    echo "virtiofsd_path=${virtiofsd_path}"
    echo "manifest_path=${manifest_path}"
    echo "manifest_bundle_path=${manifest_bundle_path}"
    echo "release_tag=${version}"
  } >> "${GITHUB_OUTPUT}"
fi
if [[ -n "${GITHUB_ENV:-}" ]]; then
  {
    echo "GH_AW_CLOUD_HYPERVISOR_BINARY=${binary_path}"
    echo "GH_AW_CLOUD_HYPERVISOR_KERNEL=${kernel_path}"
    echo "GH_AW_CLOUD_HYPERVISOR_ROOTFS=${rootfs_path}"
    echo "GH_AW_CLOUD_HYPERVISOR_SUPERVISOR=${supervisor_path}"
    echo "GH_AW_CLOUD_HYPERVISOR_VIRTIOFSD=${virtiofsd_path}"
    echo "GH_AW_CLOUD_HYPERVISOR_ARTIFACT_MANIFEST=${manifest_path}"
    echo "GH_AW_CLOUD_HYPERVISOR_ARTIFACT_MANIFEST_BUNDLE=${manifest_bundle_path}"
    echo "GH_AW_CLOUD_HYPERVISOR_ARTIFACT_RELEASE_TAG=${version}"
  } >> "${GITHUB_ENV}"
fi

echo "cloud-hypervisor bundle prepared"
