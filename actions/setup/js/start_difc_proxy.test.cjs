// @ts-check
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { spawnSync } = require("node:child_process");

describe("DIFC proxy Docker networking", () => {
  it.each([
    { isolation: "false", network: "host", listen: "127.0.0.1:18443", publish: [] },
    { isolation: "true", network: "bridge", listen: "0.0.0.0:18443", publish: ["127.0.0.1:18443:18443"] },
  ])("passes the expected network arguments with isolation=$isolation", ({ isolation, network, listen, publish }) => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-difc-proxy-test-"));
    try {
      const calls = path.join(root, "docker-run-argv");
      // Only allow the local tools needed to reach docker run. Stop there before readiness or host setup.
      fs.symlinkSync("/usr/bin/dirname", path.join(root, "dirname"));
      fs.symlinkSync("/usr/bin/id", path.join(root, "id"));
      fs.writeFileSync(path.join(root, "mkdir"), "#!/bin/bash\nexit 0\n", { mode: 0o700 });
      fs.writeFileSync(
        path.join(root, "docker"),
        `#!/bin/bash
case "$1" in
  rm) exit 0 ;;
  run) printf '%s\\0' "$@" > "$TEST_DOCKER_RUN_ARGV"; exit 73 ;;
  *) exit 1 ;;
esac
`,
        { mode: 0o700 }
      );

      const result = spawnSync("/bin/bash", [path.join(__dirname, "../sh/start_difc_proxy.sh")], {
        encoding: "utf8",
        env: {
          PATH: root,
          RUNNER_TEMP: root,
          GITHUB_SERVER_URL: "https://github.com",
          DIFC_PROXY_POLICY: '{"allow": []}',
          DIFC_PROXY_IMAGE: "synthetic-proxy:latest",
          GH_AW_NETWORK_ISOLATION: isolation,
          TEST_DOCKER_RUN_ARGV: calls,
        },
      });
      expect(result.status).toBe(73);

      const argv = fs.readFileSync(calls, "utf8").split("\0");
      const values = (...flags) => argv.flatMap((arg, index) => (flags.includes(arg) ? [argv[index + 1]] : []));
      expect(values("--network")).toEqual([network]);
      expect(values("--listen")).toEqual([listen]);
      expect(values("-p", "--publish")).toEqual(publish);
      expect(argv).not.toContain("-P");
      expect(argv).not.toContain("--publish-all");
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });
});
