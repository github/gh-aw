#!/usr/bin/env bash
set -euo pipefail

echo "::group::NVX host and trusted artifact preflight"

trusted_dirs=(/usr/sbin /usr/bin /sbin /bin)
required_tools=(
  bwrap flock getfacl getent gh groupdel id ip iptables mkfs.erofs mke2fs nft
  setfacl setpriv sysctl useradd userdel
  chmod install jq mktemp realpath sha256sum stat sudo uname
)
declare -A trusted_tools

tool_is_trusted_path() {
  local candidate="$1"
  for directory in "${trusted_dirs[@]}"; do
    [[ "$candidate" == "$directory"/* ]] && return 0
  done
  return 1
}

for name in "${required_tools[@]}"; do
  resolved=""
  for directory in "${trusted_dirs[@]}"; do
    candidate="${directory}/${name}"
    if [[ -x "$candidate" ]]; then
      resolved="$(/usr/bin/realpath -- "$candidate" 2>/dev/null || true)"
      if [[ -n "$resolved" ]] && tool_is_trusted_path "$resolved"; then
        break
      fi
      resolved=""
    fi
  done
  if [[ -z "$resolved" || ! -f "$resolved" ]]; then
    echo "::error::required trusted NVX host tool is missing or resolves outside /usr/sbin, /usr/bin, /sbin, or /bin: ${name}"
    echo "Install the NVX Ubuntu host tools before the agent setup steps (acl bubblewrap e2fsprogs erofs-utils jq nftables uidmap)."
    exit 1
  fi
  tool_owner="$(/usr/bin/stat -c '%u' -- "$resolved")"
  tool_mode="$(/usr/bin/stat -c '%a' -- "$resolved")"
  if [[ "$tool_owner" != "0" ]] || (( (8#$tool_mode & 0022) != 0 )) || (( (8#$tool_mode & 0111) == 0 )); then
    echo "::error::required NVX host tool is not root-owned and non-writable by group/other: ${name} (${resolved})"
    exit 1
  fi
  trusted_tools["$name"]="$resolved"
done

tool() {
  local name="$1"
  shift
  "${trusted_tools[$name]}" "$@"
}

if [[ "$(tool uname -s)" != "Linux" ]]; then
  echo "::error::NVX requires a Linux host."
  exit 1
fi
if [[ "$(tool uname -m)" != "x86_64" ]]; then
  echo "::error::NVX supports only Linux x86_64 hosts."
  exit 1
fi
if [[ ! -c /dev/kvm || ! -r /dev/kvm || ! -w /dev/kvm ]]; then
  echo "::error::NVX requires read/write access to the /dev/kvm character device."
  exit 1
fi
if [[ ! -r /sys/fs/cgroup/cgroup.controllers ]]; then
  echo "::error::NVX requires a readable cgroup v2 hierarchy."
  exit 1
fi
cgroup_controllers="$(</sys/fs/cgroup/cgroup.controllers)"
for controller in cpu memory pids; do
  if [[ " ${cgroup_controllers} " != *" ${controller} "* ]]; then
    echo "::error::NVX requires the cgroup v2 ${controller} controller."
    exit 1
  fi
done
if [[ ! -r /proc/sys/kernel/seccomp/actions_avail ]] ||
  [[ " $(</proc/sys/kernel/seccomp/actions_avail) " != *" kill_process "* ]]; then
  echo "::error::NVX requires seccomp kill_process support."
  exit 1
fi
if [[ "$(tool sudo -n "${trusted_tools[id]}" -u)" != "0" ]]; then
  echo "::error::NVX requires non-interactive sudo so AWF can run with effective UID 0."
  exit 1
fi

for variable in \
  GH_AW_AWF_VERSION \
  GH_AW_NVX_LAYER_SOURCE \
  GH_AW_NVX_OPENVMM_SOURCE \
  GH_AW_NVX_KERNEL_SOURCE \
  GH_AW_NVX_INITRAMFS_SOURCE \
  GH_AW_NVX_ARTIFACT_MANIFEST_SOURCE \
  GH_AW_NVX_ARTIFACT_MANIFEST_BUNDLE_SOURCE \
  GH_AW_NVX_SIGNER_WORKFLOW \
  GH_AW_NVX_MOUNT_POLICY; do
  if [[ -z "${!variable:-}" || "${!variable}" == *$'\n'* || "${!variable}" == *$'\r'* ]]; then
    echo "::error::NVX preflight requires a non-empty, single-line ${variable} value."
    exit 1
  fi
done

version="${GH_AW_AWF_VERSION}"
if [[ "${version,,}" == "latest" ]]; then
  echo "::error::NVX requires a concrete AWF release version so the attested manifest can be bound to that exact release."
  exit 1
fi
[[ "$version" == v* ]] || version="v${version}"

if [[ "${GH_AW_NVX_MOUNT_POLICY}" == "workspace-and-tool-cache" ]]; then
  tool_cache="${RUNNER_TOOL_CACHE:-${AGENT_TOOLSDIRECTORY:-}}"
  if [[ -z "$tool_cache" || ! -d "$tool_cache" ]]; then
    echo "::error::NVX mount-policy workspace-and-tool-cache requires RUNNER_TOOL_CACHE or AGENT_TOOLSDIRECTORY to exist."
    exit 1
  fi
fi

if [[ ! -d "$GH_AW_NVX_LAYER_SOURCE" || -L "$GH_AW_NVX_LAYER_SOURCE" ]]; then
  echo "::error::NVX guest layer must be an existing non-symlink directory: ${GH_AW_NVX_LAYER_SOURCE}"
  exit 1
fi
layer_path="$(tool realpath -- "$GH_AW_NVX_LAYER_SOURCE")"
if [[ "$layer_path" != /* ]]; then
  echo "::error::NVX guest layer path must resolve to an absolute path."
  exit 1
fi

artifact_paths=(
  "$GH_AW_NVX_OPENVMM_SOURCE"
  "$GH_AW_NVX_KERNEL_SOURCE"
  "$GH_AW_NVX_INITRAMFS_SOURCE"
  "$GH_AW_NVX_ARTIFACT_MANIFEST_SOURCE"
  "$GH_AW_NVX_ARTIFACT_MANIFEST_BUNDLE_SOURCE"
)
artifact_names=(openvmm vmlinux initramfs.cpio.gz nvx-test-x86_64.manifest.json nvx-test-x86_64.manifest.sigstore.jsonl)
for index in "${!artifact_paths[@]}"; do
  artifact="${artifact_paths[$index]}"
  if [[ "$artifact" != /* || ! -f "$artifact" || -L "$artifact" || ! -s "$artifact" ]]; then
    echo "::error::NVX trusted artifact must be an absolute path to a non-empty regular non-symlink file: ${artifact}"
    exit 1
  fi
  if [[ "${artifact##*/}" != "${artifact_names[$index]}" ]]; then
    echo "::error::NVX artifact ${index} must be named ${artifact_names[$index]}."
    exit 1
  fi
done

if ! "${trusted_tools[gh]}" attestation verify "${stage_dir}/manifest.json" \
  --repo github/gh-aw-firewall \
  --bundle "${stage_dir}/manifest.sigstore.jsonl" \
  --signer-workflow "$GH_AW_NVX_SIGNER_WORKFLOW" \
  --deny-self-hosted-runners; then
  echo "::error::NVX manifest offline attestation verification failed; no unsigned or hash-only fallback is permitted."
  exit 1
fi

# shellcheck disable=SC2016
if ! "${trusted_tools[jq]}" -e \
  --arg expectedTag "$version" \
  --arg signer "$GH_AW_NVX_SIGNER_WORKFLOW" '
    (keys | sort) == ["architecture", "artifacts", "release", "schemaVersion", "upstream"]
    and .schemaVersion == 2
    and .architecture == "x86_64"
    and ((.release | keys | sort) == ["repository", "sourceCommit", "tag", "workflow"])
    and .release.repository == "github/gh-aw-firewall"
    and .release.workflow == $signer
    and .release.tag == $expectedTag
    and (.release.sourceCommit | type == "string" and test("^[a-f0-9]{40}$"))
    and ((.upstream | keys | sort) == ["nvxCommit", "openvmmCommit", "releaseTag"])
    and .upstream.releaseTag == "v0.1.0-dev.be859aa77ffa"
    and .upstream.nvxCommit == "be859aa77ffa7a20f9ef50f68c5386acdfca9955"
    and .upstream.openvmmCommit == "762bc1c7a203b16aee752324d6a4ab0bde1a713a"
    and ((.artifacts | keys | sort) == ["initramfs", "kernel", "openvmm"])
    and ([.artifacts[] | (keys | sort) == ["file", "sha256", "sizeBytes"]] | all)
    and .artifacts.openvmm.file == "openvmm"
    and .artifacts.kernel.file == "vmlinux"
    and .artifacts.initramfs.file == "initramfs.cpio.gz"
    and ([.artifacts[] | (.sizeBytes | type == "number" and . > 0 and floor == .)] | all)
    and .artifacts.openvmm.sizeBytes <= 536870912
    and .artifacts.kernel.sizeBytes <= 536870912
    and .artifacts.initramfs.sizeBytes <= 1073741824
    and ([.artifacts[] | (.sha256 | type == "string" and test("^[a-f0-9]{64}$"))] | all)
  ' "$GH_AW_NVX_ARTIFACT_MANIFEST_SOURCE" >/dev/null; then
  echo "::error::NVX artifact manifest does not match the trusted AWF release and pinned upstream artifact contract."
  exit 1
fi

for name in openvmm kernel initramfs; do
  case "$name" in
    openvmm) source="$GH_AW_NVX_OPENVMM_SOURCE"; role="openvmm" ;;
    kernel) source="$GH_AW_NVX_KERNEL_SOURCE"; role="kernel" ;;
    initramfs) source="$GH_AW_NVX_INITRAMFS_SOURCE"; role="initramfs" ;;
  esac
  expected_size="$("${trusted_tools[jq]}" -er ".artifacts.${role}.sizeBytes" "$GH_AW_NVX_ARTIFACT_MANIFEST_SOURCE")"
  expected_digest="$("${trusted_tools[jq]}" -er ".artifacts.${role}.sha256" "$GH_AW_NVX_ARTIFACT_MANIFEST_SOURCE")"
  source_size="$(tool stat -c '%s' -- "$source")"
  source_digest="$(tool sha256sum -- "$source")"
  source_digest="${source_digest%% *}"
  if [[ "$source_size" != "$expected_size" || "$source_digest" != "$expected_digest" ]]; then
    echo "::error::NVX ${role} source artifact size or SHA-256 does not match the attested manifest."
    exit 1
  fi
