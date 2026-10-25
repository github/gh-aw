"use strict";

const assert = require("node:assert/strict");
const { execFileSync } = require("node:child_process");
const fs = require("node:fs");
const path = require("node:path");
const { test } = require("node:test");

const AsyncFunction = Object.getPrototypeOf(async function () {}).constructor;
const root = path.join(__dirname, "..");
const promotion = fs.readFileSync(path.join(root, ".github/workflows/promote-release.yml"), "utf8");
const ci = fs.readFileSync(path.join(root, ".github/workflows/ci.yml"), "utf8");
const release = fs.readFileSync(path.join(root, ".github/workflows/release.md"), "utf8");
const alignment = ci.split("  validate-latest-releases:\n")[1].split("  validate-container-pins:\n")[0];

function script(source, step) {
  const section = source.split(`- name: ${step}\n`)[1];
  assert.ok(section, `Missing step: ${step}`);
  const match = section.match(/^( +)script: \|\n((?:\1  .*\n|\n)+)/m);
  assert.ok(match, `Missing script for: ${step}`);
  return new AsyncFunction("github", "context", "core", "process", match[2].replace(new RegExp(`^${match[1]}  `, "gm"), ""));
}

const promote = script(promotion, "Validate and promote release");
const compare = script(alignment, "Compare latest releases");
const stable = { tag_name: "v1.2.3", draft: false, prerelease: false };

test("release checkout is credential-free while tag publishing retains git authentication", () => {
  const lock = fs.readFileSync(path.join(root, ".github/workflows/release.lock.yml"), "utf8");
  for (const source of [release, lock]) {
    const releaseJob = source.split("\n  release:\n")[1]?.split(/\n  \w+:\n/)[0];
    assert.ok(releaseJob, "Missing release job");
    assert.match(releaseJob, /fetch-depth: 0\n +persist-credentials: false/);
    assert.match(releaseJob, /GH_TOKEN: \$\{\{ secrets.GITHUB_TOKEN \}\}/);
    assert.doesNotMatch(releaseJob, /\bgit (?:fetch|pull|push|clone|ls-remote)\b/);

    const pushTagJob = source.split("\n  push_tag:\n")[1]?.split(/\n  \w+:\n/)[0];
    assert.ok(pushTagJob, "Missing push_tag job");
    assert.match(pushTagJob, /persist-credentials: true/);
    assert.match(pushTagJob, /git push origin "\$RELEASE_TAG"/);
  }
});

test("release safe-output example preserves apostrophes, newlines, and Unicode through Bash and JSON", () => {
  const outputFormat = release.split("## Output Format\n")[1];
  assert.ok(outputFormat, "Missing release output instructions");
  const example = outputFormat.match(/```bash\n(safeoutputs update_release [^\n]+)\n```/);
  assert.ok(example, "Missing inline JSON safeoutputs example");
  const output = execFileSync("bash", ["--noprofile", "--norc", "-c", `safeoutputs() { printf '%s\\n' "$#"; printf '%s\\n' "$1"; printf '%s' "$2"; }\n${example[1]}`], { encoding: "utf8", timeout: 5000 });
  const [argumentCount, tool, ...payload] = output.split("\n");
  assert.equal(argumentCount, "2");
  assert.equal(tool, "update_release");
  assert.deepEqual(JSON.parse(payload.join("\n")), {
    tag: "v0.93.1",
    operation: "prepend",
    body: "## 🌟 Release Highlights\n\n### ✨ What's New\n\n[Your complete markdown highlights here]",
  });
  assert.match(outputFormat, /native `update_release` function is explicitly exposed/);
  assert.doesNotMatch(outputFormat, /not a bash command|Use bash to simulate tool calls|MUST call the `update_release` MCP tool/);
});

function harness({ actionsRelease = stable, release = { ...stable, id: 123, prerelease: true }, error } = {}) {
  const calls = [];
  const failures = [];
  const github = {
    rest: {
      repos: {
        getReleaseByTag: async parameters => {
          calls.push(["getReleaseByTag", parameters]);
          return { data: release };
        },
        getLatestRelease: async parameters => {
          calls.push(["getLatestRelease", parameters]);
          if (error) throw error;
          return { data: parameters.repo === "gh-aw-actions" ? actionsRelease : release };
        },
        updateRelease: async parameters => calls.push(["updateRelease", parameters]),
      },
    },
  };
  const context = { repo: { owner: "github", repo: "gh-aw" } };
  const core = { info() {}, setFailed: message => failures.push(message) };
  const run = (fn, version = "v1.2.3") => fn(github, context, core, { env: { INPUT_VERSION: version } });
  return { calls, failures, run };
}

