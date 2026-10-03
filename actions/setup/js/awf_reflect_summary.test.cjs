import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";

const mockCore = {
  info: vi.fn(),
  summary: {
    addRaw: vi.fn().mockReturnThis(),
    write: vi.fn().mockResolvedValue(),
  },
};

global.core = mockCore;

const REFLECT_PATH = "/tmp/gh-aw/agent/awf-reflect.json";
const LEGACY_REFLECT_PATH = path.join(process.env.RUNNER_TEMP || os.tmpdir(), "awf-reflect.json");
const REFLECT_ARTIFACT_PATH = "/tmp/gh-aw/sandbox/firewall/awf-reflect.json";
const CONFIG_PATH = "/tmp/gh-aw/awf-config.json";
const MODELS_PATH = "/tmp/gh-aw/sandbox/firewall/models.json";

/** Full sample /reflect response from the AWF api-proxy server */
const SAMPLE_REFLECT = {
  endpoints: [
    {
      provider: "openai",
      port: 10000,
      base_url: "http://api-proxy:10000",
      configured: true,
      models: ["gpt-4o", "gpt-4o-mini", "gpt-4-turbo", "gpt-3.5-turbo"],
      models_url: "http://api-proxy:10000/v1/models",
    },
    {
      provider: "anthropic",
      port: 10001,
      base_url: "http://api-proxy:10001",
      configured: false,
      models: null,
      models_url: "http://api-proxy:10001/v1/models",
    },
    {
      provider: "copilot",
      port: 10002,
      base_url: "http://api-proxy:10002",
      configured: true,
      models: ["claude-sonnet-4.6", "gpt-4o"],
      models_url: "http://api-proxy:10002/models",
    },
    {
      provider: "gemini",
      port: 10003,
      base_url: "http://api-proxy:10003",
      configured: false,
      models: null,
      models_url: "http://api-proxy:10003/v1beta/models",
    },
    {
      provider: "opencode",
      port: 10004,
      base_url: "http://api-proxy:10004",
      configured: true,
      models: null,
      models_url: null,
    },
  ],
  models_fetch_complete: true,
};

const SAMPLE_RUNTIME_MODELS = {
  endpoints: [
    {
      provider: "copilot",
      endpoint: "http://api-proxy:10002/models",
      models: [
        { id: "claude-sonnet-4.6", name: "Claude Sonnet 4.6" },
        { id: "gpt-4o", name: "GPT-4o" },
      ],
    },
    {
      provider: "openai",
      endpoint: "http://api-proxy:10000/v1/models",
      models: [{ id: "gpt-4o" }, { id: "gpt-4o-mini" }],
    },
  ],
};

const SAMPLE_AWF_CONFIG = {
  apiProxy: {
    models: {
      "": ["sonnet", "gpt-5"],
      mini: ["haiku", "gpt-5-mini", "gpt-5-nano"],
      sonnet: ["copilot/*sonnet*", "anthropic/*sonnet*"],
    },
  },
};

