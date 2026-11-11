#!/bin/bash
# Verify the pinned Go feature and upgrade the GitHub CLI (gh).
set -euo pipefail

CURRENT_GO="$(env -u GOROOT GOTOOLCHAIN=local go version | awk '{print $3}')"
if [ "$CURRENT_GO" != go1.27.4 ]; then
  echo "ERROR: Expected Go 1.27.4 from the pinned devcontainer feature; found ${CURRENT_GO}. Rebuild the devcontainer." >&2
  exit 1
fi
echo "Go tooling is pinned to ${CURRENT_GO}."

# --- Upgrade the GitHub CLI (gh) to the latest release ---
echo "Upgrading GitHub CLI (gh)..."
sudo apt-get update
sudo apt-get install --only-upgrade -y gh || sudo apt-get install -y gh
echo "gh is now $(gh --version | head -n1)."
