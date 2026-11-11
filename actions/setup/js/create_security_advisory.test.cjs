// @ts-check
import { beforeEach, describe, expect, it, vi } from "vitest";

const createRepositoryAdvisory = vi.fn();
const main = require("./create_security_advisory.cjs").main;
const tokenConfig = { "github-token": "external-token" };
const advisory = {
  summary: "Unsafe input handling",
  description: "Untrusted input can bypass validation in the affected package.",
  severity: "high",
  cwe_ids: ["CWE-20"],
  vulnerabilities: [{ package: { ecosystem: "npm", name: "example" }, vulnerable_version_range: "< 1.0.0", patched_versions: "1.0.0" }],
};
const created = { state: "draft", ghsa_id: "GHSA-2345-6789-cfgh", html_url: "https://github.com/owner/repo/security/advisories/GHSA-2345-6789-cfgh" };

describe("create_security_advisory", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    global.core = { info: vi.fn(), warning: vi.fn(), error: vi.fn() };
    global.context = { repo: { owner: "owner", repo: "repo" }, payload: {} };
    global.github = { rest: { securityAdvisories: { createRepositoryAdvisory } } };
    global.getOctokit = vi.fn(() => global.github);
    createRepositoryAdvisory.mockResolvedValue({ data: created });
  });

  it("uses the external token and returns the canonical draft URL", async () => {
    const handler = await main(tokenConfig);
    expect(await handler(advisory)).toEqual({ success: true, ghsa_id: created.ghsa_id, url: created.html_url });
    expect(global.getOctokit).toHaveBeenCalledWith("external-token", expect.any(Object));
    expect(createRepositoryAdvisory).toHaveBeenCalledWith({ owner: "owner", repo: "repo", ...advisory, start_private_fork: false });
  });

  it("never forwards agent-supplied publication, fork, or repository controls", async () => {
    const handler = await main(tokenConfig);
    await handler({ ...advisory, type: "create_security_advisory", state: "published", start_private_fork: true, owner: "other", repo: "public-repo" });
    expect(createRepositoryAdvisory).toHaveBeenCalledWith({ owner: "owner", repo: "repo", ...advisory, start_private_fork: false });
  });

  it("rejects missing credentials rather than using the ambient Actions client", async () => {
    await expect(main()).rejects.toThrow("requires an external github-token or GitHub App token");
    await expect(main({ "github-token": " " })).rejects.toThrow("requires an external");
    expect(createRepositoryAdvisory).not.toHaveBeenCalled();
  });

  it("previews without authentication, API calls, or advisory content in preview logs", async () => {
    const handler = await main({ staged: true });
    expect(await handler(advisory)).toEqual({ success: true, staged: true });
    expect(global.getOctokit).not.toHaveBeenCalled();
    expect(createRepositoryAdvisory).not.toHaveBeenCalled();
    expect(JSON.stringify(global.core.info.mock.calls)).not.toContain(advisory.summary);
    expect(JSON.stringify(global.core.info.mock.calls)).not.toContain(advisory.description);
  });

  it("enforces max without creating another advisory", async () => {
    const handler = await main(tokenConfig);
    await handler(advisory);
    expect(await handler(advisory)).toMatchObject({ success: false, skipped: true, reasonCode: "MAX_COUNT_REACHED" });
    expect(createRepositoryAdvisory).toHaveBeenCalledTimes(1);
  });

  it("supports the standard unlimited max value from a runtime expression", async () => {
    const handler = await main({ ...tokenConfig, max: -1 });
    expect(await handler(advisory)).toMatchObject({ success: true });
    expect(await handler(advisory)).toMatchObject({ success: true });
    expect(createRepositoryAdvisory).toHaveBeenCalledTimes(2);
  });

  it("preserves summaries up to the advisory API limit rather than the issue-title limit", async () => {
    const handler = await main(tokenConfig);
    await handler({ ...advisory, summary: "x".repeat(1024), description: "x".repeat(65000) });
    expect(createRepositoryAdvisory.mock.calls[0][0].summary).toHaveLength(1024);
    expect(createRepositoryAdvisory.mock.calls[0][0].description).toHaveLength(65000);
  });

  it.each([
    { summary: " " },
    { summary: "x".repeat(1025) },
    { description: "" },
    { description: "x".repeat(65001) },
    { vulnerabilities: [] },
    { vulnerabilities: [{ package: { ecosystem: "invalid" } }] },
    { vulnerabilities: [{ package: { ecosystem: "npm", name: "x".repeat(1025) } }] },
    { vulnerabilities: [{ package: { ecosystem: "npm", state: "published" } }] },
    { severity: "urgent" },
    { cwe_ids: ["invalid"] },
  ])("rejects invalid advisory data before API calls: %j", async fields => {
    const handler = await main(tokenConfig);
    await expect(handler({ ...advisory, ...fields })).rejects.toThrow("create_security_advisory");
    expect(createRepositoryAdvisory).not.toHaveBeenCalled();
  });

  it("sanitizes agent text before creating the advisory", async () => {
    const handler = await main(tokenConfig);
    await handler({ ...advisory, summary: "Unsafe\u0000 input", description: "Description\u0000 with invalid characters." });
    const payload = createRepositoryAdvisory.mock.calls[0][0];
    expect(payload.summary).not.toContain("\u0000");
    expect(payload.description).not.toContain("\u0000");
  });

  it("reports API errors without exposing confidential payloads or retrying", async () => {
    createRepositoryAdvisory.mockRejectedValueOnce(Object.assign(new Error(advisory.description), { status: 403 }));
    const handler = await main(tokenConfig);
    const result = await handler(advisory);
    expect(result).toMatchObject({ success: false, error: expect.stringContaining("HTTP 403") });
    expect(result.error).not.toContain(advisory.description);
    expect(createRepositoryAdvisory).toHaveBeenCalledTimes(1);
  });

  it("does not report success for a response that is not a draft", async () => {
    createRepositoryAdvisory.mockResolvedValueOnce({ data: { ...created, state: "published" } });
    const handler = await main(tokenConfig);
    expect(await handler(advisory)).toMatchObject({ success: false });
  });
});
