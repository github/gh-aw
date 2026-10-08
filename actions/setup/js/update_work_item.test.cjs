import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { main } from "./update_work_item.cjs";

global.core = { debug: vi.fn(), info: vi.fn(), warning: vi.fn() };

describe("update_work_item", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    global.fetch = vi.fn();
  });

  afterEach(() => {
    delete global.fetch;
  });

  it("returns a handler function", async () => {
    expect(typeof (await main())).toBe("function");
  });

  it("uses the update work-item handler", async () => {
    const handler = await main({ target: "*", title: false });
    const result = await handler({ id: 42, title: "New title" }, {});
    expect(result).toEqual({ success: false, error: "title updates are not enabled by ado_update_work_item" });
    expect(global.fetch).not.toHaveBeenCalled();
  });

  it("does not mutate the input body when applying a configured footer", async () => {
    const handler = await main({ staged: true, target: "*", body: true, body_footer: "Configured footer" });
    const message = { id: 42, body: "Original body" };
    expect(await handler(message, {})).toMatchObject({ success: true, staged: true });
    expect(message).toEqual({ id: 42, body: "Original body" });
  });
});
