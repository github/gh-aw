// @ts-check
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const { createJiraClient, formatJiraError, JIRA_REQUEST_TIMEOUT_MS, normalizeJiraBaseUrl, textToADF } = require("./jira_client.cjs");

beforeEach(() => {
  global.core = { debug: vi.fn() };
});

afterEach(() => {
  delete global.core;
  vi.unstubAllGlobals();
});

describe("jira client", () => {
  it("normalizes site and Atlassian gateway base URLs", () => {
    expect(normalizeJiraBaseUrl("https://example.atlassian.net/")).toBe("https://example.atlassian.net");
    expect(normalizeJiraBaseUrl("https://example.atlassian.net/rest/api/3")).toBe("https://example.atlassian.net");
    expect(normalizeJiraBaseUrl("https://api.atlassian.com/ex/jira/cloud-id/")).toBe("https://api.atlassian.com/ex/jira/cloud-id");
  });

  it("rejects unsafe base URLs", () => {
    expect(() => normalizeJiraBaseUrl("http://example.atlassian.net")).toThrow("must use HTTPS");
    expect(() => normalizeJiraBaseUrl("https://example.atlassian.net?token=value")).toThrow("query string");
  });

  it("allows HTTP only for loopback URLs used by tests", () => {
    expect(normalizeJiraBaseUrl("http://localhost:3000")).toBe("http://localhost:3000");
    expect(normalizeJiraBaseUrl("http://127.0.0.1:3000")).toBe("http://127.0.0.1:3000");
    expect(normalizeJiraBaseUrl("http://[::1]:3000")).toBe("http://[::1]:3000");
  });

  it("converts plain text and newlines to ADF version 1", () => {
    expect(textToADF("first\n\nsecond")).toEqual({
      type: "doc",
      version: 1,
      content: [
        { type: "paragraph", content: [{ type: "text", text: "first" }] },
        { type: "paragraph", content: [] },
        { type: "paragraph", content: [{ type: "text", text: "second" }] },
      ],
    });
  });

  it("sends credentials only in the HTTP Authorization header and accepts 204", async () => {
    const fetchMock = vi.fn(async () => ({
      ok: true,
      status: 204,
      statusText: "No Content",
      text: async () => "",
    }));
    const client = createJiraClient(
      {
        JIRA_BASE_URL: "https://example.atlassian.net",
        JIRA_USER_EMAIL: "jira@example.com",
        JIRA_API_TOKEN: "secret-token",
      },
      fetchMock
    );

    await expect(client.request("/issue/ENG-1", { method: "PUT", body: { fields: { summary: "Updated" } } })).resolves.toBeNull();
    const [url, options] = fetchMock.mock.calls[0];
    expect(url).toBe("https://example.atlassian.net/rest/api/3/issue/ENG-1");
    expect(options.headers.Authorization).toBe(`Basic ${Buffer.from("jira@example.com:secret-token").toString("base64")}`);
    expect(options.body).not.toContain("secret-token");
    expect(global.core.debug).toHaveBeenCalledWith("Jira API request started: PUT /issue/ENG-1");
    expect(global.core.debug).toHaveBeenCalledWith("Jira API response received: PUT /issue/ENG-1 status=204");
    expect(global.core.debug.mock.calls.flat().join(" ")).not.toMatch(/secret-token|jira@example\.com|Basic /);
  });

  it("bounds Jira requests with an abort signal", async () => {
    const fetchMock = vi.fn(async (_url, options) => {
      expect(options.signal).toBeInstanceOf(AbortSignal);
      return {
        ok: true,
        status: 204,
        statusText: "No Content",
        text: async () => "",
      };
    });
    const client = createJiraClient(
      {
        JIRA_BASE_URL: "https://example.atlassian.net",
        JIRA_USER_EMAIL: "jira@example.com",
        JIRA_API_TOKEN: "secret-token",
      },
      fetchMock
    );

    await client.request("/issue/ENG-1", { method: "PUT" });
    expect(JIRA_REQUEST_TIMEOUT_MS).toBe(30_000);
  });

  it("surfaces structured Jira errors without leaking credentials", async () => {
    const fetchMock = vi.fn(async () => ({
      ok: false,
      status: 400,
      statusText: "Bad Request",
      text: async () =>
        JSON.stringify({
          errorMessages: ["Cannot create issue for jira@example.com"],
          errors: { summary: "secret-token is invalid" },
        }),
    }));
    const client = createJiraClient(
      {
        JIRA_BASE_URL: "https://example.atlassian.net",
        JIRA_USER_EMAIL: "jira@example.com",
        JIRA_API_TOKEN: "secret-token",
      },
      fetchMock
    );

    await expect(client.request("/issue", { method: "POST", body: {} })).rejects.toThrow("summary: *** is invalid");
    await expect(client.request("/issue", { method: "POST", body: {} })).rejects.not.toThrow(/secret-token|jira@example\.com/);
  });

  it("reports missing configuration without values", () => {
    expect(() => createJiraClient({})).toThrow("JIRA_BASE_URL");
    expect(() => createJiraClient({ JIRA_BASE_URL: "https://example.atlassian.net" })).toThrow("missing required GitHub Actions secrets: JIRA_USER_EMAIL, JIRA_API_TOKEN");
    expect(() =>
      createJiraClient({
        JIRA_BASE_URL: "https://example.atlassian.net",
        JIRA_USER_EMAIL: "jira@example.com",
      })
    ).toThrow("missing required GitHub Actions secret: JIRA_API_TOKEN");
    expect(() =>
      createJiraClient({
        JIRA_BASE_URL: "https://example.atlassian.net",
        JIRA_API_TOKEN: "secret-token",
      })
    ).toThrow("missing required GitHub Actions secret: JIRA_USER_EMAIL");
  });

  it("formats field and global errors", () => {
    expect(formatJiraError(400, "Bad Request", { errorMessages: ["Invalid request"], errors: { project: "Unknown project" } }, [])).toContain("Invalid request; project: Unknown project");
  });
});
