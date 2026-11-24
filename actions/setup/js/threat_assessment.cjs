// @ts-check
const fs = require("node:fs");
const { loadArtifacts, uninspectable } = require("./threat_artifacts.cjs");
const { buildAnalysis, computeEligibility } = require("./threat_static.cjs");
const { escapeWorkflowCommandValue } = require("./actions_secret_masking.cjs");
const defaultTemplate = require("./threat_assessment_prompt.json").join("\n");

function reportWarnings(warnings, allWarnings, allPrimaryMissing = false) {
  const required = new Set(["prompt", "agent_output", "patch"]);
  const strict = process.env.GH_AW_DETECTION_CONTINUE_ON_ERROR?.toLowerCase() === "false";
  const rejectWarnings = process.env.GH_AW_DETECTION_CONTINUE_ON_WARNING?.toLowerCase() === "false";
  for (const { field, code, message } of warnings) {
    const level = rejectWarnings || (strict && required.has(field)) ? "error" : "warning";
    const text = escapeWorkflowCommandValue(message.replaceAll("##[", "##\\["));
    process.stderr.write(`::${level}::${code}: ${text}\n`);
  }
  let reason = "";
  if (allPrimaryMissing) reason = "Prompt, agent output, and patch/bundle files are all missing or empty; refusing to run detection on empty input.";
  else if (strict && warnings.some(w => required.has(w.field))) reason = 'Required detection inputs are missing or unusable and GH_AW_DETECTION_CONTINUE_ON_ERROR is "false"; refusing to run degraded detection.';
  else if (rejectWarnings && warnings.length) reason = 'Artifact channels could not be inspected and GH_AW_DETECTION_CONTINUE_ON_WARNING is "false"; refusing to certify a partially readable bundle.';
  if (reason) throw Object.assign(new Error(reason), { warnings: allWarnings.slice(0, 20) });
}

function budgetText(ms, maxTurns) {
  const time = !ms ? "unlimited" : ms % 60000 === 0 ? `${ms / 60000}m` : ms >= 1000 ? `${ms / 1000}s` : `${ms}ms`;
  return (
    `You are running under a per-attempt budget: **wall-clock timeout ${time}**, **agentic turn cap ${maxTurns || "unlimited"}**. ` +
    "The wall-clock is a hard `SIGKILL` on your process group (this subprocess and every descendant) when the deadline fires, so no partial verdict survives. The turn cap is enforced by the engine host when supported (which is the common case). When either budget is exhausted the run fails with `engine_timeout` and no retry runs — the same prompt would just run away again. Pace yourself: read only what you need to reach a decision, avoid re-reading the same file, and call `threat_detection_result` as soon as you have enough evidence to commit to a verdict. If you are running out of budget, still call `threat_detection_result` with your best current assessment rather than letting the deadline fire with no result recorded."
  );
}

/**
 * Prepare a detector attempt without executing the harness or modifying its
 * inputs. File options are trusted host paths, not inline template strings.
 * @param {{artifactsDir:string, workflowName?:string, workflowDescription?:string,
 * customPrompt?:string, customPromptFile?:string, promptTemplate?:string,
 * timeoutMs?:number, maxTurns?:number}} opts
 */
function prepareAssessment(opts) {
  const arts = loadArtifacts(opts.artifactsDir);
  reportWarnings(arts.warnings, arts.warnings, arts.allPrimaryMissing);
  const analysis = buildAnalysis(arts);
  const warnings = [...arts.warnings, ...analysis.warnings].slice(0, 20);
  reportWarnings(analysis.warnings, warnings);

  const custom = opts.customPromptFile !== undefined ? fs.readFileSync(opts.customPromptFile, "utf8") : (opts.customPrompt ?? process.env.CUSTOM_PROMPT);
  const template = (opts.promptTemplate && fs.readFileSync(opts.promptTemplate, "utf8")) || defaultTemplate;
  const budget = budgetText(opts.timeoutMs ?? 300000, opts.maxTurns ?? 50);
  const promptFile = arts.inputs.prompt;
  const values = {
    WORKFLOW_NAME: opts.workflowName ?? (process.env.WORKFLOW_NAME || "Unnamed Workflow"),
    WORKFLOW_DESCRIPTION: opts.workflowDescription ?? (process.env.WORKFLOW_DESCRIPTION || "No description provided"),
    WORKFLOW_PROMPT_FILE: promptFile.error ? uninspectable("The rendered workflow prompt", "it is present but could not be read") : promptFile.path || "No prompt file found",
    AGENT_OUTPUT_FILE: arts.outputPath,
    AGENT_PATCH_FILE: arts.patches.info,
    ACTIVATION_CONTEXT: analysis.activation,
    COMMENT_MEMORY_FILES: arts.memory.info,
    PROMPT_ANALYSIS: analysis.content || "No prompt template or import tree available. Prompt analysis was not performed.",
    BUDGET: budget,
  };
  // Single pass: $ sequences and placeholder lookalikes in artifact content
  // are data and cannot introduce new template substitutions.
  let prompt = template.replace(/\{([A-Z_]+)\}/g, (token, key) => (Object.hasOwn(values, key) ? values[key] : token));
  if (analysis.content && !template.includes("{PROMPT_ANALYSIS}")) prompt += "\n\n## Prompt Analysis (Trusted vs Untrusted Content)\n\n" + analysis.content;
  if (!template.includes("{BUDGET}")) prompt += "\n\n## Budget\n\n" + budget;
  if (custom) prompt += "\n\n## Additional Instructions\n\n" + custom;
  return { prompt, eligibility: computeEligibility(arts, analysis), warnings };
}

module.exports = { prepareAssessment };
