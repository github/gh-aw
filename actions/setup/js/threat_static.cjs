// @ts-check
const { analyzeScaffolding } = require("./threat_scaffolding.cjs");
const { labels, uninspectable, warning } = require("./threat_artifacts.cjs");

const placeholder = /\$\{GH_AW_EXPR_[A-Za-z0-9_]+\}|__GH_AW_[A-Za-z0-9_]+?__|\{\{[^{}]+\}\}/g;
const descriptions = {
  prompt: "The rendered workflow prompt",
  prompt_template: "The workflow prompt template",
  prompt_import_tree: "The prompt import tree",
};

function extractInputs(template, rendered) {
  if (!template.trim() || !rendered.trim()) return { inputs: [], indeterminate: true };
  const names = template.match(placeholder) || [];
  if (!names.length) return { inputs: [], indeterminate: rendered !== template };
  if (names.length > 1024) return { inputs: [], indeterminate: true };
  const segments = template.split(placeholder);
  const inputs = [];
  let pos = 0;
  for (let i = 0; i < names.length; i++) {
    if (!rendered.startsWith(segments[i], pos)) return { inputs, indeterminate: true };
    const after = pos + segments[i].length;
    const next = segments[i + 1];
    if (!next && i + 1 < names.length) return { inputs, indeterminate: true };
    pos = next ? rendered.indexOf(next, after) : rendered.length;
    if (pos < 0) return { inputs, indeterminate: true };
    const content = rendered.slice(after, pos).trim();
    if (content) inputs.push({ placeholder: names[i], content });
  }
  return { inputs, indeterminate: !inputs.length || pos + segments.at(-1).length !== rendered.length };
}

function buildAnalysis(arts) {
  const { prompt, prompt_template: source, prompt_import_tree: tree } = arts.inputs;
  const { template, section } = analyzeScaffolding(prompt.content, source.status === "ok" ? source.content : "");
  const { inputs, indeterminate } = extractInputs(template, prompt.content);
  const sections = [];
  const notices = [];
  const warnings = [];
  for (const field of ["prompt_template", "prompt_import_tree", "prompt"]) {
    const input = arts.inputs[field];
    if (input.status !== "unreadable") continue;
    notices.push(uninspectable(`${descriptions[field]} (${labels[field]})`, "it is present but could not be read"));
    if (!arts.warnings.some(w => w.field === field))
      warnings.push(
        warning(
          field,
          `Prompt analysis input ${labels[field]} (${input.path}) could not be read: ${input.error}. That content exists but was not inspected; the trusted-vs-untrusted prompt analysis is degraded. This failure is not itself evidence of a threat.`
        )
      );
  }
  if (notices.length) {
    if (indeterminate)
      notices.push("Because of the above, the trusted-vs-untrusted breakdown below is incomplete: an empty or missing list of extracted untrusted inputs does NOT establish that no untrusted content reached this run's prompt.");
    sections.push("### Uninspectable Prompt Analysis Inputs\n\n" + notices.join("\n\n"));
  }
  for (const field of ["prompt_template", "prompt_import_tree"]) {
    if (arts.inputs[field].status === "absent")
      warnings.push(warning(field, `Missing or empty prompt analysis artifact: ${labels[field]}. Trusted-vs-untrusted prompt analysis is degraded; ensure the host stages it non-empty. This failure is not itself evidence of a threat.`));
  }
  if (section) sections.push(section);
  if (indeterminate) sections.push("Prompt alignment is indeterminate. Inspect the rendered prompt; missing extracted inputs do not rule out runtime content.");
  return { inputs, indeterminate, evidence: { template, importTree: tree.status === "ok" ? tree.content : null, extractedInputs: inputs }, guidance: sections.join("\n\n"), warnings };
}

function computeEligibility(arts, analysis) {
  // Presence and inability to inspect are both eligible. A warning is not
  // evidence of a threat, but it must not structurally rule one out.
  const degraded = field => arts.warnings.some(w => w.field === field);
  const patch = arts.patches.files.length > 0 || degraded("patch");
  const memory = arts.memory.files.length > 0 || degraded("comment_memory");
  return {
    prompt_injection: !analysis || analysis.indeterminate || analysis.inputs.length > 0 || arts.activation !== null || memory,
    secret_leak: arts.outputUnreadable || arts.outputSize > 0 || patch || memory,
    malicious_patch: patch,
  };
}

module.exports = { buildAnalysis, computeEligibility, extractInputs };
