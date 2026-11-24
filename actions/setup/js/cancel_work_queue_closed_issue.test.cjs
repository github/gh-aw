// @ts-check
import { describe, expect, it } from "vitest";
import { main } from "./cancel_work_queue_closed_issue.cjs";
import { replayTransactions } from "./work_queue_replay.cjs";
import { commit, dispatchNext, fakeQueueGitHub, ledgerText, projector, submit, work } from "./work_queue_test_helpers.cjs";

const resource = { kind: "issue", host: "github.com", repository: "owner/repo", repository_id: "77", resource_id: "1000", number: "1" };

function linkedLog() {
  const link = { kind: "IssueLink", work_id: work("a").work_id, resource };
  return commit(submit([], [work("a")]), projector, "issue_link", { operations: [link] }, [link]);
}

function closedEvent(issueId = 1000) {
  return { repo: { owner: "owner", repo: "repo" }, runId: 400, runAttempt: 1, eventName: "issues", payload: { action: "closed", issue: { id: issueId, number: 1 } } };
}

async function run(log, context = closedEvent(), liveIssue = () => ({ id: 1000, number: 1, state: "closed" }), setup = queue => {}) {
  const queue = fakeQueueGitHub(ledgerText(log));
  queue.githubClient.rest.issues = { get: async ({ issue_number }) => ({ data: { ...liveIssue(), number: issue_number } }) };
  setup(queue);
  const result = await main({ githubClient: queue.githubClient, context, core: { info() {} } });
  const state = replayTransactions(
    queue
      .contents()
      .trim()
      .split("\n")
      .map(line => JSON.parse(line))
  );
  return { result, state, queue };
}

describe("cancel_work_queue_closed_issue", () => {
  it("cancels available Work backed by the closed Issue", async () => {
    const { result, state } = await run(linkedLog());
    expect(result).toEqual({ status: "cancelled", committed: true });
    expect(state.works.get(work("a").work_id)).toMatchObject({ state: "cancelled", cancellation_reason: "related_issue_closed" });
  });

  it("does not cancel when the matching Issue has been reopened", async () => {
    const { result, state, queue } = await run(linkedLog(), closedEvent(), () => ({ id: 1000, state: "open" }));
    expect(result).toEqual({ status: "available", committed: false });
    expect(state.works.get(work("a").work_id).state).toBe("available");
    expect(queue.state.writes).toBe(0);
  });

  it("does not cancel when the Issue number now resolves to a different Issue", async () => {
    const { result } = await run(linkedLog(), closedEvent(), () => ({ id: 2000, state: "closed" }));
    expect(result).toEqual({ status: "available", committed: false });
  });

  it("rechecks the live Issue on a publication retry after it reopens", async () => {
    let live = "closed";
    const { result, state } = await run(
      linkedLog(),
      closedEvent(),
      () => ({ id: 1000, state: live }),
      queue => {
        queue.state.beforeUpdate = () => {
          live = "open";
          queue.install(ledgerText(submit(linkedLog(), [work("b")])));
        };
      }
    );
    expect(result).toEqual({ status: "available", committed: false });
    expect(state.works.get(work("a").work_id).state).toBe("available");
    expect(state.works.get(work("b").work_id).state).toBe("available");
  });

  it("leaves claimed Work alone", async () => {
    const { log } = dispatchNext(linkedLog());
    const { result, state, queue } = await run(log);
    expect(result).toEqual({ status: "claimed" });
    expect(state.works.get(work("a").work_id).state).toBe("claimed");
    expect(queue.state.writes).toBe(0);
  });

  it("ignores unbound Issues and other events", async () => {
    expect((await run(linkedLog(), closedEvent(999))).result).toEqual({ status: "unbound" });
    expect((await run(linkedLog(), { ...closedEvent(), payload: { action: "opened", issue: { id: 1000 } } })).result).toEqual({ status: "ignored" });
  });
});
