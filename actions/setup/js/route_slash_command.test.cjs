// @ts-check
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";

const globals = /** @type {any} */ global;
const { main, parseSlashCommand, GITHUB_API_VERSION } = require("./route_slash_command.cjs");

describe("parseSlashCommand", () => {
  it("extracts a simple command name", () => {
    expect(parseSlashCommand("/archie")).toBe("archie");
  });

  it("extracts a command name with dashes", () => {
    expect(parseSlashCommand("/smoke-copilot-sdk")).toBe("smoke-copilot-sdk");
  });

  it("extracts only the command name from text with arguments", () => {
    expect(parseSlashCommand("/archie please do this")).toBe("archie");
  });

  it("extracts a command with dashes from text with arguments", () => {
    expect(parseSlashCommand("/smoke-copilot-sdk run tests")).toBe("smoke-copilot-sdk");
  });

  it("returns empty string when text does not start with a slash command", () => {
    expect(parseSlashCommand("hello /archie")).toBe("");
  });

  it("returns empty string for text starting with just a slash", () => {
    expect(parseSlashCommand("/")).toBe("");
  });

  it("returns empty string for empty string", () => {
    expect(parseSlashCommand("")).toBe("");
  });

  it("returns empty string when a command does not start at character zero", () => {
    expect(parseSlashCommand("  /smoke-copilot-sdk")).toBe("");
  });

  it("does not match when command is followed by punctuation", () => {
    expect(parseSlashCommand("/smoke-copilot-sdk!")).toBe("");
  });

  it("does not match a slash command in the middle of text", () => {
    expect(parseSlashCommand("some text /archie")).toBe("");
  });

  it("extracts a command name with underscores", () => {
    expect(parseSlashCommand("/code_review")).toBe("code_review");
  });

  it("extracts a command name with dots", () => {
    expect(parseSlashCommand("/cmd.add")).toBe("cmd.add");
  });

  it("does not match a command starting with a dash", () => {
    expect(parseSlashCommand("/-command")).toBe("");
  });

  it("does not match command followed by a colon", () => {
    expect(parseSlashCommand("/archie:more")).toBe("");
  });
});

