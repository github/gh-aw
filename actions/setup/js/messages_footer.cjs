// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Footer Message Module
 *
 * This module provides footer and installation instructions generation
 * for safe-output workflows.
 */

const { getMessages, renderTemplate, renderTemplateFromFile, toSnakeCase, getPromptPath } = require("./messages_core.cjs");
const { getCollectedMissings, generateMissingInfoReportSections } = require("./missing_messages_helper.cjs");
const { getBlockedDomains, generateBlockedDomainsReportSection } = require("./firewall_blocked_domains.cjs");
const { getDifcFilteredEvents, generateDifcFilteredReportSection } = require("./gateway_difc_filtered.cjs");
const { formatCompactInteger } = require("./compact_numbers.cjs");
const { formatEstimatedAIC } = require("./model_costs.cjs");
const { reduceModelNameToIdentifier } = require("./model_aliases.cjs");
const { getDetectionWarningMessage } = require("./messages_run_status.cjs");
const { getEffectiveDetectionConclusion } = require("./threat_detection_warning.cjs");
const { getEffectiveModelLabel, resolveEffectiveModel, validateModelIdentifier } = require("./model_attribution.cjs");
const { currentClaimHandle, claimIdentity, currentClaimFooterLinks } = require("./work_queue_claim_scope.cjs");
const reportAST = require("./report_ast.cjs");
const { parseReportMarkdown } = require("./report_markdown.cjs");

/** @typedef {{type: "quote_line", children: import("./report_ast.cjs").ReportInline[]}} FooterQuoteLine */
/** @typedef {{type: "diagnostic_sections", buildChildren: () => import("./report_ast.cjs").ReportBlock[]}} FooterDiagnosticSections */
/** @typedef {{type: "template", template: string, context: import("./report_markdown.cjs").ReportContext}} FooterTemplate */
/** @typedef {string | FooterQuoteLine | FooterDiagnosticSections | FooterTemplate} FooterPart */

/** @returns {[] | [FooterQuoteLine]} */
function buildWorkQueueClaimFooter() {
  const handle = currentClaimHandle();
  if (!handle) return [];
  const { work_id, claim_id } = claimIdentity(handle);
  const links = currentClaimFooterLinks();
  if (links?.queue) {
    return [
      {
        type: "quote_line",
        children: [{ type: "link", url: links.queue, children: [{ type: "text", value: "Work queue" }] }],
      },
    ];
  }
  return [
    {
      type: "quote_line",
      children: [
        { type: "text", value: "Work " },
        { type: "code", value: work_id },
        { type: "text", value: " · Claim " },
        { type: "code", value: claim_id },
      ],
    },
  ];
}

/**
 * @param {string | undefined} url
 * @returns {import("./report_markdown.cjs").ReportInlineSlot}
 */
function footerHistoryLink(url) {
  return {
    type: "inline_slot",
    children: url
      ? [
          { type: "text", value: " · ", agentTainted: false },
          { type: "link", url, children: [{ type: "text", value: "◷", agentTainted: false }], agentTainted: false },
        ]
      : [],
  };
}

/**
 * Compatibility renderer for string-only, non-issue publishers.
 * Issue documents consume the parts directly without rendering fragments.
 * Keep literal templates and legacy diagnostic formatting untouched.
 * @param {FooterPart[]} parts
 */
function renderFooterParts(parts) {
  const body = reportAST.renderReport({ type: "report", title: "Footer", children: buildFooterBlocks(parts) }).body;
  const last = parts.at(-1);
  return typeof last === "string" && last.endsWith("\n") ? body : body.trimEnd();
}

/**
 * Import template structure once; typed slots cannot be swallowed by a fence.
 * @param {FooterPart[]} parts
 * @returns {import("./report_ast.cjs").ReportBlock[]}
 */
