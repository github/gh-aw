#!/usr/bin/env bash
set +o histexpand

# cloud_hypervisor_host_preflight.sh - Validate runner eligibility for AWF's
# preview cloud-hypervisor runtime.
#
# Supported scope is intentionally narrow:
# - GitHub-hosted runners only
# - Ubuntu Linux x86_64 only
# - /dev/kvm must be present
# - gh, rsync, and docker host tools must be present, with a usable Docker Engine
# - a usable cgroup v2 hierarchy must be present
# - the running kernel must report Landlock LSM support

set -euo pipefail

echo "::group::cloud-hypervisor host preflight"

if [[ "${RUNNER_ENVIRONMENT:-}" != "github-hosted" ]]; then
  echo "::error::cloud-hypervisor preview is supported only on GitHub-hosted runners."
  exit 1
fi

if [[ "${RUNNER_OS:-}" != "Linux" ]]; then
  echo "::error::cloud-hypervisor preview requires Linux runners."
  exit 1
fi

if [[ "${RUNNER_ARCH:-}" != "X64" ]]; then
  echo "::error::cloud-hypervisor preview requires x86_64 (RUNNER_ARCH=X64) runners."
  exit 1
fi

if [[ "${ImageOS:-}" != ubuntu* ]]; then
  echo "::error::cloud-hypervisor preview requires GitHub-hosted Ubuntu images (ImageOS starts with 'ubuntu')."
  exit 1
fi

if ! test -e /dev/kvm; then
  echo "::error::/dev/kvm is missing. cloud-hypervisor preview requires KVM-capable GitHub-hosted Ubuntu x86_64 runners."
  exit 1
fi

if ! test -c /dev/kvm; then
  echo "::error::/dev/kvm must be a character device."
  exit 1
fi

for tool in gh rsync docker; do
  if ! command -v "${tool}" >/dev/null 2>&1; then
    echo "::error::required host tool is missing: ${tool}. AWF's cloud-hypervisor runtime needs gh (attested artifact verification), rsync (guest rootfs staging), and docker (infrastructure containers)."
    exit 1
  fi
done

if ! docker info >/dev/null 2>&1; then
  echo "::error::a host-visible Docker Engine is required for cloud-hypervisor's infrastructure containers."
  exit 1
fi

if [[ ! -r /sys/fs/cgroup/cgroup.controllers ]]; then
  echo "::error::a usable cgroup v2 hierarchy is required to bound the cloud-hypervisor process (/sys/fs/cgroup/cgroup.controllers is unreadable)."
  exit 1
fi

if [[ -r /sys/kernel/security/lsm ]]; then
  if ! grep -Fq landlock /sys/kernel/security/lsm; then
    echo "::error::the running kernel does not report Landlock in /sys/kernel/security/lsm, which the cloud-hypervisor launcher requires for filesystem confinement."
    exit 1
  fi
else
  echo "::error::/sys/kernel/security/lsm is unavailable; cannot confirm Landlock LSM support required by the cloud-hypervisor launcher."
  exit 1
fi

echo "runner is eligible for cloud-hypervisor preview"
echo "::endgroup::"
