const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { randomUUID } = require("node:crypto");
const { spawnSync } = require("node:child_process");
const { test } = require("node:test");

const folders = ".agents .claude .codex .gemini .github";
const files = "AGENTS.md CLAUDE.md GEMINI.md";

function fixture(t) {
  const root = path.join(process.cwd(), ".base-github-folders-tests-" + randomUUID());
  const workspace = path.join(root, "workspace");
  const base = path.join(root, "base");
  fs.mkdirSync(workspace, { recursive: true });
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  for (const operation of ["save", "restore"]) {
    const original = fs.readFileSync(path.join(__dirname, operation + "_base_github_folders.sh"), "utf8");
    fs.writeFileSync(path.join(root, operation + ".sh"), original.replaceAll("/tmp/gh-aw/base", base));
  }
  const write = (name, content = "trusted", mode = 0o644) => {
    const destination = path.join(workspace, name);
    fs.mkdirSync(path.dirname(destination), { recursive: true });
    fs.writeFileSync(destination, content);
    fs.chmodSync(destination, mode);
    return destination;
  };
  const run = (operation, options = {}) => {
    const env = { ...process.env, COPYFILE_DISABLE: "1", GITHUB_WORKSPACE: workspace, GH_AW_AGENT_FOLDERS: folders, GH_AW_AGENT_FILES: files };
    for (const [key, value] of Object.entries(options.env ?? {})) {
      if (value === null) delete env[key];
      else env[key] = value;
    }
    const result = spawnSync("bash", [path.join(root, operation + ".sh")], { env, encoding: "utf8" });
    if (options.failure) assert.notEqual(result.status, 0, result.stdout + result.stderr);
    else assert.equal(result.status, 0, result.stdout + result.stderr);
    return result;
  };
  const link = (name, target) => fs.symlinkSync(target, path.join(workspace, name));
  const read = name => fs.readFileSync(path.join(workspace, name), "utf8");
  const exists = name => fs.existsSync(path.join(workspace, name));
  const archive = entries => {
    fs.mkdirSync(base, { recursive: true });
    const blocks = [];
    for (const [name, type, value = ""] of entries) {
      const header = Buffer.alloc(512);
      const content = Buffer.from(type === "0" || type === "x" || type === "g" || type === "L" || type === "K" ? value : "");
      header.write(name, 0, 100);
      header.write("0000755\0", 100);
      header.write("0000000\0", 108);
      header.write("0000000\0", 116);
      header.write(content.length.toString(8).padStart(11, "0") + "\0", 124);
      header.write("00000000000\0", 136);
      header.fill(32, 148, 156);
      header.write(type, 156);
      if (type === "2" || type === "1") header.write(value, 157, 100);
      header.write("ustar\0", 257);
      header.write("00", 263);
      const checksum = header.reduce((sum, byte) => sum + byte, 0);
      header.write(checksum.toString(8).padStart(6, "0") + "\0 ", 148);
      blocks.push(header, content, Buffer.alloc((512 - (content.length % 512)) % 512));
    }
    blocks.push(Buffer.alloc(1024));
    fs.writeFileSync(path.join(base, "snapshot.tar"), Buffer.concat(blocks));
  };
  return { root, workspace, base, write, run, link, read, exists, archive };
}

test("save: snapshots core folders, hidden resources, and root files; skips absent items", t => {
  const f = fixture(t);
  const names = [".github/skills/SKILL.md", ".agents/agent.md", ".claude/commands/cmd.md", ".claude/workflows/review.js", ".claude/workflows/references/.context", "AGENTS.md", "CLAUDE.md"];
  names.forEach(name => f.write(name));
  f.run("save");
  names.forEach(name => f.write(name, "untrusted"));
  f.run("restore");
  names.forEach(name => assert.equal(f.read(name), "trusted"));
  assert.equal(f.exists(".gemini"), false);
});

test("save: empty or unset lists remove stale snapshot; reruns replace old content", t => {
  const f = fixture(t);
  for (const value of ["", null]) {
    f.archive([["AGENTS.md", "0", "stale"]]);
    f.run("save", { env: { GH_AW_AGENT_FOLDERS: value, GH_AW_AGENT_FILES: value } });
    assert.equal(fs.existsSync(path.join(f.base, "snapshot.tar")), false);
  }
  f.write(".github/old.md");
  f.run("save");
  fs.unlinkSync(path.join(f.workspace, ".github/old.md"));
  f.write(".github/new.md");
  f.run("save");
  f.write(".github/old.md", "untrusted");
  f.run("restore");
  assert.equal(f.exists(".github/old.md"), false);
  assert.equal(f.read(".github/new.md"), "trusted");
});

