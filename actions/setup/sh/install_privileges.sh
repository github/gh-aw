#!/usr/bin/env bash
set +o histexpand

# Shared privilege selection for Copilot and threat-detect installation.
# An explicit --rootless always wins; otherwise use root or passwordless sudo
# when available, falling back to the installer's writable user-local directory.
configure_install_privileges() {
  INSTALL_USE_SUDO=false
  if [ "$ROOTLESS" != "true" ] && [ "$(id -u)" != "0" ]; then
    if command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
      INSTALL_USE_SUDO=true
    else
      ROOTLESS=true
      echo "No passwordless sudo available; using rootless installation." >&2
    fi
  fi
}

maybe_sudo() {
  if [ "$INSTALL_USE_SUDO" = "true" ]; then
    sudo -n "$@"
  else
    "$@"
  fi
}
