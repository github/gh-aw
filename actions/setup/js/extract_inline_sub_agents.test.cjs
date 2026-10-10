// @ts-check
/// <reference types="@actions/github-script" />

import { describe, it, expect, beforeEach, afterEach } from "vitest";
const fs = require("fs");
const path = require("path");

// Provide a minimal core mock so the module loads correctly.
global.core = {
  info: () => {},
  warning: () => {},
  error: () => {},
  setFailed: () => {},
};

const { extractInlineSubAgents, writeInlineSubAgents, preserveSubAgentFrontmatter, closeUnterminatedSubAgentMarkers, loadCopilotSDKCustomAgents } = require("./extract_inline_sub_agents.cjs");

// Helper: returns a ## agent: `name` start marker line.
const agentMarker = name => `## agent: \`${name}\``;

describe("loadCopilotSDKCustomAgents", () => {
  it("loads runtime files, preserving YAML descriptions and workflow precedence", () => {
    const base = fs.mkdtempSync(path.join(process.cwd(), ".sdk-agents-test-"));
    try {
      const workspace = path.join(base, "workspace");
      const workflow = path.join(base, "workflow");
      fs.mkdirSync(path.join(workspace, ".github/agents"), { recursive: true });
      fs.mkdirSync(path.join(workflow, ".github/agents"), { recursive: true });
      fs.writeFileSync(path.join(workspace, ".github/agents/helper.agent.md"), "---\nname: helper\n---\nRepository version.");
      fs.writeFileSync(path.join(workflow, ".github/agents/helper.agent.md"), "---\nname: helper\ndescription: |\n  First line\n  Second line\ntools: []\nmodel: small\n---\nWorkflow version.");
      fs.writeFileSync(path.join(workspace, ".github/agents/plain.agent.md"), "Prompt without frontmatter.");
      fs.writeFileSync(path.join(workspace, ".github/agents/ignored.md"), "Ignored file.");
      fs.writeFileSync(path.join(workspace, ".github/agents/broken.agent.md"), "---\ntools: {invalid: true}\n---\nInvalid tools.");
      const warnings = [];
      expect(loadCopilotSDKCustomAgents(workspace, workflow, message => warnings.push(message))).toEqual([
        { name: "helper", description: "First line\nSecond line\n", tools: [], model: "small", prompt: "Workflow version." },
        { name: "plain", prompt: "Prompt without frontmatter." },
      ]);
      expect(warnings).toEqual([expect.stringContaining("agent tools must be a list")]);
    } finally {
      fs.rmSync(base, { recursive: true, force: true });
    }
  });
});

// ─────────────────────────────────────────────────────────────────────────────
// extractInlineSubAgents — unit tests
// ─────────────────────────────────────────────────────────────────────────────