function buildFooterBlocks(parts) {
  const literals = reportAST.normalizeReportText(parts.map(part => (typeof part === "string" ? part : part.type === "template" ? part.template : "")).join(""));
  let prefix = "gh_aw_footer";
  while (literals.includes(prefix)) prefix += "_";
  /** @type {Record<string, import("./report_ast.cjs").ReportInline[]>} */
  const inlineSlots = {};
  /** @type {Record<string, import("./report_ast.cjs").ReportBlock[]>} */
  const blockSlots = {};
  /** @type {import("./report_markdown.cjs").ReportContext} */
  const context = {};
  const template = parts
    .map((part, index) => {
      if (typeof part === "string") return part;
      const key = `${prefix}_${index}`;
      if (part.type === "template") {
        return part.template.replace(/\{(\w+)\}/g, (field, name) => {
          const scoped = `${key}_${name}`;
          context[scoped] = Object.hasOwn(part.context, name) && part.context[name] !== undefined ? part.context[name] : field;
          return `{${scoped}}`;
        });
      }
      if (part.type === "quote_line") {
        inlineSlots[key] = part.children.map(node => ({ ...node, agentTainted: false }));
        return `> {${key}}`;
      }
      blockSlots[key] = part.buildChildren().map(node => ({ ...node, sizeProtected: true }));
      return `\n\n{${key}}\n\n`;
    })
    .join("");
  const consumed = new Set();
  const children = parseReportMarkdown(template, { context, inlineSlots, blockSlots, onBlockSlot: key => consumed.add(key), minimumHeadingLevel: 2, allowMetadata: true, agentTainted: false });
  if (Object.keys(blockSlots).some(key => !consumed.has(key))) throw new TypeError("Footer templates must preserve diagnostic content slots");
  return children;
}

/**
 * Get the detection caution alert if the detection job found a potential issue.
 * Reads GH_AW_DETECTION_CONCLUSION and GH_AW_DETECTION_REASON from environment variables.
 * Returns the caution alert markdown when the effective conclusion is "warning" (including a
 * warn-mode detection engine failure, see getEffectiveDetectionConclusion), or empty string otherwise.
 * @param {string} workflowName - Name of the workflow
 * @param {string} runUrl - URL of the workflow run
 * @returns {string} Caution alert markdown or empty string
 */
function getDetectionCautionAlert(workflowName, runUrl) {
  const detectionReason = process.env.GH_AW_DETECTION_REASON || "";
  if (getEffectiveDetectionConclusion(process.env.GH_AW_DETECTION_CONCLUSION, detectionReason) !== "warning") {
    return "";
  }
  return getDetectionWarningMessage({ workflowName, runUrl, reason: detectionReason });
}

/**
 * @param {string} workflowName
 * @param {string} runUrl
 * @returns {import("./report_ast.cjs").ReportBlock[]}
 */
function getDetectionCautionBlocks(workflowName, runUrl) {
  const reason = process.env.GH_AW_DETECTION_REASON || "";
  if (getEffectiveDetectionConclusion(process.env.GH_AW_DETECTION_CONCLUSION, reason) !== "warning") return [];
  return require("./messages_run_status.cjs").getDetectionWarningBlocks({ workflowName, runUrl, reason });
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
  const formatted = typeof value === "number" && value > 0 ? formatEstimatedAIC(value) : undefined;
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
  const attribution = resolveEffectiveModel();
  const aiModel = attribution.model || undefined;
  const compressedModelName = getEffectiveModelLabel();
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
  const aiCreditsFormatted = typeof aiCredits === "number" ? formatEstimatedAIC(aiCredits) : undefined;
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
  return renderFooterParts(getFooterMessageParts(ctx));
}

/**
 * Keep programmatic attribution as nodes until the complete document is rendered.
 * @param {FooterContext} ctx
 * @returns {FooterPart[]}
 */
