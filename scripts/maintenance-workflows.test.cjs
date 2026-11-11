"use strict";

const assert = require("node:assert/strict");
const { execFileSync, spawnSync } = require("node:child_process");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { test } = require("node:test");

const root = path.join(__dirname, "..");
const cleanupScript = path.join(root, "scripts/delete-old-copilot-branches.sh");
const maintenance = fs.readFileSync(path.join(root, ".github/workflows/copilot-maintenance.yml"), "utf8");
const upgrade = fs.readFileSync(path.join(root, ".github/workflows/upgrade-test.yml"), "utf8");
const now = 2000000000;
const old = now - 8 * 86400;
const hostileRefs = ["copilot/$(touch${IFS}injected)", "copilot/x;touch${IFS}injected;#", "copilot/quote'\"branch", "copilot/HEAD-old"];

function step(source, name) {
  const section = source.split(`- name: ${name}\n`)[1]?.split(/\n      - name:/)[0];
  assert.ok(section, `Missing step: ${name}`);
  const match = section.match(/^        run: \|\n((?:          .*\n|\n)+)/m);
  assert.ok(match, `Missing run block: ${name}`);
  return { section, run: match[1].replace(/^          /gm, "") };
}

function harness(t, fixture = {}) {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-maintenance-"));
  t.after(() => fs.rmSync(directory, { recursive: true, force: true }));
  const bin = path.join(directory, "bin");
  fs.mkdirSync(bin);
  fs.mkdirSync(path.join(directory, "scripts"));
  fs.symlinkSync(cleanupScript, path.join(directory, "scripts/delete-old-copilot-branches.sh"));
  fs.writeFileSync(path.join(directory, "fixture.json"), JSON.stringify({ now, branches: [], releases: [], ...fixture }));
  const mock = `#!${process.execPath}
const fs = require("node:fs");
const path = require("node:path");
const { execFileSync } = require("node:child_process");
const fixture = JSON.parse(fs.readFileSync(process.env.FIXTURE, "utf8"));
const tool = path.basename(process.argv[1]);
const args = process.argv.slice(2);
fs.appendFileSync(process.env.CALLS, JSON.stringify([tool, ...args]) + "\\n");
if (tool === "date" && args.join(" ") === "+%s") {
  console.log(fixture.now);
} else if (tool === "git" && args[0] === "fetch") {
  process.exit(fixture.fetchFailure ? 1 : 0);
} else if (tool === "git" && args[0] === "for-each-ref") {
  if (fixture.listFailure) process.exit(1);
  if (args[1] !== "--format=%(refname:strip=3)" || args[2] !== "refs/remotes/origin/copilot/") process.exit(2);
  for (const branch of fixture.branches) console.log(branch.name);
} else if (tool === "git" && args[0] === "log") {
  const branch = fixture.branches.find(branch => "origin/" + branch.name === args[3]);
  if (!branch || branch.date === null) process.exit(1);
  console.log(branch.date);
} else if (tool === "git" && args[0] === "push") {
  process.exit(fixture.pushFailure ? 1 : 0);
} else if (tool === "gh" && args[0] === "auth") {
  process.exit(0);
} else if (tool === "gh" && args[0] === "pr" && args[1] === "list") {
  if (fixture.prFailure) process.exit(1);
  const branch = fixture.branches.find(branch => branch.name === args[args.indexOf("--head") + 1]);
  if (!branch) process.exit(2);
  console.log(JSON.stringify(branch.state ? [{ number: 1, state: branch.state, headRefName: branch.name }] : []));
} else if (tool === "gh" && args[0] === "api") {
  if (fixture.apiFailure) process.exit(1);
  const json = JSON.stringify(fixture.releases);
  const jq = args.indexOf("--jq");
  process.stdout.write(jq === -1 ? json : execFileSync("jq", ["-r", args[jq + 1]], { input: json }));
} else {
  console.error("Unexpected mocked command", tool, args);
  process.exit(2);
}
`;
  for (const tool of ["git", "gh", "date"]) {
    fs.writeFileSync(path.join(bin, tool), mock, { mode: 0o755 });
  }
  const callsFile = path.join(directory, "calls.jsonl");
  const output = path.join(directory, "output");
  fs.writeFileSync(output, "");
  const env = {
    ...process.env,
    PATH: `${bin}${path.delimiter}${process.env.PATH}`,
    FIXTURE: path.join(directory, "fixture.json"),
    CALLS: callsFile,
    GITHUB_OUTPUT: output,
    MAX_BRANCHES: "0",
  };
  return {
    directory,
    run: (script, extraEnv = {}) => spawnSync("bash", ["--noprofile", "--norc", "-e", "-o", "pipefail", "-c", script], { cwd: directory, env: { ...env, ...extraEnv }, encoding: "utf8", timeout: 10000 }),
    cleanup: (args = [], extraEnv = {}) => spawnSync("bash", [cleanupScript, ...args], { cwd: directory, env: { ...env, ...extraEnv }, encoding: "utf8", timeout: 10000 }),
    calls: () =>
      fs.existsSync(callsFile)
        ? fs
            .readFileSync(callsFile, "utf8")
            .trim()
            .split("\n")
            .map(line => JSON.parse(line))
        : [],
    output: () => fs.readFileSync(output, "utf8"),
    assertNoSideEffects: () => assert.equal(fs.existsSync(path.join(directory, "injected")), false),
  };
}

