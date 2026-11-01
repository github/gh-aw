import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "fs";

const OUTPUT_DIR = "/tmp/gh-aw";
const OUTPUT_PATH = "/tmp/gh-aw/evals.jsonl";
const STATUS_PATH = "/tmp/gh-aw/evals_status.json";
const EVALS_DIR = "/tmp/gh-aw/evals";
const EXECUTION_PATH = `${EVALS_DIR}/execution.json`;
const LOG_PATH = `${EVALS_DIR}/evals.log`;
const FIXTURES_DIR = new URL("./fixtures/evals/", import.meta.url);
const RUN_FIXTURES = JSON.parse(fs.readFileSync(new URL("runs.json", FIXTURES_DIR), "utf8"));

const mockCore = {
  info: vi.fn(),
  warning: vi.fn(),
  setFailed: vi.fn(),
  summary: {
    addRaw: vi.fn().mockReturnThis(),
    write: vi.fn().mockResolvedValue(),
  },
};

global.core = mockCore;

describe("render_evals_summary.cjs", () => {
  let module;

  beforeEach(async () => {
    vi.clearAllMocks();
    fs.mkdirSync(OUTPUT_DIR, { recursive: true });
    if (fs.existsSync(OUTPUT_PATH)) {
      fs.unlinkSync(OUTPUT_PATH);
    }
    fs.rmSync(STATUS_PATH, { force: true });
    module = await import("./render_evals_summary.cjs");
  });

  afterEach(() => {
    vi.unstubAllEnvs();
    if (fs.existsSync(OUTPUT_PATH)) {
      fs.unlinkSync(OUTPUT_PATH);
    }
    fs.rmSync(STATUS_PATH, { force: true });
    fs.rmSync(EXECUTION_PATH, { force: true });
    fs.rmSync(LOG_PATH, { force: true });
  });

  describe("readEvalsResults", () => {
    it("returns an empty array when the output file does not exist", () => {
      expect(module.readEvalsResults()).toEqual([]);
    });

    it("parses JSONL, skips malformed lines, and normalizes answers", () => {
      fs.writeFileSync(
        OUTPUT_PATH,
        [
          JSON.stringify({ id: "q1", question: "First?", answer: " yes ", model: "claude-sonnet-4.6", timestamp: "2026-07-15T00:00:00Z" }),
          "not-json",
          JSON.stringify({ id: "q2", question: "Second?", answer: "No", model: "claude-sonnet-4.6" }),
          JSON.stringify({ id: "q3", question: "Third?" }),
        ].join("\n"),
        "utf8"
      );

      expect(module.readEvalsResults()).toEqual([
        {
          id: "q1",
          question: "First?",
          answer: "YES",
          model: "claude-sonnet-4.6",
          timestamp: "2026-07-15T00:00:00Z",
        },
        {
          id: "q2",
          question: "Second?",
          answer: "NO",
          model: "claude-sonnet-4.6",
          timestamp: "",
        },
        {
          id: "q3",
          question: "Third?",
          answer: "UNKNOWN",
          model: "",
          timestamp: "",
        },
      ]);
    });
  });

  describe("buildEvalsBody", () => {
    it("renders tallies, escapes table cells, and includes the model", () => {
      const markdown = module.buildEvalsBody([
        {
          id: "id|`1`\r",
          question: "Line 1\nLine 2 | `code`",
          answer: "YES",
          model: "claude`-4.6`",
          timestamp: "",
        },
        {
          id: "id2",
          question: "Question 2",
          answer: "NO",
          model: "claude`-4.6`",
          timestamp: "",
        },
        {
          id: "id3",
          question: "Question 3",
          answer: "UNKNOWN",
          model: "claude`-4.6`",
          timestamp: "",
        },
      ]);

      expect(markdown).toContain("| id\\|\\`1\\`  | Line 1 Line 2 \\| \\`code\\` | ✅ YES |");
      expect(markdown).toContain("| id2 | Question 2 | ❌ NO |");
      expect(markdown).toContain("| id3 | Question 3 | ❓ UNKNOWN |");
      expect(markdown).toContain("**YES**: 1 | **NO**: 1 | **UNKNOWN**: 1");
      expect(markdown).toContain("**model**: claude\\`-4.6\\`");
    });

    it("returns an empty string for empty results", () => {
      expect(module.buildEvalsBody([])).toBe("");
    });
  });

  describe("main", () => {
    it("logs and returns early when the output file is empty or malformed", async () => {
      fs.writeFileSync(OUTPUT_PATH, "not-json\n", "utf8");

      await module.main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("No evals results found"));
      expect(mockCore.summary.addRaw).not.toHaveBeenCalled();
      expect(mockCore.summary.write).not.toHaveBeenCalled();
    });

    it("writes the evals details section to the step summary", async () => {
      fs.writeFileSync(
        OUTPUT_PATH,
        [JSON.stringify({ id: "builds", question: "Does it build?", answer: "YES", model: "claude-sonnet-4.6" }), JSON.stringify({ id: "tests", question: "Do tests pass?", answer: "NO", model: "claude-sonnet-4.6" })].join("\n") + "\n",
        "utf8"
      );

      await module.main();

      expect(mockCore.summary.addRaw).toHaveBeenCalledTimes(1);
      const summary = mockCore.summary.addRaw.mock.calls[0][0];
      expect(summary).toContain("<details>");
      expect(summary).toContain("<summary>BinEval Results</summary>");
      expect(summary).toContain("✅ YES");
      expect(summary).toContain("❌ NO");
      expect(summary).toContain("**model**: claude-sonnet-4.6");
      expect(mockCore.summary.write).toHaveBeenCalledTimes(1);
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("2 result(s)"));
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("written to step summary"));
    });
  });
  describe("not-evaluated marker", () => {
    /**
     * Produces the marker exactly as the parse step does for an audited run.
     * @param {string} runID
     */
    async function parseFailedRun(runID) {
      const run = RUN_FIXTURES[runID];
      const { parseMain } = await import("./run_evals.cjs");
      vi.stubEnv("GH_AW_EVALS_QUESTIONS", JSON.stringify(run.questions));
      vi.stubEnv("GH_AW_EVALS_MODEL", run.model);
      vi.stubEnv("GH_AW_EVALS_ENGINE", run.engine);
      vi.stubEnv("GH_AW_EVALS_EXECUTION_OUTCOME", "failure");
      vi.stubEnv("GITHUB_RUN_ID", runID);
      vi.stubEnv("GITHUB_RUN_ATTEMPT", run.run_attempt);
      fs.mkdirSync(EVALS_DIR, { recursive: true });
      fs.writeFileSync(LOG_PATH, fs.readFileSync(new URL(run.log, FIXTURES_DIR), "utf8"));
      fs.writeFileSync(EXECUTION_PATH, JSON.stringify(run.execution) + "\n");
      await parseMain();
    }

    it("renderEvalsSummary_explains_40955414354_not_evaluated", async () => {
      await parseFailedRun("40955414354");
      expect(fs.existsSync(OUTPUT_PATH)).toBe(false);

      // Simulate a secret reaching the marker; the redaction step must scrub it
      // before the summary is published.
      const marker = JSON.parse(fs.readFileSync(STATUS_PATH, "utf8"));
      fs.writeFileSync(STATUS_PATH, JSON.stringify({ ...marker, reason: `${marker.reason} super-secret-value` }) + "\n");
      vi.stubEnv("GH_AW_SECRET_NAMES", "EVALS_SECRET");
      vi.stubEnv("SECRET_EVALS_SECRET", "super-secret-value");
      for (const variable of ["OTEL_EXPORTER_OTLP_HEADERS", "GH_AW_OTLP_ALL_HEADERS", "GH_AW_OTLP_ENDPOINTS"]) vi.stubEnv(variable, "");
      const { main: redact } = await import("./redact_evals_results.cjs");
      await redact();
      expect(mockCore.setFailed).not.toHaveBeenCalled();

      expect(module.readEvalsResults()).toEqual([]);
      await module.main();

      expect(mockCore.summary.addRaw).toHaveBeenCalledTimes(1);
      const summary = mockCore.summary.addRaw.mock.calls[0][0];
      expect(summary).toContain("<summary>BinEval Results</summary>");
      expect(summary).toContain("**Evals did not run.**");
      expect(summary).toContain("**Reason** (judge_not_started): The claude evals judge did not start");
      expect(summary).toContain("**engine**: claude | **model**: evals");
      expect(summary).not.toContain("super-secret-value");
      expect(summary).not.toContain("❓ UNKNOWN");
      expect(summary).not.toContain("| ID | Question | Answer |");
      expect(summary).not.toContain("**UNKNOWN**:");
    });

    it("explains a judge that ran but did not complete", () => {
      const body = module.buildEvalsBody([], { status: "not_evaluated", reason_code: "no_assistant_response", reason: "No <b>response</b> | text", engine: "copilot" });

      expect(body).toContain("**Evals were not completed.**");
      expect(body).toContain("No &lt;b&gt;response&lt;/b&gt; \\| text");
      expect(body).not.toContain("❓ UNKNOWN");
    });

    it("still renders a genuine judge UNKNOWN answer as a normal answer table", async () => {
      fs.writeFileSync(OUTPUT_PATH, JSON.stringify({ ...RUN_FIXTURES["40955414354"].questions[0], answer: "UNKNOWN", model: "evals", runid: "40955414354" }) + "\n");

      await module.main();

      const summary = mockCore.summary.addRaw.mock.calls[0][0];
      expect(summary).toContain("| docs_reviewed |");
      expect(summary).toContain("❓ UNKNOWN");
      expect(summary).toContain("**YES**: 0 | **NO**: 0 | **UNKNOWN**: 1");
      expect(summary).not.toContain("Evals did not run");
    });
  });
});