function getFooterMessageParts(ctx) {
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
  const historyLink = footerHistoryLink(ctx.historyUrl);

  // Pre-compute agentic_workflow_url as the direct link to the agentic workflow page
  const agenticWorkflowUrl = ctx.agenticWorkflowUrl || (ctx.runUrl ? `${ctx.runUrl}/agentic_workflow` : "");

  const hasExplicitContextAIC = ctx.aiCredits !== undefined && ctx.aiCredits !== null;
  const explicitContextAIC = parseExplicitContextAIC(ctx.aiCredits);
  let aiCreditsFormatted = envAICFormatted;
  let aiCreditsSuffix = envAICSuffix;
  if (hasExplicitContextAIC) {
    aiCreditsFormatted = explicitContextAIC ? formatEstimatedAIC(explicitContextAIC) : undefined;
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

  /** @returns {FooterPart[]} */
  const getFooterHints = () => {
    /** @type {FooterPart[]} */
    const hints = [];
    if (ctx.slashCommand) {
      hints.push("\n", { type: "template", template: "> <sub>Comment <em>/{slash_command}</em> {hint_text}</sub>", context: { ...templateContext, hint_text: ctx.slashCommandPlaceholder || "to run again" } });
    }
    if (ctx.labelCommand) {
      hints.push("\n", { type: "template", template: "> <sub>Add label <em>{label_command}</em> to run again</sub>", context: templateContext });
    }
    const claim = buildWorkQueueClaimFooter();
    return [...(claim.length ? ["\n", ...claim] : []), ...hints];
  };

  // Use custom footer template if configured
  if (messages?.footer) {
    return [{ type: "template", template: messages.footer, context: templateContext }, ...getFooterHints()];
  }

  /** @type {import("./report_ast.cjs").ReportInline[]} */
  const children = [
    { type: "text", value: "Generated by ", agentTainted: false },
    { type: "link", url: ctx.runUrl, children: [{ type: "text", value: ctx.emoji ? `${ctx.emoji} ${ctx.workflowName}` : ctx.workflowName, agentTainted: false }], agentTainted: false },
  ];
  if (ctx.triggeringNumber) {
    const prefix = ctx.triggeringType === "discussion" ? "discussion " : "";
    children.push({ type: "text", value: ` for ${prefix}`, agentTainted: false }, { type: "issue_reference", number: String(ctx.triggeringNumber), agentTainted: false });
  }
  if (aiCredits) {
    children.push({ type: "text", value: aiCreditsSuffix, agentTainted: false });
  }
  if (ambientContext) {
    children.push({ type: "text", value: envAmbientContextSuffix, agentTainted: false });
  }
  // Append history link when available
  if (ctx.historyUrl) {
    children.push({ type: "text", value: " · ", agentTainted: false }, { type: "link", url: ctx.historyUrl, children: [{ type: "text", value: "◷", agentTainted: false }], agentTainted: false });
  }
  return [{ type: "quote_line", children }, ...getFooterHints()];
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
 * @param {string | undefined} template
 * @param {{workflowName: string, runUrl: string}} ctx
 * @returns {import("./report_ast.cjs").ReportBlock[]}
 */
function getBodyFooterBlocks(template, ctx) {
  return template ? buildFooterBlocks([{ type: "template", template, context: toSnakeCase(ctx) }]) : [];
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
  const { template, context } = getFooterAgentFailureIssueTemplate(ctx);
  return renderFooterParts([{ type: "template", template, context }]);
}

/**
 * Expose the template separately so structured renderers can substitute literal
 * metadata after parsing, rather than interpreting metadata as Markdown.
 * @param {AgentFailureContext} ctx
 */
function getFooterAgentFailureIssueTemplate(ctx) {
  const messages = getMessages();

  // Pre-compute history_link as a ready-to-use markdown suffix (empty string when unavailable)
  const historyLink = footerHistoryLink(ctx.historyUrl);

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
  const aiCreditsFormatted = hasExplicitContextAIC ? (explicitContextAIC ? formatEstimatedAIC(explicitContextAIC) : undefined) : envAICFormatted;
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
    failureMetricsSuffix: `${aiCredits ? aiCreditsSuffix : ""}${ambientContext ? ambientContextSuffix : ""}`,
  });

  // Use custom agent failure issue footer if configured, otherwise use default footer
  let template;
  if (messages?.agentFailureIssue) {
    template = messages.agentFailureIssue;
    // User templates retain unknown placeholders as scalar text, not report nodes.
    for (const [placeholder, key] of template.matchAll(/\{(\w+)\}/g)) {
      if (!Object.hasOwn(templateContext, key)) {
        Object.defineProperty(templateContext, key, { value: placeholder, enumerable: true });
      }
    }
  } else {
    // Default footer template with link to workflow run
    template = "> Generated from [{workflow_name}]({run_url}){failure_metrics_suffix}";
    // Append history link when available
    if (ctx.historyUrl) {
      template += " · [◷]({history_url})";
    }
  }

  return { template, context: templateContext };
}

