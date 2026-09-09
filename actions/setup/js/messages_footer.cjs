// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Footer Message Module
 *
 * This module provides footer and installation instructions generation
 * for safe-output workflows.
 */

const { getMessages, renderTemplate, renderTemplateFromFile, toSnakeCase, getPromptPath } = require("./messages_core.cjs");
const { getMissingInfoSections } = require("./missing_messages_helper.cjs");
const { getBlockedDomains, generateBlockedDomainsSection } = require("./firewall_blocked_domains.cjs");
const { getDifcFilteredEvents, generateDifcFilteredSection } = require("./gateway_difc_filtered.cjs");
const { formatCompactInteger } = require("./compact_numbers.cjs");
const { formatAIC } = require("./model_costs.cjs");
const { reduceModelNameToIdentifier } = require("./model_aliases.cjs");
const { getDetectionWarningMessage } = require("./messages_run_status.cjs");

/**
 * Get the detection caution alert if the detection job found a potential issue.
 * Reads GH_AW_DETECTION_CONCLUSION and GH_AW_DETECTION_REASON from environment variables.
 * Returns the caution alert markdown when conclusion is "warning", or empty string otherwise.
 * @param {string} workflowName - Name of the workflow
 * @param {string} runUrl - URL of the workflow run
 * @returns {string} Caution alert markdown or empty string
 */
function getDetectionCautionAlert(workflowName, runUrl) {
  const detectionConclusion = process.env.GH_AW_DETECTION_CONCLUSION;
  if (detectionConclusion !== "warning") {
    return "";
  }
  const detectionReason = process.env.GH_AW_DETECTION_REASON || "";
  return getDetectionWarningMessage({ workflowName, runUrl, reason: detectionReason });
}

/**
 * @param {string|undefined} raw
 * @returns {number|undefined}
 */
function parsePositiveAIC(raw) {
  const parsed = raw ? Number.parseFloat(raw) : NaN;
  return Number.isFinite(parsed) && parsed > 0 ? parsed : undefined;
}

/**
 * @param {number|string|undefined} raw
 * @returns {number|undefined}
 */
function parseExplicitContextAIC(raw) {
  const parsed = raw !== undefined && raw !== null && raw !== "" ? Number.parseFloat(String(raw)) : NaN;
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : undefined;
}

/**
 * @param {string|undefined} raw
 * @returns {number|undefined}
 */
function parsePositiveAmbientContext(raw) {
  const parsed = raw ? Number.parseInt(raw, 10) : NaN;
  return Number.isFinite(parsed) && parsed > 0 ? parsed : undefined;
}

/**
 * @returns {{ ambientContext: number|undefined, ambientContextFormatted: string|undefined, ambientContextSuffix: string }}
 */
function getAmbientContextFromEnv() {
  const ambientContext = parsePositiveAmbientContext(process.env.GH_AW_AMBIENT_CONTEXT);
  const ambientContextFormatted = typeof ambientContext === "number" ? formatCompactInteger(ambientContext) : undefined;
  return {
    ambientContext,
    ambientContextFormatted,
    ambientContextSuffix: ambientContextFormatted ? ` · ⊞ ${ambientContextFormatted}` : "",
  };
}

/**
 * @param {string} label
 * @param {number|undefined} value
 * @param {string|undefined} [modelAlias]
 * @returns {{ value: number|undefined, formatted: string|undefined, suffix: string }}
 */
function buildAICEntry(label, value, modelAlias) {
  const formatted = typeof value === "number" ? formatAIC(value) : undefined;
  const prefix = [label, modelAlias].filter(Boolean).join(" ");
  return {
    value,
    formatted,
    suffix: formatted ? ` · ${prefix ? `${prefix}${modelAlias ? " · " : " "}` : ""}${formatted} AIC` : "",
  };
}

/**
 * Read AI Credits from the environment and return the total, separate entries when
 * threat detection consumed credits, and the pre-formatted footer suffix.
 * @returns {{
 *   aiCredits: number|undefined,
 *   aiCreditsFormatted: string|undefined,
 *   aiCreditsSuffix: string,
 *   aiModel: string|undefined,
 *   aiModelShort: string|undefined,
 *   compressedModelName: string|undefined,
 *   agenticEngine: string|undefined,
 *   agentModelIdentifier: string,
 *   agentAiCredits: number|undefined,
 *   agentAiCreditsFormatted: string|undefined,
 *   agentAiCreditsSuffix: string,
 *   evalsAiCredits: number|undefined,
 *   evalsAiCreditsFormatted: string|undefined,
 *   evalsAiCreditsSuffix: string,
 *   threatDetectionAiCredits: number|undefined,
 *   threatDetectionAiCreditsFormatted: string|undefined,
 *   threatDetectionAiCreditsSuffix: string
 * }}
 */