test("cleanup preview preserves valid shell-special refs without pushing", t => {
  for (const ref of hostileRefs) execFileSync("git", ["check-ref-format", `refs/heads/${ref}`]);
  const fixture = harness(t, { branches: hostileRefs.map(name => ({ name, date: old, state: "CLOSED" })) });
  const result = fixture.cleanup();
  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, /Found 4 branch\(es\) to delete/);
  assert.deepEqual(
    fixture.calls().filter(call => call[1] === "push"),
    []
  );
  assert.deepEqual(
    fixture
      .calls()
      .filter(call => call[0] === "gh" && call[1] === "pr")
      .map(call => call[4]),
    hostileRefs
  );
  fixture.assertNoSideEffects();
});

test("maintenance deletion passes each ref as one literal argument", t => {
  const fixture = harness(t, { branches: hostileRefs.map(name => ({ name, date: old, state: "MERGED" })) });
  const deletion = step(maintenance, "Delete selected branches");
  assert.doesNotMatch(maintenance, /\beval\b/);
  assert.match(deletion.section, /if: github.event.inputs.delete_branches == 'true' \|\| github.event_name == 'schedule'/);
  const result = fixture.run(deletion.run);
  assert.equal(result.status, 0, result.stderr);
  assert.deepEqual(
    fixture.calls().filter(call => call[1] === "push"),
    hostileRefs.map(ref => ["git", "push", "origin", "--delete", "--", ref])
  );
  fixture.assertNoSideEffects();
});

test("cleanup retains age, PR-state, missing-date, and maximum-count filters", t => {
  const branches = [
    { name: "copilot/recent", date: now - 86400, state: "CLOSED" },
    { name: "copilot/boundary", date: now - 7 * 86400, state: "MERGED" },
    { name: "copilot/open", date: old, state: "OPEN" },
    { name: "copilot/unknown-date", date: null },
    { name: "copilot/closed", date: old, state: "CLOSED" },
    { name: "copilot/merged", date: old, state: "MERGED" },
    { name: "copilot/no-pr", date: old },
  ];
  const fixture = harness(t, { branches });
  const result = fixture.cleanup(["--delete"], { MAX_BRANCHES: "3.0" });
  assert.equal(result.status, 0, result.stderr);
  assert.deepEqual(
    fixture
      .calls()
      .filter(call => call[1] === "push")
      .map(call => call.at(-1)),
    ["copilot/closed", "copilot/merged", "copilot/no-pr"]
  );
  const limited = harness(t, { branches });
  const limitedResult = limited.cleanup(["--delete"], { MAX_BRANCHES: "1" });
  assert.equal(limitedResult.status, 0, limitedResult.stderr);
  assert.deepEqual(
    limited
      .calls()
      .filter(call => call[1] === "push")
      .map(call => call.at(-1)),
    ["copilot/closed"]
  );
});

for (const failure of ["fetchFailure", "listFailure", "prFailure", "pushFailure"]) {
  test(`cleanup reports ${failure} instead of successful deletion`, t => {
    const fixture = harness(t, { branches: [{ name: "copilot/old", date: old }], [failure]: true });
    const result = fixture.cleanup(["--delete"]);
    assert.notEqual(result.status, 0);
    if (failure !== "pushFailure")
      assert.deepEqual(
        fixture.calls().filter(call => call[1] === "push"),
        []
      );
  });
}

test("cleanup handles no branches without pushing", t => {
  const fixture = harness(t);
  const result = fixture.cleanup(["--delete"]);
  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, /No copilot\/\* branches found/);
  assert.deepEqual(
    fixture.calls().filter(call => call[1] === "push"),
    []
  );
});

test("release selection transports shell-special tags through environment data", t => {
  const selection = step(upgrade, "Find target and previous releases");
  assert.match(selection.section, /RELEASE_TAG: \$\{\{ github.event.release.tag_name \}\}/);
  assert.match(selection.section, /EVENT_NAME: \$\{\{ github.event_name \}\}/);
  assert.doesNotMatch(selection.run, /\$\{\{/);
  for (const tag of hostileRefs.slice(0, 3).map(ref => ref.replace("copilot/", "v1-"))) {
    execFileSync("git", ["check-ref-format", `refs/tags/${tag}`]);
    const fixture = harness(t, {
      releases: [
        { tag_name: tag, draft: false },
        { tag_name: "v0.9.0", draft: false },
      ],
    });
    const result = fixture.run(selection.run, { EVENT_NAME: "release", RELEASE_TAG: tag });
    assert.equal(result.status, 0, result.stderr);
    assert.equal(fixture.output(), `latest=${tag}\nprevious=v0.9.0\n`);
    assert.deepEqual(
      fixture.calls().map(call => call.slice(0, 2)),
      [["gh", "api"]]
    );
    fixture.assertNoSideEffects();
  }
});

test("scheduled upgrade selection skips drafts and includes prereleases", t => {
  const fixture = harness(t, {
    releases: [
      { tag_name: "draft", draft: true },
      { tag_name: "v1.0.0-rc.1", draft: false },
      { tag_name: "v0.9.0", draft: false },
    ],
  });
  const result = fixture.run(step(upgrade, "Find target and previous releases").run, { EVENT_NAME: "schedule", RELEASE_TAG: "" });
  assert.equal(result.status, 0, result.stderr);
  assert.equal(fixture.output(), "latest=v1.0.0-rc.1\nprevious=v0.9.0\n");
});

test("upgrade selection fails without two releases", t => {
  const fixture = harness(t, { releases: [{ tag_name: "v1.0.0", draft: false }] });
  const result = fixture.run(step(upgrade, "Find target and previous releases").run, { EVENT_NAME: "release", RELEASE_TAG: "v1.0.0" });
  assert.notEqual(result.status, 0);
  assert.equal(fixture.output(), "");
});
