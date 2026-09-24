// @ts-check
import { describe, it, expect, beforeEach, vi } from "vitest";
const { createCountGatedHandler } = require("./handler_scaffold.cjs");

describe("handler_scaffold", () => {
  beforeEach(() => {
    global.core = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      debug: vi.fn(),
    };
  });

  describe("createCountGatedHandler", () => {
    it("should create a handler factory that returns a function", async () => {
      const factory = createCountGatedHandler({
        handlerType: "test_handler",
        setup: async () => async () => ({ success: true }),
      });

      const handler = await factory({ max: 5 });
      expect(typeof handler).toBe("function");
    });

    it("should pass config and maxCount to setup function", async () => {
      const setupSpy = vi.fn().mockResolvedValue(async () => ({ success: true }));

      const factory = createCountGatedHandler({
        handlerType: "test_handler",
        setup: setupSpy,
      });

      const config = { max: 7, allowed: ["a", "b"] };
      await factory(config);

      expect(setupSpy).toHaveBeenCalledWith(config, 7, false);
    });

    it("should default maxCount to 10 when not specified", async () => {
      const setupSpy = vi.fn().mockResolvedValue(async () => ({ success: true }));

      const factory = createCountGatedHandler({
        handlerType: "test_handler",
        setup: setupSpy,
      });

      await factory({});
      expect(setupSpy).toHaveBeenCalledWith({}, 10, false);
    });

    it("should fall back to 10 when max is 0", async () => {
      const setupSpy = vi.fn().mockResolvedValue(async () => ({ success: true }));

      const factory = createCountGatedHandler({
        handlerType: "test_handler",
        setup: setupSpy,
      });

      await factory({ max: 0 });
      expect(setupSpy).toHaveBeenCalledWith({ max: 0 }, 10, false);
    });

    it("should pass isStaged as true when config.staged is true", async () => {
      const setupSpy = vi.fn().mockResolvedValue(async () => ({ success: true }));

      const factory = createCountGatedHandler({
        handlerType: "test_handler",
        setup: setupSpy,
      });

      const config = { max: 5, staged: true };
      await factory(config);

      expect(setupSpy).toHaveBeenCalledWith(config, 5, true);
    });

    it('should pass isStaged as true when config.staged is the string "true"', async () => {
      const setupSpy = vi.fn().mockResolvedValue(async () => ({ success: true }));

      const factory = createCountGatedHandler({
        handlerType: "test_handler",
        setup: setupSpy,
      });

      const config = { max: 5, staged: "true" };
      await factory(config);

      expect(setupSpy).toHaveBeenCalledWith(config, 5, true);
    });

    it("should treat unresolved staged expressions as false until runtime resolves them", async () => {
      const setupSpy = vi.fn().mockResolvedValue(async () => ({ success: true }));

      const factory = createCountGatedHandler({
        handlerType: "test_handler",
        setup: setupSpy,
      });

      const config = { max: 5, staged: "${{ inputs.staged }}" };
      await factory(config);

      expect(setupSpy).toHaveBeenCalledWith(config, 5, false);
    });

    it("should delegate to handleItem when under the limit", async () => {
      const handleItem = vi.fn().mockResolvedValue({ success: true, data: "result" });

      const factory = createCountGatedHandler({
        handlerType: "test_handler",
        setup: async () => handleItem,
      });

      const handler = await factory({ max: 5 });
      const result = await handler({ key: "value" }, { tempId: { repo: "o/r", number: 1 } });

      expect(result).toEqual({ success: true, data: "result" });
      expect(handleItem).toHaveBeenCalledWith({ key: "value" }, { tempId: { repo: "o/r", number: 1 } });
    });

    it("should respect max count limit", async () => {
      let callCount = 0;
      const handleItem = vi.fn().mockImplementation(async () => {
        callCount++;
        return { success: true, count: callCount };
      });

      const factory = createCountGatedHandler({
        handlerType: "test_handler",
        setup: async () => handleItem,
      });

      const handler = await factory({ max: 2 });

      const result1 = await handler({}, {});
      expect(result1.success).toBe(true);

      const result2 = await handler({}, {});
      expect(result2.success).toBe(true);

      const result3 = await handler({}, {});
      expect(result3.success).toBe(false);
      expect(result3.error).toContain("Max count of 2 reached");

      expect(handleItem).toHaveBeenCalledTimes(2);
    });

    it("should not enforce the call count when max is -1", async () => {
      const handleItem = vi.fn().mockResolvedValue({ success: true });

      const factory = createCountGatedHandler({
        handlerType: "test_handler",
        setup: async () => handleItem,
      });

      const handler = await factory({ max: -1 });
      await handler({}, {});
      await handler({}, {});
      const result = await handler({}, {});

      expect(result.success).toBe(true);
      expect(handleItem).toHaveBeenCalledTimes(3);
    });

    it("should log warning when max count is reached", async () => {
      const factory = createCountGatedHandler({
        handlerType: "my_handler",
        setup: async () => async () => ({ success: true }),
      });

      const handler = await factory({ max: 1 });
      await handler({}, {});
      await handler({}, {});

      expect(global.core.warning).toHaveBeenCalledWith("Skipping my_handler: max count of 1 reached");
    });

    it("should count failed handler calls toward the limit", async () => {
      const handleItem = vi.fn().mockResolvedValue({ success: false, error: "handler error" });

      const factory = createCountGatedHandler({
        handlerType: "test_handler",
        setup: async () => handleItem,
      });

      const handler = await factory({ max: 2 });

      const result1 = await handler({}, {});
      expect(result1.success).toBe(false);
      expect(result1.error).toBe("handler error");

      const result2 = await handler({}, {});
      expect(result2.success).toBe(false);
      expect(result2.error).toBe("handler error");

      const result3 = await handler({}, {});
      expect(result3.success).toBe(false);
      expect(result3.error).toContain("Max count of 2 reached");
    });

    it("should maintain independent counts across separate factory invocations", async () => {
      const factory = createCountGatedHandler({
        handlerType: "test_handler",
        setup: async () => async () => ({ success: true }),
      });

      const handler1 = await factory({ max: 1 });
      const handler2 = await factory({ max: 1 });

      const result1a = await handler1({}, {});
      expect(result1a.success).toBe(true);

      const result2a = await handler2({}, {});
      expect(result2a.success).toBe(true);

      const result1b = await handler1({}, {});
      expect(result1b.success).toBe(false);
      expect(result1b.error).toContain("Max count");

      const result2b = await handler2({}, {});
      expect(result2b.success).toBe(false);
      expect(result2b.error).toContain("Max count");
    });
  });
});