done

stage_dir="$(tool sudo -n "${trusted_tools[mktemp]}" -d /tmp/gh-aw-nvx.XXXXXXXXXX)"
tool sudo -n "${trusted_tools[install]}" -o 0 -g 0 -m 0555 -- "$GH_AW_NVX_OPENVMM_SOURCE" "${stage_dir}/openvmm"
tool sudo -n "${trusted_tools[install]}" -o 0 -g 0 -m 0444 -- "$GH_AW_NVX_KERNEL_SOURCE" "${stage_dir}/vmlinux"
tool sudo -n "${trusted_tools[install]}" -o 0 -g 0 -m 0444 -- "$GH_AW_NVX_INITRAMFS_SOURCE" "${stage_dir}/initramfs.cpio.gz"
tool sudo -n "${trusted_tools[install]}" -o 0 -g 0 -m 0444 -- "$GH_AW_NVX_ARTIFACT_MANIFEST_SOURCE" "${stage_dir}/manifest.json"
tool sudo -n "${trusted_tools[install]}" -o 0 -g 0 -m 0444 -- "$GH_AW_NVX_ARTIFACT_MANIFEST_BUNDLE_SOURCE" "${stage_dir}/manifest.sigstore.jsonl"
tool sudo -n "${trusted_tools[chmod]}" 0555 "$stage_dir"

