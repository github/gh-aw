import { describe, expect, it } from "vitest";
import { constants } from "buffer";
import { DEFAULT_OBJECT_PACK_LIMITS, OBJECT_PACK_LIMIT_ENV, getObjectPackLimits } from "./object_pack_limits.cjs";

describe("repository-configured object-pack limits", () => {
  it("preserves all defaults when repository variables are unset or empty", () => {
    expect(Object.keys(OBJECT_PACK_LIMIT_ENV)).toHaveLength(4);
    expect(getObjectPackLimits({})).toEqual(DEFAULT_OBJECT_PACK_LIMITS);
    expect(getObjectPackLimits(Object.fromEntries(Object.values(OBJECT_PACK_LIMIT_ENV).map(name => [name, ""])))).toEqual(DEFAULT_OBJECT_PACK_LIMITS);
    expect(Object.isFrozen(getObjectPackLimits())).toBe(true);
    expect(Object.isFrozen(getObjectPackLimits().fetchDepths)).toBe(true);
  });

  it.each(Object.keys(OBJECT_PACK_LIMIT_ENV).filter(key => key !== "fetchDepths"))("allows raising and lowering the %s budget", key => {
    const name = OBJECT_PACK_LIMIT_ENV[key];
    for (const value of [1, DEFAULT_OBJECT_PACK_LIMITS[key] * 2]) {
      expect(getObjectPackLimits({ [name]: String(value) })[key]).toBe(value);
    }
  });

  it("derives every subordinate byte limit from one budget while preserving default ratios", () => {
    const doubled = getObjectPackLimits({ GH_AW_OBJECT_PACK_MAX_BYTES: "134217728" });
    for (const key of ["maxUncompressedBytes", "maxPackBytes", "maxPreviewBytes", "maxHeaderBytes", "maxHydrationBytes", "maxCommandOutputBytes", "maxGitOutputBytes"]) {
      expect(doubled[key]).toBe(DEFAULT_OBJECT_PACK_LIMITS[key] * 2);
    }
    const smallest = getObjectPackLimits({ GH_AW_OBJECT_PACK_MAX_BYTES: "1" });
    expect(smallest.maxPackBytes).toBe(2);
    expect(smallest.maxPreviewBytes).toBe(2);
    expect(smallest.maxHeaderBytes).toBe(1);
    expect(smallest.maxCommandOutputBytes).toBe(1);
  });

  it("derives entry and batch budgets from object count and the network deadline from timeout", () => {
    const limits = getObjectPackLimits({ GH_AW_OBJECT_PACK_MAX_OBJECTS: "200000", GH_AW_OBJECT_PACK_TIMEOUT_MS: "120000" });
    expect(limits.maxTreeEntries).toBe(2000000);
    expect(limits.maxScanEntries).toBe(2000000);
    expect(limits.fetchBatchSize).toBe(512);
    expect(limits.networkTimeoutMs).toBe(360000);
    expect(getObjectPackLimits({ GH_AW_OBJECT_PACK_MAX_OBJECTS: "1" }).fetchBatchSize).toBe(1);
  });

  it.each(["0", "-1", "1.5", "1e3", "Infinity", "1x", " ", " 12", "9007199254740992", "${SECRET_VALUE}"])("rejects invalid scalar %j without logging its value", value => {
    for (const [key, name] of Object.entries(OBJECT_PACK_LIMIT_ENV)) {
      if (key === "fetchDepths") continue;
      expect(() => getObjectPackLimits({ [name]: value })).toThrow(name);
      if (value.includes("SECRET")) expect(() => getObjectPackLimits({ [name]: value })).not.toThrow(value);
    }
  });

  it("uses a strictly increasing finite depth schedule instead of a fixed maximum", () => {
    expect(getObjectPackLimits({ GH_AW_OBJECT_PACK_FETCH_DEPTHS: "2, 32, 131072" }).fetchDepths).toEqual([2, 32, 131072]);
    expect(getObjectPackLimits({ GH_AW_OBJECT_PACK_FETCH_DEPTHS: "1" }).fetchDepths).toEqual([1]);
  });

  it.each(["0", "1,0", "1,1", "2,1", "1,", ",1", "1,  ", "1,Infinity", "1,2.5", "2147483648"])("rejects an invalid depth schedule %j", value => {
    expect(() => getObjectPackLimits({ GH_AW_OBJECT_PACK_FETCH_DEPTHS: value })).toThrow("GH_AW_OBJECT_PACK_FETCH_DEPTHS");
  });

  it("retains runtime representation limits rather than silently clamping", () => {
    expect(() => getObjectPackLimits({ GH_AW_OBJECT_PACK_TIMEOUT_MS: "715827883" })).toThrow("GH_AW_OBJECT_PACK_TIMEOUT_MS");
    expect(() => getObjectPackLimits({ GH_AW_OBJECT_PACK_MAX_OBJECTS: "4294967296" })).toThrow("GH_AW_OBJECT_PACK_MAX_OBJECTS");
    expect(() => getObjectPackLimits({ GH_AW_OBJECT_PACK_MAX_BYTES: String(constants.MAX_STRING_LENGTH) })).toThrow("GH_AW_OBJECT_PACK_MAX_BYTES");
  });
});
