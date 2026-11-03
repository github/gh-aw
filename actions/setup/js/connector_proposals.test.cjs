import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";
import { createHandlers } from "./safe_outputs_handlers.cjs";

describe("connector creation proposal references", () => {
  beforeEach(() => {
    vi.stubGlobal("core", { info: vi.fn(), debug: vi.fn(), warning: vi.fn(), error: vi.fn() });
    vi.stubGlobal("context", { repo: { owner: "fixture", repo: "fixture" }, eventName: "workflow_dispatch", payload: {} });
  });
  afterEach(() => vi.unstubAllGlobals());

  it("allocates an opaque proposal alias when the configured creator omits it", () => {
    const append = vi.fn();
    const handlers = createHandlers({ debug: vi.fn() }, append, { custom_create: { temporary_id_field: "temporary_id" } });
    const reply = handlers.defaultHandler("custom_create")({ title: "Reviewed creation proposal" });
    const result = JSON.parse(reply.content[0].text);
    expect(result.temporary_id).toMatch(/^aw_[A-Za-z0-9]{8}$/);
    expect(append).toHaveBeenCalledWith(expect.objectContaining({ type: "custom_create", temporary_id: result.temporary_id }));
    expect(result).not.toHaveProperty("createdResource");
    expect(result).not.toHaveProperty("id");
  });

  it("preserves explicit aliases and does not invent them for noncreators", () => {
    const append = vi.fn();
    const handlers = createHandlers({ debug: vi.fn() }, append, { custom_create: { temporary_id_field: "temporary_id" } });
    const created = handlers.defaultHandler("custom_create")({ temporary_id: "aw_explicit" });
    expect(JSON.parse(created.content[0].text).temporary_id).toBe("aw_explicit");
    const ordinary = handlers.defaultHandler("custom_update")({ text: "No alias allocation" });
    expect(JSON.parse(ordinary.content[0].text)).not.toHaveProperty("temporary_id");
  });
});
