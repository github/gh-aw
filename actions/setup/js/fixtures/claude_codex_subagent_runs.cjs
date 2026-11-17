// @ts-check
"use strict";

/**
 * Recorded artifacts for the Claude/Codex sub-agent model tests (github/gh-aw#70667).
 *
 * Typist, https://github.com/github/gh-aw/actions/runs/39415087125 (Claude Code 2.1.296):
 * - claude-codex-sub-agents/typist-39415087125/.claude/agents/duplicate-type-finder.md is the
 *   activation artifact's staged agent, unchanged (no `name`, `model: small`).
 * - awf-reflect-claude-typist-39415087125.json is the agent artifact's
 *   sandbox/firewall/awf-reflect.json reduced to endpoint provider/port/base_url/configured/
 *   models/models_url, `models_fetch_complete` and the same-run `model_aliases`.
 * - The records below come from agent-stdio.log (init and the failed `Agent` call) and
 *   usage/aw_session.jsonl (the built-in general-purpose delegation that followed). Only the
 *   fields used here are kept; the delegated prompt is truncated.
 *
 * daily-arxiv-researcher, https://github.com/github/gh-aw/actions/runs/39394965127 (Codex 0.163.0):
 * - claude-codex-sub-agents/arxiv-39394965127/.codex/agents/*.md are the activation artifact's
 *   four staged agents, unchanged.
 * - awf-reflect-codex-arxiv-39394965127.json is reduced the same way.
 * - The run's only model.resolution record (role `main`) is below; it has no subagent records.
 *
 * purelock, https://github.com/github/gh-aw/actions/runs/39601756922 (Codex, provider github):
 * - claude-codex-sub-agents/purelock-39601756922/.codex/agents/test-writer.md is the
 *   activation artifact's staged agent, unchanged (`model: large`).
 * - awf-reflect-codex-purelock-39601756922.json is reduced the same way, keeping each
 *   endpoint's `routing_models` reduced to model_id/supported_endpoints/
 *   candidate_metadata_complete (Claude models list only /v1/messages and /chat/completions).
 * - agent-stdio.log line 115 recorded `codex sub-agent 'test-writer': model 'large' ->
 *   'claude-sonnet-5.5'` before endpoint compatibility was checked.
 *
 * Package Specification Enforcer, https://github.com/github/gh-aw/actions/runs/39771119523
 * (Codex 0.163.0, provider openai, github/gh-aw#71157):
 * - claude-codex-sub-agents/spec-enforcer-39771119523/.codex/agents/readme-spec-extractor.md is
 *   the `readme-spec-extractor` block of .github/workflows/spec-enforcer.md at the run's revision
 *   c931ace9725c968f83144b0634822abac2cefd9e, as extractInlineSubAgents stages it
 *   (`model: small`); the run uploaded no activation artifact.
 * - awf-reflect-codex-spec-enforcer-39771119523.json is reduced the same way as purelock. AWF's
 *   native OpenAI endpoint lists protocol names (`chat_completions`, `responses`), not paths.
 * - agent-stdio.log lines 106-110 recorded each `small` sub-agent resolving to
 *   'gpt-5-mini-2025-08-07' and then refusing startup because /responses could not be verified.
 */

const path = require("path");

const TYPIST_RUN_ID = "39415087125";
const ARXIV_RUN_ID = "39394965127";
const typistAgentsDir = path.join(__dirname, "claude-codex-sub-agents", `typist-${TYPIST_RUN_ID}`, ".claude", "agents");
const arxivAgentsDir = path.join(__dirname, "claude-codex-sub-agents", `arxiv-${ARXIV_RUN_ID}`, ".codex", "agents");
const PURELOCK_RUN_ID = "39601756922";
const purelockAgentsDir = path.join(__dirname, "claude-codex-sub-agents", `purelock-${PURELOCK_RUN_ID}`, ".codex", "agents");
const SPEC_ENFORCER_RUN_ID = "39771119523";
const specEnforcerAgentsDir = path.join(__dirname, "claude-codex-sub-agents", `spec-enforcer-${SPEC_ENFORCER_RUN_ID}`, ".codex", "agents");