describe("awf_reflect_summary.cjs", () => {
  let module;

  beforeEach(async () => {
    vi.clearAllMocks();
    fs.mkdirSync("/tmp/gh-aw/sandbox/firewall", { recursive: true });
    fs.mkdirSync(path.dirname(REFLECT_PATH), { recursive: true });
    module = await import("./awf_reflect_summary.cjs");
  });

  afterEach(() => {
    vi.restoreAllMocks();
    if (fs.existsSync(CONFIG_PATH)) {
      fs.unlinkSync(CONFIG_PATH);
    }
    if (fs.existsSync(REFLECT_PATH)) {
      fs.unlinkSync(REFLECT_PATH);
    }
    if (fs.existsSync(LEGACY_REFLECT_PATH)) {
      fs.unlinkSync(LEGACY_REFLECT_PATH);
    }
    if (fs.existsSync(REFLECT_ARTIFACT_PATH)) {
      fs.unlinkSync(REFLECT_ARTIFACT_PATH);
    }
    if (fs.existsSync(MODELS_PATH)) {
      fs.unlinkSync(MODELS_PATH);
    }
  });

  describe("readReflectData", () => {
    it("returns null when file does not exist", () => {
      expect(module.readReflectData()).toBeNull();
    });

    it("returns null when file contains invalid JSON", () => {
      fs.writeFileSync(REFLECT_PATH, "not-json", "utf8");
      expect(module.readReflectData()).toBeNull();
    });

    it("parses and returns valid JSON", () => {
      fs.writeFileSync(REFLECT_PATH, JSON.stringify(SAMPLE_REFLECT), "utf8");
      const result = module.readReflectData();
      expect(result).not.toBeNull();
      expect(result.endpoints).toHaveLength(5);
      expect(result.models_fetch_complete).toBe(true);
    });
    it("reads legacy runner-temp data when no sandbox payload exists", () => {
      fs.writeFileSync(LEGACY_REFLECT_PATH, JSON.stringify(SAMPLE_REFLECT), "utf8");
      expect(module.readReflectData()).toEqual(SAMPLE_REFLECT);
    });
    it("prefers fresh sandbox data over legacy payloads", () => {
      fs.writeFileSync(LEGACY_REFLECT_PATH, '{"endpoints":[]}', "utf8");
      fs.writeFileSync(REFLECT_PATH, JSON.stringify(SAMPLE_REFLECT), "utf8");
      expect(module.readReflectData()).toEqual(SAMPLE_REFLECT);
    });
  });

  describe("readAWFConfigData", () => {
    it("returns null when file does not exist", () => {
      expect(module.readAWFConfigData()).toBeNull();
    });

    it("returns null when file contains invalid JSON", () => {
      fs.writeFileSync(CONFIG_PATH, "not-json", "utf8");
      expect(module.readAWFConfigData()).toBeNull();
    });

    it("parses and returns valid JSON", () => {
      fs.writeFileSync(CONFIG_PATH, JSON.stringify(SAMPLE_AWF_CONFIG), "utf8");
      expect(module.readAWFConfigData()).toEqual(SAMPLE_AWF_CONFIG);
    });
  });

  describe("formatModelList", () => {
    it("returns em-dash for null models", () => {
      expect(module.formatModelList(null, 5)).toBe("—");
    });

    it("returns em-dash for empty array", () => {
      expect(module.formatModelList([], 5)).toBe("—");
    });

    it("lists all models when count is within limit", () => {
      const result = module.formatModelList(["a", "b", "c"], 5);
      expect(result).toBe("a, b, c");
    });

    it("truncates model list at maxModels and appends overflow indicator", () => {
      const models = ["m1", "m2", "m3", "m4", "m5", "m6", "m7"];
      const result = module.formatModelList(models, 5);
      expect(result).toBe("m1, m2, m3, m4, m5 … +2 more");
    });

    it("returns all models when count equals maxModels", () => {
      const result = module.formatModelList(["x", "y"], 2);
      expect(result).toBe("x, y");
    });
  });

  describe("readRuntimeModelsData", () => {
    it("returns null when file does not exist", () => {
      expect(module.readRuntimeModelsData()).toBeNull();
    });

    it("returns null when file contains invalid JSON", () => {
      fs.writeFileSync(MODELS_PATH, "not-json", "utf8");
      expect(module.readRuntimeModelsData()).toBeNull();
    });

    it("parses and returns valid JSON", () => {
      fs.writeFileSync(MODELS_PATH, JSON.stringify(SAMPLE_RUNTIME_MODELS), "utf8");
      expect(module.readRuntimeModelsData()).toEqual(SAMPLE_RUNTIME_MODELS);
    });
  });

  describe("normalizeRuntimeModelRows", () => {
    it("normalizes endpoint payloads into sorted rows", () => {
      expect(module.normalizeRuntimeModelRows(SAMPLE_RUNTIME_MODELS)).toEqual([
        {
          endpoint: "http://api-proxy:10002/models",
          models: ["claude-sonnet-4.6", "gpt-4o"],
          provider: "copilot",
        },
        {
          endpoint: "http://api-proxy:10000/v1/models",
          models: ["gpt-4o", "gpt-4o-mini"],
          provider: "openai",
        },
      ]);
    });

    it("normalizes provider-map models.json payloads", () => {
      const providerMap = {
        providers: {
          openai: {
            models_url: "http://api-proxy:10000/v1/models",
            available_models: [{ id: "gpt-4o" }, { id: "gpt-4o-mini" }],
          },
          copilot: {
            endpoint: "http://api-proxy:10002/models",
            detected_models: ["claude-sonnet-4.6", "gpt-4o"],
          },
        },
      };

      expect(module.normalizeRuntimeModelRows(providerMap)).toEqual([
        {
          endpoint: "http://api-proxy:10002/models",
          models: ["claude-sonnet-4.6", "gpt-4o"],
          provider: "copilot",
        },
        {
          endpoint: "http://api-proxy:10000/v1/models",
          models: ["gpt-4o", "gpt-4o-mini"],
          provider: "openai",
        },
      ]);
    });

    it("normalizes single-provider models.json payloads", () => {
      const singleProvider = {
        provider: "anthropic",
        base_url: "http://api-proxy:10001/v1/models",
        models: [{ id: "claude-opus-4-1" }, { id: "claude-sonnet-4-5" }],
      };

      expect(module.normalizeRuntimeModelRows(singleProvider)).toEqual([
        {
          endpoint: "http://api-proxy:10001/v1/models",
          models: ["claude-opus-4-1", "claude-sonnet-4-5"],
          provider: "anthropic",
        },
      ]);
    });
  });

  describe("normalizeModelAliasRows", () => {
    it("normalizes alias mappings and sorts the default alias first", () => {
      expect(module.normalizeModelAliasRows(SAMPLE_AWF_CONFIG)).toEqual([
        {
          alias: "",
          label: "(default)",
          targets: ["sonnet", "gpt-5"],
        },
        {
          alias: "mini",
          label: "mini",
          targets: ["haiku", "gpt-5-mini", "gpt-5-nano"],
        },
        {
          alias: "sonnet",
          label: "sonnet",
          targets: ["copilot/*sonnet*", "anthropic/*sonnet*"],
        },
      ]);
    });
  });

  describe("buildReflectSummary", () => {
    it("produces a summary with provider table and details wrapper", () => {
      const markdown = module.buildReflectSummary(SAMPLE_REFLECT, {});

      expect(markdown).toContain("<details>");
      expect(markdown).toContain("</details>");
      expect(markdown).toContain("<summary>AWF API proxy: 3 of 5 providers configured</summary>");
      expect(markdown).toContain("| Provider | Port | Configured | Available models |");
    });

    it("marks configured providers with checkmark and unconfigured with cross", () => {
      const markdown = module.buildReflectSummary(SAMPLE_REFLECT, {});

      // openai is configured
      expect(markdown).toMatch(/\|\s*openai\s*\|\s*10000\s*\|\s*✅/);
      // anthropic is not configured
      expect(markdown).toMatch(/\|\s*anthropic\s*\|\s*10001\s*\|\s*❌/);
    });

    it("shows model list for configured providers with models", () => {
      const markdown = module.buildReflectSummary(SAMPLE_REFLECT, {});
      expect(markdown).toContain("gpt-4o");
      expect(markdown).toContain("claude-sonnet-4.6");
    });

    it("shows em-dash for providers with no models", () => {
      const markdown = module.buildReflectSummary(SAMPLE_REFLECT, {});
      // anthropic has null models — should appear as —
      expect(markdown).toMatch(/\|\s*anthropic\s*\|[^|]+\|[^|]+\|\s*—\s*\|/);
    });

    it("appends models_fetch_complete note when fetch is incomplete", () => {
      const incomplete = { ...SAMPLE_REFLECT, models_fetch_complete: false };
      const markdown = module.buildReflectSummary(incomplete, {});
      expect(markdown).toContain("model list may be incomplete");
    });

    it("does not append fetch note when models_fetch_complete is true", () => {
      const markdown = module.buildReflectSummary(SAMPLE_REFLECT, {});
      expect(markdown).not.toContain("model list may be incomplete");
    });

    it("handles empty endpoints array gracefully", () => {
      const empty = { endpoints: [], models_fetch_complete: true };
      const markdown = module.buildReflectSummary(empty, {});
      expect(markdown).toContain("No endpoint information available.");
      expect(markdown).toContain("<summary>AWF API proxy: 0 of 0 providers configured</summary>");
    });

    it("respects the maxModels option when truncating model lists", () => {
      const data = {
        endpoints: [
          {
            provider: "openai",
            port: 10000,
            configured: true,
            models: ["a", "b", "c", "d", "e", "f"],
          },
        ],
        models_fetch_complete: true,
      };
      const markdown = module.buildReflectSummary(data, { maxModels: 3 });
      expect(markdown).toContain("a, b, c … +3 more");
    });

    it("renders runtime models.json table alongside configured endpoint summary", () => {
      const markdown = module.buildReflectSummary(SAMPLE_REFLECT, { runtimeModelsData: SAMPLE_RUNTIME_MODELS });

      expect(markdown).toContain("Configured endpoints");
      expect(markdown).toContain("Runtime models.json");
      expect(markdown).toContain("| Provider | Endpoint | Available models |");
      expect(markdown).toContain("| copilot | http://api-proxy:10002/models | claude-sonnet-4.6, gpt-4o |");
      expect(markdown).toContain("| openai | http://api-proxy:10000/v1/models | gpt-4o, gpt-4o-mini |");
    });

    it("renders model aliases from awf-config.json", () => {
      const markdown = module.buildReflectSummary(SAMPLE_REFLECT, { awfConfigData: SAMPLE_AWF_CONFIG });

      expect(markdown).toContain("Model aliases");
      expect(markdown).toContain("| Alias | Resolution order |");
      expect(markdown).toContain("| (default) | sonnet, gpt-5 |");
      expect(markdown).toContain("| sonnet | copilot/*sonnet*, anthropic/*sonnet* |");
    });
  });

  describe("main", () => {
    it("logs and returns early when reflect data file is absent", async () => {
      await module.main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("not available"));
      expect(mockCore.summary.addRaw).not.toHaveBeenCalled();
    });

    it("writes step summary when reflect data file is present", async () => {
      fs.writeFileSync(CONFIG_PATH, JSON.stringify(SAMPLE_AWF_CONFIG), "utf8");
      fs.writeFileSync(REFLECT_PATH, JSON.stringify(SAMPLE_REFLECT), "utf8");
      fs.writeFileSync(MODELS_PATH, JSON.stringify(SAMPLE_RUNTIME_MODELS), "utf8");

      await module.main();

      expect(JSON.parse(fs.readFileSync(REFLECT_ARTIFACT_PATH, "utf8"))).toEqual(SAMPLE_REFLECT);
      expect(mockCore.summary.addRaw).toHaveBeenCalledTimes(1);
      const summary = mockCore.summary.addRaw.mock.calls[0][0];
      expect(summary).toContain("AWF API proxy");
      expect(summary).toContain("Model aliases");
      expect(summary).toContain("openai");
      expect(summary).toContain("Runtime models.json");
      expect(mockCore.summary.write).toHaveBeenCalledTimes(1);
      expect(mockCore.info).toHaveBeenCalledTimes(2);
      expect(mockCore.info).toHaveBeenCalledWith(summary);
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("AWF reflect summary written"));
    });

    it("writes step summary when staging the artifact fails", async () => {
      fs.writeFileSync(REFLECT_PATH, JSON.stringify(SAMPLE_REFLECT), "utf8");
      vi.spyOn(fs, "copyFileSync").mockImplementationOnce(() => {
        throw new Error("EROFS: read-only file system");
      });

      await module.main();

      expect(mockCore.summary.write).toHaveBeenCalledTimes(1);
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Unable to stage AWF reflect data"));
    });
  });
});
