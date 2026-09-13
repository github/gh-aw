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

function operationalValueEvaluator(output) {
  return `#!/usr/bin/env bash
set -euo pipefail
cat >/dev/null
cat <<'RESULT'
${JSON.stringify(output)}
RESULT
`;
}

describe("operational_value_grader", () => {
  it("uses the first named metric as the primary value", () => {
    const metrics = [
      { id: "assigned-go-file-decomposition", value: 0.75 },
      { id: "repository-health", value: 0.9 },
    ];

    expect(executeOperationalValueEvaluator(operationalValueEvaluator(metrics), { digest: "abc" }, { env: TEST_ENV })).toEqual({ value: 0.75, metrics });
  });

  it("preserves null metric values", () => {
    const metrics = [{ id: "deployment-adoption", value: null }];
    expect(executeOperationalValueEvaluator(operationalValueEvaluator(metrics), {}, { env: TEST_ENV })).toEqual({ value: null, metrics });
  });

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

  it("rejects an empty metric array", () => {
    expect(() => executeOperationalValueEvaluator(operationalValueEvaluator([]), {}, { env: TEST_ENV })).toThrow("non-empty metric array");
  });

  it("rejects duplicate or empty metric ids", () => {
    expect(() => executeOperationalValueEvaluator(operationalValueEvaluator([{ id: "", value: 0.5 }]), {}, { env: TEST_ENV })).toThrow("id must be a non-empty string");
    expect(() =>
      executeOperationalValueEvaluator(
        operationalValueEvaluator([
          { id: "health", value: 0.5 },
          { id: "health", value: 0.8 },
        ]),
        {},
        { env: TEST_ENV }
      )
    ).toThrow("metric id is duplicated: health");
  });

  it("rejects metric values outside [0,1]", () => {
    expect(() => executeOperationalValueEvaluator(operationalValueEvaluator([{ id: "health", value: 2 }]), {}, { env: TEST_ENV })).toThrow("metric health must be null or a finite number in [0,1]");
  });

  it("rejects additional metric fields", () => {
    expect(() => executeOperationalValueEvaluator(operationalValueEvaluator([{ id: "health", value: 1, explanation: "hidden output" }]), {}, { env: TEST_ENV })).toThrow("metrics must contain only id and value");
  });

  it("rejects invalid JSON and invalid Bash", () => {
    expect(() => executeOperationalValueEvaluator("#!/usr/bin/env bash\nprintf 'nope'\n", {}, { env: TEST_ENV })).toThrow("returned invalid JSON");
    expect(() => executeOperationalValueEvaluator("#!/usr/bin/env bash\nif", {}, { env: TEST_ENV })).toThrow("invalid Bash syntax");
  });

  it("supports a configurable timeout", () => {
    const evaluator = `#!/usr/bin/env bash
set -euo pipefail
cat >/dev/null
sleep 1
printf '[{"id":"health","value":1}]\n'
`;

    expect(() => executeOperationalValueEvaluator(evaluator, {}, { env: { ...TEST_ENV, GH_AW_OPERATIONAL_VALUE_GRADE_RUN_TIMEOUT_MS: "50" } })).toThrow("operational-value evaluator timed out after 50ms");
  });
});
