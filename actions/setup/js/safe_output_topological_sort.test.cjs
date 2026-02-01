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

      const { providers } = buildDependencyGraph(messages);

      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Duplicate temporary_id 'aw_abc111def222'"));

      // Verify only the first occurrence is used as provider
      expect(providers.get("aw_abc111def222")).toBe(0);
      expect(providers.size).toBe(1);
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

    it("should handle large graphs with many dependencies", async () => {
      const { sortSafeOutputMessages } = await import("./safe_output_topological_sort.cjs");

      // Create a large tree: 1 root, 10 level-1 children, each with 5 level-2 children
      const messages = [];
      const rootId = "aw_root00000000";
      messages.push({ type: "create_issue", temporary_id: rootId, title: "Root" });

      for (let i = 0; i < 10; i++) {
        const level1Id = `aw_lv1_${i.toString().padStart(7, "0")}`;
        messages.push({
          type: "create_issue",
          temporary_id: level1Id,
          body: `Parent: #${rootId}`,
        });

        for (let j = 0; j < 5; j++) {
          const level2Id = `aw_lv2_${i}_${j.toString().padStart(4, "0")}`;
          messages.push({
            type: "create_issue",
            temporary_id: level2Id,
            body: `Parent: #${level1Id}`,
          });
        }
      }

      const sorted = sortSafeOutputMessages(messages);

      // Verify root comes first
      expect(sorted[0].temporary_id).toBe(rootId);

      // Verify each level-1 item comes before its level-2 children
      for (let i = 0; i < 10; i++) {
        const level1Id = `aw_lv1_${i.toString().padStart(7, "0")}`;
        const level1Index = sorted.findIndex(m => m.temporary_id === level1Id);

        for (let j = 0; j < 5; j++) {
          const level2Id = `aw_lv2_${i}_${j.toString().padStart(4, "0")}`;
          const level2Index = sorted.findIndex(m => m.temporary_id === level2Id);
          expect(level1Index).toBeLessThan(level2Index);
        }
      }

      // Verify root comes before all level-1 items
      const rootIndex = sorted.findIndex(m => m.temporary_id === rootId);
      for (let i = 0; i < 10; i++) {
        const level1Id = `aw_lv1_${i.toString().padStart(7, "0")}`;
        const level1Index = sorted.findIndex(m => m.temporary_id === level1Id);
        expect(rootIndex).toBeLessThan(level1Index);
      }
    });

    it("should handle deeply nested linear dependencies", async () => {
      const { sortSafeOutputMessages } = await import("./safe_output_topological_sort.cjs");

      // Create a chain: A -> B -> C -> D -> E -> F
      const messages = [];
      const ids = ["aw_aaaaaa111111", "aw_bbbbbb222222", "aw_cccccc333333", "aw_dddddd444444", "aw_eeeeee555555", "aw_ffffff666666"];

      // Add in reverse order to test sorting
      for (let i = ids.length - 1; i >= 0; i--) {
        messages.push({
          type: "create_issue",
          temporary_id: ids[i],
          body: i > 0 ? `Depends on #${ids[i - 1]}` : "First issue",
        });
      }

      const sorted = sortSafeOutputMessages(messages);

      // Verify they're sorted in dependency order
      for (let i = 0; i < ids.length; i++) {
        expect(sorted[i].temporary_id).toBe(ids[i]);
      }
    });

    it("should handle multiple disconnected dependency chains", async () => {
      const { sortSafeOutputMessages } = await import("./safe_output_topological_sort.cjs");

      const messages = [
        // Chain 1: A -> B
        { type: "create_issue", temporary_id: "aw_aaaaaa111111", title: "A" },
        { type: "create_issue", temporary_id: "aw_bbbbbb222222", body: "Ref #aw_aaaaaa111111" },

        // Chain 2: C -> D
        { type: "create_issue", temporary_id: "aw_cccccc333333", title: "C" },
        { type: "create_issue", temporary_id: "aw_dddddd444444", body: "Ref #aw_cccccc333333" },

        // Chain 3: E -> F
        { type: "create_issue", temporary_id: "aw_eeeeee555555", title: "E" },
        { type: "create_issue", temporary_id: "aw_ffffff666666", body: "Ref #aw_eeeeee555555" },
      ];

      const sorted = sortSafeOutputMessages(messages);

      // Within each chain, verify dependency order
      const aIndex = sorted.findIndex(m => m.temporary_id === "aw_aaaaaa111111");
      const bIndex = sorted.findIndex(m => m.temporary_id === "aw_bbbbbb222222");
      expect(aIndex).toBeLessThan(bIndex);

      const cIndex = sorted.findIndex(m => m.temporary_id === "aw_cccccc333333");
      const dIndex = sorted.findIndex(m => m.temporary_id === "aw_dddddd444444");
      expect(cIndex).toBeLessThan(dIndex);

      const eIndex = sorted.findIndex(m => m.temporary_id === "aw_eeeeee555555");
      const fIndex = sorted.findIndex(m => m.temporary_id === "aw_ffffff666666");
      expect(eIndex).toBeLessThan(fIndex);
    });

    it("should handle diamond dependency pattern", async () => {
      const { sortSafeOutputMessages } = await import("./safe_output_topological_sort.cjs");

      // Diamond: A -> B, A -> C, B -> D, C -> D
      const messages = [
        { type: "create_issue", temporary_id: "aw_dddddd444444", body: "Needs #aw_bbbbbb222222 and #aw_cccccc333333" },
        { type: "create_issue", temporary_id: "aw_bbbbbb222222", body: "Child of #aw_aaaaaa111111" },
        { type: "create_issue", temporary_id: "aw_aaaaaa111111", title: "Root" },
        { type: "create_issue", temporary_id: "aw_cccccc333333", body: "Child of #aw_aaaaaa111111" },
      ];

      const sorted = sortSafeOutputMessages(messages);

      const aIndex = sorted.findIndex(m => m.temporary_id === "aw_aaaaaa111111");
      const bIndex = sorted.findIndex(m => m.temporary_id === "aw_bbbbbb222222");
      const cIndex = sorted.findIndex(m => m.temporary_id === "aw_cccccc333333");
      const dIndex = sorted.findIndex(m => m.temporary_id === "aw_dddddd444444");

      // A must come first
      expect(aIndex).toBe(0);

      // B and C must come after A
      expect(bIndex).toBeGreaterThan(aIndex);
      expect(cIndex).toBeGreaterThan(aIndex);

      // D must come after both B and C
      expect(dIndex).toBeGreaterThan(bIndex);
      expect(dIndex).toBeGreaterThan(cIndex);
    });

    it("should handle messages with multiple temporary ID references in body", async () => {
      const { sortSafeOutputMessages } = await import("./safe_output_topological_sort.cjs");

      const messages = [
        {
          type: "create_issue",
          temporary_id: "aw_ffffff666666",
          body: "Blocks #aw_aaaaaa111111, #aw_bbbbbb222222, and #aw_cccccc333333",
        },
        { type: "create_issue", temporary_id: "aw_aaaaaa111111", title: "First" },
        { type: "create_issue", temporary_id: "aw_bbbbbb222222", title: "Second" },
        { type: "create_issue", temporary_id: "aw_cccccc333333", title: "Third" },
      ];

      const sorted = sortSafeOutputMessages(messages);

      // All referenced IDs must come before the message that references them
      const fIndex = sorted.findIndex(m => m.temporary_id === "aw_ffffff666666");
      const aIndex = sorted.findIndex(m => m.temporary_id === "aw_aaaaaa111111");
      const bIndex = sorted.findIndex(m => m.temporary_id === "aw_bbbbbb222222");
      const cIndex = sorted.findIndex(m => m.temporary_id === "aw_cccccc333333");

      expect(fIndex).toBeGreaterThan(aIndex);
      expect(fIndex).toBeGreaterThan(bIndex);
      expect(fIndex).toBeGreaterThan(cIndex);
    });

    it("should handle empty messages array gracefully", async () => {
      const { sortSafeOutputMessages } = await import("./safe_output_topological_sort.cjs");

      const sorted = sortSafeOutputMessages([]);

      expect(sorted).toEqual([]);
    });

    it("should handle single message", async () => {
      const { sortSafeOutputMessages } = await import("./safe_output_topological_sort.cjs");

      const messages = [{ type: "create_issue", temporary_id: "aw_aaaaaa111111", title: "Solo" }];

      const sorted = sortSafeOutputMessages(messages);

      expect(sorted).toEqual(messages);
    });

    it("should preserve message object references", async () => {
      const { sortSafeOutputMessages } = await import("./safe_output_topological_sort.cjs");

      const msg1 = { type: "create_issue", temporary_id: "aw_aaaaaa111111", title: "First" };
      const msg2 = { type: "create_issue", temporary_id: "aw_bbbbbb222222", body: "Ref #aw_aaaaaa111111" };

      const sorted = sortSafeOutputMessages([msg2, msg1]);

      // Objects should be the same references, not copies
      expect(sorted[0]).toBe(msg1);
      expect(sorted[1]).toBe(msg2);
    });

    it("should handle cycle with 3 nodes", async () => {
      const { sortSafeOutputMessages } = await import("./safe_output_topological_sort.cjs");

      // Cycle: A -> B -> C -> A
      const messages = [
        { type: "create_issue", temporary_id: "aw_aaaaaa111111", body: "Needs #aw_cccccc333333" },
        { type: "create_issue", temporary_id: "aw_bbbbbb222222", body: "Needs #aw_aaaaaa111111" },
        { type: "create_issue", temporary_id: "aw_cccccc333333", body: "Needs #aw_bbbbbb222222" },
      ];

      const sorted = sortSafeOutputMessages(messages);

      // Should preserve original order when cycle detected
      expect(sorted).toEqual(messages);
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Dependency cycle detected"));
    });

    it("should handle messages with no type field", async () => {
      const { sortSafeOutputMessages } = await import("./safe_output_topological_sort.cjs");

      const messages = [
        { temporary_id: "aw_aaaaaa111111", title: "No type" },
        { type: "create_issue", temporary_id: "aw_bbbbbb222222", title: "Has type" },
      ];

      const sorted = sortSafeOutputMessages(messages);

      // Should handle gracefully without crashing
      expect(sorted.length).toBe(2);
    });

    it("should handle mixed types (issues, PRs, discussions, comments)", async () => {
      const { sortSafeOutputMessages } = await import("./safe_output_topological_sort.cjs");

      const messages = [
        { type: "create_pull_request", temporary_id: "aw_aaaaaa111111", title: "PR" },
        { type: "add_comment", issue_number: "aw_aaaaaa111111", body: "Comment on PR" },
        { type: "create_discussion", temporary_id: "aw_bbbbbb222222", body: "See PR #aw_aaaaaa111111" },
        { type: "create_issue", temporary_id: "aw_cccccc333333", body: "Related to #aw_bbbbbb222222" },
      ];

      const sorted = sortSafeOutputMessages(messages);

      // PR comes first
      expect(sorted[0].type).toBe("create_pull_request");
      // Comment on PR comes after PR
      const prIndex = sorted.findIndex(m => m.type === "create_pull_request");
      const commentIndex = sorted.findIndex(m => m.type === "add_comment");
      expect(commentIndex).toBeGreaterThan(prIndex);
      // Discussion referencing PR comes after PR
      const discussionIndex = sorted.findIndex(m => m.type === "create_discussion");
      expect(discussionIndex).toBeGreaterThan(prIndex);
      // Issue referencing discussion comes after discussion
      const issueIndex = sorted.findIndex(m => m.type === "create_issue");
      expect(issueIndex).toBeGreaterThan(discussionIndex);
    });
  });
});
