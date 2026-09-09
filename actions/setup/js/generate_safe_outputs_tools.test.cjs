// @ts-check
import { describe, it, expect, beforeEach, afterEach } from "vitest";
import fs from "fs";
import path from "path";
import { execSync } from "child_process";

const scriptPath = path.join(__dirname, "generate_safe_outputs_tools.cjs");

describe("generate_safe_outputs_tools", () => {
  /** @type {string} */
  let testDir;
  /** @type {string} */
  let toolsSourcePath;
  /** @type {string} */
  let configPath;
  /** @type {string} */
  let toolsMetaPath;
  /** @type {string} */
  let outputPath;

  const sampleSourceTools = [
    {
      name: "create_issue",
      description: "Creates a GitHub issue.",
      inputSchema: {
        type: "object",
        properties: {
          title: { type: "string", description: "Issue title" },
          body: { type: "string", description: "Issue body" },
        },
        required: ["title"],
      },
    },
    {
      name: "add_comment",
      description: "Adds a comment.",
      inputSchema: {
        type: "object",
        properties: {
          body: { type: "string", description: "Comment body" },
        },
        required: ["body"],
      },
    },
    {
      name: "missing_tool",
      description: "Reports a missing tool.",
      inputSchema: { type: "object", properties: {} },
    },
  ];

  beforeEach(() => {
    const testId = Math.random().toString(36).substring(7);
    testDir = `/tmp/test-generate-tools-${testId}`;
    fs.mkdirSync(testDir, { recursive: true });

    toolsSourcePath = path.join(testDir, "safe_outputs_tools.json");
    configPath = path.join(testDir, "config.json");
    toolsMetaPath = path.join(testDir, "tools_meta.json");
    outputPath = path.join(testDir, "tools.json");

    // Write source tools
    fs.writeFileSync(toolsSourcePath, JSON.stringify(sampleSourceTools));
  });

  afterEach(() => {
    fs.rmSync(testDir, { recursive: true, force: true });
  });

  /**
   * Run the generate script with the test env vars.
   * @param {Record<string, string>} [extraEnv] Additional env vars to set.
   * @returns {string} stdout output of the script.
   */
  function runScript(extraEnv = {}) {
    const env = {
      ...process.env,
      GH_AW_SAFE_OUTPUTS_TOOLS_SOURCE_PATH: toolsSourcePath,
      GH_AW_SAFE_OUTPUTS_CONFIG_PATH: configPath,
      GH_AW_SAFE_OUTPUTS_TOOLS_META_PATH: toolsMetaPath,
      GH_AW_SAFE_OUTPUTS_TOOLS_PATH: outputPath,
      ...extraEnv,
    };
    return execSync(`node ${scriptPath}`, { env, encoding: "utf8" });
  }

  it("filters tools based on config keys", () => {
    // Only create_issue and add_comment are enabled
    fs.writeFileSync(configPath, JSON.stringify({ create_issue: { max: 5 }, add_comment: { max: 10 } }));
    fs.writeFileSync(toolsMetaPath, JSON.stringify({ description_suffixes: {}, repo_params: {}, dynamic_tools: [] }));

    runScript();

    const result = JSON.parse(fs.readFileSync(outputPath, "utf8"));
    expect(result).toHaveLength(2);
    expect(result.map((/** @type {{name: string}} */ t) => t.name)).toEqual(expect.arrayContaining(["create_issue", "add_comment"]));
    // missing_tool should NOT be included since it's not in config
    expect(result.map((/** @type {{name: string}} */ t) => t.name)).not.toContain("missing_tool");
  });

  it("preserves namespaced public names", () => {
    fs.writeFileSync(
      toolsSourcePath,
      JSON.stringify([
        ...sampleSourceTools,
        {
          name: "ado_create_work_item",
          description: "Creates an Azure DevOps work item.",
          inputSchema: { type: "object", properties: {} },
        },
      ])
    );
    fs.writeFileSync(configPath, JSON.stringify({ ado_create_work_item: { max: 1 } }));
    fs.writeFileSync(toolsMetaPath, JSON.stringify({ description_suffixes: {}, repo_params: {}, dynamic_tools: [] }));

    runScript();

    const result = JSON.parse(fs.readFileSync(outputPath, "utf8"));
    expect(result).toHaveLength(1);
    expect(result[0].name).toBe("ado_create_work_item");
  });

  it("applies description suffix from tools_meta", () => {
    fs.writeFileSync(configPath, JSON.stringify({ create_issue: { max: 5 } }));
    fs.writeFileSync(
      toolsMetaPath,
      JSON.stringify({
        description_suffixes: {
          create_issue: " CONSTRAINTS: Maximum 5 issue(s) can be created.",
        },
        repo_params: {},
        dynamic_tools: [],
      })
    );

    runScript();

    const result = JSON.parse(fs.readFileSync(outputPath, "utf8"));
    const createIssueTool = result.find((/** @type {{name: string}} */ t) => t.name === "create_issue");
    expect(createIssueTool).toBeDefined();
    expect(createIssueTool.description).toContain("Creates a GitHub issue.");
    expect(createIssueTool.description).toContain("CONSTRAINTS: Maximum 5 issue(s) can be created.");
  });

  it("adds repo parameter when specified in tools_meta", () => {
    fs.writeFileSync(configPath, JSON.stringify({ create_issue: { max: 5 } }));
    fs.writeFileSync(
      toolsMetaPath,
      JSON.stringify({
        description_suffixes: {},
        repo_params: {
          create_issue: {
            type: "string",
            description: "Target repository in 'owner/repo' format.",
          },
        },
        dynamic_tools: [],
      })
    );

    runScript();

    const result = JSON.parse(fs.readFileSync(outputPath, "utf8"));
    const createIssueTool = result.find((/** @type {{name: string}} */ t) => t.name === "create_issue");
    expect(createIssueTool).toBeDefined();
    expect(createIssueTool.inputSchema.properties.repo).toBeDefined();
    expect(createIssueTool.inputSchema.properties.repo.type).toBe("string");
  });

  it("adds required fields when specified in tools_meta", () => {
    fs.writeFileSync(configPath, JSON.stringify({ create_issue: { max: 5 } }));
    fs.writeFileSync(
      toolsMetaPath,
      JSON.stringify({
        description_suffixes: {},
        repo_params: {},
        dynamic_tools: [],
        required_field_additions: {
          create_issue: ["temporary_id"],
        },
      })
    );

    runScript();

    const result = JSON.parse(fs.readFileSync(outputPath, "utf8"));
    const createIssueTool = result.find((/** @type {{name: string}} */ t) => t.name === "create_issue");
    expect(createIssueTool).toBeDefined();
    expect(createIssueTool.inputSchema.required).toEqual(expect.arrayContaining(["title", "temporary_id"]));
  });

  it("adds anyOf alternative requirements for assign_milestone", () => {
    fs.writeFileSync(
      toolsSourcePath,
      JSON.stringify([
        {
          name: "assign_milestone",
          description: "Assign milestone.",
          inputSchema: {
            type: "object",
            properties: {
              issue_number: { type: "number" },
              milestone_number: { type: "number" },
              milestone_title: { type: "string" },
            },
            required: ["issue_number"],
          },
        },
      ])
    );
    fs.writeFileSync(configPath, JSON.stringify({ assign_milestone: {} }));
    fs.writeFileSync(toolsMetaPath, JSON.stringify({ description_suffixes: {}, repo_params: {}, dynamic_tools: [] }));

    runScript();

    const result = JSON.parse(fs.readFileSync(outputPath, "utf8"));
    expect(result[0].inputSchema.anyOf).toEqual([{ required: ["milestone_number"] }, { required: ["milestone_title"] }]);
  });

  it("appends dynamic tools from tools_meta", () => {
    fs.writeFileSync(configPath, JSON.stringify({ create_issue: { max: 1 } }));
    fs.writeFileSync(
      toolsMetaPath,
      JSON.stringify({
        description_suffixes: {},
        repo_params: {},
        dynamic_tools: [
          {
            name: "dispatch_deploy_workflow",
            description: "Dispatches the deploy workflow.",
            inputSchema: { type: "object", properties: { env: { type: "string" } } },
            _workflow_name: "deploy",
          },
        ],
      })
    );

    runScript();

    const result = JSON.parse(fs.readFileSync(outputPath, "utf8"));
    expect(result).toHaveLength(2);
    expect(result.map((/** @type {{name: string}} */ t) => t.name)).toContain("dispatch_deploy_workflow");
    const dynamicTool = result.find((/** @type {{name: string, _workflow_name?: string}} */ t) => t._workflow_name === "deploy");
    expect(dynamicTool).toBeDefined();
  });

  it("handles empty config with no enabled tools", () => {
    fs.writeFileSync(configPath, JSON.stringify({}));
    fs.writeFileSync(toolsMetaPath, JSON.stringify({ description_suffixes: {}, repo_params: {}, dynamic_tools: [] }));

    runScript();

    const result = JSON.parse(fs.readFileSync(outputPath, "utf8"));
    expect(result).toHaveLength(0);
  });

  it("resolves env placeholders in GH_AW_TOOLS_META_JSON", () => {
    fs.writeFileSync(configPath, JSON.stringify({ create_issue: { max: 1 } }));
    const metaFromEnv = JSON.stringify({
      description_suffixes: {
        create_issue: " TARGET: ${GH_AW_INPUT_TARGET_REPO}",
      },
      repo_params: {},
      dynamic_tools: [],
    });

    runScript({
      GH_AW_TOOLS_META_JSON: metaFromEnv,
      GH_AW_INPUT_TARGET_REPO: "github/gh-aw",
    });

    const result = JSON.parse(fs.readFileSync(outputPath, "utf8"));
    const createIssueTool = result.find((/** @type {{name: string, description: string}} */ t) => t.name === "create_issue");
    expect(createIssueTool).toBeDefined();
    expect(createIssueTool.description).toContain("TARGET: github/gh-aw");
  });

  it("resolves multiple distinct env placeholders in GH_AW_TOOLS_META_JSON", () => {
    fs.writeFileSync(configPath, JSON.stringify({ create_issue: { max: 1 } }));
    const metaFromEnv = JSON.stringify({
      description_suffixes: {
        create_issue: " TARGET: ${GH_AW_INPUT_TARGET_REPO} OWNER: ${GH_AW_GITHUB_REPOSITORY_OWNER}",
      },
      repo_params: {},
      dynamic_tools: [],
    });

    runScript({
      GH_AW_TOOLS_META_JSON: metaFromEnv,
      GH_AW_INPUT_TARGET_REPO: "github/gh-aw",
      GH_AW_GITHUB_REPOSITORY_OWNER: "github",
    });

    const result = JSON.parse(fs.readFileSync(outputPath, "utf8"));
    const createIssueTool = result.find((/** @type {{name: string, description: string}} */ t) => t.name === "create_issue");
    expect(createIssueTool).toBeDefined();
    expect(createIssueTool.description).toContain("TARGET: github/gh-aw");
    expect(createIssueTool.description).toContain("OWNER: github");
  });

  it("leaves unresolved placeholders in GH_AW_TOOLS_META_JSON unchanged", () => {
    fs.writeFileSync(configPath, JSON.stringify({ create_issue: { max: 1 } }));
    const metaFromEnv = JSON.stringify({
      description_suffixes: {
        create_issue: " TARGET: ${GH_AW_INPUT_MISSING}",
      },
      repo_params: {},
      dynamic_tools: [],
    });

    runScript({
      GH_AW_TOOLS_META_JSON: metaFromEnv,
    });

    const result = JSON.parse(fs.readFileSync(outputPath, "utf8"));
    const createIssueTool = result.find((/** @type {{name: string, description: string}} */ t) => t.name === "create_issue");
    expect(createIssueTool).toBeDefined();
    expect(createIssueTool.description).toContain("TARGET: ${GH_AW_INPUT_MISSING}");
  });

  it("escapes quotes, backslashes, and newlines when resolving GH_AW_TOOLS_META_JSON placeholders", () => {
    fs.writeFileSync(configPath, JSON.stringify({ create_issue: { max: 1 } }));
    const metaFromEnv = JSON.stringify({
      description_suffixes: {
        create_issue: " TARGET: ${GH_AW_INPUT_TARGET_REPO}",
      },
      repo_params: {},
      dynamic_tools: [],
    });

    runScript({
      GH_AW_TOOLS_META_JSON: metaFromEnv,
      GH_AW_INPUT_TARGET_REPO: 'a"b\\c\nd',
    });

    // The written tools_meta.json must remain valid JSON despite the unsafe characters.
    const writtenMeta = JSON.parse(fs.readFileSync(toolsMetaPath, "utf8"));
    expect(writtenMeta.description_suffixes.create_issue).toContain('a"b\\c\nd');

    const result = JSON.parse(fs.readFileSync(outputPath, "utf8"));
    const createIssueTool = result.find((/** @type {{name: string, description: string}} */ t) => t.name === "create_issue");
    expect(createIssueTool).toBeDefined();
    expect(createIssueTool.description).toContain('TARGET: a"b\\c\nd');
  });

  it("ignores non-tool config keys when filtering", () => {
    // dispatch_workflow and max_bot_mentions are not tool names in source file
    fs.writeFileSync(
      configPath,
      JSON.stringify({
        create_issue: { max: 1 },
        dispatch_workflow: { workflows: ["deploy"] },
        max_bot_mentions: 5,
      })
    );
    fs.writeFileSync(toolsMetaPath, JSON.stringify({ description_suffixes: {}, repo_params: {}, dynamic_tools: [] }));

    runScript();

    const result = JSON.parse(fs.readFileSync(outputPath, "utf8"));
    // Only create_issue should be in filtered static tools
    expect(result.map((/** @type {{name: string}} */ t) => t.name)).not.toContain("dispatch_workflow");
    expect(result.map((/** @type {{name: string}} */ t) => t.name)).not.toContain("max_bot_mentions");
    expect(result.map((/** @type {{name: string}} */ t) => t.name)).toContain("create_issue");
  });

  it("does not modify source tools in memory (deep copy)", () => {
    fs.writeFileSync(configPath, JSON.stringify({ create_issue: { max: 5 } }));
    fs.writeFileSync(
      toolsMetaPath,
      JSON.stringify({
        description_suffixes: { create_issue: " CONSTRAINTS: Maximum 5 issue(s)." },
        repo_params: {
          create_issue: { type: "string", description: "Target repo" },
        },
        dynamic_tools: [],
      })
    );

    // Run twice to ensure source tools are not modified between runs
    runScript();
    const result1 = JSON.parse(fs.readFileSync(outputPath, "utf8"));
    runScript();
    const result2 = JSON.parse(fs.readFileSync(outputPath, "utf8"));

    expect(result1[0].description).toEqual(result2[0].description);
    expect(result1[0].inputSchema.properties.repo).toEqual(result2[0].inputSchema.properties.repo);
  });

  it("exits with error when source tools file is missing", () => {
    fs.writeFileSync(configPath, JSON.stringify({ create_issue: {} }));
    fs.writeFileSync(toolsMetaPath, JSON.stringify({ description_suffixes: {}, repo_params: {}, dynamic_tools: [] }));

    expect(() => runScript({ GH_AW_SAFE_OUTPUTS_TOOLS_SOURCE_PATH: "/nonexistent/path.json" })).toThrow("ERR_CONFIG:");
  });

  it("exits with error when config file is missing", () => {
    fs.writeFileSync(toolsMetaPath, JSON.stringify({ description_suffixes: {}, repo_params: {}, dynamic_tools: [] }));

    expect(() => runScript({ GH_AW_SAFE_OUTPUTS_CONFIG_PATH: "/nonexistent/config.json" })).toThrow("ERR_CONFIG:");
  });

  it("works when tools_meta file is missing (graceful fallback)", () => {
    fs.writeFileSync(configPath, JSON.stringify({ create_issue: { max: 1 } }));
    // No tools_meta.json - should still work with fallback to empty meta

    runScript({ GH_AW_SAFE_OUTPUTS_TOOLS_META_PATH: "/nonexistent/tools_meta.json" });

    const result = JSON.parse(fs.readFileSync(outputPath, "utf8"));
    expect(result).toHaveLength(1);
    expect(result[0].name).toBe("create_issue");
    // Description should be unchanged (no suffix applied)
    expect(result[0].description).toBe("Creates a GitHub issue.");
  });

  it("dynamically marks add_comment discussion support as enabled when discussions:true", () => {
    fs.writeFileSync(configPath, JSON.stringify({ add_comment: { discussions: true } }));
    fs.writeFileSync(toolsMetaPath, JSON.stringify({ description_suffixes: {}, repo_params: {}, dynamic_tools: [] }));

    runScript();

    const result = JSON.parse(fs.readFileSync(outputPath, "utf8"));
    const addCommentTool = result.find((/** @type {{name: string, description: string}} */ t) => t.name === "add_comment");
    expect(addCommentTool).toBeDefined();
    expect(addCommentTool.description).toContain("Discussion comments are enabled for this workflow");
    expect(addCommentTool.description).toContain("Supports reply_to_id for discussion threading.");
  });

  it("dynamically marks add_comment discussion support as disabled by default", () => {
    fs.writeFileSync(configPath, JSON.stringify({ add_comment: { max: 1 } }));
    fs.writeFileSync(
      toolsMetaPath,
      JSON.stringify({
        description_suffixes: { add_comment: " Supports reply_to_id for discussion threading." },
        repo_params: {},
        dynamic_tools: [],
      })
    );

    runScript();

    const result = JSON.parse(fs.readFileSync(outputPath, "utf8"));
    const addCommentTool = result.find((/** @type {{name: string, description: string}} */ t) => t.name === "add_comment");
    expect(addCommentTool).toBeDefined();
    expect(addCommentTool.description).toContain("Discussion comments are disabled for this workflow");
    expect(addCommentTool.description).not.toContain("Supports reply_to_id for discussion threading.");
  });

  it("adds issue intent suffix for issue tools when explicitly enabled", () => {
    fs.writeFileSync(
      toolsSourcePath,
      JSON.stringify([
        { name: "set_issue_type", description: "Sets issue type.", inputSchema: { type: "object", properties: {} } },
        { name: "set_issue_field", description: "Sets issue field.", inputSchema: { type: "object", properties: {} } },
        { name: "add_labels", description: "Adds labels.", inputSchema: { type: "object", properties: {} } },
        { name: "close_issue", description: "Closes issue.", inputSchema: { type: "object", properties: {} } },
        { name: "assign_to_user", description: "Assigns users.", inputSchema: { type: "object", properties: {} } },
        { name: "assign_to_agent", description: "Assigns agent.", inputSchema: { type: "object", properties: {} } },
        { name: "create_issue", description: "Creates a GitHub issue.", inputSchema: { type: "object", properties: {} } },
      ])
    );
    fs.writeFileSync(
      configPath,
      JSON.stringify({
        set_issue_type: { issue_intent: true },
        set_issue_field: { issue_intent: true },
        add_labels: { issue_intent: true },
        close_issue: { issue_intent: true },
        assign_to_user: { issue_intent: true },
        assign_to_agent: { issue_intent: true },
        create_issue: {},
      })
    );
    fs.writeFileSync(toolsMetaPath, JSON.stringify({ description_suffixes: {}, repo_params: {}, dynamic_tools: [] }));

    runScript();

    const result = JSON.parse(fs.readFileSync(outputPath, "utf8"));
    const intentRequiredSuffix = "INTENT REQUIRED: rationale (string, max 280 chars) and confidence (exactly one of: LOW, MEDIUM, HIGH) are required for each call.";
    expect(result.find((/** @type {{name: string, description: string}} */ t) => t.name === "set_issue_type").description).toContain(intentRequiredSuffix);
    expect(result.find((/** @type {{name: string, description: string}} */ t) => t.name === "set_issue_field").description).toContain(intentRequiredSuffix);
    expect(result.find((/** @type {{name: string, description: string}} */ t) => t.name === "add_labels").description).toContain(intentRequiredSuffix);
    expect(result.find((/** @type {{name: string, description: string}} */ t) => t.name === "close_issue").description).toContain(intentRequiredSuffix);
    expect(result.find((/** @type {{name: string, description: string}} */ t) => t.name === "assign_to_user").description).toContain(intentRequiredSuffix);
    expect(result.find((/** @type {{name: string, description: string}} */ t) => t.name === "assign_to_agent").description).toContain(intentRequiredSuffix);
    expect(result.find((/** @type {{name: string, description: string}} */ t) => t.name === "create_issue").description).not.toContain(intentRequiredSuffix);
  });

  it("adds issue intent suffix even when unrelated runtime features are present", () => {
    fs.writeFileSync(
      toolsSourcePath,
      JSON.stringify([
        { name: "set_issue_type", description: "Sets issue type.", inputSchema: { type: "object", properties: {} } },
        { name: "set_issue_field", description: "Sets issue field.", inputSchema: { type: "object", properties: {} } },
        { name: "add_labels", description: "Adds labels.", inputSchema: { type: "object", properties: {} } },
      ])
    );
    fs.writeFileSync(configPath, JSON.stringify({ set_issue_type: { issue_intent: true }, set_issue_field: { issue_intent: true }, add_labels: { issue_intent: true } }));
    fs.writeFileSync(toolsMetaPath, JSON.stringify({ description_suffixes: {}, repo_params: {}, dynamic_tools: [] }));

    runScript({ GH_AW_RUNTIME_FEATURES: "other\nanother=true" });

    const result = JSON.parse(fs.readFileSync(outputPath, "utf8"));
    const intentRequiredSuffix = "INTENT REQUIRED: rationale (string, max 280 chars) and confidence (exactly one of: LOW, MEDIUM, HIGH) are required for each call.";
    expect(result.find((/** @type {{name: string, description: string}} */ t) => t.name === "set_issue_type").description).toContain(intentRequiredSuffix);
    expect(result.find((/** @type {{name: string, description: string}} */ t) => t.name === "set_issue_field").description).toContain(intentRequiredSuffix);
    expect(result.find((/** @type {{name: string, description: string}} */ t) => t.name === "add_labels").description).toContain(intentRequiredSuffix);
  });

  it("omits issue intent guidance when explicitly disabled per tool", () => {
    fs.writeFileSync(toolsSourcePath, JSON.stringify([{ name: "close_issue", description: "Closes issue.", inputSchema: { type: "object", properties: {} } }]));
    fs.writeFileSync(configPath, JSON.stringify({ close_issue: { issue_intent: false } }));
    fs.writeFileSync(toolsMetaPath, JSON.stringify({ description_suffixes: {}, repo_params: {}, dynamic_tools: [] }));

    runScript();

    const result = JSON.parse(fs.readFileSync(outputPath, "utf8"));
    const closeIssue = result.find((/** @type {{name: string, description: string}} */ t) => t.name === "close_issue");
    expect(closeIssue.description).not.toContain("INTENT REQUIRED:");
    expect(closeIssue.description).not.toContain("INTENT ENCOURAGED:");
  });

  it("makes add_labels label items object-only with required name/rationale/confidence when issue_intent is true", () => {
    const addLabelsSourceTool = {
      name: "add_labels",
      description: "Adds labels.",
      inputSchema: {
        type: "object",
        properties: {
          labels: {
            type: "array",
            items: {
              oneOf: [
                { type: "string" },
                {
                  type: "object",
                  required: ["name"],
                  properties: {
                    name: { type: "string" },
                    rationale: { type: "string" },
                    confidence: { type: "string" },
                    suggest: { type: "boolean" },
                  },
                  additionalProperties: false,
                },
              ],
            },
          },
        },
        required: ["labels"],
      },
    };
    fs.writeFileSync(toolsSourcePath, JSON.stringify([addLabelsSourceTool]));
    fs.writeFileSync(configPath, JSON.stringify({ add_labels: { issue_intent: true } }));
    fs.writeFileSync(toolsMetaPath, JSON.stringify({ description_suffixes: {}, repo_params: {}, dynamic_tools: [] }));

    runScript();

    const result = JSON.parse(fs.readFileSync(outputPath, "utf8"));
    const tool = result.find((/** @type {{name: string}} */ t) => t.name === "add_labels");
    expect(tool).toBeDefined();
    const items = tool.inputSchema.properties.labels.items;
    // No longer a oneOf — plain strings are removed
    expect(items.oneOf).toBeUndefined();
    expect(items.type).toBe("object");
    // name, rationale, and confidence are all required
    expect(items.required).toEqual(expect.arrayContaining(["name", "rationale", "confidence"]));
  });

  it("does not modify add_labels label items schema when issue_intent is omitted or false", () => {
    const addLabelsSourceTool = {
      name: "add_labels",
      description: "Adds labels.",
      inputSchema: {
        type: "object",
        properties: {
          labels: {
            type: "array",
            items: {
              oneOf: [{ type: "string" }, { type: "object", required: ["name"], properties: { name: { type: "string" } } }],
            },
          },
        },
        required: ["labels"],
      },
    };
    fs.writeFileSync(toolsSourcePath, JSON.stringify([addLabelsSourceTool]));
    // Test omitted (default)
    fs.writeFileSync(configPath, JSON.stringify({ add_labels: {} }));
    fs.writeFileSync(toolsMetaPath, JSON.stringify({ description_suffixes: {}, repo_params: {}, dynamic_tools: [] }));

    runScript();

    const defaultResult = JSON.parse(fs.readFileSync(outputPath, "utf8"));
    const defaultTool = defaultResult.find((/** @type {{name: string}} */ t) => t.name === "add_labels");
    expect(defaultTool.inputSchema.properties.labels.items.oneOf).toBeDefined();

    // Test explicit false
    fs.writeFileSync(configPath, JSON.stringify({ add_labels: { issue_intent: false } }));
    runScript();

    const disabledResult = JSON.parse(fs.readFileSync(outputPath, "utf8"));
    const disabledTool = disabledResult.find((/** @type {{name: string}} */ t) => t.name === "add_labels");
    expect(disabledTool.inputSchema.properties.labels.items.oneOf).toBeDefined();
  });

  it("reflects required/optional/absent intent fields per tool configuration", () => {
    fs.writeFileSync(
      toolsSourcePath,
      JSON.stringify([
        {
          name: "close_issue",
          description: "Closes issue.",
          inputSchema: {
            type: "object",
            properties: { body: { type: "string" }, rationale: { type: "string" }, confidence: { type: "string" }, suggest: { type: "boolean" } },
            required: ["body"],
          },
        },
        {
          name: "assign_to_user",
          description: "Assigns users.",
          inputSchema: {
            type: "object",
            properties: { issue_number: { type: "number" }, rationale: { type: "string" }, confidence: { type: "string" }, suggest: { type: "boolean" } },
            required: ["issue_number"],
          },
        },
        {
          name: "assign_to_agent",
          description: "Assigns agent.",
          inputSchema: {
            type: "object",
            properties: { issue_number: { type: "number" }, rationale: { type: "string" }, confidence: { type: "string" }, suggest: { type: "boolean" } },
            required: ["issue_number"],
          },
        },
      ])
    );
    fs.writeFileSync(
      configPath,
      JSON.stringify({
        close_issue: { issue_intent: true },
        assign_to_user: {},
        assign_to_agent: { issue_intent: false },
      })
    );
    fs.writeFileSync(
      toolsMetaPath,
      JSON.stringify({
        description_suffixes: {},
        repo_params: {},
        dynamic_tools: [],
        required_field_additions: { close_issue: ["rationale", "confidence"] },
      })
    );

    runScript();

    const result = JSON.parse(fs.readFileSync(outputPath, "utf8"));
    const closeIssue = result.find((/** @type {{name: string, inputSchema: {properties: Record<string, unknown>, required: string[]}}} */ t) => t.name === "close_issue");
    const assignToUser = result.find((/** @type {{name: string, inputSchema: {properties: Record<string, unknown>, required: string[]}}} */ t) => t.name === "assign_to_user");
    const assignToAgent = result.find((/** @type {{name: string, inputSchema: {properties: Record<string, unknown>, required: string[]}}} */ t) => t.name === "assign_to_agent");

    expect(closeIssue.inputSchema.properties).toHaveProperty("rationale");
    expect(closeIssue.inputSchema.properties).toHaveProperty("confidence");
    expect(closeIssue.inputSchema.required).toEqual(expect.arrayContaining(["rationale", "confidence"]));

    expect(assignToUser.inputSchema.properties).toHaveProperty("rationale");
    expect(assignToUser.inputSchema.properties).toHaveProperty("confidence");
    expect(assignToUser.inputSchema.required).not.toContain("rationale");
    expect(assignToUser.inputSchema.required).not.toContain("confidence");

    expect(assignToAgent.inputSchema.properties).not.toHaveProperty("rationale");
    expect(assignToAgent.inputSchema.properties).not.toHaveProperty("confidence");
    expect(assignToAgent.inputSchema.properties).not.toHaveProperty("suggest");
    expect(assignToAgent.inputSchema.required).not.toContain("rationale");
    expect(assignToAgent.inputSchema.required).not.toContain("confidence");
  });

  it("adds encouraged intent guidance for all intent-aware tools when issue_intent is omitted", () => {
    fs.writeFileSync(
      toolsSourcePath,
      JSON.stringify([
        {
          name: "set_issue_type",
          description: "Sets issue type.",
          inputSchema: {
            type: "object",
            properties: { rationale: { type: "string", description: "Optional rationale for the change (max 280 characters)." }, confidence: { type: "string", description: "Optional confidence level for the change." } },
          },
        },
        {
          name: "set_issue_field",
          description: "Sets issue field.",
          inputSchema: {
            type: "object",
            properties: { rationale: { type: "string", description: "Optional rationale for the change (max 280 characters)." }, confidence: { type: "string", description: "Optional confidence level for the change." } },
          },
        },
        {
          name: "add_labels",
          description: "Adds labels.",
          inputSchema: { type: "object", properties: { labels: { type: "array", items: { oneOf: [{ type: "string" }, { type: "object", required: ["name"], properties: { name: { type: "string" } } }] } } } },
        },
        {
          name: "close_issue",
          description: "Closes issue.",
          inputSchema: {
            type: "object",
            properties: { rationale: { type: "string", description: "Optional rationale for closing the issue (max 280 characters)." }, confidence: { type: "string", description: "Optional confidence level for closing the issue." } },
          },
        },
        {
          name: "assign_to_user",
          description: "Assigns users.",
          inputSchema: {
            type: "object",
            properties: { rationale: { type: "string", description: "Optional rationale for the assignment (max 280 characters)." }, confidence: { type: "string", description: "Optional confidence level for the assignment." } },
          },
        },
        {
          name: "assign_to_agent",
          description: "Assigns agent.",
          inputSchema: {
            type: "object",
            properties: { rationale: { type: "string", description: "Optional rationale for the assignment (max 280 characters)." }, confidence: { type: "string", description: "Optional confidence level for the assignment." } },
          },
        },
      ])
    );
    // All tools with omitted issue_intent (no issue_intent key)
    fs.writeFileSync(
      configPath,
      JSON.stringify({
        set_issue_type: {},
        set_issue_field: {},
        add_labels: {},
        close_issue: {},
        assign_to_user: {},
        assign_to_agent: {},
      })
    );
    fs.writeFileSync(toolsMetaPath, JSON.stringify({ description_suffixes: {}, repo_params: {}, dynamic_tools: [] }));

    runScript();

    const result = JSON.parse(fs.readFileSync(outputPath, "utf8"));
    const optionalSuffix = "INTENT ENCOURAGED:";
    const requiredSuffix = "INTENT REQUIRED:";
    const toolNames = ["set_issue_type", "set_issue_field", "add_labels", "close_issue", "assign_to_user", "assign_to_agent"];
    for (const name of toolNames) {
      const tool = result.find((/** @type {{name: string}} */ t) => t.name === name);
      expect(tool, `${name} should be in result`).toBeDefined();
      expect(tool.description, `${name} should have encouraged suffix`).toContain(optionalSuffix);
      expect(tool.description, `${name} should not have required suffix`).not.toContain(requiredSuffix);
    }
    // Rationale and confidence should remain optional (not added to required array, descriptions unchanged)
    for (const name of toolNames.filter(n => n !== "add_labels")) {
      const tool = result.find((/** @type {{name: string, inputSchema: {required?: string[]}}} */ t) => t.name === name);
      const required = tool.inputSchema.required ?? [];
      expect(required, `${name} rationale should not be required`).not.toContain("rationale");
      expect(required, `${name} confidence should not be required`).not.toContain("confidence");
      const props = tool.inputSchema.properties;
      if (props?.rationale?.description) {
        expect(props.rationale.description, `${name} rationale description should remain optional`).toMatch(/optional/i);
      }
      if (props?.confidence?.description) {
        expect(props.confidence.description, `${name} confidence description should remain optional`).toMatch(/optional/i);
      }
    }
  });

  it("strict mode add_labels description does not permit plain strings and marks fields as required", () => {
    const addLabelsSourceTool = {
      name: "add_labels",
      description: "Adds labels.",
      inputSchema: {
        type: "object",
        properties: {
          labels: {
            type: "array",
            description: "Labels to add (e.g., ['bug', 'priority-high']). Each entry can be either a label name string or an object with name plus optional rationale/confidence/suggest intent metadata.",
            items: {
              oneOf: [
                { type: "string" },
                {
                  type: "object",
                  required: ["name"],
                  properties: {
                    name: { type: "string", description: "Label name to apply." },
                    rationale: { type: "string", maxLength: 280, description: "Optional rationale for the change (max 280 characters)." },
                    confidence: { type: "string", enum: ["LOW", "MEDIUM", "HIGH"], description: "Optional confidence level for the change." },
                    suggest: { type: "boolean" },
                  },
                  additionalProperties: false,
                },
              ],
            },
          },
        },
        required: ["labels"],
      },
    };
    fs.writeFileSync(toolsSourcePath, JSON.stringify([addLabelsSourceTool]));
    fs.writeFileSync(configPath, JSON.stringify({ add_labels: { issue_intent: true } }));
    fs.writeFileSync(toolsMetaPath, JSON.stringify({ description_suffixes: {}, repo_params: {}, dynamic_tools: [] }));

    runScript();

    const result = JSON.parse(fs.readFileSync(outputPath, "utf8"));
    const tool = result.find((/** @type {{name: string}} */ t) => t.name === "add_labels");
    const labelsDesc = tool.inputSchema.properties.labels.description;

    // Ensure the description does not say plain strings are *accepted* in any form
    // (e.g. "plain strings are accepted", "plain strings remain accepted").
    // Note: the strict description itself says "not permitted" — that IS the desired phrase.
    expect(labelsDesc).not.toMatch(/plain string[s]? (?:are |remain )?(?:also )?accepted/i);
    expect(labelsDesc).not.toMatch(/either a label name string/i);
    expect(labelsDesc).not.toMatch(/can be either a label name string/i);
    // Must not call rationale or confidence optional
    expect(labelsDesc).not.toMatch(/optional rationale/i);
    expect(labelsDesc).not.toMatch(/optional confidence/i);
    // Must include an object example with all required fields
    expect(labelsDesc).toContain('"name"');
    expect(labelsDesc).toContain('"rationale"');
    expect(labelsDesc).toContain('"confidence"');
    // Items rationale/confidence nested descriptions must not say "Optional"
    const items = tool.inputSchema.properties.labels.items;
    expect(items.properties.rationale.description).not.toMatch(/optional/i);
    expect(items.properties.confidence.description).not.toMatch(/optional/i);
  });

  it("omitted mode add_labels description prefers structured label objects and permits plain strings", () => {
    const addLabelsSourceTool = {
      name: "add_labels",
      description: "Adds labels.",
      inputSchema: {
        type: "object",
        properties: {
          labels: {
            type: "array",
            description: "Original description.",
            items: { oneOf: [{ type: "string" }, { type: "object", required: ["name"], properties: { name: { type: "string" } } }] },
          },
        },
        required: ["labels"],
      },
    };
    fs.writeFileSync(toolsSourcePath, JSON.stringify([addLabelsSourceTool]));
    fs.writeFileSync(configPath, JSON.stringify({ add_labels: {} }));
    fs.writeFileSync(toolsMetaPath, JSON.stringify({ description_suffixes: {}, repo_params: {}, dynamic_tools: [] }));

    runScript();

    const result = JSON.parse(fs.readFileSync(outputPath, "utf8"));
    const tool = result.find((/** @type {{name: string}} */ t) => t.name === "add_labels");
    const labelsDesc = tool.inputSchema.properties.labels.description;

    // Should include a structured object example
    expect(labelsDesc).toContain('"name"');
    expect(labelsDesc).toContain('"rationale"');
    expect(labelsDesc).toContain('"confidence"');
    // Should show suggest inside the label object example (not as a top-level field)
    expect(labelsDesc).toContain('"suggest"');
    // Should not say plain strings are forbidden (they are still allowed in omitted mode)
    expect(labelsDesc).not.toMatch(/plain string.*not permitted/i);
    // oneOf schema must be preserved (plain strings still valid)
    expect(tool.inputSchema.properties.labels.items.oneOf).toBeDefined();
  });

  it("strict mode updates rationale and confidence property descriptions to say required", () => {
    fs.writeFileSync(
      toolsSourcePath,
      JSON.stringify([
        {
          name: "close_issue",
          description: "Closes issue.",
          inputSchema: {
            type: "object",
            properties: {
              body: { type: "string" },
              rationale: { type: "string", maxLength: 280, description: "Optional rationale for the change (max 280 characters)." },
              confidence: { type: "string", enum: ["LOW", "MEDIUM", "HIGH"], description: "Optional confidence level for the change." },
              suggest: { type: "boolean" },
            },
            required: ["body"],
          },
        },
        {
          name: "assign_to_user",
          description: "Assigns users.",
          inputSchema: {
            type: "object",
            properties: {
              issue_number: { type: "number" },
              rationale: { type: "string", maxLength: 280, description: "Optional rationale for the assignment (max 280 characters)." },
              confidence: { type: "string", enum: ["LOW", "MEDIUM", "HIGH"], description: "Optional confidence level for the assignment." },
            },
            required: ["issue_number"],
          },
        },
      ])
    );
    fs.writeFileSync(configPath, JSON.stringify({ close_issue: { issue_intent: true }, assign_to_user: { issue_intent: true } }));
    fs.writeFileSync(toolsMetaPath, JSON.stringify({ description_suffixes: {}, repo_params: {}, dynamic_tools: [] }));

    runScript();

    const result = JSON.parse(fs.readFileSync(outputPath, "utf8"));
    for (const toolName of ["close_issue", "assign_to_user"]) {
      const tool = result.find((/** @type {{name: string}} */ t) => t.name === toolName);
      expect(tool.inputSchema.properties.rationale.description, `${toolName} rationale description`).not.toMatch(/optional/i);
      expect(tool.inputSchema.properties.confidence.description, `${toolName} confidence description`).not.toMatch(/optional/i);
      // rationale and confidence must be in required so JSON Schema validators enforce them
      expect(tool.inputSchema.required, `${toolName} rationale should be in required`).toContain("rationale");
      expect(tool.inputSchema.required, `${toolName} confidence should be in required`).toContain("confidence");
      // Pre-existing required fields must still be present
    }
    const closeIssueTool = result.find((/** @type {{name: string}} */ t) => t.name === "close_issue");
    expect(closeIssueTool.inputSchema.required, "close_issue should still require body").toContain("body");
    const assignToUserTool = result.find((/** @type {{name: string}} */ t) => t.name === "assign_to_user");
    expect(assignToUserTool.inputSchema.required, "assign_to_user should still require issue_number").toContain("issue_number");
  });

  it("strict mode assign_to_agent replaces inline examples with versions that include rationale and confidence", () => {
    const sourceDesc = 'Assigns agent. Example usage: assign_to_agent(issue_number=123, agent="copilot") or assign_to_agent(pull_number=456, agent="copilot", pull_request_repo="owner/repo")';
    fs.writeFileSync(
      toolsSourcePath,
      JSON.stringify([
        {
          name: "assign_to_agent",
          description: sourceDesc,
          inputSchema: {
            type: "object",
            properties: {
              issue_number: { type: "number" },
              rationale: { type: "string", maxLength: 280, description: "Optional rationale for the assignment (max 280 characters)." },
              confidence: { type: "string", enum: ["LOW", "MEDIUM", "HIGH"], description: "Optional confidence level for the assignment." },
            },
          },
        },
      ])
    );
    fs.writeFileSync(configPath, JSON.stringify({ assign_to_agent: { issue_intent: true } }));
    fs.writeFileSync(toolsMetaPath, JSON.stringify({ description_suffixes: {}, repo_params: {}, dynamic_tools: [] }));

    runScript();

    const result = JSON.parse(fs.readFileSync(outputPath, "utf8"));
    const tool = result.find((/** @type {{name: string}} */ t) => t.name === "assign_to_agent");
    // Source description must not have contained rationale/confidence in examples (ensures replacement is meaningful)
    expect(sourceDesc).not.toMatch(/assign_to_agent\([^)]*rationale=[^)]*\)/);
    expect(sourceDesc).not.toMatch(/assign_to_agent\([^)]*confidence=[^)]*\)/);
    // Strict mode: BOTH inline examples must include rationale and confidence
    const matches = [...tool.description.matchAll(/assign_to_agent\(([^)]+)\)/g)];
    expect(matches.length, "should have two assign_to_agent example calls").toBeGreaterThanOrEqual(2);
    for (const [, args] of matches) {
      expect(args, "each example call should include rationale").toContain("rationale=");
      expect(args, "each example call should include confidence").toContain("confidence=");
    }
    // Must still contain the intent required suffix
    expect(tool.description).toContain("INTENT REQUIRED:");
  });

  it("injects property_injections from tools_meta into tool inputSchema", () => {
    fs.writeFileSync(
      toolsSourcePath,
      JSON.stringify([
        {
          name: "close_issue",
          description: "Closes issue.",
          inputSchema: {
            type: "object",
            required: ["body"],
            properties: {
              body: { type: "string", description: "Closing comment." },
            },
            additionalProperties: false,
          },
        },
      ])
    );
    fs.writeFileSync(configPath, JSON.stringify({ close_issue: {} }));
    fs.writeFileSync(
      toolsMetaPath,
      JSON.stringify({
        description_suffixes: {},
        repo_params: {},
        dynamic_tools: [],
        property_injections: {
          close_issue: {
            state_reason: {
              type: "string",
              enum: ["completed", "not_planned", "duplicate"],
              description: "Optional closing state reason.",
            },
          },
        },
      })
    );

    runScript();

    const result = JSON.parse(fs.readFileSync(outputPath, "utf8"));
    const tool = result.find((/** @type {{name: string}} */ t) => t.name === "close_issue");
    expect(tool).toBeDefined();
    expect(tool.inputSchema.properties.state_reason).toBeDefined();
    expect(tool.inputSchema.properties.state_reason.type).toBe("string");
    expect(tool.inputSchema.properties.state_reason.enum).toEqual(["completed", "not_planned", "duplicate"]);
    // Injected property should NOT be required
    expect(tool.inputSchema.required || []).not.toContain("state_reason");
  });

  it("injects property_injections with restricted enum for list state-reason config", () => {
    fs.writeFileSync(
      toolsSourcePath,
      JSON.stringify([
        {
          name: "close_issue",
          description: "Closes issue.",
          inputSchema: { type: "object", required: ["body"], properties: { body: { type: "string" } }, additionalProperties: false },
        },
      ])
    );
    fs.writeFileSync(configPath, JSON.stringify({ close_issue: { allowed_state_reason: ["not_planned", "duplicate"] } }));
    fs.writeFileSync(
      toolsMetaPath,
      JSON.stringify({
        description_suffixes: {},
        repo_params: {},
        dynamic_tools: [],
        property_injections: {
          close_issue: {
            state_reason: {
              type: "string",
              enum: ["not_planned", "duplicate"],
              description: "Optional closing state reason.",
            },
          },
        },
      })
    );

    runScript();

    const result = JSON.parse(fs.readFileSync(outputPath, "utf8"));
    const tool = result.find((/** @type {{name: string}} */ t) => t.name === "close_issue");
    expect(tool.inputSchema.properties.state_reason.enum).toEqual(["not_planned", "duplicate"]);
  });

  it("does not inject state_reason when property_injections is absent (scalar state-reason config)", () => {
    fs.writeFileSync(
      toolsSourcePath,
      JSON.stringify([
        {
          name: "close_issue",
          description: "Closes issue.",
          inputSchema: { type: "object", required: ["body"], properties: { body: { type: "string" } }, additionalProperties: false },
        },
      ])
    );
    fs.writeFileSync(configPath, JSON.stringify({ close_issue: { state_reason: "not_planned" } }));
    fs.writeFileSync(
      toolsMetaPath,
      JSON.stringify({
        description_suffixes: {},
        repo_params: {},
        dynamic_tools: [],
        // No property_injections — scalar state-reason config
      })
    );

    runScript();

    const result = JSON.parse(fs.readFileSync(outputPath, "utf8"));
    const tool = result.find((/** @type {{name: string}} */ t) => t.name === "close_issue");
    expect(tool.inputSchema.properties.state_reason).toBeUndefined();
  });
});
