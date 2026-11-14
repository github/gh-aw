// @ts-check
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import fs from "fs";
import path from "path";
import os from "os";
import { createRequire } from "module";

const require = createRequire(import.meta.url);

describe("check_workflow_recompile_needed", () => {
  let mockCore;
  let mockGithub;
  let mockContext;
  let mockExec;
  let originalGlobals;
  let originalEnv;
  let workspaceDir;
  const testPromptsDir = path.join(os.tmpdir(), "gh-aw-test", "prompts");
  const templatePath = path.join(testPromptsDir, "workflow_recompile_issue.md");

  beforeEach(() => {
    // Save original environment
    originalEnv = {
      GH_AW_PROMPTS_DIR: process.env.GH_AW_PROMPTS_DIR,
      GH_AW_MAINTENANCE_GITHUB_TOKEN: process.env.GH_AW_MAINTENANCE_GITHUB_TOKEN,
      GITHUB_WORKSPACE: process.env.GITHUB_WORKSPACE,
    };

    // Set test prompts directory
    process.env.GH_AW_PROMPTS_DIR = testPromptsDir;
    workspaceDir = path.join(os.tmpdir(), "gh-aw-test", "workspace");
    process.env.GITHUB_WORKSPACE = workspaceDir;
    fs.mkdirSync(path.join(workspaceDir, ".github", "workflows"), { recursive: true });
    fs.writeFileSync(path.join(workspaceDir, ".github", "workflows", "example.lock.yml"), "name: example\n", "utf8");

    // Create the template file for testing
    const templateDir = path.dirname(templatePath);
    if (!fs.existsSync(templateDir)) {
      fs.mkdirSync(templateDir, { recursive: true });
    }

    const templateContent = `## Problem

The workflow lock files (\`.lock.yml\`) are out of sync with their source markdown files (\`.md\`). This means the workflows that run in GitHub Actions are not using the latest configuration.

## What needs to be done

The workflows need to be recompiled to regenerate the lock files from the markdown sources.

## Instructions

Recompile all workflows using one of the following methods:

### Using gh aw CLI

\`\`\`bash
gh aw compile --validate --verbose
\`\`\`

### Using gh-aw MCP Server

If you have the gh-aw MCP server configured, use the \`compile\` tool:

\`\`\`json
{
  "tool": "compile",
  "arguments": {
    "validate": true,
    "verbose": true
  }
}
\`\`\`

This will:
1. Build the latest version of \`gh-aw\`
2. Compile all workflow markdown files to YAML lock files
3. Ensure all workflows are up to date

After recompiling, commit the changes with a message like:
\`\`\`
Recompile workflows to update lock files
\`\`\`

## Detected Changes

The following workflow lock files have changes:

<details>
<summary>View diff</summary>

\`\`\`diff
{DIFF_CONTENT}
\`\`\`

</details>

## References

- **Repository:** {REPOSITORY}
`;

    fs.writeFileSync(templatePath, templateContent, "utf8");

    // Save original globals
    originalGlobals = {
      core: global.core,
      github: global.github,
      context: global.context,
      exec: global.exec,
    };

    // Setup mock core module
    mockCore = {
      debug: vi.fn(),
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      setSecret: vi.fn(),
      summary: {
        addHeading: vi.fn().mockReturnThis(),
        addRaw: vi.fn().mockReturnThis(),
        write: vi.fn().mockResolvedValue(undefined),
      },
    };

    // Setup mock github module
    mockGithub = {
      rest: {
        search: {
          issuesAndPullRequests: vi.fn(),
        },
        issues: {
          create: vi.fn(),
          createComment: vi.fn(),
        },
        pulls: {
          list: vi.fn(),
          create: vi.fn(),
          update: vi.fn(),
        },
        git: {
          getCommit: vi.fn(async () => ({ data: { tree: { sha: "base-tree-sha" } } })),
          createBlob: vi.fn(async () => ({ data: { sha: "new-blob-sha" } })),
          createTree: vi.fn(async () => ({ data: { sha: "new-tree-sha" } })),
          createCommit: vi.fn(async () => ({ data: { sha: "verified-initial-commit", verification: { verified: true } } })),
          createRef: vi.fn(),
        },
        repos: {
          get: vi.fn().mockResolvedValue({
            data: {
              node_id: "R_testowner_testrepo",
              default_branch: "main",
            },
          }),
        },
      },
      graphql: vi.fn().mockImplementation(query => {
        if (String(query).includes("createCommitOnBranch")) {
          return Promise.resolve({
            createCommitOnBranch: {
              commit: { oid: "signed-oid" },
            },
          });
        }
        return Promise.resolve({
          repository: {
            id: "repo-id",
            defaultBranchRef: { name: "main" },
          },
        });
      }),
    };

    // Setup mock context
    mockContext = {
      repo: {
        owner: "testowner",
        repo: "testrepo",
      },
      runId: 123456,
      payload: {
        repository: {
          html_url: "https://github.com/testowner/testrepo",
          default_branch: "main",
        },
      },
    };

    // Setup mock exec module
    mockExec = {
      exec: vi.fn(),
      getExecOutput: vi.fn(),
    };

    // Set globals for the module
    global.core = mockCore;
    global.github = mockGithub;
    global.context = mockContext;
    global.exec = mockExec;
  });

  afterEach(() => {
    // Restore environment variable
    if (originalEnv.GH_AW_PROMPTS_DIR !== undefined) {
      process.env.GH_AW_PROMPTS_DIR = originalEnv.GH_AW_PROMPTS_DIR;
    } else {
      delete process.env.GH_AW_PROMPTS_DIR;
    }
    if (originalEnv.GH_AW_MAINTENANCE_GITHUB_TOKEN !== undefined) {
      process.env.GH_AW_MAINTENANCE_GITHUB_TOKEN = originalEnv.GH_AW_MAINTENANCE_GITHUB_TOKEN;
    } else {
      delete process.env.GH_AW_MAINTENANCE_GITHUB_TOKEN;
    }
    if (originalEnv.GITHUB_WORKSPACE !== undefined) {
      process.env.GITHUB_WORKSPACE = originalEnv.GITHUB_WORKSPACE;
    } else {
      delete process.env.GITHUB_WORKSPACE;
    }

    // Clean up the test directory
    const testDir = path.join(os.tmpdir(), "gh-aw-test");
    if (fs.existsSync(testDir)) {
      fs.rmSync(testDir, { recursive: true, force: true });
    }

    // Restore original globals
    global.core = originalGlobals.core;
    global.github = originalGlobals.github;
    global.context = originalGlobals.context;
    global.exec = originalGlobals.exec;

    // Clear mock state and reset the module cache because each test dynamically imports the CJS module.
    vi.clearAllMocks();
    vi.resetModules();
  });

  it("should report no changes when workflows are up to date", async () => {
    // Mock exec to return no changes (empty diff output)
    mockExec.exec.mockResolvedValue(0);
    mockExec.getExecOutput.mockResolvedValue({ stdout: "", stderr: "", exitCode: 0 });

    const { main } = await import("./check_workflow_recompile_needed.cjs");
    await main();

    expect(mockCore.info).toHaveBeenCalledWith("✓ All workflow lock files are up to date");
    expect(mockGithub.rest.search.issuesAndPullRequests).not.toHaveBeenCalled();
  });

  it("should add comment to existing issue when workflows are out of sync", async () => {
    // Mock exec to return changes (non-empty diff output)
    mockExec.exec
      .mockImplementationOnce(async (cmd, args, options) => {
        if (options?.listeners?.stdout) {
          options.listeners.stdout(Buffer.from("diff content"));
        }
        return 1; // Non-zero exit code indicates changes
      })
      .mockImplementationOnce(async (cmd, args, options) => {
        if (options?.listeners?.stdout) {
          options.listeners.stdout(Buffer.from("detailed diff content"));
        }
        return 0;
      });
    mockExec.getExecOutput.mockImplementation(async (cmd, args) => {
      const joinedArgs = args.join(" ");
      if (joinedArgs === "diff --name-only .github/workflows/*.lock.yml") {
        return { stdout: ".github/workflows/example.lock.yml\n", stderr: "", exitCode: 0 };
      }
      return { stdout: "", stderr: "", exitCode: 0 };
    });

    // Mock search to return existing issue
    mockGithub.rest.search.issuesAndPullRequests.mockResolvedValue({
      data: {
        total_count: 1,
        items: [
          {
            number: 42,
            html_url: "https://github.com/testowner/testrepo/issues/42",
          },
        ],
      },
    });

    mockGithub.rest.issues.createComment.mockResolvedValue({});

    const { main } = await import("./check_workflow_recompile_needed.cjs");
    await main();

    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Found existing issue"));
    expect(mockGithub.rest.issues.createComment).toHaveBeenCalledWith({
      owner: "testowner",
      repo: "testrepo",
      issue_number: 42,
      body: expect.stringContaining("Workflows are still out of sync"),
    });
    expect(mockGithub.rest.issues.create).not.toHaveBeenCalled();
  });

  it("should create new issue when workflows are out of sync and no issue exists", async () => {
    // Mock exec to return changes (non-empty diff output)
    mockExec.exec
      .mockImplementationOnce(async (cmd, args, options) => {
        if (options?.listeners?.stdout) {
          options.listeners.stdout(Buffer.from("diff content"));
        }
        return 1;
      })
      .mockImplementationOnce(async (cmd, args, options) => {
        if (options?.listeners?.stdout) {
          options.listeners.stdout(Buffer.from("detailed diff content"));
        }
        return 0;
      });
    mockExec.getExecOutput.mockImplementation(async (cmd, args) => {
      const joinedArgs = args.join(" ");
      if (joinedArgs === "diff --name-only .github/workflows/*.lock.yml") {
        return { stdout: ".github/workflows/example.lock.yml\n", stderr: "", exitCode: 0 };
      }
      return { stdout: "", stderr: "", exitCode: 0 };
    });

    // Mock search to return no existing issue
    mockGithub.rest.search.issuesAndPullRequests.mockResolvedValue({
      data: {
        total_count: 0,
        items: [],
      },
    });

    mockGithub.rest.issues.create.mockResolvedValue({
      data: {
        number: 43,
        html_url: "https://github.com/testowner/testrepo/issues/43",
      },
    });

    const { main } = await import("./check_workflow_recompile_needed.cjs");
    await main();

    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("No existing issue found"));
    expect(mockGithub.rest.issues.create).toHaveBeenCalledWith({
      owner: "testowner",
      repo: "testrepo",
      title: "[aw] agentic workflows out of sync",
      body: expect.stringContaining("Using gh aw CLI"),
      labels: ["agentic-workflows", "maintenance"],
    });
  });

  it("should handle errors gracefully", async () => {
    // Mock exec to throw error
    mockExec.exec.mockRejectedValue(new Error("Git command failed"));
    mockExec.getExecOutput.mockResolvedValue({ stdout: "", stderr: "", exitCode: 0 });

    const { main } = await import("./check_workflow_recompile_needed.cjs");

    await expect(main()).rejects.toThrow("Git command failed");
    expect(mockCore.error).toHaveBeenCalledWith(expect.stringContaining("Failed to check for workflow changes"));
  });

  it("should create a pull request when PR mode is enabled", async () => {
    process.env.GH_AW_MAINTENANCE_GITHUB_TOKEN = "ghs_test_token";

    mockExec.exec.mockImplementation(async (cmd, args, options) => {
      const joinedArgs = args.join(" ");
      if (joinedArgs === "diff --exit-code .github/workflows/*.lock.yml") {
        options?.listeners?.stdout?.(Buffer.from("diff content"));
        return 1;
      }
      if (joinedArgs === "diff .github/workflows/*.lock.yml") {
        options?.listeners?.stdout?.(Buffer.from("detailed diff content"));
        return 0;
      }
      if (joinedArgs === "diff --cached --name-only") {
        options?.listeners?.stdout?.(Buffer.from(".github/workflows/example.lock.yml\n"));
        return 0;
      }
      if (joinedArgs === "cat-file blob blobhash") {
        options?.listeners?.stdout?.(Buffer.from("name: example\n"));
        return 0;
      }
      return 0;
    });
    mockExec.getExecOutput.mockImplementation(async (cmd, args) => {
      const joinedArgs = args.join(" ");
      if (joinedArgs === "diff --name-only .github/workflows/*.lock.yml") {
        return { stdout: ".github/workflows/example.lock.yml\n", stderr: "", exitCode: 0 };
      }
      if (joinedArgs === "rev-parse HEAD") {
        return { stdout: "base-head-sha\n", stderr: "", exitCode: 0 };
      }
      if (joinedArgs === "ls-remote origin refs/heads/aw/recompile-workflows") {
        return { stdout: "", stderr: "", exitCode: 0 };
      }
      if (joinedArgs === "rev-list --parents --topo-order --reverse base-head-sha..HEAD") {
        return { stdout: "commit-sha base-head-sha\n", stderr: "", exitCode: 0 };
      }
      if (joinedArgs === "diff-tree -r --raw commit-sha") {
        return { stdout: ":100644 100644 oldhash blobhash M\t.github/workflows/example.lock.yml\n", stderr: "", exitCode: 0 };
      }
      if (joinedArgs === "rev-parse commit-sha^") {
        return { stdout: "base-head-sha\n", stderr: "", exitCode: 0 };
      }
      if (joinedArgs === "log -1 --format=%B commit-sha") {
        return { stdout: "chore: recompile agentic workflows\n", stderr: "", exitCode: 0 };
      }
      return { stdout: "", stderr: "", exitCode: 0 };
    });

    mockGithub.rest.search.issuesAndPullRequests.mockResolvedValue({
      data: {
        total_count: 1,
        items: [
          {
            number: 42,
            html_url: "https://github.com/testowner/testrepo/issues/42",
          },
        ],
      },
    });
    mockGithub.rest.pulls.list.mockResolvedValue({ data: [] });
    mockGithub.rest.pulls.create.mockResolvedValue({
      data: {
        number: 44,
        html_url: "https://github.com/testowner/testrepo/pull/44",
      },
    });

    const { main } = await import("./check_workflow_recompile_needed.cjs");
    await main();

    expect(mockGithub.rest.pulls.list).toHaveBeenCalledWith({
      owner: "testowner",
      repo: "testrepo",
      state: "open",
      head: "testowner:aw/recompile-workflows",
      per_page: 1,
    });
    expect(mockGithub.rest.pulls.create).toHaveBeenCalledWith(
      expect.objectContaining({
        owner: "testowner",
        repo: "testrepo",
        title: "[aw] recompile agentic workflows",
        head: "aw/recompile-workflows",
        base: "main",
      })
    );
    const createdBody = mockGithub.rest.pulls.create.mock.calls[0][0].body;
    expect(createdBody).toContain("Workflow Recompilation");
    expect(createdBody).toContain("Fixes #42");
    expect(createdBody).toContain(".github/workflows/example.lock.yml");
    expect(createdBody).not.toContain("detailed diff content");
    expect(mockGithub.rest.git.createRef).toHaveBeenCalledWith({
      owner: "testowner",
      repo: "testrepo",
      ref: "refs/heads/aw/recompile-workflows",
      sha: "verified-initial-commit",
    });
    expect(mockGithub.rest.issues.create).not.toHaveBeenCalled();
  });

  it("should require signed commits when creating a maintenance pull request", async () => {
    process.env.GH_AW_MAINTENANCE_GITHUB_TOKEN = "ghs_test_token";

    const pushSignedCommitsModule = require("./push_signed_commits.cjs");
    const pushSignedSpy = vi.spyOn(pushSignedCommitsModule, "pushSignedCommits").mockResolvedValue("signed-oid");

    mockExec.exec.mockImplementation(async (cmd, args, options) => {
      const joinedArgs = args.join(" ");
      if (joinedArgs === "diff --exit-code .github/workflows/*.lock.yml") {
        options?.listeners?.stdout?.(Buffer.from("diff content"));
        return 1;
      }
      if (joinedArgs === "diff .github/workflows/*.lock.yml") {
        options?.listeners?.stdout?.(Buffer.from("detailed diff content"));
        return 0;
      }
      return 0;
    });
    mockExec.getExecOutput.mockImplementation(async (cmd, args) => {
      const joinedArgs = args.join(" ");
      if (joinedArgs === "diff --name-only .github/workflows/*.lock.yml") {
        return { stdout: ".github/workflows/example.lock.yml\n", stderr: "", exitCode: 0 };
      }
      if (joinedArgs === "rev-parse HEAD") {
        return { stdout: "base-head-sha\n", stderr: "", exitCode: 0 };
      }
      if (joinedArgs === "ls-remote origin refs/heads/aw/recompile-workflows") {
        return { stdout: "", stderr: "", exitCode: 0 };
      }
      return { stdout: "", stderr: "", exitCode: 0 };
    });

    mockGithub.rest.search.issuesAndPullRequests.mockResolvedValue({
      data: {
        total_count: 0,
        items: [],
      },
    });
    mockGithub.rest.pulls.list.mockResolvedValue({ data: [] });
    mockGithub.rest.pulls.create.mockResolvedValue({
      data: {
        number: 44,
        html_url: "https://github.com/testowner/testrepo/pull/44",
      },
    });

    const { main } = await import("./check_workflow_recompile_needed.cjs");
    await main();

    expect(pushSignedSpy).toHaveBeenCalledWith(
      expect.objectContaining({
        owner: "testowner",
        repo: "testrepo",
        branch: "aw/recompile-workflows",
        allowGitPushFallback: false,
      })
    );
  });

  it("should reuse an existing pull request when PR mode is enabled", async () => {
    process.env.GH_AW_MAINTENANCE_GITHUB_TOKEN = "ghs_test_token";

    mockExec.exec.mockImplementation(async (cmd, args, options) => {
      const joinedArgs = args.join(" ");
      if (joinedArgs === "diff --exit-code .github/workflows/*.lock.yml") {
        options?.listeners?.stdout?.(Buffer.from("diff content"));
        return 1;
      }
      if (joinedArgs === "diff .github/workflows/*.lock.yml") {
        options?.listeners?.stdout?.(Buffer.from("detailed diff content"));
        return 0;
      }
      if (joinedArgs === "diff --cached --name-only") {
        options?.listeners?.stdout?.(Buffer.from(".github/workflows/example.lock.yml\n"));
        return 0;
      }
      if (joinedArgs === "cat-file blob blobhash") {
        options?.listeners?.stdout?.(Buffer.from("name: example\n"));
        return 0;
      }
      return 0;
    });
    mockExec.getExecOutput.mockImplementation(async (cmd, args) => {
      const joinedArgs = args.join(" ");
      if (joinedArgs === "diff --name-only .github/workflows/*.lock.yml") {
        return { stdout: ".github/workflows/example.lock.yml\n", stderr: "", exitCode: 0 };
      }
      if (joinedArgs === "rev-parse HEAD") {
        return { stdout: "base-head-sha\n", stderr: "", exitCode: 0 };
      }
      if (joinedArgs === "ls-remote origin refs/heads/aw/recompile-workflows") {
        return { stdout: "remote-head-sha\trefs/heads/aw/recompile-workflows\n", stderr: "", exitCode: 0 };
      }
      if (joinedArgs === "show refs/remotes/origin/aw/recompile-workflows:.github/workflows/example.lock.yml") {
        return { stdout: "older: true\n", stderr: "", exitCode: 0 };
      }
      if (joinedArgs === "rev-list --parents --topo-order --reverse remote-head-sha..HEAD") {
        return { stdout: "commit-sha remote-head-sha\n", stderr: "", exitCode: 0 };
      }
      if (joinedArgs === "diff-tree -r --raw commit-sha") {
        return { stdout: ":100644 100644 oldhash blobhash M\t.github/workflows/example.lock.yml\n", stderr: "", exitCode: 0 };
      }
      if (joinedArgs === "log -1 --format=%B commit-sha") {
        return { stdout: "chore: recompile agentic workflows\n", stderr: "", exitCode: 0 };
      }
      return { stdout: "", stderr: "", exitCode: 0 };
    });

    mockGithub.rest.search.issuesAndPullRequests.mockResolvedValue({
      data: {
        total_count: 1,
        items: [
          {
            number: 42,
            html_url: "https://github.com/testowner/testrepo/issues/42",
          },
        ],
      },
    });
    mockGithub.rest.pulls.list.mockResolvedValue({
      data: [
        {
          number: 45,
          html_url: "https://github.com/testowner/testrepo/pull/45",
        },
      ],
    });

    const { main } = await import("./check_workflow_recompile_needed.cjs");
    await main();

    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Found existing pull request"));
    expect(mockGithub.rest.pulls.update).toHaveBeenCalledWith(
      expect.objectContaining({
        owner: "testowner",
        repo: "testrepo",
        pull_number: 45,
      })
    );
    const updatedBody = mockGithub.rest.pulls.update.mock.calls[0][0].body;
    expect(updatedBody).toContain("Workflow Recompilation");
    expect(updatedBody).toContain("Fixes #42");
    expect(updatedBody).toContain(".github/workflows/example.lock.yml");
    expect(updatedBody).not.toContain("detailed diff content");
    expect(mockGithub.rest.git.createRef).not.toHaveBeenCalled();
    expect(mockGithub.rest.pulls.create).not.toHaveBeenCalled();
    expect(mockGithub.rest.issues.create).not.toHaveBeenCalled();
  });

  it("should skip signed commit push when the existing maintenance branch already matches", async () => {
    process.env.GH_AW_MAINTENANCE_GITHUB_TOKEN = "ghs_test_token";

    mockExec.exec.mockImplementation(async (cmd, args, options) => {
      const joinedArgs = args.join(" ");
      if (joinedArgs === "diff --exit-code .github/workflows/*.lock.yml") {
        options?.listeners?.stdout?.(Buffer.from("diff content"));
        return 1;
      }
      if (joinedArgs === "diff .github/workflows/*.lock.yml") {
        options?.listeners?.stdout?.(Buffer.from("detailed diff content"));
        return 0;
      }
      return 0;
    });
    mockExec.getExecOutput.mockImplementation(async (cmd, args) => {
      const joinedArgs = args.join(" ");
      if (joinedArgs === "diff --name-only .github/workflows/*.lock.yml") {
        return { stdout: ".github/workflows/example.lock.yml\n", stderr: "", exitCode: 0 };
      }
      if (joinedArgs === "rev-parse HEAD") {
        return { stdout: "base-head-sha\n", stderr: "", exitCode: 0 };
      }
      if (joinedArgs === "ls-remote origin refs/heads/aw/recompile-workflows") {
        return { stdout: "remote-head-sha\trefs/heads/aw/recompile-workflows\n", stderr: "", exitCode: 0 };
      }
      if (joinedArgs === "show refs/remotes/origin/aw/recompile-workflows:.github/workflows/example.lock.yml") {
        return { stdout: "name: example\n", stderr: "", exitCode: 0 };
      }
      return { stdout: "", stderr: "", exitCode: 0 };
    });

    mockGithub.rest.search.issuesAndPullRequests.mockResolvedValue({
      data: {
        total_count: 1,
        items: [
          {
            number: 42,
            html_url: "https://github.com/testowner/testrepo/issues/42",
          },
        ],
      },
    });
    mockGithub.rest.pulls.list.mockResolvedValue({
      data: [
        {
          number: 45,
          html_url: "https://github.com/testowner/testrepo/pull/45",
        },
      ],
    });

    const { main } = await import("./check_workflow_recompile_needed.cjs");
    await main();

    expect(mockGithub.rest.pulls.update).toHaveBeenCalledWith(
      expect.objectContaining({
        pull_number: 45,
      })
    );
    expect(mockCore.info).toHaveBeenCalledWith("Existing maintenance branch already contains the latest compiled workflow lock files");
  });

  it("should stay in issue mode without a configured maintenance token secret", async () => {
    mockExec.exec
      .mockImplementationOnce(async (cmd, args, options) => {
        if (options?.listeners?.stdout) {
          options.listeners.stdout(Buffer.from("diff content"));
        }
        return 1;
      })
      .mockImplementationOnce(async (cmd, args, options) => {
        if (options?.listeners?.stdout) {
          options.listeners.stdout(Buffer.from("detailed diff content"));
        }
        return 0;
      });
    mockExec.getExecOutput.mockResolvedValueOnce({ stdout: ".github/workflows/example.lock.yml\n", stderr: "", exitCode: 0 });

    mockGithub.rest.search.issuesAndPullRequests.mockResolvedValue({
      data: {
        total_count: 0,
        items: [],
      },
    });
    mockGithub.rest.issues.create.mockResolvedValue({
      data: {
        number: 43,
        html_url: "https://github.com/testowner/testrepo/issues/43",
      },
    });

    const { main } = await import("./check_workflow_recompile_needed.cjs");
    await main();

    expect(mockGithub.rest.pulls.create).not.toHaveBeenCalled();
    expect(mockGithub.rest.issues.create).toHaveBeenCalled();
    expect(mockCore.info).toHaveBeenCalledWith("Configured maintenance token present: false");
  });
});
