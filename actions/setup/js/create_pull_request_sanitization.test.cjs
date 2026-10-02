// @ts-check
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { createRequire } from "module";
import * as fs from "fs";
import * as path from "path";
import * as os from "os";

const require = createRequire(import.meta.url);

describe("create_pull_request - body sanitization", () => {
  let tempDir;
  let originalEnv;

  beforeEach(() => {
    originalEnv = { ...process.env };
    process.env.GH_AW_WORKFLOW_ID = "test-workflow";
    process.env.GITHUB_REPOSITORY = "test-owner/test-repo";
    process.env.GITHUB_BASE_REF = "main";
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "create-pr-sanitize-test-"));

    global.core = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      debug: vi.fn(),
      setFailed: vi.fn(),
      setOutput: vi.fn(),
      startGroup: vi.fn(),
      endGroup: vi.fn(),
      summary: {
        addRaw: vi.fn().mockReturnThis(),
        write: vi.fn().mockResolvedValue(undefined),
      },
    };
    global.github = {
      rest: {
        pulls: {
          create: vi.fn().mockResolvedValue({ data: { number: 1, html_url: "https://github.com/test" } }),
        },
        repos: {
          get: vi.fn().mockResolvedValue({ data: { default_branch: "main" } }),
        },
        issues: {
          addLabels: vi.fn().mockResolvedValue({}),
        },
      },
      graphql: vi.fn(),
    };
    global.context = {
      eventName: "workflow_dispatch",
      repo: { owner: "test-owner", repo: "test-repo" },
      payload: {},
    };
    global.exec = {
      exec: vi.fn().mockResolvedValue(0),
      getExecOutput: vi.fn().mockResolvedValue({ exitCode: 0, stdout: "", stderr: "" }),
    };

    // Clear module cache so globals are picked up fresh
    delete require.cache[require.resolve("./create_pull_request.cjs")];
  });

  afterEach(() => {
    for (const key of Object.keys(process.env)) {
      if (!(key in originalEnv)) {
        delete process.env[key];
      }
    }
    Object.assign(process.env, originalEnv);

    if (tempDir && fs.existsSync(tempDir)) {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }

    delete global.core;
    delete global.github;
    delete global.context;
    delete global.exec;
    vi.clearAllMocks();
  });

  it("should neutralize @mentions in PR body", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allow_empty: true });

    await handler(
      {
        title: "Test PR",
        body: "This PR fixes a bug reported by @malicious-user and reviewed by @another-user.",
      },
      {}
    );

    const createCall = global.github.rest.pulls.create.mock.calls[0]?.[0];
    expect(createCall).toBeDefined();
    // @mentions must be neutralized to backtick form
    expect(createCall.body).toContain("`@malicious-user`");
    expect(createCall.body).toContain("`@another-user`");
    // Raw unescaped @mentions must not appear
    expect(createCall.body).not.toMatch(/(?<![`])@malicious-user(?![`])/);
    expect(createCall.body).not.toMatch(/(?<![`])@another-user(?![`])/);
  });

  it("should pass shell metacharacters in PR content as API data", async () => {
    const payload = "$(touch /tmp/gh-aw-command-injection); echo safe";
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allow_empty: true });

    await handler({ title: "Test PR", body: payload }, {});

    const createCall = global.github.rest.pulls.create.mock.calls[0]?.[0];
    expect(createCall.body).toContain(payload);
    expect(JSON.stringify(global.exec.exec.mock.calls)).not.toContain(payload);
  });

  it("should expose hidden markdown link title XPIA payloads in PR body (closing the XPIA channel)", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allow_empty: true });

    await handler(
      {
        title: "Security Fix",
        body: 'See [the report](https://example.com "XPIA hidden payload") for context.',
      },
      {}
    );

    const createCall = global.github.rest.pulls.create.mock.calls[0]?.[0];
    expect(createCall).toBeDefined();
    // The hidden title must be moved to visible text (closing the XPIA channel)
    // sanitizeContent converts [text](url "hidden") → [text (hidden)](url)
    expect(createCall.body).toContain("XPIA hidden payload");
    // The payload must no longer be in a hidden markdown link title attribute
    expect(createCall.body).not.toMatch(/\[.*\]\([^)]*"XPIA hidden payload"[^)]*\)/);
  });

  it("should sanitize body but preserve the footer workflow marker", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allow_empty: true });

    await handler(
      {
        title: "Test PR",
        body: "Please notify @someone about this change.",
      },
      {}
    );

    const createCall = global.github.rest.pulls.create.mock.calls[0]?.[0];
    expect(createCall).toBeDefined();
    // @mention must be neutralized
    expect(createCall.body).toContain("`@someone`");
    // System-generated footer marker must still be present
    expect(createCall.body).toContain("gh-aw-workflow-id");
  });

  it("should preserve allowlisted mentions when mentions config is provided", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({
      allow_empty: true,
      mentions: { allowTeamMembers: false, allowContext: false, allowed: ["copilot"] },
    });

    await handler(
      {
        title: "Test PR",
        body: "Please ask @copilot and @someone-else to review this.",
      },
      {}
    );

    const createCall = global.github.rest.pulls.create.mock.calls[0]?.[0];
    expect(createCall).toBeDefined();
    expect(createCall.body).toContain("@copilot");
    expect(createCall.body).not.toContain("`@copilot`");
    expect(createCall.body).toContain("`@someone-else`");
  });
});