function getAICFromEnv() {
  const aiModel = process.env.GH_AW_PRIMARY_MODEL || process.env.GH_AW_ENGINE_MODEL || undefined;
  const compressedModelName = reduceModelNameToIdentifier(aiModel);
  const agenticEngine = process.env.GH_AW_ENGINE_ID || undefined;
  const agentModelIdentifier = [agenticEngine, compressedModelName].filter(Boolean).join(" · ");
  const totalAIC = parsePositiveAIC(process.env.GH_AW_AIC);
  const explicitAgentAIC = parsePositiveAIC(process.env.GH_AW_AGENT_AIC);
  const evalsAIC = parsePositiveAIC(process.env.GH_AW_EVALS_AIC);
  const threatDetectionAIC = parsePositiveAIC(process.env.GH_AW_THREAT_DETECTION_AIC);
  const agentAIC = typeof explicitAgentAIC === "number" ? explicitAgentAIC : totalAIC;
  const agentEntry = buildAICEntry("", agentAIC, agentModelIdentifier);
  const evalsEntry = buildAICEntry("◇", evalsAIC);
  const threatDetectionEntry = buildAICEntry("⌖", threatDetectionAIC);
  const useBreakdown = threatDetectionEntry.suffix.length > 0 || evalsEntry.suffix.length > 0;
  const aiCredits = useBreakdown ? (agentAIC || 0) + (threatDetectionAIC || 0) + (evalsAIC || 0) : typeof totalAIC === "number" ? totalAIC : agentAIC;
  const aiCreditsFormatted = typeof aiCredits === "number" ? formatAIC(aiCredits) : undefined;
  const aiCreditsSuffix = useBreakdown ? `${agentEntry.suffix}${threatDetectionEntry.suffix}${evalsEntry.suffix}` : buildAICEntry("", aiCredits, agentModelIdentifier).suffix;

  return {
    aiCredits,
    aiCreditsFormatted,
    aiCreditsSuffix,
    aiModel,
    aiModelShort: compressedModelName,
    compressedModelName,
    agentModelIdentifier,
    agenticEngine,
    agentAiCredits: agentEntry.value,
    agentAiCreditsFormatted: agentEntry.formatted,
    agentAiCreditsSuffix: agentEntry.suffix,
    evalsAiCredits: evalsEntry.value,
    evalsAiCreditsFormatted: evalsEntry.formatted,
    evalsAiCreditsSuffix: evalsEntry.suffix,
    threatDetectionAiCredits: threatDetectionEntry.value,
    threatDetectionAiCreditsFormatted: threatDetectionEntry.formatted,
    threatDetectionAiCreditsSuffix: threatDetectionEntry.suffix,
  };
}

/**
 * @typedef {Object} FooterContext
 * @property {string} workflowName - Name of the workflow
 * @property {string} runUrl - URL of the workflow run
 * @property {string} [agenticWorkflowUrl] - Direct URL to the agentic workflow page ({run_url}/agentic_workflow)
 * @property {string} [workflowSource] - Source of the workflow (owner/repo/path@ref)
 * @property {string} [workflowSourceUrl] - GitHub URL for the workflow source
 * @property {number|string} [triggeringNumber] - Issue, PR, or discussion number that triggered this workflow
 * @property {"issue"|"PR"|"discussion"} [triggeringType] - Triggering item type used in the default footer
 * @property {string} [historyUrl] - GitHub search URL for items created by this workflow (for the history link)
 * @property {string} [historyLink] - Pre-formatted markdown history link (e.g. " · [◷](url)"), or "" if unavailable
 * @property {number|string} [aiCredits] - Total AI Credits cost for the run (1 AIC == 0.01 USD)
 * @property {string} [emoji] - Optional emoji representing the workflow (from frontmatter)
 * @property {string} [slashCommand] - Slash command name (without leading slash) for the run-again hint, when applicable
 * @property {string} [slashCommandPlaceholder] - Custom hint text appended after the command name (replaces default "to run again")
 * @property {string} [labelCommand] - Label command name for the run-again hint, when applicable
 */