describe("route_slash_command", () => {
  /** @type {{ core: any, github: any, context: any, exec: any, io: any, getOctokit: any }} */
  let savedGlobals;
  /** @type {any[]} */
  let dispatchCalls;
  /** @type {any[]} */
  let reactionCalls;
  /** @type {any[]} */
  let issueCommentCalls;
  /** @type {any} */
  let summaryMock;

  beforeEach(() => {
    savedGlobals = {
      core: globals.core,
      github: globals.github,
      context: globals.context,
      exec: globals.exec,
      io: globals.io,
      getOctokit: globals.getOctokit,
    };
    dispatchCalls = [];
    reactionCalls = [];
    issueCommentCalls = [];
    summaryMock = {};
    summaryMock.addHeading = vi.fn(() => summaryMock);
    summaryMock.addRaw = vi.fn(() => summaryMock);
    summaryMock.addEOL = vi.fn(() => summaryMock);
    summaryMock.write = vi.fn(async () => undefined);
    globals.core = {
      info: vi.fn(),
      warning: vi.fn(),
      setOutput: vi.fn(),
      summary: summaryMock,
    };
    globals.github = {
      request: vi.fn(async (route, params) => {
        if (String(route).includes("/dispatches")) {
          dispatchCalls.push(params);
          return {
            data: {
              workflow_run_id: 123456,
              workflow_run_url: "https://github.com/github/gh-aw/actions/runs/123456",
            },
          };
        }
        reactionCalls.push([route, params]);
        return { data: { id: 1 } };
      }),
      graphql: vi.fn(async () => ({ repository: { discussion: { id: "D_node" } }, addReaction: { reaction: { id: "R_1" } } })),
      rest: {
        actions: {
          listRepoWorkflows: vi.fn(async () => ({
            data: {
              workflows: [
                { path: ".github/workflows/archie.lock.yml", state: "active" },
                { path: ".github/workflows/ci-doctor.lock.yml", state: "active" },
                { path: ".github/workflows/smoke-copilot.lock.yml", state: "active" },
              ],
            },
          })),
          createWorkflowDispatch: vi.fn(),
        },
        pulls: {
          get: vi.fn(async ({ pull_number }) => ({
            data: {
              number: pull_number,
              head: { ref: "feature/pr-branch" },
            },
          })),
        },
        issues: {
          createComment: vi.fn(async params => {
            issueCommentCalls.push(params);
          }),
        },
      },
    };
    globals.context = {
      eventName: "issue_comment",
      ref: "refs/heads/main",
      repo: { owner: "github", repo: "gh-aw" },
      payload: { issue: {}, comment: { id: 123456 } },
    };
    globals.exec = {};
    globals.io = {};
    globals.getOctokit = vi.fn();
    process.env.GH_AW_SLASH_ROUTING = JSON.stringify({
      archie: [{ workflow: "archie", events: ["issue_comment", "pull_request_comment"], ai_reaction: "eyes" }],
    });
    process.env.GH_AW_LABEL_ROUTING = JSON.stringify({});
    process.env.GH_AW_HELP_COMMANDS = JSON.stringify([
      { command: "archie", description: "Run archie workflow", centralized: true, decentralized: false, source_file: "archie" },
      { command: "local-summary", description: "Run summary workflow", centralized: false, decentralized: true, source_file: "local-summary" },
      { command: "triage", description: "Apply triage label", label: true, source_file: "triage-workflow" },
    ]);
    process.env.GH_AW_HELP_COMMAND_ENABLED = "true";
    process.env.GH_AW_SLASH_COMMAND_DOCS_URL = "https://github.github.com/gh-aw/reference/command-triggers/";
    process.env.GITHUB_WORKSPACE = `${process.cwd()}`;
  });

  afterEach(() => {
    globals.core = savedGlobals.core;
    globals.github = savedGlobals.github;
    globals.context = savedGlobals.context;
    globals.exec = savedGlobals.exec;
    globals.io = savedGlobals.io;
    globals.getOctokit = savedGlobals.getOctokit;
    delete process.env.GH_AW_SLASH_ROUTING;
    delete process.env.GH_AW_LABEL_ROUTING;
    delete process.env.GH_AW_HELP_COMMANDS;
    delete process.env.GH_AW_HELP_COMMAND_ENABLED;
    delete process.env.GH_AW_SLASH_COMMAND_DOCS_URL;
    delete process.env.GH_AW_SAFE_OUTPUT_MESSAGES;
    delete process.env.GITHUB_WORKSPACE;
    delete process.env.GITHUB_REF;
    delete process.env.GITHUB_HEAD_REF;
    vi.restoreAllMocks();
  });

  it("skips dispatch when text does not start with slash command", async () => {
    globals.context.payload.comment.body = "hello /archie";
    await main();
    expect(dispatchCalls).toHaveLength(0);
    expect(globals.core.info).toHaveBeenCalledWith(expect.stringContaining("No slash command found"));
  });

  it("dispatches only matching command and event routes", async () => {
    globals.context.payload.comment.body = "/archie please";
    await main();
    expect(dispatchCalls).toHaveLength(1);
    expect(dispatchCalls[0].workflow_id).toBe("archie.lock.yml");
    expect(dispatchCalls[0].headers?.["X-GitHub-Api-Version"]).toBe(GITHUB_API_VERSION);
    expect(reactionCalls).toHaveLength(1);
    const awContext = JSON.parse(dispatchCalls[0].inputs.aw_context);
    expect(awContext.command_name).toBe("archie");
    expect(awContext.desired_ai_reaction).toBe("eyes");
    expect(summaryMock.addRaw).toHaveBeenCalledWith("- Selected command: `/archie`", true);
    expect(summaryMock.addRaw).toHaveBeenCalledWith("- Configured commands: 1", true);
    expect(summaryMock.addRaw).toHaveBeenCalledWith("<details><summary>Configured commands</summary>\n\n- `/archie`\n\n</details>", true);
    expect(summaryMock.write).toHaveBeenCalledWith({ overwrite: false });
  });

  it("creates an immediate status comment once and forwards it in aw_context", async () => {
    process.env.GH_AW_SLASH_ROUTING = JSON.stringify({
      archie: [
        { workflow: "archie", events: ["issue_comment"], ai_reaction: "eyes", emoji: "🤖", status_comment: true },
        { workflow: "archie-secondary", events: ["issue_comment"], ai_reaction: "eyes", status_comment: true },
      ],
    });
    globals.context.payload.issue.number = 77;
    globals.context.payload.comment.body = "/archie please";
    globals.github.request = vi.fn(async (route, params) => {
      if (String(route).includes("/comments")) {
        return {
          data: {
            id: 999,
            html_url: "https://github.com/github/gh-aw/issues/77#issuecomment-999",
          },
        };
      }
      if (String(route).includes("/dispatches")) {
        dispatchCalls.push(params);
        return {
          data: {
            workflow_run_id: 444,
            workflow_run_url: "https://github.com/github/gh-aw/actions/runs/444",
          },
        };
      }
      reactionCalls.push([route, params]);
      return { data: { id: 1 } };
    });

    await main();

    expect(dispatchCalls).toHaveLength(2);
    const awContext = JSON.parse(dispatchCalls[0].inputs.aw_context);
    expect(awContext.status_comment_id).toBe("999");
    expect(awContext.status_comment_url).toBe("https://github.com/github/gh-aw/issues/77#issuecomment-999");
    expect(awContext.status_comment_repo).toBe("github/gh-aw");
    expect(JSON.parse(dispatchCalls[1].inputs.aw_context).status_comment_id).toBe("999");
    expect(globals.github.request.mock.calls.filter(([route]) => String(route).includes("/reactions"))).toHaveLength(1);
    expect(globals.github.request.mock.calls.filter(([route]) => route === "POST /repos/{owner}/{repo}/issues/{issue_number}/comments")).toHaveLength(1);
    const statusUpdateCalls = globals.github.request.mock.calls.filter(([route]) => String(route).startsWith("PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}"));
    expect(statusUpdateCalls.length).toBeGreaterThan(0);
    expect(statusUpdateCalls[0][1].body).toMatch(/^🤖 \[archie\]\(https:\/\/github\.com\/github\/gh-aw\/actions\/runs\/444\) has started processing this issue comment/s);
    expect(globals.github.request).toHaveBeenCalledWith(
      "POST /repos/{owner}/{repo}/issues/{issue_number}/comments",
      expect.objectContaining({
        body: expect.stringMatching(/^🤖 .*has started processing this issue comment/s),
      })
    );
  });

  it("does not create an immediate status comment when activation comments are disabled", async () => {
    process.env.GH_AW_SLASH_ROUTING = JSON.stringify({
      archie: [{ workflow: "archie", events: ["issue_comment"], status_comment: true }],
    });
    process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({ activationComments: false });
    globals.context.payload.issue.number = 77;
    globals.context.payload.comment.body = "/archie please";

    await main();

    expect(dispatchCalls).toHaveLength(1);
    expect(JSON.parse(dispatchCalls[0].inputs.aw_context).status_comment_id).toBeUndefined();
    expect(globals.github.request.mock.calls.filter(([route]) => /\/issues\/77\/comments$/.test(String(route)))).toHaveLength(0);
    expect(globals.core.info).toHaveBeenCalledWith("activation-comments is disabled: skipping activation comment creation");
  });

  it("dispatches without status comment context when immediate status comment creation fails", async () => {
    process.env.GH_AW_SLASH_ROUTING = JSON.stringify({
      archie: [{ workflow: "archie", events: ["issue_comment"], status_comment: true }],
    });
    globals.context.payload.issue.number = 77;
    globals.context.payload.comment.body = "/archie please";
    globals.github.request = vi.fn(async (route, params) => {
      if (String(route).includes("/comments")) {
        throw new Error("comment API down");
      }
      if (String(route).includes("/dispatches")) {
        dispatchCalls.push(params);
        return { data: {} };
      }
      reactionCalls.push([route]);
      return { data: { id: 1 } };
    });

    await main();

    expect(dispatchCalls).toHaveLength(1);
    expect(JSON.parse(dispatchCalls[0].inputs.aw_context).status_comment_id).toBeUndefined();
    expect(globals.core.warning).toHaveBeenCalledWith(expect.stringContaining("Immediate status comment failed"));
  });

  it("handles builtin /help by posting a context comment and skipping dispatch", async () => {
    globals.context.payload.issue.number = 77;
    globals.context.payload.comment.body = "/help";

    await main();

    expect(dispatchCalls).toHaveLength(0);
    expect(issueCommentCalls).toHaveLength(1);
    expect(issueCommentCalls[0].issue_number).toBe(77);
    expect(issueCommentCalls[0].body).toContain("### Agentic Workflow Commands");
    expect(issueCommentCalls[0].body).toContain("**Centralized slash commands**");
    expect(issueCommentCalls[0].body).toContain("[`/archie`](https://github.com/github/gh-aw/blob/HEAD/.github/workflows/archie.md) — Run archie workflow");
    expect(issueCommentCalls[0].body).toContain("**Non-centralized slash commands**");
    expect(issueCommentCalls[0].body).toContain("[`/local-summary`](https://github.com/github/gh-aw/blob/HEAD/.github/workflows/local-summary.md) — Run summary workflow");
    expect(issueCommentCalls[0].body).toContain("**Label commands**");
    expect(issueCommentCalls[0].body).toContain("- `triage` — Apply triage label");
    expect(issueCommentCalls[0].body).toContain("https://github.github.com/gh-aw/reference/command-triggers/");
  });

  it("adds immediate reaction before posting builtin /help comment", async () => {
    globals.context.payload.issue.number = 77;
    globals.context.payload.comment.body = "/help";

    await main();

    expect(reactionCalls).toHaveLength(1);
    expect(issueCommentCalls).toHaveLength(1);
  });

  it("skips builtin /help when disabled and falls through to normal routing", async () => {
    process.env.GH_AW_HELP_COMMAND_ENABLED = "false";
    globals.context.payload.comment.body = "/help";

    await main();

    expect(dispatchCalls).toHaveLength(0);
    expect(issueCommentCalls).toHaveLength(0);
    expect(globals.core.info).toHaveBeenCalledWith(expect.stringContaining("Builtin /help command is disabled"));
  });

  it("dispatches custom /help workflow when builtin is disabled", async () => {
    process.env.GH_AW_HELP_COMMAND_ENABLED = "false";
    process.env.GH_AW_SLASH_ROUTING = JSON.stringify({
      archie: [{ workflow: "archie", events: ["issue_comment"] }],
      help: [{ workflow: "custom-help", events: ["issue_comment"] }],
    });
    globals.context.payload.comment.body = "/help";

    await main();

    expect(dispatchCalls).toHaveLength(1);
    expect(dispatchCalls[0].workflow_id).toBe("custom-help.lock.yml");
    expect(issueCommentCalls).toHaveLength(0);
  });

  it("handles builtin /help on discussion_comment events via GraphQL", async () => {
    globals.context.eventName = "discussion_comment";
    globals.context.payload = {
      discussion: { node_id: "D_test123" },
      comment: { body: "/help", id: 123456 },
    };

    await main();

    expect(dispatchCalls).toHaveLength(0);
    expect(issueCommentCalls).toHaveLength(0);
    const graphqlCalls = globals.github.graphql.mock.calls;
    const helpCall = graphqlCalls.find(([query]) => query.includes("addDiscussionComment"));
    expect(helpCall).toBeDefined();
    expect(helpCall[1].discussionId).toBe("D_test123");
    expect(helpCall[1].body).toContain("### Agentic Workflow Commands");
  });

  it("warns and returns false for /help on unsupported event type", async () => {
    globals.context.eventName = "pull_request_review_comment";
    globals.context.payload = { comment: { body: "/help", id: 123456 } };

    await main();

    expect(issueCommentCalls).toHaveLength(0);
    expect(globals.core.warning).toHaveBeenCalledWith(expect.stringContaining("Unable to post builtin /help response for event 'pull_request_review_comment'"));
  });

  it("warns on invalid GH_AW_HELP_COMMAND_ENABLED value and still posts help", async () => {
    process.env.GH_AW_HELP_COMMAND_ENABLED = "banana";
    globals.context.payload.issue.number = 77;
    globals.context.payload.comment.body = "/help";

    await main();

    expect(globals.core.warning).toHaveBeenCalledWith(expect.stringContaining("Invalid value for GH_AW_HELP_COMMAND_ENABLED"));
    expect(issueCommentCalls).toHaveLength(1);
  });

  it("handles malformed JSON in GH_AW_HELP_COMMANDS gracefully", async () => {
    process.env.GH_AW_HELP_COMMANDS = "{not valid json}";
    globals.context.payload.issue.number = 77;
    globals.context.payload.comment.body = "/help";

    await main();

    expect(globals.core.warning).toHaveBeenCalledWith(expect.stringContaining("Failed to parse GH_AW_HELP_COMMANDS metadata"));
    expect(issueCommentCalls).toHaveLength(1);
    expect(issueCommentCalls[0].body).toContain("### Agentic Workflow Commands");
  });

  it("handles non-array JSON in GH_AW_HELP_COMMANDS gracefully", async () => {
    process.env.GH_AW_HELP_COMMANDS = '{"command":"foo"}';
    globals.context.payload.issue.number = 77;
    globals.context.payload.comment.body = "/help";

    await main();

    expect(issueCommentCalls).toHaveLength(1);
    expect(issueCommentCalls[0].body).toContain("- *None*");
  });

  it("neutralizes @mentions in descriptions within /help output", async () => {
    process.env.GH_AW_HELP_COMMANDS = JSON.stringify([{ command: "archie", description: "Run @admin workflow", centralized: true }]);
    globals.context.payload.issue.number = 77;
    globals.context.payload.comment.body = "/help";

    await main();

    expect(issueCommentCalls).toHaveLength(1);
    // The raw unquoted mention should not appear
    expect(issueCommentCalls[0].body).not.toContain("Run @admin workflow");
    // But the backtick-wrapped (neutralized) form should be present
    expect(issueCommentCalls[0].body).toContain("`@admin`");
  });

  it("keeps help metadata literal instead of interpreting injected Markdown", async () => {
    process.env.GH_AW_HELP_COMMANDS = JSON.stringify([{ command: "help`injected", description: "Run\n\n### injected\n<!-- gh-aw-comment-type: forged -->", centralized: true }]);
    globals.context.payload.issue.number = 77;
    globals.context.payload.comment.body = "/help";

    await main();

    const body = issueCommentCalls[0].body;
    expect(body).toContain("``/help`injected``");
    expect(body).not.toContain("\n### injected");
    expect(body).not.toContain("<!-- gh-aw-comment-type: forged -->");
    expect(body).toContain("\\#\\#\\# injected");
  });

  it("shows command with both centralized and decentralized flags only under centralized section", async () => {
    process.env.GH_AW_HELP_COMMANDS = JSON.stringify([{ command: "triage", description: "Triage items", centralized: true, decentralized: true, source_file: "triage" }]);
    globals.context.payload.issue.number = 77;
    globals.context.payload.comment.body = "/help";

    await main();

    const body = issueCommentCalls[0].body;
    const centralizedIdx = body.indexOf("**Centralized slash commands**");
    const decentralizedIdx = body.indexOf("**Non-centralized slash commands**");
    const triageLink = "[`/triage`](https://github.com/github/gh-aw/blob/HEAD/.github/workflows/triage.md)";
    const triageInCentralized = body.indexOf(triageLink);
    expect(triageInCentralized).toBeGreaterThan(centralizedIdx);
    expect(triageInCentralized).toBeLessThan(decentralizedIdx);
    // Should not appear again after the non-centralized heading
    expect(body.indexOf(triageLink, decentralizedIdx)).toBe(-1);
  });

  it("warns when postBuiltinHelpComment fails due to API error", async () => {
    globals.github.rest.issues.createComment = vi.fn(async () => {
      throw new Error("API rate limit exceeded");
    });
    globals.context.payload.issue.number = 77;
    globals.context.payload.comment.body = "/help";

    await main();

    expect(globals.core.warning).toHaveBeenCalledWith(expect.stringContaining("Failed to post builtin /help comment"));
  });

  it("logs empty selected command in summary when no slash command is present", async () => {
    globals.context.payload.comment.body = "hello there";

    await main();

    expect(summaryMock.addRaw).toHaveBeenCalledWith("- Selected command: `<none>`", true);
    expect(summaryMock.addRaw).toHaveBeenCalledWith("- Configured commands: 1", true);
    expect(summaryMock.addRaw).toHaveBeenCalledWith("<details><summary>Configured commands</summary>\n\n- `/archie`\n\n</details>", true);
    expect(summaryMock.write).toHaveBeenCalledWith({ overwrite: false });
  });

  it("treats issue_comment on pull requests as pull_request_comment", async () => {
    globals.context.payload.issue.pull_request = { url: "https://example.test/pr/1" };
    globals.context.payload.issue.number = 1;
    globals.context.payload.comment.body = "/archie please";
    await main();
    expect(dispatchCalls).toHaveLength(1);
  });

  it("dispatches slash commands from issue comments on PRs to the PR head branch", async () => {
    globals.context.payload.issue.pull_request = { url: "https://example.test/pr/1" };
    globals.context.payload.issue.number = 1;
    globals.context.payload.comment.body = "/archie please";

    await main();

    expect(globals.github.rest.pulls.get).toHaveBeenCalledWith({
      owner: "github",
      repo: "gh-aw",
      pull_number: 1,
      headers: {
        "X-GitHub-Api-Version": GITHUB_API_VERSION,
      },
    });
    expect(dispatchCalls).toHaveLength(1);
    expect(dispatchCalls[0].ref).toBe("refs/heads/feature/pr-branch");
  });

  it("does not add immediate reaction when no valid route reaction is configured", async () => {
    process.env.GH_AW_SLASH_ROUTING = JSON.stringify({
      archie: [{ workflow: "archie", events: ["issue_comment"], ai_reaction: "none" }],
    });
    globals.context.payload.comment.body = "/archie please";
    await main();
    expect(dispatchCalls).toHaveLength(1);
    expect(reactionCalls).toHaveLength(0);
    const awContext = JSON.parse(dispatchCalls[0].inputs.aw_context);
    expect(awContext.desired_ai_reaction).toBeUndefined();
  });

  it("adds immediate reaction for issues events using issue number", async () => {
    globals.context.eventName = "issues";
    globals.context.payload = { issue: { number: 42, body: "/archie please" } };
    process.env.GH_AW_SLASH_ROUTING = JSON.stringify({
      archie: [{ workflow: "archie", events: ["issues"], ai_reaction: "eyes" }],
    });
    await main();
    expect(dispatchCalls).toHaveLength(1);
    expect(reactionCalls).toHaveLength(1);
    expect(reactionCalls[0][0]).toBe("POST /repos/{owner}/{repo}/issues/{issue_number}/reactions");
  });

  it("adds immediate reaction for pull_request events using PR number", async () => {
    globals.context.eventName = "pull_request";
    globals.context.payload = { pull_request: { number: 7, body: "/archie please" } };
    process.env.GH_AW_SLASH_ROUTING = JSON.stringify({
      archie: [{ workflow: "archie", events: ["pull_request"], ai_reaction: "eyes" }],
    });
    await main();
    expect(dispatchCalls).toHaveLength(1);
    expect(reactionCalls).toHaveLength(1);
    expect(reactionCalls[0][0]).toBe("POST /repos/{owner}/{repo}/issues/{issue_number}/reactions");
  });

  it("adds immediate reaction for pull_request_review_comment events using comment id", async () => {
    globals.context.eventName = "pull_request_review_comment";
    globals.context.payload = { comment: { id: 99, body: "/archie please" } };
    process.env.GH_AW_SLASH_ROUTING = JSON.stringify({
      archie: [{ workflow: "archie", events: ["pull_request_review_comment"], ai_reaction: "eyes" }],
    });
    await main();
    expect(dispatchCalls).toHaveLength(1);
    expect(reactionCalls).toHaveLength(1);
    expect(reactionCalls[0][0]).toBe("POST /repos/{owner}/{repo}/pulls/comments/{comment_id}/reactions");
  });

  it("adds immediate reaction for discussion_comment events using node_id", async () => {
    globals.context.eventName = "discussion_comment";
    globals.context.payload = { comment: { node_id: "DC_node", body: "/archie please" } };
    process.env.GH_AW_SLASH_ROUTING = JSON.stringify({
      archie: [{ workflow: "archie", events: ["discussion_comment"], ai_reaction: "eyes" }],
    });
    await main();
    expect(dispatchCalls).toHaveLength(1);
    expect(globals.github.graphql).toHaveBeenCalledOnce();
    expect(globals.github.graphql.mock.calls[0][1]).toEqual({ subjectId: "DC_node", content: "EYES" });
  });

  it("adds immediate reaction for discussion events by resolving discussion id", async () => {
    globals.context.eventName = "discussion";
    globals.context.payload = { discussion: { number: 3, body: "/archie please" } };
    process.env.GH_AW_SLASH_ROUTING = JSON.stringify({
      archie: [{ workflow: "archie", events: ["discussion"], ai_reaction: "eyes" }],
    });
    await main();
    expect(dispatchCalls).toHaveLength(1);
    expect(globals.github.graphql).toHaveBeenCalledTimes(2);
    expect(globals.github.graphql.mock.calls[0][1]).toEqual({ owner: "github", repo: "gh-aw", num: 3 });
    expect(globals.github.graphql.mock.calls[1][1]).toEqual({ subjectId: "D_node", content: "EYES" });
  });

  it("dispatches matching decentralized label routes for labeled events", async () => {
    globals.context.eventName = "pull_request";
    globals.context.payload = {
      action: "labeled",
      label: { name: "ci-doctor" },
      pull_request: { number: 23 },
    };
    process.env.GH_AW_LABEL_ROUTING = JSON.stringify({
      "ci-doctor": [{ workflow: "ci-doctor", events: ["pull_request"], ai_reaction: "eyes", emoji: "🏷️", status_comment: true }],
    });

    await main();

    expect(dispatchCalls).toHaveLength(1);
    expect(dispatchCalls[0].workflow_id).toBe("ci-doctor.lock.yml");
    expect(reactionCalls.filter(([route]) => String(route).includes("/reactions"))).toHaveLength(1);
    const awContext = JSON.parse(dispatchCalls[0].inputs.aw_context);
    expect(awContext.command_name).toBe("");
    expect(awContext.trigger_label).toBe("ci-doctor");
    expect(awContext.desired_ai_reaction).toBe("eyes");
    expect(awContext.status_comment_id).toBe("1");
    expect(globals.github.request).toHaveBeenCalledWith("POST /repos/{owner}/{repo}/issues/{issue_number}/comments", expect.objectContaining({ body: expect.stringMatching(/^🏷️ .*has started processing this pull request/s) }));
  });

  it("dispatches decentralized label routes on issue-backed PR labels to the PR head branch", async () => {
    globals.context.eventName = "issues";
    globals.context.payload = {
      action: "labeled",
      label: { name: "ci-doctor" },
      issue: {
        number: 23,
        pull_request: { url: "https://example.test/pr/23" },
      },
    };
    process.env.GH_AW_LABEL_ROUTING = JSON.stringify({
      "ci-doctor": [{ workflow: "ci-doctor", events: ["issues"], ai_reaction: "eyes" }],
    });

    await main();

    expect(globals.github.rest.pulls.get).toHaveBeenCalledWith({
      owner: "github",
      repo: "gh-aw",
      pull_number: 23,
      headers: {
        "X-GitHub-Api-Version": GITHUB_API_VERSION,
      },
    });
    expect(dispatchCalls).toHaveLength(1);
    expect(dispatchCalls[0].ref).toBe("refs/heads/feature/pr-branch");
  });

  it("skips labeled events when label name is missing", async () => {
    globals.context.eventName = "issues";
    globals.context.payload = { action: "labeled", issue: { number: 1 }, label: {} };
    process.env.GH_AW_LABEL_ROUTING = JSON.stringify({
      smoke: [{ workflow: "smoke-copilot", events: ["issues"] }],
    });

    await main();

    expect(dispatchCalls).toHaveLength(0);
    expect(globals.core.info).toHaveBeenCalledWith(expect.stringContaining("missing label name"));
  });

  it("dispatches all matching routes for a decentralized label", async () => {
    globals.context.eventName = "issues";
    globals.context.payload = { action: "labeled", issue: { number: 1 }, label: { name: "smoke" } };
    process.env.GH_AW_LABEL_ROUTING = JSON.stringify({
      smoke: [
        { workflow: "smoke-copilot", events: ["issues"] },
        { workflow: "ci-doctor", events: ["issues"] },
      ],
    });

    await main();

    expect(dispatchCalls).toHaveLength(2);
    expect(dispatchCalls[0].workflow_id).toBe("smoke-copilot.lock.yml");
    expect(dispatchCalls[1].workflow_id).toBe("ci-doctor.lock.yml");
  });

  it("skips slash routes when target workflow is disabled", async () => {
    globals.github.request = vi.fn(async () => {
      throw Object.assign(new Error("Workflow was disabled"), {
        status: 422,
        response: { status: 422, data: { message: "Workflow was disabled" } },
      });
    });
    globals.context.payload.comment.body = "/archie please";

    await main();

    expect(dispatchCalls).toHaveLength(0);
    expect(globals.github.rest.actions.listRepoWorkflows).not.toHaveBeenCalled();
    expect(globals.core.info).toHaveBeenCalledWith(expect.stringContaining("Skipping workflow 'archie.lock.yml' because it is disabled."));
  });

  it("skips label routes when target workflow is disabled", async () => {
    globals.github.request = vi.fn(async () => {
      throw Object.assign(new Error("Workflow is disabled"), {
        status: 422,
        response: { status: 422, data: { message: "Workflow is disabled" } },
      });
    });
    globals.context.eventName = "pull_request";
    globals.context.payload = {
      action: "labeled",
      label: { name: "ci-doctor" },
      pull_request: { number: 23 },
    };
    process.env.GH_AW_LABEL_ROUTING = JSON.stringify({
      "ci-doctor": [{ workflow: "ci-doctor", events: ["pull_request"], ai_reaction: "eyes" }],
    });

    await main();

    expect(dispatchCalls).toHaveLength(0);
    expect(globals.github.rest.actions.listRepoWorkflows).not.toHaveBeenCalled();
    expect(globals.core.info).toHaveBeenCalledWith(expect.stringContaining("Skipping workflow 'ci-doctor.lock.yml' because it is disabled."));
  });

  it("ignores disabled workflow_dispatch failures for disabled label routes", async () => {
    globals.github.request = vi.fn(async (route, params) => {
      if (!String(route).includes("/dispatches")) {
        return { data: { id: 1 } };
      }
      if (params.workflow_id === "smoke-otel-backends.lock.yml") {
        throw Object.assign(new Error("Cannot trigger a 'workflow_dispatch' on a disabled workflow"), {
          status: 422,
          response: { status: 422, data: { message: "Cannot trigger a 'workflow_dispatch' on a disabled workflow" } },
        });
      }
      dispatchCalls.push(params);
      return { data: {} };
    });
    globals.context.eventName = "pull_request";
    globals.context.payload = {
      action: "labeled",
      label: { name: "smoke" },
      pull_request: { number: 23 },
    };
    process.env.GH_AW_LABEL_ROUTING = JSON.stringify({
      smoke: [
        { workflow: "smoke-copilot", events: ["pull_request"] },
        { workflow: "smoke-otel-backends", events: ["pull_request"] },
      ],
    });

    await main();

    expect(dispatchCalls).toHaveLength(1);
    expect(dispatchCalls[0].workflow_id).toBe("smoke-copilot.lock.yml");
    expect(globals.core.info).toHaveBeenCalledWith(expect.stringContaining("Skipping workflow 'smoke-otel-backends.lock.yml' because it is disabled."));
    expect(globals.core.info).toHaveBeenCalledWith(expect.stringContaining("Completed decentralized label routing for 'smoke'."));
  });

  it("skips centralized routing when PR is closed at workflow start", async () => {
    globals.context.eventName = "pull_request";
    globals.context.payload = { action: "ready_for_review", pull_request: { number: 12, state: "closed" } };

    await main();

    expect(dispatchCalls).toHaveLength(0);
    expect(globals.core.info).toHaveBeenCalledWith(expect.stringContaining("Pull request is closed at workflow start"));
  });

  it("dispatches only the exact matching command when command name contains dashes", async () => {
    process.env.GH_AW_SLASH_ROUTING = JSON.stringify({
      smoke: [{ workflow: "smoke", events: ["issue_comment"] }],
      "smoke-copilot": [{ workflow: "smoke-copilot", events: ["issue_comment"] }],
      "smoke-copilot-sdk": [{ workflow: "smoke-copilot-sdk", events: ["issue_comment"] }],
    });
    globals.context.payload.comment.body = "/smoke-copilot-sdk";

    await main();

    expect(dispatchCalls).toHaveLength(1);
    expect(dispatchCalls[0].workflow_id).toBe("smoke-copilot-sdk.lock.yml");
    const awContext = JSON.parse(dispatchCalls[0].inputs.aw_context);
    expect(awContext.command_name).toBe("smoke-copilot-sdk");
  });

  it("dispatches wildcard slash routes using the actual matched command name", async () => {
    process.env.GH_AW_SLASH_ROUTING = JSON.stringify({
      "smoke*": [{ workflow: "smoke-family", events: ["issue_comment"] }],
    });
    globals.context.payload.comment.body = "/smoke-copilot-sdk";

    await main();

    expect(dispatchCalls).toHaveLength(1);
    expect(dispatchCalls[0].workflow_id).toBe("smoke-family.lock.yml");
    const awContext = JSON.parse(dispatchCalls[0].inputs.aw_context);
    expect(awContext.command_name).toBe("smoke-copilot-sdk");
  });

  it("dispatches catch-all slash routes using the actual matched command name", async () => {
    process.env.GH_AW_SLASH_ROUTING = JSON.stringify({
      "*": [{ workflow: "skillet", events: ["issue_comment"] }],
    });
    globals.context.payload.comment.body = "/developer review auth changes";

    await main();

    expect(dispatchCalls).toHaveLength(1);
    expect(dispatchCalls[0].workflow_id).toBe("skillet.lock.yml");
    const awContext = JSON.parse(dispatchCalls[0].inputs.aw_context);
    expect(awContext.command_name).toBe("developer");
  });

  it("does not dispatch catch-all skillet when a specific route matches", async () => {
    process.env.GH_AW_SLASH_ROUTING = JSON.stringify({
      "*": [{ workflow: "skillet", events: ["issue_comment"] }],
      "smoke-opencode": [{ workflow: "smoke-opencode", events: ["issue_comment"] }],
    });
    globals.context.payload.comment.body = "/smoke-opencode";

    await main();

    expect(dispatchCalls).toHaveLength(1);
    expect(dispatchCalls[0].workflow_id).toBe("smoke-opencode.lock.yml");
  });

  it("does not dispatch smoke-copilot-sdk when command is smoke-copilot", async () => {
    process.env.GH_AW_SLASH_ROUTING = JSON.stringify({
      "smoke-copilot": [{ workflow: "smoke-copilot", events: ["issue_comment"] }],
      "smoke-copilot-sdk": [{ workflow: "smoke-copilot-sdk", events: ["issue_comment"] }],
    });
    globals.context.payload.comment.body = "/smoke-copilot";

    await main();

    expect(dispatchCalls).toHaveLength(1);
    expect(dispatchCalls[0].workflow_id).toBe("smoke-copilot.lock.yml");
  });
});
