// @ts-check

import { describe, it, expect, beforeEach, vi } from "vitest";
import { createRequire } from "module";

const req = createRequire(import.meta.url);
const { extractRunUrl, main } = req("./label_apply_safe_outputs.cjs");

// ─── global mocks ────────────────────────────────────────────────────────────

const mockCore = {
  info: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  setFailed: vi.fn(),
};

const mockGithub = {
  rest: {
    issues: {
      createComment: vi.fn(),
      removeLabel: vi.fn(),
      createLabel: vi.fn(),
    },
  },
};

// Default context: issue labeled with apply-safe-outputs, body has a combined XML marker
const defaultIssueBody = `<!-- gh-aw-agentic-workflow: my-workflow, id: 99, workflow_id: my-workflow, run: https://github.com/test-owner/test-repo/actions/runs/99 -->`;

const mockContext = {
  eventName: "issues",
  repo: { owner: "test-owner", repo: "test-repo" },
  payload: {
    issue: { number: 42, body: defaultIssueBody },
    label: { name: "agentic-workflows:apply-safe-outputs" },
  },
};

global.core = mockCore;
global.github = mockGithub;
global.context = mockContext;

// ─── extractRunUrl ────────────────────────────────────────────────────────────

describe("extractRunUrl", () => {
  it("extracts run URL from combined marker run: field", () => {
    const body = `<!-- gh-aw-agentic-workflow: my-wf, id: 123, run: https://github.com/owner/repo/actions/runs/123 -->`;
    expect(extractRunUrl(body)).toBe("https://github.com/owner/repo/actions/runs/123");
  });

  it("extracts numeric run ID from combined marker id: field when run: is absent", () => {
    const body = `<!-- gh-aw-agentic-workflow: my-wf, id: 456 -->`;
    expect(extractRunUrl(body)).toBe("456");
  });

  it("run: field takes priority over id: field", () => {
    const body = `<!-- gh-aw-agentic-workflow: my-wf, id: 111, run: https://github.com/o/r/actions/runs/222 -->`;
    expect(extractRunUrl(body)).toBe("https://github.com/o/r/actions/runs/222");
  });

  it("extracts run URL from standalone gh-aw-run-url marker", () => {
    const body = `<!-- gh-aw-run-url: https://github.com/owner/repo/actions/runs/789 -->`;
    expect(extractRunUrl(body)).toBe("https://github.com/owner/repo/actions/runs/789");
  });

  it("extracts numeric run ID from standalone marker", () => {
    const body = `<!-- gh-aw-run-url: 5555 -->`;
    expect(extractRunUrl(body)).toBe("5555");
  });

  it("combined marker takes priority over standalone marker", () => {
    const body = `<!-- gh-aw-agentic-workflow: my-wf, run: https://github.com/o/r/actions/runs/100 -->\n` + `<!-- gh-aw-run-url: https://github.com/o/r/actions/runs/999 -->`;
    expect(extractRunUrl(body)).toBe("https://github.com/o/r/actions/runs/100");
  });

  it("returns null when body is empty", () => {
    expect(extractRunUrl("")).toBeNull();
  });

  it("returns null when body is null", () => {
    expect(extractRunUrl(null)).toBeNull();
  });

  it("returns null when body has no recognized markers", () => {
    expect(extractRunUrl("Just a plain issue body with no markers.")).toBeNull();
  });

  it("does not match partial or malformed combined markers", () => {
    const body = `<!-- gh-aw-agentic-workflow: name only no run field -->`;
    expect(extractRunUrl(body)).toBeNull();
  });
});

// ─── main ─────────────────────────────────────────────────────────────────────

