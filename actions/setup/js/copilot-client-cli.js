#!/usr/bin/env node
import { CopilotClient } from '@github/copilot-sdk';
import { mkdirSync, readFileSync, appendFileSync } from 'fs';
import { dirname } from 'path';
import debugFactory from 'debug';

var debug = debugFactory("copilot-client");
async function runCopilotSession(config) {
  debug("Starting Copilot session with config:", config);
  mkdirSync(dirname(config.eventLogFile), { recursive: true });
  const logEvent = (type, data, sessionId) => {
    const event = {
      timestamp: (/* @__PURE__ */ new Date()).toISOString(),
      type,
      sessionId,
      data
    };
    appendFileSync(config.eventLogFile, JSON.stringify(event) + "\n", "utf-8");
    debug("Event logged:", type);
  };
  debug("Loading prompt from:", config.promptFile);
  const prompt = readFileSync(config.promptFile, "utf-8");
  logEvent("prompt.loaded", { file: config.promptFile, length: prompt.length });
  debug("Creating Copilot client");
  const clientOptions = {
    logLevel: config.logLevel ?? "info",
    githubToken: config.githubToken,
    useLoggedInUser: config.useLoggedInUser
  };
  if (config.cliUrl) {
    clientOptions.cliUrl = config.cliUrl;
  } else {
    clientOptions.cliPath = config.cliPath;
    clientOptions.cliArgs = config.cliArgs;
    clientOptions.port = config.port;
    clientOptions.useStdio = config.useStdio ?? true;
    clientOptions.autoStart = config.autoStart ?? true;
    clientOptions.autoRestart = config.autoRestart ?? true;
  }
  const client = new CopilotClient(clientOptions);
  logEvent("client.created", {
    cliPath: config.cliPath,
    useStdio: config.useStdio,
    logLevel: config.logLevel
  });
  debug("Starting Copilot client");
  await client.start();
  logEvent("client.started", {});
  let session = null;
  try {
    debug("Creating Copilot session");
    session = await client.createSession({
      model: config.session?.model,
      reasoningEffort: config.session?.reasoningEffort,
      systemMessage: config.session?.systemMessage ? {
        mode: "replace",
        content: config.session.systemMessage
      } : void 0,
      mcpServers: config.session?.mcpServers
    });
    logEvent("session.created", {
      sessionId: session.sessionId,
      model: config.session?.model
    }, session.sessionId);
    debug("Setting up event handlers");
    session.on((event) => {
      logEvent(`session.${event.type}`, event.data, session.sessionId);
      debug("Session event:", event.type, event.data);
    });
    const done = new Promise((resolve, reject) => {
      let lastAssistantMessage = null;
      session.on("assistant.message", (event) => {
        lastAssistantMessage = event.data;
        debug("Assistant message:", event.data.content);
      });
      session.on("session.idle", () => {
        debug("Session became idle");
        resolve();
      });
      session.on("session.error", (event) => {
        debug("Session error:", event.data);
        reject(new Error(event.data.message || "Session error"));
      });
    });
    debug("Sending prompt");
    await session.send({ prompt });
    logEvent("prompt.sent", { prompt }, session.sessionId);
    debug("Waiting for session to complete");
    await done;
    debug("Session completed successfully");
    logEvent("session.completed", {}, session.sessionId);
  } catch (error) {
    const errorMessage = error instanceof Error ? error.message : String(error);
    debug("Error during session:", errorMessage);
    logEvent("session.error", { error: errorMessage }, session?.sessionId);
    throw error;
  } finally {
    if (session) {
      debug("Destroying session");
      try {
        await session.destroy();
        logEvent("session.destroyed", {}, session.sessionId);
      } catch (error) {
        debug("Error destroying session:", error);
      }
    }
    debug("Stopping client");
    try {
      const errors = await client.stop();
      if (errors.length > 0) {
        debug("Errors during client stop:", errors);
        logEvent("client.stopped", { errors: errors.map((e) => e.message) });
      } else {
        logEvent("client.stopped", {});
      }
    } catch (error) {
      debug("Error stopping client:", error);
    }
  }
}
async function main() {
  debug("Reading configuration from stdin");
  const stdinBuffer = [];
  await new Promise((resolve, reject) => {
    process.stdin.on("data", (chunk) => {
      stdinBuffer.push(chunk);
    });
    process.stdin.on("end", () => {
      resolve();
    });
    process.stdin.on("error", (error) => {
      reject(error);
    });
  });
  const configJson = Buffer.concat(stdinBuffer).toString("utf-8");
  debug("Received config:", configJson);
  let config;
  try {
    config = JSON.parse(configJson);
  } catch (error) {
    console.error("Failed to parse configuration JSON:", error);
    process.exit(1);
  }
  debug("Parsed config:", config);
  try {
    await runCopilotSession(config);
    debug("Session completed successfully");
    process.exit(0);
  } catch (error) {
    console.error("Error running Copilot session:", error);
    process.exit(1);
  }
}

// src/cli.ts
main().catch((error) => {
  console.error("Fatal error:", error);
  process.exit(1);
});
//# sourceMappingURL=cli.js.map
//# sourceMappingURL=cli.js.map