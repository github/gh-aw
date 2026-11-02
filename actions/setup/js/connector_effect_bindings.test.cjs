import { describe, expect, it } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { connectorEffectBindings } = require("./connector_effect_bindings.cjs");

describe("trusted connector effect binding sessions", () => {
  it("uses private execution identity, never temporary-ID map contents", () => {
    const execution = new Map([["aw_forged", { createdResource: "forged" }]]);
    const first = connectorEffectBindings(execution, "frozen-plan");
    expect(JSON.parse(first.env.GH_AW_CONNECTOR_BINDING_RECEIPTS)).toEqual([]);
    expect(Buffer.from(first.env.GH_AW_CONNECTOR_BINDING_KEY, "base64")).toHaveLength(32);
    const result = { statusCode: 201, createdResource: "opaque-authenticated-receipt" };
    first.record(result);
    expect(result).toEqual({ statusCode: 201 });
    expect(JSON.parse(connectorEffectBindings(execution, "frozen-plan").env.GH_AW_CONNECTOR_BINDING_RECEIPTS)).toEqual(["opaque-authenticated-receipt"]);
    for (const other of [connectorEffectBindings(new Map(execution), "frozen-plan"), connectorEffectBindings(execution, "different-plan")]) {
      expect(other.env.GH_AW_CONNECTOR_BINDING_KEY).not.toEqual(first.env.GH_AW_CONNECTOR_BINDING_KEY);
      expect(other.env.GH_AW_CONNECTOR_BINDING_RUN).not.toEqual(first.env.GH_AW_CONNECTOR_BINDING_RUN);
      expect(JSON.parse(other.env.GH_AW_CONNECTOR_BINDING_RECEIPTS)).toEqual([]);
    }
  });

  it.each([{ staged: true, statusCode: 201 }, { statusCode: 500 }, {}, { statusCode: 201, createdResource: 42 }])("rejects unsuccessful or malformed receipt results", result => {
    const bindings = connectorEffectBindings(new Map(), "plan");
    expect(() => bindings.record({ createdResource: "untrusted", ...result })).toThrow(/Invalid connector/);
    expect(bindings.env.GH_AW_CONNECTOR_BINDING_RECEIPTS).toBe("[]");
  });

  it("rejects absent trusted execution state", () => {
    expect(() => connectorEffectBindings({}, "plan")).toThrow(/trusted execution/);
  });
});
