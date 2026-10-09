import { describe, expect, it } from "vitest";
const { wrapGithubClient } = require("./work_queue_issue_pacing.cjs");
const { isRetryableBeforeExecution, isConfirmedRejectionBeforeExecution } = require("./work_queue_issue_api.cjs");

function clock() {
  let time = 0;
  const delays = [];
  return {
    now: () => time,
    sleep: async delay => {
      delays.push(delay);
      time += delay;
    },
    delays,
  };
}

describe("Issue projection pacing", () => {
  it("paces mutation aliases, counts reads separately, and disables hidden GraphQL retries", async () => {
    const timer = clock();
    const calls = [];
    const github = {
      graphql: async function (query, variables) {
        expect(this).toBe(github);
        calls.push([query, variables]);
        return { value: true };
      },
    };
    const { client, metrics } = wrapGithubClient(github, timer);
    await client.graphql("query { viewer { login } }", {});
    await client.graphql("mutation { a: createIssue(input:$a) { issue { id } } b: addComment(input:$b) { clientMutationId } }", { request: { retries: 9, timeout: 123 } });
    await client.graphql("mutation { closeIssue(input:$a) { clientMutationId } }", {});
    expect(timer.delays).toEqual([2000, 1000]);
    expect(metrics).toEqual({ requests: 3, reads: 1, mutations: 2, paced_ms: 3000 });
    expect(calls[1][1].request).toEqual({ retries: 0, timeout: 123 });
  });

  it("paces a batch of label removals by mutation count", async () => {
    const timer = clock();
    const { client } = wrapGithubClient({ graphql: async () => ({ value: true }) }, timer);
    const removals = Array.from({ length: 50 }, (_, index) => `r${index}: removeLabelsFromLabelable(input:$r${index}) { clientMutationId }`).join(" ");
    await client.graphql(`mutation { ${removals} }`, {});
    expect(timer.delays).toEqual([50000]);
  });

  it("preserves REST method receivers and paces only writes", async () => {
    const timer = clock();
    const issues = {
      get: async function () {
        expect(this).toBe(issues);
        return "read";
      },
      listForRepo: async () => "list",
      create: async function (input) {
        expect(this).toBe(issues);
        return input;
      },
    };
    const { client, metrics } = wrapGithubClient({ rest: { issues }, identity: "unchanged" }, timer);
    expect(await client.rest.issues.get()).toBe("read");
    expect(await client.rest.issues.listForRepo()).toBe("list");
    expect(await client.rest.issues.create({ title: "test" })).toEqual({ title: "test" });
    expect(client.identity).toBe("unchanged");
    expect(timer.delays).toEqual([1000]);
    expect(metrics).toEqual({ requests: 3, reads: 2, mutations: 1, paced_ms: 1000 });
  });

  for (const signal of [{ status: 429 }, { status: 403, response: { headers: { "retry-after": "60" } } }, { status: 403, response: { headers: { "x-ratelimit-remaining": "0" } } }]) {
    it(`stops later requests after ${JSON.stringify(signal)} without replaying the failed write`, async () => {
      let attempts = 0;
      const error = Object.assign(new Error("rate limit"), signal);
      const { client, metrics } = wrapGithubClient(
        {
          graphql: async () => {
            attempts++;
            throw error;
          },
        },
        clock()
      );
      await expect(client.graphql("mutation { createIssue(input:$a) { clientMutationId } }", {})).rejects.toBe(error);
      await expect(client.graphql("query { viewer { login } }", {})).rejects.toMatchObject({ code: "projection_rate_pending" });
      expect(attempts).toBe(1);
      expect(metrics.requests).toBe(1);
    });
  }

  it("propagates an ordinary failure without incorrectly exhausting the rate budget", async () => {
    let attempts = 0;
    const { client, metrics } = wrapGithubClient(
      {
        graphql: async () => {
          if (++attempts === 1) throw new Error("connection lost");
          return "read";
        },
      },
      clock()
    );
    await expect(client.graphql("mutation { createIssue(input:$a) { clientMutationId } }", {})).rejects.toThrow("connection lost");
    expect(await client.graphql("query { viewer { login } }", {})).toBe("read");
    expect(metrics.requests).toBe(2);
  });
});

describe("confirmed pre-execution creation rejection", () => {
  for (const [name, error, rejected] of [
    ["global rate rejection", { errors: [{ type: "RATE_LIMITED" }], data: { m0: null } }, true],
    ["empty-path rate rejection", { errors: [{ type: "RATE_LIMITED", path: [] }] }, true],
    ["alias-specific rate error", { errors: [{ type: "RATE_LIMITED", path: ["m0"] }] }, false],
    ["partial creation receipt", { errors: [{ type: "RATE_LIMITED" }], data: { m0: { issue: { id: "created" } } } }, false],
    ["mixed errors", { errors: [{ type: "RATE_LIMITED" }, { type: "INTERNAL" }] }, false],
    ["transport timeout", { status: 504 }, false],
    ["bare HTTP rate response", { status: 429 }, false],
    ["no GraphQL errors", { errors: [] }, false],
    ["malformed GraphQL error", { errors: [null] }, false],
  ]) {
    it(`classifies ${name} consistently for retry and recovery`, () => {
      expect(isConfirmedRejectionBeforeExecution(error)).toBe(rejected);
      expect(isRetryableBeforeExecution(error)).toBe(rejected);
    });
  }
});