test("save: root instruction symlinks are materialized with trusted mode", t => {
  const f = fixture(t);
  f.write("INSTRUCTIONS.md", "trusted", 0o600);
  f.link("AGENTS.md", "INSTRUCTIONS.md");
  f.run("save");
  f.write("INSTRUCTIONS.md", "untrusted");
  f.run("restore");
  assert.equal(f.read("AGENTS.md"), "trusted");
  assert.equal(fs.lstatSync(path.join(f.workspace, "AGENTS.md")).isSymbolicLink(), false);
  assert.equal(fs.statSync(path.join(f.workspace, "AGENTS.md")).mode & 0o777, 0o600);
});

test("save: external skill targets are materialized; internal links and modes survive artifacts", t => {
  const f = fixture(t);
  f.write(".agents/scripts/run.sh", "#!/bin/bash\necho ok\n", 0o755);
  f.write(".agents/scripts/.resource", "trusted", 0o640);
  f.link(".agents/scripts/link", "run.sh");
  f.write(".ai/skills/SKILL.md");
  f.write(".ai/skills/.hidden", "trusted", 0o600);
  f.link(".ai/skills/link", "SKILL.md");
  fs.mkdirSync(path.join(f.workspace, ".github"));
  f.link(".github/skills", "../.ai/skills");
  f.run("save");
  fs.chmodSync(path.join(f.base, "snapshot.tar"), 0o644);
  f.write(".ai/skills/SKILL.md", "untrusted");
  f.write(".ai/skills/.hidden", "untrusted");
  fs.rmSync(path.join(f.workspace, ".agents"), { recursive: true });
  f.run("restore");
  assert.equal(f.read(".github/skills/SKILL.md"), "trusted");
  assert.equal(f.read(".github/skills/link"), "trusted");
  assert.equal(fs.lstatSync(path.join(f.workspace, ".github/skills")).isSymbolicLink(), false);
  assert.equal(fs.lstatSync(path.join(f.workspace, ".github/skills/link")).isSymbolicLink(), true);
  for (const [name, mode] of [
    [".agents/scripts/run.sh", 0o755],
    [".agents/scripts/.resource", 0o640],
    [".github/skills/.hidden", 0o600],
  ]) {
    assert.equal(fs.statSync(path.join(f.workspace, name)).mode & 0o777, mode);
  }
  const execution = spawnSync(path.join(f.workspace, ".agents/scripts/link"), [], { encoding: "utf8" });
  assert.equal(execution.status, 0);
  assert.equal(execution.stdout.trim(), "ok");
});

test("save: invalid candidates fail closed in both lists", t => {
  const f = fixture(t);
  for (const item of ["../sentinel", "/etc/passwd", ".", "..", ".github//skills", ".github/../sentinel", "--checkpoint-action=exec=echo"]) {
    for (const key of ["GH_AW_AGENT_FOLDERS", "GH_AW_AGENT_FILES"]) {
      f.run("save", { failure: true, env: { [key]: item } });
      assert.equal(fs.existsSync(path.join(f.base, "snapshot.tar")), false);
    }
  }
});

test("save: rejects symlinks outside workspace and external cycles", t => {
  const f = fixture(t);
  fs.writeFileSync(path.join(f.root, "secret"), "secret");
  f.link("AGENTS.md", "../secret");
  f.run("save", { failure: true });
  fs.unlinkSync(path.join(f.workspace, "AGENTS.md"));
  fs.mkdirSync(path.join(f.root, "external"));
  f.link(".github", "../external");
  f.run("save", { failure: true });
  fs.unlinkSync(path.join(f.workspace, ".github"));
  f.write(".github/skills/SKILL.md");
  f.link(".github/skills/cycle", "../..");
  f.run("save", { failure: true });
  assert.equal(fs.existsSync(path.join(f.base, "snapshot.tar")), false);
});

test("restore: replaces configs and removes injected files, directories, and dangling links", t => {
  const f = fixture(t);
  f.write(".github/trusted.md");
  f.write(".gemini/settings.json");
  f.run("save");
  f.write(".github/evil.md", "untrusted");
  f.write(".claude/config", "untrusted");
  fs.mkdirSync(path.join(f.workspace, "AGENTS.md"));
  f.link(".codex", "missing");
  f.link("CLAUDE.md", "missing");
  f.link(".mcp.json", "missing");
  f.run("restore");
  assert.equal(f.read(".github/trusted.md"), "trusted");
  for (const name of [".github/evil.md", ".claude", "AGENTS.md", ".codex", "CLAUDE.md", ".mcp.json"]) {
    assert.throws(() => fs.lstatSync(path.join(f.workspace, name)), { code: "ENOENT" });
  }
});

test("restore: absent snapshot and empty lists preserve no-op behavior and remove MCP config", t => {
  const f = fixture(t);
  f.write(".claude/config", "untrusted");
  f.write("AGENTS.md", "untrusted");
  f.run("restore");
  assert.equal(f.exists(".claude"), false);
  assert.equal(f.exists("AGENTS.md"), false);
  for (const value of ["", null]) {
    f.write(".claude/config", "untrusted");
    f.write(".mcp.json", "untrusted");
    const env = { GH_AW_AGENT_FOLDERS: value, GH_AW_AGENT_FILES: value };
    f.run("restore", { env });
    assert.equal(f.exists(".claude/config"), true);
    assert.equal(f.exists(".mcp.json"), false);
    f.run("restore", { env });
  }
});

