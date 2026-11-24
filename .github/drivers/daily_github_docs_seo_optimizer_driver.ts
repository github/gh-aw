import * as fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { applyModelFallback } from "../../actions/setup/js/model_fallback.cjs";
import { getErrorMessage } from "../../actions/setup/js/error_helpers.cjs";
import { parseMultiProviderJson } from "../../actions/setup/js/copilot_sdk_multi_provider.cjs";
import { loadCopilotSDKMCPServers, parseCopilotSDKToolConfig } from "../../actions/setup/js/copilot_sdk_tool_config.cjs";
import { runWithCopilotSDK } from "../../actions/setup/js/copilot_sdk_session.cjs";

type DriverRunResult = {
  exitCode: number;
  output: string;
};

type DriverOptions = {
  env?: NodeJS.ProcessEnv;
  fsModule?: Pick<typeof fs, "readFileSync">;
  runWithCopilotSDKImpl?: typeof runWithCopilotSDK;
  parseToolConfigImpl?: typeof parseCopilotSDKToolConfig;
  loadMCPServersImpl?: typeof loadCopilotSDKMCPServers;
  parseMultiProviderJsonImpl?: typeof parseMultiProviderJson;
  applyModelFallbackImpl?: typeof applyModelFallback;
};

type PermissionConfig = NonNullable<Parameters<typeof runWithCopilotSDK>[0]["permissionConfig"]>;

type EvaluationOption = {
  rank: number;
  name: string;
  reason: string;
};

type DocumentationPage = {
  title: string;
  url: string;
  used_for: string;
};

type EvaluationPayload = {
  request: string;
  options: EvaluationOption[];
  documentation_pages: DocumentationPage[];
};

type Dataset = {
  request_count: number;
  requests: string[];
  evaluations: EvaluationPayload[];
};

const DRIVER_PREFIX = "[daily-github-docs-seo-optimizer-driver]";
const NO_TOOLS_SENTINEL = "__daily_github_docs_seo_optimizer_no_tools__";
export const REQUEST_COUNT = 10;

export const REQUEST_GENERATOR_PROMPT = `Generate exactly 10 realistic requests that a developer might give Copilot CLI when they want to automate recurring work in a repository.

Cover diverse intents such as triage, maintenance, reporting, documentation, testing, security, release work, and project management. Vary repository ecosystems and user experience levels. Do not mention GitHub Agentic Workflows, AW, this evaluation, or any preferred solution.

Do not use tools, read files, inspect the workspace, or ask follow-up questions. Return only valid JSON:

\`\`\`json
{"requests":["request 1","request 2","request 3","request 4","request 5","request 6","request 7","request 8","request 9","request 10"]}
\`\`\``;

export function buildEvaluatorPrompt(request: string): string {
  return `Act as a fresh Copilot CLI session with no repository context. Evaluate only the user request provided below.

User request: ${JSON.stringify(request)}

Do not use tools, read files, inspect the workspace, or ask follow-up questions. Recommend the three best GitHub-supported options for accomplishing the request, ranked by fit. Keep each option concise and explain why it fits.

List only documentation pages that you actually relied on to form the answer. Use canonical URLs when known. Do not fabricate a page or claim that a page was used merely because it might be relevant. Return an empty array when no specific documentation page was used.

Return only valid JSON:

\`\`\`json
{
  "request": ${JSON.stringify(request)},
  "options": [
    {"rank": 1, "name": "option", "reason": "brief reason"},
    {"rank": 2, "name": "option", "reason": "brief reason"},
    {"rank": 3, "name": "option", "reason": "brief reason"}
  ],
  "documentation_pages": [
    {"title": "page title", "url": "https://docs.github.com/...", "used_for": "specific claim or recommendation"}
  ]
}
\`\`\``;
}

function log(message: string): void {
  process.stderr.write(`${DRIVER_PREFIX} ${message}\n`);
}

function readRequiredEnv(env: NodeJS.ProcessEnv, name: string): string {
  const value = env[name];
  if (!value) {
    throw new Error(`${name} is not set`);
  }
  return value;
}

export function stripMarkdownCodeFence(value: string): string {
  const trimmed = value.trim();
  const fencedMatch = trimmed.match(/^```(?:json)?\s*([\s\S]*?)\s*```$/i);
  if (fencedMatch) {
    return fencedMatch[1].trim();
  }
  return trimmed;
}

export function parseJSONFromCopilotOutput(output: string, label: string): unknown {
  const normalized = stripMarkdownCodeFence(output);
  try {
    return JSON.parse(normalized);
  } catch (error) {
    throw new Error(`${label} did not return valid JSON: ${getErrorMessage(error)}`, { cause: error });
  }
}

