import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import path from "node:path";
import { fixture, mock } from "./work_queue_issues_checks.cjs";
import { main } from "./work_queue_issues.cjs";
import { mutateIssues } from "./work_queue_issue_api.cjs";

const collisionMessage = "Name has already been taken ghp_secret_collision_error";
const collisionError = { type: "UNPROCESSABLE", path: ["createLabel"], message: collisionMessage };

describe("safe projection diagnostics and label rejection recovery", () => {
  it("renders prepared Issue and comment trees only at the GraphQL mutation boundary", async () => {
    const report = {
      type: "report",
      title: "Work",
      children: [
        { type: "paragraph", children: [{ type: "text", value: "## literal <!-- injected -->", agentTainted: true }] },
        { type: "xml_marker", name: "work-id", value: "abc", agentTainted: false },
      ],
    };
    const graphql = vi.fn().mockResolvedValue({ m0: { issue: { id: "issue" } }, m1: { commentEdge: { node: { id: "comment" } } } });
    await mutateIssues({ graphql }, [
      { name: "createIssue", type: "CreateIssueInput", input: { repositoryId: "repo", title: "Work" }, report, selection: "issue { id }" },
      { name: "addComment", type: "AddCommentInput", input: { subjectId: "issue" }, report, selection: "commentEdge { node { id } }" },
    ]);
    const variables = graphql.mock.calls[0][1];
    expect(variables.m0.body).toContain("\\#\\# literal &lt;");
    expect(variables.m1.body).toBe(variables.m0.body.trimEnd());
    expect(variables.m1.title).toBeUndefined();
    expect(variables.m0.report).toBeUndefined();
    expect(variables.m0.body).toContain("<!-- work-id: abc -->");
  });

  let stderr;
  beforeEach(() => {
    stderr = vi.spyOn(process.stderr, "write").mockReturnValue(true);
    vi.stubEnv("GH_AW_PROMPTS_DIR", path.resolve(import.meta.dirname, "../md"));
  });
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
  });

  it.each(["RATE_LIMITED", "FORBIDDEN", "NOT_FOUND", "GRAPHQL_VALIDATION_FAILED"].flatMap(type => ["create", "recolor"].map(kind => ({ type, kind }))))(
    "releases projection locks after confirmed label rejection and permits repair: %j",
    async ({ type, kind }) => {
      const m = mock(fixture(), { labelMissing: kind === "create" });
      if (kind === "recolor") m.discovery.label.color = "808080";
      const original = m.options.githubClient.graphql;
      const sentinel = "ghp_secret_API_error_payload";
      const requestId = "ABCD:1234:5678:9ABC:DEF0";
      m.options.githubClient.graphql = async (query, variables) => {
        if (query.includes(kind === "create" ? "WorkQueueTrackingLabel" : "WorkQueueLabelColor"))
          throw Object.assign(new Error(sentinel), {
            status: 403,
            response: { headers: { "x-github-request-id": requestId }, data: { message: sentinel, token: sentinel } },
            data: { [kind === "create" ? "createLabel" : "updateLabel"]: null },
            errors: [{ type, message: sentinel }],
          });
        return original(query, variables);
      };
      const pending = await main(m.options);
      expect(pending.pending[0].reason).toContain("projection_label_pending");
      expect(m.locks.size).toBe(0);
      const output = stderr.mock.calls.map(call => call[0]).join("");
      expect(output).toContain("projection.label.failed");
      expect(output).toContain("http_status=403");
      expect(output).toContain(requestId);
      const category = { RATE_LIMITED: "rate_limited", FORBIDDEN: "forbidden", NOT_FOUND: "not_found", GRAPHQL_VALIDATION_FAILED: "validation_failed" }[type];
      expect(output).toContain(`${category}=true`);
      expect(JSON.stringify([pending, m.warnings, output])).not.toContain(sentinel);
      m.options.githubClient.graphql = original;
      expect(await main(m.options)).toMatchObject({ pending: [] });
      expect(m.locks.size).toBe(0);
    }
  );

  it.each([
    ["transport", { status: 504 }],
    ["unknown GraphQL error", { errors: [{ type: "INTERNAL" }], data: { createLabel: null } }],
    ["partial receipt", { errors: [{ type: "FORBIDDEN" }], data: { createLabel: { label: { id: "possibly-written" } } } }],
    ["alias-specific rate failure", { errors: [{ type: "RATE_LIMITED", path: ["createLabel"] }], data: { createLabel: null } }],
    ["mixed rejection", { errors: [{ type: "FORBIDDEN" }, { type: "INTERNAL" }], data: { createLabel: null } }],
  ])("retains the no-overlap fence after %s", async (_name, failure) => {
    const m = mock(fixture(), { labelMissing: true });
    const original = m.options.githubClient.graphql;
    m.options.githubClient.graphql = async (query, variables) => {
      if (query.includes("WorkQueueTrackingLabel")) throw Object.assign(new Error("secret-label-error"), failure);
      return original(query, variables);
    };
    expect((await main(m.options)).pending[0].reason).toContain("projection_write_ambiguous");
    expect(m.locks.size).toBeGreaterThan(0);
    m.options.githubClient.graphql = original;
    expect((await main(m.options)).pending.length).toBeGreaterThan(0);
    expect(m.calls.some(call => call[0] === "mutation")).toBe(false);
  });

  it.each([false, true])("discovers and verifies an actual structured duplicate-label rejection with response envelope=%s", async envelope => {
    const m = mock(fixture());
    const original = m.options.githubClient.graphql;
    let lookups = 0;
    m.options.githubClient.graphql = async (query, variables) => {
      if (query.includes("WorkQueueStatusLabel")) lookups++;
      if (query.includes("WorkQueueTrackingLabel")) {
        m.labels.set(variables.input.name, { id: "concurrent-label", name: variables.input.name, color: "808080" });
        throw Object.assign(new Error(collisionMessage), {
          data: { createLabel: null },
          errors: [collisionError],
          ...(envelope ? { response: { data: { data: { createLabel: null }, errors: [collisionError] } } } : {}),
        });
      }
      return original(query, variables);
    };
    expect(await main(m.options)).toMatchObject({ pending: [] });
    expect(lookups).toBe(2);
    expect(m.labels.get("work:queued").color).toBe("7057FF");
    expect(m.locks.size).toBe(0);
    expect(JSON.stringify([m.warnings, stderr.mock.calls])).not.toContain("ghp_secret_collision_error");
  });

  it.each([
    ["transport text", { status: 504 }],
    ["unstructured collision", { errors: [{ message: collisionMessage }], data: { createLabel: null } }],
    ["missing null receipt", { errors: [collisionError] }],
    ["partial create", { errors: [collisionError], data: { createLabel: { label: { id: "native-label" } } } }],
    ["another native receipt", { errors: [collisionError], data: { createLabel: null, other: { id: "native-label" } } }],
    ["wrong mutation path", { errors: [{ ...collisionError, path: ["updateLabel"] }], data: { createLabel: null } }],
    ["missing mutation path", { errors: [{ ...collisionError, path: undefined }], data: { createLabel: null } }],
    ["wrong error type", { errors: [{ ...collisionError, type: "INTERNAL" }], data: { createLabel: null } }],
    ["mixed rejection", { errors: [collisionError, { type: "INTERNAL" }], data: { createLabel: null } }],
    ["partial response envelope", { errors: [collisionError], data: { createLabel: null }, response: { data: { data: { createLabel: { label: { id: "native-label" } } } } } }],
  ])("retains fences without duplicate discovery for collision-like %s failures", async (_name, failure) => {
    const m = mock(fixture());
    const original = m.options.githubClient.graphql;
    let lookups = 0;
    m.options.githubClient.graphql = async (query, variables) => {
      if (query.includes("WorkQueueStatusLabel")) lookups++;
      if (query.includes("WorkQueueTrackingLabel")) {
        m.labels.set(variables.input.name, { id: "possibly-created-label", name: variables.input.name, color: "808080" });
        throw Object.assign(new Error(collisionMessage), failure);
      }
      return original(query, variables);
    };
    const result = await main(m.options);
    expect(result.pending[0].reason).toContain("projection_write_ambiguous");
    expect(lookups).toBe(1);
    expect(m.labels.get("work:queued").color).toBe("808080");
    expect(m.locks.size).toBeGreaterThan(0);
    expect(JSON.stringify([result, m.warnings, stderr.mock.calls])).not.toContain("ghp_secret_collision_error");
    m.options.githubClient.graphql = original;
    expect((await main(m.options)).pending.length).toBeGreaterThan(0);
    expect(m.labels.get("work:queued").color).toBe("808080");
    expect(m.calls.some(call => call[0] === "mutation")).toBe(false);
  });

  it("does not classify a recoloring error as a duplicate createLabel rejection", async () => {
    const m = mock(fixture());
    m.discovery.label.color = "808080";
    const original = m.options.githubClient.graphql;
    m.options.githubClient.graphql = async (query, variables) => {
      if (query.includes("WorkQueueLabelColor")) throw Object.assign(new Error(collisionMessage), { data: { createLabel: null }, errors: [collisionError] });
      return original(query, variables);
    };
    expect((await main(m.options)).pending[0].reason).toContain("projection_write_ambiguous");
    expect(m.locks.size).toBeGreaterThan(0);
  });

  it.each([
    ["projection_comment_pending", "canonical comment is missing"],
    ["projection_pagination_pending", "pagination is incomplete"],
    ["projection_journal_invalid", "stored native receipt disagrees"],
  ])("explains finite local %s errors without exposing their raw messages", async (code, explanation) => {
    const m = mock(fixture());
    const result = await main({
      ...m.options,
      withProjectionLocks: async () => {
        throw Object.assign(new Error("secret-local-details"), { code });
      },
    });
    expect(result.pending[0].reason).toContain(explanation);
    expect(JSON.stringify([result, m.warnings, stderr.mock.calls])).not.toContain("secret-local-details");
  });

  it.each(["RATE_LIMITED", "INTERNAL"])("never logs raw creation errors while handling %s, even with debug logging enabled", async type => {
    vi.stubEnv("DEBUG", "*");
    const messages = [];
    for (const method of ["debug", "info", "warning"]) vi.spyOn(global.core, method).mockImplementation(message => messages.push(message));
    let attempts = 0;
    const sleep = vi.fn(async () => {});
    const result = await mutateIssues(
      {
        graphql: async () => {
          attempts++;
          throw Object.assign(new Error("ghp_secret_creation_error"), { errors: [{ type, message: "ghp_secret_creation_error" }], data: { m0: null } });
        },
      },
      [{ key: "work", name: "createIssue", type: "CreateIssueInput", input: { repositoryId: "repo", title: "Work", body: "details" }, selection: "issue { id }" }],
      { sleep }
    );
    expect(attempts).toBe(type === "RATE_LIMITED" ? 4 : 1);
    expect(result.results[0].uncertain).toBe(type !== "RATE_LIMITED");
    expect(sleep.mock.calls.map(([delay]) => delay)).toEqual(type === "RATE_LIMITED" ? [1000, 2000, 4000] : []);
    const output = stderr.mock.calls.map(call => call[0]).join("");
    expect(output).toContain("projection.mutation.failed");
    expect(JSON.stringify([messages, output])).not.toContain("ghp_secret_creation_error");
  });
});
