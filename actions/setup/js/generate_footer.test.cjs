import { describe, it, expect, beforeEach, vi } from "vitest";

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
  let getWorkflowIdMarkerContent;

  beforeEach(async () => {
    // Reset mocks
    vi.clearAllMocks();
    // Clear env vars
    delete process.env.GH_AW_ENGINE_ID;
    delete process.env.GH_AW_ENGINE_VERSION;
    delete process.env.GH_AW_ENGINE_MODEL;
    delete process.env.GH_AW_TRACKER_ID;
    delete process.env.GH_AW_WORKFLOW_ID;

    // Dynamic import to get fresh module state
    const module = await import("./generate_footer.cjs");
    generateXMLMarker = module.generateXMLMarker;
    generateWorkflowIdMarker = module.generateWorkflowIdMarker;
    getWorkflowIdMarkerContent = module.getWorkflowIdMarkerContent;
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

  describe("generateExpiredEntityFooter", () => {
    let generateExpiredEntityFooter;

    beforeEach(async () => {
      // Reset modules and import fresh
      vi.resetModules();
      const freshModule = await import("./generate_footer.cjs");
      generateExpiredEntityFooter = freshModule.generateExpiredEntityFooter;
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
  });
});