describe("extractInlineSubAgents", () => {
  it("returns original content unchanged when no markers present", () => {
    const content = "# Hello\n\nThis is a workflow.";
    const { mainContent, agents } = extractInlineSubAgents(content);
    expect(mainContent).toBe(content);
    expect(agents).toHaveLength(0);
  });

  it("returns empty main content and no agents for empty string", () => {
    const { mainContent, agents } = extractInlineSubAgents("");
    expect(mainContent).toBe("");
    expect(agents).toHaveLength(0);
  });

  it("extracts a single agent block", () => {
    const content = ["# Main workflow", "", "Handle the issue.", "", agentMarker("planner"), "---", "engine: copilot", "---", "You are a planning assistant."].join("\n");

    const { mainContent, agents } = extractInlineSubAgents(content);

    expect(mainContent).toBe("# Main workflow\n\nHandle the issue.");
    expect(agents).toHaveLength(1);
    expect(agents[0].name).toBe("planner");
    expect(agents[0].content).toContain("You are a planning assistant.");
    expect(agents[0].content).toContain("engine: copilot");
  });

  it("extracts multiple agent blocks", () => {
    const content = ["Main prompt.", "", agentMarker("planner"), "Planner prompt.", "", agentMarker("executor"), "Executor prompt."].join("\n");

    const { mainContent, agents } = extractInlineSubAgents(content);

    expect(mainContent).toBe("Main prompt.");
    expect(agents).toHaveLength(2);
    expect(agents[0].name).toBe("planner");
    expect(agents[0].content).toBe("Planner prompt.");
    expect(agents[1].name).toBe("executor");
    expect(agents[1].content).toBe("Executor prompt.");
  });

  it("agent block ends at next H2 heading", () => {
    const content = ["Main prompt.", "", agentMarker("planner"), "Planner content.", "", "## Summary", "This content is outside the agent block."].join("\n");

    const { mainContent, agents } = extractInlineSubAgents(content);

    expect(mainContent).toBe("Main prompt.");
    expect(agents).toHaveLength(1);
    expect(agents[0].name).toBe("planner");
    expect(agents[0].content).toBe("Planner content.");
    expect(agents[0].content).not.toContain("Summary");
    expect(agents[0].content).not.toContain("outside the agent block");
  });

  it("preserves an implicit boundary before an inline skill", () => {
    const content = ["Main.", "", agentMarker("planner"), "Planner.", "", "## skill: `reporting`", "Reporting."].join("\n");

    const { mainContent, agents } = extractInlineSubAgents(content);

    expect(agents).toHaveLength(1);
    expect(agents[0]).toEqual({ name: "planner", content: "Planner." });
    expect(mainContent).toBe("Main.\n\n## skill: `reporting`\nReporting.");
  });

  it("next agent marker (H2) ends the previous agent block", () => {
    const content = ["Main.", "", agentMarker("planner"), "Planner.", "", agentMarker("executor"), "Executor."].join("\n");

    const { agents } = extractInlineSubAgents(content);

    expect(agents).toHaveLength(2);
    expect(agents[0].content).toBe("Planner.");
    expect(agents[1].content).toBe("Executor.");
  });

  it("agent at start of file produces empty main content", () => {
    const content = agentMarker("only") + "\nAgent content.";
    const { mainContent, agents } = extractInlineSubAgents(content);
    expect(mainContent).toBe("");
    expect(agents).toHaveLength(1);
    expect(agents[0].name).toBe("only");
  });

  it("agent content is trimmed", () => {
    const content = "Main.\n\n" + agentMarker("a") + "\n\n\n  Trimmed.  \n\n";
    const { agents } = extractInlineSubAgents(content);
    expect(agents[0].content).toBe("Trimmed.");
  });

  it("trailing newlines are stripped from main content", () => {
    const content = "Line 1.\nLine 2.\n\n\n" + agentMarker("a") + "\nContent.";
    const { mainContent } = extractInlineSubAgents(content);
    expect(mainContent).toBe("Line 1.\nLine 2.");
  });

  it("accepts valid lowercase name variants", () => {
    const cases = [{ name: "my-agent" }, { name: "my_agent" }, { name: "agent1" }, { name: "a" }, { name: "planner-v2" }];
    for (const { name } of cases) {
      const { agents } = extractInlineSubAgents("Main.\n\n" + agentMarker(name) + "\nContent.");
      expect(agents).toHaveLength(1);
      expect(agents[0].name).toBe(name);
    }
  });

  it("does not recognize invalid separator forms", () => {
    const invalids = ["## agent: `1agent`", "## agent: `my agent`", "## agent: `my/agent`", "## agent:", "## agent: myagent", "## agent: `MyAgent`", "# agent: `myagent`", "### agent: `myagent`"];
    for (const sep of invalids) {
      const content = `Main.\n\n${sep}\nContent.`;
      const { mainContent, agents } = extractInlineSubAgents(content);
      expect(mainContent).toBe(content);
      expect(agents).toHaveLength(0);
    }
  });

  // ── Explicit end marker ("## end agent: `name`") ───────────────────────────

  const agentEndMarker = name => `## end agent: \`${name}\``;

  it("explicit end marker resumes main content after it", () => {
    const content = ["Before.", "", agentMarker("planner"), "---", "engine: copilot", "---", "You are a planner.", agentEndMarker("planner"), "", "After."].join("\n");
    const { mainContent, agents } = extractInlineSubAgents(content);

    expect(agents).toHaveLength(1);
    expect(agents[0].name).toBe("planner");
    expect(agents[0].content).toBe("---\nengine: copilot\n---\nYou are a planner.");
    expect(mainContent).toBe("Before.\n\nAfter.");
  });

  it("explicit end marker allows nested H2 headings inside the agent", () => {
    const content = ["Main.", "", agentMarker("guide"), "## Section A", "Content A.", "## Section B", "Content B.", agentEndMarker("guide"), "", "Tail."].join("\n");
    const { mainContent, agents } = extractInlineSubAgents(content);

    expect(agents).toHaveLength(1);
    expect(agents[0].content).toBe("## Section A\nContent A.\n## Section B\nContent B.");
    expect(mainContent).toBe("Main.\n\nTail.");
  });

  it("supports mixing explicit and implicit end boundaries across agents", () => {
    const content = ["Main.", "", agentMarker("bounded"), "Bounded.", agentEndMarker("bounded"), "", "Interlude.", "", agentMarker("unbounded"), "Unbounded."].join("\n");
    const { mainContent, agents } = extractInlineSubAgents(content);

    expect(agents).toHaveLength(2);
    expect(agents[0]).toEqual({ name: "bounded", content: "Bounded." });
    expect(agents[1]).toEqual({ name: "unbounded", content: "Unbounded." });
    expect(mainContent).toBe("Main.\n\nInterlude.");
  });

  it("throws on an orphan end marker whose name has no matching start", () => {
    const content = ["Main.", "", agentMarker("planner"), "Content.", agentEndMarker("plannerr")].join("\n");
    expect(() => extractInlineSubAgents(content)).toThrow(/plannerr.*line 5/);
  });

  it("throws on a standalone end marker with no agent starts", () => {
    const content = ["Main.", "", agentEndMarker("planner")].join("\n");
    expect(() => extractInlineSubAgents(content)).toThrow(/planner.*line 3/);
  });

  it("throws when the end marker names a different agent than the one it could close", () => {
    const content = ["Main.", "", agentMarker("first"), "First content.", agentEndMarker("second")].join("\n");
    expect(() => extractInlineSubAgents(content)).toThrow(/second/);
  });

  it("falls back to implicit H2 boundary when end marker is malformed", () => {
    const malformed = ["## end agent:", "## end agent: planner", "### end agent: `planner`"];
    for (const line of malformed) {
      const content = "Main.\n\n" + agentMarker("planner") + "\nContent.\n" + line + "\nTail.";
      const { mainContent, agents } = extractInlineSubAgents(content);
      expect(agents).toHaveLength(1);
      expect(agents[0].name).toBe("planner");
      // Legacy implicit H2 closing discards content after the boundary from
      // the reassembled main prompt unless the boundary is another inline block.
      expect(mainContent).not.toContain("Tail.");
    }
  });
});

