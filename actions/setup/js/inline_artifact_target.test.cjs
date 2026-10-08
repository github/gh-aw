import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const { inlineArtifactTarget } = await import("./inline_artifact_target.cjs");
const { writeInlineSubAgents } = await import("./extract_inline_sub_agents.cjs");
const { writeInlineSkills } = await import("./extract_inline_skills.cjs");
let dir;

beforeEach(() => {
  dir = fs.mkdtempSync(path.join(os.tmpdir(), "inline-target-"));
  vi.stubGlobal("core", { info: vi.fn(), warning: vi.fn() });
});
afterEach(() => {
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
  fs.rmSync(dir, { recursive: true, force: true });
});

describe("compiler-owned inline artifact targets", () => {
  it("stages pi agents and skills where activation uploads them", () => {
    writeInlineSubAgents("Main.\n## agent: `reader`\n---\ndescription: Reads files\n---\nRead.", dir, dir, "pi");
    writeInlineSkills("Main.\n## skill: `report`\n---\ndescription: Reports facts\n---\nReport.", dir, dir, "pi");
    expect(fs.existsSync(path.join(dir, ".pi/agents/reader.md"))).toBe(true);
    expect(fs.existsSync(path.join(dir, ".pi/skills/report/SKILL.md"))).toBe(true);
  });

  it("honors compiler paths even for engines unknown to JavaScript", () => {
    vi.stubEnv("GH_AW_SUB_AGENT_DIR", ".custom/agents");
    vi.stubEnv("GH_AW_SUB_AGENT_EXT", ".md");
    vi.stubEnv("GH_AW_SKILL_DIR", ".custom/skills");
    vi.stubEnv("GH_AW_SKILL_EXT", "/SKILL.md");
    writeInlineSubAgents("## agent: `reader`\nRead.", dir, dir, "custom");
    writeInlineSkills("## skill: `report`\nReport.", dir, dir, "custom");
    expect(fs.existsSync(path.join(dir, ".custom/agents/reader.md"))).toBe(true);
    expect(fs.existsSync(path.join(dir, ".custom/skills/report/SKILL.md"))).toBe(true);
  });

  it.each(["../outside", "/absolute", ".pi/../outside", ".", ".pi//agents", ""])("rejects unsafe target %s", target => {
    vi.stubEnv("GH_AW_SUB_AGENT_DIR", target);
    vi.stubEnv("GH_AW_SUB_AGENT_EXT", ".md");
    expect(() => inlineArtifactTarget("SUB_AGENT", { dir: ".github/agents", ext: ".agent.md" })).toThrow();
  });

  it("requires a valid extension when a target is supplied", () => {
    vi.stubEnv("GH_AW_SUB_AGENT_DIR", ".pi/agents");
    expect(() => inlineArtifactTarget("SUB_AGENT", { dir: ".github/agents", ext: ".agent.md" })).toThrow();
  });
});
