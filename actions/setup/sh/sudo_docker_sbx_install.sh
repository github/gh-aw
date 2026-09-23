#!/usr/bin/env bash
set +o histexpand

# sudo_docker_sbx_install.sh - Install the docker-sbx package via the Docker apt repository.
#
# Requires sudo access to install the package and fix KVM device permissions.
#
# Usage: sudo_docker_sbx_install.sh
# No arguments required.
#
# Key notes:
#   - Adds the Docker apt repository deterministically (pinned GPG key fetched to a
#     file, signed-by apt source line) instead of piping a convenience script into
#     a root shell. See:
#     https://docs.docker.com/engine/install/ubuntu/#install-using-the-repository

set -euo pipefail

echo "::group::Install docker-sbx"
KEYRING_PATH="/etc/apt/keyrings/docker.asc"
SOURCE_LIST="/etc/apt/sources.list.d/docker.list"
# Update only after verifying a replacement fingerprint against Docker's published signing-key documentation.
DOCKER_GPG_FINGERPRINT="9DC858229FC7DD38854AE2D88D81803C0EBFCD88"
. /etc/os-release
DISTRO_ID="${ID:-}"
DISTRO_CODENAME="${VERSION_CODENAME:-}"

if [[ -z "${DISTRO_ID}" || -z "${DISTRO_CODENAME}" ]]; then
  echo "Docker apt repository requires ID and VERSION_CODENAME in /etc/os-release." >&2
  exit 1
fi

echo "Adding Docker apt repository for ${DISTRO_ID} ${DISTRO_CODENAME}..."
sudo install -m 0755 -d /etc/apt/keyrings
KEY_TMP="$(mktemp)"
trap 'rm -f "${KEY_TMP}"' EXIT
# runner-guard:ignore RGS-012 -- fetches Docker's GPG signing key to a temporary file (never piped to a shell); the key fingerprint is verified before it is used to verify the signed-by apt repository below.
curl -fsSL "https://download.docker.com/linux/${DISTRO_ID}/gpg" -o "${KEY_TMP}"
mapfile -t KEY_FINGERPRINTS < <(
  gpg --show-keys --with-colons "${KEY_TMP}" \
    | awk -F: '$1 == "pub" { primary = 1; next } primary && $1 == "fpr" { print $10; primary = 0 }'
)
if (( ${#KEY_FINGERPRINTS[@]} == 0 )); then
  echo "Downloaded Docker GPG key contains no primary key fingerprint." >&2
  exit 1
fi
for key_fingerprint in "${KEY_FINGERPRINTS[@]}"; do
  if [[ "${key_fingerprint}" != "${DOCKER_GPG_FINGERPRINT}" ]]; then
    echo "Downloaded Docker GPG key fingerprint does not match the expected fingerprint." >&2
    exit 1
  fi
done
sudo install -m 0644 "${KEY_TMP}" "${KEYRING_PATH}"

echo "deb [arch=$(dpkg --print-architecture) signed-by=${KEYRING_PATH}] https://download.docker.com/linux/${DISTRO_ID} ${DISTRO_CODENAME} stable" \
  | sudo tee "${SOURCE_LIST}" > /dev/null

sudo apt-get update -qq
sudo apt-get install -y docker-sbx
sbx version
# Fix KVM permissions so the runner user can create microVMs.
sudo chmod 666 /dev/kvm
echo "docker-sbx installed successfully"
echo "::endgroup::"
