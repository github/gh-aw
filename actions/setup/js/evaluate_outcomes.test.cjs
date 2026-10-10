import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { createRequire } from "module";
import crypto from "crypto";

const req = createRequire(import.meta.url);
const { evaluateItem, normalizeOutcome } = req("./evaluate_outcomes.cjs");

describe("outcome GitHub API transport", () => {
  function withTransport(implementation, check) {
    const childProcess = req("child_process");
    const modulePath = req.resolve("./evaluate_outcomes.cjs");
    const cached = req.cache[modulePath];
    const mock = vi.spyOn(childProcess, "execFileSync").mockImplementation(implementation);
    delete req.cache[modulePath];
    try {
      check(req("./evaluate_outcomes.cjs").ghAPI, mock);
    } finally {
      mock.mockRestore();
      req.cache[modulePath] = cached;
    }
  }

  it("flattens all array pages and preserves a single object response", () => {
    withTransport(
      () => '[[{"id":1}],[{"id":2}]]',
      (api, mock) => {
        expect(api("repos/acme/repo/issues")).toEqual([{ id: 1 }, { id: 2 }]);
        expect(mock).toHaveBeenCalledWith("gh", ["api", "repos/acme/repo/issues", "--paginate", "--slurp"], expect.objectContaining({ encoding: "utf8" }));
        mock.mockReturnValue('[{"state":"open"}]');
        expect(api("repos/acme/repo/issues/1")).toEqual({ state: "open" });
        mock.mockReturnValue("[[]]");
        expect(api("repos/acme/repo/issues")).toEqual([]);
      }
    );
  });

  it("preserves parsed HTTP status and distinguishes non-HTTP failures", () => {
    withTransport(
      () => {
        throw Object.assign(new Error("gh failed"), { stderr: "gh: Not Found (HTTP 404)" });
      },
      (api, mock) => {
        expect(() => api("repos/acme/repo/issues/1")).toThrow(expect.objectContaining({ status: 404 }));
        mock.mockImplementation(() => {
          throw Object.assign(new Error("gh failed"), { stderr: "connection reset" });
        });
        expect(() => api("repos/acme/repo/issues/1")).toThrow(expect.objectContaining({ status: null }));
      }
    );
  });

  it("rejects malformed and missing page envelopes", () => {
    withTransport(
      () => "[]",
      (api, mock) => {
        expect(() => api("repos/acme/repo/issues")).toThrow("Invalid GitHub outcome response");
        mock.mockReturnValue('{"state":"open"}');
        expect(() => api("repos/acme/repo/issues/1")).toThrow("Invalid GitHub outcome response");
        mock.mockReturnValue("not json");
        expect(() => api("repos/acme/repo/issues")).toThrow(SyntaxError);
      }
    );
  });
});

function hashBody(body) {
  return crypto
    .createHash("sha256")
    .update(
      String(body || "")
        .replace(/\r\n/g, "\n")
        .replace(/[ \t]+\n/g, "\n")
        .trim(),
      "utf8"
    )
    .digest("hex");
}

/**
 * @param {Record<string, any>} apiResponses
 * @returns {(endpoint: string) => any}
 */
function mockAPI(apiResponses) {
  return endpoint => {
    if (!(endpoint in apiResponses)) {
      return null;
    }
    return apiResponses[endpoint];
  };
}

const createAPIStub = mockAPI;

