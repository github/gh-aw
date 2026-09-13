// @ts-check

const { normalizeRunCreatedAt } = require("./run_created_at.cjs");

describe("normalizeRunCreatedAt", () => {
  it("keeps an already normalized UTC timestamp", () => {
    expect(normalizeRunCreatedAt("2026-09-12T20:58:00Z")).toBe("2026-09-12T20:58:00Z");
  });

  it("normalizes offsets and millisecond precision", () => {
    expect(normalizeRunCreatedAt("2026-09-12T22:58:00+02:00")).toBe("2026-09-12T20:58:00Z");
    expect(normalizeRunCreatedAt("2026-09-12T20:58:00.553Z")).toBe("2026-09-12T20:58:00Z");
    expect(normalizeRunCreatedAt("2026-09-12T20:58:00.999Z")).toBe("2026-09-12T20:58:00Z");
  });

  it("reports unusable values as empty", () => {
    expect(normalizeRunCreatedAt("")).toBe("");
    expect(normalizeRunCreatedAt("   ")).toBe("");
    expect(normalizeRunCreatedAt("not-a-timestamp")).toBe("");
    expect(normalizeRunCreatedAt("2026")).toBe("");
    expect(normalizeRunCreatedAt("2026-09-12 20:58:00")).toBe("");
    expect(normalizeRunCreatedAt(null)).toBe("");
    expect(normalizeRunCreatedAt(undefined)).toBe("");
    expect(normalizeRunCreatedAt(1757710680000)).toBe("");
  });
});
