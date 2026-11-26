// @ts-check
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { spawnSync } = require("node:child_process");

describe("DIFC proxy Docker startup", () => {
  it.each([
    { isolation: "false", failures: 0, network: "host", listen: "127.0.0.1:18443", publish: [] },
    { isolation: "true", failures: 0, network: "bridge", listen: "0.0.0.0:18443", publish: ["127.0.0.1:18443:18443"] },
    { isolation: "false", failures: 1, network: "host", listen: "127.0.0.1:18443", publish: [] },
    { isolation: "false", failures: 2, network: "host", listen: "127.0.0.1:18443", publish: [] },
    { isolation: "false", failures: 3, network: "host", listen: "127.0.0.1:18443", publish: [] },
  ])("pulls before running with isolation=$isolation and $failures pull failures", ({ isolation, failures, network, listen, publish }) => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-difc-proxy-test-"));
    try {
      const calls = path.join(root, "docker-run-argv");
      const pullCount = path.join(root, "pull-count");
      const events = path.join(root, "events");
      const image = `ghcr.io/github/gh-aw-mcpg:v0.0.0@sha256:${"a".repeat(64)}`;
      // Only allow the local tools needed to reach docker run. Stop there before readiness or host setup.
      for (const tool of ["dirname", "id", "bash", "seq", "timeout", "xargs"]) {
        fs.symlinkSync(`/usr/bin/${tool}`, path.join(root, tool));
      }
      fs.writeFileSync(path.join(root, "mkdir"), "#!/bin/bash\nexit 0\n", { mode: 0o700 });
      fs.writeFileSync(path.join(root, "sleep"), '#!/bin/bash\nprintf "sleep %s\\n" "$1" >> "$TEST_EVENTS"\n', { mode: 0o700 });
      fs.writeFileSync(
        path.join(root, "docker"),
        `#!/bin/bash
case "$1" in
  rm|tag) exit 0 ;;
  pull)
    count=0
    if [[ -f "$TEST_PULL_COUNT" ]]; then read -r count < "$TEST_PULL_COUNT"; fi
    count=$((count + 1))
    printf '%s\\n' "$count" > "$TEST_PULL_COUNT"
    printf '%s\\n' "$*" >> "$TEST_EVENTS"
    if (( count <= TEST_PULL_FAILURES )); then exit 125; fi
    exit 0 ;;
  run)
    printf 'run\\n' >> "$TEST_EVENTS"
    printf '%s\\0' "$@" > "$TEST_DOCKER_RUN_ARGV"; exit 73 ;;
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
          DIFC_PROXY_IMAGE: image,
          GH_AW_NETWORK_ISOLATION: isolation,
          TEST_DOCKER_RUN_ARGV: calls,
          TEST_PULL_COUNT: pullCount,
          TEST_PULL_FAILURES: String(failures),
          TEST_EVENTS: events,
        },
      });
      const expectedEvents = [];
      for (let attempt = 0; attempt < Math.min(failures + 1, 3); attempt++) {
        expectedEvents.push(`pull --quiet ${image}`);
        if (attempt < Math.min(failures, 2)) expectedEvents.push(`sleep ${5 * 2 ** attempt}`);
      }
      if (failures < 3) expectedEvents.push("run");
      expect(fs.readFileSync(events, "utf8").trim().split("\n")).toEqual(expectedEvents);
      if (failures === 3) {
        expect(result.status).not.toBe(0);
        expect(fs.existsSync(calls)).toBe(false);
        expect(result.stdout).toContain(`Failed to pull ${image} after 3 attempts`);
        return;
      }
      expect(result.status).toBe(73);

      const argv = fs.readFileSync(calls, "utf8").split("\0");
      const values = (...flags) => argv.flatMap((arg, index) => (flags.includes(arg) ? [argv[index + 1]] : []));
      expect(values("--network")).toEqual([network]);
      expect(values("--listen")).toEqual([listen]);
      expect(values("-p", "--publish")).toEqual(publish);
      expect(argv).not.toContain("-P");
      expect(argv).not.toContain("--publish-all");
      expect(argv).toContain(image);
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });
});