describe("main", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    process.env.GH_TOKEN = "fake-token";
    process.env.GITHUB_TOKEN = "fake-token";
    process.env.GITHUB_REPOSITORY = "test-owner/test-repo";
    process.env.GITHUB_SERVER_URL = "https://github.com";
    process.env.GITHUB_RUN_ID = "999";
    delete process.env.GH_AW_RUN_URL;

    // Default: createLabel returns 422 (already exists)
    const alreadyExists = Object.assign(new Error("Unprocessable Entity"), { status: 422 });
    mockGithub.rest.issues.createLabel.mockRejectedValue(alreadyExists);
    mockGithub.rest.issues.createComment.mockResolvedValue({});
    mockGithub.rest.issues.removeLabel.mockResolvedValue({});

    // Restore default context
    global.context = {
      eventName: "issues",
      repo: { owner: "test-owner", repo: "test-repo" },
      payload: {
        issue: { number: 42, body: defaultIssueBody },
        label: { name: "agentic-workflows:apply-safe-outputs" },
      },
    };
  });

  it("uses a canonical workflow link when the existing marker contains only a numeric run ID", async () => {
    global.context.payload.issue.body = "<!-- gh-aw-agentic-workflow: my-workflow, id: 456 -->";

    await main();

    expect(process.env.GH_AW_RUN_URL).toBe("456");
    expect(mockGithub.rest.issues.createComment).toHaveBeenCalledWith(
      expect.objectContaining({
        body: expect.stringContaining("[run `456`](https://github.com/test-owner/test-repo/actions/runs/456)"),
      })
    );
  });

  it("skips silently when event type is not 'issues'", async () => {
    global.context = { ...global.context, eventName: "pull_request" };

    await main();

    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Skipping"));
    expect(mockGithub.rest.issues.createComment).not.toHaveBeenCalled();
  });

  it("skips when the label is not the apply-safe-outputs label", async () => {
    global.context = {
      ...global.context,
      payload: {
        issue: { number: 42, body: defaultIssueBody },
        label: { name: "some-other-label" },
      },
    };

    await main();

    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Skipping"));
    expect(mockGithub.rest.issues.createComment).not.toHaveBeenCalled();
  });

  it("calls setFailed and posts a warning comment when no run URL is found", async () => {
    global.context = {
      ...global.context,
      payload: {
        issue: { number: 5, body: "Plain issue body with no markers." },
        label: { name: "agentic-workflows:apply-safe-outputs" },
      },
    };

    await main();

    expect(mockCore.setFailed).toHaveBeenCalled();
    expect(mockGithub.rest.issues.createComment).toHaveBeenCalledWith(
      expect.objectContaining({
        body: expect.stringContaining("Could not apply safe outputs"),
      })
    );
    expect(mockGithub.rest.issues.removeLabel).not.toHaveBeenCalled();
  });

  it("ensures the apply-safe-outputs label exists at the start of main", async () => {
    const alreadyExists = Object.assign(new Error("Unprocessable Entity"), { status: 422 });
    mockGithub.rest.issues.createLabel.mockRejectedValue(alreadyExists);

    // The issue body has no run URL, so main() will call setFailed after ensureLabelExists —
    // we only care that createLabel was called with the right args.
    global.context = {
      ...global.context,
      payload: {
        issue: { number: 42, body: "no markers here" },
        label: { name: "agentic-workflows:apply-safe-outputs" },
      },
    };

    await main();

    expect(mockGithub.rest.issues.createLabel).toHaveBeenCalledWith(
      expect.objectContaining({
        name: "agentic-workflows:apply-safe-outputs",
        color: "8250df",
      })
    );
  });

  it("removes the label after a successful apply", async () => {
    // Stub the replay driver to succeed
    const replayModule = req("./apply_safe_outputs_replay.cjs");
    const originalMain = replayModule.main;
    replayModule.main = vi.fn().mockResolvedValue(undefined);

    await main();

    expect(mockGithub.rest.issues.removeLabel).toHaveBeenCalledWith(
      expect.objectContaining({
        issue_number: 42,
        name: "agentic-workflows:apply-safe-outputs",
      })
    );

    // Restore
    replayModule.main = originalMain;
  });

  it("logs a warning when label removal fails but does not fail the step", async () => {
    const replayModule = req("./apply_safe_outputs_replay.cjs");
    const originalMain = replayModule.main;
    replayModule.main = vi.fn().mockResolvedValue(undefined);

    mockGithub.rest.issues.removeLabel.mockRejectedValue(new Error("Not Found"));

    await main();

    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Failed to remove label"));
    expect(mockCore.setFailed).not.toHaveBeenCalled();

    // Restore
    replayModule.main = originalMain;
  });
});
