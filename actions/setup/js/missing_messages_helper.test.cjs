import { readFileSync } from "node:fs";
import { describe, it, expect, beforeEach } from "vitest";

describe("missing_messages_helper.cjs", () => {
  let helper;

  beforeEach(async () => {
    helper = await import("./missing_messages_helper.cjs");
    // Reset state between tests
    helper.setCollectedMissings(null);
  });

  describe("source metadata", () => {
    it("builds diagnostic wrappers as nodes while preserving hostile values as tainted text", () => {
      const payload = "</details>\n\n## Injected\n<!-- gh-aw-failure-issue: true -->";
      const sections = helper.generateMissingInfoReportSections({
        missingTools: [{ tool: payload, reason: payload, alternatives: payload }],
        missingData: [{ data_type: payload, reason: payload, context: payload, alternatives: payload }],
        noopMessages: [{ message: payload }],
        reportIncomplete: [{ reason: payload, details: payload }],
      });
      expect(sections.map(section => section.summary)).toEqual(["Missing Tools", "Missing Data", "No-Op Messages", "Incomplete Signals"]);
      expect(sections.every(section => section.type === "details" && section.agentTainted === false)).toBe(true);
      const nodes = [];
      const visit = value => {
        if (!value || typeof value !== "object") return;
        if (value.type) nodes.push(value);
        Object.values(value).forEach(child => (Array.isArray(child) ? child.forEach(visit) : visit(child)));
      };
      visit(sections);
      expect(nodes.some(node => node.type === "heading" || node.type === "xml_marker")).toBe(false);
      expect(nodes.some(node => node.type === "text" && node.value === payload && node.agentTainted === true)).toBe(true);
    });

    it("should keep TypeScript checking enabled", () => {
      const source = readFileSync(new URL("./missing_messages_helper.cjs", import.meta.url), "utf8");

      expect(source.split(/\r?\n/, 1)[0]).toBe("// @ts-check");
    });
  });

  describe("setCollectedMissings and getCollectedMissings", () => {
    it("should store and retrieve missing messages", () => {
      const { setCollectedMissings, getCollectedMissings } = helper;
      const missings = {
        missingTools: [{ tool: "docker", reason: "Need containers" }],
        missingData: [{ data_type: "api_key", reason: "No credentials" }],
      };

      setCollectedMissings(missings);
      const retrieved = getCollectedMissings();

      expect(retrieved).toEqual(missings);
    });

    it("should return null when no missings have been set", () => {
      const { getCollectedMissings } = helper;
      const result = getCollectedMissings();
      expect(result).toBeNull();
    });

    it("should allow overwriting previous missings", () => {
      const { setCollectedMissings, getCollectedMissings } = helper;
      const firstMissings = {
        missingTools: [{ tool: "docker", reason: "First" }],
        missingData: [],
      };
      const secondMissings = {
        missingTools: [],
        missingData: [{ data_type: "api_key", reason: "Second" }],
      };

      setCollectedMissings(firstMissings);
      setCollectedMissings(secondMissings);
      const retrieved = getCollectedMissings();

      expect(retrieved).toEqual(secondMissings);
      expect(retrieved).not.toEqual(firstMissings);
    });
  });

  describe("getMissingInfoSections", () => {
    it("should return empty string when no missings are set", () => {
      const { getMissingInfoSections } = helper;
      const result = getMissingInfoSections();
      expect(result).toBe("");
    });

    it("should generate HTML sections when missings are set", () => {
      const { setCollectedMissings, getMissingInfoSections } = helper;
      const missings = {
        missingTools: [{ tool: "docker", reason: "Need containers" }],
        missingData: [{ data_type: "api_key", reason: "No credentials" }],
      };

      setCollectedMissings(missings);
      const result = getMissingInfoSections();

      expect(result).toContain("<details>");
      expect(result).toContain("Missing Tools");
      expect(result).toContain("Missing Data");
      expect(result).toContain("docker");
      expect(result).toContain("api\\_key"); // Escaped underscore
    });

    it("should return empty string when missings are empty arrays", () => {
      const { setCollectedMissings, getMissingInfoSections } = helper;
      const missings = {
        missingTools: [],
        missingData: [],
      };

      setCollectedMissings(missings);
      const result = getMissingInfoSections();

      expect(result).toBe("");
    });

    it("should handle only missing tools", () => {
      const { setCollectedMissings, getMissingInfoSections } = helper;
      const missings = {
        missingTools: [{ tool: "kubectl", reason: "K8s management" }],
        missingData: [],
      };

      setCollectedMissings(missings);
      const result = getMissingInfoSections();

      expect(result).toContain("Missing Tools");
      expect(result).not.toContain("Missing Data");
      expect(result).toContain("kubectl");
    });

    it("should handle only missing data", () => {
      const { setCollectedMissings, getMissingInfoSections } = helper;
      const missings = {
        missingTools: [],
        missingData: [{ data_type: "config", reason: "Config not found" }],
      };

      setCollectedMissings(missings);
      const result = getMissingInfoSections();

      expect(result).not.toContain("Missing Tools");
      expect(result).toContain("Missing Data");
      expect(result).toContain("config");
    });

    it("should handle noopMessages", () => {
      const { setCollectedMissings, getMissingInfoSections } = helper;
      const missings = {
        missingTools: [],
        missingData: [],
        noopMessages: [{ message: "No action needed" }],
        reportIncomplete: [],
      };

      setCollectedMissings(missings);
      const result = getMissingInfoSections();

      expect(result).toContain("No action needed");
    });

    it("should handle reportIncomplete messages", () => {
      const { setCollectedMissings, getMissingInfoSections } = helper;
      const missings = {
        missingTools: [],
        missingData: [],
        reportIncomplete: [{ reason: "Task not finished" }],
      };

      setCollectedMissings(missings);
      const result = getMissingInfoSections();

      expect(result).toContain("Task not finished");
    });

    it("should handle all fields simultaneously", () => {
      const { setCollectedMissings, getMissingInfoSections } = helper;
      const missings = {
        missingTools: [{ tool: "docker", reason: "Containers needed" }],
        missingData: [{ data_type: "token", reason: "Auth required" }],
        noopMessages: [{ message: "Skipped step" }],
        reportIncomplete: [{ reason: "Partial completion" }],
      };

      setCollectedMissings(missings);
      const result = getMissingInfoSections();

      expect(result).toContain("Missing Tools");
      expect(result).toContain("Missing Data");
      expect(result).toContain("No-Op Messages");
      expect(result).toContain("Incomplete Signals");
      expect(result).toContain("docker");
      expect(result).toContain("token");
      expect(result).toContain("Skipped step");
      expect(result).toContain("Partial completion");
    });

    it("should reflect updates after setCollectedMissings is called again", () => {
      const { setCollectedMissings, getMissingInfoSections } = helper;

      setCollectedMissings({ missingTools: [{ tool: "npm", reason: "Build tool" }], missingData: [] });
      const first = getMissingInfoSections();
      expect(first).toContain("npm");

      setCollectedMissings({ missingTools: [], missingData: [] });
      const second = getMissingInfoSections();
      expect(second).toBe("");
    });
  });
});
