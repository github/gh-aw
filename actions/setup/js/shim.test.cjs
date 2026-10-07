import { describe, expect, it } from "vitest";
import { spawnSync } from "child_process";
import { join } from "path";
import { writeFileSync, rmSync, mkdtempSync, realpathSync } from "node:fs";

const temporaryDirectory = prefix => mkdtempSync(join(realpathSync(process.cwd()), `.gh-aw-${prefix}-`));

describe("core shim", () => {
  it.each([0, 1, 2])("preserves injected SDK objects across reloads with %i Claims and stray disabled roles", count => {
    const shimPath = join(import.meta.dirname, "shim.cjs");
    const result = spawnSync(
      process.execPath,
      [
        "-e",
        `
      const assert = require("node:assert/strict");
      const assignment = {claims: Array.from({length: ${count}}, (_, i) => ({handle: "h" + i}))};
      const core = global.core = {setSecret() {}, info() {}};
      const context = global.context = {payload: {inputs: {work_queue_assignment: assignment}}};
      const github = global.github = {injected: true};
      require(${JSON.stringify(shimPath)});
      delete require.cache[require.resolve(${JSON.stringify(shimPath)})];
      require(${JSON.stringify(shimPath)});
      assert.equal(global.core, core);
      assert.equal(global.context, context);
      assert.equal(global.github, github);
      assert.equal(global.context.payload.inputs.work_queue_assignment, assignment);
    `,
      ],
      { encoding: "utf8", env: { ...process.env, GH_AW_WORK_QUEUE_ENABLED: count ? "true" : "false", GH_AW_WORK_QUEUE_ROLE: "stray-role", GITHUB_EVENT_PATH: "" } }
    );
    expect(result.status).toBe(0);
    expect(result.stdout).toBe("");
    expect(result.stderr).toBe("");
  });

  it("rejects setSecret outside the github-script runtime", () => {
    const shimPath = join(import.meta.dirname, "shim.cjs");
    const result = spawnSync(process.execPath, ["-e", `require(${JSON.stringify(shimPath)}); core.setSecret("derived-value");`], {
      encoding: "utf8",
    });

    expect(result.status).not.toBe(0);
    expect(result.stderr).toContain("core.setSecret is unavailable outside the github-script runtime");
  });

  it("adds a throwing setSecret to an existing partial core object", () => {
    const shimPath = join(import.meta.dirname, "shim.cjs");
    const result = spawnSync(process.execPath, ["-e", `global.core = { info() {} }; require(${JSON.stringify(shimPath)}); core.setSecret("derived-value");`], {
      encoding: "utf8",
    });

    expect(result.status).not.toBe(0);
    expect(result.stderr).toContain("core.setSecret is unavailable outside the github-script runtime");
  });

  it("accepts Error annotations on stderr without contaminating MCP stdout", () => {
    const shimPath = join(import.meta.dirname, "shim.cjs");
    const result = spawnSync(process.execPath, ["-e", `require(${JSON.stringify(shimPath)}); for (const name of ["notice", "warning", "error"]) core[name](new Error(name));`], { encoding: "utf8" });
    expect(result.status).toBe(0);
    expect(result.stdout).toBe("");
    for (const name of ["notice", "warning", "error"]) expect(result.stderr).toContain(`[${name}] Error: ${name}`);
  });

  it("retains failure exit status when passed an Error", () => {
    const shimPath = join(import.meta.dirname, "shim.cjs");
    const result = spawnSync(process.execPath, ["-e", `require(${JSON.stringify(shimPath)}); core.setFailed(new Error("failed"));`], { encoding: "utf8" });
    expect(result.status).toBe(1);
    expect(result.stderr).toContain("[error] Error: failed");
    expect(result.stdout).toBe("");
  });

  it("fails explicitly for unsupported Actions APIs without manufacturing values or executing callbacks", () => {
    const shimPath = join(import.meta.dirname, "shim.cjs");
    const script = `
      const assert = require("node:assert/strict");
      require(${JSON.stringify(shimPath)});
      for (const name of ["getInput", "getBooleanInput", "getState", "getIDToken", "exportVariable", "addPath"])
        assert.throws(() => core[name]("test"), new RegExp("core." + name + " is unavailable"));
      let called = false;
      assert.throws(() => core.group("test", async () => { called = true; }), /core.group is unavailable/);
      assert.equal(called, false);
      assert.throws(() => core.summary, /core.summary is unavailable/);
      assert.throws(() => core.platform, /core.platform is unavailable/);
      assert.equal(core.ExitCode.Success, 0);
      assert.equal(core.ExitCode.Failure, 1);
      const copy = {...core, ...context};
      assert.equal(typeof copy.info, "function");
      assert.equal(Object.hasOwn(copy, "summary"), false);
      assert.equal(Object.hasOwn(copy, "issue"), false);
    `;
    const result = spawnSync(process.execPath, ["-e", script], { encoding: "utf8", env: { ...process.env, GITHUB_EVENT_PATH: "", GITHUB_REPOSITORY: "" } });
    expect(result.status).toBe(0);
    expect(result.stdout).toBe("");
    expect(result.stderr).toBe("");
  });

  it.each(["issue", "pull_request"])("mirrors native runAttempt and the %s getter without adding enumerable prototype fields", kind => {
    const root = temporaryDirectory("shim-context");
    const eventPath = join(root, "event.json");
    writeFileSync(eventPath, JSON.stringify({ [kind]: { number: 42 } }));
    const shimPath = join(import.meta.dirname, "shim.cjs");
    try {
      const result = spawnSync(process.execPath, ["-e", `require(${JSON.stringify(shimPath)}); console.log(JSON.stringify({ attempt: context.runAttempt, issue: context.issue, keys: Object.keys(context) }));`], {
        encoding: "utf8",
        env: { ...process.env, GITHUB_EVENT_PATH: eventPath, GITHUB_REPOSITORY: "owner/repo", GITHUB_RUN_ATTEMPT: "2" },
      });
      expect(result.status).toBe(0);
      const data = JSON.parse(result.stdout);
      expect(data.attempt).toBe(2);
      expect(data.issue).toEqual({ owner: "owner", repo: "repo", number: 42 });
      expect(data.keys).not.toContain("issue");
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });
});
