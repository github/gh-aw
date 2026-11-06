import { describe, expect, it } from "vitest";
import fs from "fs";
import path from "path";
import os from "os";
import vm from "vm";
import { spawnSync } from "child_process";
import { fileURLToPath } from "url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

describe("shared-runner cleanup boundary", () => {
  it("post.js sends its conclusion without mutating shared runtime state", async () => {
    const mutations = [];
    const warnings = [];
    let conclusions = 0;
    const script = fs.readFileSync(path.join(__dirname, "../post.js"), "utf8");
    const execute = async present => {
      await vm.runInNewContext(script, {
        __dirname: path.resolve(__dirname, ".."),
        process: { env: {} },
        console: { warn: message => warnings.push(message), log: () => {}, error: () => {} },
        require(name) {
          if (name === "path") return path;
          if (name === "fs") return { existsSync: () => present, rmSync: (...args) => mutations.push(args) };
          if (name === "child_process") return { spawnSync: (...args) => mutations.push(args) };
          if (name.endsWith("action_conclusion_otlp.cjs"))
            return {
              run: async () => {
                conclusions += 1;
              },
            };
          throw new Error(`Unexpected import: ${name}`);
        },
      });
    };
    await execute(false);
    expect(warnings).toEqual([]);
    await execute(true);
    expect(conclusions).toBe(2);
    expect(mutations).toEqual([]);
    expect(warnings).toEqual(["Warning: leaving shared /tmp/gh-aw untouched; per-invocation ownership cannot be established."]);
  });

  it("post.js invokes only the scoped AWF runtime cleanup recorded by setup", async () => {
    const calls = [];
    const script = fs.readFileSync(path.join(__dirname, "../post.js"), "utf8");
    vm.runInNewContext(script, {
      __dirname: path.resolve(__dirname, ".."),
      process: { env: { GH_AW_RUNTIME_TMP_DIR: "/tmp/gh-aw-runtime-test" } },
      console: { warn: () => {}, log: () => {}, error: () => {} },
      require(name) {
        if (name === "path") return path;
        if (name === "fs") return { existsSync: () => false };
        if (name === "child_process") {
          return {
            spawnSync: (...args) => {
              calls.push(args);
              return { status: 0 };
            },
          };
        }
        if (name.endsWith("action_conclusion_otlp.cjs")) return { run: async () => {} };
        throw new Error(`Unexpected import: ${name}`);
      },
    });
    await new Promise(resolve => setImmediate(resolve));
    expect(calls).toEqual([["bash", [path.join(__dirname, "../sh/cleanup_runtime_tmp.sh")], { stdio: "inherit" }]]);
  });

  it("clean.sh never invokes removal or privileged cleanup", () => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-clean-test-"));
    try {
      const bin = path.join(root, "bin");
      const log = path.join(root, "mutations");
      const sibling = path.join(root, "awf-sibling-chroot-home");
      fs.mkdirSync(bin);
      fs.mkdirSync(sibling);
      fs.writeFileSync(path.join(sibling, "artifact"), "other job");
      for (const command of ["sudo", "rm", "find"]) {
        fs.writeFileSync(path.join(bin, command), '#!/bin/sh\nprintf "%s\\n" "$0 $*" >> "$MUTATION_LOG"\nexit 99\n', { mode: 0o755 });
      }
      const result = spawnSync("bash", [path.resolve(__dirname, "../clean.sh")], {
        cwd: root,
        env: { ...process.env, RUNNER_TEMP: root, INPUT_DESTINATION: root, PATH: `${bin}:${process.env.PATH}`, MUTATION_LOG: log },
        encoding: "utf8",
      });
      expect(result.status, result.stderr).toBe(0);
      expect(fs.existsSync(log)).toBe(false);
      expect(fs.readFileSync(path.join(sibling, "artifact"), "utf8")).toBe("other job");
      expect(result.stderr).not.toContain("untracked AWF chroot");
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });
});