// ─────────────────────────────────────────────────────────────────────────────
// closeUnterminatedSubAgentMarkers — unit tests
// ─────────────────────────────────────────────────────────────────────────────

describe("closeUnterminatedSubAgentMarkers", () => {
  const agentEndMarker = name => `## end agent: \`${name}\``;

  it("adds an explicit end marker at EOF for an unterminated sub-agent", () => {
    const content = ["Main.", "", agentMarker("helper"), "Agent content."].join("\n");

    expect(closeUnterminatedSubAgentMarkers(content)).toBe(["Main.", "", agentMarker("helper"), "Agent content.", "", agentEndMarker("helper"), ""].join("\n"));
  });

  it("adds an explicit end marker before the next H2 boundary", () => {
    const content = ["Main.", "", agentMarker("helper"), "Agent content.", "## skill: `reporting`", "Skill content."].join("\n");

    expect(closeUnterminatedSubAgentMarkers(content)).toBe(["Main.", "", agentMarker("helper"), "Agent content.", "", agentEndMarker("helper"), "## skill: `reporting`", "Skill content."].join("\n"));
  });

  it("leaves an explicitly terminated sub-agent unchanged", () => {
    const content = ["Main.", "", agentMarker("helper"), "Agent content.", agentEndMarker("helper"), "", "Tail."].join("\n");

    expect(closeUnterminatedSubAgentMarkers(content)).toBe(content);
  });
});

// ─────────────────────────────────────────────────────────────────────────────
// writeInlineSubAgents — integration tests (real filesystem)
// ─────────────────────────────────────────────────────────────────────────────

