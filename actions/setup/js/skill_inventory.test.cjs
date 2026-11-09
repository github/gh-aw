import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createRequire } from "node:module";

const { inventoryDirectory, detectSkillConflicts, main } = createRequire(import.meta.url)("../../../scripts/markdown_metadata_test_adapter.cjs").withCompilerFixtures(createRequire(import.meta.url)("./skill_inventory.cjs"));

describe("skill inventory", () => {
  let root;
  let oldWorkspace;
  let oldSkillDir;
  let oldCore;
  beforeEach(() => {
    root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-skills-"));
    oldWorkspace = process.env.GITHUB_WORKSPACE;
    oldSkillDir = process.env.GH_AW_SKILL_DIR;
    oldCore = global.core;
    global.core = { warning: vi.fn(), info: vi.fn() };
  });
  afterEach(() => {
    if (oldWorkspace === undefined) delete process.env.GITHUB_WORKSPACE;
    else process.env.GITHUB_WORKSPACE = oldWorkspace;
    if (oldSkillDir === undefined) delete process.env.GH_AW_SKILL_DIR;
    else process.env.GH_AW_SKILL_DIR = oldSkillDir;
    global.core = oldCore;
    fs.rmSync(root, { recursive: true, force: true });
  });

  it("records stable names, sources, scopes, digests and declared purposes without content", () => {
    const dir = path.join(root, "skills");
    fs.mkdirSync(path.join(dir, "review"), { recursive: true });
    fs.writeFileSync(path.join(dir, "review", "SKILL.md"), "---\ndescription: Review changes\n---\nSECRET_CONTENT\n");
    const skills = inventoryDirectory(dir, "repository", "skills");
    expect(skills).toEqual([{ name: "review", source: "skills/review", scope: "repository", digest: expect.stringMatching(/^sha256:[0-9a-f]{64}$/), purpose: "Review changes" }]);
    expect(JSON.stringify(skills)).not.toContain("SECRET_CONTENT");
    expect(inventoryDirectory(dir, "repository", "skills")).toEqual(skills);
  });

  it("parses YAML descriptions and ignores non-string declarations", () => {
    const dir = path.join(root, "skills");
    fs.mkdirSync(path.join(dir, "quoted"), { recursive: true });
    fs.mkdirSync(path.join(dir, "folded"), { recursive: true });
    fs.mkdirSync(path.join(dir, "numeric"), { recursive: true });
    fs.writeFileSync(path.join(dir, "quoted", "SKILL.md"), '---\ndescription: "Review user\'s code"\n---\n');
    fs.writeFileSync(path.join(dir, "folded", "SKILL.md"), "---\ndescription: >\n  Review user's code\n  across files\n---\n");
    fs.writeFileSync(path.join(dir, "numeric", "SKILL.md"), "---\ndescription: 42\n---\n");
    expect(inventoryDirectory(dir, "repository", "skills")).toEqual([
      { name: "folded", source: "skills/folded", scope: "repository", digest: expect.any(String), purpose: "Review user's code across files" },
      { name: "numeric", source: "skills/numeric", scope: "repository", digest: expect.any(String) },
      { name: "quoted", source: "skills/quoted", scope: "repository", digest: expect.any(String), purpose: "Review user's code" },
    ]);
  });

  it("reports exact-name and cross-scope collisions without treating similar names as conflicts", () => {
    const entries = [
      { name: "review", source: "skills/review", scope: "repository", digest: "a" },
      { name: "review", source: "staged/.claude/skills/review", scope: "engine", digest: "b" },
      { name: "review-security", source: "skills/review-security", scope: "repository", digest: "c" },
    ];
    expect(detectSkillConflicts(entries)).toEqual([{ name: "review", kind: "scope_collision", sources: ["skills/review", "staged/.claude/skills/review"] }]);
    expect(detectSkillConflicts([entries[0], { ...entries[0], source: "other/review" }])).toEqual([]);
  });

  it("writes activation evidence and warns for conflicting scopes", () => {
    process.env.GITHUB_WORKSPACE = root;
    process.env.GH_AW_SKILL_DIR = ".claude/skills";
    const repo = path.join(root, ".github/skills/review");
    const staged = path.join(root, ".claude/skills/review");
    const inventoryPath = path.join(root, "evidence/skill_inventory.json");
    fs.mkdirSync(repo, { recursive: true });
    fs.mkdirSync(staged, { recursive: true });
    fs.writeFileSync(path.join(repo, "SKILL.md"), "# repo");
    fs.writeFileSync(path.join(staged, "SKILL.md"), "# staged");
    try {
      main({ stagedRoot: root, inventoryPath });
      const evidence = JSON.parse(fs.readFileSync(inventoryPath, "utf8"));
      expect(evidence.skills).toHaveLength(2);
      expect(evidence.conflicts).toMatchObject([{ name: "review", kind: "scope_collision" }]);
      expect(global.core.warning).toHaveBeenCalledTimes(1);
    } finally {
      fs.rmSync(staged, { recursive: true, force: true });
    }
  });

  it("reports overwritten installations with the same name and different digests", () => {
    process.env.GITHUB_WORKSPACE = root;
    process.env.GH_AW_SKILL_DIR = ".claude/skills";
    const historyPath = path.join(root, "skill_install_history.json");
    const inventoryPath = path.join(root, "skill_inventory.json");
    fs.writeFileSync(
      historyPath,
      JSON.stringify([
        { name: "review", source: "frontmatter-1", digest: "sha256:first" },
        { name: "review", source: "frontmatter-2", digest: "sha256:second" },
      ])
    );
    main({ stagedRoot: root, inventoryPath, historyPath });
    const evidence = JSON.parse(fs.readFileSync(inventoryPath, "utf8"));
    expect(evidence.conflicts).toContainEqual({ name: "review", kind: "name_collision", sources: ["frontmatter-1", "frontmatter-2"] });
  });

  it("merges overwrite-history sources with current repository collision sources", () => {
    process.env.GITHUB_WORKSPACE = root;
    process.env.GH_AW_SKILL_DIR = ".claude/skills";
    const inventoryPath = path.join(root, "skill_inventory.json");
    const historyPath = path.join(root, "skill_install_history.json");
    for (const [directory, content] of [
      ["skills", "first"],
      [".github/skills", "second"],
    ]) {
      const skill = path.join(root, directory, "review");
      fs.mkdirSync(skill, { recursive: true });
      fs.writeFileSync(path.join(skill, "SKILL.md"), content);
    }
    fs.writeFileSync(
      historyPath,
      JSON.stringify([
        { name: "review", source: "frontmatter-1", digest: "sha256:first" },
        { name: "review", source: "frontmatter-2", digest: "sha256:second" },
      ])
    );
    main({ stagedRoot: root, inventoryPath, historyPath });
    const evidence = JSON.parse(fs.readFileSync(inventoryPath, "utf8"));
    expect(evidence.conflicts).toEqual([
      {
        name: "review",
        kind: "name_collision",
        sources: [".github/skills/review", "frontmatter-1", "frontmatter-2", "skills/review"],
      },
    ]);
  });
});