describe("evaluate_outcomes retained updates", () => {
  it("evaluates update_issue retained and reverted states from persisted execution metadata", () => {
    const retained = evaluateItem(
      {
        type: "update_issue",
        repo: "acme/repo",
        number: 12,
        before_state: {
          title: "Old title",
          body_hash: hashBody("Old body"),
          state: "open",
          labels: ["triage"],
          assignees: [],
        },
        after_state: {
          title: "New title",
          body_hash: hashBody("New body"),
          state: "open",
          labels: ["triage", "bug"],
          assignees: ["octo"],
        },
      },
      "acme/repo",
      {
        ghAPI: createAPIStub({
          "repos/acme/repo/issues/12": {
            title: "New title",
            body: "New body",
            state: "open",
            labels: [{ name: "triage" }, { name: "bug" }],
            assignees: [{ login: "octo" }],
          },
        }),
      }
    );
    expect(retained).toMatchObject({
      result: "accepted",
      outcome_status: "accepted",
      evidence_strength: "medium",
      signal: "state_retained",
      detail: "update retained",
    });

    const reverted = evaluateItem(
      {
        type: "update_issue",
        repo: "acme/repo",
        number: 12,
        before_state: {
          title: "Old title",
          body_hash: hashBody("Old body"),
          state: "open",
        },
        after_state: {
          title: "New title",
          body_hash: hashBody("New body"),
          state: "closed",
        },
      },
      "acme/repo",
      {
        ghAPI: createAPIStub({
          "repos/acme/repo/issues/12": {
            title: "Old title",
            body: "Old body",
            state: "open",
            labels: [],
            assignees: [],
          },
        }),
      }
    );
    expect(reverted).toMatchObject({
      result: "rejected",
      outcome_status: "rejected",
      evidence_strength: "strong",
      signal: "state_reverted",
      detail: "update reverted",
    });
  });

  it("evaluates update_pull_request retained-merged and replaced states from persisted execution metadata", () => {
    const retainedMerged = evaluateItem(
      {
        type: "update_pull_request",
        repo: "acme/repo",
        number: 99,
        before_state: {
          title: "Old title",
          body_hash: hashBody("Old body"),
          state: "open",
          base: "main",
          draft: true,
          head_sha: "abc123",
        },
        after_state: {
          title: "New title",
          body_hash: hashBody("New body"),
          state: "open",
          base: "release",
          draft: false,
          head_sha: "def456",
        },
      },
      "acme/repo",
      {
        ghAPI: createAPIStub({
          "repos/acme/repo/pulls/99": {
            title: "New title",
            body: "New body",
            state: "closed",
            merged: true,
            base: { ref: "release" },
            draft: false,
            head: { sha: "def456" },
          },
        }),
      }
    );
    expect(retainedMerged).toMatchObject({
      result: "accepted",
      outcome_status: "accepted",
      evidence_strength: "strong",
      signal: "state_retained_and_merged",
      detail: "update retained and merged",
    });

    const replaced = evaluateItem(
      {
        type: "update_pull_request",
        repo: "acme/repo",
        number: 99,
        before_state: {
          title: "Old title",
          body_hash: hashBody("Old body"),
          state: "open",
          base: "main",
          draft: true,
          head_sha: "abc123",
        },
        after_state: {
          title: "New title",
          body_hash: hashBody("New body"),
          state: "open",
          base: "release",
          draft: false,
          head_sha: "def456",
        },
      },
      "acme/repo",
      {
        ghAPI: createAPIStub({
          "repos/acme/repo/pulls/99": {
            title: "Maintainer rewrite",
            body: "New body with edits",
            state: "open",
            merged: false,
            base: { ref: "hotfix" },
            draft: false,
            head: { sha: "zzz999" },
          },
        }),
      }
    );
    expect(replaced).toMatchObject({
      result: "rejected",
      outcome_status: "rejected",
      evidence_strength: "strong",
      signal: "state_replaced",
      detail: "update replaced",
    });
  });
});

