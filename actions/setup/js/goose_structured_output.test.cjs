import { describe, expect, it } from "vitest";
import http from "node:http";
import { createGooseStructuredOutputRecipe, createGooseStructuredOutputCollector, startGooseStructuredOutputProxy } from "./goose_structured_output.cjs";

const value = { summary: "Done", passed: true };
const message = (role, content) => ({ type: "message", message: { role, content } });
const request = (id = "native-output", argumentsValue = value) => message("assistant", [{ type: "toolRequest", id, toolCall: { status: "success", value: { name: "recipe__final_output", arguments: argumentsValue } } }]);
const response = (id = "native-output") => message("user", [{ type: "toolResponse", id, toolResult: { status: "success", value: { content: [{ type: "text", text: "Final output successfully collected." }] } } }]);
const text = result => message("assistant", [{ type: "text", text: JSON.stringify(result) }]);
const complete = { type: "complete" };
const collect = events => {
  const collector = createGooseStructuredOutputCollector();
  for (const event of events) collector.onStdoutLine(typeof event === "string" ? event : JSON.stringify(event));
  return collector.result();
};

describe("Goose native structured output", () => {
  it("passes the schema and prompt through a native response recipe with gh-aw's default draft", () => {
    const schema = { type: "object", description: "Literal {{ name }} and {% include 'file' %}", properties: { summary: { type: "string" } } };
    const prompt = 'Use {{ value }} literally, including {% endraw %} and quotes: "hello".\n${{ not_code }}';
    const encoded = createGooseStructuredOutputRecipe(schema, prompt);
    expect(encoded).not.toContain("{{");
    expect(encoded).not.toContain("{%");
    expect(JSON.parse(encoded)).toMatchObject({ prompt, response: { json_schema: schema } });
    expect(JSON.parse(encoded).response.json_schema.$schema).toBe("http://json-schema.org/draft-07/schema#");
    expect(schema).not.toHaveProperty("$schema");
  });

  it("preserves optional properties and open objects instead of adding strict-provider restrictions", () => {
    const schema = { type: "object", properties: { optional: { type: "string" } } };
    const nativeSchema = JSON.parse(createGooseStructuredOutputRecipe(schema, "Return an object")).response.json_schema;
    expect(nativeSchema).not.toHaveProperty("required");
    expect(nativeSchema).not.toHaveProperty("additionalProperties");
  });

  it("allows native draft-2020-12 local references and tuple schemas without format constraints", () => {
    const schema = {
      $schema: "https://json-schema.org/draft/2020-12/schema",
      type: "object",
      $defs: { name: { type: "string" } },
      properties: { name: { $ref: "#/$defs/name" }, pair: { type: "array", prefixItems: [{ type: "string" }, { type: "number" }] } },
    };
    expect(JSON.parse(createGooseStructuredOutputRecipe(schema, "Return an object")).response.json_schema).toEqual(schema);
  });

  it("rejects modern format assertions that Goose's native validator cannot enforce", () => {
    for (const subschema of [{ properties: { email: { type: "string", format: "email" } } }, { $defs: { email: { type: "string", format: "email" } } }, { allOf: [{ properties: { email: { type: "string", format: "email" } } }] }]) {
      expect(() => createGooseStructuredOutputRecipe({ $schema: "https://json-schema.org/draft/2020-12/schema", type: "object", ...subschema }, "Return an object")).toThrow("cannot natively enforce draft-2020-12 format constraints");
    }
    expect(() => createGooseStructuredOutputRecipe({ type: "object", properties: { email: { type: "string", format: "email" } } }, "Return an object")).not.toThrow();
  });

  it("does not mistake format-named properties or examples for format assertions", () => {
    const schema = { $schema: "https://json-schema.org/draft/2020-12/schema", type: "object", properties: { format: { type: "string" } }, examples: [{ format: "json" }] };
    expect(() => createGooseStructuredOutputRecipe(schema, "Return an object")).not.toThrow();
  });

  describe("Goose native correction transport budget", () => {
    const nativeFailure = (id, name = "recipe__final_output") => [
      { role: "assistant", tool_calls: [{ id, type: "function", function: { name, arguments: '{"summary":42}' } }] },
      { role: "tool", tool_call_id: id, content: "Validation failed:\n- /summary: expected string" },
    ];
    const withProxy = async run => {
      const requests = [];
      const upstream = http.createServer(async (request, response) => {
        let body = "";
        for await (const chunk of request) body += chunk;
        requests.push({ path: request.url, authorization: request.headers.authorization, body });
        response.writeHead(200, { "Content-Type": "text/event-stream" });
        response.end("data: unchanged-native-response\n\ndata: [DONE]\n\n");
      });
      await new Promise(resolve => upstream.listen(0, "127.0.0.1", resolve));
      const proxy = await startGooseStructuredOutputProxy(`http://127.0.0.1:${upstream.address().port}`);
      try {
        await run(proxy, requests);
      } finally {
        await proxy.close();
        await new Promise(resolve => upstream.close(resolve));
      }
    };
    const send = (proxy, messages) =>
      fetch(proxy.host + "/v1/chat/completions", {
        method: "POST",
        headers: { "Content-Type": "application/json", Authorization: "Bearer test-only" },
        body: JSON.stringify({ messages }),
      });

    it("forwards native requests, authorization, paths, and streaming bytes unchanged", async () => {
      await withProxy(async (proxy, requests) => {
        const messages = [{ role: "user", content: "Unmodified {{ prompt }}" }];
        const response = await send(proxy, messages);
        expect(response.status).toBe(200);
        expect(await response.text()).toBe("data: unchanged-native-response\n\ndata: [DONE]\n\n");
        expect(requests).toEqual([{ path: "/v1/chat/completions", authorization: "Bearer test-only", body: JSON.stringify({ messages }) }]);
      });
    });

    it("allows one native correction and independent transport retries, but prevents a second correction from reaching inference", async () => {
      await withProxy(async (proxy, requests) => {
        for (const messages of [[], nativeFailure("first"), nativeFailure("first")]) {
          const response = await send(proxy, messages);
          expect(response.status).toBe(200);
          await response.text();
        }
        expect(proxy.hasExhaustedCorrection()).toBe(false);
        const response = await send(proxy, [...nativeFailure("first"), ...nativeFailure("second")]);
        expect(response.status).toBe(400);
        expect(await response.text()).toContain("failed after one correction attempt");
        expect(proxy.hasExhaustedCorrection()).toBe(true);
        expect(requests).toHaveLength(3);
      });
    });

    it("does not spend the primary schema budget on another tool's failures", async () => {
      await withProxy(async (proxy, requests) => {
        const response = await send(proxy, [...nativeFailure("first", "probe"), ...nativeFailure("second", "probe")]);
        expect(response.status).toBe(200);
        await response.text();
        expect(proxy.hasExhaustedCorrection()).toBe(false);
        expect(requests).toHaveLength(1);
      });
    });
  });

  it("extracts only the completed runtime-validated final assistant response", () => {
    expect(collect(["infrastructure", "{malformed", request(), response(), text(value), complete])).toEqual(value);
    expect(collect([request(), response(), text({ passed: true, summary: "Done" }), complete])).toEqual(value);
  });

  it.each([
    ["JSON-looking text only", [text(value), complete]],
    ["unanswered native request", [request(), text(value), complete]],
    ["unrelated tool response", [request(), response("other-call"), text(value), complete]],
    ["missing final assistant message", [request(), response(), complete]],
    ["incomplete run", [request(), response(), text(value)]],
    ["terminal error", [request(), response(), text(value), { type: "error", error: "Refused" }, complete]],
    ["failed native validation", [request(), message("user", [{ type: "toolResponse", id: "native-output", toolResult: { status: "error", error: "Validation failed" } }]), text(value), complete]],
    [
      "isError native result",
      [
        request(),
        message("user", [{ type: "toolResponse", id: "native-output", toolResult: { status: "success", value: { isError: true, content: [{ type: "text", text: "Final output successfully collected." }] } } }]),
        text(value),
        complete,
      ],
    ],
  ])("rejects %s", (_name, events) => {
    expect(() => collect(events)).toThrow("Goose did not complete a native schema-validated final output");
  });

  it("rejects final text that differs from the accepted native output", () => {
    expect(() => collect([request(), response(), text({ summary: "invented", passed: true }), complete])).toThrow("does not match");
  });

  it("rejects malformed final JSON without leaking the response", () => {
    expect(() => collect([request(), response(), message("assistant", [{ type: "text", text: "PRIVATE_VALUE" }]), complete])).toThrow("not valid JSON");
    expect(() => collect([request(), response(), message("assistant", [{ type: "text", text: "PRIVATE_VALUE" }]), complete])).not.toThrow("PRIVATE_VALUE");
  });

  it("uses the latest successfully validated native output after a retry", () => {
    const corrected = { summary: "Corrected", passed: true };
    expect(collect([request(), response(), text(value), request("second", corrected), response("second"), text(corrected), complete])).toEqual(corrected);
  });

  it("allows one native schema correction and stops after the second mismatch", () => {
    const collector = createGooseStructuredOutputCollector();
    const invalid = id => message("user", [{ type: "toolResponse", id, toolResult: { status: "error", error: { code: -32602, message: "Validation failed:\n- /summary: expected string" } } }]);
    for (const event of [request("first"), invalid("first"), invalid("first")]) {
      collector.onStdoutLine(JSON.stringify(event));
    }
    expect(collector.hasExhaustedCorrection()).toBe(false);
    for (const event of [request("second"), invalid("second")]) {
      collector.onStdoutLine(JSON.stringify(event));
    }
    expect(collector.hasExhaustedCorrection()).toBe(true);
    expect(() => collector.result()).toThrow("failed after one correction attempt");
  });

  it("does not spend the schema correction budget on unrelated tool errors", () => {
    const collector = createGooseStructuredOutputCollector();
    for (const id of ["first", "second"]) {
      collector.onStdoutLine(JSON.stringify(request(id)));
      collector.onStdoutLine(JSON.stringify(message("user", [{ type: "toolResponse", id, toolResult: { status: "error", error: { code: -32600, message: "Tool execution denied" } } }])));
    }
    expect(collector.hasExhaustedCorrection()).toBe(false);
  });
});
