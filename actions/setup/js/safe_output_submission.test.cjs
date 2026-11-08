import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { addDependencySchema, createSubmissionTracker } from "./safe_output_submission.cjs";
import { createHandlers } from "./safe_outputs_handlers.cjs";
import { validateItem } from "./safe_output_type_validator.cjs";
import { processMessages } from "./safe_output_handler_manager.cjs";

const outputIdPattern = /^so_[A-Za-z0-9_-]{12}$/;
const firstDependency = "so_AAAAAAAAAAAA";
const secondDependency = "so_BBBBBBBBBBBB";
const success = () => ({ content: [{ type: "text", text: JSON.stringify({ result: "success" }) }] });
const payload = response => JSON.parse(response.content[0].text);

describe("safe-output submission tracker", () => {
  it("injects the dependency schema idempotently and rejects reserved input names", () => {
    const tool = { name: "custom", inputSchema: { type: "object", properties: { body: { type: "string" } } } };
    addDependencySchema(tool);
    const dependencySchema = tool.inputSchema.properties["depends-on"];
    addDependencySchema(tool);
    expect(tool.inputSchema.properties["depends-on"]).toEqual(dependencySchema);
    expect(dependencySchema.description).toContain("Do not use ledger output IDs as prerequisites");
    expect(tool.inputSchema.required || []).not.toContain("depends-on");

    for (const reservedName of ["depends-on", "depends_on", "output_id"]) {
      expect(() => addDependencySchema({ name: "conflicting", inputSchema: { properties: { [reservedName]: { type: "string" } } } })).toThrow(/reserved input/);
    }
  });

  it("returns exactly the IDs queued by distinct calls", () => {
    const tracker = createSubmissionTracker();
    const queued = [];
    const handler = tracker.wrap(args => {
      tracker.append({ type: "custom", body: args.body }, entry => queued.push(entry));
      return success();
    });

    const first = payload(handler({ body: "first" }));
    const second = payload(handler({ body: "second" }));
    expect(first).toEqual({ result: "success", output_id: expect.stringMatching(outputIdPattern) });
    expect(second).toEqual({ result: "success", output_id: expect.stringMatching(outputIdPattern) });
    expect(second.output_id).not.toBe(first.output_id);
    expect(queued).toEqual([
      { type: "custom", body: "first", output_id: first.output_id },
      { type: "custom", body: "second", output_id: second.output_id },
    ]);
  });

  it("returns matching text, structured-content and queued IDs", () => {
    const tracker = createSubmissionTracker();
    const queued = [];
    const handler = tracker.wrap(args => {
      tracker.append({ type: "custom_create", ...args }, entry => queued.push(entry));
      const result = { result: "success", temporary_id: "aw_12345678" };
      return { content: [{ type: "text", text: JSON.stringify(result) }], structuredContent: result };
    });
    const response = handler({ temporary_id: "aw_12345678" });
    const result = payload(response);
    expect(result).toEqual({ result: "success", temporary_id: "aw_12345678", output_id: expect.stringMatching(outputIdPattern) });
    expect(response.structuredContent).toEqual(result);
    expect(queued).toEqual([{ type: "custom_create", temporary_id: result.temporary_id, output_id: result.output_id }]);
  });

  it("reports all queued IDs for a multi-output call", () => {
    const tracker = createSubmissionTracker();
    const queued = [];
    const handler = tracker.wrap(() => {
      tracker.append({ type: "custom", body: "first" }, entry => queued.push(entry));
      tracker.append({ type: "custom", body: "second" }, entry => queued.push(entry));
      return success();
    });

    const response = payload(handler({}));
    expect(response).toEqual({ result: "success", output_id: queued[0].output_id, output_ids: queued.map(entry => entry.output_id) });
    expect(response.output_ids).toEqual([expect.stringMatching(outputIdPattern), expect.stringMatching(outputIdPattern)]);
    expect(new Set(response.output_ids).size).toBe(2);
  });

  it("isolates IDs and dependencies when asynchronous calls complete out of order", async () => {
    const tracker = createSubmissionTracker();
    const queued = [];
    let releaseFirst;
    const firstReady = new Promise(resolve => {
      releaseFirst = resolve;
    });
    const handler = tracker.wrap(async args => {
      if (args.body === "first") await firstReady;
      tracker.append({ type: "custom", body: args.body }, entry => queued.push(entry));
      await Promise.resolve();
      return success();
    });

    const firstPending = handler({ body: "first", "depends-on": firstDependency });
    const second = payload(await handler({ body: "second", depends_on: [secondDependency] }));
    releaseFirst();
    const first = payload(await firstPending);
    expect(queued).toEqual([
      { type: "custom", body: "second", "depends-on": [secondDependency], output_id: second.output_id },
      { type: "custom", body: "first", "depends-on": firstDependency, output_id: first.output_id },
    ]);
    expect(first.output_id).toMatch(outputIdPattern);
    expect(second.output_id).toMatch(outputIdPattern);
    expect(first.output_id).not.toBe(second.output_id);
    expect(first).not.toHaveProperty("output_ids");
    expect(second).not.toHaveProperty("output_ids");
  });

  it("does not invent an ID for an error response even if a handler appended", () => {
    const tracker = createSubmissionTracker();
    const write = vi.fn();
    const response = { isError: true, content: [{ type: "text", text: JSON.stringify({ result: "error", error: "failed" }) }] };
    const handler = tracker.wrap(() => {
      tracker.append({ type: "custom" }, write);
      return response;
    });
    expect(handler({})).toBe(response);
    expect(payload(response)).not.toHaveProperty("output_id");
    expect(write).toHaveBeenCalledExactlyOnceWith({ type: "custom", output_id: expect.stringMatching(outputIdPattern) });
  });

  it("removes submission metadata from handler arguments without mutating caller input", () => {
    const tracker = createSubmissionTracker();
    const write = vi.fn();
    const handler = vi.fn(args => {
      tracker.append({ type: "custom", ...args }, write);
      return success();
    });
    const input = { body: "Actual content", output_id: secondDependency, depends_on: firstDependency };
    const response = payload(tracker.wrap(handler)(input));
    expect(handler).toHaveBeenCalledExactlyOnceWith({ body: "Actual content" });
    expect(input).toEqual({ body: "Actual content", output_id: secondDependency, depends_on: firstDependency });
    expect(response.output_id).toMatch(outputIdPattern);
    expect(response.output_id).not.toBe(secondDependency);
    expect(write).toHaveBeenCalledExactlyOnceWith({ type: "custom", body: "Actual content", output_id: response.output_id, "depends-on": firstDependency });
  });

  it("rejects dependencies for delegated outputs before invoking the handler", () => {
    const tracker = createSubmissionTracker();
    const handler = vi.fn(success);
    const response = tracker.wrap(handler, false)({ "depends-on": firstDependency });
    expect(response.isError).toBe(true);
    expect(payload(response)).toEqual({ result: "error", error: expect.stringContaining("depends-on is not supported") });
    expect(handler).not.toHaveBeenCalled();
  });
});

