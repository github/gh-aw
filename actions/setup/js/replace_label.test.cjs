// @ts-check
import { describe, it, expect, beforeEach, vi } from "vitest";
const { main } = require("./replace_label.cjs");

describe("replace_label", () => {
  let mockCore;
  let mockGithub;
  let mockContext;

  beforeEach(() => {
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
      rest: {
        issues: {
          get: async () => ({
            data: {
              title: "Test issue title",
              labels: [
                { name: "in-progress", node_id: "LA_in_progress_123" },
                { name: "bug", node_id: "LA_bug_456" },
              ],
              node_id: "I_issue_789",
            },
          }),
          setLabels: async ({ labels }) => ({
            data: labels.map(name => ({ name, node_id: `LA_${name}_id` })),
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
          number: 42,
        },
      },
    };

    global.core = mockCore;
    global.github = mockGithub;
    global.context = mockContext;
  });

  describe("runtime target authorization", () => {
    const message = { label_to_remove: "in-progress", label_to_add: "done", item_number: 99 };

    it("T-RL-015 ignores a conflicting item_number when target is triggering", async () => {
      const setLabelsCalls = [];
      mockGithub.rest.issues.setLabels = async params => {
        setLabelsCalls.push(params);
        return { data: params.labels.map(name => ({ name })) };
      };
      const handler = await main({ target: "triggering" });

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(result.number).toBe(42);
      expect(setLabelsCalls[0].issue_number).toBe(42);
    });

    it("T-RL-014 defaults to the triggering item when target is omitted", async () => {
      const setLabelsCalls = [];
      mockGithub.rest.issues.setLabels = async params => {
        setLabelsCalls.push(params);
        return { data: params.labels.map(name => ({ name })) };
      };
      const handler = await main({});

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(result.number).toBe(42);
      expect(setLabelsCalls[0].issue_number).toBe(42);
    });

    it("T-RL-016 ignores a conflicting item_number when target is a fixed number", async () => {
      const setLabelsCalls = [];
      mockGithub.rest.issues.setLabels = async params => {
        setLabelsCalls.push(params);
        return { data: params.labels.map(name => ({ name })) };
      };
      const handler = await main({ target: "123" });

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(result.number).toBe(123);
      expect(setLabelsCalls[0].issue_number).toBe(123);
    });

    it("T-RL-017 uses a fixed numeric target without an item_number", async () => {
      const setLabelsCalls = [];
      mockGithub.rest.issues.setLabels = async params => {
        setLabelsCalls.push(params);
        return { data: params.labels.map(name => ({ name })) };
      };
      const handler = await main({ target: "123" });

      const result = await handler({ label_to_remove: "in-progress", label_to_add: "done" }, {});

      expect(result.success).toBe(true);
      expect(result.number).toBe(123);
      expect(setLabelsCalls[0].issue_number).toBe(123);
    });

    it.each([-1, 1.5, Infinity])("rejects invalid fixed target %s", async target => {
      const handler = await main({ target });
      const setLabelsCalls = [];
      mockGithub.rest.issues.setLabels = async params => {
        setLabelsCalls.push(params);
        return { data: [] };
      };

      const result = await handler({ label_to_remove: "in-progress", label_to_add: "done" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toBe("Invalid issue/PR number");
      expect(setLabelsCalls).toHaveLength(0);
    });

    it("T-RL-018 accepts item_number when target is wildcard", async () => {
      const setLabelsCalls = [];
      mockGithub.rest.issues.setLabels = async params => {
        setLabelsCalls.push(params);
        return { data: params.labels.map(name => ({ name })) };
      };
      const handler = await main({ target: "*" });

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(result.number).toBe(99);
      expect(setLabelsCalls[0].issue_number).toBe(99);
    });
  });

  it("should replace label when both labels are valid", async () => {
    const handler = await main({ allowed_add: [], allowed_remove: [], blocked: [] });
    const result = await handler({ label_to_remove: "in-progress", label_to_add: "done" }, {});

    expect(result.success).toBe(true);
    expect(result.labelRemoved).toBe("in-progress");
    expect(result.labelAdded).toBe("done");
  });

  it("should return error when label_to_add does not exist in the repo", async () => {
    mockGithub.rest.issues.setLabels = async () => {
      const err = new Error("Validation Failed");
      err.status = 422;
      throw err;
    };

    const handler = await main({});
    const result = await handler({ label_to_remove: "in-progress", label_to_add: "needs-review" }, {});

    expect(result.success).toBe(false);
  });

  it("should succeed even when label_to_remove is not present on the issue", async () => {
    mockGithub.rest.issues.get = async () => ({
      data: {
        title: "Test issue",
        labels: [{ name: "bug", node_id: "LA_bug_456" }], // "in-progress" not present
        node_id: "I_issue_789",
      },
    });

    const handler = await main({});
    const result = await handler({ label_to_remove: "in-progress", label_to_add: "done" }, {});

    expect(result.success).toBe(true);
    expect(result.labelRemoved).toBeNull();
    expect(result.labelAdded).toBe("done");
  });

  it("should return error when label_to_remove is missing", async () => {
    const handler = await main({});
    // @ts-ignore - testing missing field
    const result = await handler({ label_to_add: "done" }, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("label_to_remove");
  });

  it("should return error when label_to_add is missing", async () => {
    const handler = await main({});
    // @ts-ignore - testing missing field
    const result = await handler({ label_to_remove: "in-progress" }, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("label_to_add");
  });

  it("should reject label_to_add that is not in allowed-add list", async () => {
    const handler = await main({ allowed_add: ["approved", "done"] });
    const result = await handler({ label_to_remove: "in-progress", label_to_add: "wontfix" }, {});

    expect(result.success).toBe(false);
  });

  it("should reject label_to_remove that is not in allowed-remove list", async () => {
    const handler = await main({ allowed_remove: ["in-progress", "review-needed"] });
    const result = await handler({ label_to_remove: "bug", label_to_add: "done" }, {});

    expect(result.success).toBe(false);
  });

  it("should reject labels matching blocked patterns", async () => {
    const handler = await main({ blocked: ["~*"] });
    const result = await handler({ label_to_remove: "in-progress", label_to_add: "~internal" }, {});

    expect(result.success).toBe(false);
  });

  it("should reject label_to_add before setLabels when blocklist changes mid-flight", async () => {
    let setLabelsCalls = 0;
    let getCalls = 0;
    const config = { allowed_add: ["done"], blocked: [] };
    mockGithub.rest.issues.get = async () => {
      getCalls++;
      if (getCalls === 2) {
        config.blocked = ["done"];
      }
      return {
        data: {
          title: "Test issue title",
          labels: [
            { name: "in-progress", node_id: "LA_in_progress_123" },
            { name: "bug", node_id: "LA_bug_456" },
          ],
          node_id: "I_issue_789",
        },
      };
    };
    mockGithub.rest.issues.setLabels = async () => {
      setLabelsCalls++;
      return { data: [] };
    };

    const handler = await main(config);
    const result = await handler({ label_to_remove: "in-progress", label_to_add: "done" }, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("blocked pattern");
    expect(getCalls).toBe(2);
    expect(config.blocked).toEqual(["done"]);
    expect(setLabelsCalls).toBe(0);
  });

  it("should skip when required-labels filter does not match", async () => {
    const handler = await main({ required_labels: ["approved"] });
    // Issue has "in-progress" and "bug" but not "approved"
    const result = await handler({ label_to_remove: "in-progress", label_to_add: "done" }, {});

    expect(result.success).toBe(false);
    expect(result.skipped).toBe(true);
  });

  it("should skip when required-title-prefix does not match", async () => {
    const handler = await main({ required_title_prefix: "[BUG]" });
    // Issue title is "Test issue title", does not start with "[BUG]"
    const result = await handler({ label_to_remove: "in-progress", label_to_add: "done" }, {});

    expect(result.success).toBe(false);
    expect(result.skipped).toBe(true);
  });

  it("should return staged result when in staged mode", async () => {
    const handler = await main({ staged: true });
    const result = await handler({ label_to_remove: "in-progress", label_to_add: "done" }, {});

    expect(result.success).toBe(true);
    expect(result.staged).toBe(true);
    expect(result.previewInfo?.labelToRemove).toBe("in-progress");
    expect(result.previewInfo?.labelToAdd).toBe("done");
  });

  it("should return error when no item number is available", async () => {
    global.context = {
      repo: { owner: "test-owner", repo: "test-repo" },
      payload: {}, // no issue or pull_request in payload
    };

    const handler = await main({});
    const result = await handler({ label_to_remove: "in-progress", label_to_add: "done" }, {});

    expect(result.success).toBe(false);
  });

  it("should return error when setLabels API call fails", async () => {
    mockGithub.rest.issues.setLabels = async () => {
      throw new Error("Service unavailable");
    };

    const handler = await main({});
    const result = await handler({ label_to_remove: "in-progress", label_to_add: "done" }, {});

    expect(result.success).toBe(false);
  });

  it("should reject a successful setLabels response that omits label_to_add", async () => {
    mockGithub.rest.issues.setLabels = async () => ({
      data: [{ name: "bug" }],
    });

    const handler = await main({});
    const result = await handler({ label_to_remove: "in-progress", label_to_add: "done" }, {});

    expect(result).toEqual({
      success: false,
      error: 'replace_label: label_to_add "done" not found in POST-setLabels response',
    });
    expect(mockCore.errors).toContain(result.error);
  });

  it("should reject a successful setLabels response that retains label_to_remove", async () => {
    mockGithub.rest.issues.setLabels = async () => ({
      data: [{ name: "in-progress" }, { name: "bug" }, { name: "done" }],
    });

    const handler = await main({});
    const result = await handler({ label_to_remove: "in-progress", label_to_add: "done" }, {});

    expect(result).toEqual({
      success: false,
      error: 'replace_label: label_to_remove "in-progress" still present after setLabels call',
    });
    expect(mockCore.errors).toContain(result.error);
  });

  describe("allowed-transitions", () => {
    it("should allow a transition that is in the allowed-transitions list", async () => {
      const handler = await main({
        allowed_transitions: [
          { from: "in-progress", to: "done" },
          { from: "pending", to: "in-progress" },
        ],
      });
      const result = await handler({ label_to_remove: "in-progress", label_to_add: "done" }, {});

      expect(result.success).toBe(true);
    });

    it("should reject a transition that is not in the allowed-transitions list", async () => {
      const handler = await main({
        allowed_transitions: [{ from: "in-progress", to: "done" }],
      });
      const result = await handler({ label_to_remove: "in-progress", label_to_add: "rejected" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain('"in-progress" → "rejected"');
      expect(result.error).toContain("allowed-transitions");
    });

    it("should reject a reversed transition when only forward direction is listed", async () => {
      const handler = await main({
        allowed_transitions: [{ from: "in-progress", to: "done" }],
      });
      // Reversed: done → in-progress is NOT in the list
      const result = await handler({ label_to_remove: "done", label_to_add: "in-progress" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("allowed-transitions");
    });

    it("should enforce both allowed-transitions and blocked patterns", async () => {
      const handler = await main({
        allowed_transitions: [{ from: "in-progress", to: "~internal" }],
        blocked: ["~*"],
      });
      // Even though the transition is listed, the blocked pattern must reject it first
      const result = await handler({ label_to_remove: "in-progress", label_to_add: "~internal" }, {});

      expect(result.success).toBe(false);
    });

    it("should allow any transition when allowed-transitions is empty", async () => {
      const handler = await main({ allowed_transitions: [] });
      const result = await handler({ label_to_remove: "in-progress", label_to_add: "done" }, {});

      expect(result.success).toBe(true);
    });
  });
});
