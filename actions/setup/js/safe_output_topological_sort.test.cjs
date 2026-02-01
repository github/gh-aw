// @ts-check
import { describe, it, expect, beforeEach, vi } from "vitest";

// Mock core for logging
const mockCore = {
  info: vi.fn(),
  warning: vi.fn(),
  debug: vi.fn(),
};
global.core = mockCore;

describe("safe_output_topological_sort.cjs", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe("extractTemporaryIdReferences", () => {
    it("should extract temporary IDs from body field", async () => {
      const { extractTemporaryIdReferences } = await import("./safe_output_topological_sort.cjs");

      const message = {
        type: "create_issue",
        title: "Test Issue",
        body: "See #aw_abc123def456 and #aw_111222333444 for details",
      };

      const refs = extractTemporaryIdReferences(message);

      expect(refs.size).toBe(2);
      expect(refs.has("aw_abc123def456")).toBe(true);
      expect(refs.has("aw_111222333444")).toBe(true);
    });

    it("should extract temporary IDs from title field", async () => {
      const { extractTemporaryIdReferences } = await import("./safe_output_topological_sort.cjs");

      const message = {
        type: "create_issue",
        title: "Follow up to #aw_abc123def456",
        body: "Details here",
      };

      const refs = extractTemporaryIdReferences(message);

      expect(refs.size).toBe(1);
      expect(refs.has("aw_abc123def456")).toBe(true);
    });

    it("should extract temporary IDs from direct ID fields", async () => {
      const { extractTemporaryIdReferences } = await import("./safe_output_topological_sort.cjs");

      const message = {
        type: "link_sub_issue",
        parent_issue_number: "aw_aaaaaa123456",
        sub_issue_number: "aw_bbbbbb123456",
      };

      const refs = extractTemporaryIdReferences(message);

      expect(refs.size).toBe(2);
      expect(refs.has("aw_aaaaaa123456")).toBe(true);
      expect(refs.has("aw_bbbbbb123456")).toBe(true);
    });

    it("should handle # prefix in ID fields", async () => {
      const { extractTemporaryIdReferences } = await import("./safe_output_topological_sort.cjs");

      const message = {
        type: "add_comment",
        issue_number: "#aw_abc123def456",
        body: "Comment text",
      };

      const refs = extractTemporaryIdReferences(message);

      expect(refs.size).toBe(1);
      expect(refs.has("aw_abc123def456")).toBe(true);
    });

    it("should normalize temporary IDs to lowercase", async () => {
      const { extractTemporaryIdReferences } = await import("./safe_output_topological_sort.cjs");

      const message = {
        type: "create_issue",
        body: "See #AW_ABC123DEF456",
      };

      const refs = extractTemporaryIdReferences(message);

      expect(refs.size).toBe(1);
      expect(refs.has("aw_abc123def456")).toBe(true);
    });

    it("should extract from items array for bulk operations", async () => {
      const { extractTemporaryIdReferences } = await import("./safe_output_topological_sort.cjs");

      const message = {
        type: "add_comment",
        items: [
          { issue_number: "aw_dddddd111111", body: "Comment 1" },
          { issue_number: "aw_eeeeee222222", body: "Comment 2" },
        ],
      };

      const refs = extractTemporaryIdReferences(message);

      expect(refs.size).toBe(2);
      expect(refs.has("aw_dddddd111111")).toBe(true);
      expect(refs.has("aw_eeeeee222222")).toBe(true);
    });

    it("should return empty set for messages without temp IDs", async () => {
      const { extractTemporaryIdReferences } = await import("./safe_output_topological_sort.cjs");

      const message = {
        type: "create_issue",
        title: "Regular Issue",
        body: "No temporary IDs here",
      };

      const refs = extractTemporaryIdReferences(message);

      expect(refs.size).toBe(0);
    });

    it("should ignore invalid temporary ID formats", async () => {
      const { extractTemporaryIdReferences } = await import("./safe_output_topological_sort.cjs");

      const message = {
        type: "create_issue",
        body: "Invalid: #aw_short #aw_toolongxxxxxxxx #temp_123456789012",
      };

      const refs = extractTemporaryIdReferences(message);

      expect(refs.size).toBe(0);
    });
  });

  describe("getCreatedTemporaryId", () => {
    it("should return temporary_id when present and valid", async () => {
      const { getCreatedTemporaryId } = await import("./safe_output_topological_sort.cjs");

      const message = {
        type: "create_issue",
        temporary_id: "aw_abc123def456",
        title: "Test",
      };

      const created = getCreatedTemporaryId(message);

      expect(created).toBe("aw_abc123def456");
    });

    it("should normalize created temporary ID to lowercase", async () => {
      const { getCreatedTemporaryId } = await import("./safe_output_topological_sort.cjs");

      const message = {
        type: "create_issue",
        temporary_id: "AW_ABC123DEF456",
        title: "Test",
      };

      const created = getCreatedTemporaryId(message);

      expect(created).toBe("aw_abc123def456");
    });

    it("should return null when temporary_id is missing", async () => {
      const { getCreatedTemporaryId } = await import("./safe_output_topological_sort.cjs");

      const message = {
        type: "create_issue",
        title: "Test",
      };

      const created = getCreatedTemporaryId(message);

      expect(created).toBe(null);
    });

    it("should return null when temporary_id is invalid", async () => {
      const { getCreatedTemporaryId } = await import("./safe_output_topological_sort.cjs");

      const message = {
        type: "create_issue",
        temporary_id: "invalid_id",
        title: "Test",
      };

      const created = getCreatedTemporaryId(message);

      expect(created).toBe(null);
    });
  });

  describe("buildDependencyGraph", () => {
    it("should build graph with simple dependency", async () => {
      const { buildDependencyGraph } = await import("./safe_output_topological_sort.cjs");

      const messages = [
        { type: "create_issue", temporary_id: "aw_dddddd111111", title: "Parent" },
        { type: "add_comment", issue_number: "aw_dddddd111111", body: "Comment" },
      ];

      const { dependencies, providers } = buildDependencyGraph(messages);

      expect(providers.size).toBe(1);
      expect(providers.get("aw_dddddd111111")).toBe(0);

      expect(dependencies.get(0).size).toBe(0); // Message 0 has no dependencies
      expect(dependencies.get(1).size).toBe(1); // Message 1 depends on message 0
      expect(dependencies.get(1).has(0)).toBe(true);
    });

    it("should build graph with chain of dependencies", async () => {
      const { buildDependencyGraph } = await import("./safe_output_topological_sort.cjs");

      const messages = [
        { type: "create_issue", temporary_id: "aw_dddddd111111", title: "First" },
        { type: "create_issue", temporary_id: "aw_eeeeee222222", body: "Ref #aw_dddddd111111" },
        { type: "create_issue", temporary_id: "aw_ffffff333333", body: "Ref #aw_eeeeee222222" },
      ];

      const { dependencies, providers } = buildDependencyGraph(messages);

      expect(providers.size).toBe(3);

      expect(dependencies.get(0).size).toBe(0); // No dependencies
      expect(dependencies.get(1).size).toBe(1); // Depends on 0
      expect(dependencies.get(1).has(0)).toBe(true);
      expect(dependencies.get(2).size).toBe(1); // Depends on 1
      expect(dependencies.get(2).has(1)).toBe(true);
    });

    it("should handle multiple dependencies", async () => {
      const { buildDependencyGraph } = await import("./safe_output_topological_sort.cjs");

      const messages = [
        { type: "create_issue", temporary_id: "aw_dddddd111111", title: "Issue 1" },
        { type: "create_issue", temporary_id: "aw_eeeeee222222", title: "Issue 2" },
        {
          type: "create_issue",
          temporary_id: "aw_ffffff333333",
          body: "See #aw_dddddd111111 and #aw_eeeeee222222",
        },
      ];

      const { dependencies, providers } = buildDependencyGraph(messages);

      expect(dependencies.get(2).size).toBe(2);
      expect(dependencies.get(2).has(0)).toBe(true);
      expect(dependencies.get(2).has(1)).toBe(true);
    });

    it("should warn on duplicate temporary IDs", async () => {
      const { buildDependencyGraph } = await import("./safe_output_topological_sort.cjs");

      const messages = [
        { type: "create_issue", temporary_id: "aw_abc111def222", title: "First" },
        { type: "create_issue", temporary_id: "aw_abc111def222", title: "Second" },
      ];

      buildDependencyGraph(messages);

      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Duplicate temporary_id 'aw_abc111def222'"));
    });

    it("should handle messages without temporary IDs", async () => {
      const { buildDependencyGraph } = await import("./safe_output_topological_sort.cjs");

      const messages = [
        { type: "create_issue", title: "No temp ID" },
        { type: "add_comment", issue_number: 123, body: "Regular issue" },
      ];

      const { dependencies, providers } = buildDependencyGraph(messages);

      expect(providers.size).toBe(0);
      expect(dependencies.get(0).size).toBe(0);
      expect(dependencies.get(1).size).toBe(0);
    });
  });

  describe("detectCycle", () => {
    it("should detect simple cycle", async () => {
      const { detectCycle } = await import("./safe_output_topological_sort.cjs");

      // Create a cycle: 0 -> 1 -> 0
      const dependencies = new Map([
        [0, new Set([1])],
        [1, new Set([0])],
      ]);

      const cycle = detectCycle(dependencies);

      expect(cycle.length).toBeGreaterThan(0);
    });

    it("should detect complex cycle", async () => {
      const { detectCycle } = await import("./safe_output_topological_sort.cjs");

      // Create a cycle: 0 -> 1 -> 2 -> 0
      const dependencies = new Map([
        [0, new Set([1])],
        [1, new Set([2])],
        [2, new Set([0])],
      ]);

      const cycle = detectCycle(dependencies);

      expect(cycle.length).toBeGreaterThan(0);
    });

    it("should return empty array for acyclic graph", async () => {
      const { detectCycle } = await import("./safe_output_topological_sort.cjs");

      // Acyclic: 0 -> 1 -> 2
      const dependencies = new Map([
        [0, new Set()],
        [1, new Set([0])],
        [2, new Set([1])],
      ]);

      const cycle = detectCycle(dependencies);

      expect(cycle.length).toBe(0);
    });

    it("should handle disconnected components", async () => {
      const { detectCycle } = await import("./safe_output_topological_sort.cjs");

      // Two separate chains: 0 -> 1 and 2 -> 3
      const dependencies = new Map([
        [0, new Set()],
        [1, new Set([0])],
        [2, new Set()],
        [3, new Set([2])],
      ]);

      const cycle = detectCycle(dependencies);

      expect(cycle.length).toBe(0);
    });
  });

  describe("topologicalSort", () => {
    it("should sort messages with simple dependency", async () => {
      const { topologicalSort, buildDependencyGraph } = await import("./safe_output_topological_sort.cjs");

      const messages = [
        { type: "add_comment", issue_number: "aw_dddddd111111", body: "Comment" },
        { type: "create_issue", temporary_id: "aw_dddddd111111", title: "Parent" },
      ];

      const { dependencies } = buildDependencyGraph(messages);
      const sorted = topologicalSort(messages, dependencies);

      // Message 1 (create_issue) should come before message 0 (add_comment)
      expect(sorted).toEqual([1, 0]);
    });

    it("should preserve original order when no dependencies", async () => {
      const { topologicalSort, buildDependencyGraph } = await import("./safe_output_topological_sort.cjs");

      const messages = [
        { type: "create_issue", temporary_id: "aw_dddddd111111", title: "Issue 1" },
        { type: "create_issue", temporary_id: "aw_eeeeee222222", title: "Issue 2" },
        { type: "create_issue", temporary_id: "aw_ffffff333333", title: "Issue 3" },
      ];

      const { dependencies } = buildDependencyGraph(messages);
      const sorted = topologicalSort(messages, dependencies);

      expect(sorted).toEqual([0, 1, 2]);
    });

    it("should sort dependency chain correctly", async () => {
      const { topologicalSort, buildDependencyGraph } = await import("./safe_output_topological_sort.cjs");

      const messages = [
        { type: "create_issue", temporary_id: "aw_ffffff333333", body: "Ref #aw_eeeeee222222" },
        { type: "create_issue", temporary_id: "aw_eeeeee222222", body: "Ref #aw_dddddd111111" },
        { type: "create_issue", temporary_id: "aw_dddddd111111", title: "First" },
      ];

      const { dependencies } = buildDependencyGraph(messages);
      const sorted = topologicalSort(messages, dependencies);

      // Should be: message 2 (first), then 1 (second), then 0 (third)
      expect(sorted).toEqual([2, 1, 0]);
    });

    it("should handle multiple independent messages", async () => {
      const { topologicalSort, buildDependencyGraph } = await import("./safe_output_topological_sort.cjs");

      const messages = [
        { type: "create_issue", temporary_id: "aw_dddddd111111", title: "Independent 1" },
        { type: "add_comment", issue_number: "aw_dddddd111111", body: "Comment on 1" },
        { type: "create_issue", temporary_id: "aw_eeeeee222222", title: "Independent 2" },
        { type: "add_comment", issue_number: "aw_eeeeee222222", body: "Comment on 2" },
      ];

      const { dependencies } = buildDependencyGraph(messages);
      const sorted = topologicalSort(messages, dependencies);

      // Creates should come before their comments
      expect(sorted.indexOf(0)).toBeLessThan(sorted.indexOf(1)); // Issue 1 before comment on 1
      expect(sorted.indexOf(2)).toBeLessThan(sorted.indexOf(3)); // Issue 2 before comment on 2
    });

    it("should handle complex dependency graph", async () => {
      const { topologicalSort, buildDependencyGraph } = await import("./safe_output_topological_sort.cjs");

      const messages = [
        { type: "create_issue", temporary_id: "aw_aaaaaa111111", title: "Parent" },
        { type: "create_issue", temporary_id: "aw_bbbbbb111111", body: "Parent: #aw_aaaaaa111111" },
        { type: "create_issue", temporary_id: "aw_cccccc222222", body: "Parent: #aw_aaaaaa111111" },
        { type: "link_sub_issue", parent_issue_number: "aw_aaaaaa111111", sub_issue_number: "aw_bbbbbb111111" },
        { type: "link_sub_issue", parent_issue_number: "aw_aaaaaa111111", sub_issue_number: "aw_cccccc222222" },
      ];

      const { dependencies } = buildDependencyGraph(messages);
      const sorted = topologicalSort(messages, dependencies);

      // Parent must come first
      expect(sorted[0]).toBe(0);
      // Children must come after parent
      const childIndices = [sorted.indexOf(1), sorted.indexOf(2)];
      expect(Math.min(...childIndices)).toBeGreaterThan(0);
      // Links must come after all creates
      expect(sorted.indexOf(3)).toBeGreaterThan(Math.max(...childIndices));
      expect(sorted.indexOf(4)).toBeGreaterThan(Math.max(...childIndices));
    });
  });

  describe("sortSafeOutputMessages", () => {
    it("should return empty array for empty input", async () => {
      const { sortSafeOutputMessages } = await import("./safe_output_topological_sort.cjs");

      const sorted = sortSafeOutputMessages([]);

      expect(sorted).toEqual([]);
    });

    it("should return original messages for non-array input", async () => {
      const { sortSafeOutputMessages } = await import("./safe_output_topological_sort.cjs");

      const input = null;
      const sorted = sortSafeOutputMessages(input);

      expect(sorted).toBe(input);
    });

    it("should sort messages without temporary IDs first", async () => {
      const { sortSafeOutputMessages } = await import("./safe_output_topological_sort.cjs");

      const messages = [
        { type: "add_comment", issue_number: "aw_dddddd111111", body: "Comment" },
        { type: "create_issue", temporary_id: "aw_dddddd111111", title: "Issue" },
        { type: "create_issue", title: "No temp ID" },
      ];

      const sorted = sortSafeOutputMessages(messages);

      // Messages without dependencies should come first
      expect(sorted[0].type).toBe("create_issue");
      expect(sorted[0].title).toBe("Issue");
      expect(sorted[1].type).toBe("create_issue");
      expect(sorted[1].title).toBe("No temp ID");
      expect(sorted[2].type).toBe("add_comment");
    });

    it("should handle cross-references between issues, PRs, and discussions", async () => {
      const { sortSafeOutputMessages } = await import("./safe_output_topological_sort.cjs");

      const messages = [
        { type: "create_pull_request", temporary_id: "aw_fedcba111111", body: "Fixes #aw_dddddd111111" },
        { type: "create_issue", temporary_id: "aw_dddddd111111", title: "Bug report" },
        { type: "create_discussion", temporary_id: "aw_abcdef111111", body: "See #aw_fedcba111111" },
      ];

      const sorted = sortSafeOutputMessages(messages);

      // Issue should come first, then PR (which references issue), then discussion (which references PR)
      expect(sorted[0].type).toBe("create_issue");
      expect(sorted[1].type).toBe("create_pull_request");
      expect(sorted[2].type).toBe("create_discussion");
    });

    it("should return original order when cycle is detected", async () => {
      const { sortSafeOutputMessages } = await import("./safe_output_topological_sort.cjs");

      const messages = [
        { type: "create_issue", temporary_id: "aw_dddddd111111", body: "See #aw_eeeeee222222" },
        { type: "create_issue", temporary_id: "aw_eeeeee222222", body: "See #aw_dddddd111111" },
      ];

      const sorted = sortSafeOutputMessages(messages);

      // Should return original order due to cycle
      expect(sorted).toEqual(messages);
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Dependency cycle detected"));
    });

    it("should log info about reordering", async () => {
      const { sortSafeOutputMessages } = await import("./safe_output_topological_sort.cjs");

      const messages = [
        { type: "add_comment", issue_number: "aw_dddddd111111", body: "Comment" },
        { type: "create_issue", temporary_id: "aw_dddddd111111", title: "Issue" },
      ];

      sortSafeOutputMessages(messages);

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Topological sort reordered"));
    });

    it("should log info when order doesn't change", async () => {
      const { sortSafeOutputMessages } = await import("./safe_output_topological_sort.cjs");

      const messages = [
        { type: "create_issue", temporary_id: "aw_dddddd111111", title: "Issue" },
        { type: "add_comment", issue_number: "aw_dddddd111111", body: "Comment" },
      ];

      sortSafeOutputMessages(messages);

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("already in optimal order"));
    });

    it("should handle complex real-world scenario", async () => {
      const { sortSafeOutputMessages } = await import("./safe_output_topological_sort.cjs");

      // Simulate a real workflow: create parent issue, create sub-issues, link them, add comments
      const messages = [
        { type: "add_comment", issue_number: "aw_aaaaaa111111", body: "Status update" },
        { type: "link_sub_issue", parent_issue_number: "aw_aaaaaa111111", sub_issue_number: "aw_cccccc222222" },
        { type: "create_issue", temporary_id: "aw_bbbbbb111111", title: "Sub-task 1", body: "Parent: #aw_aaaaaa111111" },
        { type: "create_issue", temporary_id: "aw_aaaaaa111111", title: "Epic" },
        { type: "link_sub_issue", parent_issue_number: "aw_aaaaaa111111", sub_issue_number: "aw_bbbbbb111111" },
        { type: "create_issue", temporary_id: "aw_cccccc222222", title: "Sub-task 2", body: "Parent: #aw_aaaaaa111111" },
        { type: "add_comment", issue_number: "aw_bbbbbb111111", body: "Work started" },
      ];

      const sorted = sortSafeOutputMessages(messages);

      // Verify parent is created first
      const parentIndex = sorted.findIndex(m => m.temporary_id === "aw_aaaaaa111111");
      expect(parentIndex).toBe(0);

      // Verify children come after parent
      const child1Index = sorted.findIndex(m => m.temporary_id === "aw_bbbbbb111111");
      const child2Index = sorted.findIndex(m => m.temporary_id === "aw_cccccc222222");
      expect(child1Index).toBeGreaterThan(parentIndex);
      expect(child2Index).toBeGreaterThan(parentIndex);

      // Verify links come after all creates
      const link1Index = sorted.findIndex(m => m.type === "link_sub_issue" && m.sub_issue_number === "aw_bbbbbb111111");
      const link2Index = sorted.findIndex(m => m.type === "link_sub_issue" && m.sub_issue_number === "aw_cccccc222222");
      expect(link1Index).toBeGreaterThan(child1Index);
      expect(link2Index).toBeGreaterThan(child2Index);

      // Verify comments come after their targets
      const parentCommentIndex = sorted.findIndex(m => m.type === "add_comment" && m.issue_number === "aw_aaaaaa111111");
      const child1CommentIndex = sorted.findIndex(m => m.type === "add_comment" && m.issue_number === "aw_bbbbbb111111");
      expect(parentCommentIndex).toBeGreaterThan(parentIndex);
      expect(child1CommentIndex).toBeGreaterThan(child1Index);
    });

    it("should handle messages referencing external (already resolved) temp IDs", async () => {
      const { sortSafeOutputMessages } = await import("./safe_output_topological_sort.cjs");

      // Message references a temp ID that's not created in this batch
      // (might be from a previous step)
      const messages = [
        { type: "create_issue", temporary_id: "aw_abc123456789", title: "New Issue" },
        { type: "add_comment", issue_number: "aw_def987654321", body: "Comment on external" },
        { type: "add_comment", issue_number: "aw_abc123456789", body: "Comment on new" },
      ];

      const sorted = sortSafeOutputMessages(messages);

      // New issue should come before its comment
      expect(sorted[0].temporary_id).toBe("aw_abc123456789");
      expect(sorted[2].issue_number).toBe("aw_abc123456789");

      // External reference can be anywhere (no dependency in this batch)
      // It should appear but we don't enforce ordering relative to unrelated items
      expect(sorted.some(m => m.issue_number === "aw_def987654321")).toBe(true);
    });
  });
});
