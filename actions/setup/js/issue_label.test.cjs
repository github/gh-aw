// @ts-check
import { afterEach, describe, expect, it, vi } from "vitest";

const { getIssueLabel, getIssueLabelQuery, getRunIssueLabels } = require("./issue_label.cjs");

afterEach(() => vi.unstubAllEnvs());

describe("issue label", () => {
  it.each([undefined, ""])("defaults an unset or empty label (%s)", value => {
    vi.stubEnv("GH_AW_ISSUE_LABEL", value);
    expect(getIssueLabel()).toBe("agentic-workflows");
    expect(getIssueLabelQuery()).toBe('label:"agentic-workflows"');
  });

  it.each(["area:agentic-workflows", "area:agentic workflows"])("uses a custom label (%s) as one search value", label => {
    vi.stubEnv("GH_AW_ISSUE_LABEL", label);
    expect(getIssueLabel()).toBe(label);
    expect(getIssueLabelQuery()).toBe(`label:"${label}"`);
  });

  it("escapes quotes and backslashes in search values", () => {
    vi.stubEnv("GH_AW_ISSUE_LABEL", 'area:"workflow\\issues"');
    expect(getIssueLabelQuery()).toBe('label:"area:\\"workflow\\\\issues\\""');
  });

  it("adds the gh-aw backlog label only to run reports targeting this repository", () => {
    vi.stubEnv("GH_AW_ISSUE_LABEL", "custom-run-label");
    expect(getRunIssueLabels("GitHub", "gh-aw")).toEqual(["custom-run-label", "backlog:run-alerts"]);
    expect(getRunIssueLabels("another-owner", "gh-aw")).toEqual(["custom-run-label"]);
  });
});
