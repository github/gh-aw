#!/usr/bin/env node
"use strict";

const fs = require("node:fs");
const { isValidProviderConfig, isValidModelConfig, parseMultiProviderJson } = require("../../actions/setup/js/copilot_sdk_multi_provider.cjs");

// Default timeout for a single sendAndWait call: 10 minutes.
// Override via the COPILOT_SDK_SEND_TIMEOUT_MS environment variable.
const DEFAULT_SEND_TIMEOUT_MS = 10 * 60 * 1000;

function readRequiredEnv(name) {
  const value = process.env[name];
  if (!value) {
    throw new Error(`${name} is not set`);
  }
  return value;
}

function parseSendTimeoutMs() {
  const raw = process.env.COPILOT_SDK_SEND_TIMEOUT_MS;
  if (typeof raw === "string") {
    const trimmed = raw.trim();
    if (/^\d+$/.test(trimmed)) {
      const parsed = Number.parseInt(trimmed, 10);
      if (Number.isSafeInteger(parsed) && parsed > 0) {
        return parsed;
      }
    }
  }
  return DEFAULT_SEND_TIMEOUT_MS;
}

function extractAssistantContent(message) {
  if (!message || typeof message !== "object") {
    return "";
  }
  const data = message.data;
  if (data && typeof data.content === "string") {
    return data.content;
  }
  if (typeof message.content === "string") {
    return message.content;
  }
  return "";
}

function buildSessionConfig(model, onPermissionRequest) {
  const config = {
    permissions: { onRequest: onPermissionRequest },
    model,
  };

  // Multi-provider BYOK configuration (preferred)
  const multiProviderJson = process.env.GH_AW_COPILOT_SDK_MULTI_PROVIDER_JSON;
  const multiProviderConfig = parseMultiProviderJson(multiProviderJson);
  if (multiProviderConfig) {
    config.inference = { providers: multiProviderConfig.providers, models: multiProviderConfig.models };
  }

  return config;
}

async function main() {
  const { CopilotClient, RuntimeConnection, approveAll } = require("@microsoft/copilot-sdk");
  const promptPath = readRequiredEnv("GH_AW_PROMPT");
  const endpoint = new URL(readRequiredEnv("COPILOT_SDK_URI"));
  const port = endpoint.port || (endpoint.protocol === "http:" ? "80" : "");
  if (!port || !["http:", "tcp:"].includes(endpoint.protocol)) {
    throw new Error("COPILOT_SDK_URI must identify a plaintext HTTP endpoint or a TCP endpoint with an explicit port");
  }
  const connectionToken = readRequiredEnv("COPILOT_CONNECTION_TOKEN");
  const model = readRequiredEnv("COPILOT_MODEL");
  const prompt = fs.readFileSync(promptPath, "utf8");

  const client = new CopilotClient({
    mode: "coding-agent",
    runtime: RuntimeConnection.forTcp(endpoint.hostname.replace(/^\[|\]$/g, ""), Number(port), { connectionToken }),
  });

  let session;
  await client.start();
  try {
    session = await client.createSession({ ...buildSessionConfig(model, approveAll), workingDirectory: process.env.GITHUB_WORKSPACE || process.cwd() });
    const response = await session.sendAndWait({ prompt }, parseSendTimeoutMs());
    const content = extractAssistantContent(response);
    if (content) {
      process.stdout.write(content.endsWith("\n") ? content : `${content}\n`);
    }
  } finally {
    if (session) {
      await session.disconnect();
    }
    await client.stop();
  }
}

if (require.main === module) {
  main().catch(error => {
    process.stderr.write(`[copilot-sdk-driver-sample-node] ${error instanceof Error ? error.message : String(error)}\n`);
    process.exit(1);
  });
}

module.exports = {
  buildSessionConfig,
  parseMultiProviderJson,
  isValidProviderConfig,
  isValidModelConfig,
};