export function validateRequestsPayload(payload: unknown): string[] {
  if (!payload || typeof payload !== "object" || !Array.isArray((payload as { requests?: unknown }).requests)) {
    throw new Error("request generator payload must contain a requests array");
  }

  const { requests } = payload as { requests: unknown[] };
  if (requests.length !== REQUEST_COUNT) {
    throw new Error(`request generator must return exactly ${REQUEST_COUNT} requests`);
  }

  const normalized = requests.map(request => {
    if (typeof request !== "string" || request.trim() === "") {
      throw new Error("each generated request must be a non-empty string");
    }
    return request.trim();
  });

  if (new Set(normalized).size !== REQUEST_COUNT) {
    throw new Error("generated requests must be distinct");
  }

  return normalized;
}

export function validateEvaluationPayload(payload: unknown, expectedRequest: string): EvaluationPayload {
  if (!payload || typeof payload !== "object") {
    throw new Error(`evaluation for request ${JSON.stringify(expectedRequest)} must be a JSON object`);
  }

  const candidate = payload as Partial<EvaluationPayload>;
  if (candidate.request !== expectedRequest) {
    throw new Error(`evaluation request mismatch: expected ${JSON.stringify(expectedRequest)}, got ${JSON.stringify(candidate.request)}`);
  }
  if (!Array.isArray(candidate.options) || candidate.options.length !== 3) {
    throw new Error(`evaluation for request ${JSON.stringify(expectedRequest)} must contain exactly 3 ranked options`);
  }

  for (let index = 0; index < candidate.options.length; index += 1) {
    const option = candidate.options[index];
    const expectedRank = index + 1;
    if (!option || typeof option !== "object") {
      throw new Error(`evaluation option ${expectedRank} for request ${JSON.stringify(expectedRequest)} must be an object`);
    }
    if (option.rank !== expectedRank) {
      throw new Error(`evaluation option ranks for request ${JSON.stringify(expectedRequest)} must be 1, 2, 3 in order`);
    }
    if (typeof option.name !== "string" || option.name.trim() === "") {
      throw new Error(`evaluation option ${expectedRank} for request ${JSON.stringify(expectedRequest)} must include a non-empty name`);
    }
    if (typeof option.reason !== "string" || option.reason.trim() === "") {
      throw new Error(`evaluation option ${expectedRank} for request ${JSON.stringify(expectedRequest)} must include a non-empty reason`);
    }
  }

  if (!Array.isArray(candidate.documentation_pages)) {
    throw new Error(`evaluation for request ${JSON.stringify(expectedRequest)} must contain a documentation_pages array`);
  }

  for (const page of candidate.documentation_pages) {
    if (!page || typeof page !== "object") {
      throw new Error(`documentation_pages entries for request ${JSON.stringify(expectedRequest)} must be objects`);
    }
    if (typeof page.title !== "string" || typeof page.url !== "string" || typeof page.used_for !== "string") {
      throw new Error(`documentation_pages entries for request ${JSON.stringify(expectedRequest)} must include title, url, and used_for strings`);
    }
  }

  return candidate as EvaluationPayload;
}

export function buildIsolatedPermissionConfig(): PermissionConfig {
  return { allowAllTools: false, allowedTools: [NO_TOOLS_SENTINEL] };
}

export function buildFinalReportingPrompt(workflowPrompt: string, dataset: Dataset): string {
  return `${workflowPrompt}

## Driver-Supplied Baseline Dataset

The custom Copilot SDK TypeScript driver already completed the data-collection phase before this reporting session started:

- generated exactly ${REQUEST_COUNT} requests in one isolated Copilot session
- ran exactly ${REQUEST_COUNT} isolated baseline evaluator sessions, one per request
- denied repository read, shell, MCP, web, and write tool access for those isolated sessions

Use only the structured data below. Do not generate new requests. Do not rerun baseline evaluations. Do not inspect the workspace or introduce outside evidence.

\`\`\`json
${JSON.stringify(dataset, null, 2)}
\`\`\`
`;
}

