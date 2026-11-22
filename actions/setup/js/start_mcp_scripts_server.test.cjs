const assert = require("assert");
const path = require("path");
const { spawnSync } = require("child_process");

describe("MCP Scripts bind address selection", () => {
  const scriptPath = path.join(__dirname, "../sh/start_mcp_scripts_server.sh");

  it.each([
    ["host networking", "false", "", 1, "127.0.0.1", ""],
    ["dual-stack bridge", "true", "fd00::1\n172.17.0.1", 0, "172.17.0.1", ""],
    ["wildcard gateway", "true", "fd00::1\n0.0.0.0", 0, "", "Cannot determine the IPv4 Docker bridge gateway"],
    ["empty gateway", "true", "", 0, "", "Cannot determine the IPv4 Docker bridge gateway"],
    ["invalid IPv4 gateway", "true", "172.17.0.256", 0, "", "Cannot determine the IPv4 Docker bridge gateway"],
    ["Docker inspection failure", "true", "172.17.0.1", 1, "", "Cannot inspect the default Docker bridge"],
  ])("%s", (_name, bridged, gateways, dockerStatus, expected, error) => {
    const result = spawnSync(
      "bash",
      [
        "-ec",
        `docker() {
          [[ "$1 $2 $3" == "network inspect bridge" ]] || return 2
          printf '%s\\n' "$TEST_GATEWAYS"
          return "$TEST_DOCKER_STATUS"
        }
        source "$1"
        select_mcp_scripts_bind_host`,
        "bash",
        scriptPath,
      ],
      {
        env: { ...process.env, GH_AW_MCP_SCRIPTS_BIND_HOST: "0.0.0.0", GH_AW_MCP_SCRIPTS_BRIDGED_GATEWAY: bridged, TEST_GATEWAYS: gateways, TEST_DOCKER_STATUS: String(dockerStatus) },
        encoding: "utf8",
      }
    );
    assert.strictEqual(result.status, error ? 1 : 0, result.stderr);
    assert.strictEqual(result.stdout.trim(), expected);
    if (error) {
      assert.ok(result.stderr.includes(`::error::${error}`), result.stderr);
    }
  });
});