for name in openvmm kernel initramfs; do
  case "$name" in
    openvmm) file="${stage_dir}/openvmm"; role="openvmm" ;;
    kernel) file="${stage_dir}/vmlinux"; role="kernel" ;;
    initramfs) file="${stage_dir}/initramfs.cpio.gz"; role="initramfs" ;;
  esac
  expected_size="$("${trusted_tools[jq]}" -er ".artifacts.${role}.sizeBytes" "${stage_dir}/manifest.json")"
  expected_digest="$("${trusted_tools[jq]}" -er ".artifacts.${role}.sha256" "${stage_dir}/manifest.json")"
  actual_size="$(tool stat -c '%s' -- "$file")"
  actual_digest="$(tool sha256sum -- "$file")"
  actual_digest="${actual_digest%% *}"
  if [[ "$actual_size" != "$expected_size" || "$actual_digest" != "$expected_digest" ]]; then
    echo "::error::NVX ${role} artifact size or SHA-256 does not match the attested manifest."
    exit 1
  fi
  metadata="$(tool stat -c '%u:%a' -- "$file")"
  expected_mode=444
  [[ "$role" == openvmm ]] && expected_mode=555
  if [[ "$metadata" != "0:${expected_mode}" ]]; then
    echo "::error::NVX ${role} artifact is not root-owned with its required read-only mode."
    exit 1
  fi
done

manifest_metadata="$(tool stat -c '%u:%a' -- "${stage_dir}/manifest.json")"
bundle_metadata="$(tool stat -c '%u:%a' -- "${stage_dir}/manifest.sigstore.jsonl")"
if [[ "$manifest_metadata" != "0:444" || "$bundle_metadata" != "0:444" ]]; then
  echo "::error::NVX manifest and attestation bundle must be root-owned read-only regular files."
  exit 1
fi

{
  printf 'GH_AW_NVX_LAYER=%s\n' "$layer_path"
  printf 'GH_AW_NVX_OPENVMM=%s/openvmm\n' "$stage_dir"
  printf 'GH_AW_NVX_KERNEL=%s/vmlinux\n' "$stage_dir"
  printf 'GH_AW_NVX_INITRAMFS=%s/initramfs.cpio.gz\n' "$stage_dir"
  printf 'GH_AW_NVX_ARTIFACT_MANIFEST=%s/manifest.json\n' "$stage_dir"
  printf 'GH_AW_NVX_ARTIFACT_MANIFEST_BUNDLE=%s/manifest.sigstore.jsonl\n' "$stage_dir"
} >> "${GITHUB_ENV:?GITHUB_ENV is required}"

echo "NVX host and attested artifacts validated; AWF will fail closed if runtime startup fails."
echo "::endgroup::"
