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
#     file, signed-by apt source line) instead of piping the get.docker.com
#     convenience script into a root shell. See:
#     https://docs.docker.com/engine/install/ubuntu/#install-using-the-repository

set -euo pipefail

echo "::group::Install docker-sbx"
KEYRING_PATH="/etc/apt/keyrings/docker.asc"
SOURCE_LIST="/etc/apt/sources.list.d/docker.list"
DISTRO_ID="$(. /etc/os-release && echo "$ID")"
DISTRO_CODENAME="$(. /etc/os-release && echo "$VERSION_CODENAME")"

echo "Adding Docker apt repository for ${DISTRO_ID} ${DISTRO_CODENAME}..."
sudo install -m 0755 -d /etc/apt/keyrings
# runner-guard:ignore RGS-012 -- fetches Docker's GPG signing key to a file (never piped to a shell); the key is used only to verify the signed-by apt repository below.
curl -fsSL "https://download.docker.com/linux/${DISTRO_ID}/gpg" -o /tmp/docker.asc
sudo install -m 0644 /tmp/docker.asc "${KEYRING_PATH}"
rm -f /tmp/docker.asc

echo "deb [arch=$(dpkg --print-architecture) signed-by=${KEYRING_PATH}] https://download.docker.com/linux/${DISTRO_ID} ${DISTRO_CODENAME} stable" \
  | sudo tee "${SOURCE_LIST}" > /dev/null

sudo apt-get update -qq
sudo apt-get install -y docker-sbx
sbx version
# Fix KVM permissions so the runner user can create microVMs.
sudo chmod 666 /dev/kvm
echo "docker-sbx installed successfully"
echo "::endgroup::"