test("promotion requires gh-aw-actions to be stable and latest before updating gh-aw", async () => {
  const fixture = harness();
  await fixture.run(promote, " v1.2.3 ");
  assert.deepEqual(fixture.failures, []);
  assert.deepEqual(fixture.calls, [
    ["getReleaseByTag", { owner: "github", repo: "gh-aw", tag: "v1.2.3" }],
    ["getLatestRelease", { owner: "github", repo: "gh-aw-actions" }],
    ["updateRelease", { owner: "github", repo: "gh-aw", release_id: 123, prerelease: false, make_latest: "true" }],
  ]);
});

for (const [label, actionsRelease] of [
  ["older latest (target is still draft or prerelease)", { ...stable, tag_name: "v1.2.2" }],
  ["newer latest", { ...stable, tag_name: "v1.2.4" }],
  ["draft", { ...stable, draft: true }],
  ["prerelease", { ...stable, prerelease: true }],
]) {
  test(`promotion rejects gh-aw-actions ${label} without mutating releases`, async () => {
    const fixture = harness({ actionsRelease });
    await fixture.run(promote);
    assert.equal(fixture.failures.length, 1);
    assert.match(fixture.failures[0], /Manually publish "v1.2.3".*first/);
    assert.equal(
      fixture.calls.some(([method]) => method === "updateRelease"),
      false
    );
  });
}

for (const status of [404, 403, 500]) {
  test(`promotion fails closed when downstream latest lookup fails (${status})`, async () => {
    const fixture = harness({ error: Object.assign(new Error("API failure"), { status }) });
    await fixture.run(promote);
    assert.equal(fixture.failures.length, 1);
    assert.match(fixture.failures[0], /Cannot verify.*Manually publish/);
    assert.equal(
      fixture.calls.some(([method]) => method === "updateRelease"),
      false
    );
  });
}

test("promotion still rejects an empty version or a draft gh-aw release", async () => {
  for (const [version, release] of [
    [" ", stable],
    ["v1.2.3", { ...stable, draft: true }],
  ]) {
    const fixture = harness({ release });
    await fixture.run(promote, version);
    assert.equal(fixture.failures.length, 1);
    assert.equal(
      fixture.calls.some(([method]) => method === "getLatestRelease" || method === "updateRelease"),
      false
    );
  }
});

test("promotion remains idempotent for an already stable gh-aw release", async () => {
  const fixture = harness({ release: { ...stable, id: 123 } });
  await fixture.run(promote);
  assert.deepEqual(fixture.failures, []);
  assert.equal(fixture.calls.at(-1)[0], "updateRelease");
});

test("CI compares both latest release endpoints without writes", async () => {
  const fixture = harness({ release: stable });
  await fixture.run(compare);
  assert.deepEqual(fixture.failures, []);
  assert.deepEqual(fixture.calls, [
    ["getLatestRelease", { owner: "github", repo: "gh-aw" }],
    ["getLatestRelease", { owner: "github", repo: "gh-aw-actions" }],
  ]);
});

test("CI fails when latest tags or stable publication status do not match", async () => {
  for (const overrides of [
    { release: { ...stable, tag_name: "v1.2.2" } },
    { actionsRelease: { ...stable, tag_name: "v1.2.4" } },
    { release: { ...stable, draft: true } },
    { release: { ...stable, prerelease: true } },
    { actionsRelease: { ...stable, draft: true } },
    { actionsRelease: { ...stable, prerelease: true } },
  ]) {
    const fixture = harness({ release: stable, ...overrides });
    await fixture.run(compare);
    assert.equal(fixture.failures.length, 1);
    assert.match(fixture.failures[0], /Latest releases must match/);
    assert.equal(
      fixture.calls.some(([method]) => method === "updateRelease"),
      false
    );
  }
});

test("CI fails closed when a latest release is unavailable", async () => {
  const error = new Error("Not Found");
  const fixture = harness({ release: stable, error });
  await assert.rejects(() => fixture.run(compare), error);
});

test("alignment check is read-only, skips forks, and runs independently of recent changes", () => {
  assert.match(alignment, /contents: read/);
  assert.match(alignment, /if: \$\{\{ !github.event.repository.fork \}\}/);
  assert.doesNotMatch(alignment, /needs:|has_changes|secrets\.|checkout@/);
  assert.match(promotion, /INPUT_VERSION: \$\{\{ inputs.version \}\}/);
  assert.ok(fs.readFileSync(path.join(root, "Makefile"), "utf8").includes("node --test scripts/release-workflows.test.cjs"));
});