/**
 * Get the footer message, using custom template if configured.
 * @param {FooterContext} ctx - Context for footer generation
 * @returns {string} Footer message
 */
function getFooterMessage(ctx) {
  const messages = getMessages();

  const {
    aiCredits: envAIC,
    aiCreditsFormatted: envAICFormatted,
    aiCreditsSuffix: envAICSuffix,
    aiModel,
    aiModelShort,
    compressedModelName,
    agentModelIdentifier,
    agenticEngine,
    agentAiCredits,
    agentAiCreditsFormatted,
    agentAiCreditsSuffix,
    evalsAiCredits,
    evalsAiCreditsFormatted,
    evalsAiCreditsSuffix,
    threatDetectionAiCredits,
    threatDetectionAiCreditsFormatted,
    threatDetectionAiCreditsSuffix,
  } = getAICFromEnv();
  const { ambientContext: envAmbientContext, ambientContextFormatted: envAmbientContextFormatted, ambientContextSuffix: envAmbientContextSuffix } = getAmbientContextFromEnv();
  const aiCredits = ctx.aiCredits ?? envAIC;
  const ambientContext = envAmbientContext;
  const detectionConclusion = process.env.GH_AW_DETECTION_CONCLUSION || undefined;
  const detectionReason = process.env.GH_AW_DETECTION_REASON || undefined;

  // Pre-compute history_link as a ready-to-use markdown suffix (empty string when unavailable)
  const historyLink = ctx.historyUrl ? ` · [◷](${ctx.historyUrl})` : "";

  // Pre-compute agentic_workflow_url as the direct link to the agentic workflow page
  const agenticWorkflowUrl = ctx.agenticWorkflowUrl || (ctx.runUrl ? `${ctx.runUrl}/agentic_workflow` : "");

  const hasExplicitContextAIC = ctx.aiCredits !== undefined && ctx.aiCredits !== null;
  const explicitContextAIC = parseExplicitContextAIC(ctx.aiCredits);
  let aiCreditsFormatted = envAICFormatted;
  let aiCreditsSuffix = envAICSuffix;
  if (hasExplicitContextAIC) {
    aiCreditsFormatted = explicitContextAIC ? formatAIC(explicitContextAIC) : undefined;
    aiCreditsSuffix = buildAICEntry("", explicitContextAIC, agentModelIdentifier).suffix;
  }
  const aiCreditsSuffixForTemplate = `${aiCreditsSuffix}${envAmbientContextSuffix}`;

  // Create context with both camelCase and snake_case keys, including computed history_link and agentic_workflow_url
  const templateContext = toSnakeCase({
    ...ctx,
    aiCredits,
    historyLink,
    agenticWorkflowUrl,
    aiCreditsFormatted,
    aiCreditsSuffix: aiCreditsSuffixForTemplate,
    aiModel,
    aiModelShort,
    aiCreditsUnit: "AIC",
    detectionConclusion,
    detectionReason,
    ambientContext,
    ambientContextFormatted: envAmbientContextFormatted,
    ambientContextSuffix: envAmbientContextSuffix,
    agentAiCredits,
    agentAiCreditsFormatted,
    agentAiCreditsSuffix,
    evalsAiCredits,
    evalsAiCreditsFormatted,
    evalsAiCreditsSuffix,
    threatDetectionAiCredits,
    threatDetectionAiCreditsFormatted,
    threatDetectionAiCreditsSuffix,
  });

  const getRunAgainHints = renderedFooter => {
    let hints = "";
    if (ctx.slashCommand) {
      const hintText = ctx.slashCommandPlaceholder || "to run again";
      hints += `> <sub>Comment <em>/{slash_command}</em> ${hintText}</sub>`;
    }
    if (ctx.labelCommand) {
      if (hints) hints += "\n";
      hints += `> <sub>Add label <em>{label_command}</em> to run again</sub>`;
    }
    const renderedHints = renderTemplate(hints, templateContext);
    if (!renderedHints) return "";
    const separator = renderedFooter && !renderedFooter.endsWith("\n") ? "\n" : "";
    return separator + renderedHints;
  };

  // Use custom footer template if configured
  if (messages?.footer) {
    const renderedCustomFooter = renderTemplate(messages.footer, templateContext);
    return renderedCustomFooter + getRunAgainHints(renderedCustomFooter);
  }

  // Default footer template - includes emoji prefix when available
  const workflowLabel = ctx.emoji ? `${ctx.emoji} {workflow_name}` : "{workflow_name}";
  let defaultFooter = `> Generated by [${workflowLabel}]({run_url})`;
  if (ctx.triggeringNumber) {
    const prefix = ctx.triggeringType === "discussion" ? "discussion " : "";
    defaultFooter += ` for ${prefix}#{triggering_number}`;
  }
  const metricSuffixes = [];
  if (aiCredits) {
    metricSuffixes.push(aiCreditsSuffix);
  }
  if (ambientContext) {
    metricSuffixes.push(envAmbientContextSuffix);
  }
  if (metricSuffixes.length > 0) {
    defaultFooter += metricSuffixes.join("");
  }
  // Append history link when available
  if (ctx.historyUrl) {
    defaultFooter += " · [◷]({history_url})";
  }
  const renderedDefaultFooter = renderTemplate(defaultFooter, templateContext);
  return renderedDefaultFooter + getRunAgainHints(renderedDefaultFooter);
}

