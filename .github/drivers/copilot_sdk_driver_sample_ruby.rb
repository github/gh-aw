#!/usr/bin/env ruby

require "open3"

NODE_DRIVER = <<~'JS'
  const fs = require("node:fs");
  const { CopilotClient, RuntimeConnection, approveAll } = require("@microsoft/copilot-sdk");

  function requiredEnv(name) {
    const value = process.env[name];
    if (!value) {
      throw new Error(`${name} is not set`);
    }
    return value;
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

  async function main() {
    const promptPath = requiredEnv("GH_AW_PROMPT");
    const endpoint = new URL(requiredEnv("COPILOT_SDK_URI"));
    const port = endpoint.port || (endpoint.protocol === "http:" ? "80" : "");
    if (!port || !["http:", "tcp:"].includes(endpoint.protocol)) {
      throw new Error("COPILOT_SDK_URI must identify a plaintext HTTP endpoint or a TCP endpoint with an explicit port");
    }
    const connectionToken = requiredEnv("COPILOT_CONNECTION_TOKEN");
    const model = requiredEnv("COPILOT_MODEL");
    const prompt = fs.readFileSync(promptPath, "utf8");

    const client = new CopilotClient({
      mode: "coding-agent",
      runtime: RuntimeConnection.forTcp(endpoint.hostname.replace(/^\[|\]$/g, ""), Number(port), { connectionToken }),
    });

    let session;
    await client.start();
    try {
      session = await client.createSession({
        permissions: { onRequest: approveAll },
        workingDirectory: process.env.GITHUB_WORKSPACE || process.cwd(),
        model,
      });
      const response = await session.sendAndWait({ prompt });
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

  main().catch(error => {
    process.stderr.write(`[copilot-sdk-driver-sample-ruby] ${error instanceof Error ? error.message : String(error)}\n`);
    process.exit(1);
  });
JS

begin
  stdout_str, stderr_str, status = Open3.capture3("node", "-e", NODE_DRIVER)
  $stdout.write(stdout_str)
  $stderr.write(stderr_str)
  exit(status.exitstatus || 1)
rescue StandardError => error
  $stderr.write("[copilot-sdk-driver-sample-ruby] #{error.class}: #{error}\n")
  exit(1)
end
