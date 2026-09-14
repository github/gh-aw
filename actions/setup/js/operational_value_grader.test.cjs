// @ts-check

const { executeOperationalValueEvaluator, buildRunSubject, safeFunctionEnv, OPERATIONAL_VALUE_EVALUATOR_TEMP_ROOT } = require("./operational_value_grader.cjs");

const TEST_ENV = {
  PATH: process.env.PATH,
  HOME: process.env.HOME,
  TMPDIR: process.env.TMPDIR,
  GITHUB_RUN_ID: "12345",
  GITHUB_RUN_ATTEMPT: "2",
  GITHUB_REPOSITORY: "github/gh-aw",
  GITHUB_WORKFLOW: "Example",
  GITHUB_REF: "refs/heads/main",
  GITHUB_SHA: "0123456789abcdef",
  GITHUB_EVENT_NAME: "schedule",
};

/** @type {(output: unknown, expectedOutputs?: any[]) => string} */
const operationalValueEvaluator = (output, expectedOutputs = []) => {
  return `#!/usr/bin/env bash
set -euo pipefail
[[ $# -eq 0 ]]
request=$(cat)
printf '%s' "$request" | jq -e '
  .schemaVersion == 1
  and .run.id == "12345"
  and .run.attempt == 2
  and .run.repository == "github/gh-aw"
  and .run.workflow == "Example"
  and .run.ref == "refs/heads/main"
  and .run.sha == "0123456789abcdef"
  and .run.eventName == "schedule"
  and .event.issue.number == 42
  and .outputs == ${JSON.stringify(expectedOutputs)}
  and .config.mode == "test"
' >/dev/null
cat <<'RESULT'
${JSON.stringify(output)}
RESULT
`;
};

describe("operational_value_grader", () => {
  it("uses the gh-aw agent temp root and forwards the GitHub GraphQL URL", () => {
    expect(OPERATIONAL_VALUE_EVALUATOR_TEMP_ROOT).toBe("/tmp/gh-aw/agent");
    expect(safeFunctionEnv({ GITHUB_GRAPHQL_URL: "https://api.github.com/graphql" })).toEqual({
      GITHUB_GRAPHQL_URL: "https://api.github.com/graphql",
    });
  });

  it("builds a stable workflow-run subject", () => {
    expect(buildRunSubject(TEST_ENV)).toEqual({
      id: "12345",
      attempt: 2,
      repository: "github/gh-aw",
      workflow: "Example",
      ref: "refs/heads/main",
      sha: "0123456789abcdef",
      eventName: "schedule",
    });
  });

  it("runs once without mode arguments and returns ordered metrics", () => {
    const output = executeOperationalValueEvaluator(
      operationalValueEvaluator(
        [
          { id: "goal-attained", value: 0.75 },
          { id: "evidence-available", value: null },
        ],
        [{ type: "noop", message: "nothing to do" }]
      ),
      { digest: "abc", config: { mode: "test" } },
      { env: TEST_ENV, event: { issue: { number: 42 } }, outputs: [{ type: "noop", message: "nothing to do" }] }
    );

    expect(output).toEqual([
      { id: "goal-attained", value: 0.75 },
      { id: "evidence-available", value: null },
    ]);
  });

  it.each([
    ["empty array", [], "non-empty array"],
    ["extra fields", [{ id: "score", value: 1, message: "no" }], "exactly id and value"],
    [
      "duplicate ids",
      [
        { id: "score", value: 1 },
        { id: "score", value: 0 },
      ],
      "non-empty and unique",
    ],
    ["empty id", [{ id: " ", value: 1 }], "non-empty and unique"],
    ["out-of-range value", [{ id: "score", value: 2 }], "finite numbers in [0,1]"],
    ["string value", [{ id: "score", value: "1" }], "finite numbers in [0,1]"],
  ])("rejects %s", (_name, output, message) => {
    expect(() => executeOperationalValueEvaluator(operationalValueEvaluator(output), { config: { mode: "test" } }, { env: TEST_ENV, event: { issue: { number: 42 } } })).toThrow(message);
  });

  it("rejects invalid Bash", () => {
    expect(() => executeOperationalValueEvaluator("#!/usr/bin/env bash\nif", {}, { env: TEST_ENV })).toThrow("invalid Bash syntax");
  });

  it("supports a configurable evaluator timeout", () => {
    const slowEvaluator = `#!/usr/bin/env bash
set -euo pipefail
cat >/dev/null
sleep 1
printf '%s\n' '[{"id":"score","value":1}]'
`;

    expect(() =>
      executeOperationalValueEvaluator(
        slowEvaluator,
        {},
        {
          env: { ...TEST_ENV, GH_AW_OPERATIONAL_VALUE_EVALUATOR_TIMEOUT_MS: "50" },
        }
      )
    ).toThrow("operational-value evaluator timed out after 50ms");
  });
});