describe("evaluate_outcomes.cjs", () => {
  it("maps existence-only fallback to weak unknown evidence", () => {
    expect(normalizeOutcome("unknown", "object still exists")).toEqual({
      outcome_status: "unknown",
      evidence_strength: "weak",
      signal: "target_exists_only",
    });
  });

  it("maps dedicated review lifecycle details to typed signals", () => {
    expect(normalizeOutcome("accepted", "review approved")).toEqual({
      outcome_status: "accepted",
      evidence_strength: "strong",
      signal: "review_approved",
    });
    expect(normalizeOutcome("rejected", "review request removed")).toEqual({
      outcome_status: "rejected",
      evidence_strength: "strong",
      signal: "review_request_removed",
    });
    expect(normalizeOutcome("rejected", "review dismissed")).toEqual({
      outcome_status: "rejected",
      evidence_strength: "strong",
      signal: "review_dismissed",
    });
  });

  it("classifies add_reviewer approval as accepted", () => {
    const api = endpoint => {
      if (endpoint.endsWith("/reviews")) {
        return [{ state: "APPROVED", submitted_at: "2026-05-12T01:00:00Z", user: { login: "reviewer1" } }];
      }
      if (endpoint.endsWith("/requested_reviewers")) {
        return { users: [], teams: [] };
      }
      throw new Error(`unexpected endpoint: ${endpoint}`);
    };

    const result = evaluateItem(
      {
        type: "add_reviewer",
        repo: "owner/repo",
        number: 42,
        timestamp: "2026-05-12T00:00:00Z",
        metadata: {
          requested_reviewers: ["reviewer1"],
        },
      },
      "owner/repo",
      api
    );

    expect(normalizeOutcome(result.result, result.detail)).toMatchObject({
      outcome_status: "accepted",
      evidence_strength: "strong",
      signal: "review_approved",
    });
  });

  it("classifies add_reviewer removal without review as rejected", () => {
    const api = endpoint => {
      if (endpoint.endsWith("/reviews")) {
        return [];
      }
      if (endpoint.endsWith("/requested_reviewers")) {
        return { users: [], teams: [] };
      }
      throw new Error(`unexpected endpoint: ${endpoint}`);
    };

    const result = evaluateItem(
      {
        type: "add_reviewer",
        repo: "owner/repo",
        number: 42,
        timestamp: "2026-05-12T00:00:00Z",
        metadata: {
          requested_reviewers: ["reviewer1"],
        },
      },
      "owner/repo",
      api
    );

    expect(normalizeOutcome(result.result, result.detail)).toMatchObject({
      outcome_status: "rejected",
      evidence_strength: "strong",
      signal: "review_request_removed",
    });
  });

  it("classifies add_reviewer pending requests as pending", () => {
    const api = endpoint => {
      if (endpoint.endsWith("/reviews")) {
        return [];
      }
      if (endpoint.endsWith("/requested_reviewers")) {
        return { users: [{ login: "reviewer1" }], teams: [] };
      }
      throw new Error(`unexpected endpoint: ${endpoint}`);
    };

    const result = evaluateItem(
      {
        type: "add_reviewer",
        repo: "owner/repo",
        number: 42,
        timestamp: "2026-05-12T00:00:00Z",
        metadata: {
          requested_reviewers: ["reviewer1"],
        },
      },
      "owner/repo",
      api
    );

    expect(normalizeOutcome(result.result, result.detail)).toMatchObject({
      outcome_status: "pending",
      evidence_strength: "medium",
      signal: "awaiting_review",
    });
  });

  it("uses the latest requested-reviewer state when approval is superseded", () => {
    const api = endpoint => {
      if (endpoint.endsWith("/reviews")) {
        return [
          { state: "APPROVED", submitted_at: "2026-05-12T01:00:00Z", user: { login: "reviewer1" } },
          { state: "CHANGES_REQUESTED", submitted_at: "2026-05-12T02:00:00Z", user: { login: "reviewer1" } },
        ];
      }
      if (endpoint.endsWith("/requested_reviewers")) {
        return { users: [], teams: [] };
      }
      throw new Error(`unexpected endpoint: ${endpoint}`);
    };

    const result = evaluateItem(
      {
        type: "add_reviewer",
        repo: "owner/repo",
        number: 42,
        timestamp: "2026-05-12T00:00:00Z",
        metadata: {
          requested_reviewers: ["reviewer1"],
        },
      },
      "owner/repo",
      api
    );

    expect(normalizeOutcome(result.result, result.detail)).toMatchObject({
      outcome_status: "accepted",
      evidence_strength: "medium",
      signal: "review_submitted",
    });
  });

  it("ignores malformed submitted_at values for review-request acceptance", () => {
    const api = endpoint => {
      if (endpoint.endsWith("/reviews")) {
        return [{ state: "APPROVED", submitted_at: "not-a-timestamp", user: { login: "reviewer1" } }];
      }
      if (endpoint.endsWith("/requested_reviewers")) {
        return { users: [], teams: [] };
      }
      throw new Error(`unexpected endpoint: ${endpoint}`);
    };

    const result = evaluateItem(
      {
        type: "add_reviewer",
        repo: "owner/repo",
        number: 42,
        timestamp: "2026-05-12T00:00:00Z",
        metadata: {
          requested_reviewers: ["reviewer1"],
        },
      },
      "owner/repo",
      api
    );

    expect(normalizeOutcome(result.result, result.detail)).toMatchObject({
      outcome_status: "rejected",
      evidence_strength: "strong",
      signal: "review_request_removed",
    });
  });

  it("classifies dismissed submitted reviews as rejected", () => {
    const api = endpoint => {
      if (endpoint.endsWith("/pulls/42")) {
        return { state: "open", merged: false };
      }
      if (endpoint.endsWith("/reviews")) {
        return [{ id: 101, state: "DISMISSED", submitted_at: "2026-05-12T01:00:00Z" }];
      }
      throw new Error(`unexpected endpoint: ${endpoint}`);
    };

    const result = evaluateItem(
      {
        type: "submit_pull_request_review",
        repo: "owner/repo",
        number: 42,
        timestamp: "2026-05-12T01:00:00Z",
        metadata: { review_id: 101 },
      },
      "owner/repo",
      api
    );

    expect(normalizeOutcome(result.result, result.detail)).toMatchObject({
      outcome_status: "rejected",
      evidence_strength: "strong",
      signal: "review_dismissed",
    });
  });

  it("classifies changes requested, push, and merge as accepted", () => {
    const api = endpoint => {
      if (endpoint.endsWith("/pulls/42")) {
        return { state: "closed", merged: true, merged_at: "2026-05-12T05:00:00Z" };
      }
      if (endpoint.endsWith("/reviews")) {
        return [{ id: 101, state: "CHANGES_REQUESTED", submitted_at: "2026-05-12T02:00:00Z" }];
      }
      if (endpoint.endsWith("/commits")) {
        return [{ commit: { committer: { date: "2026-05-12T03:00:00Z" } } }];
      }
      throw new Error(`unexpected endpoint: ${endpoint}`);
    };

    const result = evaluateItem(
      {
        type: "submit_pull_request_review",
        repo: "owner/repo",
        number: 42,
        timestamp: "2026-05-12T02:00:00Z",
        metadata: { review_id: 101 },
      },
      "owner/repo",
      api
    );

    expect(normalizeOutcome(result.result, result.detail)).toMatchObject({
      outcome_status: "accepted",
      evidence_strength: "medium",
      signal: "changes_requested_addressed",
    });
  });

  it("returns unknown outcome when commit dates are missing", () => {
    const api = endpoint => {
      if (endpoint.endsWith("/pulls/42")) {
        return { state: "closed", merged: true, merged_at: "2026-05-12T05:00:00Z" };
      }
      if (endpoint.endsWith("/reviews")) {
        return [{ id: 101, state: "CHANGES_REQUESTED", submitted_at: "2026-05-12T02:00:00Z" }];
      }
      if (endpoint.endsWith("/commits")) {
        return [{ commit: { committer: { date: "" }, author: { date: "" } } }];
      }
      throw new Error(`unexpected endpoint: ${endpoint}`);
    };

    const result = evaluateItem(
      {
        type: "submit_pull_request_review",
        repo: "owner/repo",
        number: 42,
        timestamp: "2026-05-12T02:00:00Z",
        metadata: { review_id: 101 },
      },
      "owner/repo",
      api
    );

    expect(normalizeOutcome(result.result, result.detail)).toMatchObject({
      outcome_status: "unknown",
      evidence_strength: "weak",
      signal: "unknown",
    });
  });

  it("classifies latest review on open PR as pending", () => {
    const api = endpoint => {
      if (endpoint.endsWith("/pulls/42")) {
        return { state: "open", merged: false };
      }
      if (endpoint.endsWith("/reviews")) {
        return [
          { id: 100, state: "COMMENTED", submitted_at: "2026-05-12T00:30:00Z" },
          { id: 101, state: "COMMENTED", submitted_at: "2026-05-12T01:00:00Z" },
        ];
      }
      throw new Error(`unexpected endpoint: ${endpoint}`);
    };

    const result = evaluateItem(
      {
        type: "submit_pull_request_review",
        repo: "owner/repo",
        number: 42,
        timestamp: "2026-05-12T01:00:00Z",
        metadata: { review_id: 101 },
      },
      "owner/repo",
      api
    );

    expect(normalizeOutcome(result.result, result.detail)).toMatchObject({
      outcome_status: "pending",
      evidence_strength: "medium",
      signal: "latest_review_pending",
    });
  });

  it("does not reject merged CHANGES_REQUESTED reviews without a follow-up push", () => {
    const api = endpoint => {
      if (endpoint.endsWith("/pulls/42")) {
        return { state: "closed", merged: true, merged_at: "2026-05-12T05:00:00Z" };
      }
      if (endpoint.endsWith("/reviews")) {
        return [{ id: 101, state: "CHANGES_REQUESTED", submitted_at: "2026-05-12T02:00:00Z" }];
      }
      if (endpoint.endsWith("/commits")) {
        return [];
      }
      throw new Error(`unexpected endpoint: ${endpoint}`);
    };

    const result = evaluateItem(
      {
        type: "submit_pull_request_review",
        repo: "owner/repo",
        number: 42,
        timestamp: "2026-05-12T02:00:00Z",
        metadata: { review_id: 101 },
      },
      "owner/repo",
      api
    );

    expect(result.result).toBe("unknown");
  });

  it("ignores unsubmitted reviews when checking latest open review state", () => {
    const api = endpoint => {
      if (endpoint.endsWith("/pulls/42")) {
        return { state: "open", merged: false };
      }
      if (endpoint.endsWith("/reviews")) {
        return [
          { id: 100, state: "COMMENTED", submitted_at: "2026-05-12T00:30:00Z" },
          { id: 101, state: "PENDING" },
        ];
      }
      throw new Error(`unexpected endpoint: ${endpoint}`);
    };

    const result = evaluateItem(
      {
        type: "submit_pull_request_review",
        repo: "owner/repo",
        number: 42,
        timestamp: "2026-05-12T00:30:00Z",
        metadata: { review_id: 100 },
      },
      "owner/repo",
      api
    );

    expect(result.result).toBe("pending");
    expect(result.detail).toBe("latest review awaiting outcome");
  });
});
