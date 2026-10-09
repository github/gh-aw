#!/usr/bin/env bash

# Shared by the KVM-backed preview runtimes. Callers supply their resolved host
# tools so NVX can retain its trusted-tool policy.
prepare_kvm_access() {
  local id_command="$1" sudo_command="$2" setfacl_command="$3" getfacl_command="$4"
  local cleanup_env="${5:-}" runner_uid acl_output entry acl_verified=false
  local previous_acl_exists=false previous_acl_permissions="---"

  if [[ ! -e /dev/kvm ]]; then
    echo "::error::/dev/kvm is missing. KVM preview runtimes require a KVM-capable runner."
    exit 1
  fi
  if [[ ! -c /dev/kvm ]]; then
    echo "::error::/dev/kvm must be a character device."
    exit 1
  fi
  if [[ ! -x "$setfacl_command" ]]; then
    echo "::error::setfacl is required to grant scoped access to /dev/kvm."
    exit 1
  fi

  runner_uid="$("$id_command" -u)"
  if [[ ! "${runner_uid}" =~ ^[0-9]+$ ]]; then
    echo "::error::failed to resolve a numeric runner UID."
    exit 1
  fi
  if ! acl_output="$("$getfacl_command" -ncp /dev/kvm)" || [[ -z "${acl_output}" ]]; then
    echo "::error::failed to read /dev/kvm ACLs before setup."
    exit 1
  fi
  while IFS= read -r entry; do
    entry="${entry%%#effective:*}"
    if [[ "$entry" == "user:${runner_uid}:"* ]]; then
      previous_acl_permissions="${entry#user:"${runner_uid}":}"
      if [[ ! "$previous_acl_permissions" =~ ^[r-][w-][x-]$ ]]; then
        echo "::error::failed to parse the existing runner ACL entry on /dev/kvm."
        exit 1
      fi
      previous_acl_exists=true
      break
    fi
  done <<<"$acl_output"

  if [[ -n "$cleanup_env" ]]; then
    {
      printf 'GH_AW_NVX_KVM_ACL_UID=%s\n' "$runner_uid"
      printf 'GH_AW_NVX_KVM_ACL_EXISTS=%s\n' "$previous_acl_exists"
      printf 'GH_AW_NVX_KVM_ACL_PERMISSIONS=%s\n' "$previous_acl_permissions"
      printf 'GH_AW_NVX_KVM_SUDO=%s\n' "$sudo_command"
      printf 'GH_AW_NVX_KVM_SETFACL=%s\n' "$setfacl_command"
      printf 'GH_AW_NVX_KVM_GETFACL=%s\n' "$getfacl_command"
    } >> "$cleanup_env"
  fi

  if ! "$sudo_command" -n "$setfacl_command" -m "u:${runner_uid}:rw" /dev/kvm; then
    echo "::error::failed to configure scoped access to /dev/kvm for the runner user."
    exit 1
  fi
  if [[ ! -r /dev/kvm || ! -w /dev/kvm ]]; then
    echo "::error::failed to grant the runner user read/write access to /dev/kvm."
    exit 1
  fi

  if ! acl_output="$("$getfacl_command" -ncp /dev/kvm)" || [[ -z "${acl_output}" ]]; then
    echo "::error::failed to read /dev/kvm ACLs for verification."
    exit 1
  fi
  while IFS= read -r entry; do
    entry="${entry%%[[:space:]]#effective:*}"
    if [[ "$entry" == "user:${runner_uid}:rw-" ]]; then
      acl_verified=true
      break
    fi
  done <<<"$acl_output"
  if [[ "$acl_verified" != true ]]; then
    echo "::error::failed to verify scoped ACL entry for the runner user on /dev/kvm."
    exit 1
  fi

  echo "runner user has scoped read/write access to /dev/kvm"
}
