// @ts-check
"use strict";

const fs = require("fs");
const path = require("path");
const { normalizeAssignment, normalizeClaimScope, readClaimScopeContext, withClaimExecution, assertClaimAuthorized, claimArtifactPath, assertClaimArtifactDirectory } = require("./work_queue_claim_scope.cjs");
const { validateAdapter, preparedAdapterPath } = require("./work_queue_claim_adapters.cjs");
const { parseStrictJSON } = require("./work_queue_codec.cjs");

async function prepareAdapterContext(options) {
  const assignment = normalizeAssignment(options.assignment);
  const adapter = validateAdapter(options.adapter);
  const index = Number(options.index);
  if (!Number.isSafeInteger(index) || index < 0 || index > 15) throw new Error("Claim adapter slot must be within the original bounded assignment");
  const member = assignment.claims[index];
  if (!member) return { active: false };
  return withClaimExecution({ assignment, claim_handle: member.handle, authorize: options.authorize }, async () => {
    await assertClaimAuthorized(
      { type: "work_queue_adapter_prepare", claim_handle: member.handle, repo: adapter["target-repo"] },
      {
        authorize: options.authorize,
        github: options.github,
        context: options.context,
        requireCompletion: false,
        resource: { repository: adapter["target-repo"] },
      }
    );
    const messages = [];
    for (const input of options.messages || []) {
      try {
        const message = normalizeClaimScope(input, assignment);
        if (message.type === options.type && message.claim_handle === member.handle) messages.push(message);
      } catch {
        // Invalid messages remain errors at authoritative ingestion. They never
        // become executable custom input or prevent valid sibling preparation.
      }
    }
    if (!messages.length) return { active: false, claim_handle: member.handle };
    const directory = claimArtifactPath(options.artifactRoot || "/tmp/gh-aw", member.handle);
    fs.mkdirSync(path.join(directory, "adapters"), { recursive: true, mode: 0o700 });
    assertClaimArtifactDirectory(directory);
    const inputFile = path.join(directory, "adapter-input.json");
    const outputFile = preparedAdapterPath(options.artifactRoot || "/tmp/gh-aw", member.handle, options.type);
    const assignmentFile = path.join(directory, "original-assignment.json");
    const writeOptions = { mode: 0o400, flag: "wx" };
    fs.writeFileSync(inputFile, JSON.stringify({ version: 3, claim_handle: member.handle, type: options.type, messages }) + "\n", writeOptions);
    fs.writeFileSync(assignmentFile, JSON.stringify(assignment) + "\n", writeOptions);
    return { active: true, claim_handle: member.handle, directory, inputFile, outputFile, assignmentFile, payload: messages[0], messages };
  });
}

async function main(options = {}) {
  const coreApi = options.core || global.core;
  const scope = readClaimScopeContext();
  if (!scope?.assignment) throw new Error("Claim adapter preparation requires the authenticated immutable worker snapshot");
  const { loadAgentOutput } = require("./load_agent_output.cjs");
  const output = loadAgentOutput();
  const adapter = parseStrictJSON(process.env.GH_AW_CLAIM_ADAPTER_CONFIG || "{}");
  const result = await prepareAdapterContext({
    ...options,
    assignment: scope.assignment,
    adapter,
    index: process.env.GH_AW_CLAIM_ADAPTER_INDEX,
    type: process.env.GH_AW_CLAIM_ADAPTER_TYPE,
    messages: output.success ? output.items : [],
  });
  coreApi.setOutput("active", result.active ? "true" : "false");
  if (!result.active) return result;
  for (const [name, value] of Object.entries({
    GH_AW_CLAIM_HANDLE: result.claim_handle,
    GH_AW_CLAIM_INPUT: result.inputFile,
    GH_AW_AGENT_OUTPUT: result.inputFile,
    GH_AW_CLAIM_OUTPUT: result.outputFile,
    GH_AW_CLAIM_ASSIGNMENT: result.assignmentFile,
    GH_AW_CLAIM_ARTIFACTS: result.directory,
  }))
    coreApi.exportVariable(name, value);
  coreApi.setOutput("payload", JSON.stringify(result.payload));
  coreApi.setOutput("directory", result.directory);
  coreApi.setOutput("claim_handle", result.claim_handle);
  return result;
}

module.exports = { prepareAdapterContext, main };