/**
 * Get the footer message for comments on agent failure tracking issues, using custom template if configured.
 * @param {AgentFailureContext} ctx - Context for footer generation
 * @returns {string} Footer message for comments on agent failure tracking issues
 */
function getFooterAgentFailureCommentMessage(ctx) {
  return reportAST.renderReport({ type: "report", title: "Agent failure footer", children: getFooterAgentFailureCommentBlocks(ctx) }).body.trimEnd();
}

/**
 * @param {AgentFailureContext} ctx
 * @returns {import("./report_ast.cjs").ReportBlock[]}
 */
function getFooterAgentFailureCommentBlocks(ctx) {
  const { template, context } = getFooterAgentFailureCommentTemplate(ctx);
  return parseReportMarkdown(template, { context, agentTainted: false });
}

/** @param {AgentFailureContext} ctx */
function getFooterAgentFailureCommentTemplate(ctx) {
  const messages = getMessages();

  // Pre-compute history_link as a ready-to-use markdown suffix (empty string when unavailable)
  const historyLink = footerHistoryLink(ctx.historyUrl);

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
  const aiCreditsFormatted = hasExplicitContextAIC ? (explicitContextAIC ? formatEstimatedAIC(explicitContextAIC) : undefined) : envAICFormatted;
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
    footer = messages.agentFailureComment;
  } else {
    // Default footer template with link to workflow run
    let defaultFooter = "> Generated from [{workflow_name}]({run_url})";
    if (aiCredits) {
      defaultFooter += "{agent_failure_ai_credits_suffix}";
      templateContext.agent_failure_ai_credits_suffix = aiCreditsSuffix;
    }
    if (ambientContext) {
      defaultFooter += "{ambient_context_suffix}";
    }
    // Append history link when available
    if (ctx.historyUrl) {
      defaultFooter += " · [◷]({history_url})";
    }
    footer = defaultFooter;
  }

  return { template: footer, context: templateContext };
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
  return `<!-- ${getAgenticWorkflowMetadata(workflowName, runUrl)
    .map(field => `${field.key}: ${field.value}`)
    .join(", ")} -->`;
}

/**
 * @param {string} workflowName
 * @param {string} runUrl
 * @returns {{key: string, value: string}[]}
 */
function getAgenticWorkflowMetadata(workflowName, runUrl) {
  // Read engine metadata from environment variables
  const engineId = process.env.GH_AW_ENGINE_ID || "";
  const engineVersion = process.env.GH_AW_ENGINE_VERSION || "";
  const attribution = resolveEffectiveModel();
  const engineModel = attribution.model || (attribution.routing ? attribution.requestedModel : validateModelIdentifier(process.env.GH_AW_ENGINE_MODEL));
  const trackerId = process.env.GH_AW_TRACKER_ID || "";
  const runId = process.env.GITHUB_RUN_ID || "";
  const workflowId = process.env.GH_AW_WORKFLOW_ID || "";

  // Build the key-value pairs for the marker
  const parts = [];

  // Always include agentic-workflow name
  parts.push({ key: "gh-aw-agentic-workflow", value: workflowName });

  // Add tracker-id if available (for searchability and tracing)
  if (trackerId) {
    parts.push({ key: "gh-aw-tracker-id", value: trackerId });
  }

  // Add engine ID if available
  if (engineId) {
    parts.push({ key: "engine", value: engineId });
  }

  // Add version if available
  if (engineVersion) {
    parts.push({ key: "version", value: engineVersion });
  }

  // Add model if available
  if (engineModel) {
    parts.push({ key: "model", value: engineModel });
  }
  if (attribution.routing?.status === "selected") {
    if (attribution.effort) parts.push({ key: "effort", value: attribution.effort });
    parts.push({ key: "routed", value: "true" });
  } else if (attribution.routing) {
    parts.push({ key: "routed", value: attribution.routing.status });
  }

  // Add numeric run ID if available
  if (runId) {
    parts.push({ key: "id", value: runId });
  }

  // Add workflow identifier if available
  if (workflowId) {
    parts.push({ key: "workflow_id", value: workflowId });
  }

  // Always include run URL
  parts.push({ key: "run", value: runUrl });

  return parts;
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
  return renderFooterParts(buildGeneratedFooterParts(workflowName, runUrl, workflowSource, workflowSourceURL, triggeringIssueNumber, triggeringPRNumber, triggeringDiscussionNumber, historyUrl, options));
}