describe("safe-output submission handler integration", () => {
  let savedCore;
  let savedContext;
  let append;
  let handlers;

  beforeEach(() => {
    savedCore = global.core;
    savedContext = global.context;
    global.core = { debug: vi.fn(), info: vi.fn(), notice: vi.fn(), warning: vi.fn(), error: vi.fn(), setFailed: vi.fn(), setOutput: vi.fn() };
    global.context = { repo: { owner: "owner", repo: "repo" }, eventName: "push", payload: {} };
    append = vi.fn();
    handlers = createHandlers({ debug: vi.fn() }, append, { "update-issue": { target: "*" } });
  });

  afterEach(() => {
    global.core = savedCore;
    global.context = savedContext;
  });

  it.each([{ "depends-on": firstDependency }, { "depends-on": [firstDependency, secondDependency] }, { depends_on: firstDependency }, { depends_on: [firstDependency, secondDependency] }])(
    "preserves dependency metadata through handler argument transformations: %j",
    args => {
      const response = payload(handlers.updateIssueHandler({ issue_number: 42, title: "Updated", ...args }));
      expect(response).toEqual({ result: "success", output_id: expect.stringMatching(outputIdPattern) });
      expect(append).toHaveBeenCalledExactlyOnceWith({
        type: "update_issue",
        issue_number: 42,
        title: "Updated",
        "depends-on": args["depends-on"] ?? args.depends_on,
        output_id: response.output_id,
      });
    }
  );

  it.each([
    { "depends-on": "" },
    { "depends-on": "#aw_12345678" },
    { "depends-on": [] },
    { "depends-on": [firstDependency, firstDependency] },
    { "depends-on": [firstDependency, null] },
    { "depends-on": 42 },
    { "depends-on": null },
    { "depends-on": { output_id: firstDependency } },
    { depends_on: "invalid" },
    { "depends-on": firstDependency, depends_on: firstDependency },
  ])("rejects invalid dependencies before appending: %j", args => {
    const response = handlers.defaultHandler("custom")({ body: "Real content", ...args });
    expect(response.isError).toBe(true);
    expect(payload(response)).toEqual({ result: "error", error: expect.stringContaining("depends-on") });
    expect(append).not.toHaveBeenCalled();
  });

  it("does not let a custom output_id field forge the identifier", () => {
    const response = payload(handlers.defaultHandler("custom")({ body: "Real content", output_id: firstDependency }));
    expect(response).toEqual({ result: "success", output_id: expect.stringMatching(outputIdPattern) });
    expect(response.output_id).not.toBe(firstDependency);
    expect(append).toHaveBeenCalledExactlyOnceWith({ type: "custom", body: "Real content", output_id: response.output_id });
  });

  it.each([{ "depends-on": firstDependency }, { depends_on: [firstDependency, secondDependency] }])("accepts dependency metadata on strict built-in ledger inputs: %j", args => {
    const ledgerHandlers = createHandlers({ debug: vi.fn() }, append, {
      ledger_append: { ledgers: [{ name: "cache", type: "map" }] },
    });
    const response = ledgerHandlers.ledgerBuiltinHandler("map", "put")({ key: "status", value: { ready: true }, temp_id: "entry", ...args });
    expect(response.isError).not.toBe(true);
    const result = payload(response);
    expect(result).toEqual({ result: "success", output_id: expect.stringMatching(outputIdPattern) });
    expect(append).toHaveBeenCalledExactlyOnceWith({
      type: "ledger_append",
      ledger: "cache",
      operation: "put",
      key: "status",
      value: { ready: true },
      temp_id: "entry",
      "depends-on": args["depends-on"] ?? args.depends_on,
      output_id: result.output_id,
    });
  });

  it("rejects dependencies on generated custom jobs before append without disabling ordinary calls", () => {
    const customHandlers = createHandlers({ debug: vi.fn() }, append, { publish_report: { supports_dependencies: false } });
    const handler = customHandlers.defaultHandler("publish_report");
    const rejected = handler({ body: "Actual findings", "depends-on": firstDependency });
    expect(rejected.isError).toBe(true);
    expect(payload(rejected)).toEqual({ result: "error", error: expect.stringContaining("depends-on is not supported") });
    expect(append).not.toHaveBeenCalled();

    const accepted = payload(handler({ body: "Actual findings" }));
    expect(accepted).toEqual({ result: "success", output_id: expect.stringMatching(outputIdPattern) });
    expect(append).toHaveBeenCalledExactlyOnceWith({ type: "publish_report", body: "Actual findings", output_id: accepted.output_id });
  });

  it.each(["call_workflow", "push_repo_memory", "report_incomplete", "report_missing_tool", "report_missing_data"])("rejects dependencies for standalone %s handlers before execution", type => {
    const handler = handlers.defaultHandler(type);
    const response = handler({ "depends-on": firstDependency });
    expect(response.isError).toBe(true);
    expect(payload(response)).toMatchObject({ result: "error", error: expect.stringContaining("depends-on is not supported") });
    expect(append).not.toHaveBeenCalled();
  });

  it("rejects dependencies for push_repo_memory before staging memory files", () => {
    const response = handlers.pushRepoMemoryHandler({ "depends-on": firstDependency });
    expect(response.isError).toBe(true);
    expect(payload(response)).toMatchObject({ result: "error", error: expect.stringContaining("depends-on is not supported") });
    expect(append).not.toHaveBeenCalled();
  });

  it("rejects dependencies for real noop and upload_asset handlers before processing", () => {
    const noop = handlers.defaultHandler("noop")({ "depends-on": firstDependency });
    const asset = handlers.uploadAssetHandler({ path: "/tmp/image.png", "depends-on": firstDependency });
    expect(noop.isError).toBe(true);
    expect(asset.isError).toBe(true);
    expect(append).not.toHaveBeenCalled();
  });

  it("returns matching text and queued IDs for connector creation proposals", () => {
    const connectorHandlers = createHandlers({ debug: vi.fn() }, append, { jira_create_issue: { temporary_id_field: "temporary_id" } });
    const response = connectorHandlers.defaultHandler("jira_create_issue")({ project: "ENG", issue_type: "Task", summary: "Meaningful issue" });
    const result = payload(response);
    expect(result).toEqual({
      result: "success",
      temporary_id: expect.stringMatching(/^aw_[A-Za-z0-9]{8}$/),
      output_id: expect.stringMatching(outputIdPattern),
    });
    expect(append).toHaveBeenCalledExactlyOnceWith({
      type: "jira_create_issue",
      project: "ENG",
      issue_type: "Task",
      summary: "Meaningful issue",
      temporary_id: result.temporary_id,
      output_id: result.output_id,
    });
  });

  it("keeps the temporary resource ID independent of the output ID", () => {
    const response = payload(handlers.createIssueHandler({ title: "Meaningful issue", body: "Actual findings", temporary_id: "#aw_12345678" }));
    expect(response).toEqual({
      result: "success",
      output_id: expect.stringMatching(outputIdPattern),
    });
    expect(append).toHaveBeenCalledExactlyOnceWith({
      type: "create_issue",
      title: "Meaningful issue",
      body: "Actual findings",
      temporary_id: "#aw_12345678",
      output_id: response.output_id,
    });
  });

  it("returns no ID for rejected calls or ignored probes", () => {
    const error = handlers.defaultHandler("custom")({ data: { disabled: true } });
    expect(error.isError).toBe(true);
    expect(payload(error)).not.toHaveProperty("output_id");
    expect(payload(error)).not.toHaveProperty("output_ids");
    const probe = handlers.defaultHandler("noop")({ message: "test" });
    expect(probe.isError).not.toBe(true);
    expect(payload(probe)).toEqual({ result: "success" });
    expect(append).not.toHaveBeenCalled();
  });

  it.each([false, true])("preserves submission IDs and enforces dependencies through validation and processing (prerequisite succeeds: %s)", async prerequisiteSucceeds => {
    const prerequisite = payload(handlers.updateIssueHandler({ issue_number: 42, title: "Completed analysis" }));
    const dependent = payload(handlers.defaultHandler("add_labels")({ item_number: 42, labels: ["ready"], depends_on: prerequisite.output_id }));
    expect(prerequisite.output_id).toMatch(outputIdPattern);
    expect(dependent.output_id).toMatch(outputIdPattern);
    expect(dependent.output_id).not.toBe(prerequisite.output_id);

    const messages = append.mock.calls.map(([entry], index) => {
      const validated = validateItem(entry, entry.type, index + 1);
      expect(validated.isValid).toBe(true);
      return validated.normalizedItem;
    });
    expect(messages).toEqual([
      { type: "update_issue", issue_number: 42, title: "Completed analysis", output_id: prerequisite.output_id },
      { type: "add_labels", item_number: 42, labels: ["ready"], "depends-on": prerequisite.output_id, output_id: dependent.output_id },
    ]);

    const prerequisiteHandler = vi.fn().mockResolvedValue(prerequisiteSucceeds ? { success: true } : { success: false, error: "Prerequisite rejected" });
    const dependentHandler = vi.fn().mockResolvedValue({ success: true });
    const processed = await processMessages(
      new Map([
        ["update_issue", prerequisiteHandler],
        ["add_labels", dependentHandler],
      ]),
      messages
    );

    expect(prerequisiteHandler).toHaveBeenCalledTimes(1);
    expect(prerequisiteHandler.mock.calls[0][0]).toEqual(messages[0]);
    expect(processed.results).toHaveLength(2);
    expect(processed.results[0]).toMatchObject({ messageIndex: 0, success: prerequisiteSucceeds, output_id: prerequisite.output_id });
    expect(processed.results[1]).toMatchObject({ messageIndex: 1, success: prerequisiteSucceeds, output_id: dependent.output_id });
    if (prerequisiteSucceeds) {
      expect(dependentHandler).toHaveBeenCalledTimes(1);
      expect(dependentHandler.mock.calls[0][0]).toEqual(messages[1]);
    } else {
      expect(dependentHandler).not.toHaveBeenCalled();
      expect(processed.results[0].error).toBe("Prerequisite rejected");
      expect(processed.results[1]).toMatchObject({ errorCode: "safe_output_dependency_failed" });
    }
  });
});
