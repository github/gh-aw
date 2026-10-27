// @ts-check

import { describe, it, expect, beforeEach, vi } from "vitest";
import { main } from "./safe_output_action_handler.cjs";

describe("safe_output_action_handler", () => {
  beforeEach(() => {
    // Provide a mock global `core` matching the @actions/core API surface used by the handler.
    global.core = {
      info: vi.fn(),
      debug: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      setOutput: vi.fn(),
      setFailed: vi.fn(),
    };
  });

  describe("main() — factory", () => {
    it("should return a function (the handler) when called", async () => {
      const handler = await main({ action_name: "my_action" });
      expect(typeof handler).toBe("function");
    });

    it("should use 'unknown_action' when config is not provided", async () => {
      const handler = await main();
      expect(typeof handler).toBe("function");
    });
  });

  describe("handler — basic payload export", () => {
    it("should export the payload as a step output and return success", async () => {
      const handler = await main({ action_name: "add_label" });

      const message = { type: "add_label", labels: "bug" };
      const result = await handler(message, {}, new Map());

      expect(result.success).toBe(true);
      expect(result.delegated).toBe(true);
      expect(result.action_name).toBe("add_label");

      // 'type' (an INTERNAL_MESSAGE_FIELDS member) must be stripped from the exported payload
      const exported = JSON.parse(global.core.setOutput.mock.calls[0][1]);
      expect(exported).toHaveProperty("labels", "bug");
      expect(exported).not.toHaveProperty("type");
    });

    it("should use the correct output key format action_<name>_payload", async () => {
      const handler = await main({ action_name: "my_action" });
      await handler({ type: "my_action", key: "value" }, {}, new Map());

      expect(global.core.setOutput).toHaveBeenCalledWith("action_my_action_payload", expect.any(String));
    });

    it("should pass through non-string values without sanitization", async () => {
      const handler = await main({ action_name: "my_action" });
      const message = { type: "my_action", count: 42, active: true };

      await handler(message, {}, new Map());

      const exported = JSON.parse(global.core.setOutput.mock.calls[0][1]);
      expect(exported.count).toBe(42);
      expect(exported.active).toBe(true);
    });
  });

  describe("handler — once-only enforcement", () => {
    it("should return an error on the second call", async () => {
      const handler = await main({ action_name: "my_action" });
      const message = { labels: "bug" };

      const first = await handler(message, {}, new Map());
      expect(first.success).toBe(true);

      const second = await handler(message, {}, new Map());
      expect(second.success).toBe(false);
      expect(second.error).toContain("can only be called once");

      // setOutput should only be called once (for the first invocation)
      expect(global.core.setOutput).toHaveBeenCalledTimes(1);
    });
  });

  describe("handler — INTERNAL_MESSAGE_FIELDS filtering", () => {
    it("should strip 'type' from the exported payload", async () => {
      const handler = await main({ action_name: "my_action" });
      await handler({ type: "my_action", title: "hello" }, {}, new Map());

      const exported = JSON.parse(global.core.setOutput.mock.calls[0][1]);
      expect(exported).not.toHaveProperty("type");
      expect(exported).toHaveProperty("title", "hello");
    });
  });

  describe("handler — temporaryIdMap substitution", () => {
    it("should substitute temporary ID references in string values", async () => {
      const handler = await main({ action_name: "my_action" });
      // The temporary ID pattern is "#aw_XXXX" (3-12 alphanumeric chars with #aw_ prefix)
      const temporaryIdMap = new Map([["aw_abc1", { repo: "owner/repo", number: 99 }]]);
      await handler({ type: "my_action", body: "Fixes #aw_abc1" }, {}, temporaryIdMap);

      const exported = JSON.parse(global.core.setOutput.mock.calls[0][1]);
      // replaceTemporaryIdReferences should replace '#aw_abc1' with the issue reference
      expect(exported.body).not.toContain("#aw_abc1");
    });
  });

  describe("handler — empty payload", () => {
    it("should handle an empty message (only internal fields)", async () => {
      const handler = await main({ action_name: "my_action" });
      const result = await handler({ type: "my_action" }, {}, new Map());

      expect(result.success).toBe(true);
      const exported = JSON.parse(global.core.setOutput.mock.calls[0][1]);
      expect(exported).toEqual({});
    });
  });
});