/**
 * @param {string} workflowName
 * @param {string} runUrl
 * @param {string} workflowSource
 * @param {string} workflowSourceURL
 * @param {number|undefined} triggeringIssueNumber
 * @param {number|undefined} triggeringPRNumber
 * @param {number|undefined} triggeringDiscussionNumber
 * @param {string|null|undefined} [historyUrl]
 * @param {GenerateFooterOptions} [options]
 * @returns {FooterPart[]}
 */
function buildGeneratedFooterParts(workflowName, runUrl, workflowSource, workflowSourceURL, triggeringIssueNumber, triggeringPRNumber, triggeringDiscussionNumber, historyUrl, options) {
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
  /** @type {import("./report_ast.cjs").ReportBlock[]} */
  const guardNotices = [];

  // Add detection caution alert if detection job found a potential issue.
  // Skip when the caller has already placed the caution alert at the top of the body.
  if (!skipDetectionCaution) {
    guardNotices.push(...getDetectionCautionBlocks(workflowName, runUrl));
  }

  // Add firewall blocked domains section if any domains were blocked
  const blockedDomains = getBlockedDomains();
  guardNotices.push(...generateBlockedDomainsReportSection(blockedDomains));

  // Add integrity filtering section if any items were filtered
  guardNotices.push(...generateDifcFilteredReportSection(getDifcFilteredEvents()));

  // Attribution footer line comes after any guard notices. Only add the separating
  // blank line when guard notices are present, otherwise the footer would start with
  // stray blank lines that render as a large gap after the body.
  /** @type {FooterPart[]} */
  const parts = [];
  if (guardNotices.length) parts.push({ type: "diagnostic_sections", buildChildren: () => guardNotices }, "\n\n");
  parts.push(...getFooterMessageParts(ctx));
  let suffix = "";

  // Add installation instructions if source is available
  if (ctx.workflowSource && ctx.workflowSourceUrl) {
    const template = getMessages()?.footerInstall || renderTemplateFromFile(getPromptPath("workflow_install_note.md"), {});
    parts.push("\n>\n", { type: "template", template, context: toSnakeCase({ ...ctx, agenticWorkflowUrl: `${runUrl}/agentic_workflow` }) });
  }

  // Add missing tools and data sections if available
  const collectedMissings = getCollectedMissings();
  const missingInfoSections = generateMissingInfoReportSections(collectedMissings);
  if (missingInfoSections.length) {
    parts.push(suffix, {
      type: "diagnostic_sections",
      buildChildren: () => missingInfoSections,
    });
    suffix = "";
  }

  // Add XML comment marker for traceability
  suffix += "\n\n" + generateXMLMarker(workflowName, runUrl);

  parts.push(suffix + "\n");
  return parts;
}

module.exports = {
  getDetectionCautionAlert,
  getDetectionCautionBlocks,
  getBodyFooterMessage,
  getBodyFooterBlocks,
  getFooterMessage,
  getFooterInstallMessage,
  getFooterWorkflowRecompileMessage,
  getFooterWorkflowRecompileCommentMessage,
  getFooterAgentFailureIssueMessage,
  getFooterAgentFailureIssueTemplate,
  getAgenticWorkflowMetadata,
  getFooterAgentFailureCommentMessage,
  getFooterAgentFailureCommentBlocks,
  generateFooterWithMessages,
  buildGeneratedFooterParts,
  buildFooterBlocks,
  getFooterMessageParts,
  generateXMLMarker,
};