test("restore: invalid candidates are rejected before workspace changes", t => {
  const f = fixture(t);
  f.write("AGENTS.md", "unchanged");
  for (const item of ["../sentinel", "/etc/passwd", ".", "..", ".github//skills"]) {
    for (const key of ["GH_AW_AGENT_FOLDERS", "GH_AW_AGENT_FILES"]) {
      f.run("restore", { failure: true, env: { [key]: item } });
      assert.equal(f.read("AGENTS.md"), "unchanged");
    }
  }
});

test("restore: rejects malicious archive paths, links, special files, and duplicates before extraction", t => {
  const f = fixture(t);
  f.write("AGENTS.md", "unchanged");
  const cases = [
    [["/absolute", "0", "evil"]],
    [["../outside", "0", "evil"]],
    [[".github/../outside", "0", "evil"]],
    [["unselected", "0", "evil"]],
    [[".github/link", "2", "/absolute"]],
    [[".github/link", "2", "../outside"]],
    [
      [".github/link", "2", "target"],
      [".github/link/child", "0", "evil"],
    ],
    [[".github/link", "1", "AGENTS.md"]],
    [[".github/pipe", "6"]],
    [["AGENTS.md", "2", "INSTRUCTIONS.md"]],
    [
      ["AGENTS.md", "0", "one"],
      ["AGENTS.md", "0", "two"],
    ],
    [
      [".github/sub/link", "2", ".."],
      [".github/escape", "2", "sub/link/../../outside"],
    ],
    [
      [".github/a", "2", "b"],
      [".github/b", "2", "a"],
    ],
    [
      ["./.github/a", "0"],
      [".github/a", "0"],
    ],
  ];
  for (const entries of cases) {
    f.archive(entries);
    f.run("restore", { failure: true });
    assert.equal(f.read("AGENTS.md"), "unchanged");
    assert.equal(
      fs.readdirSync(f.base).some(name => name.startsWith(".restore.")),
      false
    );
  }
});

test("restore: accepts dot-prefixed entries and safe parent-relative symlinks", t => {
  const f = fixture(t);
  f.archive([
    [".", "5"],
    ["./.github", "5"],
    ["./.github/resource", "0", "trusted"],
    ["./.github/sub", "5"],
    ["./.github/sub/link", "2", "../resource"],
  ]);
  f.run("restore");
  assert.equal(f.read(".github/sub/link"), "trusted");
  assert.equal(fs.lstatSync(path.join(f.workspace, ".github/sub/link")).isSymbolicLink(), true);
});

function pax(key, value) {
  const body = `${key}=${value}\n`;
  let length = Buffer.byteLength(body) + 2;
  while (Buffer.byteLength(`${length} ${body}`) !== length) length = Buffer.byteLength(`${length} ${body}`);
  return `${length} ${body}`;
}

test("restore: validates GNU long names and PAX effective paths and links", t => {
  const f = fixture(t);
  for (const entries of [
    [
      ["extended", "x", pax("path", "../outside")],
      [".github/file", "0", "evil"],
    ],
    [
      ["extended", "g", pax("path", "/absolute")],
      [".github/file", "0", "evil"],
    ],
    [
      ["extended", "x", pax("linkpath", "../outside")],
      [".github/link", "2", "resource"],
    ],
    [
      ["longname", "L", "../outside\0"],
      [".github/file", "0", "evil"],
    ],
    [
      ["longlink", "K", "../outside\0"],
      [".github/link", "2", "resource"],
    ],
    [
      ["extended", "x", "999 path=bad\n"],
      [".github/file", "0", "evil"],
    ],
  ]) {
    f.archive(entries);
    f.run("restore", { failure: true });
  }
  const longName = ".github/" + "a".repeat(120);
  f.archive([
    ["extended", "x", pax("path", longName)],
    [".github/file", "0", "trusted"],
  ]);
  f.run("restore");
  assert.equal(f.read(longName), "trusted");
});

test("restore: rejects corrupt and truncated archives", t => {
  const f = fixture(t);
  fs.mkdirSync(f.base);
  fs.writeFileSync(path.join(f.base, "snapshot.tar"), "not tar");
  f.run("restore", { failure: true });
  f.archive([["AGENTS.md", "0", "trusted"]]);
  const archive = fs.readFileSync(path.join(f.base, "snapshot.tar"));
  archive[0] = 42;
  fs.writeFileSync(path.join(f.base, "snapshot.tar"), archive);
  f.run("restore", { failure: true });
  f.archive([["AGENTS.md", "0", "trusted"]]);
  fs.truncateSync(path.join(f.base, "snapshot.tar"), 512);
  f.run("restore", { failure: true });
});