/**
 * Render a deterministic footer configured for a generated issue or pull request body.
 * @param {string|undefined} template - Body footer template
 * @param {{workflowName: string, runUrl: string}} ctx - Template context
 * @returns {string} Rendered body footer, or an empty string when not configured
 */
function getBodyFooterMessage(template, ctx) {
  if (!template) {
    return "";
  }
  return renderTemplate(template, toSnakeCase(ctx));
}

/**
 * @param {string|undefined} commandsJSON
 * @returns {string|undefined}
 */
function getFirstCommandHint(commandsJSON) {
  if (!commandsJSON) {
    return undefined;
  }
  try {
    const commands = JSON.parse(commandsJSON);
    if (Array.isArray(commands) && commands.length > 0 && typeof commands[0] === "string") {
      return commands[0];
    }
  } catch {
    // Silently ignore malformed JSON; hints are non-critical enhancements.
  }
  return undefined;
}

/**
 * Get the footer installation instructions, using custom template if configured.
 * @param {FooterContext} ctx - Context for footer generation
 * @returns {string} Footer installation message or empty string if no source
 */
function getFooterInstallMessage(ctx) {
  if (!ctx.workflowSource || !ctx.workflowSourceUrl) {
    return "";
  }

  const messages = getMessages();

  // Pre-compute agentic_workflow_url as the direct link to the agentic workflow page
  const agenticWorkflowUrl = ctx.agenticWorkflowUrl || (ctx.runUrl ? `${ctx.runUrl}/agentic_workflow` : "");

  // Create context with both camelCase and snake_case keys, including computed agentic_workflow_url
  const templateContext = toSnakeCase({ ...ctx, agenticWorkflowUrl });

  const defaultInstallTemplatePath = getPromptPath("workflow_install_note.md");

  // Use custom installation message if configured
  return messages?.footerInstall ? renderTemplate(messages.footerInstall, templateContext) : renderTemplateFromFile(defaultInstallTemplatePath, templateContext);
}

/**
 * @typedef {Object} WorkflowRecompileContext
 * @property {string} workflowName - Name of the workflow
 * @property {string} runUrl - URL of the workflow run
 * @property {string} [agenticWorkflowUrl] - Direct URL to the agentic workflow page ({run_url}/agentic_workflow)
 * @property {string} repository - Repository name (owner/repo)
 */

/**
 * Get the footer message for workflow recompile issues, using custom template if configured.
 * @param {WorkflowRecompileContext} ctx - Context for footer generation
 * @returns {string} Footer message for workflow recompile issues
 */
function getFooterWorkflowRecompileMessage(ctx) {
  const messages = getMessages();

  // Pre-compute agentic_workflow_url as the direct link to the agentic workflow page
  const agenticWorkflowUrl = ctx.agenticWorkflowUrl || (ctx.runUrl ? `${ctx.runUrl}/agentic_workflow` : "");

  // Create context with both camelCase and snake_case keys
  const templateContext = toSnakeCase({ ...ctx, agenticWorkflowUrl });

  // Default footer template
  const defaultFooter = "> Generated by [{workflow_name}]({run_url})";

  // Use custom workflow recompile footer if configured, otherwise use default footer
  let footer = messages?.footerWorkflowRecompile ? renderTemplate(messages.footerWorkflowRecompile, templateContext) : renderTemplate(defaultFooter, templateContext);

  return footer;
}

