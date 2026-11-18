"use strict";

const fs = require("node:fs");
const path = require("node:path");

const ghAwDir = "/tmp/gh-aw";
const endpoint = process.env.OTEL_EXPORTER_OTLP_ENDPOINT;
const [conclusion, mode] = process.argv.slice(2);

if (!["success", "failure"].includes(conclusion) || !["compat", "latest"].includes(mode) || !endpoint) {
  throw new Error("Usage: node agent-run.cjs <success|failure> <compat|latest>");
}

const { sendJobConclusionSpan, sendJobSetupSpan } = require("../../send_otlp_span.cjs");

async function main() {
  fs.rmSync(ghAwDir, { recursive: true, force: true });
  fs.mkdirSync(ghAwDir, { recursive: true });
  const startMs = Date.now() - 1000;
  fs.writeFileSync(path.join(ghAwDir, "aw_info.json"), JSON.stringify({ engine_id: "copilot", workflow_name: "OpenTelemetry GenAI Conformance", model: "gpt-4o-mini", cli_version: "test" }));
  fs.writeFileSync(path.join(ghAwDir, "agent_usage.json"), JSON.stringify({ input_tokens: 100, output_tokens: 20, cache_read_tokens: 25, cache_write_tokens: 10, input_tokens_include_cache: false }));
  fs.writeFileSync(path.join(ghAwDir, "agent_output.json"), JSON.stringify({ items: [], errors: [] }));
  fs.writeFileSync(path.join(ghAwDir, "agent_cli_start_ms.txt"), String(startMs));

  process.env.GH_AW_AGENT_CONCLUSION = conclusion;
  process.env.GH_AW_OTEL_METRICS = "true";
  process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: endpoint }]);
  process.env.GITHUB_ACTOR = "conformance";
  process.env.GITHUB_EVENT_NAME = "workflow_dispatch";
  process.env.GITHUB_JOB = "agent";
  process.env.GITHUB_REF = "refs/heads/main";
  process.env.GITHUB_REF_NAME = "main";
  process.env.GITHUB_REPOSITORY = "github/gh-aw";
  process.env.GITHUB_RUN_ATTEMPT = "1";
  process.env.GITHUB_RUN_ID = "123456789";
  process.env.GITHUB_SHA = "0123456789abcdef0123456789abcdef01234567";
  process.env.GITHUB_WORKFLOW = "OpenTelemetry GenAI Conformance";
  process.env.GH_AW_INFO_WORKFLOW_NAME = "OpenTelemetry GenAI Conformance";
  process.env.INPUT_JOB_NAME = "agent";
  process.env.OTEL_SERVICE_NAME = "gh-aw";
  if (mode === "latest") process.env.OTEL_SEMCONV_STABILITY_OPT_IN = "gen_ai_latest_experimental";
  else delete process.env.OTEL_SEMCONV_STABILITY_OPT_IN;

  try {
    const setup = await sendJobSetupSpan({ startMs });
    process.env.GITHUB_AW_OTEL_TRACE_ID = setup.traceId;
    process.env.GITHUB_AW_OTEL_PARENT_SPAN_ID = setup.spanId;
    await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs });
  } finally {
    fs.rmSync(ghAwDir, { recursive: true, force: true });
  }
}

main().catch(error => {
  console.error(error);
  process.exitCode = 1;
});
