#!/usr/bin/env bash

# Host-network CLI proxies must remain reachable from AWF's Docker bridges,
# but must not lend their GitHub credential to callers on runner-facing NICs.
install_cli_proxy_ingress_filter() {
  local interface
  if ! sudo -n iptables -w -S GH_AW_CLI_PROXY >/dev/null 2>&1; then
    sudo -n iptables -w -N GH_AW_CLI_PROXY || return 1
  fi
  # Install the deny before any exceptions or INPUT hook. Retries are idempotent.
  sudo -n iptables -w -C GH_AW_CLI_PROXY -j DROP 2>/dev/null ||
    sudo -n iptables -w -A GH_AW_CLI_PROXY -j DROP || return 1
  for interface in lo docker0 br-+; do
    sudo -n iptables -w -C GH_AW_CLI_PROXY -i "$interface" -j RETURN 2>/dev/null ||
      sudo -n iptables -w -I GH_AW_CLI_PROXY 1 -i "$interface" -j RETURN || return 1
  done
  sudo -n iptables -w -C INPUT -p tcp --dport 18443 -j GH_AW_CLI_PROXY 2>/dev/null ||
    sudo -n iptables -w -I INPUT 1 -p tcp --dport 18443 -j GH_AW_CLI_PROXY || return 1
}

remove_cli_proxy_ingress_filter() {
  local rules
  rules="$(sudo -n iptables -w -S)" || return 1
  if ! grep -q '^-N GH_AW_CLI_PROXY$' <<< "$rules"; then
    return 0
  fi
  if sudo -n iptables -w -C INPUT -p tcp --dport 18443 -j GH_AW_CLI_PROXY 2>/dev/null; then
    sudo -n iptables -w -D INPUT -p tcp --dport 18443 -j GH_AW_CLI_PROXY || return 1
  fi
  sudo -n iptables -w -F GH_AW_CLI_PROXY || return 1
  sudo -n iptables -w -X GH_AW_CLI_PROXY
}