/** agent-stdio.log line 123: native initialization, custom agents not registered. */
const typistInit = {
  type: "system",
  subtype: "init",
  cwd: "/home/runner/work/gh-aw/gh-aw",
  model: "claude-sonnet-5",
  claude_code_version: "2.1.296",
  agents: ["claude", "Explore", "general-purpose", "Plan", "statusline-setup"],
};

/** agent-stdio.log line 151: the parent's first named delegation (prompt truncated). */
const typistAgentToolUse = {
  type: "tool_use",
  id: "toolu_011gZG7sPRerwjpFjDVKyUsb",
  name: "Agent",
  input: { description: "Find duplicate Go type definitions", subagent_type: "duplicate-type-finder", run_in_background: true, prompt: "Repository: /home/runner/work/gh-aw/gh-aw (github/gh-aw). Follow your system ins" },
};

/** agent-stdio.log line 152: the delegation failed because the agent was not registered. */
const typistAgentToolResult = {
  type: "tool_result",
  content: "Agent type 'duplicate-type-finder' not found. Available agents: claude, Explore, general-purpose, Plan, statusline-setup",
  is_error: true,
  tool_use_id: "toolu_011gZG7sPRerwjpFjDVKyUsb",
};

/** usage/aw_session.jsonl lines 247 and 249: the built-in fallback delegation (provenance omitted). */
const typistFallbackSubagentStarted = {
  type: "subagent.started",
  agentId: "a14bc56b8185b2efb",
  data: { toolCallId: "toolu_01C3hnT2mvz1njAfWxMJHg65", agentName: "general-purpose", agentType: "general-purpose", executionMode: "background", spawnDepth: 1, sessionId: "2139bbfd-8795-42c5-8f19-af8314bc98f9", parentToolUseId: null },
};
const typistFallbackSubagentConfigured = {
  type: "subagent.configured",
  timestamp: "2026-10-20T11:30:55.661Z",
  agentId: "a14bc56b8185b2efb",
  data: { model: "claude-sonnet-5", sessionId: "2139bbfd-8795-42c5-8f19-af8314bc98f9", parentToolUseId: null },
};

/** usage/aw_session.jsonl line 831 (provenance omitted). */
const arxivMainModelResolution = {
  type: "model.resolution",
  data: { engine: "codex", role: "main", configured_model: "gpt-5.6-sol", resolved_model: "gpt-5.6-sol", provider: "openai", status: "exact", cause: null },
};

/** @returns {any} A fresh copy of the reduced Typist /reflect. */
function typistReflect() {
  return structuredClone(require("./awf-reflect-claude-typist-39415087125.json"));
}

/** @returns {any} A fresh copy of the reduced arXiv /reflect. */
function arxivReflect() {
  return structuredClone(require("./awf-reflect-codex-arxiv-39394965127.json"));
}

/** @returns {any} A fresh copy of the reduced purelock /reflect (Copilot endpoint with routing_models). */
function purelockReflect() {
  return structuredClone(require("./awf-reflect-codex-purelock-39601756922.json"));
}

/** @returns {any} A fresh copy of the reduced Spec Enforcer /reflect (native OpenAI endpoint with routing_models). */
function specEnforcerReflect() {
  return structuredClone(require("./awf-reflect-codex-spec-enforcer-39771119523.json"));
}

module.exports = {
  ARXIV_RUN_ID,
  arxivAgentsDir,
  arxivMainModelResolution,
  arxivReflect,
  PURELOCK_RUN_ID,
  purelockAgentsDir,
  purelockReflect,
  SPEC_ENFORCER_RUN_ID,
  specEnforcerAgentsDir,
  specEnforcerReflect,
  TYPIST_RUN_ID,
  typistAgentsDir,
  typistAgentToolResult,
  typistAgentToolUse,
  typistFallbackSubagentConfigured,
  typistFallbackSubagentStarted,
  typistInit,
  typistReflect,
};
