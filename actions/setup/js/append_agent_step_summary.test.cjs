import { afterEach, beforeEach, describe, expect, it } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import { spawnSync } from "child_process";

const script = path.join(__dirname, "append_agent_step_summary.cjs");

describe.skipIf(process.platform !== "linux")("agent step summary publication", () => {
  let directory, root, source, output, sentinel;

  beforeEach(() => {
    directory = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), "agent-summary-")));
    root = path.join(directory, "gh-aw");
    fs.mkdirSync(root);
    source = path.join(root, "agent-step-summary.md");
    output = path.join(directory, "published.md");
    sentinel = path.join(directory, "external.md");
    fs.writeFileSync(output, "existing\n");
    fs.writeFileSync(sentinel, "external-private-sentinel\n");
  });

  afterEach(() => {
    fs.chmodSync(root, 0o700);
    expect(fs.readFileSync(sentinel, "utf8")).toBe("external-private-sentinel\n");
    fs.rmSync(directory, { recursive: true, force: true });
  });

  function publish(preamble) {
    return spawnSync(process.execPath, preamble ? ["-e", `${preamble}\nrequire(${JSON.stringify(script)}).main();`] : [script], {
      env: { ...process.env, GH_AW_TMP_DIR: root, GITHUB_STEP_SUMMARY: output },
      encoding: "utf8",
      timeout: 10000,
    });
  }

  function expectRejected(result) {
    expect(result.error).toBeUndefined();
    expect(result.status).toBe(1);
    expect(result.stderr).toContain("Agent step summary could not be published safely");
    expect(result.stderr).not.toContain("external-private-sentinel");
    expect(fs.readFileSync(output, "utf8")).toBe("existing\n");
  }

  it("appends ordinary summaries without changing their bytes", () => {
    fs.writeFileSync(source, "# Agent result\n\nCompleted.\n");
    expect(publish().status).toBe(0);
    expect(fs.readFileSync(output, "utf8")).toBe("existing\n# Agent result\n\nCompleted.\n");
  });

  it.each(["missing", "empty"])("does nothing for a %s summary", state => {
    if (state === "empty") fs.writeFileSync(source, "");
    expect(publish().status).toBe(0);
    expect(fs.readFileSync(output, "utf8")).toBe("existing\n");
  });

  it.each(["existing", "dangling"])("rejects a %s source symlink", state => {
    fs.symlinkSync(state === "existing" ? sentinel : path.join(directory, "missing"), source);
    expectRejected(publish());
  });

  it.each(["root", "ancestor"])("rejects a symlink at the %s directory", level => {
    const external = path.join(directory, "external-directory");
    fs.mkdirSync(external);
    fs.copyFileSync(sentinel, path.join(external, "agent-step-summary.md"));
    if (level === "root") {
      fs.rmdirSync(root);
      fs.symlinkSync(external, root);
    } else {
      const link = path.join(directory, "linked-parent");
      fs.symlinkSync(directory, link);
      root = path.join(link, "external-directory");
    }
    expectRejected(publish());
  });

  it.each(["directory", "fifo"])("rejects a %s instead of a regular summary without blocking", type => {
    if (type === "directory") {
      fs.mkdirSync(source);
    } else {
      expect(spawnSync("mkfifo", [source]).status).toBe(0);
    }
    expectRejected(publish());
  });

  it.skipIf(process.getuid?.() === 0)("fails closed for a searchable but unlistable summary directory", () => {
    fs.symlinkSync(sentinel, source);
    fs.chmodSync(root, 0o300);
    expectRejected(publish());
  });

  it.each(["source", "ancestor"])("rejects %s replacement after opening without reading redirected content", level => {
    fs.writeFileSync(source, "sanitized summary\n");
    expectRejected(
      publish(`
const fs = require("fs");
const path = require("path");
const open = fs.openSync;
fs.openSync = (filename, ...args) => {
  const descriptor = open(filename, ...args);
  if (path.basename(filename) === ${JSON.stringify(level === "source" ? "agent-step-summary.md" : "gh-aw")}) {
    const root = process.env.GH_AW_TMP_DIR;
    if (path.basename(filename) === "gh-aw") {
      fs.renameSync(root, root + "-original");
      fs.mkdirSync(root + "-replacement");
      fs.symlinkSync(${JSON.stringify(sentinel)}, root + "-replacement/agent-step-summary.md");
      fs.symlinkSync(root + "-replacement", root);
    } else {
      fs.unlinkSync(root + "/agent-step-summary.md");
      fs.symlinkSync(${JSON.stringify(sentinel)}, root + "/agent-step-summary.md");
    }
  }
  return descriptor;
};
`)
    );
  });

  it("runs through the deployed shell wrapper", () => {
    fs.writeFileSync(source, "sanitized summary\n");
    const actions = path.join(root, "actions");
    fs.mkdirSync(actions);
    for (const filename of ["append_agent_step_summary.cjs", "symlink_guard.cjs", "error_codes.cjs", "error_helpers.cjs"]) {
      fs.copyFileSync(path.join(__dirname, filename), path.join(actions, filename));
    }
    const result = spawnSync("bash", [path.join(__dirname, "../sh/append_agent_step_summary.sh")], {
      env: { ...process.env, RUNNER_TEMP: directory, GH_AW_TMP_DIR: root, GITHUB_STEP_SUMMARY: output },
      encoding: "utf8",
    });
    expect(result.status).toBe(0);
    expect(fs.readFileSync(output, "utf8")).toBe("existing\nsanitized summary\n");
  });
});

it.skipIf(process.platform === "linux")("fails closed on platforms without descriptor-relative guarded reads", () => {
  const result = spawnSync(process.execPath, [script], { env: { ...process.env, GH_AW_TMP_DIR: os.tmpdir() }, encoding: "utf8" });
  expect(result.status).toBe(1);
  expect(result.stderr).toContain("requires Linux descriptor-relative paths");
});
