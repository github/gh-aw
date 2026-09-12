// @ts-check
import { describe, it, expect, beforeEach } from "vitest";
const { main } = require("./add_labels.cjs");

describe("add_labels", () => {
  let mockCore;
  let mockGithub;
  let mockContext;

  beforeEach(() => {
    // Reset mocks before each test
    mockCore = {
      info: () => {},
      warning: () => {},
      error: () => {},
      debug: () => {},
      messages: [],
      infos: [],
      warnings: [],
      errors: [],
    };

    // Capture all logged messages
    mockCore.info = msg => {
      mockCore.infos.push(msg);
      mockCore.messages.push({ level: "info", message: msg });
    };
    mockCore.warning = msg => {
      mockCore.warnings.push(msg);
      mockCore.messages.push({ level: "warning", message: msg });
    };
    mockCore.error = msg => {
      mockCore.errors.push(msg);
      mockCore.messages.push({ level: "error", message: msg });
    };

    mockGithub = {
      graphql: async (query, variables) => {
        // Repo labels query used by fetchAllRepoLabels: resolve label IDs by name.
        if (typeof query === "string" && query.includes("repository(owner")) {
          return {
            repository: {
              labels: {
                nodes: (mockGithub._repoLabels || ["bug", "enhancement", "documentation", "security:low", "security:medium", "security:high"]).map(name => ({
                  id: `LABEL_${name}`,
                  name,
                })),
                pageInfo: { hasNextPage: false, endCursor: null },
              },
            },
          };
        }
        // updateIssue intent mutation: echo back the requested label names.
        const labels = (variables?.labels || []).map(l => ({ name: l.name || l.labelId?.replace(/^LABEL_/, "") }));
        return { updateIssue: { issue: { id: variables?.issueId, labels: { nodes: labels } } } };
      },
      rest: {
        issues: {
          addLabels: async () => ({}),
          createLabel: async ({ name }) => {
            mockGithub._repoLabels = [...(mockGithub._repoLabels || ["bug", "enhancement", "documentation", "security:low", "security:medium", "security:high"]), name];
            return { data: { name } };
          },
          get: async () => ({
            data: {
              node_id: "ISSUE_NODE_ID",
              title: "Test issue title",
              labels: [],
            },
          }),
        },
      },
    };

    mockContext = {
      repo: {
        owner: "test-owner",
        repo: "test-repo",
      },
      payload: {
        issue: {
          number: 123,
        },
      },
    };

    // Set globals
    global.core = mockCore;
    global.github = mockGithub;
    global.context = mockContext;
  });

  describe("main factory", () => {
    it("should create a handler function with default configuration", async () => {
      const handler = await main();
      expect(typeof handler).toBe("function");
    });

    it("should create a handler function with custom configuration", async () => {
      const handler = await main({
        allowed: ["bug", "enhancement"],
        max: 5,
      });
      expect(typeof handler).toBe("function");
    });

    it("should log configuration on initialization", async () => {
      await main({ allowed: ["bug", "enhancement"], max: 3 });
      expect(mockCore.infos.some(msg => msg.includes("max=3"))).toBe(true);
      expect(mockCore.infos.some(msg => msg.includes("bug, enhancement"))).toBe(true);
    });
  });

  describe("handleAddLabels", () => {
    describe("AL-005 runtime target authorization", () => {
      it("AL-002 ignores a conflicting item_number when target is triggering", async () => {
        const handler = await main({ max: 10, target: "triggering" });
        const addLabelsCalls = [];
        mockGithub.rest.issues.addLabels = async params => {
          addLabelsCalls.push(params);
          return {};
        };

        const result = await handler({ item_number: 456, labels: ["bug"] }, {});

        expect(result.success).toBe(true);
        expect(result.number).toBe(123);
        expect(addLabelsCalls[0].issue_number).toBe(123);
      });

      it("AL-001 defaults to the triggering item when target is omitted", async () => {
        const handler = await main({ max: 10 });
        const addLabelsCalls = [];
        mockGithub.rest.issues.addLabels = async params => {
          addLabelsCalls.push(params);
          return {};
        };

        const result = await handler({ item_number: 456, labels: ["bug"] }, {});

        expect(result.success).toBe(true);
        expect(result.number).toBe(123);
        expect(addLabelsCalls[0].issue_number).toBe(123);
      });

      it("AL-003 ignores a conflicting item_number when target is a fixed number", async () => {
        const handler = await main({ max: 10, target: "789" });
        const addLabelsCalls = [];
        mockGithub.rest.issues.addLabels = async params => {
          addLabelsCalls.push(params);
          return {};
        };

        const result = await handler({ item_number: 456, labels: ["bug"] }, {});

        expect(result.success).toBe(true);
        expect(result.number).toBe(789);
        expect(addLabelsCalls[0].issue_number).toBe(789);
      });

      it("AL-003 uses a fixed numeric target without an item_number", async () => {
        const handler = await main({ max: 10, target: "789" });
        const addLabelsCalls = [];
        mockGithub.rest.issues.addLabels = async params => {
          addLabelsCalls.push(params);
          return {};
        };

        const result = await handler({ labels: ["bug"] }, {});

        expect(result.success).toBe(true);
        expect(result.number).toBe(789);
        expect(addLabelsCalls[0].issue_number).toBe(789);
      });

      it.each([-1, 1.5, Infinity])("rejects invalid fixed target %s", async target => {
        const handler = await main({ max: 10, target });
        const addLabelsCalls = [];
        mockGithub.rest.issues.addLabels = async params => {
          addLabelsCalls.push(params);
          return {};
        };

        const result = await handler({ labels: ["bug"] }, {});

        expect(result.success).toBe(false);
        expect(result.error).toBe("Invalid issue/PR number");
        expect(addLabelsCalls).toHaveLength(0);
      });

      it("AL-004 accepts item_number when target is wildcard", async () => {
        const handler = await main({ max: 10, target: "*" });
        const addLabelsCalls = [];
        mockGithub.rest.issues.addLabels = async params => {
          addLabelsCalls.push(params);
          return {};
        };

        const result = await handler({ item_number: 456, labels: ["bug"] }, {});

        expect(result.success).toBe(true);
        expect(result.number).toBe(456);
        expect(addLabelsCalls[0].issue_number).toBe(456);
      });
    });

    it("should add labels to an issue using explicit item_number", async () => {
      const handler = await main({ max: 10, target: "*" });
      const addLabelsCalls = [];

      mockGithub.rest.issues.addLabels = async params => {
        addLabelsCalls.push(params);
        return {};
      };

      const result = await handler(
        {
          item_number: 456,
          labels: ["bug", "enhancement"],
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(result.number).toBe(456);
      expect(result.labelsAdded).toEqual(["bug", "enhancement"]);
      expect(addLabelsCalls.length).toBe(1);
      expect(addLabelsCalls[0].issue_number).toBe(456);
      expect(addLabelsCalls[0].labels).toEqual(["bug", "enhancement"]);
    });

    it("should accept structured label entries and add normalized label names", async () => {
      const handler = await main({ max: 10, target: "*", issue_intent: true });
      const graphqlMutationCalls = [];

      const originalGraphql = mockGithub.graphql;
      mockGithub.graphql = async (query, variables) => {
        if (typeof query === "string" && query.includes("updateIssue")) {
          graphqlMutationCalls.push(variables);
        }
        return originalGraphql(query, variables);
      };

      const result = await handler(
        {
          item_number: 456,
          labels: [{ name: "bug", rationale: "Known crash path", confidence: "HIGH", suggest: true }],
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(result.number).toBe(456);
      // Intent metadata routes through the GraphQL updateIssue mutation, not REST addLabels
      expect(graphqlMutationCalls).toHaveLength(1);
      expect(graphqlMutationCalls[0].labels).toEqual([{ labelId: "LABEL_bug", rationale: "Known crash path", confidence: "HIGH", suggest: true }]);
      expect(graphqlMutationCalls[0].headers).toEqual({ "GraphQL-Features": "update_issue_suggestions" });
    });

    it("should send structured label metadata without requiring a runtime feature", async () => {
      const handler = await main({ max: 10, issue_intent: true });
      const graphqlMutationCalls = [];

      const originalGraphql = mockGithub.graphql;
      mockGithub.graphql = async (query, variables) => {
        if (typeof query === "string" && query.includes("updateIssue")) {
          graphqlMutationCalls.push(variables);
        }
        return originalGraphql(query, variables);
      };

      const result = await handler(
        {
          item_number: 456,
          labels: [{ name: "bug", rationale: "Application crashes on file uploads >5MB", confidence: "HIGH" }],
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(graphqlMutationCalls).toHaveLength(1);
      expect(graphqlMutationCalls[0].labels).toEqual([{ labelId: "LABEL_bug", rationale: "Application crashes on file uploads >5MB", confidence: "HIGH" }]);
    });

    it("should normalize lowercase confidence in structured label metadata", async () => {
      const handler = await main({ max: 10, issue_intent: true });
      const graphqlMutationCalls = [];

      const originalGraphql = mockGithub.graphql;
      mockGithub.graphql = async (query, variables) => {
        if (typeof query === "string" && query.includes("updateIssue")) {
          graphqlMutationCalls.push(variables);
        }
        return originalGraphql(query, variables);
      };

      const result = await handler(
        {
          item_number: 456,
          labels: [{ name: "bug", rationale: "Application crashes on file uploads >5MB", confidence: "high" }],
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(graphqlMutationCalls).toHaveLength(1);
      expect(graphqlMutationCalls[0].labels).toEqual([{ labelId: "LABEL_bug", rationale: "Application crashes on file uploads >5MB", confidence: "HIGH" }]);
    });

    it("should preserve existing labels when adding intent labels via GraphQL", async () => {
      const handler = await main({ max: 10, issue_intent: true });
      const graphqlMutationCalls = [];

      mockGithub.rest.issues.get = async () => ({
        data: {
          node_id: "ISSUE_NODE_ID",
          title: "Test issue title",
          labels: [{ name: "enhancement" }],
        },
      });

      const originalGraphql = mockGithub.graphql;
      mockGithub.graphql = async (query, variables) => {
        if (typeof query === "string" && query.includes("updateIssue")) {
          graphqlMutationCalls.push(variables);
        }
        return originalGraphql(query, variables);
      };

      const result = await handler(
        {
          item_number: 456,
          labels: [{ name: "bug", rationale: "Crash on upload", confidence: "HIGH" }],
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(result.labelsAdded).toEqual(["bug"]);
      expect(graphqlMutationCalls).toHaveLength(1);
      // Existing labels are merged (metadata-free) so add-only semantics are preserved
      expect(graphqlMutationCalls[0].labels).toEqual([{ labelId: "LABEL_bug", rationale: "Crash on upload", confidence: "HIGH" }, { labelId: "LABEL_enhancement" }]);
    });

    it("should skip an already-applied label instead of re-proposing its intent metadata", async () => {
      const handler = await main({ max: 10 });
      const graphqlMutationCalls = [];
      const addLabelsCalls = [];

      mockGithub.rest.issues.get = async () => ({
        data: {
          node_id: "ISSUE_NODE_ID",
          title: "Test issue title",
          labels: [{ name: "feature-openapi" }, { name: "area-minimal" }],
        },
      });
      const originalGraphql = mockGithub.graphql;
      mockGithub.graphql = async (query, variables) => {
        if (typeof query === "string" && query.includes("updateIssue")) {
          graphqlMutationCalls.push(variables);
        }
        return originalGraphql(query, variables);
      };
      mockGithub.rest.issues.addLabels = async params => {
        addLabelsCalls.push(params);
        return {};
      };

      const result = await handler(
        {
          item_number: 68619,
          labels: [{ name: "area-minimal", rationale: "Minimal APIs area", confidence: "MEDIUM" }],
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(result.labelsAdded).toEqual([]);
      expect(result.labelsSuggested).toEqual([]);
      expect(result.after_state.labels).toEqual(["feature-openapi", "area-minimal"]);
      expect(graphqlMutationCalls).toHaveLength(0);
      expect(addLabelsCalls).toHaveLength(0);
    });

    it("should report a confidence-gated intent label as suggested rather than added", async () => {
      const handler = await main({ max: 10, target: "*" });

      mockGithub.rest.issues.get = async () => ({
        data: {
          node_id: "ISSUE_NODE_ID",
          title: "Test issue title",
          labels: [{ name: "feature-openapi" }],
        },
      });
      mockGithub.graphql = async (query, variables) => {
        if (typeof query === "string" && query.includes("repository(owner")) {
          return {
            repository: {
              labels: {
                nodes: [
                  { id: "LABEL_feature-openapi", name: "feature-openapi" },
                  { id: "LABEL_area-minimal", name: "area-minimal" },
                ],
                pageInfo: { hasNextPage: false, endCursor: null },
              },
            },
          };
        }
        return { updateIssue: { issue: { labels: { nodes: [{ name: "feature-openapi" }] } } } };
      };

      const result = await handler(
        {
          item_number: 68619,
          labels: [{ name: "area-minimal", rationale: "Minimal APIs area", confidence: "MEDIUM" }],
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(result.labelsAdded).toEqual([]);
      expect(result.labelsSuggested).toEqual(["area-minimal"]);
      expect(result.after_state.labels).toEqual(["feature-openapi"]);
      expect(mockCore.infos).toContain("Successfully added 0 labels to issue #68619 in test-owner/test-repo");
    });

    it("should add metadata-free labels through REST when the intent mutation does not apply them", async () => {
      const handler = await main({ max: 10 });
      const addLabelsCalls = [];
      const graphqlMutationCalls = [];

      mockGithub.rest.issues.get = async () => ({
        data: {
          node_id: "ISSUE_NODE_ID",
          title: "Test issue title",
          labels: [{ name: "feature-openapi" }],
        },
      });
      mockGithub.graphql = async (query, variables) => {
        if (typeof query === "string" && query.includes("repository(owner")) {
          return {
            repository: {
              labels: {
                nodes: [
                  { id: "LABEL_feature-openapi", name: "feature-openapi" },
                  { id: "LABEL_area-minimal", name: "area-minimal" },
                  { id: "LABEL_bug", name: "bug" },
                ],
                pageInfo: { hasNextPage: false, endCursor: null },
              },
            },
          };
        }
        graphqlMutationCalls.push(variables);
        return { updateIssue: { issue: { labels: { nodes: [{ name: "feature-openapi" }] } } } };
      };
      mockGithub.rest.issues.addLabels = async params => {
        addLabelsCalls.push(params);
        return { data: [{ name: "bug" }] };
      };

      const result = await handler(
        {
          item_number: 68619,
          labels: [{ name: "area-minimal", rationale: "Minimal APIs area", confidence: "MEDIUM" }, { name: "bug" }],
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(result.labelsAdded).toEqual(["bug"]);
      expect(result.labelsSuggested).toEqual(["area-minimal"]);
      expect(result.after_state.labels).toEqual(["feature-openapi", "bug"]);
      expect(result.after_state.labels).not.toContain("area-minimal");
      expect(graphqlMutationCalls).toHaveLength(1);
      expect(graphqlMutationCalls[0].labels).toEqual([{ labelId: "LABEL_area-minimal", rationale: "Minimal APIs area", confidence: "MEDIUM" }, { labelId: "LABEL_bug" }, { labelId: "LABEL_feature-openapi" }]);
      expect(addLabelsCalls).toHaveLength(1);
      expect(addLabelsCalls[0].labels).toEqual(["bug"]);
    });

    it("should restore pre-existing labels omitted by the intent mutation", async () => {
      const handler = await main({ max: 10 });
      const addLabelsCalls = [];
      const restoredLabels = [{ name: "area-minimal" }];

      mockGithub.rest.issues.get = async () => ({
        data: {
          node_id: "ISSUE_NODE_ID",
          title: "Test issue title",
          labels: [{ name: "feature-openapi" }, { name: "area-minimal" }],
        },
      });
      mockGithub.graphql = async (query, variables) => {
        if (typeof query === "string" && query.includes("repository(owner")) {
          return {
            repository: {
              labels: {
                nodes: [
                  { id: "LABEL_feature-openapi", name: "feature-openapi" },
                  { id: "LABEL_area-minimal", name: "area-minimal" },
                  { id: "LABEL_bug", name: "bug" },
                ],
                pageInfo: { hasNextPage: false, endCursor: null },
              },
            },
          };
        }
        return { updateIssue: { issue: { labels: { nodes: [{ name: "feature-openapi" }, { name: "bug" }] } } } };
      };
      mockGithub.rest.issues.addLabels = async params => {
        addLabelsCalls.push(params);
        return { data: restoredLabels };
      };

      const result = await handler(
        {
          item_number: 68619,
          labels: [{ name: "bug", rationale: "Confirmed defect", confidence: "HIGH" }],
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(result.labelsAdded).toEqual(["bug"]);
      expect(result.labelsSuggested).toEqual([]);
      expect(result.after_state.labels).toEqual(["feature-openapi", "area-minimal", "bug"]);
      expect(addLabelsCalls).toHaveLength(1);
      expect(addLabelsCalls[0].labels).toEqual(["area-minimal"]);
      expect(mockCore.warnings[0]).toContain("restoring them via the REST add-labels endpoint");
    });

    it("should return a standardized error code when issue node_id is missing on issue-intent path", async () => {
      const handler = await main({ max: 10, issue_intent: true });
      mockGithub.rest.issues.get = async () => ({
        data: {
          title: "Test issue title",
          labels: [],
        },
      });

      const result = await handler(
        {
          item_number: 456,
          labels: [{ name: "bug", rationale: "Crash on upload", confidence: "HIGH" }],
        },
        {}
      );

      expect(result.success).toBe(false);
      expect(result.error).toContain("E099");
      expect(result.error).toContain("Failed to resolve GraphQL node ID");
    });

    it("should accept issue_number as an alias for item_number", async () => {
      const handler = await main({ max: 10, target: "*" });
      const addLabelsCalls = [];

      mockGithub.rest.issues.addLabels = async params => {
        addLabelsCalls.push(params);
        return {};
      };

      const result = await handler(
        {
          issue_number: 456,
          labels: ["bug"],
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(result.number).toBe(456);
      expect(addLabelsCalls[0].issue_number).toBe(456);
    });

    it("should accept pr_number as an alias for item_number", async () => {
      const handler = await main({ max: 10, target: "*" });
      const addLabelsCalls = [];

      mockGithub.rest.issues.addLabels = async params => {
        addLabelsCalls.push(params);
        return {};
      };

      const result = await handler(
        {
          pr_number: 789,
          labels: ["enhancement"],
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(result.number).toBe(789);
      expect(addLabelsCalls[0].issue_number).toBe(789);
    });

    it("should accept pull_number as an alias for item_number", async () => {
      const handler = await main({ max: 10, target: "*" });
      const addLabelsCalls = [];

      mockGithub.rest.issues.addLabels = async params => {
        addLabelsCalls.push(params);
        return {};
      };

      const result = await handler(
        {
          pull_number: 101,
          labels: ["needs-review"],
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(result.number).toBe(101);
      expect(addLabelsCalls[0].issue_number).toBe(101);
    });

    it("should add labels to an issue from context when item_number not provided", async () => {
      const handler = await main({ max: 10 });
      const addLabelsCalls = [];

      mockGithub.rest.issues.addLabels = async params => {
        addLabelsCalls.push(params);
        return {};
      };

      const result = await handler(
        {
          labels: ["documentation"],
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(result.number).toBe(123);
      expect(result.labelsAdded).toEqual(["documentation"]);
      expect(result.contextType).toBe("issue");
    });

    it("should add labels from workflow_dispatch aw_context when issue payload is absent", async () => {
      mockContext.eventName = "workflow_dispatch";
      mockContext.payload = {
        inputs: {
          aw_context: JSON.stringify({
            event_type: "issue_comment",
            item_type: "issue",
            item_number: 456,
            repo: "test-owner/test-repo",
          }),
        },
      };

      const handler = await main({ max: 10 });
      const addLabelsCalls = [];

      mockGithub.rest.issues.addLabels = async params => {
        addLabelsCalls.push(params);
        return {};
      };

      const result = await handler(
        {
          labels: ["documentation"],
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(result.number).toBe(456);
      expect(result.contextType).toBe("issue");
      expect(addLabelsCalls[0].issue_number).toBe(456);
    });

    it("should add labels to a pull request from context", async () => {
      mockContext.payload = {
        pull_request: {
          number: 789,
        },
      };

      const handler = await main({ max: 10 });
      const addLabelsCalls = [];

      mockGithub.rest.issues.addLabels = async params => {
        addLabelsCalls.push(params);
        return {};
      };

      const result = await handler(
        {
          labels: ["needs-review"],
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(result.number).toBe(789);
      expect(result.contextType).toBe("pull request");
    });

    it("should handle invalid item_number", async () => {
      const handler = await main({ max: 10, target: "*" });

      const result = await handler(
        {
          item_number: "invalid",
          labels: ["bug"],
        },
        {}
      );

      expect(result.success).toBe(false);
      expect(result.error.includes("Invalid item number")).toBe(true);
    });

    it("should handle missing item_number and no context", async () => {
      mockContext.payload = {};

      const handler = await main({ max: 10 });

      const result = await handler(
        {
          labels: ["bug"],
        },
        {}
      );

      expect(result.success).toBe(false);
      expect(result.error.includes("No issue/PR number available")).toBe(true);
    });

    it("should respect max count limit", async () => {
      const handler = await main({ max: 2 });

      // First call succeeds
      const result1 = await handler(
        {
          item_number: 1,
          labels: ["bug"],
        },
        {}
      );
      expect(result1.success).toBe(true);

      // Second call succeeds
      const result2 = await handler(
        {
          item_number: 2,
          labels: ["enhancement"],
        },
        {}
      );
      expect(result2.success).toBe(true);

      // Third call should fail
      const result3 = await handler(
        {
          item_number: 3,
          labels: ["documentation"],
        },
        {}
      );
      expect(result3.success).toBe(false);
      expect(result3.error.includes("Max count")).toBe(true);
    });

    it("should filter labels based on allowed list", async () => {
      const handler = await main({
        allowed: ["bug", "enhancement"],
        max: 10,
      });

      const addLabelsCalls = [];
      mockGithub.rest.issues.addLabels = async params => {
        addLabelsCalls.push(params);
        return {};
      };

      const result = await handler(
        {
          item_number: 100,
          labels: ["bug", "invalid-label", "enhancement"],
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(result.labelsAdded).toEqual(["bug", "enhancement"]);
    });

    it("should handle empty labels array", async () => {
      const handler = await main({ max: 10 });

      const result = await handler(
        {
          item_number: 100,
          labels: [],
        },
        {}
      );

      expect(result.success).toBe(false);
      expect(result.error).toContain("No labels provided");
      expect(result.error).toContain("repository's available labels");
    });

    it("should handle missing labels field", async () => {
      const handler = await main({ max: 10 });

      const result = await handler(
        {
          item_number: 100,
        },
        {}
      );

      expect(result.success).toBe(false);
      expect(result.error).toContain("No labels provided");
      expect(result.error).toContain("repository's available labels");
    });

    it("should return allowed labels list when labels missing and allowed list configured", async () => {
      const handler = await main({
        allowed: ["bug", "enhancement", "documentation"],
        max: 10,
      });

      const result = await handler(
        {
          item_number: 100,
          labels: [],
        },
        {}
      );

      expect(result.success).toBe(false);
      expect(result.error).toContain("No labels provided");
      expect(result.error).toContain("allowed list");
      expect(result.error).toContain("bug");
      expect(result.error).toContain("enhancement");
      expect(result.error).toContain("documentation");
    });

    it("should handle API errors gracefully", async () => {
      const handler = await main({ max: 10 });

      mockGithub.rest.issues.addLabels = async () => {
        throw new Error("API Error: Not found");
      };

      const result = await handler(
        {
          item_number: 100,
          labels: ["bug"],
        },
        {}
      );

      expect(result.success).toBe(false);
      expect(result.error).toContain("API Error: Not found");
    });

    it("should deduplicate labels", async () => {
      const handler = await main({ max: 10 });
      const addLabelsCalls = [];

      mockGithub.rest.issues.addLabels = async params => {
        addLabelsCalls.push(params);
        return {};
      };

      const result = await handler(
        {
          item_number: 100,
          labels: ["bug", "bug", "enhancement", "bug"],
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(result.labelsAdded).toEqual(["bug", "enhancement"]);
    });

    it("should prefer the metadata-bearing entry when a duplicate label name appears", async () => {
      // Default (omitted issue_intent) accepts both strings and objects; deduplication favours the metadata-bearing entry.
      const handler = await main({ max: 10 });
      const graphqlMutationCalls = [];

      const originalGraphql = mockGithub.graphql;
      mockGithub.graphql = async (query, variables) => {
        if (typeof query === "string" && query.includes("updateIssue")) {
          graphqlMutationCalls.push(variables);
        }
        return originalGraphql(query, variables);
      };

      const result = await handler(
        {
          item_number: 100,
          // First "bug" has no metadata; second "bug" carries intent metadata — second should win
          labels: [{ name: "bug" }, { name: "bug", rationale: "Known crash path", confidence: "HIGH", suggest: true }],
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(result.labelsAdded).toEqual(["bug"]);
      // Intent metadata routes through the GraphQL mutation; the metadata-bearing spec wins
      expect(graphqlMutationCalls).toHaveLength(1);
      expect(graphqlMutationCalls[0].labels).toEqual([{ labelId: "LABEL_bug", rationale: "Known crash path", confidence: "HIGH", suggest: true }]);
    });

    it("should strip structured intent metadata when issue_intent is disabled", async () => {
      const handler = await main({ max: 10, issue_intent: false });
      const addLabelsCalls = [];

      mockGithub.rest.issues.addLabels = async params => {
        addLabelsCalls.push(params);
        return {};
      };

      const result = await handler(
        {
          item_number: 456,
          labels: [{ name: "bug", rationale: "Known crash path", confidence: "HIGH", suggest: true }],
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(addLabelsCalls).toHaveLength(1);
      expect(addLabelsCalls[0].labels).toEqual(["bug"]);
    });

    it("should forward per-label intent metadata via GraphQL when issue_intent is omitted", async () => {
      const handler = await main({ max: 10 });
      const graphqlMutationCalls = [];
      const addLabelsCalls = [];

      const originalGraphql = mockGithub.graphql;
      mockGithub.graphql = async (query, variables) => {
        if (typeof query === "string" && query.includes("updateIssue")) {
          graphqlMutationCalls.push(variables);
        }
        return originalGraphql(query, variables);
      };
      mockGithub.rest.issues.addLabels = async params => {
        addLabelsCalls.push(params);
        return {};
      };

      const result = await handler(
        {
          item_number: 456,
          labels: [{ name: "bug", rationale: "Known crash path", confidence: "HIGH", suggest: true }],
        },
        {}
      );

      expect(result.success).toBe(true);
      // Intent metadata is forwarded through GraphQL, not the REST addLabels endpoint
      expect(addLabelsCalls).toHaveLength(0);
      expect(graphqlMutationCalls).toHaveLength(1);
      expect(graphqlMutationCalls[0].labels).toEqual([{ labelId: "LABEL_bug", rationale: "Known crash path", confidence: "HIGH", suggest: true }]);
    });

    it("should accept plain string labels by default when issue_intent is omitted", async () => {
      const handler = await main({ max: 10 });
      const addLabelsCalls = [];

      mockGithub.rest.issues.addLabels = async params => {
        addLabelsCalls.push(params);
        return {};
      };

      const result = await handler(
        {
          item_number: 456,
          labels: ["bug", "enhancement"],
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(addLabelsCalls).toHaveLength(1);
      expect(addLabelsCalls[0].labels).toEqual(["bug", "enhancement"]);
    });

    it("should reject plain string labels when issue_intent is explicitly true (strict mode)", async () => {
      const handler = await main({ max: 10, issue_intent: true });

      const result = await handler(
        {
          item_number: 456,
          labels: ["bug", "enhancement"],
        },
        {}
      );

      expect(result.success).toBe(false);
      expect(result.error).toContain("Plain string label names are not permitted when issue_intent is explicitly enabled");
      expect(result.error).toContain('"bug"');
    });

    it("should reject label objects missing rationale or confidence in strict mode", async () => {
      const handler = await main({ max: 10, issue_intent: true });

      const result = await handler(
        {
          item_number: 456,
          labels: [{ name: "bug" }],
        },
        {}
      );

      expect(result.success).toBe(false);
      expect(result.error).toContain('both "rationale" and "confidence"');
      expect(result.error).toContain('"bug"');
    });

    it("should reject label object missing confidence in strict mode even when rationale is present", async () => {
      const handler = await main({ max: 10, issue_intent: true });

      const result = await handler(
        {
          item_number: 456,
          labels: [{ name: "bug", rationale: "Crash on upload" }],
        },
        {}
      );

      expect(result.success).toBe(false);
      expect(result.error).toContain('both "rationale" and "confidence"');
    });

    it("should accept label objects with both rationale and confidence in strict mode", async () => {
      const handler = await main({ max: 10, issue_intent: true });
      const graphqlMutationCalls = [];

      const originalGraphql = mockGithub.graphql;
      mockGithub.graphql = async (query, variables) => {
        if (typeof query === "string" && query.includes("updateIssue")) {
          graphqlMutationCalls.push(variables);
        }
        return originalGraphql(query, variables);
      };

      const result = await handler(
        {
          item_number: 456,
          labels: [{ name: "bug", rationale: "Crash on upload", confidence: "HIGH" }],
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(graphqlMutationCalls).toHaveLength(1);
      expect(graphqlMutationCalls[0].labels).toEqual([{ labelId: "LABEL_bug", rationale: "Crash on upload", confidence: "HIGH" }]);
    });

    it("should fall back to the REST add-labels endpoint for PRs when using issue_intent (pull_request field)", async () => {
      const handler = await main({ max: 10, target: "*", issue_intent: true });
      const graphqlMutationCalls = [];
      const addLabelsCalls = [];

      mockGithub.rest.issues.get = async () => ({
        data: {
          node_id: "PR_kwDOB7ZBY877o-t0",
          title: "Test PR title",
          labels: [],
          pull_request: { url: "https://api.github.com/repos/test-owner/test-repo/pulls/456" },
        },
      });

      mockGithub.rest.issues.addLabels = async params => {
        addLabelsCalls.push(params);
        return { data: (params.labels || []).map(name => ({ name })) };
      };

      const originalGraphql = mockGithub.graphql;
      mockGithub.graphql = async (query, variables) => {
        if (typeof query === "string" && (query.includes("updatePullRequest") || query.includes("updateIssue"))) {
          graphqlMutationCalls.push({ query, variables });
        }
        return originalGraphql(query, variables);
      };

      const result = await handler(
        {
          item_number: 456,
          labels: [{ name: "security:medium", rationale: "CVE found", confidence: "HIGH" }],
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(result.labelsAdded).toEqual(["security:medium"]);
      expect(result.after_state.labels).toEqual(["security:medium"]);
      expect(graphqlMutationCalls).toHaveLength(0);
      expect(addLabelsCalls).toHaveLength(1);
      expect(addLabelsCalls[0].issue_number).toBe(456);
      expect(addLabelsCalls[0].labels).toEqual(["security:medium"]);
    });

    it("should fall back to the REST add-labels endpoint for PRs when node_id starts with PR_", async () => {
      const handler = await main({ max: 10, target: "*", issue_intent: true });
      const graphqlMutationCalls = [];
      const addLabelsCalls = [];

      // PR without pull_request field (detected by node_id prefix)
      mockGithub.rest.issues.get = async () => ({
        data: {
          node_id: "PR_kwDOABC123",
          title: "Test PR",
          labels: [{ name: "bug" }],
        },
      });

      mockGithub.rest.issues.addLabels = async params => {
        addLabelsCalls.push(params);
        return { data: (params.labels || []).map(name => ({ name })) };
      };

      const originalGraphql = mockGithub.graphql;
      mockGithub.graphql = async (query, variables) => {
        if (typeof query === "string" && (query.includes("updatePullRequest") || query.includes("updateIssue"))) {
          graphqlMutationCalls.push({ query, variables });
        }
        return originalGraphql(query, variables);
      };

      const result = await handler(
        {
          item_number: 789,
          labels: [{ name: "enhancement", rationale: "Improves UX", confidence: "MEDIUM" }],
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(result.labelsAdded).toEqual(["enhancement"]);
      expect(result.after_state.labels).toEqual(["enhancement"]);
      expect(graphqlMutationCalls).toHaveLength(0);
      expect(addLabelsCalls).toHaveLength(1);
      expect(addLabelsCalls[0].issue_number).toBe(789);
      expect(addLabelsCalls[0].labels).toEqual(["enhancement"]);
    });

    it("should use the updateIssue intent mutation for regular issues (ISSUE_ node_id)", async () => {
      const handler = await main({ max: 10, issue_intent: true });
      const updateIssueCalls = [];
      const updatePRCalls = [];

      // Explicitly set up a regular issue fixture (no pull_request field, non-PR_ node_id)
      // so this test is resilient to changes in the shared default mock.
      mockGithub.rest.issues.get = async () => ({
        data: {
          node_id: "ISSUE_kwDOB7ZBY877o-t0",
          title: "Regular issue",
          labels: [{ name: "bug" }],
        },
      });

      const originalGraphql = mockGithub.graphql;
      mockGithub.graphql = async (query, variables) => {
        if (typeof query === "string" && query.includes("updateIssue")) {
          updateIssueCalls.push(variables);
        }
        if (typeof query === "string" && query.includes("updatePullRequest")) {
          updatePRCalls.push(variables);
        }
        return originalGraphql(query, variables);
      };

      const result = await handler(
        {
          item_number: 456,
          labels: [{ name: "enhancement", rationale: "Improves the issue", confidence: "HIGH" }],
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(updateIssueCalls).toHaveLength(1);
      expect(updatePRCalls).toHaveLength(0);
    });

    it("should sanitize and trim label names", async () => {
      const handler = await main({ max: 10 });
      const addLabelsCalls = [];

      mockGithub.rest.issues.addLabels = async params => {
        addLabelsCalls.push(params);
        return {};
      };

      const result = await handler(
        {
          item_number: 100,
          labels: ["  bug  ", " enhancement ", "documentation"],
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(result.labelsAdded.length).toBeGreaterThan(0);
    });

    it("should use spread operator for context.repo", async () => {
      const handler = await main({ max: 10 });
      const addLabelsCalls = [];

      mockGithub.rest.issues.addLabels = async params => {
        addLabelsCalls.push(params);
        return {};
      };

      await handler(
        {
          item_number: 100,
          labels: ["bug"],
        },
        {}
      );

      expect(addLabelsCalls[0].owner).toBe("test-owner");
      expect(addLabelsCalls[0].repo).toBe("test-repo");
    });

    it("should support target-repo from config", async () => {
      const handler = await main({
        max: 10,
        "target-repo": "external-org/external-repo",
      });
      const addLabelsCalls = [];

      mockGithub.rest.issues.addLabels = async params => {
        addLabelsCalls.push(params);
        return {};
      };

      const result = await handler(
        {
          item_number: 100,
          labels: ["bug"],
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(addLabelsCalls[0].owner).toBe("external-org");
      expect(addLabelsCalls[0].repo).toBe("external-repo");
    });

    it("should support repo field in message for cross-repository operations", async () => {
      const handler = await main({
        max: 10,
        "target-repo": "default-org/default-repo",
        allowed_repos: ["cross-org/cross-repo"],
      });
      const addLabelsCalls = [];

      mockGithub.rest.issues.addLabels = async params => {
        addLabelsCalls.push(params);
        return {};
      };

      const result = await handler(
        {
          item_number: 456,
          labels: ["enhancement"],
          repo: "cross-org/cross-repo",
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(addLabelsCalls[0].owner).toBe("cross-org");
      expect(addLabelsCalls[0].repo).toBe("cross-repo");
    });

    it("should reject repo not in allowed-repos list", async () => {
      const handler = await main({
        max: 10,
        "target-repo": "default-org/default-repo",
        allowed_repos: ["allowed-org/allowed-repo"],
      });

      const result = await handler(
        {
          item_number: 100,
          labels: ["bug"],
          repo: "unauthorized-org/unauthorized-repo",
        },
        {}
      );

      expect(result.success).toBe(false);
      expect(result.error).toContain("not in the allowed-repos list");
    });

    it("should qualify bare repo name with default repo org", async () => {
      const handler = await main({
        max: 10,
        "target-repo": "github/default-repo",
        allowed_repos: ["github/gh-aw"],
      });
      const addLabelsCalls = [];

      mockGithub.rest.issues.addLabels = async params => {
        addLabelsCalls.push(params);
        return {};
      };

      const result = await handler(
        {
          item_number: 100,
          labels: ["bug"],
          repo: "gh-aw", // Bare name without org
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(addLabelsCalls[0].owner).toBe("github");
      expect(addLabelsCalls[0].repo).toBe("gh-aw");
    });

    it("should enforce max limit on labels per operation", async () => {
      const handler = await main({ max: 10 });

      // Try to add more than MAX_LABELS (10)
      const result = await handler(
        {
          item_number: 100,
          labels: [
            "label1",
            "label2",
            "label3",
            "label4",
            "label5",
            "label6",
            "label7",
            "label8",
            "label9",
            "label10",
            "label11", // 11th label exceeds limit
          ],
        },
        {}
      );

      expect(result.success).toBe(false);
      expect(result.error).toContain("E003");
      expect(result.error).toContain("Cannot add more than 10 labels");
      expect(result.error).toContain("received 11");
    });

    it("should resolve temporary ID in item_number to real issue number", async () => {
      const handler = await main({ max: 10, target: "*" });
      const addLabelsCalls = [];

      mockGithub.rest.issues.addLabels = async params => {
        addLabelsCalls.push(params);
        return {};
      };

      const result = await handler(
        {
          item_number: "aw_report1",
          labels: ["bug"],
        },
        { aw_report1: { repo: "test-owner/test-repo", number: 42 } }
      );

      expect(result.success).toBe(true);
      expect(result.number).toBe(42);
      expect(addLabelsCalls.length).toBe(1);
      expect(addLabelsCalls[0].issue_number).toBe(42);
    });

    it("should defer when item_number is an unresolved temporary ID", async () => {
      const handler = await main({ max: 10, target: "*" });

      const result = await handler(
        {
          item_number: "aw_report1",
          labels: ["bug"],
        },
        {}
      );

      expect(result.success).toBe(false);
      expect(result.deferred).toBe(true);
      expect(result.error).toContain("aw_report1");
    });

    it("should resolve temporary ID with hash prefix in item_number", async () => {
      const handler = await main({ max: 10, target: "*" });
      const addLabelsCalls = [];

      mockGithub.rest.issues.addLabels = async params => {
        addLabelsCalls.push(params);
        return {};
      };

      const result = await handler(
        {
          item_number: "#aw_report1",
          labels: ["enhancement"],
        },
        { aw_report1: { repo: "test-owner/test-repo", number: 99 } }
      );

      expect(result.success).toBe(true);
      expect(result.number).toBe(99);
      expect(addLabelsCalls[0].issue_number).toBe(99);
    });

    it("should preview labels in staged mode without calling API", async () => {
      const handler = await main({ max: 10, target: "*", staged: true });
      const addLabelsCalls = [];

      mockGithub.rest.issues.addLabels = async params => {
        addLabelsCalls.push(params);
        return {};
      };

      const result = await handler(
        {
          item_number: 100,
          labels: ["bug", "enhancement"],
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(result.staged).toBe(true);
      expect(result.previewInfo).toBeDefined();
      expect(result.previewInfo.number).toBe(100);
      expect(result.previewInfo.labels).toEqual(["bug", "enhancement"]);
      expect(addLabelsCalls.length).toBe(0);
    });

    it("should count staged calls toward processedCount", async () => {
      const handler = await main({ max: 1, staged: true });

      const result1 = await handler({ item_number: 1, labels: ["bug"] }, {});
      expect(result1.success).toBe(true);
      expect(result1.staged).toBe(true);

      const result2 = await handler({ item_number: 2, labels: ["enhancement"] }, {});
      expect(result2.success).toBe(false);
      expect(result2.error).toContain("Max count");
    });

    it("should filter out labels matching blocked patterns", async () => {
      const handler = await main({
        max: 10,
        blocked: ["internal-*", "~*"],
      });
      const addLabelsCalls = [];

      mockGithub.rest.issues.addLabels = async params => {
        addLabelsCalls.push(params);
        return {};
      };

      const result = await handler(
        {
          item_number: 100,
          labels: ["bug", "internal-only", "~secret", "enhancement"],
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(result.labelsAdded).toEqual(["bug", "enhancement"]);
      expect(addLabelsCalls[0].labels).toEqual(["bug", "enhancement"]);
    });

    it("should succeed with empty labelsAdded when all labels filtered by allowed list", async () => {
      const handler = await main({
        max: 10,
        allowed: ["bug", "enhancement"],
      });

      const result = await handler(
        {
          item_number: 100,
          labels: ["documentation", "invalid-label"],
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(result.labelsAdded).toEqual([]);
      expect(result.message).toContain("No valid labels");
    });

    it("should use default max=10 when config.max is not provided", async () => {
      // No max provided - defaults to 10 via ?? operator
      const handler = await main({});
      const result = await handler({ item_number: 1, labels: ["bug"] }, {});
      expect(result.success).toBe(true);
    });

    it("should handle labels array containing only whitespace strings gracefully", async () => {
      const handler = await main({ max: 10 });

      const result = await handler(
        {
          item_number: 100,
          labels: ["   ", "\t"],
        },
        {}
      );

      // Whitespace-only labels are sanitized away, resulting in no labels added
      expect(result.success).toBe(true);
      expect(result.labelsAdded).toEqual([]);
    });

    it("should log initialization info without allowed/blocked when not configured", async () => {
      await main({ max: 5 });
      // Should not log allowed/blocked info when not configured
      expect(mockCore.infos.some(msg => msg.includes("Allowed labels:"))).toBe(false);
      expect(mockCore.infos.some(msg => msg.includes("Blocked patterns:"))).toBe(false);
      expect(mockCore.infos.some(msg => msg.includes("max=5"))).toBe(true);
    });

    it("should succeed with empty labelsAdded when all labels are blocked by patterns", async () => {
      const handler = await main({
        max: 10,
        blocked: ["*"],
      });

      const addLabelsCalls = [];
      mockGithub.rest.issues.addLabels = async params => {
        addLabelsCalls.push(params);
        return {};
      };

      const result = await handler(
        {
          item_number: 100,
          labels: ["bug", "enhancement"],
        },
        {}
      );

      // All labels blocked → treated as "no valid labels"
      expect(result.success).toBe(true);
      expect(result.labelsAdded).toEqual([]);
      expect(result.message).toContain("No valid labels");
      expect(addLabelsCalls.length).toBe(0);
    });

    it("should reject labels starting with '-' (removal attempt)", async () => {
      const handler = await main({ max: 10 });

      const result = await handler(
        {
          item_number: 100,
          labels: ["-bug"],
        },
        {}
      );

      expect(result.success).toBe(false);
      expect(result.error).toContain("Label removal is not permitted");
    });

    it("should truncate labels longer than 64 characters", async () => {
      const handler = await main({ max: 10 });
      const addLabelsCalls = [];

      mockGithub.rest.issues.addLabels = async params => {
        addLabelsCalls.push(params);
        return {};
      };

      const longLabel = "a".repeat(80);
      const result = await handler(
        {
          item_number: 100,
          labels: [longLabel],
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(result.labelsAdded[0].length).toBe(64);
    });

    it("should handle numeric string from context payload correctly", async () => {
      const handler = await main({ max: 10 });
      const addLabelsCalls = [];

      mockContext.payload = {
        issue: {
          number: "123", // String number from payload
        },
      };

      mockGithub.rest.issues.addLabels = async params => {
        addLabelsCalls.push(params);
        return {};
      };

      const result = await handler(
        {
          labels: ["bug"],
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(addLabelsCalls).toHaveLength(1);
    });

    it("should reject invalid non-numeric value from context", async () => {
      const handler = await main({ max: 10 });

      mockContext.payload = {
        issue: {
          number: "not-a-number",
        },
      };

      const result = await handler(
        {
          labels: ["bug"],
        },
        {}
      );

      expect(result.success).toBe(false);
      expect(result.error).toContain("No issue/PR number available");
    });

    it("should skip when item does not have all required_labels", async () => {
      const handler = await main({ max: 10, required_labels: ["needs-triage"] });

      mockGithub.rest.issues.get = async () => ({
        data: { title: "Some issue", labels: [{ name: "bug" }] },
      });

      const result = await handler({ item_number: 100, labels: ["enhancement"] }, {});

      expect(result.success).toBe(false);
      expect(result.skipped).toBe(true);
      expect(result.error).toContain("required-labels");
    });

    it("should add labels when item has all required_labels", async () => {
      const handler = await main({ max: 10, required_labels: ["needs-triage"] });
      const addLabelsCalls = [];

      mockGithub.rest.issues.get = async () => ({
        data: { title: "Some issue", labels: [{ name: "needs-triage" }, { name: "bug" }] },
      });
      mockGithub.rest.issues.addLabels = async params => {
        addLabelsCalls.push(params);
        return {};
      };

      const result = await handler({ item_number: 100, labels: ["enhancement"] }, {});

      expect(result.success).toBe(true);
      expect(addLabelsCalls.length).toBe(1);
    });

    it("should skip when item title does not start with required_title_prefix", async () => {
      const handler = await main({ max: 10, required_title_prefix: "[Bot]" });

      mockGithub.rest.issues.get = async () => ({
        data: { title: "Regular issue title", labels: [] },
      });

      const result = await handler({ item_number: 100, labels: ["bug"] }, {});

      expect(result.success).toBe(false);
      expect(result.skipped).toBe(true);
      expect(result.error).toContain("required prefix");
    });

    it("should add labels when item title starts with required_title_prefix", async () => {
      const handler = await main({ max: 10, required_title_prefix: "[Bot]" });
      const addLabelsCalls = [];

      mockGithub.rest.issues.get = async () => ({
        data: { title: "[Bot] Automated issue", labels: [] },
      });
      mockGithub.rest.issues.addLabels = async params => {
        addLabelsCalls.push(params);
        return {};
      };

      const result = await handler({ item_number: 100, labels: ["automation"] }, {});

      expect(result.success).toBe(true);
      expect(addLabelsCalls.length).toBe(1);
    });

    it("should check both required_labels and required_title_prefix together", async () => {
      const handler = await main({
        max: 10,
        required_labels: ["approved"],
        required_title_prefix: "[Ready]",
      });

      // Passes required_labels but fails required_title_prefix
      mockGithub.rest.issues.get = async () => ({
        data: { title: "Not ready issue", labels: [{ name: "approved" }] },
      });

      const result = await handler({ item_number: 100, labels: ["bug"] }, {});

      expect(result.success).toBe(false);
      expect(result.skipped).toBe(true);
      expect(result.error).toContain("required prefix");
    });

    it("should add labels when both required_labels and required_title_prefix match", async () => {
      const handler = await main({
        max: 10,
        required_labels: ["approved"],
        required_title_prefix: "[Ready]",
      });
      const addLabelsCalls = [];

      mockGithub.rest.issues.get = async () => ({
        data: { title: "[Ready] Ship it", labels: [{ name: "approved" }, { name: "bug" }] },
      });
      mockGithub.rest.issues.addLabels = async params => {
        addLabelsCalls.push(params);
        return {};
      };

      const result = await handler({ item_number: 100, labels: ["enhancement"] }, {});

      expect(result.success).toBe(true);
      expect(addLabelsCalls.length).toBe(1);
    });

    describe("create-if-missing", () => {
      it("should fail with a clear error mentioning create-if-missing when a label does not exist", async () => {
        const handler = await main({ max: 10, issue_intent: true });

        const result = await handler(
          {
            item_number: 456,
            labels: [{ name: "new-label", rationale: "Needs triage", confidence: "HIGH" }],
          },
          {}
        );

        expect(result.success).toBe(false);
        expect(result.error).toContain('Label "new-label" not found');
        expect(result.error).toContain("create-if-missing");
      });

      it("should not create labels when create_if_missing is false (default)", async () => {
        const handler = await main({ max: 10, issue_intent: true });
        const createLabelCalls = [];
        mockGithub.rest.issues.createLabel = async params => {
          createLabelCalls.push(params);
          return { data: { name: params.name } };
        };

        const result = await handler(
          {
            item_number: 456,
            labels: [{ name: "new-label", rationale: "Needs triage", confidence: "HIGH" }],
          },
          {}
        );

        expect(result.success).toBe(false);
        expect(createLabelCalls.length).toBe(0);
      });

      it("should create a missing label and apply it when create_if_missing is true (issue-intent path)", async () => {
        const handler = await main({ max: 10, issue_intent: true, create_if_missing: true });
        const createLabelCalls = [];
        mockGithub.rest.issues.createLabel = async params => {
          createLabelCalls.push(params);
          mockGithub._repoLabels = [...(mockGithub._repoLabels || ["bug", "enhancement", "documentation", "security:low", "security:medium", "security:high"]), params.name];
          return { data: { name: params.name } };
        };

        const graphqlMutationCalls = [];
        const originalGraphql = mockGithub.graphql;
        mockGithub.graphql = async (query, variables) => {
          if (typeof query === "string" && query.includes("updateIssue")) {
            graphqlMutationCalls.push(variables);
          }
          return originalGraphql(query, variables);
        };

        const result = await handler(
          {
            item_number: 456,
            labels: [{ name: "new-label", rationale: "Needs triage", confidence: "HIGH" }],
          },
          {}
        );

        expect(result.success).toBe(true);
        expect(createLabelCalls.length).toBe(1);
        expect(createLabelCalls[0].name).toBe("new-label");
        expect(graphqlMutationCalls).toHaveLength(1);
        expect(graphqlMutationCalls[0].labels).toEqual([{ labelId: "LABEL_new-label", rationale: "Needs triage", confidence: "HIGH" }]);
      });

      it("should create a missing label and apply it when create_if_missing is true (plain REST path)", async () => {
        const handler = await main({ max: 10, create_if_missing: true });
        const createLabelCalls = [];
        mockGithub.rest.issues.createLabel = async params => {
          createLabelCalls.push(params);
          mockGithub._repoLabels = [...(mockGithub._repoLabels || ["bug", "enhancement", "documentation", "security:low", "security:medium", "security:high"]), params.name];
          return { data: { name: params.name } };
        };
        const addLabelsCalls = [];
        mockGithub.rest.issues.addLabels = async params => {
          addLabelsCalls.push(params);
          return { data: params.labels };
        };

        const result = await handler(
          {
            item_number: 456,
            labels: ["good first issue", "needs investigation"],
          },
          {}
        );

        expect(result.success).toBe(true);
        expect(createLabelCalls.map(c => c.name).sort()).toEqual(["good first issue", "needs investigation"]);
        expect(addLabelsCalls.length).toBe(1);
        expect(addLabelsCalls[0].labels).toEqual(["good first issue", "needs investigation"]);
      });

      it("should treat a 422 from createLabel as already-existing and continue", async () => {
        const handler = await main({ max: 10, create_if_missing: true });
        mockGithub.rest.issues.createLabel = async () => {
          const err = new Error("Label already exists");
          /** @type {any} */ err.status = 422;
          throw err;
        };
        const addLabelsCalls = [];
        mockGithub.rest.issues.addLabels = async params => {
          addLabelsCalls.push(params);
          return { data: params.labels };
        };

        const result = await handler(
          {
            item_number: 456,
            labels: ["brand-new-label"],
          },
          {}
        );

        expect(result.success).toBe(true);
        expect(addLabelsCalls.length).toBe(1);
      });
    });
  });
});