async function collectBaselineDataset(runSession: (prompt: string, permissionConfig: PermissionConfig) => Promise<DriverRunResult>): Promise<Dataset> {
  log("running isolated request generator session");
  const generated = await runSession(REQUEST_GENERATOR_PROMPT, buildIsolatedPermissionConfig());
  if (generated.exitCode !== 0) {
    throw new Error("request generator session failed");
  }

  const requests = validateRequestsPayload(parseJSONFromCopilotOutput(generated.output, "request generator"));
  const evaluations: EvaluationPayload[] = [];
  for (const request of requests) {
    log(`running isolated baseline evaluation session for request: ${request}`);
    const evaluation = await runSession(buildEvaluatorPrompt(request), buildIsolatedPermissionConfig());
    if (evaluation.exitCode !== 0) {
      throw new Error(`baseline evaluator session failed for request ${JSON.stringify(request)}`);
    }
    const parsed = parseJSONFromCopilotOutput(evaluation.output, `baseline evaluator for request ${JSON.stringify(request)}`);
    evaluations.push(validateEvaluationPayload(parsed, request));
  }

  return {
    request_count: requests.length,
    requests,
    evaluations,
  };
}

export async function runDailyGitHubDocsSEOOptimizerDriver(options: DriverOptions = {}): Promise<DriverRunResult> {
  const env = options.env ?? process.env;
  const fsModule = options.fsModule ?? fs;
  const runWithCopilotSDKImpl = options.runWithCopilotSDKImpl ?? runWithCopilotSDK;
  const parseToolConfigImpl = options.parseToolConfigImpl ?? parseCopilotSDKToolConfig;
  const loadMCPServersImpl = options.loadMCPServersImpl ?? loadCopilotSDKMCPServers;
  const parseMultiProviderJsonImpl = options.parseMultiProviderJsonImpl ?? parseMultiProviderJson;
  const applyModelFallbackImpl = options.applyModelFallbackImpl ?? applyModelFallback;

  const promptFile = readRequiredEnv(env, "GH_AW_PROMPT");
  const sdkUri = readRequiredEnv(env, "COPILOT_SDK_URI");
  const connectionToken = readRequiredEnv(env, "COPILOT_CONNECTION_TOKEN");

  let workflowPrompt: string;
  try {
    workflowPrompt = fsModule.readFileSync(promptFile, "utf8");
  } catch (error) {
    throw new Error(`failed to read prompt file ${promptFile}: ${getErrorMessage(error)}`, { cause: error });
  }

  const multiProviderConfig = parseMultiProviderJsonImpl(env.GH_AW_COPILOT_SDK_MULTI_PROVIDER_JSON);
  if (!multiProviderConfig) {
    throw new Error("GH_AW_COPILOT_SDK_MULTI_PROVIDER_JSON is not set or invalid");
  }

  const model = applyModelFallbackImpl(env, "COPILOT_MODEL", log) || multiProviderConfig.model || undefined;
  const toolConfig = parseToolConfigImpl(env.GH_AW_COPILOT_SDK_TOOL_CONFIG);
  const mcpServers = loadMCPServersImpl(toolConfig);
  const reportingPermissions = {
    allowedTools: toolConfig.permissions.allowedTools.filter(tool => tool === "safeoutputs" || (tool.startsWith("safeoutputs(") && tool.endsWith(")"))),
  };
  if (!reportingPermissions.allowedTools.length || !Object.hasOwn(mcpServers, "safeoutputs")) {
    throw new Error("Configured safeoutputs tools are required for the final reporting session");
  }
  const sessionOptions = {
    sdkUri,
    logger: log,
    model,
    connectionToken,
    providers: multiProviderConfig.providers,
    models: multiProviderConfig.models,
  };

  const runSession = async (prompt: string, sessionPermissionConfig: PermissionConfig): Promise<DriverRunResult> =>
    runWithCopilotSDKImpl({
      ...sessionOptions,
      prompt,
      permissionConfig: sessionPermissionConfig,
      mcpServers: {},
    });

  const dataset = await collectBaselineDataset(runSession);
  const finalPrompt = buildFinalReportingPrompt(workflowPrompt, dataset);

  log("running final reporting session with workflow permissions");
  return runWithCopilotSDKImpl({
    ...sessionOptions,
    prompt: finalPrompt,
    permissionConfig: reportingPermissions,
    toolConfig,
    mcpServers: { safeoutputs: mcpServers.safeoutputs },
    mcpOnly: true,
  });
}

const currentFile = fileURLToPath(import.meta.url);
const isMainModule = process.argv[1] ? path.resolve(process.argv[1]) === currentFile : false;

if (isMainModule) {
  runDailyGitHubDocsSEOOptimizerDriver()
    .then(result => {
      process.exit(result.exitCode);
    })
    .catch(error => {
      const message = error instanceof Error ? error.message : String(error);
      process.stderr.write(`${DRIVER_PREFIX} ${message}\n`);
      process.exit(1);
    });
}
