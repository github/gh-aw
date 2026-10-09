#!/usr/bin/env bash
set +o histexpand

# Grant only the current runner user access to KVM. This avoids weakening the
# device permissions for unrelated users on the host.

set -euo pipefail

echo "::group::Configure cloud-hypervisor KVM access"

if [[ "${RUNNER_ENVIRONMENT:-}" != "github-hosted" || "${RUNNER_OS:-}" != "Linux" || "${RUNNER_ARCH:-}" != "X64" || "${ImageOS:-}" != ubuntu* ]]; then
  echo "::error::cloud-hypervisor KVM access is supported only on GitHub-hosted Ubuntu x86_64 runners."
  exit 1
fi

source "${BASH_SOURCE[0]%/*}/kvm_access.sh"
prepare_kvm_access "$(command -v id)" "$(command -v sudo)" \
  "$(command -v setfacl || true)" "$(command -v getfacl || true)"

echo "::endgroup::"