/**
 * Get the footer message for comments on workflow recompile issues, using custom template if configured.
 * @param {WorkflowRecompileContext} ctx - Context for footer generation
 * @returns {string} Footer message for comments on workflow recompile issues
 */
function getFooterWorkflowRecompileCommentMessage(ctx) {
  const messages = getMessages();

  // Pre-compute agentic_workflow_url as the direct link to the agentic workflow page
  const agenticWorkflowUrl = ctx.agenticWorkflowUrl || (ctx.runUrl ? `${ctx.runUrl}/agentic_workflow` : "");

  // Create context with both camelCase and snake_case keys
  const templateContext = toSnakeCase({ ...ctx, agenticWorkflowUrl });

  // Default footer template
  const defaultFooter = "> Updated by [{workflow_name}]({run_url})";

  // Use custom workflow recompile comment footer if configured, otherwise use default footer
  let footer = messages?.footerWorkflowRecompileComment ? renderTemplate(messages.footerWorkflowRecompileComment, templateContext) : renderTemplate(defaultFooter, templateContext);

  return footer;
}

/**
 * @typedef {Object} AgentFailureContext
 * @property {string} workflowName - Name of the workflow
 * @property {string} runUrl - URL of the workflow run
 * @property {string} [agenticWorkflowUrl] - Direct URL to the agentic workflow page ({run_url}/agentic_workflow)
 * @property {string} [workflowSource] - Source of the workflow (owner/repo/path@ref)
 * @property {string} [workflowSourceUrl] - GitHub URL for the workflow source
 * @property {string} [historyUrl] - GitHub search URL for issues created by this workflow (for the history link)
 * @property {number|string} [aiCredits] - Total AI Credits cost for the run (1 AIC == 0.01 USD)
 */

/**
 * Get the footer message for agent failure tracking issues, using custom template if configured.
 * @param {AgentFailureContext} ctx - Context for footer generation
 * @returns {string} Footer message for agent failure tracking issues
 */
function getFooterAgentFailureIssueMessage(ctx) {
  const messages = getMessages();

  // Pre-compute history_link as a ready-to-use markdown suffix (empty string when unavailable)
  const historyLink = ctx.historyUrl ? ` · [◷](${ctx.historyUrl})` : "";

  // Pre-compute agentic_workflow_url as the direct link to the agentic workflow page
  const agenticWorkflowUrl = ctx.agenticWorkflowUrl || (ctx.runUrl ? `${ctx.runUrl}/agentic_workflow` : "");

  const {
    aiCredits: envAIC,
    aiCreditsFormatted: envAICFormatted,
    aiCreditsSuffix: envAICSuffix,
    aiModel,
    aiModelShort,
    compressedModelName,
    agentModelIdentifier,
    agentAiCredits,
    agentAiCreditsFormatted,
    agentAiCreditsSuffix,
    evalsAiCredits,
    evalsAiCreditsFormatted,
    evalsAiCreditsSuffix,
    threatDetectionAiCredits,
    threatDetectionAiCreditsFormatted,
    threatDetectionAiCreditsSuffix,
  } = getAICFromEnv();
  const { ambientContext, ambientContextFormatted, ambientContextSuffix } = getAmbientContextFromEnv();
  const hasExplicitContextAIC = ctx.aiCredits !== undefined && ctx.aiCredits !== null;
  const explicitContextAIC = parseExplicitContextAIC(ctx.aiCredits);
  const aiCredits = hasExplicitContextAIC ? explicitContextAIC : envAIC;
  const aiCreditsFormatted = hasExplicitContextAIC ? (explicitContextAIC ? formatAIC(explicitContextAIC) : undefined) : envAICFormatted;
  const aiCreditsSuffix = hasExplicitContextAIC ? buildAICEntry("", explicitContextAIC, agentModelIdentifier).suffix : envAICSuffix;
  const aiCreditsSuffixForTemplate = `${aiCreditsSuffix}${ambientContextSuffix}`;
  const detectionConclusion = process.env.GH_AW_DETECTION_CONCLUSION || undefined;
  const detectionReason = process.env.GH_AW_DETECTION_REASON || undefined;

  // Create context with both camelCase and snake_case keys, including computed history_link and agentic_workflow_url
  const templateContext = toSnakeCase({
    ...ctx,
    historyLink,
    agenticWorkflowUrl,
    aiCredits,
    aiCreditsFormatted,
    aiCreditsSuffix: aiCreditsSuffixForTemplate,
    aiModel,
    aiModelShort,
    aiCreditsUnit: "AIC",
    detectionConclusion,
    detectionReason,
    agentAiCredits,
    agentAiCreditsFormatted,
    agentAiCreditsSuffix,
    evalsAiCredits,
    evalsAiCreditsFormatted,
    evalsAiCreditsSuffix,
    threatDetectionAiCredits,
    threatDetectionAiCreditsFormatted,
    threatDetectionAiCreditsSuffix,
    ambientContext,
    ambientContextFormatted,
    ambientContextSuffix,
  });

  // Use custom agent failure issue footer if configured, otherwise use default footer
  let footer;
  if (messages?.agentFailureIssue) {
    footer = renderTemplate(messages.agentFailureIssue, templateContext);
  } else {
    // Default footer template with link to workflow run
    let defaultFooter = "> Generated from [{workflow_name}]({run_url})";
    if (aiCredits) {
      defaultFooter += aiCreditsSuffix;
    }
    if (ambientContext) {
      defaultFooter += ambientContextSuffix;
    }
    // Append history link when available
    if (ctx.historyUrl) {
      defaultFooter += " · [◷]({history_url})";
    }
    footer = renderTemplate(defaultFooter, templateContext);
  }

  return footer;
}

