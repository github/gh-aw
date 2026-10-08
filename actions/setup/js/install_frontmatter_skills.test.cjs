import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import { createRequire } from "module";

const require = createRequire(import.meta.url);
const script = require("./install_frontmatter_skills.cjs");

describe("install_frontmatter_skills", () => {
  let originalEnv;
  let originalCore;
  let originalExec;
  let tempRoot;

  beforeEach(() => {
    originalEnv = {
      GH_AW_SKILL_DIR: process.env.GH_AW_SKILL_DIR,
      GH_AW_INFO_ENGINE_ID: process.env.GH_AW_INFO_ENGINE_ID,
      GH_AW_GH_SKILL_AGENT_NAME: process.env.GH_AW_GH_SKILL_AGENT_NAME,
      GH_AW_FRONTMATTER_SKILLS: process.env.GH_AW_FRONTMATTER_SKILLS,
    };
    originalCore = global.core;
    originalExec = global.exec;
    tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-frontmatter-skills-"));

    global.core = {
      info: vi.fn(),
      warning: vi.fn(),
      summary: {
        addRaw: vi.fn().mockReturnThis(),
        write: vi.fn().mockResolvedValue(undefined),
      },
    };
    global.exec = {
      exec: vi.fn().mockResolvedValue(0),
    };
  });

  afterEach(() => {
    if (originalEnv.GH_AW_SKILL_DIR === undefined) {
      delete process.env.GH_AW_SKILL_DIR;
    } else {
      process.env.GH_AW_SKILL_DIR = originalEnv.GH_AW_SKILL_DIR;
    }
    if (originalEnv.GH_AW_INFO_ENGINE_ID === undefined) {
      delete process.env.GH_AW_INFO_ENGINE_ID;
    } else {
      process.env.GH_AW_INFO_ENGINE_ID = originalEnv.GH_AW_INFO_ENGINE_ID;
    }
    if (originalEnv.GH_AW_GH_SKILL_AGENT_NAME === undefined) {
      delete process.env.GH_AW_GH_SKILL_AGENT_NAME;
    } else {
      process.env.GH_AW_GH_SKILL_AGENT_NAME = originalEnv.GH_AW_GH_SKILL_AGENT_NAME;
    }
    if (originalEnv.GH_AW_FRONTMATTER_SKILLS === undefined) {
      delete process.env.GH_AW_FRONTMATTER_SKILLS;
    } else {
      process.env.GH_AW_FRONTMATTER_SKILLS = originalEnv.GH_AW_FRONTMATTER_SKILLS;
    }
    global.core = originalCore;
    global.exec = originalExec;
    fs.rmSync(tempRoot, { recursive: true, force: true });
    fs.rmSync("/tmp/gh-aw/.claude", { recursive: true, force: true });
    fs.rmSync("/tmp/gh-aw/skill_install_failures.json", { force: true });
  });

  it("splits repo-level and path-level skill specs into gh skill install arguments", () => {
    expect(script.buildSkillInstallCommand("githubnext/skills@abc123", "/tmp/gh-aw/.claude/skills", "claude-code").args).toEqual([
      "skill",
      "install",
      "githubnext/skills",
      "--all",
      "--pin",
      "abc123",
      "--agent",
      "claude-code",
      "--dir",
      "/tmp/gh-aw/.claude/skills",
      "--force",
    ]);
    expect(script.buildSkillInstallCommand("githubnext/skills/review/security@abc123", "/tmp/gh-aw/.claude/skills").args).toEqual([
      "skill",
      "install",
      "githubnext/skills",
      "review/security",
      "--pin",
      "abc123",
      "--dir",
      "/tmp/gh-aw/.claude/skills",
      "--force",
    ]);
    expect(script.buildSkillInstallCommand("githubnext/skills/review/security@abc123", "/tmp/gh-aw/.claude/skills", "claude-code").args).toEqual([
      "skill",
      "install",
      "githubnext/skills",
      "review/security",
      "--pin",
      "abc123",
      "--agent",
      "claude-code",
      "--dir",
      "/tmp/gh-aw/.claude/skills",
      "--force",
    ]);
  });

  it("treats specs without @ as local path refs (installs with --from-local)", () => {
    // Any spec without "@" and without "${{" is a local path reference.
    // Remote refs must always be pinned (owner/repo@sha).
    expect(script.buildSkillInstallCommand("skills/review/security", "/tmp/gh-aw/.claude/skills").args).toEqual(["skill", "install", "skills/review/security", "security", "--from-local", "--dir", "/tmp/gh-aw/.claude/skills", "--force"]);
  });

  it("installs local path reference using --from-local", () => {
    expect(script.buildSkillInstallCommand("skills/rig", "/tmp/gh-aw/.claude/skills").args).toEqual(["skill", "install", "skills/rig", "rig", "--from-local", "--dir", "/tmp/gh-aw/.claude/skills", "--force"]);
    expect(script.buildSkillInstallCommand(".github/skills/my-skill", "/tmp/gh-aw/.claude/skills", "claude-code").args).toEqual([
      "skill",
      "install",
      ".github/skills/my-skill",
      "my-skill",
      "--from-local",
      "--agent",
      "claude-code",
      "--dir",
      "/tmp/gh-aw/.claude/skills",
      "--force",
    ]);
    expect(script.buildSkillInstallCommand("./skills/my-skill", "/tmp/gh-aw/.claude/skills").args).toEqual(["skill", "install", "./skills/my-skill", "my-skill", "--from-local", "--dir", "/tmp/gh-aw/.claude/skills", "--force"]);
  });

  it("isLocalSkillRef returns true for local paths and false for remote or expression specs", () => {
    expect(script.isLocalSkillRef("skills/rig")).toBe(true);
    expect(script.isLocalSkillRef(".github/skills/my-skill")).toBe(true);
    expect(script.isLocalSkillRef("./skills/my-skill")).toBe(true);
    expect(script.isLocalSkillRef("my-skill")).toBe(true);
    expect(script.isLocalSkillRef("owner/repo@abc123")).toBe(false);
    expect(script.isLocalSkillRef("${{ inputs.skill_ref }}")).toBe(false);
    expect(script.isLocalSkillRef("")).toBe(false);
  });

  it("reads skill specs from the env var and installs them at runtime", async () => {
    process.env.GH_AW_SKILL_DIR = ".claude/skills";
    process.env.GH_AW_INFO_ENGINE_ID = "claude";
    process.env.GH_AW_GH_SKILL_AGENT_NAME = "claude-code";
    process.env.GH_AW_FRONTMATTER_SKILLS = ["githubnext/skills@abc123", "githubnext/skills/review/security@def456", "${{ inputs.skill_ref }}"].join("\n");
    fs.mkdirSync("/tmp/gh-aw/.claude/skills/example", { recursive: true });
    fs.writeFileSync("/tmp/gh-aw/.claude/skills/example/SKILL.md", "# test\n", "utf8");

    await script.main();

    expect(global.exec.exec).toHaveBeenNthCalledWith(1, "gh", ["skill", "install", "githubnext/skills", "--all", "--pin", "abc123", "--agent", "claude-code", "--dir", "/tmp/gh-aw/.claude/skills", "--force"]);
    expect(global.exec.exec).toHaveBeenNthCalledWith(2, "gh", ["skill", "install", "githubnext/skills", "review/security", "--pin", "def456", "--agent", "claude-code", "--dir", "/tmp/gh-aw/.claude/skills", "--force"]);
    expect(global.exec.exec).toHaveBeenNthCalledWith(3, "gh", ["skill", "install", "${{ inputs.skill_ref }}", "--agent", "claude-code", "--dir", "/tmp/gh-aw/.claude/skills", "--force"]);
    expect(global.core.summary.addRaw).toHaveBeenCalledWith(expect.stringMatching(/^<details>\n<summary>Skill install details<\/summary>/));
    expect(global.core.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining('["githubnext/skills@abc123","githubnext/skills/review/security@def456","${{ inputs.skill_ref }}"]'));
  });

  it("omits --agent when no gh skill agent name is provided", async () => {
    process.env.GH_AW_SKILL_DIR = ".claude/skills";
    process.env.GH_AW_FRONTMATTER_SKILLS = "githubnext/skills@abc123";

    await script.main();

    expect(global.exec.exec).toHaveBeenCalledWith("gh", ["skill", "install", "githubnext/skills", "--all", "--pin", "abc123", "--dir", "/tmp/gh-aw/.claude/skills", "--force"]);
  });

  it("records failures without throwing when skill install fails", async () => {
    process.env.GH_AW_SKILL_DIR = ".claude/skills";
    process.env.GH_AW_FRONTMATTER_SKILLS = "bad/repo@abc123";
    global.exec.exec = vi.fn().mockRejectedValue(new Error("exit code 1\nHTTP 404"));

    await expect(script.main()).resolves.toBeUndefined();

    const failures = JSON.parse(fs.readFileSync("/tmp/gh-aw/skill_install_failures.json", "utf8"));
    expect(failures).toEqual([{ skill: "bad/repo@abc123", error: "exit code 1 HTTP 404" }]);
    expect(global.core.warning).toHaveBeenCalledWith(expect.stringContaining("Failed to install skill 'bad/repo@abc123'"));
    expect(global.core.summary.addRaw).toHaveBeenCalledWith(expect.stringMatching(/^<details open>\n<summary>Skill install details<\/summary>/));
  });

  it("installs local path references using --from-local at runtime", async () => {
    process.env.GH_AW_SKILL_DIR = ".claude/skills";
    process.env.GH_AW_GH_SKILL_AGENT_NAME = "claude-code";
    process.env.GH_AW_FRONTMATTER_SKILLS = "skills/rig";

    await script.main();

    expect(global.exec.exec).toHaveBeenCalledWith("gh", ["skill", "install", "skills/rig", "rig", "--from-local", "--agent", "claude-code", "--dir", "/tmp/gh-aw/.claude/skills", "--force"]);
  });
});