describe("writeInlineSubAgents", () => {
  let tmpDir;

  beforeEach(() => {
    tmpDir = fs.mkdtempSync(path.join(process.cwd(), ".inline-agents-test-"));
  });

  afterEach(() => {
    if (fs.existsSync(tmpDir)) {
      fs.rmSync(tmpDir, { recursive: true, force: true });
    }
  });

  it("returns original content unchanged when no markers present", () => {
    const content = "# Workflow\n\nNo agents here.";
    const result = writeInlineSubAgents(content, tmpDir);
    expect(result).toBe(content);
    const agentsDir = path.join(tmpDir, ".github", "agents");
    expect(fs.existsSync(agentsDir)).toBe(false);
  });

  it("writes a single agent file and returns main content", () => {
    const content = ["# Workflow", "", "Main prompt.", "", agentMarker("helper"), "---", "model: claude-haiku-4.5", "---", "You are a helper."].join("\n");

    const result = writeInlineSubAgents(content, tmpDir);

    expect(result).toBe("# Workflow\n\nMain prompt.");

    const agentPath = path.join(tmpDir, ".github", "agents", "helper.agent.md");
    expect(fs.existsSync(agentPath)).toBe(true);
    const written = fs.readFileSync(agentPath, "utf8");
    expect(written).toContain("You are a helper.");
    expect(written).toContain("model: claude-haiku-4.5");
  });

  it("writes multiple agent files", () => {
    const content = ["Main.", "", agentMarker("planner"), "Planner.", "", agentMarker("executor"), "Executor."].join("\n");

    writeInlineSubAgents(content, tmpDir);

    expect(fs.existsSync(path.join(tmpDir, ".github", "agents", "planner.agent.md"))).toBe(true);
    expect(fs.existsSync(path.join(tmpDir, ".github", "agents", "executor.agent.md"))).toBe(true);
  });

  it("agent file content ends with a newline", () => {
    const content = "Main.\n\n" + agentMarker("a") + "\nContent without trailing newline";
    writeInlineSubAgents(content, tmpDir);
    const written = fs.readFileSync(path.join(tmpDir, ".github", "agents", "a.agent.md"), "utf8");
    expect(written.endsWith("\n")).toBe(true);
  });

  it("creates .github/agents directory if it does not exist", () => {
    const content = "Main.\n\n" + agentMarker("new") + "\nContent.";
    const agentsDir = path.join(tmpDir, ".github", "agents");
    expect(fs.existsSync(agentsDir)).toBe(false);
    writeInlineSubAgents(content, tmpDir);
    expect(fs.existsSync(agentsDir)).toBe(true);
  });

  it("agent block ends at H2 — content after is not written to agent file", () => {
    const content = ["Main.", "", agentMarker("a"), "Agent body.", "", "## Notes", "Footer content that should not appear in the agent file."].join("\n");

    const result = writeInlineSubAgents(content, tmpDir);

    expect(result).toBe("Main.");
    const written = fs.readFileSync(path.join(tmpDir, ".github", "agents", "a.agent.md"), "utf8");
    expect(written).toContain("Agent body.");
    expect(written).not.toContain("Footer content");
  });

  it("preserves sub-agent frontmatter exactly when writing agent file", () => {
    const content = ["Main.", "", agentMarker("a"), "---", "engine: copilot", "model: claude-haiku-4.5", "tools:", "  github:", "    toolsets: [issues]", "---", "Agent prompt."].join("\n");

    writeInlineSubAgents(content, tmpDir);

    const written = fs.readFileSync(path.join(tmpDir, ".github", "agents", "a.agent.md"), "utf8");
    expect(written).toContain("engine: copilot");
    expect(written).toContain("model: claude-haiku-4.5");
    expect(written).toContain("tools:");
    expect(written).toContain("Agent prompt.");
  });

  it("preserves the authored frontmatter field order", () => {
    const content = ["Main.", "", agentMarker("a"), "---", "description: A helpful agent", "model: gpt-4", "engine: openai", "---", "Prompt."].join("\n");

    writeInlineSubAgents(content, tmpDir);

    const written = fs.readFileSync(path.join(tmpDir, ".github", "agents", "a.agent.md"), "utf8");
    expect(written).toBe(["---", "description: A helpful agent", "model: gpt-4", "engine: openai", "---", "Prompt.", ""].join("\n"));
  });
});

// ─────────────────────────────────────────────────────────────────────────────
// preserveSubAgentFrontmatter — unit tests
// ─────────────────────────────────────────────────────────────────────────────

describe("preserveSubAgentFrontmatter", () => {
  it("returns content unchanged when no frontmatter present", () => {
    const content = "Just a plain prompt.";
    expect(preserveSubAgentFrontmatter(content)).toBe(content);
  });

  it("keeps description and model fields", () => {
    const content = "---\ndescription: A planner\nmodel: claude-haiku-4.5\n---\nPrompt.";
    const result = preserveSubAgentFrontmatter(content);
    expect(result).toContain("description: A planner");
    expect(result).toContain("model: claude-haiku-4.5");
    expect(result).toContain("Prompt.");
  });

  it("preserves unsupported fields too", () => {
    const content = "---\nengine: copilot\nmodel: claude-haiku-4.5\ndescription: Helper\n---\nPrompt.";
    const result = preserveSubAgentFrontmatter(content);
    expect(result).toBe(content);
  });

  it("preserves the entire frontmatter when nested fields appear between supported fields", () => {
    const content = ["---", "description: Helper", "tools:", "  github:", "    toolsets: [issues]", "model: claude-haiku-4.5", "engine: copilot", "---", "Prompt."].join("\n");

    expect(preserveSubAgentFrontmatter(content)).toBe(content);
  });

  it("preserves frontmatter even when it only contains previously unsupported fields", () => {
    const content = "---\nengine: copilot\ntools:\n  github:\n    toolsets: [issues]\n---\nPrompt.";
    const result = preserveSubAgentFrontmatter(content);
    expect(result).toBe(content);
  });

  it("returns content unchanged when no closing delimiter found", () => {
    const content = "---\nengine: copilot\nPrompt without closing delimiter.";
    expect(preserveSubAgentFrontmatter(content)).toBe(content);
  });

  it("handles content with only model field", () => {
    const content = "---\nmodel: gpt-4o\n---\nYou are a summarizer.";
    const result = preserveSubAgentFrontmatter(content);
    expect(result).toBe(content);
  });

  it("handles content with only description field", () => {
    const content = "---\ndescription: Summarizes files\n---\nYou are a summarizer.";
    const result = preserveSubAgentFrontmatter(content);
    expect(result).toBe(content);
  });
});