/**
 * Get the footer message for comments on agent failure tracking issues, using custom template if configured.
 * @param {AgentFailureContext} ctx - Context for footer generation
 * @returns {string} Footer message for comments on agent failure tracking issues
 */
function getFooterAgentFailureCommentMessage(ctx) {
  const messages = getMessages();

  // Pre-compute history_link as a ready-to-use markdown suffix (empty string when unavailable)
  const historyLink = ctx.historyUrl ? ` · [◷](${ctx.historyUrl})` : "";

  // Pre-compute agentic_workflow_url as the direct link to the agentic workflow page
  const agenticWorkflowUrl = ctx.agenticWorkflowUrl || (ctx.runUrl ? `${ctx.runUrl}/agentic_workflow` : "");

  const {
    aiCredits: envAIC,
    aiCreditsFormatted: envAICFormatted,
    aiCreditsSuffix: envAICSuffix,
    aiModel,
    aiModelShort,
    compressedModelName,
    agentModelIdentifier,
    agentAiCredits,
    agentAiCreditsFormatted,
    agentAiCreditsSuffix,
    evalsAiCredits,
    evalsAiCreditsFormatted,
    evalsAiCreditsSuffix,
    threatDetectionAiCredits,
    threatDetectionAiCreditsFormatted,
    threatDetectionAiCreditsSuffix,
  } = getAICFromEnv();
  const { ambientContext, ambientContextFormatted, ambientContextSuffix } = getAmbientContextFromEnv();
  const hasExplicitContextAIC = ctx.aiCredits !== undefined && ctx.aiCredits !== null;
  const explicitContextAIC = parseExplicitContextAIC(ctx.aiCredits);
  const aiCredits = hasExplicitContextAIC ? explicitContextAIC : envAIC;
  const aiCreditsFormatted = hasExplicitContextAIC ? (explicitContextAIC ? formatAIC(explicitContextAIC) : undefined) : envAICFormatted;
  const aiCreditsSuffix = hasExplicitContextAIC ? buildAICEntry("", explicitContextAIC, agentModelIdentifier).suffix : envAICSuffix;
  const aiCreditsSuffixForTemplate = `${aiCreditsSuffix}${ambientContextSuffix}`;
  const detectionConclusion = process.env.GH_AW_DETECTION_CONCLUSION || undefined;
  const detectionReason = process.env.GH_AW_DETECTION_REASON || undefined;

  // Create context with both camelCase and snake_case keys, including computed history_link and agentic_workflow_url
  const templateContext = toSnakeCase({
    ...ctx,
    historyLink,
    agenticWorkflowUrl,
    aiCredits,
    aiCreditsFormatted,
    aiCreditsSuffix: aiCreditsSuffixForTemplate,
    aiModel,
    aiModelShort,
    aiCreditsUnit: "AIC",
    detectionConclusion,
    detectionReason,
    agentAiCredits,
    agentAiCreditsFormatted,
    agentAiCreditsSuffix,
    evalsAiCredits,
    evalsAiCreditsFormatted,
    evalsAiCreditsSuffix,
    threatDetectionAiCredits,
    threatDetectionAiCreditsFormatted,
    threatDetectionAiCreditsSuffix,
    ambientContext,
    ambientContextFormatted,
    ambientContextSuffix,
  });

  // Use custom agent failure comment footer if configured, otherwise use default footer
  let footer;
  if (messages?.agentFailureComment) {
    footer = renderTemplate(messages.agentFailureComment, templateContext);
  } else {
    // Default footer template with link to workflow run
    let defaultFooter = "> Generated from [{workflow_name}]({run_url})";
    if (aiCredits) {
      defaultFooter += aiCreditsSuffix;
    }
    if (ambientContext) {
      defaultFooter += ambientContextSuffix;
    }
    // Append history link when available
    if (ctx.historyUrl) {
      defaultFooter += " · [◷]({history_url})";
    }
    footer = renderTemplate(defaultFooter, templateContext);
  }

  return footer;
}

