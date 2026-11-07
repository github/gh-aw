#!/usr/bin/env bash
set +o histexpand

# Shared privilege selection for Copilot and threat-detect installation.
# An explicit --rootless always wins; otherwise use root or passwordless sudo
# when available, falling back to a job-scoped directory on the runner.
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

prepare_rootless_install_dir() {
  if [ -z "${RUNNER_TEMP:-}" ]; then
    echo "ERROR: --rootless requires RUNNER_TEMP for a job-scoped install directory" >&2
    return 1
  fi

  local gh_aw_dir="${RUNNER_TEMP%/}/gh-aw"
  local install_dir="${gh_aw_dir}/bin"
  if [ -L "$gh_aw_dir" ] || [ -L "$install_dir" ] ||
    ! { mkdir -p "$install_dir" && [ ! -L "$gh_aw_dir" ] && [ ! -L "$install_dir" ] &&
      [ -d "$install_dir" ] && [ -w "$install_dir" ]; }; then
    echo "ERROR: --rootless could not create a writable install directory at ${install_dir}" >&2
    return 1
  fi

  printf '%s\n' "$install_dir"
}
