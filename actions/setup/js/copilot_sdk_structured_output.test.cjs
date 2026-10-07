import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "module";
import * as fs from "fs";
import * as path from "path";
import { randomUUID } from "crypto";
import { spawnSync } from "child_process";

const require = createRequire(import.meta.url);
const { runWithCopilotSDK } = require("./copilot_sdk_session.cjs");
const { shouldRetryFailedExecution } = require("./copilot_harness.cjs");
const schema = {
  type: "object",
  properties: { answer: { type: "integer" } },
  required: ["answer"],
  additionalProperties: false,
};

describe("Copilot SDK native structured output", () => {
  let directory;
  let outputPath;

  beforeEach(() => {
    directory = path.join(process.cwd(), `.copilot-structured-output-test-${randomUUID()}`);
    fs.mkdirSync(directory);
    outputPath = path.join(directory, "response.json");
    vi.spyOn(process.stderr, "write").mockImplementation(() => true);
  });

  afterEach(() => {
    vi.unstubAllEnvs();
    vi.restoreAllMocks();
    fs.rmSync(directory, { recursive: true, force: true });
  });

  function fakeSDK(replies, events = []) {
    let onEvent = () => {};
    const session = {
      sessionId: "structured-session",
      on: handler => {
        onEvent = handler;
      },
      sendAndWait: vi.fn(async () => {
        for (const event of events) onEvent(event);
        const reply = replies.shift();
        if (reply instanceof Error) throw reply;
        return reply;
      }),
      disconnect: vi.fn(async () => {}),
    };
    const client = {
      start: vi.fn(async () => {}),
      createSession: vi.fn(async () => session),
      stop: vi.fn(async () => {}),
    };
    class CopilotClient {
      constructor() {
        return client;
      }
    }
    return {
      session,
      client,
      options: {
        sdkUri: "http://127.0.0.1:3002",
        prompt: "Calculate 6 * 7",
        logger: () => {},
        sessionStateBaseDir: directory,
        structuredOutput: { schema, outputPath },
        sdkModule: { CopilotClient, RuntimeConnection: { forUri: () => ({}) }, approveAll: () => "allow" },
      },
    };
  }

  function reply(content, metadata = {}) {
    return { type: "assistant.message", data: { content }, ...metadata };
  }

  it("the pinned SDK forwards the schema as strict native JSON-RPC responseFormat", async () => {
    const { CopilotSession } = require("@github/copilot-sdk");
    const sendRequest = vi.fn().mockResolvedValue({ messageId: "submitted-message" });
    const session = new CopilotSession("native-contract", { sendRequest });
    await expect(session.send({ prompt: "Calculate 6 * 7", responseSchema: schema })).resolves.toBe("submitted-message");
    expect(sendRequest).toHaveBeenCalledWith(
      "session.send",
      expect.objectContaining({
        prompt: "Calculate 6 * 7",
        responseFormat: { type: "json_schema", jsonSchema: { name: "response", strict: true, schema } },
      })
    );
  });

  it("passes the raw schema natively and publishes only the correlated final root message", async () => {
    const sdk = fakeSDK([reply('{"answer":42}')], [reply("Progress: calculating"), reply('{"answer":99}', { agentId: "child" }), reply('{"answer":7}', { data: { content: '{"answer":7}', toolRequests: [{ toolCallId: "call" }] } })]);
    const result = await runWithCopilotSDK(sdk.options);
    expect(result.exitCode).toBe(0);
    expect(sdk.session.sendAndWait).toHaveBeenCalledExactlyOnceWith({ prompt: "Calculate 6 * 7", responseSchema: schema }, expect.any(Number));
    expect(JSON.parse(fs.readFileSync(outputPath, "utf8"))).toEqual({ answer: 42 });
    expect(sdk.client.createSession.mock.calls[0][0]).not.toHaveProperty("tools");
  });

  it("makes exactly one correction with the same native schema", async () => {
    const sdk = fakeSDK([reply('{"answer":"wrong"}'), reply('{"answer":42}')]);
    const result = await runWithCopilotSDK(sdk.options);
    expect(result.exitCode).toBe(0);
    expect(sdk.session.sendAndWait).toHaveBeenCalledTimes(2);
    expect(sdk.session.sendAndWait.mock.calls[1][0]).toMatchObject({ responseSchema: schema });
    expect(sdk.session.sendAndWait.mock.calls[1][0].prompt).toContain("Do not repeat completed work");
    expect(JSON.parse(fs.readFileSync(outputPath, "utf8"))).toEqual({ answer: 42 });
  });

  it.each([
    ['{"answer":"invalid"}', "schema-invalid values"],
    ['```json\n{"answer":42}\n```', "Markdown-wrapped JSON"],
    ['Progress\n{"answer":42}', "prose and embedded JSON"],
    ['{"answer":42}{"answer":43}', "concatenated messages"],
  ])("rejects %s after one correction (%s)", async content => {
    fs.writeFileSync(outputPath, '{"answer":123}');
    const sdk = fakeSDK([reply(content), reply(content)]);
    const result = await runWithCopilotSDK(sdk.options);
    expect(result.exitCode).toBe(65);
    expect(sdk.session.sendAndWait).toHaveBeenCalledTimes(2);
    expect(fs.existsSync(outputPath)).toBe(false);
    vi.stubEnv("GH_AW_STRUCTURED_OUTPUT_SCHEMA_FILE", path.join(directory, "schema.json"));
    expect(shouldRetryFailedExecution({ ...result, attempt: 0, maxRetries: 3 })).toBe(false);
  });

  it.each([reply('{"answer":99}', { agentId: "child" }), { type: "assistant.message", data: { content: '{"answer":99}', toolRequests: [{ toolCallId: "call" }] } }, undefined])(
    "does not publish subagent, tool-request, or missing final responses",
    async response => {
      const sdk = fakeSDK([response, response]);
      const result = await runWithCopilotSDK(sdk.options);
      expect(result.exitCode).toBe(65);
      expect(fs.existsSync(outputPath)).toBe(false);
    }
  );

  it("does not rescue an SDK idle timeout just because intermediate output exists", async () => {
    const sdk = fakeSDK([new Error("Timeout after 1000ms waiting for session.idle")], [reply('{"answer":42}')]);
    const result = await runWithCopilotSDK(sdk.options);
    expect(result.exitCode).toBe(1);
    expect(fs.existsSync(outputPath)).toBe(false);
    expect(sdk.session.sendAndWait).toHaveBeenCalledTimes(1);
  });

  it("retains transport retries without consuming the schema correction budget", async () => {
    const failed = fakeSDK([new Error("connection reset by peer")], [reply("Work in progress")]);
    const failure = await runWithCopilotSDK(failed.options);
    expect(failure.exitCode).toBe(1);
    vi.stubEnv("GH_AW_STRUCTURED_OUTPUT_SCHEMA_FILE", path.join(directory, "schema.json"));
    expect(shouldRetryFailedExecution({ ...failure, attempt: 0, maxRetries: 3 })).toBe(true);
    const recovered = fakeSDK([reply('{"answer":"invalid"}'), reply('{"answer":42}')]);
    expect((await runWithCopilotSDK(recovered.options)).exitCode).toBe(0);
    expect(recovered.session.sendAndWait).toHaveBeenCalledTimes(2);
  });

  it("never grants a second schema correction after a correction transport failure", async () => {
    const first = fakeSDK([reply('{"answer":"invalid"}'), new Error("connection reset by peer")], [reply("Work in progress")]);
    const failure = await runWithCopilotSDK(first.options);
    expect(failure.exitCode).toBe(1);
    expect(first.session.sendAndWait).toHaveBeenCalledTimes(2);
    vi.stubEnv("GH_AW_STRUCTURED_OUTPUT_SCHEMA_FILE", path.join(directory, "schema.json"));
    expect(shouldRetryFailedExecution({ ...failure, attempt: 0, maxRetries: 3 })).toBe(true);
    const second = fakeSDK([reply('{"answer":"invalid"}')]);
    expect((await runWithCopilotSDK(second.options)).exitCode).toBe(65);
    expect(second.session.sendAndWait).toHaveBeenCalledTimes(1);
    expect(fs.existsSync(outputPath)).toBe(false);
  });

  it("loads the compiler materialized schema from its separate file environment variable", async () => {
    const schemaPath = path.join(directory, "schema.json");
    fs.writeFileSync(schemaPath, JSON.stringify(schema));
    vi.stubEnv("GH_AW_STRUCTURED_OUTPUT_SCHEMA_FILE", schemaPath);
    vi.stubEnv("GH_AW_STRUCTURED_OUTPUT_FILE", outputPath);
    const sdk = fakeSDK([reply('{"answer":42}')]);
    delete sdk.options.structuredOutput;
    const result = await runWithCopilotSDK(sdk.options);
    expect(result.exitCode).toBe(0);
    expect(sdk.session.sendAndWait.mock.calls[0][0]).toMatchObject({ responseSchema: schema });
    expect(JSON.parse(fs.readFileSync(outputPath, "utf8"))).toEqual({ answer: 42 });
  });

  it("preserves ordinary SDK text execution without a schema or result file", async () => {
    const sdk = fakeSDK([reply("ordinary answer")]);
    delete sdk.options.structuredOutput;
    const result = await runWithCopilotSDK(sdk.options);
    expect(result.exitCode).toBe(0);
    expect(result.output).toBe("ordinary answer");
    expect(sdk.session.sendAndWait.mock.calls[0][0]).toEqual({ prompt: "Calculate 6 * 7" });
    expect(fs.existsSync(outputPath)).toBe(false);
  });

  it("the real harness treats exhausted structured correction as terminal", () => {
    const stubPath = path.join(directory, "driver.cjs");
    const invocationsPath = path.join(directory, "invocations");
    fs.writeFileSync(stubPath, "const fs = require('fs'); const p = process.env.INVOCATIONS_PATH; fs.appendFileSync(p, 'attempt\\n'); console.log('Structured output schema violation'); process.exit(65);");
    const result = spawnSync(process.execPath, [require.resolve("./copilot_harness.cjs"), process.execPath, stubPath], {
      encoding: "utf8",
      timeout: 10000,
      env: {
        ...process.env,
        GH_AW_COPILOT_SDK_DRIVER: "",
        COPILOT_SDK_URI: "",
        GH_AW_PROMPT: "",
        GITHUB_EVENT_NAME: "workflow_dispatch",
        GITHUB_WORKSPACE: directory,
        GH_AW_ENGINE_CWD: directory,
        GH_AW_STRUCTURED_OUTPUT_SCHEMA_FILE: path.join(directory, "schema.json"),
        INVOCATIONS_PATH: invocationsPath,
      },
    });
    expect(result.status).toBe(65);
    expect(result.stderr).toContain("not restarting the SDK session");
    expect(fs.readFileSync(invocationsPath, "utf8")).toBe("attempt\n");
  });

  it("the real harness retries transport errors even after a terminal safe-output", () => {
    const stubPath = path.join(directory, "transport-driver.cjs");
    const invocationsPath = path.join(directory, "transport-invocations");
    const safeOutputsPath = path.join(directory, "safe-outputs.jsonl");
    fs.writeFileSync(
      stubPath,
      [
        "const fs = require('fs');",
        "const p = process.env.INVOCATIONS_PATH;",
        "fs.appendFileSync(p, 'attempt\\n');",
        "const attempts = fs.readFileSync(p, 'utf8').trim().split('\\n').length;",
        "fs.writeFileSync(process.env.GH_AW_SAFE_OUTPUTS, JSON.stringify({type:'create_issue',data:{title:'Finished'}})+'\\n');",
        "if (attempts === 1) { console.log('Error: connection reset by peer'); process.exit(1); }",
        "fs.writeFileSync(process.env.GH_AW_STRUCTURED_OUTPUT_FILE, '{\"answer\":42}');",
        "console.log('completed');",
      ].join("\n")
    );
    const result = spawnSync(process.execPath, [require.resolve("./copilot_harness.cjs"), process.execPath, stubPath], {
      encoding: "utf8",
      timeout: 10000,
      env: {
        ...process.env,
        GH_AW_COPILOT_SDK_DRIVER: "",
        COPILOT_SDK_URI: "",
        GH_AW_PROMPT: "",
        GITHUB_EVENT_NAME: "workflow_dispatch",
        GITHUB_WORKSPACE: directory,
        GH_AW_ENGINE_CWD: directory,
        GH_AW_SAFE_OUTPUTS: safeOutputsPath,
        GH_AW_STRUCTURED_OUTPUT_SCHEMA_FILE: path.join(directory, "schema.json"),
        GH_AW_STRUCTURED_OUTPUT_FILE: outputPath,
        GH_AW_HARNESS_INITIAL_DELAY_MS: "1",
        GH_AW_HARNESS_MAX_DELAY_MS: "1",
        INVOCATIONS_PATH: invocationsPath,
      },
    });
    expect(result.status).toBe(0);
    expect(fs.readFileSync(invocationsPath, "utf8")).toBe("attempt\nattempt\n");
    expect(JSON.parse(fs.readFileSync(outputPath, "utf8"))).toEqual({ answer: 42 });
  });
});