/**
 * Generates an XML comment marker with agentic workflow metadata for traceability.
 * This marker enables searching and tracing back items generated by an agentic workflow.
 *
 * The marker format is:
 * <!-- gh-aw-agentic-workflow: workflow-name, gh-aw-tracker-id: id, engine: copilot, version: 1.0.0, model: gpt-5, run: https://github.com/... -->
 *
 * @param {string} workflowName - Name of the workflow
 * @param {string} runUrl - URL of the workflow run
 * @returns {string} XML comment marker with workflow metadata
 */
function generateXMLMarker(workflowName, runUrl) {
  // Read engine metadata from environment variables
  const engineId = process.env.GH_AW_ENGINE_ID || "";
  const engineVersion = process.env.GH_AW_ENGINE_VERSION || "";
  const engineModel = process.env.GH_AW_ENGINE_MODEL || "";
  const trackerId = process.env.GH_AW_TRACKER_ID || "";
  const runId = process.env.GITHUB_RUN_ID || "";
  const workflowId = process.env.GH_AW_WORKFLOW_ID || "";

  // Build the key-value pairs for the marker
  const parts = [];

  // Always include agentic-workflow name
  parts.push(`gh-aw-agentic-workflow: ${workflowName}`);

  // Add tracker-id if available (for searchability and tracing)
  if (trackerId) {
    parts.push(`gh-aw-tracker-id: ${trackerId}`);
  }

  // Add engine ID if available
  if (engineId) {
    parts.push(`engine: ${engineId}`);
  }

  // Add version if available
  if (engineVersion) {
    parts.push(`version: ${engineVersion}`);
  }

  // Add model if available
  if (engineModel) {
    parts.push(`model: ${engineModel}`);
  }

  // Add numeric run ID if available
  if (runId) {
    parts.push(`id: ${runId}`);
  }

  // Add workflow identifier if available
  if (workflowId) {
    parts.push(`workflow_id: ${workflowId}`);
  }

  // Always include run URL
  parts.push(`run: ${runUrl}`);

  // Return the XML comment marker
  return `<!-- ${parts.join(", ")} -->`;
}

/**
 * @typedef {Object} GenerateFooterOptions
 * @property {boolean} [skipDetectionCaution=false] - When true, omit the threat detection caution alert
 *   from the footer. Use this when the caution alert has already been placed at the top of the body.
 */

/**
 * Generate the complete footer with AI attribution and optional installation instructions.
 * This is a drop-in replacement for the original generateFooter function.
 * @param {string} workflowName - Name of the workflow
 * @param {string} runUrl - URL of the workflow run
 * @param {string} workflowSource - Source of the workflow (owner/repo/path@ref)
 * @param {string} workflowSourceURL - GitHub URL for the workflow source
 * @param {number|undefined} triggeringIssueNumber - Issue number that triggered this workflow
 * @param {number|undefined} triggeringPRNumber - Pull request number that triggered this workflow
 * @param {number|undefined} triggeringDiscussionNumber - Discussion number that triggered this workflow
 * @param {string|null|undefined} [historyUrl] - GitHub search URL for items created by this workflow
 * @param {GenerateFooterOptions} [options] - Optional generation flags
 * @returns {string} Complete footer text
 */
