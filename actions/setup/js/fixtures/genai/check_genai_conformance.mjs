import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";

const reportDir = process.argv[2];
if (!reportDir) throw new Error("Usage: node check_genai_conformance.mjs <weaver-report-directory>");

const expectedMetricNames = new Set([
  "gen_ai.client.inference.usage.input_tokens",
  "gen_ai.client.inference.usage.output_tokens",
  "gen_ai.client.inference.usage.cache_read.input_tokens",
  "gen_ai.client.inference.usage.cache_write.input_tokens",
  "gen_ai.invoke_agent.duration",
]);
const observedMetrics = new Set();
const expectedScenarios = ["agent_success_latest", "agent_failure_latest"];

function visitSample(value, parentKey = "") {
  if (Array.isArray(value)) {
    for (const item of value) visitSample(item, parentKey);
    return;
  }
  if (!value || typeof value !== "object") return;
  for (const [key, child] of Object.entries(value)) {
    if (key === "live_check_result") continue;
    if (typeof child === "string" && expectedMetricNames.has(child) && ["name", "metric_name", "instrument_name", "signal_name"].includes(key)) {
      observedMetrics.add(child);
    }
    if ((key === "metrics" || key === "metric") && child && typeof child === "object") {
      collectMetricNames(child);
    }
    visitSample(child, key);
  }
}

function collectMetricNames(value) {
  if (Array.isArray(value)) {
    for (const item of value) collectMetricNames(item);
    return;
  }
  if (!value || typeof value !== "object") return;
  for (const [key, child] of Object.entries(value)) {
    if (typeof child === "string" && expectedMetricNames.has(child) && ["name", "metric_name", "instrument_name", "signal_name"].includes(key)) {
      observedMetrics.add(child);
    }
    collectMetricNames(child);
  }
}

function collectAdvice(value, findings = []) {
  if (Array.isArray(value)) {
    for (const item of value) collectAdvice(item, findings);
    return findings;
  }
  if (!value || typeof value !== "object") return findings;
  for (const [key, child] of Object.entries(value)) {
    if (key === "all_advice" && Array.isArray(child)) findings.push(...child);
    collectAdvice(child, findings);
  }
  return findings;
}

for (const scenario of expectedScenarios) {
  const reportPath = path.join(reportDir, `${scenario}.json`);
  assert.ok(fs.existsSync(reportPath), `Missing Weaver report for ${scenario}: ${reportPath}`);
  const report = JSON.parse(fs.readFileSync(reportPath, "utf8"));
  const samples = Array.isArray(report.samples) ? report.samples : [];
  assert.ok(samples.length > 0, `Weaver report for ${scenario} contains no samples`);
  for (const sample of samples) {
    for (const finding of collectAdvice(sample)) {
      const context = finding.context || {};
      const attribute = context.attribute_key || context.missing_attribute || context.attribute;
      const metric = finding.signal_name || context.metric_name || context.metric;
      if (finding.id === "missing_attribute" || finding.id === "genai_expected_attribute_missing") {
        assert.ok(!String(attribute || "").startsWith("gen_ai."), `${scenario}: missing GenAI attribute ${attribute}`);
      }
      if (finding.id === "missing_metric") {
        assert.ok(!String(metric || "").startsWith("gen_ai."), `${scenario}: missing GenAI metric ${metric}`);
      }
    }
    visitSample(sample);
  }
}

for (const name of expectedMetricNames) {
  assert.ok(observedMetrics.has(name), `Latest GenAI reports did not observe metric ${name}`);
}

// Weaver's missing_metric advice has no context, so the runner cannot constrain
// that compatibility exception by metric name. Check it on the unfiltered report.
const compatibilityReport = JSON.parse(fs.readFileSync(path.join(reportDir, "agent_success_compat.json"), "utf8"));
assert.ok(compatibilityReport.samples?.length > 0, "Compatibility report contains no samples");
for (const sample of compatibilityReport.samples) {
  for (const finding of collectAdvice(sample)) {
    if (finding.id === "missing_metric") {
      assert.equal(finding.signal_name, "gen_ai.client.token.usage", `Unexpected unregistered compatibility metric ${finding.signal_name}`);
    }
  }
}

console.log("Latest GenAI semantic-convention reports contain the required attributes and metrics.");
