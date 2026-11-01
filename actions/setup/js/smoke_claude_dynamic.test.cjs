import fs from "node:fs";
import path from "node:path";
import { execFileSync } from "node:child_process";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";
import { describe, test, expect } from "vitest";

const require = createRequire(import.meta.url);
const workspace = fileURLToPath(new URL("../../../", import.meta.url));
const workflow = fs.readFileSync(path.join(workspace, ".github/workflows/smoke-claude-dynamic.md"), "utf8");
const verification = workflow
  .split("      node <<'NODE'\n")[1]
  .split("      const result =")[0]
  .replace(/^      /gm, "");
const script = ".claude/workflows/smoke-claude-dynamic.js";
const fixture = ".claude/workflows/references/smoke-claude-dynamic/.context";

function verifySnapshot({ members = [script, fixture], changedFile, archive } = {}) {
  const snapshot = archive ?? execFileSync("tar", ["-cf", "-", "--", ...members], { cwd: workspace });
  const mockRequire = name => {
    if (name === "node:fs") {
      return {
        readFileSync: file => {
          expect(file.startsWith(workspace)).toBe(true);
          return file === path.join(workspace, changedFile ?? "") ? Buffer.from("modified") : fs.readFileSync(file);
        },
      };
    }
    if (name === "node:child_process") {
      return {
        execFileSync: (command, args) => {
          expect(command).toBe("tar");
          expect(args.slice(0, 3)).toEqual(["-xOf", "/tmp/gh-aw/base/snapshot.tar", "--"]);
          return execFileSync(command, ["-xOf", "-", "--", args[3]], { input: snapshot, stdio: ["pipe", "pipe", "pipe"] });
        },
      };
    }
    if (name.endsWith("log_parser_shared.cjs") || name.endsWith("claude_session.cjs")) return {};
    return require(name);
  };
  new Function("require", "process", verification)(mockRequire, {
    env: { GITHUB_WORKSPACE: workspace, RUNNER_TEMP: "/runner" },
  });
}

describe("Claude dynamic smoke trusted activation snapshot", () => {
  test("reads the script and nested hidden fixture directly from the tar archive", () => {
    expect(() => verifySnapshot()).not.toThrow();
  });

  test.each([script, fixture])("rejects changed workspace content for %s", changedFile => {
    expect(() => verifySnapshot({ changedFile })).toThrow("must match the trusted activation artifact");
  });

  test("rejects an archive missing the hidden fixture", () => {
    expect(() => verifySnapshot({ members: [script] })).toThrow();
  });

  test("rejects an invalid archive", () => {
    expect(() => verifySnapshot({ archive: Buffer.from("invalid archive") })).toThrow();
  });
});
