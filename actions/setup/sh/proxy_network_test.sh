#!/usr/bin/env bash
# Linux integration test; requires node, curl, iproute2, iptables and namespace
# privileges. Always isolate firewall changes from the caller's network namespace.
set -euo pipefail

if [ "${1:-}" != "--isolated" ]; then
  exec unshare --net bash "$0" --isolated
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=proxy_network_lib.sh
source "${SCRIPT_DIR}/proxy_network_lib.sh"

# The fixture runs as root inside its isolated namespace.
sudo() {
  test "$1" = -n
  shift
  "$@"
}

SERVER_PID=
MCP_PID=
cleanup() {
  if [ -n "$SERVER_PID" ]; then kill "$SERVER_PID" 2>/dev/null || true; fi
  if [ -n "$MCP_PID" ]; then kill "$MCP_PID" 2>/dev/null || true; fi
  for namespace in external sidecar isolated; do
    ip netns delete "$namespace" 2>/dev/null || true
  done
}
trap cleanup EXIT

ip link set lo up
for entry in external:eth0:192.0.2 sidecar:docker0:172.18.0 isolated:br-awf-test:172.19.0; do
  IFS=: read -r namespace interface subnet <<< "$entry"
  ip netns add "$namespace"
  ip link add "$interface" type bridge
  ip addr add "$subnet.1/24" dev "$interface"
  ip link set "$interface" up
  ip link add "v-$namespace" type veth peer name peer
  ip link set "v-$namespace" master "$interface"
  ip link set "v-$namespace" up
  ip link set peer netns "$namespace"
  ip -n "$namespace" addr add "$subnet.2/24" dev peer
  ip -n "$namespace" link set peer up
  ip -n "$namespace" link set lo up
done
ip -n external route add 172.18.0.0/24 via 192.0.2.1

start_listener() {
  node -e 'require("http").createServer((req,res)=>res.end("proxy-ok")).listen(18443, process.argv[1])' "$1" &
  SERVER_PID=$!
  for attempt in $(seq 1 50); do
    if curl -fs --noproxy '*' http://127.0.0.1:18443 >/dev/null; then return; fi
    sleep 0.1
  done
  echo "Listener failed to start" >&2
  exit 1
}

probe() {
  local namespace=$1 address=$2
  ip netns exec "$namespace" curl -fs --noproxy '*' --connect-timeout 1 --max-time 2 "http://$address:18443"
}

start_listener 0.0.0.0
test "$(probe external 192.0.2.1)" = proxy-ok
iptables -A INPUT -p tcp --dport 19999 -j ACCEPT
install_cli_proxy_ingress_filter
rules="$(iptables -S)"
install_cli_proxy_ingress_filter
test "$(iptables -S)" = "$rules"
test "$(curl -fs --noproxy '*' http://127.0.0.1:18443)" = proxy-ok
test "$(probe sidecar 172.18.0.1)" = proxy-ok
test "$(probe isolated 172.19.0.1)" = proxy-ok
for address in 192.0.2.1 172.18.0.1; do
  if probe external "$address"; then
    echo "External client reached the protected legacy proxy" >&2
    exit 1
  fi
done
remove_cli_proxy_ingress_filter
iptables -C INPUT -p tcp --dport 19999 -j ACCEPT
test "$(probe external 192.0.2.1)" = proxy-ok
remove_cli_proxy_ingress_filter
echo "PASS: legacy CLI host/bridge connectivity, external rejection, retry and scoped cleanup"

kill "$SERVER_PID"
wait "$SERVER_PID" || true
SERVER_PID=
start_listener 127.0.0.1
test "$(curl -fs --noproxy '*' http://127.0.0.1:18443)" = proxy-ok
for entry in external:192.0.2.1 sidecar:172.18.0.1; do
  IFS=: read -r namespace address <<< "$entry"
  if probe "$namespace" "$address"; then
    echo "Non-host client reached the loopback DIFC listener" >&2
    exit 1
  fi
done
echo "PASS: legacy DIFC host connectivity and non-host rejection"

node - "$SCRIPT_DIR/../js/mcp_http_server_runner.cjs" <<'NODE' &
const { runHttpServer } = require(process.argv[2]);
runHttpServer({
  port: 18444,
  apiKey: "network-fixture-key",
  serverLabel: "Network fixture",
  logger: { debug() {}, debugError: console.error },
  getHealthPayload: () => ({}),
  transport: { async handleRequest(req, res) { res.end("authenticated"); } },
});
NODE
MCP_PID=$!
for attempt in $(seq 1 50); do
  if curl -fs --noproxy '*' http://127.0.0.1:18444/health >/dev/null; then break; fi
  sleep 0.1
done
for entry in external:192.0.2.1 isolated:172.19.0.1; do
  IFS=: read -r namespace address <<< "$entry"
  for key in "" wrong-key network-fixture-key; do
    expected=401
    if [ "$key" = network-fixture-key ]; then expected=200; fi
    status="$(ip netns exec "$namespace" curl -s --noproxy '*' --max-time 2 -o /dev/null -w '%{http_code}' \
      -X POST -H "Authorization: $key" -H 'Content-Type: application/json' --data '{}' "http://$address:18444/")"
    test "$status" = "$expected"
  done
done
echo "PASS: MCP bridge reachability with valid keys; absent/wrong keys rejected on every interface"
