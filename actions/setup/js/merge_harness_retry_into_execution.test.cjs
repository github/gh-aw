"use strict";

import { afterEach, beforeEach, describe, expect, it } from "vitest";
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { mergeHarnessRetryIntoExecution } = require("./merge_harness_retry_into_execution.cjs");

describe("merge_harness_retry_into_execution.cjs", () => {
  let directory;

  beforeEach(() => {
    directory = fs.mkdtempSync(path.join(os.tmpdir(), "detection-retry-"));
  });

  afterEach(() => {
    fs.rmSync(directory, { recursive: true, force: true });
  });

  it("adds sanitized retry metadata to existing detection execution evidence", () => {
    const retryPath = path.join(directory, "detection_harness_retry.json");
    const executionPath = path.join(directory, "execution.json");
    const execution = { version: 1, component: "detection", run_id: 123, run_attempt: 2, state: "started" };
    fs.writeFileSync(executionPath, JSON.stringify(execution));
    fs.writeFileSync(
      retryPath,
      JSON.stringify({
        attempts: 1,
        exit_code: 1,
        attempt_results: [{ attempt: 1, exit_code: 1, failure_classes: ["rate_limit_error"], output: "PRIVATE_OUTPUT" }],
        private: "PRIVATE_FIELD",
      })
    );

    expect(mergeHarnessRetryIntoExecution(retryPath, executionPath)).toBe(true);
    expect(JSON.parse(fs.readFileSync(executionPath, "utf8"))).toEqual({
      ...execution,
      harness_retry: {
        attempts: 1,
        exitCode: 1,
        attemptResults: [{ attempt: 1, exitCode: 1, failureClasses: ["rate_limit_error"] }],
      },
    });
    expect(fs.readFileSync(executionPath, "utf8")).not.toContain("PRIVATE_");
  });

  it("leaves execution evidence unchanged when retry telemetry is absent", () => {
    const executionPath = path.join(directory, "execution.json");
    const execution = { version: 1, component: "detection", run_id: 123, run_attempt: 2, state: "started" };
    fs.writeFileSync(executionPath, JSON.stringify(execution));

    expect(mergeHarnessRetryIntoExecution(path.join(directory, "missing.json"), executionPath)).toBe(false);
    expect(JSON.parse(fs.readFileSync(executionPath, "utf8"))).toEqual(execution);
  });
});
