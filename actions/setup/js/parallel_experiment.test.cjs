import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import path from "node:path";
import { randomUUID } from "node:crypto";
import { createRequire } from "node:module";
import { main, collectUsage } from "./parallel_experiment.cjs";
import { readExperimentAssignments } from "./experiment_helpers.cjs";
import { buildExperimentAttributes, sendJobConclusionSpan } from "./send_otlp_span.cjs";
createRequire(import.meta.url)("./otlp_metrics.cjs");

describe("parallel experiment collection", () => {
  let root;
  let env;
  let clock;
  const write = (name, value) => {
    const file = path.join(root, name);
    fs.mkdirSync(path.dirname(file), { recursive: true });
    fs.writeFileSync(file, typeof value === "string" ? value : JSON.stringify(value));
  };
  const run = mode => main(mode, { rootDir: root, outputDir: path.join(root, "parallel"), env, now: () => new Date(clock) });
  const steps = (outcome, conclusion = outcome) => {
    env.GH_AW_PARALLEL_STEPS = JSON.stringify({ execute_agent: { outcome, conclusion, outputs: { token: "private-step-output" } } });
  };
  beforeEach(() => {
    root = path.resolve(".cache", `parallel-experiment-${randomUUID()}`);
    fs.mkdirSync(root, { recursive: true });
    env = {
      GH_AW_PARALLEL_EXPERIMENT: JSON.stringify({
        id: "style-concise",
        experiment: "style",
        variant: "concise",
        engine: "copilot",
        model: "configured-model",
        harness: "copilot-cli",
        max_ai_credits: 100,
        timeout_minutes: 10,
        secret: "DO_NOT_PUBLISH",
      }),
      GH_AW_EXPERIMENT_STATE_DIR: path.join(root, "experiments"),
      GITHUB_RUN_ID: "100",
      GITHUB_RUN_ATTEMPT: "2",
      GITHUB_JOB: "parallel_experiments",
      GITHUB_ENV: path.join(root, "github-env"),
    };
    clock = "2026-11-28T10:00:00.000Z";
  });

  describe("parallel experiment OTLP export", () => {
    afterEach(() => {
      vi.unstubAllEnvs();
      vi.unstubAllGlobals();
      vi.restoreAllMocks();
    });

    it("exports agent usage and metrics with local variant attribution and actual matrix job identity", async () => {
      for (const [key, value] of Object.entries({
        GH_AW_OTLP_ENDPOINTS: JSON.stringify([{ url: "https://otel.example.test" }]),
        GH_AW_OTEL_METRICS: "true",
        INPUT_JOB_NAME: "agent",
        GITHUB_JOB: "experiments",
        GITHUB_RUN_ID: "123",
        GITHUB_RUN_ATTEMPT: "2",
        GITHUB_REPOSITORY: "owner/repo",
        GH_AW_ENGINE_ID: "copilot",
        GH_AW_INFO_WORKFLOW_NAME: "Parallel workflow",
        GH_AW_AGENT_CONCLUSION: "failure",
        GH_AW_AIC: "1.5",
        GH_AW_PARALLEL_EXPERIMENT: JSON.stringify({ id: "local", experiment: "style", variant: "concise" }),
        OTEL_SEMCONV_STABILITY_OPT_IN: "gen_ai_v1",
      }))
        vi.stubEnv(key, value);
      const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
      const readFile = fs.readFileSync.bind(fs);
      vi.stubGlobal("fetch", fetchMock);
      vi.spyOn(fs, "existsSync").mockReturnValue(false);
      vi.spyOn(fs, "mkdirSync").mockImplementation(() => {});
      vi.spyOn(fs, "appendFileSync").mockImplementation(() => {});
      vi.spyOn(fs, "chmodSync").mockImplementation(() => {});
      vi.spyOn(fs, "fchmodSync").mockImplementation(() => {});
      vi.spyOn(fs, "openSync").mockReturnValue(-100);
      vi.spyOn(fs, "writeSync").mockImplementation((_fd, content) => String(content).length);
      vi.spyOn(fs, "closeSync").mockImplementation(() => {});
      vi.spyOn(fs, "statSync").mockImplementation(() => ({ mtimeMs: 1700000005000 }));
      vi.spyOn(fs, "readFileSync").mockImplementation(file => {
        if (String(file).endsWith(".cjs")) return readFile(file, "utf8");
        if (String(file).endsWith("/aw_info.json")) return JSON.stringify({ engine_id: "copilot", model: "gpt-4o", workflow_name: "Parallel workflow" });
        if (String(file).endsWith("/agent_usage.json")) return JSON.stringify({ input_tokens: 10, output_tokens: 3, cache_read_tokens: 0, cache_write_tokens: 2, input_tokens_inclusive: 12, ai_credits: 1.5 });
        if (String(file).endsWith("/assignments.json")) return '{"style":"production"}';
        if (String(file).endsWith("/agent_output.json")) return '{"items":[],"errors":[]}';
        throw Object.assign(new Error("missing"), { code: "ENOENT" });
      });
      await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs: 1700000000000 });
      const payloads = fetchMock.mock.calls.map(([, options]) => JSON.parse(options.body));
      const spans = payloads.flatMap(payload => payload.resourceSpans?.flatMap(resource => resource.scopeSpans.flatMap(scope => scope.spans)) ?? []);
      const agent = spans.find(span => span.name.startsWith("invoke_agent"));
      expect(agent).toBeDefined();
      const attrs = Object.fromEntries(agent.attributes.map(attr => [attr.key, attr.value.stringValue ?? attr.value.intValue ?? attr.value.doubleValue]));
      expect(attrs).toMatchObject({
        "gh-aw.experiment.style": "concise",
        "gh-aw.parallel_experiment.id": "local",
        "gen_ai.usage.input_tokens": 12,
        "gen_ai.usage.output_tokens": 3,
        "gen_ai.usage.cache_read.input_tokens": 0,
        "gh-aw.aic": 1.5,
      });
      expect(agent.status.code).toBe(2);
      const resource = payloads.find(payload => payload.resourceSpans)?.resourceSpans[0].resource.attributes;
      expect(resource).toContainEqual({ key: "cicd.pipeline.task.name", value: { stringValue: "experiments" } });
      const metrics = payloads.flatMap(payload => payload.resourceMetrics?.flatMap(resource => resource.scopeMetrics.flatMap(scope => scope.metrics)) ?? []);
      expect(payloads.find(payload => payload.resourceMetrics)?.resourceMetrics[0].resource.attributes).toContainEqual({ key: "gh-aw.parallel_experiment.variant", value: { stringValue: "concise" } });
      expect(payloads.find(payload => payload.resourceMetrics)?.resourceMetrics[0].resource.attributes).toContainEqual({ key: "gh-aw.parallel_experiment.id", value: { stringValue: "local" } });
      expect(metrics.some(metric => metric.name === "gh_aw.job.duration")).toBe(true);
      expect(metrics.some(metric => metric.name.includes("token"))).toBe(true);
    });
  });
  afterEach(() => {
    vi.unstubAllEnvs();
    vi.restoreAllMocks();
    fs.rmSync(root, { recursive: true, force: true });
  });

  it("overrides production assignments for prompts and existing OTLP instrumentation", () => {
    write("experiments/assignments.json", { style: "verbose", other: "on" });
    const prepared = run("prepare");
    expect(prepared.assignments).toEqual({ style: "concise", other: "on" });
    expect(readExperimentAssignments(env)).toEqual(prepared.assignments);
    expect(buildExperimentAttributes(readExperimentAssignments(env))).toContainEqual({ key: "gh-aw.experiment.style", value: { stringValue: "concise" } });
    expect(fs.readFileSync(env.GITHUB_ENV, "utf8")).toBe("GH_AW_EXPERIMENTS_STYLE=concise\n");
    expect(JSON.stringify(prepared)).not.toContain("DO_NOT_PUBLISH");
    write("experiments/assignments.json", { style: "verbose" });
    expect(readExperimentAssignments(env)).toEqual({ style: "concise" });
  });

  it("records actual execution boundaries, never prepare/finalize or workflow clocks", () => {
    run("prepare");
    clock = "2026-11-28T10:03:00.000Z";
    run("start");
    clock = "2026-11-28T10:04:00.000Z";
    run("end");
    clock = "2026-11-28T10:09:00.000Z";
    steps("success");
    const result = run("finalize");
    expect(result).toMatchObject({ state: "completed", admitted: true, executed: true, failed: false, completed: true, execution_started_at: "2026-11-28T10:03:00.000Z", execution_ended_at: "2026-11-28T10:04:00.000Z" });
    expect(result.identity).toEqual({ run_id: "100", run_attempt: "2", job: "parallel_experiments", matrix_id: "style-concise" });
    expect(JSON.stringify(result.outcomes)).not.toContain("private-step-output");
  });

  it("retains failed job partial usage and nullable missing counters", () => {
    run("prepare");
    run("start");
    write("agent_usage.json", { ai_credits: 2.5, input_tokens: 100, cache_read_tokens: 0 });
    steps("failure", "success");
    const result = run("finalize");
    expect(result).toMatchObject({ state: "failed", failed: true, completed: false, executed: true, execution_ended_at: null });
    expect(result.usage).toMatchObject({ aic: 2.5, input_tokens: 100, output_tokens: null, cache_read_tokens: 0, cache_write_tokens: null });
    expect(result.missing_evidence).toEqual(expect.arrayContaining(["output_tokens", "cache_write_tokens", "execution_end"]));
    expect(result.missing_evidence).not.toContain("cache_read_tokens");
  });

  it.each([undefined, {}, { ai_credits: 0, input_tokens: 0, output_tokens: 0, cache_read_tokens: 0, cache_write_tokens: 0 }])("distinguishes missing usage from explicit zero: %j", usage => {
    if (usage) write("agent_usage.json", usage);
    const result = collectUsage(root);
    for (const field of ["aic", "input_tokens", "output_tokens", "cache_read_tokens", "cache_write_tokens"]) expect(result[field]).toBe(usage?.ai_credits === 0 ? 0 : null);
  });

  it("reuses AWF accounting once rather than summing mirrored logs or aggregate totals", () => {
    const entry = { event: "token_usage", request_id: "one", provider: "openai", model: "gpt-4o", input_tokens: 20, output_tokens: 5, cache_read_tokens: 0, cache_write_tokens: 0, ai_credits_this_response: 0.75, ai_credits_total: 0.75 };
    const raw = JSON.stringify(entry) + "\n";
    write("sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl", raw + raw);
    write("sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl", raw);
    write("agent_usage.json", { ai_credits: 999 });
    expect(collectUsage(root)).toMatchObject({ aic: 0.75, input_tokens: 20, output_tokens: 5, cache_read_tokens: 0, cache_write_tokens: 0, aic_source: "awf_reported" });
  });

  it("does not turn empty proxy evidence into zero and uses available partial fallback", () => {
    write("sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl", "");
    expect(collectUsage(root)).toMatchObject({ aic: null, input_tokens: null });
    write("agent_usage.json", { ai_credits: 999 });
    expect(collectUsage(root)).toMatchObject({ aic: 999, input_tokens: null });
  });

  it("does not price absent proxy counters as zero", () => {
    write("sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl", JSON.stringify({ event: "token_usage", request_id: "partial", provider: "openai", model: "gpt-4o" }) + "\n");
    expect(collectUsage(root)).toMatchObject({ aic: null, input_tokens: null, output_tokens: null, cache_read_tokens: null, cache_write_tokens: null });
  });

  it("publishes an independently redacted session and staged output, never graders", () => {
    vi.stubEnv("GH_AW_SECRET_NAMES", "EXAMPLE");
    vi.stubEnv("SECRET_EXAMPLE", "synthetic-private-secret");
    vi.stubEnv("GH_AW_DYNAMIC_SECRET_VALUE_1", "dynamic-private-secret");
    write("agent-stdio.log", "::add-mask::masked-body-value\n");
    write("agent_output.json", { items: [{ type: "create_issue", title: "preserve title", body: "synthetic-private-secret masked-body-value dynamic-private-secret" }] });
    write("agent-session.jsonl", '{"type":"assistant.message","data":{"content":"synthetic-private-secret masked-body-value dynamic-private-secret"}}\n');
    run("prepare");
    run("start");
    run("end");
    steps("success");
    const result = run("finalize");
    expect(result.session).toBe("aw_session.jsonl");
    const session = fs.readFileSync(path.join(root, "parallel", result.session), "utf8");
    const staged = fs.readFileSync(path.join(root, "parallel", result.staged_safe_outputs), "utf8");
    expect(staged).toContain("preserve title");
    for (const content of [session, staged]) {
      expect(content).not.toContain("synthetic-private-secret");
      expect(content).not.toContain("masked-body-value");
      expect(content).not.toContain("dynamic-private-secret");
    }
    expect(session).toContain("assistant.message");
    expect(fs.existsSync(path.join(root, "parallel/graders"))).toBe(false);
  });

  it("bundles logs, safe configuration, and telemetry with decoded secret redaction", () => {
    vi.stubEnv("GH_AW_SECRET_NAMES", "EXAMPLE");
    vi.stubEnv("SECRET_EXAMPLE", "synthetic-private-secret");
    vi.stubEnv("GH_AW_DYNAMIC_SECRET_VALUE_1", "dynamic-private-secret");
    write("aw_info.json", { engine_id: "old-engine", model: "old-model", engine_harness: "old-harness" });
    write("awf-config.json", { network: { allowDomains: ["example.test"] }, env: { API_KEY: "not-in-secret-registry" }, token: "sensitive" });
    write("awf-resolved-config.json", { sandbox: { mode: "container" }, network: { allowDomains: ["example.test"] }, headers: { Authorization: "not-in-secret-registry" } });
    write("agent-stdio.log", "::add-mask::masked-value\nexecution log synthetic-private-secret masked-value dynamic-private-secret\n");
    write("otel.jsonl", '{"resourceSpans":[],"value":"synthetic\\u002dprivate-secret"}\n');
    write("otel-metrics.jsonl", '{"resourceMetrics":[],"value":"dynamic-private-secret"}\n');
    write("otlp-export-errors.count", "1\n");
    write("agent_usage.json", { input_tokens: 7, cache_read_tokens: 0 });
    write("aw-prompts/user.txt", "Compare candidate prompt synthetic-private-secret");
    write("aw-prompts/system.txt", "System instructions");
    write("sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl", '{"input_tokens":7,"output_tokens":0}\n');
    run("prepare");
    const localInfo = JSON.parse(fs.readFileSync(path.join(root, "aw_info.json"), "utf8"));
    expect(localInfo).toMatchObject({ engine_id: "copilot", model: "configured-model", engine_harness: "copilot-cli", parallel_experiment: { variant: "concise" } });
    const result = run("finalize");
    expect(result.configuration_evidence).toMatchObject({ configured: "evidence/awf-config.json", resolved: "evidence/awf-resolved-config.json" });
    expect(result.evidence["otlp-export-errors.count"]).toBe("evidence/otlp-export-errors.count.txt");
    expect(result.evidence["agent/model-resolution.json"]).toBeNull();
    const bundled = Object.values(result.evidence)
      .filter(Boolean)
      .map(relative => fs.readFileSync(path.join(root, "parallel", relative), "utf8"))
      .join("\n");
    for (const secret of ["synthetic-private-secret", "synthetic\\u002dprivate-secret", "dynamic-private-secret", "not-in-secret-registry", "masked-value"]) expect(bundled).not.toContain(secret);
    expect(bundled).toContain("execution log");
    expect(bundled).toContain("example.test");
    expect(bundled).toContain("Compare candidate prompt");
    expect(result.evidence["sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl"]).toBe("evidence/sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl");
    expect(JSON.parse(fs.readFileSync(path.join(root, "parallel/evidence/agent_usage.json"), "utf8"))).toEqual({ input_tokens: 7, cache_read_tokens: 0 });
    fs.unlinkSync(path.join(root, "aw-prompts/user.txt"));
    expect(run("finalize").evidence["aw-prompts/user.txt"]).toBeNull();
    expect(fs.existsSync(path.join(root, "parallel/evidence/aw-prompts/user.txt"))).toBe(false);
  });

  it("retains raw staged outputs after a failed collector without claiming validation", () => {
    write("safeoutputs/outputs.ndjson", '{"type":"create_issue","title":"saved"}\nnot-json\n');
    run("prepare");
    steps("failure");
    const result = run("finalize");
    const staged = JSON.parse(fs.readFileSync(path.join(root, "parallel", result.staged_safe_outputs), "utf8"));
    expect(staged).toMatchObject({ items: [{ type: "create_issue", title: "saved" }], validated: false, malformed_records: 1 });
    expect(result.missing_evidence).toContain("partial_staged_safe_outputs");
    expect(result.evidence["safeoutputs/outputs.ndjson"]).toBe("evidence/safeoutputs/outputs.ndjson.jsonl");
  });

  it("records actual model resolution and does not treat configuration as resolution", () => {
    run("prepare");
    expect(run("finalize").resolved).toEqual({ engine: null, model: null, harness: null });
    write("runtime/aw_info.json", { engine_id: "copilot", model: "resolved-model", engine_harness: "copilot-cli" });
    expect(run("finalize").resolved).toEqual({ engine: "copilot", model: null, harness: "copilot-cli" });
    expect(run("finalize").missing_evidence).toContain("resolved_model");
    write("agent_usage.json", { primary_model: "observed-model" });
    expect(run("finalize").resolved.model).toBe("observed-model");
    write("agent/model-resolution.json", [{ engine: "copilot", role: "main", configured_model: "alias", resolved_model: "resolved-alias", provider: "github", status: "resolved", cause: null }]);
    expect(run("finalize").resolved.model).toBe("resolved-alias");
    write("agent/model-resolution.json", [{ engine: "copilot", role: "main", configured_model: "alias", resolved_model: null, provider: null, status: "failed", cause: "model_unavailable" }]);
    expect(run("finalize").resolved.model).toBeNull();
  });

  it("selects the current compiler branch, not another skipped or executed branch", () => {
    env.GH_AW_PARALLEL_EXPERIMENT = JSON.stringify({ ...JSON.parse(env.GH_AW_PARALLEL_EXPERIMENT), id: "entry-2" });
    run("prepare");
    env.GH_AW_PARALLEL_STEPS = JSON.stringify({ entry_1_agentic_execution: { outcome: "success" }, entry_2_agentic_execution: { outcome: "skipped" } });
    expect(run("finalize")).toMatchObject({ executed: false, completed: false, skipped: true });
  });

  it("rejects environment-file symlinks before writing", () => {
    write("private-env", "unchanged");
    fs.symlinkSync(path.join(root, "private-env"), env.GITHUB_ENV);
    expect(() => run("prepare")).toThrow("must not follow symlinks");
    expect(fs.readFileSync(path.join(root, "private-env"), "utf8")).toBe("unchanged");
  });

  it("records skipped admission and missing timing without inventing execution", () => {
    env.GH_AW_ADMISSION_RESULT = "skipped";
    run("prepare");
    expect(() => run("start")).toThrow("requires admission");
    steps("skipped");
    const result = run("finalize");
    expect(result).toMatchObject({ state: "skipped", admitted: false, executed: false, completed: false, execution_started_at: null, execution_ended_at: null });
    expect(result.usage.aic).toBeNull();
  });

  it("honors compiler admission and its namespaced execution step IDs", () => {
    env.GH_AW_PARALLEL_EXPERIMENT = JSON.stringify({ ...JSON.parse(env.GH_AW_PARALLEL_EXPERIMENT), admitted: false, skip_reason: "max_parallel" });
    expect(run("prepare")).toMatchObject({ admitted: false, skipped: true });
    env.GH_AW_PARALLEL_STEPS = JSON.stringify({ entry_1_agentic_execution: { outcome: "skipped" } });
    expect(run("finalize")).toMatchObject({ executed: false, state: "skipped", configured: { skip_reason: "max_parallel" } });
  });

  it("preserves the compiler ledger for skipped rows without claiming other jobs executed", () => {
    const selected = { ...JSON.parse(env.GH_AW_PARALLEL_EXPERIMENT), id: "entry-1", admitted: true };
    env.GH_AW_PARALLEL_EXPERIMENT = JSON.stringify(selected);
    env.GH_AW_PARALLEL_ADMISSION = JSON.stringify([selected, { ...selected, id: "entry-2", variant: "verbose", admitted: false, skip_reason: "admission-limit" }]);
    run("prepare");
    const result = run("finalize");
    expect(result.admission).toMatchObject({ configured: 2, admitted: 1, skipped: 1 });
    expect(result.admission.entries[1]).toMatchObject({ id: "entry-2", state: "skipped", skip_reason: "admission-limit", executed: null, completed: null, failed: null });
    expect(result.admission.entries[0]).toMatchObject({ state: "admitted", executed: null });
    expect(result.missing_evidence).not.toContain("admission_ledger");
  });

  it("supports partial collection when prepare was never reached", () => {
    steps("skipped");
    const result = run("finalize");
    expect(result).toMatchObject({ admitted: null, executed: false, state: "skipped" });
    expect(result.session).toBe("aw_session.jsonl");
    expect(result.missing_evidence).toContain("agent_session");
  });

  it("keeps compiler-rejected rows skipped even without prepare or an execution step", () => {
    env.GH_AW_PARALLEL_EXPERIMENT = JSON.stringify({ ...JSON.parse(env.GH_AW_PARALLEL_EXPERIMENT), admitted: false, skip_reason: "budget" });
    expect(run("finalize")).toMatchObject({ admitted: false, executed: false, skipped: true, state: "skipped" });
  });

  it("rejects cross-attempt result reuse and unsafe local assignment values", () => {
    run("prepare");
    env.GITHUB_RUN_ATTEMPT = "3";
    expect(() => run("finalize")).toThrow("identity mismatch");
    env.GH_AW_PARALLEL_EXPERIMENT = JSON.stringify({ id: "id", experiment: "style", variant: "yes\nEVIL=value" });
    expect(() => run("prepare")).toThrow("Invalid parallel experiment");
  });

  it("does not follow evidence or output symlinks", () => {
    write("private.json", { ai_credits: 999 });
    fs.symlinkSync(path.join(root, "private.json"), path.join(root, "agent_usage.json"));
    expect(collectUsage(root).aic).toBeNull();
    fs.mkdirSync(path.join(root, "other"));
    fs.symlinkSync(path.join(root, "other"), path.join(root, "parallel"));
    expect(() => run("prepare")).toThrow("must not follow symlinks");
  });
});
