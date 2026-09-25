import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { fileURLToPath } from "url";
import path from "path";

// Mock the global objects that GitHub Actions provides
const mockCore = {
  debug: vi.fn(),
  info: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  setFailed: vi.fn(),
  setOutput: vi.fn(),
  summary: {
    addRaw: vi.fn().mockReturnThis(),
    write: vi.fn().mockResolvedValue(),
  },
};

const mockContext = {
  runId: 12345,
  repo: {
    owner: "testowner",
    repo: "testrepo",
  },
  payload: {
    repository: {
      html_url: "https://github.com/testowner/testrepo",
    },
  },
};

// Set up global mocks before importing the module
global.core = mockCore;
global.context = mockContext;

describe("generate_footer.cjs", () => {
  let generateXMLMarker;
  let generateWorkflowIdMarker;
  let generateWorkflowCallIdMarker;
  let generateWorkflowCallIdReviewMarker;
  let getWorkflowIdMarkerContent;
  let normalizeCloseOlderKey;

  beforeEach(async () => {
    // Reset mocks
    vi.clearAllMocks();
    // Clear env vars
    delete process.env.GH_AW_ENGINE_ID;
    delete process.env.GH_AW_ENGINE_VERSION;
    delete process.env.GH_AW_ENGINE_MODEL;
    delete process.env.GH_AW_TRACKER_ID;
    delete process.env.GH_AW_WORKFLOW_ID;
    delete process.env.GITHUB_RUN_ID;
    delete process.env.GH_AW_DETECTION_CONCLUSION;
    delete process.env.GH_AW_DETECTION_REASON;

    // Dynamic import to get fresh module state
    const module = await import("./generate_footer.cjs");
    generateXMLMarker = module.generateXMLMarker;
    generateWorkflowIdMarker = module.generateWorkflowIdMarker;
    generateWorkflowCallIdMarker = module.generateWorkflowCallIdMarker;
    generateWorkflowCallIdReviewMarker = module.generateWorkflowCallIdReviewMarker;
    getWorkflowIdMarkerContent = module.getWorkflowIdMarkerContent;
    normalizeCloseOlderKey = module.normalizeCloseOlderKey;
  });

  describe("generateXMLMarker", () => {
    it("should generate basic XML marker with workflow name and run URL", () => {
      const result = generateXMLMarker("Test Workflow", "https://github.com/test/repo/actions/runs/123");

      expect(result).toBe("<!-- gh-aw-agentic-workflow: Test Workflow, run: https://github.com/test/repo/actions/runs/123 -->");
    });

    it("should include engine ID when env var is set", async () => {
      process.env.GH_AW_ENGINE_ID = "copilot";

      vi.resetModules();
      const freshModule = await import("./generate_footer.cjs");

      const result = freshModule.generateXMLMarker("Test Workflow", "https://github.com/test/repo/actions/runs/123");

      expect(result).toBe("<!-- gh-aw-agentic-workflow: Test Workflow, engine: copilot, run: https://github.com/test/repo/actions/runs/123 -->");
    });

    it("should include engine version when env var is set", async () => {
      process.env.GH_AW_ENGINE_ID = "copilot";
      process.env.GH_AW_ENGINE_VERSION = "2.0.0";

      vi.resetModules();
      const freshModule = await import("./generate_footer.cjs");

      const result = freshModule.generateXMLMarker("Test Workflow", "https://github.com/test/repo/actions/runs/123");

      expect(result).toBe("<!-- gh-aw-agentic-workflow: Test Workflow, engine: copilot, version: 2.0.0, run: https://github.com/test/repo/actions/runs/123 -->");
    });

    it("should include all engine metadata when all env vars are set", async () => {
      process.env.GH_AW_ENGINE_ID = "copilot";
      process.env.GH_AW_ENGINE_VERSION = "1.0.0";
      process.env.GH_AW_ENGINE_MODEL = "gpt-5";

      vi.resetModules();
      const freshModule = await import("./generate_footer.cjs");

      const result = freshModule.generateXMLMarker("Test Workflow", "https://github.com/test/repo/actions/runs/123");

      expect(result).toBe("<!-- gh-aw-agentic-workflow: Test Workflow, engine: copilot, version: 1.0.0, model: gpt-5, run: https://github.com/test/repo/actions/runs/123 -->");
    });

    it("should handle special characters in workflow name", async () => {
      const result = generateXMLMarker("Test Workflow (v2) [beta]", "https://github.com/test/repo/actions/runs/123");

      expect(result).toContain("gh-aw-agentic-workflow: Test Workflow (v2) [beta]");
    });

    it("should include tracker-id when env var is set", async () => {
      process.env.GH_AW_TRACKER_ID = "my-tracker-12345";

      vi.resetModules();
      const freshModule = await import("./generate_footer.cjs");

      const result = freshModule.generateXMLMarker("Test Workflow", "https://github.com/test/repo/actions/runs/123");

      expect(result).toBe("<!-- gh-aw-agentic-workflow: Test Workflow, gh-aw-tracker-id: my-tracker-12345, run: https://github.com/test/repo/actions/runs/123 -->");
    });

    it("should include tracker-id with engine metadata when all env vars are set", async () => {
      process.env.GH_AW_ENGINE_ID = "copilot";
      process.env.GH_AW_ENGINE_VERSION = "1.0.0";
      process.env.GH_AW_ENGINE_MODEL = "gpt-5";
      process.env.GH_AW_TRACKER_ID = "workflow-2024-q1";

      vi.resetModules();
      const freshModule = await import("./generate_footer.cjs");

      const result = freshModule.generateXMLMarker("Test Workflow", "https://github.com/test/repo/actions/runs/123");

      expect(result).toBe("<!-- gh-aw-agentic-workflow: Test Workflow, gh-aw-tracker-id: workflow-2024-q1, engine: copilot, version: 1.0.0, model: gpt-5, run: https://github.com/test/repo/actions/runs/123 -->");
    });

    it("should include run id when GITHUB_RUN_ID env var is set", async () => {
      process.env.GITHUB_RUN_ID = "9876543210";

      vi.resetModules();
      const freshModule = await import("./generate_footer.cjs");

      const result = freshModule.generateXMLMarker("Test Workflow", "https://github.com/test/repo/actions/runs/9876543210");

      expect(result).toBe("<!-- gh-aw-agentic-workflow: Test Workflow, id: 9876543210, run: https://github.com/test/repo/actions/runs/9876543210 -->");
    });

    it("should include workflow_id when GH_AW_WORKFLOW_ID env var is set", async () => {
      process.env.GH_AW_WORKFLOW_ID = "smoke-copilot";

      vi.resetModules();
      const freshModule = await import("./generate_footer.cjs");

      const result = freshModule.generateXMLMarker("Test Workflow", "https://github.com/test/repo/actions/runs/123");

      expect(result).toBe("<!-- gh-aw-agentic-workflow: Test Workflow, workflow_id: smoke-copilot, run: https://github.com/test/repo/actions/runs/123 -->");
    });

    it("should include all identifiers when all standard env vars are set", async () => {
      process.env.GH_AW_ENGINE_ID = "copilot";
      process.env.GH_AW_TRACKER_ID = "tracker-abc";
      process.env.GITHUB_RUN_ID = "12345";
      process.env.GH_AW_WORKFLOW_ID = "my-workflow";

      vi.resetModules();
      const freshModule = await import("./generate_footer.cjs");

      const result = freshModule.generateXMLMarker("My Workflow", "https://github.com/test/repo/actions/runs/12345");

      expect(result).toBe("<!-- gh-aw-agentic-workflow: My Workflow, gh-aw-tracker-id: tracker-abc, engine: copilot, id: 12345, workflow_id: my-workflow, run: https://github.com/test/repo/actions/runs/12345 -->");
    });
  });

  describe("generateWorkflowIdMarker", () => {
    it("should generate workflow-id XML comment marker", () => {
      const result = generateWorkflowIdMarker("test-workflow");

      expect(result).toBe("<!-- gh-aw-workflow-id: test-workflow -->");
    });

    it("should handle workflow IDs with special characters", () => {
      const result = generateWorkflowIdMarker("daily-report-v2");

      expect(result).toBe("<!-- gh-aw-workflow-id: daily-report-v2 -->");
    });

    it("should handle workflow IDs with spaces", () => {
      const result = generateWorkflowIdMarker("my workflow");

      expect(result).toBe("<!-- gh-aw-workflow-id: my workflow -->");
    });

    it("should handle empty workflow ID", () => {
      const result = generateWorkflowIdMarker("");

      expect(result).toBe("<!-- gh-aw-workflow-id:  -->");
    });

    it("should be consistent with format used in comments", () => {
      const workflowId = "smoke-copilot";
      const result = generateWorkflowIdMarker(workflowId);

      // Should match the format: <!-- gh-aw-workflow-id: {id} -->
      expect(result).toMatch(/^<!-- gh-aw-workflow-id: .+ -->$/);
      expect(result).toContain(workflowId);
    });
  });

  describe("generateWorkflowCallIdMarker", () => {
    it("should generate workflow-call-id XML comment marker", () => {
      const result = generateWorkflowCallIdMarker("owner/repo/CallerWorkflow");

      expect(result).toBe("<!-- gh-aw-workflow-call-id: owner/repo/CallerWorkflow -->");
    });

    describe("generateWorkflowCallIdReviewMarker", () => {
      it("should generate an encoded non-rendered marker", () => {
        const result = generateWorkflowCallIdReviewMarker('owner/repo/Workflow "with" spaces');

        expect(result).toBe('[gh-aw-workflow-call-id]: # "owner%2Frepo%2FWorkflow%20%22with%22%20spaces"');
      });
    });

    it("should handle caller IDs with slashes (repo/workflow format)", () => {
      const result = generateWorkflowCallIdMarker("elastic/ai-github-actions/Explore: Live Elasticsearch");

      expect(result).toBe("<!-- gh-aw-workflow-call-id: elastic/ai-github-actions/Explore: Live Elasticsearch -->");
    });

    it("should handle empty caller ID", () => {
      const result = generateWorkflowCallIdMarker("");

      expect(result).toBe("<!-- gh-aw-workflow-call-id:  -->");
    });

    it("should produce a different marker than generateWorkflowIdMarker", () => {
      const id = "test-workflow";
      const callIdMarker = generateWorkflowCallIdMarker(id);
      const workflowIdMarker = generateWorkflowIdMarker(id);

      expect(callIdMarker).not.toBe(workflowIdMarker);
      expect(callIdMarker).toContain("gh-aw-workflow-call-id:");
      expect(workflowIdMarker).toContain("gh-aw-workflow-id:");
    });

    it("should follow XML comment format", () => {
      const callerId = "owner/repo/MyWorkflow";
      const result = generateWorkflowCallIdMarker(callerId);

      expect(result).toMatch(/^<!-- gh-aw-workflow-call-id: .+ -->$/);
      expect(result).toContain(callerId);
    });
  });

  describe("getWorkflowIdMarkerContent", () => {
    it("should return marker content without XML wrapper", () => {
      const result = getWorkflowIdMarkerContent("test-workflow");

      expect(result).toBe("gh-aw-workflow-id: test-workflow");
      expect(result).not.toContain("<!--");
      expect(result).not.toContain("-->");
    });

    it("should handle workflow IDs with special characters", () => {
      const result = getWorkflowIdMarkerContent("daily-report-v2");

      expect(result).toBe("gh-aw-workflow-id: daily-report-v2");
    });

    it("should be usable in search queries", () => {
      const workflowId = "smoke-copilot";
      const markerContent = getWorkflowIdMarkerContent(workflowId);

      // Should be the format used in search: gh-aw-workflow-id: {id}
      expect(markerContent).toBe(`gh-aw-workflow-id: ${workflowId}`);

      // Should not contain XML comment markers
      expect(markerContent).not.toContain("<!--");
      expect(markerContent).not.toContain("-->");
    });

    it("should match the content inside generateWorkflowIdMarker", () => {
      const workflowId = "test-workflow";
      const fullMarker = generateWorkflowIdMarker(workflowId);
      const content = getWorkflowIdMarkerContent(workflowId);

      // The full marker should contain the content
      expect(fullMarker).toContain(content);

      // The full marker should wrap the content in XML comments
      expect(fullMarker).toBe(`<!-- ${content} -->`);
    });

    it("should handle empty workflow ID", () => {
      const result = getWorkflowIdMarkerContent("");

      expect(result).toBe("gh-aw-workflow-id: ");
    });
  });

  describe("normalizeCloseOlderKey", () => {
    it("should return an already-valid identifier unchanged", () => {
      expect(normalizeCloseOlderKey("my-key")).toBe("my-key");
      expect(normalizeCloseOlderKey("smoke_copilot")).toBe("smoke_copilot");
      expect(normalizeCloseOlderKey("abc123")).toBe("abc123");
    });

    it("should convert to lowercase", () => {
      expect(normalizeCloseOlderKey("MyKey")).toBe("mykey");
      expect(normalizeCloseOlderKey("SMOKE-COPILOT")).toBe("smoke-copilot");
    });

    it("should replace spaces with dashes", () => {
      expect(normalizeCloseOlderKey("my key")).toBe("my-key");
      expect(normalizeCloseOlderKey("hello world foo")).toBe("hello-world-foo");
    });

    it("should replace special characters with dashes", () => {
      expect(normalizeCloseOlderKey("My Key!")).toBe("my-key");
      expect(normalizeCloseOlderKey("foo@bar#baz")).toBe("foo-bar-baz");
    });

    it("should collapse multiple consecutive dashes into one", () => {
      expect(normalizeCloseOlderKey("a  b")).toBe("a-b");
      expect(normalizeCloseOlderKey("foo---bar")).toBe("foo-bar");
    });

    it("should trim leading and trailing dashes and underscores", () => {
      expect(normalizeCloseOlderKey("  hello  ")).toBe("hello");
      expect(normalizeCloseOlderKey("!hello!")).toBe("hello");
      expect(normalizeCloseOlderKey("-foo-")).toBe("foo");
    });

    it("should return empty string for whitespace-only input", () => {
      expect(normalizeCloseOlderKey("   ")).toBe("");
      expect(normalizeCloseOlderKey("\t\n")).toBe("");
    });

    it("should return empty string for input with only special characters", () => {
      expect(normalizeCloseOlderKey("!!!")).toBe("");
      expect(normalizeCloseOlderKey("@#$%")).toBe("");
    });
  });

  describe("generateExpiredEntityFooter", () => {
    let generateExpiredEntityFooter;
    let getExpiredEntityCautionAlert;
    let originalPromptsDir;

    beforeEach(async () => {
      // Point GH_AW_PROMPTS_DIR to the source md/ directory so getPromptPath()
      // resolves to the local templates. This is needed because RUNNER_TEMP may be
      // set but the runtime prompts directory is not populated in the test environment.
      originalPromptsDir = process.env.GH_AW_PROMPTS_DIR;
      process.env.GH_AW_PROMPTS_DIR = path.join(path.dirname(fileURLToPath(import.meta.url)), "../md");
      // Reset modules and import fresh
      vi.resetModules();
      const freshModule = await import("./generate_footer.cjs");
      generateExpiredEntityFooter = freshModule.generateExpiredEntityFooter;
      getExpiredEntityCautionAlert = freshModule.getExpiredEntityCautionAlert;
    });

    afterEach(() => {
      if (originalPromptsDir !== undefined) {
        process.env.GH_AW_PROMPTS_DIR = originalPromptsDir;
      } else {
        delete process.env.GH_AW_PROMPTS_DIR;
      }
    });

    it("should generate footer with 'Closed by' wording and workflow link", () => {
      const result = generateExpiredEntityFooter("Test Workflow", "https://github.com/test/repo/actions/runs/123", "test-workflow");

      expect(result).toContain("> Closed by [Test Workflow](https://github.com/test/repo/actions/runs/123)");
    });

    it("should use markdown quote for footer text", () => {
      const result = generateExpiredEntityFooter("Test Workflow", "https://github.com/test/repo/actions/runs/123", "test-workflow");

      expect(result).toMatch(/\n\n> Closed by/);
    });

    it("should include gh-aw-expired-comments marker", () => {
      const result = generateExpiredEntityFooter("Test Workflow", "https://github.com/test/repo/actions/runs/123", "test-workflow");

      expect(result).toContain("<!-- gh-aw-expired-comments -->");
    });

    it("should include workflow ID marker when provided", () => {
      const result = generateExpiredEntityFooter("Test Workflow", "https://github.com/test/repo/actions/runs/123", "test-workflow");

      expect(result).toContain("<!-- gh-aw-workflow-id: test-workflow -->");
    });

    it("should omit workflow ID marker when not provided", () => {
      const result = generateExpiredEntityFooter("Test Workflow", "https://github.com/test/repo/actions/runs/123", "");

      expect(result).not.toContain("<!-- gh-aw-workflow-id:");
    });

    it("should include XML marker with workflow metadata", () => {
      const result = generateExpiredEntityFooter("Test Workflow", "https://github.com/test/repo/actions/runs/123", "test-workflow");

      expect(result).toContain("<!-- gh-aw-agentic-workflow: Test Workflow");
      expect(result).toContain("run: https://github.com/test/repo/actions/runs/123");
    });

    it("should have correct structure with newlines and workflow ID", () => {
      const result = generateExpiredEntityFooter("Test Workflow", "https://github.com/test/repo/actions/runs/123", "test-workflow");

      // Should start with double newline and quote
      expect(result.startsWith("\n\n>")).toBe(true);
      // Should have proper spacing between sections
      expect(result).toMatch(/\n\n<!-- gh-aw-expired-comments -->\n<!-- gh-aw-workflow-id: test-workflow -->\n<!-- gh-aw-agentic-workflow:/);
    });

    it("should include engine metadata when env vars are set", async () => {
      process.env.GH_AW_ENGINE_ID = "copilot";
      process.env.GH_AW_ENGINE_VERSION = "2.0.0";
      process.env.GH_AW_ENGINE_MODEL = "gpt-5";

      vi.resetModules();
      const freshModule = await import("./generate_footer.cjs");
      const freshGenerateExpiredEntityFooter = freshModule.generateExpiredEntityFooter;

      const result = freshGenerateExpiredEntityFooter("Test Workflow", "https://github.com/test/repo/actions/runs/123", "test-workflow");

      expect(result).toContain("engine: copilot");
      expect(result).toContain("version: 2.0.0");
      expect(result).toContain("model: gpt-5");
    });

    it("should include tracker-id when env var is set", async () => {
      process.env.GH_AW_TRACKER_ID = "test-tracker-123";

      vi.resetModules();
      const freshModule = await import("./generate_footer.cjs");
      const freshGenerateExpiredEntityFooter = freshModule.generateExpiredEntityFooter;

      const result = freshGenerateExpiredEntityFooter("Test Workflow", "https://github.com/test/repo/actions/runs/123", "test-workflow");

      expect(result).toContain("gh-aw-tracker-id: test-tracker-123");
    });

    it("should be searchable by gh-aw-expired-comments marker", () => {
      const result = generateExpiredEntityFooter("Test Workflow", "https://github.com/test/repo/actions/runs/123", "test-workflow");

      // The marker should be searchable in GitHub
      expect(result).toContain("<!-- gh-aw-expired-comments -->");
      // Should be a standalone marker (not embedded in another marker)
      const markerMatches = result.match(/<!-- gh-aw-expired-comments -->/g);
      expect(markerMatches?.length).toBe(1);
    });

    it("should be searchable by workflow ID marker", () => {
      const result = generateExpiredEntityFooter("Test Workflow", "https://github.com/test/repo/actions/runs/123", "daily-cleanup");

      // The marker should be searchable in GitHub
      expect(result).toContain("<!-- gh-aw-workflow-id: daily-cleanup -->");
      // Should be a standalone marker (not embedded in another marker)
      const markerMatches = result.match(/<!-- gh-aw-workflow-id: daily-cleanup -->/g);
      expect(markerMatches?.length).toBe(1);
    });

    it("should NOT include caution alert (caution is now injected by callers at the top)", () => {
      process.env.GH_AW_DETECTION_CONCLUSION = "warning";
      process.env.GH_AW_DETECTION_REASON = "threat_detected";

      const result = generateExpiredEntityFooter("Test Workflow", "https://github.com/test/repo/actions/runs/123", "test-workflow");

      expect(result).not.toContain("> [!CAUTION]");
      expect(result).toContain("> Closed by [Test Workflow]");
    });

    it("should not include caution alert when detection conclusion is not warning", () => {
      process.env.GH_AW_DETECTION_CONCLUSION = "success";

      const result = generateExpiredEntityFooter("Test Workflow", "https://github.com/test/repo/actions/runs/123", "test-workflow");

      expect(result).not.toContain("> [!CAUTION]");
      expect(result).toContain("> Closed by [Test Workflow]");
    });

    it("should not include caution alert when detection conclusion is not set", () => {
      const result = generateExpiredEntityFooter("Test Workflow", "https://github.com/test/repo/actions/runs/123", "test-workflow");

      expect(result).not.toContain("> [!CAUTION]");
      expect(result).toContain("> Closed by [Test Workflow]");
    });

    describe("getExpiredEntityCautionAlert", () => {
      it("should return caution alert when detection conclusion is warning", () => {
        process.env.GH_AW_DETECTION_CONCLUSION = "warning";
        process.env.GH_AW_DETECTION_REASON = "threat_detected";

        const result = getExpiredEntityCautionAlert("Test Workflow", "https://github.com/test/repo/actions/runs/123");

        expect(result).toContain("> [!CAUTION]");
        expect(result).toContain("agentic threat detected");
        expect(result).toContain("<!-- gh-aw-threat-detected -->");
        expect(result).toContain("Potential security threats were detected");
      });

      it("should return warning alert for agent_failure (tooling failure, not security finding)", () => {
        process.env.GH_AW_DETECTION_CONCLUSION = "warning";
        process.env.GH_AW_DETECTION_REASON = "agent_failure";

        const result = getExpiredEntityCautionAlert("Test Workflow", "https://github.com/test/repo/actions/runs/123");

        expect(result).toContain("> [!WARNING]");
        expect(result).toContain("**Threat Detection Engine Failure**");
        expect(result).toContain("What happened");
        expect(result).toContain("<!-- gh-aw-threat-engine-error -->");
        expect(result).not.toContain("<!-- gh-aw-threat-detected -->");
        expect(result).not.toContain("> [!CAUTION]");
        expect(result).not.toContain("agentic threat detected");
        expect(result).toContain("failed to produce results");
      });

      it("should return warning alert for parse_error (tooling failure, not security finding)", () => {
        process.env.GH_AW_DETECTION_CONCLUSION = "warning";
        process.env.GH_AW_DETECTION_REASON = "parse_error";

        const result = getExpiredEntityCautionAlert("Test Workflow", "https://github.com/test/repo/actions/runs/123");

        expect(result).toContain("> [!WARNING]");
        expect(result).toContain("**Threat Detection Engine Failure**");
        expect(result).toContain("What happened");
        expect(result).toContain("<!-- gh-aw-threat-engine-error -->");
        expect(result).not.toContain("<!-- gh-aw-threat-detected -->");
        expect(result).not.toContain("> [!CAUTION]");
        expect(result).not.toContain("agentic threat detected");
        expect(result).toContain("could not be parsed");
      });

      it("should return empty string when detection conclusion is not warning", () => {
        process.env.GH_AW_DETECTION_CONCLUSION = "success";

        const result = getExpiredEntityCautionAlert("Test Workflow", "https://github.com/test/repo/actions/runs/123");

        expect(result).toBe("");
      });

      it("should return empty string when detection conclusion is not set", () => {
        const result = getExpiredEntityCautionAlert("Test Workflow", "https://github.com/test/repo/actions/runs/123");

        expect(result).toBe("");
      });
    });
  });
});
