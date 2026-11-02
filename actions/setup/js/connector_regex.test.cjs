const { matchesConnectorPattern } = require("./connector_regex.cjs");

describe("bundled connector pattern validation", () => {
  it("uses strict end anchors rather than accepting a trailing newline", async () => {
    expect(await matchesConnectorPattern("^[A-Z]+$", "APP")).toBe(true);
    expect(await matchesConnectorPattern("^[A-Z]+$", "APP\n")).toBe(false);
    expect(await matchesConnectorPattern("\\A[A-Z]+\\z", "APP\n")).toBe(false);
  });

  it("preserves Go ASCII whitespace and rune matching", async () => {
    expect(await matchesConnectorPattern("^\\s$", "\t")).toBe(true);
    expect(await matchesConnectorPattern("^\\s$", "\u00a0")).toBe(false);
    expect(await matchesConnectorPattern("^\\S$", "\u00a0")).toBe(true);
    expect(await matchesConnectorPattern("^.$", "\r")).toBe(true);
    expect(await matchesConnectorPattern("^.$", "\u2028")).toBe(true);
    expect(await matchesConnectorPattern("^.$", "\n")).toBe(false);
    expect(await matchesConnectorPattern("(?s)^.$", "\n")).toBe(true);
  });

  it("normalizes invalid Unicode like the Go JSON decoder", async () => {
    expect(await matchesConnectorPattern("^\ufffd$", "\ud800")).toBe(true);
    expect(await matchesConnectorPattern("^\\S+$", "é".repeat(65000))).toBe(true);
    expect(await matchesConnectorPattern("^\\S+$", "😀".repeat(65000))).toBe(true);
  });

  it("fails closed on unsupported syntax and oversized inputs", async () => {
    for (const pattern of ["(?i)abc", "[[:alpha:]]", "\\p{Greek}", "(?=abc)", "\\Qabc\\E"]) {
      await expect(matchesConnectorPattern(pattern, "abc")).rejects.toThrow("nonportable");
    }
    await expect(matchesConnectorPattern("a".repeat(4097), "a")).rejects.toThrow("bound");
    await expect(matchesConnectorPattern("a", "a".repeat(65537))).rejects.toThrow("bound");
  });

  it("terminates catastrophic backtracking without blocking the host", async () => {
    await expect(matchesConnectorPattern("^(a+)+$", `${"a".repeat(50000)}!`)).rejects.toThrow("budget");
    expect(await matchesConnectorPattern("^ok$", "ok")).toBe(true);
  });
});
