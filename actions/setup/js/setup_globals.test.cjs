// @ts-check

import { afterEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "module";
import { getOctokit } from "@actions/github";
import fs from "node:fs";

const require = createRequire(import.meta.url);
const { setupGlobals } = require("./setup_globals.cjs");
const GLOBAL_NAMES = ["core", "github", "context", "exec", "io", "getOctokit", "runtimeFeatures", "hasRuntimeFeature", "getRuntimeFeatureValue"];
const savedGlobals = new Map(GLOBAL_NAMES.map(name => [name, Object.getOwnPropertyDescriptor(global, name)]));

afterEach(() => {
  for (const name of GLOBAL_NAMES) {
    const descriptor = savedGlobals.get(name);
    if (descriptor) {
      Object.defineProperty(global, name, descriptor);
    } else {
      delete global[name];
    }
  }
});

describe("setupGlobals API version", () => {
  function setupClient() {
    const fetch = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ number: 50 }), {
        status: 200,
        headers: { "content-type": "application/json" },
      })
    );
    const client = getOctokit("test-token", { request: { fetch } });
    setupGlobals({}, client, {}, {}, {}, getOctokit);
    return { fetch };
  }

  it.each(["POST /repos/{owner}/{repo}/issues", "PATCH /repos/{owner}/{repo}/issues/{issue_number}"])("uses the current API version for %s", async route => {
    const { fetch } = setupClient();
    await global.github.request(route, { owner: "owner", repo: "repo", issue_number: 50, title: "Failure" });
    expect(fetch.mock.calls[0][1].headers["x-github-api-version"]).toBe("2026-03-10");
  });

  it.each(["X-GitHub-Api-Version", "x-github-api-version", "X-GITHUB-API-VERSION"])("preserves an explicit %s header on the builtin client", async header => {
    const { fetch } = setupClient();
    await global.github.request("GET /repos/{owner}/{repo}", {
      owner: "owner",
      repo: "repo",
      headers: { [header]: "2022-11-28" },
    });
    const headers = fetch.mock.calls[0][1].headers;
    expect(headers["x-github-api-version"]).toBe("2022-11-28");
    expect(Object.keys(headers).filter(name => name.toLowerCase() === "x-github-api-version")).toHaveLength(1);
  });

  it("uses the current API version for per-handler authenticated clients", async () => {
    const { fetch } = setupClient();
    const client = global.getOctokit("handler-token", { request: { fetch } });
    await client.rest.issues.update({ owner: "owner", repo: "repo", issue_number: 50, title: "Failure" });
    expect(fetch.mock.calls[0][1].headers["x-github-api-version"]).toBe("2026-03-10");
  });

  it.each(["X-GitHub-Api-Version", "x-github-api-version", "X-GITHUB-API-VERSION"])("preserves an explicit %s header on per-handler clients", async header => {
    const { fetch } = setupClient();
    const client = global.getOctokit("handler-token", {
      request: { fetch },
      headers: { [header]: "2022-11-28", "x-custom-header": "custom" },
    });
    await client.rest.repos.get({ owner: "owner", repo: "repo" });
    const headers = fetch.mock.calls[0][1].headers;
    expect(headers["x-github-api-version"]).toBe("2022-11-28");
    expect(headers["x-custom-header"]).toBe("custom");
    expect(Object.keys(headers).filter(name => name.toLowerCase() === "x-github-api-version")).toHaveLength(1);
  });

  it("continues to reject OAuth tokens", () => {
    setupClient();
    expect(() => global.getOctokit("gho_test")).toThrow("OAuth tokens are not suitable for automation");
  });

  it.each([
    ["github_actions", "ghs_actions", true],
    ["pat", "github_pat_example", false],
    ["app", "ghs_installation", false],
    ["unknown", "opaque-token", false],
  ])("logs only the %s credential category for per-handler API calls", async (source, token, actionsToken) => {
    const previousToken = process.env.GITHUB_TOKEN;
    process.env.GITHUB_TOKEN = actionsToken ? token : "different-token";
    const append = vi.spyOn(fs, "appendFileSync").mockImplementation(() => undefined);
    try {
      const fetch = vi.fn().mockResolvedValue(
        new Response("{}", {
          status: 200,
          headers: { "x-ratelimit-limit": "5000", "x-ratelimit-remaining": "4999" },
        })
      );
      const client = getOctokit("test-token", { request: { fetch } });
      setupGlobals({}, client, {}, {}, {}, getOctokit);
      await global.getOctokit(token, { request: { fetch } }).rest.repos.get({ owner: "owner", repo: "repo" });
      const entries = append.mock.calls.map(([, content]) => JSON.parse(content.trim()));
      expect(entries).toMatchObject([{ source: "response_headers", credentialSource: source, operation: "repos.get", remaining: 4999 }]);
      expect(JSON.stringify(entries)).not.toContain(token);
    } finally {
      append.mockRestore();
      if (previousToken === undefined) delete process.env.GITHUB_TOKEN;
      else process.env.GITHUB_TOKEN = previousToken;
    }
  });

  it("lets per-request headers override per-handler defaults", async () => {
    const { fetch } = setupClient();
    const client = global.getOctokit("handler-token", {
      request: { fetch },
      headers: { "X-GitHub-Api-Version": "2022-11-28" },
    });
    await client.rest.issues.update({
      owner: "owner",
      repo: "repo",
      issue_number: 50,
      title: "Failure",
      headers: { "x-github-api-version": "2026-03-10" },
    });
    expect(fetch.mock.calls[0][1].headers["x-github-api-version"]).toBe("2026-03-10");
  });
});