function generateFooterWithMessages(workflowName, runUrl, workflowSource, workflowSourceURL, triggeringIssueNumber, triggeringPRNumber, triggeringDiscussionNumber, historyUrl, options) {
  // Determine triggering number (issue takes precedence, then PR, then discussion)
  let triggeringNumber;
  /** @type {"issue"|"PR"|"discussion"|undefined} */
  let triggeringType;
  if (triggeringIssueNumber) {
    triggeringNumber = triggeringIssueNumber;
    triggeringType = "issue";
  } else if (triggeringPRNumber) {
    triggeringNumber = triggeringPRNumber;
    triggeringType = "PR";
  } else if (triggeringDiscussionNumber) {
    triggeringNumber = triggeringDiscussionNumber;
    triggeringType = "discussion";
  }

  // Read workflow emoji from environment variable if available.
  const emoji = process.env.GH_AW_WORKFLOW_EMOJI || undefined;

  // Read slash command from GH_AW_COMMANDS (JSON array) when available.
  // Use the first command as the hint. This is only set when the workflow has a slash command trigger.
  const slashCommand = getFirstCommandHint(process.env.GH_AW_COMMANDS);

  // Read label command from GH_AW_LABEL_COMMANDS (JSON array) when available.
  // Use the first configured label as the hint.
  const labelCommand = getFirstCommandHint(process.env.GH_AW_LABEL_COMMANDS);

  // Read optional footer hint placeholder from GH_AW_COMMAND_PLACEHOLDER.
  // When set, it replaces the default "to run again" suffix in the slash command hint.
  const slashCommandPlaceholder = process.env.GH_AW_COMMAND_PLACEHOLDER;

  const ctx = {
    workflowName,
    runUrl,
    workflowSource,
    workflowSourceUrl: workflowSourceURL,
    triggeringNumber,
    triggeringType,
    historyUrl: historyUrl || undefined,
    emoji,
    slashCommand,
    slashCommandPlaceholder,
    labelCommand,
  };

  const { skipDetectionCaution = false } = options || {};

  // Collect guard notices to show BEFORE the attribution footer
  let guardNotices = "";

  // Add detection caution alert if detection job found a potential issue.
  // Skip when the caller has already placed the caution alert at the top of the body.
  if (!skipDetectionCaution) {
    const detectionCaution = getDetectionCautionAlert(workflowName, runUrl);
    if (detectionCaution) {
      guardNotices += detectionCaution;
    }
  }

  // Add firewall blocked domains section if any domains were blocked
  const blockedDomains = getBlockedDomains();
  const blockedDomainsSection = generateBlockedDomainsSection(blockedDomains);
  if (blockedDomainsSection) {
    guardNotices += blockedDomainsSection;
  }

  // Add integrity filtering section if any items were filtered
  try {
    const difcFilteredEvents = getDifcFilteredEvents();
    const difcFilteredSection = generateDifcFilteredSection(difcFilteredEvents);
    if (difcFilteredSection) {
      guardNotices += difcFilteredSection;
    }
  } catch {
    // ignore errors so the rest of the footer is always preserved
  }

  // Attribution footer line comes after any guard notices
  // Attribution footer line comes after any guard notices. Only add the separating
  // blank line when guard notices are present, otherwise the footer would start with
  // stray blank lines that render as a large gap after the body.
  let footer = guardNotices ? guardNotices.trim() + "\n\n" + getFooterMessage(ctx) : getFooterMessage(ctx);

  // Add installation instructions if source is available
  const installMessage = getFooterInstallMessage(ctx);
  if (installMessage) {
    footer += "\n>\n" + installMessage;
  }

  // Add missing tools and data sections if available
  const missingInfoSections = getMissingInfoSections();
  if (missingInfoSections) {
    footer += missingInfoSections;
  }

  // Add XML comment marker for traceability
  footer += "\n\n" + generateXMLMarker(workflowName, runUrl);

  footer += "\n";
  return footer;
}

module.exports = {
  getDetectionCautionAlert,
  getBodyFooterMessage,
  getFooterMessage,
  getFooterInstallMessage,
  getFooterWorkflowRecompileMessage,
  getFooterWorkflowRecompileCommentMessage,
  getFooterAgentFailureIssueMessage,
  getFooterAgentFailureCommentMessage,
  generateFooterWithMessages,
  generateXMLMarker,
};
