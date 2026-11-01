import { afterEach, beforeEach, describe, expect, it } from "vitest";

const fs = require("fs");
const os = require("os");
const path = require("path");
const substitutePlaceholders = require("./substitute_placeholders.cjs");
const { processRuntimeImports } = require("./runtime_import.cjs");
const { writeInlineSubAgents, getEngineSubAgentTarget } = require("./extract_inline_sub_agents.cjs");
const { writeInlineSkills, getEngineSkillTarget } = require("./extract_inline_skills.cjs");

describe("substitutePlaceholders", () => {
  /** @type {string} */
  let tempDir;
  /** @type {string} */
  let testFile;

  beforeEach(() => {
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "substitute-test-"));
    testFile = path.join(tempDir, "test.txt");
  });

  afterEach(() => {
    if (fs.existsSync(tempDir)) fs.rmSync(tempDir, { recursive: true, force: true });
  });

  it("should substitute a single placeholder", async () => {
    fs.writeFileSync(testFile, "Hello __NAME__!", "utf8");
    await substitutePlaceholders({ file: testFile, substitutions: { NAME: "World" } });
    expect(fs.readFileSync(testFile, "utf8")).toBe("Hello World!");
  });

  it("should substitute multiple placeholders", async () => {
    fs.writeFileSync(testFile, "Repository: __REPO__\nActor: __ACTOR__\nBranch: __BRANCH__", "utf8");
    await substitutePlaceholders({
      file: testFile,
      substitutions: { REPO: "test/repo", ACTOR: "testuser", BRANCH: "main" },
    });
    expect(fs.readFileSync(testFile, "utf8")).toBe("Repository: test/repo\nActor: testuser\nBranch: main");
  });

  it("should handle special characters safely", async () => {
    fs.writeFileSync(testFile, "Command: __CMD__", "utf8");
    await substitutePlaceholders({
      file: testFile,
      substitutions: { CMD: "$(malicious) `backdoor` ${VAR} | pipe" },
    });
    expect(fs.readFileSync(testFile, "utf8")).toBe("Command: $(malicious) `backdoor` ${VAR} | pipe");
  });

  it("preserves single-quoted JSON and shell metacharacters as data", async () => {
    const canary = path.join(tempDir, "must-not-exist");
    const value = JSON.stringify({
      description: `user's cache'; touch "${canary}"; #`,
      command: `$(touch "${canary}") \`touch "${canary}"\``,
      multiline: "single ' and double \" quotes\n${PATH}; | & > * \\ backslash",
    });
    fs.writeFileSync(testFile, "__GH_AW_STATIC__", "utf8");

    await substitutePlaceholders({ file: testFile, substitutions: { GH_AW_STATIC: value } });

    expect(fs.readFileSync(testFile, "utf8")).toBe(value);
    expect(fs.existsSync(canary)).toBe(false);
  });

  it("should handle placeholders appearing multiple times", async () => {
    fs.writeFileSync(testFile, "__NAME__ is great. I love __NAME__!", "utf8");
    await substitutePlaceholders({ file: testFile, substitutions: { NAME: "Testing" } });
    expect(fs.readFileSync(testFile, "utf8")).toBe("Testing is great. I love Testing!");
  });

  it("should leave unmatched placeholders unchanged", async () => {
    fs.writeFileSync(testFile, "__FOO__ and __BAR__", "utf8");
    await substitutePlaceholders({ file: testFile, substitutions: { FOO: "foo" } });
    expect(fs.readFileSync(testFile, "utf8")).toBe("foo and __BAR__");
  });

  it("should handle empty values", async () => {
    fs.writeFileSync(testFile, "Value: __VAL__", "utf8");
    await substitutePlaceholders({ file: testFile, substitutions: { VAL: "" } });
    expect(fs.readFileSync(testFile, "utf8")).toBe("Value: ");
  });

  it("should throw error if file parameter is missing", async () => {
    // @ts-expect-error - testing missing file param
    await expect(substitutePlaceholders({ substitutions: { NAME: "test" } })).rejects.toThrow("ERR_VALIDATION: file parameter is required");
  });

  it("should throw error if substitutions parameter is missing", async () => {
    // @ts-expect-error - testing missing substitutions param
    await expect(substitutePlaceholders({ file: testFile })).rejects.toThrow("ERR_VALIDATION: substitutions parameter must be an object");
  });

  it("should throw error if file does not exist", async () => {
    await expect(substitutePlaceholders({ file: "/nonexistent/file.txt", substitutions: { NAME: "test" } })).rejects.toThrow("ERR_SYSTEM: Failed to read file");
  });

  it("should handle undefined values as empty strings", async () => {
    fs.writeFileSync(testFile, "Value: __VAL__", "utf8");
    await substitutePlaceholders({ file: testFile, substitutions: { VAL: undefined } });
    expect(fs.readFileSync(testFile, "utf8")).toBe("Value: ");
  });

  it("should handle null values as empty strings", async () => {
    fs.writeFileSync(testFile, "Value: __VAL__", "utf8");
    await substitutePlaceholders({ file: testFile, substitutions: { VAL: null } });
    expect(fs.readFileSync(testFile, "utf8")).toBe("Value: ");
  });

  it("should handle mixed undefined and defined values", async () => {
    fs.writeFileSync(testFile, "Repo: __REPO__\nComment: __COMMENT__\nIssue: __ISSUE__", "utf8");
    await substitutePlaceholders({
      file: testFile,
      substitutions: { REPO: "test/repo", COMMENT: undefined, ISSUE: null },
    });
    expect(fs.readFileSync(testFile, "utf8")).toBe("Repo: test/repo\nComment: \nIssue: ");
  });

  it("preserves legacy prompt substitution when split files are absent", async () => {
    const promptPath = path.join(tempDir, "prompt.txt");
    fs.writeFileSync(promptPath, "__ACTOR__");
    await substitutePlaceholders({ file: promptPath, substitutions: { ACTOR: "developer" } });
    expect(fs.readFileSync(promptPath, "utf8")).toBe("developer");
  });

  it.each(["123", "", "value with ${{ literal }} and $(literal)\nnext line"])("resolves late job outputs after activation without changing split prompts (%j)", async value => {
    const promptPath = path.join(tempDir, "prompt.txt");
    const systemPath = path.join(tempDir, "system.txt");
    const userPath = path.join(tempDir, "user.txt");
    const system = "System instructions\n";
    const user = "Release: __GH_AW_NEEDS_RELEASE_OUTPUTS_RELEASE_ID__";
    fs.writeFileSync(systemPath, system);
    fs.writeFileSync(userPath, user);
    fs.writeFileSync(promptPath, system + user);

    await substitutePlaceholders({
      file: promptPath,
      substitutions: { GH_AW_NEEDS_RELEASE_OUTPUTS_RELEASE_ID: "__GH_AW_DEFERRED_1234__" },
      deferredBindings: { GH_AW_NEEDS_RELEASE_OUTPUTS_RELEASE_ID: "__GH_AW_DEFERRED_1234__" },
    });
    expect(fs.readFileSync(promptPath, "utf8")).toBe(system + "Release: __GH_AW_DEFERRED_1234__");

    await substitutePlaceholders({ file: promptPath, substitutions: { GH_AW_DEFERRED_1234: value } });
    expect(fs.readFileSync(systemPath, "utf8")).toBe(system);
    expect(fs.readFileSync(userPath, "utf8")).toBe("Release: " + value);
    expect(fs.readFileSync(promptPath, "utf8")).toBe(system + "Release: " + value);
  });

  it("substitutes split files for a custom combined prompt filename", async () => {
    const systemPath = path.join(tempDir, "system.txt");
    const userPath = path.join(tempDir, "user.txt");
    const system = "System: __ACTOR__\n";
    const user = "User: __ACTOR__";
    fs.writeFileSync(systemPath, system);
    fs.writeFileSync(userPath, user);
    fs.writeFileSync(testFile, system + user);

    await substitutePlaceholders({ file: testFile, substitutions: { ACTOR: "developer" } });

    const finalSystem = fs.readFileSync(systemPath, "utf8");
    const finalUser = fs.readFileSync(userPath, "utf8");
    expect(finalSystem).toBe("System: developer\n");
    expect(finalUser).toBe("User: developer");
    expect(fs.readFileSync(testFile, "utf8")).toBe(finalSystem + finalUser);
  });

  it("rejects split files that do not match the combined prompt after substitution", async () => {
    const systemPath = path.join(tempDir, "system.txt");
    const userPath = path.join(tempDir, "user.txt");
    fs.writeFileSync(systemPath, "System __ACTOR__");
    fs.writeFileSync(userPath, "User");
    fs.writeFileSync(testFile, "Different __ACTOR__");

    await expect(substitutePlaceholders({ file: testFile, substitutions: { ACTOR: "developer" } })).rejects.toThrow("ERR_VALIDATION: Split prompt files do not match the combined prompt after substitution");
    expect(fs.readFileSync(systemPath, "utf8")).toBe("System __ACTOR__");
    expect(fs.readFileSync(userPath, "utf8")).toBe("User");
    expect(fs.readFileSync(testFile, "utf8")).toBe("Different __ACTOR__");
  });

  it.each(["copilot", "claude", "codex", "gemini", "pi"])("resolves runtime-import outputs in extracted staged agents and skills (%s)", async engine => {
    const artifactRoot = path.join(tempDir, "artifact");
    const promptDir = path.join(artifactRoot, "aw-prompts");
    fs.mkdirSync(promptDir, { recursive: true });
    const promptPath = path.join(promptDir, "prompt.txt");
    const envKey = "GH_AW_NEEDS_RELEASE_OUTPUTS_RELEASE_ID";
    const previous = process.env[envKey];
    process.env[envKey] = "__GH_AW_DEFERRED_RELEASE__";
    fs.mkdirSync(path.join(tempDir, ".github", "workflows"), { recursive: true });
    fs.writeFileSync(path.join(tempDir, ".github", "workflows", "release.md"), "Release: ${{ needs.release.outputs.release_id }}");
    const markdown = [
      "# Main",
      "## agent: `reader`",
      "---",
      "description: Read the release",
      "---",
      "{{#runtime-import release.md}}",
      "## end agent: `reader`",
      "## skill: `release`",
      "---",
      "description: Read the release",
      "---",
      "{{#runtime-import release.md}}",
      "## end skill: `release`",
    ].join("\n");
    let imported;
    try {
      imported = await processRuntimeImports(markdown, tempDir);
    } finally {
      if (previous === undefined) delete process.env[envKey];
      else process.env[envKey] = previous;
    }
    let main = writeInlineSubAgents(imported, tempDir, artifactRoot, engine);
    main = writeInlineSkills(main, tempDir, artifactRoot, engine);
    expect(main).not.toContain("__GH_AW_DEFERRED_RELEASE__");
    const { dir: agentDir, ext: agentExt } = getEngineSubAgentTarget(engine);
    const { dir: skillDir, ext: skillExt } = getEngineSkillTarget(engine);
    const agentPath = path.join(artifactRoot, agentDir, "reader" + agentExt);
    const skillPath = path.join(artifactRoot, skillDir, "release" + skillExt);
    const stagedAgent = fs.readFileSync(agentPath, "utf8");
    const stagedSkill = fs.readFileSync(skillPath, "utf8");
    expect(stagedAgent).toContain("__GH_AW_DEFERRED_RELEASE__");
    expect(stagedSkill).toContain("__GH_AW_DEFERRED_RELEASE__");
    fs.writeFileSync(promptPath, main);
    fs.writeFileSync(path.join(promptDir, "system.txt"), main);
    fs.writeFileSync(path.join(promptDir, "user.txt"), "");
    const unrelated = path.join(artifactRoot, "unrelated.txt");
    fs.writeFileSync(unrelated, "__GH_AW_DEFERRED_RELEASE__");

    for (const value of ["", "line one\n${{ literal }} $(literal) $& __GH_AW_DEFERRED_OTHER__"]) {
      // Each iteration recreates the activation inputs, including their provenance.
      fs.rmSync(path.join(promptDir, "prompt-literal-values.json"), { force: true });
      fs.writeFileSync(agentPath, stagedAgent);
      fs.writeFileSync(skillPath, stagedSkill);
      await substitutePlaceholders({
        file: promptPath,
        substitutions: { GH_AW_DEFERRED_RELEASE: value, GH_AW_DEFERRED_OTHER: "must not replace literal value" },
        inlineArtifacts: [
          { directory: agentDir, extension: agentExt },
          { directory: skillDir, extension: skillExt },
        ],
      });
      expect(fs.readFileSync(agentPath, "utf8")).toBe(stagedAgent.replace("__GH_AW_DEFERRED_RELEASE__", () => value));
      expect(fs.readFileSync(skillPath, "utf8")).toBe(stagedSkill.replace("__GH_AW_DEFERRED_RELEASE__", () => value));
      expect(fs.readFileSync(promptPath, "utf8")).toBe(main);
      expect(fs.readFileSync(unrelated, "utf8")).toBe("__GH_AW_DEFERRED_RELEASE__");
    }
  });

  it("rejects traversal and symlinked inline artifact directories without writing prompts", async () => {
    const artifactRoot = path.join(tempDir, "artifact");
    const promptPath = path.join(artifactRoot, "aw-prompts", "prompt.txt");
    fs.mkdirSync(path.dirname(promptPath), { recursive: true });
    fs.writeFileSync(promptPath, "__NAME__");
    fs.symlinkSync(tempDir, path.join(artifactRoot, ".github"));
    for (const directory of ["../outside", "/outside", ".github/agents"]) {
      await expect(
        substitutePlaceholders({
          file: promptPath,
          substitutions: { NAME: "resolved" },
          inlineArtifacts: [{ directory, extension: ".agent.md" }],
        })
      ).rejects.toThrow("ERR_VALIDATION");
      expect(fs.readFileSync(promptPath, "utf8")).toBe("__NAME__");
    }
  });

  it("does not follow symlinked staged instruction files or skill folders", async () => {
    const artifactRoot = path.join(tempDir, "artifact");
    const promptPath = path.join(artifactRoot, "aw-prompts", "prompt.txt");
    const agentDir = path.join(artifactRoot, ".github", "agents");
    const skillDir = path.join(artifactRoot, ".github", "skills");
    for (const dir of [path.dirname(promptPath), agentDir, skillDir]) fs.mkdirSync(dir, { recursive: true });
    fs.writeFileSync(promptPath, "__NAME__");
    fs.writeFileSync(testFile, "__NAME__");
    fs.symlinkSync(testFile, path.join(agentDir, "reader.agent.md"));
    fs.symlinkSync(tempDir, path.join(skillDir, "release"));
    await substitutePlaceholders({
      file: promptPath,
      substitutions: { NAME: "resolved" },
      inlineArtifacts: [
        { directory: ".github/agents", extension: ".agent.md" },
        { directory: ".github/skills", extension: "/SKILL.md" },
      ],
    });
    expect(fs.readFileSync(promptPath, "utf8")).toBe("resolved");
    expect(fs.readFileSync(testFile, "utf8")).toBe("__NAME__");
  });
});
