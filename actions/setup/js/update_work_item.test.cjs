import { beforeEach, describe, expect, it, vi } from "vitest";
import { main } from "./update_work_item.cjs";

global.core = { debug: vi.fn(), info: vi.fn(), warning: vi.fn() };

describe("update_work_item", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    global.fetch = vi.fn();
  });

  it("returns a handler function", async () => {
    expect(typeof (await main())).toBe("function");
  });

  it("works with default config", async () => {
    const handler = await main(undefined);
    expect(typeof handler).toBe("function");
  });

  it("rejects fields disabled by configuration", async () => {
    const handler = await main({ target: "*", title: false });
    const result = await handler({ id: 42, title: "New title" }, {});
    expect(result).toEqual({ success: false, error: "title updates are not enabled by ado_update_work_item" });
    expect(global.fetch).not.toHaveBeenCalled();
  });

  it("rejects area paths outside configured prefixes", async () => {
    const handler = await main({ staged: true, area_path: true, allowed_area_prefixes: ["proj\\Platform"] });
    const result = await handler({ id: 42, area_path: "proj\\Other" }, {});
    expect(result.success).toBe(false);
    expect(result.error).toContain("area_path");
    expect(global.fetch).not.toHaveBeenCalled();
  });

  it("does not call the API in staged mode", async () => {
    const handler = await main({ staged: true, target: "*", title: true });
    const result = await handler({ id: 42, title: "New title" }, {});
    expect(result.success).toBe(true);
    expect(global.fetch).not.toHaveBeenCalled();
  });

  it("does not mutate the input message", async () => {
    const handler = await main({ staged: true, target: "*", title: true });
    const message = { id: 42, title: "T" };
    await handler(message, {});
    expect(message).toEqual({ id: 42, title: "T" });
  });
});
