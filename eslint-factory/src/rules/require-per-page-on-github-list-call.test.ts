import { RuleTester } from "eslint";
import { describe, expect, it } from "vitest";
import { requirePerPageOnGithubListCallRule } from "./require-per-page-on-github-list-call";

const ruleTester = new RuleTester({ languageOptions: { ecmaVersion: 2022, sourceType: "commonjs" } });

describe("require-per-page-on-github-list-call", () => {
  it("uses the correct docs URL", () => {
    expect(requirePerPageOnGithubListCallRule.meta.docs.url).toBe("https://github.com/github/gh-aw/tree/main/eslint-factory#require-per-page-on-github-list-call");
  });

  it("valid and invalid cases", () => {
    ruleTester.run("require-per-page-on-github-list-call", requirePerPageOnGithubListCallRule, {
      valid: [
        `async function f() { await github.rest.issues.listComments({ owner, repo, issue_number: 1, per_page: 100 }); }`,
        `async function f() { await github.rest.issues.listComments({ owner, repo, issue_number: 1, "per_page": 100 }); }`,
        `async function f() { await github.rest.actions.listWorkflowRuns({ owner, repo, per_page: MAX }); }`,
        `async function f() { await github.rest.repos.listLanguages({ owner, repo }); }`,
        `async function f() { await github.rest.issues.listComments({ ...base }); }`,
        `async function f() { await github.rest.issues.listComments(params); }`,
        `async function f() { await github.rest.issues.get({ owner, repo, issue_number: 1 }); }`,
        `async function f() { await github.paginate(github.rest.issues.listComments, { owner, repo }); }`,
        `async function f() { await list.listItems({ owner }); }`,
      ],
      invalid: [
        {
          code: `async function f() { await github.rest.actions.listWorkflowRunArtifacts({ owner, repo, run_id: 1 }); }`,
          errors: [{ messageId: "missingPerPage", data: { method: "actions.listWorkflowRunArtifacts" } }],
        },
        {
          code: `async function f() { await githubClient.rest.pulls.listReviews({ owner, repo, pull_number: 1 }); }`,
          errors: [{ messageId: "missingPerPage" }],
        },
        {
          code: `async function f() { await github.rest.issues.listComments({ owner, repo, issue_number: 1, per_page: undefined }); }`,
          errors: [{ messageId: "missingPerPage" }],
        },
        {
          code: `async function f() { await github.rest.issues.listComments({ owner, repo, issue_number: 1, per_page: void 0 }); }`,
          errors: [{ messageId: "missingPerPage" }],
        },
      ],
    });
  });
});
