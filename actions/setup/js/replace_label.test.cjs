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
      eventName: "issues",
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

    it("skips when the triggering context has no issue or pull request", async () => {
      mockContext.eventName = "workflow_dispatch";
      mockContext.payload = {};
      const setLabelsCalls = [];
      mockGithub.rest.issues.setLabels = async params => {
        setLabelsCalls.push(params);
        return { data: [] };
      };
      const handler = await main({});

      const result = await handler({ label_to_remove: "in-progress", label_to_add: "done" }, {});

      expect(result.success).toBe(false);
      expect(result.skipped).toBe(true);
      expect(result.error).toContain('Target is "triggering" but not running in issue or pull request context');
      expect(mockCore.warnings).toContain(result.error);
      expect(mockCore.errors).toHaveLength(0);
      expect(setLabelsCalls).toHaveLength(0);
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

  it("T-RL-045 uses the current REST label state when label_to_remove is absent", async () => {
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
    expect(mockCore.infos).toContain('Label "in-progress" is not present on issue #42 in test-owner/test-repo — will only add "done"');
    expect(mockCore.infos).toContain('Successfully replaced label "in-progress" → "done" on issue #42 in test-owner/test-repo');
    expect(mockCore.infos).toContain('Updated labels: ["bug","done"]');
  });

  it("should return error when label_to_remove is missing", async () => {
    const handler = await main({});
    // @ts-ignore - testing missing field
    const result = await handler({ label_to_add: "done" }, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("label_to_remove");
    expect(mockCore.warnings).toContain(result.error);
    expect(mockCore.errors).toHaveLength(0);
  });

  it("should return error when label_to_add is missing", async () => {
    const handler = await main({});
    // @ts-ignore - testing missing field
    const result = await handler({ label_to_remove: "in-progress" }, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("label_to_add");
    expect(mockCore.warnings).toContain(result.error);
    expect(mockCore.errors).toHaveLength(0);
  });

  it("should reject label_to_add that is not in allowed-add list", async () => {
    const handler = await main({ allowed_add: ["approved", "done"] });
    const result = await handler({ label_to_remove: "in-progress", label_to_add: "wontfix" }, {});

    expect(result.success).toBe(false);
    expect(mockCore.warnings).toContain(`label_to_add validation failed: ${result.error}`);
    expect(mockCore.errors).toHaveLength(0);
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
    expect(mockCore.warnings).toContain(`label_to_add validation failed: ${result.error}`);
    expect(mockCore.errors).toHaveLength(0);
  });

  it.each([
    ["case variation", "BlOcKeD"],
    ["zero-width character", "block\u200Bed"],
    ["full-width characters", "ｂｌｏｃｋｅｄ"],
  ])("should reject blocked labels with %s", async (_variant, label) => {
    const handler = await main({ blocked: ["blocked"] });
    const result = await handler({ label_to_remove: "in-progress", label_to_add: label }, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("blocked pattern");
  });

  it.each([
    ["ｂｌｏｃｋｅｄ", "blocked"],
    ["Ｂｌｏｃｋ*", "blocked-label"],
    ["Β", "β"],
    ["Β*", "β-label"],
    ["＊", "*"],
    ["&#42;", "*"],
    ["prefix＊*", "prefix*tail"],
  ])("should reject blocked pattern %s with label %s after matching or normalization", async (pattern, label) => {
    const handler = await main({ blocked: [pattern] });
    const result = await handler({ label_to_remove: label, label_to_add: "done" }, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("label_to_remove");
    expect(result.error).toContain("blocked pattern");
  });

  it.each(["＊", "&#42;", "prefix＊suffix", "prefix&#42;suffix", "prefix＊*"])("should keep normalized asterisks literal in blocked pattern %s", async pattern => {
    const handler = await main({ blocked: [pattern] });
    const result = await handler({ label_to_remove: "in-progress", label_to_add: "prefix-any-suffix" }, {});

    expect(result.success).toBe(true);
  });

  it("T-RL-026 rechecks blocklist state before setLabels", async () => {
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
    expect(mockCore.warnings).toContain("Skipping replace_label for issue #42: does not match required-labels filter (approved)");
    expect(mockCore.errors).toHaveLength(0);
  });

  it("should skip when required-title-prefix does not match", async () => {
    const handler = await main({ required_title_prefix: "[BUG]" });
    // Issue title is "Test issue title", does not start with "[BUG]"
    const result = await handler({ label_to_remove: "in-progress", label_to_add: "done" }, {});

    expect(result.success).toBe(false);
    expect(result.skipped).toBe(true);
    expect(mockCore.warnings).toContain('Skipping replace_label for issue #42: title does not start with required prefix "[BUG]"');
    expect(mockCore.errors).toHaveLength(0);
  });

  it("warning-only skips enforce the count gate and repository allowlist", async () => {
    const countGatedHandler = await main({ max: 1 });
    await countGatedHandler({ label_to_remove: "in-progress", label_to_add: "done" }, {});
    const countResult = await countGatedHandler({ label_to_remove: "in-progress", label_to_add: "done" }, {});

    expect(countResult.success).toBe(false);
    expect(mockCore.warnings).toContain("Skipping replace_label: max count of 1 reached");
    expect(mockCore.errors).toHaveLength(0);

    mockCore.warnings.length = 0;
    const restrictedHandler = await main({ allowed_repos: ["other/repo"] });
    const repoResult = await restrictedHandler({ repo: "blocked/repo", label_to_remove: "in-progress", label_to_add: "done" }, {});

    expect(repoResult.success).toBe(false);
    expect(mockCore.warnings).toContain(`Skipping replace_label: ${repoResult.error}`);
    expect(mockCore.errors).toHaveLength(0);
  });

  it("executes the label pipeline in order and stops before writes on a gate skip", async () => {
    const events = [];
    mockGithub.rest.issues.get = async () => {
      events.push("read");
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
    mockGithub.rest.issues.setLabels = async ({ labels }) => {
      events.push("write");
      return { data: labels.map(name => ({ name })) };
    };

    const gatedHandler = await main({ required_labels: ["approved"] });
    const skipped = await gatedHandler({ label_to_remove: "in-progress", label_to_add: "done" }, {});
    expect(skipped.skipped).toBe(true);
    expect(events).toEqual(["read"]);

    events.length = 0;
    const handler = await main({});
    const result = await handler({ label_to_remove: "in-progress", label_to_add: "done" }, {});

    expect(result.success).toBe(true);
    expect(events).toEqual(["read", "read", "write"]);
  });

  it("asserts success logs, complete result fields, and execution-state metadata", async () => {
    const handler = await main({});
    const result = await handler({ label_to_remove: "in-progress", label_to_add: "done" }, {});

    expect(mockCore.infos).toContain('Successfully replaced label "in-progress" → "done" on issue #42 in test-owner/test-repo');
    expect(mockCore.infos).toContain('Updated labels: ["bug","done"]');
    expect(Object.keys(result).sort()).toEqual(["after_state", "before_state", "contextType", "labelAdded", "labelRemoved", "number", "repo", "success"]);
    expect(result).toMatchObject({
      success: true,
      number: 42,
      repo: "test-owner/test-repo",
      labelRemoved: "in-progress",
      labelAdded: "done",
      contextType: "issue",
      before_state: {
        title: "Test issue title",
        state: "",
        labels: ["in-progress", "bug"],
        assignees: [],
      },
      after_state: {
        title: "Test issue title",
        state: "",
        labels: ["bug", "done"],
        assignees: [],
      },
    });
    expect(Object.keys(result.before_state).sort()).toEqual(["assignees", "body_hash", "labels", "state", "title"]);
    expect(Object.keys(result.after_state).sort()).toEqual(["assignees", "body_hash", "labels", "state", "title"]);
  });

  it("T-RL-060 does not call setLabels in staged mode", async () => {
    const setLabels = vi.spyOn(mockGithub.rest.issues, "setLabels");
    const handler = await main({ staged: true });
    const result = await handler({ label_to_remove: "in-progress", label_to_add: "done" }, {});

    expect(result.success).toBe(true);
    expect(result.staged).toBe(true);
    expect(result.previewInfo?.labelToRemove).toBe("in-progress");
    expect(result.previewInfo?.labelToAdd).toBe("done");
    expect(setLabels).not.toHaveBeenCalled();
  });

  it("T-RL-019 rejects when no item number is available", async () => {
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
    expect(mockCore.errors).toContain(`Failed to replace label: ${result.error}`);
  });

  it.each([
    { status: 429, headers: { "retry-after": "0.001" } },
    { status: 403, headers: { "retry-after": "0.001" } },
  ])("T-RL-054 retries rate-limit response $status", async response => {
    let setLabelsCalls = 0;
    mockGithub.rest.issues.setLabels = async ({ labels }) => {
      setLabelsCalls++;
      if (setLabelsCalls === 1) {
        const error = new Error("Rate limited");
        error.status = response.status;
        error.response = response;
        throw error;
      }
      return { data: labels.map(name => ({ name })) };
    };

    vi.useFakeTimers();
    try {
      const handler = await main({});
      const resultPromise = handler({ label_to_remove: "in-progress", label_to_add: "done" }, {});
      await vi.runAllTimersAsync();
      const result = await resultPromise;

      expect(result.success).toBe(true);
      expect(setLabelsCalls).toBe(2);
    } finally {
      vi.useRealTimers();
    }
  });

  it("T-RL-046a rejects an HTTP 404 without retrying and logs a hard error", async () => {
    let setLabelsCalls = 0;
    mockGithub.rest.issues.setLabels = async () => {
      setLabelsCalls++;
      const error = new Error("Not Found");
      error.status = 404;
      throw error;
    };

    const handler = await main({});
    const result = await handler({ label_to_remove: "in-progress", label_to_add: "done" }, {});

    expect(result.success).toBe(false);
    expect(setLabelsCalls).toBe(1);
    expect(result.error).toContain("Not Found");
    expect(mockCore.errors).toContain(`Failed to replace label: ${result.error}`);
  });

  it.each([
    [
      "HTTP 503",
      () => {
        const error = new Error("Service unavailable");
        error.status = 503;
        return error;
      },
    ],
    [
      "transport timeout",
      () => {
        const error = new Error("Request timeout");
        error.code = "ETIMEDOUT";
        return error;
      },
    ],
  ])("T-RL-046b retries %s failures", async (_name, createError) => {
    let setLabelsCalls = 0;
    mockGithub.rest.issues.setLabels = async ({ labels }) => {
      setLabelsCalls++;
      if (setLabelsCalls < 3) throw createError();
      return { data: labels.map(name => ({ name })) };
    };

    vi.useFakeTimers();
    try {
      const handler = await main({});
      const resultPromise = handler({ label_to_remove: "in-progress", label_to_add: "done" }, {});
      await vi.runAllTimersAsync();
      const result = await resultPromise;

      expect(result.success).toBe(true);
      expect(setLabelsCalls).toBe(3);
    } finally {
      vi.useRealTimers();
    }
  });

  it("T-RL-037 returns a hard error after transient retries are exhausted", async () => {
    let setLabelsCalls = 0;
    mockGithub.rest.issues.setLabels = async () => {
      setLabelsCalls++;
      const error = new Error("Service unavailable");
      error.status = 503;
      throw error;
    };

    vi.useFakeTimers();
    try {
      const handler = await main({});
      const resultPromise = handler({ label_to_remove: "in-progress", label_to_add: "done" }, {});
      await vi.runAllTimersAsync();
      const result = await resultPromise;

      expect(result.success).toBe(false);
      expect(setLabelsCalls).toBe(6);
      expect(result.error).toContain("Original error: Service unavailable");
      expect(mockCore.errors).toContain(`Failed to replace label: ${result.error}`);
    } finally {
      vi.useRealTimers();
    }
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
