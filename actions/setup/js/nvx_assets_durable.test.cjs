// @ts-check
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import fs from "node:fs";
import path from "node:path";
import { spawnSync } from "node:child_process";
import { collect, guestEnvironment, stage } from "./nvx_assets.cjs";

describe.skipIf(process.platform === "linux" && !fs.existsSync("/usr/bin/patchelf"))("NVX Pi durable dependency handoff", () => {
  /** @type {string} */
  let root;
  /** @type {string} */
  let workspace;
  /** @type {string} */
  let hostRoot;
  /** @type {string} */
  let state;
  /** @type {NodeJS.ProcessEnv} */
  let environment;

  beforeEach(() => {
    root = fs.mkdtempSync(path.join(process.cwd(), ".nvx-durable-test-"));
    workspace = path.join(root, "workspace");
    hostRoot = path.join(root, "runner", "gh-aw");
    state = path.join(root, "state");
    fs.mkdirSync(workspace);
    fs.mkdirSync(path.join(hostRoot, "actions"), { recursive: true });
    fs.mkdirSync(state);
    for (const name of ["nvx_assets.cjs", "copilot_sdk_mcp_config_path.cjs"]) fs.copyFileSync(path.join(import.meta.dirname, name), path.join(hostRoot, "actions", name));
    for (const name of ["pi-durable", "pi-ai", "chord", "pi-telemetry"]) {
      const directory = path.join(state, "pi-durable-runtime", "node_modules", "@earendil-works", name);
      fs.mkdirSync(path.join(directory, "dist"), { recursive: true });
      fs.writeFileSync(path.join(directory, "package.json"), JSON.stringify({ name: `@earendil-works/${name}`, version: "1.4.0", type: "module" }));
      fs.writeFileSync(path.join(directory, "dist", "index.js"), `export const name = ${JSON.stringify(name)};`);
    }
    environment = { GITHUB_WORKSPACE: workspace, RUNNER_TEMP: path.dirname(hostRoot), GH_AW_PI_DURABLE_PACKAGE_ROOT: "/tmp/gh-aw/pi-durable-runtime/node_modules" };
  });

  afterEach(() => fs.rmSync(root, { recursive: true, force: true }));

  it("loads every durable package from the exported guest state after host assets are removed", () => {
    const guestRoot = path.join(workspace, ".gh-aw-nvx");
    stage(environment, state, guestRoot);
    const guestEnv = guestEnvironment(environment, guestRoot);
    expect(guestEnv.GH_AW_PI_DURABLE_PACKAGE_ROOT).toBe(environment.GH_AW_PI_DURABLE_PACKAGE_ROOT);
    fs.rmSync(state, { recursive: true });
    fs.rmSync(hostRoot, { recursive: true });

    // Use a private state link in place of the guest's /tmp/gh-aw.
    const stateLink = path.join(root, "guest-state");
    const packageRoot = path.join(stateLink, path.posix.relative("/tmp/gh-aw", guestEnv.GH_AW_PI_DURABLE_PACKAGE_ROOT || ""));
    const script = path.join(root, "verify-packages.mjs");
    fs.writeFileSync(
      script,
      `
import fs from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";
for (const name of ["pi-durable", "pi-ai", "chord", "pi-telemetry"]) {
  const directory = path.join(process.env.GH_AW_PI_DURABLE_PACKAGE_ROOT, "@earendil-works", name);
  const manifest = JSON.parse(fs.readFileSync(path.join(directory, "package.json"), "utf8"));
  if (manifest.version !== "1.4.0") throw new Error("Missing durable dependency");
  if ((await import(pathToFileURL(path.join(directory, "dist", "index.js")).href)).name !== name) throw new Error("Wrong durable dependency");
}
`
    );
    const bootstrap = path.join(guestRoot, "gh-aw", "actions", "nvx_assets.cjs");
    const node = process.platform === "darwin" ? process.execPath : path.join(guestRoot, "gh-aw", "bin", "node");
    const command = `${JSON.stringify(node)} ${JSON.stringify(script)}`;
    const result = spawnSync(node, ["-e", `require(${JSON.stringify(bootstrap)}).guest(${JSON.stringify(command)}, ${JSON.stringify(guestRoot)}, ${JSON.stringify(stateLink)})`], {
      env: { ...process.env, GH_AW_PI_DURABLE_PACKAGE_ROOT: packageRoot },
      encoding: "utf8",
    });
    expect(result.error).toBeUndefined();
    expect(result.status, result.stderr).toBe(0);
    expect(fs.realpathSync(packageRoot)).toBe(path.join(guestRoot, "state", "pi-durable-runtime", "node_modules"));
  });

  it("rejects redirected durable dependency directories", () => {
    fs.rmSync(path.join(state, "pi-durable-runtime"), { recursive: true });
    fs.symlinkSync(hostRoot, path.join(state, "pi-durable-runtime"));
    expect(() => stage(environment, state)).toThrow(/must not be a symlink/);
    collect(environment, state);
  });
});
