const fs = require("fs");
const cp = require("child_process");
const path = require("path");
const { performance } = require("perf_hooks");
const { computeGraders, BUILTIN_GRADERS, BUILTIN_META, DEFAULT_GRADER_IDS, createSessionLookups } = require("./trace_graders.cjs");
const { writeGraderArtifacts } = require("./grader_artifacts.cjs");
const { writeUnifiedSession } = require("./unified_session.cjs");
const { normalizeUnifiedSessionEvent } = require("./unified_session_payload.cjs");
const { FAILURE_CATEGORY_IDS, MAX_FAILURE_OBSERVATIONS, projectFailureDiagnostics } = require("./failure_graders.cjs");
const { generateSessionSchemas } = require("./scripts/generate_session_schemas.cjs");
const Ajv = require("ajv");

function event(type, data = {}, scope = {}) {
  const { path: source = "agent.jsonl", attemptId, startupAttemptId, ...context } = scope;
  return { type, data: { ...data, ...context }, provenance: { component: "agent", phase: "agent", path: source, index: 0, ...(attemptId ? { attemptId } : {}), ...(startupAttemptId ? { startupAttemptId } : {}) } };
}
function tool(id, success, scope = {}, input = { path: "file" }, name = "edit") {
  return [event("tool.execution_start", { toolCallId: id, toolName: name, input }, scope), ...(success === undefined ? [] : [event("tool.execution_complete", { toolCallId: id, success }, scope)])];
}
function grade(session, id) {
  const lookups = createSessionLookups(session);
  return BUILTIN_GRADERS[id](session, {}, lookups);
}
function objectives(satisfied, scope = {}) {
  return event("trajectory.ir", { objectives: [{ id: "goal", satisfiedAtEventIndex: satisfied ? 0 : null }] }, scope);
}
function freeze(value) {
  if (value && typeof value === "object") {
    Object.values(value).forEach(freeze);
    Object.freeze(value);
  }
  return value;
}

afterEach(() => vi.restoreAllMocks());

describe("conservative failure observations", () => {
  it("enables the four count metrics without changing the grader format version", () => {
    const computed = computeGraders([]);
    expect(computed.manifest.version).toBe(2);
    for (const id of FAILURE_CATEGORY_IDS) {
      expect(DEFAULT_GRADER_IDS).toContain(id);
      expect(BUILTIN_META[id]).toMatchObject({ unit: "count", direction: "lower_is_better", threshold: 0 });
      expect(computed.output.results.find(result => result.id === id)).toMatchObject({ value: null, status: "unavailable", diagnostics: { failureObservations: [] } });
    }
  });

  it("detects unsuccessful unchanged retries and stagnation, retaining recovered history", () => {
    const session = [...tool("a", false), ...tool("b", false), ...tool("c", false), ...tool("d", true)];
    const retries = grade(session, "ineffective-retries");
    expect(retries.value).toBe(2);
    expect(grade(session, "progress-stagnation").value).toBe(1);
    expect(retries.diagnostics.failureObservations.every(item => item.recoverability === "recovered")).toBe(true);
    expect(retries.diagnostics.failureObservations[0]).toMatchObject({
      schemaVersion: 1,
      categoryId: "ineffective-retries",
      severity: "warning",
      confidence: "medium",
      evidenceState: "observed",
      evidence: [
        { eventIndex: 1, type: "tool.execution_complete" },
        { eventIndex: 3, type: "tool.execution_complete" },
        { eventIndex: 7, type: "tool.execution_complete" },
      ],
    });
  });

  it("does not confuse legitimate repeated reads or identical successful writes with failure", () => {
    for (const name of ["read", "edit"]) {
      const session = Array.from({ length: 8 }, (_, index) => tool(String(index), true, {}, { path: "file" }, name)).flat();
      expect(grade(session, "progress-stagnation").value).toBe(0);
      expect(grade(session, "ineffective-retries").value).toBe(0);
    }
    const changed = [...tool("a", false), ...tool("b", false, {}, { path: "other" }), ...tool("c", false)];
    expect(grade(changed, "progress-stagnation").value).toBe(0);
    expect(grade(changed, "ineffective-retries").value).toBe(0);
    const evolving = Array.from({ length: 4 }, (_, index) => {
      const calls = tool(String(index), false);
      calls[1].data.error = `Different failure ${index}`;
      return calls;
    }).flat();
    expect(grade(evolving, "progress-stagnation").value).toBe(0);
    expect(grade(evolving, "ineffective-retries").value).toBe(0);
  });

  it("treats absent results and truncated inputs as unavailable, never confirmed failures", () => {
    const session = [...tool("a", undefined), ...tool("b", undefined), ...tool("c", undefined), event("session.result", { status: "success" })];
    for (const id of ["progress-stagnation", "ineffective-retries", "premature-termination"]) expect(grade(session, id).value).toBeNull();
    expect(grade(session, "premature-termination").diagnostics.failureObservations[0]).toMatchObject({ evidenceState: "missing", recoverability: "unknown" });
    const truncated = [...tool("a", false), ...tool("b", false), ...tool("c", false)];
    for (const entry of truncated) if (entry.type === "tool.execution_start") entry.data.inputTruncated = true;
    expect(grade(truncated, "progress-stagnation").value).toBeNull();
  });

  it("requires explicit unmet objective state at a scoped session boundary", () => {
    expect(grade([objectives(false), event("session.result")], "premature-termination").value).toBe(1);
    expect(grade([objectives(false), event("session.result"), event("session.shutdown")], "premature-termination").value).toBe(1);
    expect(grade([objectives(true), event("session.shutdown")], "premature-termination").value).toBe(0);
    expect(grade([event("session.result", { status: "error" })], "premature-termination").value).toBeNull();
    expect(grade([objectives(false)], "premature-termination").value).toBeNull();
    expect(grade([event("trajectory.ir", { objectives: [{ id: "goal" }] }), event("session.result")], "premature-termination").value).toBeNull();
    expect(grade([objectives(false, { agentId: "a" }), event("session.result", {}, { agentId: "b" })], "premature-termination").value).toBeNull();
  });

  it("interprets structured completion as a claim, not execution success", () => {
    const claim = event("session.task_complete", { success: true, summary: "PRIVATE_SUMMARY" });
    expect(grade([claim], "completion-misrepresentation").value).toBeNull();
    const session = [objectives(false), claim, objectives(true)];
    const result = grade(session, "completion-misrepresentation");
    expect(result.value).toBe(1);
    expect(result.diagnostics.failureObservations[0]).toMatchObject({ evidenceState: "observed", severity: "error", recoverability: "recovered" });
    expect(JSON.stringify(result)).not.toContain("PRIVATE_SUMMARY");
    expect(grade([objectives(true), claim], "completion-misrepresentation").value).toBe(0);
    expect(grade([objectives(false), event("session.task_complete", { success: false })], "completion-misrepresentation").value).toBeNull();
    const replaced = [objectives(false), claim, event("trajectory.ir", { objectives: [{ id: "unrelated", satisfiedAtEventIndex: 0 }] })];
    expect(grade(replaced, "completion-misrepresentation").diagnostics.failureObservations[0].recoverability).toBe("recoverable");
  });

  it("projects only safe bounded failure diagnostics from historical measurements", () => {
    const observation = grade([...tool("a", false), ...tool("b", false)], "ineffective-retries").diagnostics.failureObservations[0];
    const diagnostics = {
      private: "PRIVATE_DIAGNOSTIC",
      failureObservations: Array.from({ length: 50 }, () => ({ ...observation, summary: "PRIVATE_SUMMARY", content: "PRIVATE_CONTENT", evidence: observation.evidence.map(reference => ({ ...reference, output: "PRIVATE_OUTPUT" })) })),
      totalFailureObservations: 50,
      failureObservationsTruncated: false,
      coverage: { assessed: 50, missing: 0, scopeCount: 1, state: "complete", private: "PRIVATE_COVERAGE" },
    };
    const projected = projectFailureDiagnostics(diagnostics);
    expect(projected.failureObservations).toHaveLength(32);
    expect(projected.failureObservationsTruncated).toBe(true);
    expect(JSON.stringify(projected)).not.toContain("PRIVATE_");
    expect(projectFailureDiagnostics({ private: "PRIVATE" })).toBeUndefined();
    const normalized = normalizeUnifiedSessionEvent(
      {
        type: "grader.result",
        data: { results: [{ id: "ineffective-retries", diagnostics: { ...diagnostics, failureObservations: [observation] } }] },
      },
      "conclusion"
    );
    expect(normalized.data.results[0].diagnostics.failureObservations).toEqual([observation]);
    expect(JSON.stringify(normalized)).not.toContain("PRIVATE_");
    const derived = normalizeUnifiedSessionEvent({ type: "failure.observation", data: { ...observation, content: "PRIVATE_CONTENT" } }, "conclusion");
    expect(derived.data).toEqual(observation);
    expect(JSON.stringify(derived)).not.toContain("PRIVATE_CONTENT");
  });

  it("matches only narrow explicit prose and only scoped test results", () => {
    const tests = tool("a", false, {}, { command: "npm test" }, "bash");
    const claim = event("assistant.message", { content: "All tests passed." });
    expect(grade([...tests, claim], "completion-misrepresentation").value).toBe(1);
    expect(grade([claim], "completion-misrepresentation").value).toBeNull();
    expect(grade([...tool("a", true, {}, { command: "npm test" }, "bash"), claim], "completion-misrepresentation").value).toBe(0);
    expect(grade([...tool("a", false), claim], "completion-misrepresentation").value).toBeNull();
    expect(grade([...tool("a", true, {}, { command: "npm test || true" }, "bash"), claim], "completion-misrepresentation").value).toBeNull();
    for (const content of ["Done.", "If all tests passed, continue.", "The log says: All tests passed.", "All tests passed, except one.", "I will ensure all tests passed."]) {
      expect(grade([...tests, event("assistant.message", { content })], "completion-misrepresentation").value).toBeNull();
    }
    expect(grade([...tests, event("assistant.message", { content: "All tests passed.", partial: true })], "completion-misrepresentation").value).toBeNull();
    expect(grade([...tool("a", false, { agentId: "child" }, { command: "npm test" }, "bash"), claim], "completion-misrepresentation").value).toBeNull();
  });

  it("does not let unrelated successful tests erase unsuccessful test history", () => {
    const session = [...tool("a", false, {}, { command: "npm test first" }, "bash"), ...tool("b", true, {}, { command: "npm test second" }, "bash"), event("assistant.message", { content: "All tests passed." })];
    expect(grade(session, "completion-misrepresentation").value).toBe(1);
    expect(grade(session, "completion-misrepresentation").diagnostics.failureObservations[0].recoverability).toBe("recoverable");
    const recovered = [...session, ...tool("c", true, {}, { command: "npm test first" }, "bash")];
    expect(grade(recovered, "completion-misrepresentation").value).toBe(1);
    expect(grade(recovered, "completion-misrepresentation").diagnostics.failureObservations[0].recoverability).toBe("recovered");
  });

  it("separates agents, native sessions, anonymous source files and non-resumed attempts", () => {
    for (const [one, two] of [
      [{ sessionId: "one" }, { sessionId: "two" }],
      [{ agentId: "one" }, { agentId: "two" }],
      [{ path: "one" }, { path: "two" }],
      [
        { sessionId: "same", attemptId: "one" },
        { sessionId: "same", attemptId: "two" },
      ],
    ]) {
      const session = [...tool("a", false, one), ...tool("b", false, two), ...tool("c", false, one)];
      expect(grade(session, "progress-stagnation").value).toBe(0);
      expect(grade(session, "ineffective-retries").value).toBe(1);
    }
  });

  it("uses existing compatible-resume attempt scoping while preserving failed attempts", () => {
    const attempt = (number, strategy) => ({ type: "harness.attempt", data: { layer: "harness", attempt: number, attemptId: `harness:${number}`, strategy }, provenance: { component: "harness", phase: "agent", path: "retry.json" } });
    const session = [
      attempt(1, "fresh"),
      attempt(2, "resume"),
      ...tool("a", false, { sessionId: "same", attemptId: "harness:1" }),
      ...tool("b", false, { sessionId: "same", attemptId: "harness:2" }),
      ...tool("c", false, { sessionId: "same", attemptId: "harness:2" }),
      ...tool("d", true, { sessionId: "same", attemptId: "harness:2" }),
    ];
    expect(grade(session, "progress-stagnation").value).toBe(1);
    expect(grade(session, "ineffective-retries").value).toBe(2);
    expect(grade(session, "progress-stagnation").diagnostics.failureObservations[0].recoverability).toBe("recovered");
  });

  it("is deterministic with deeply frozen input and denied I/O", () => {
    const session = freeze([...tool("a", false), ...tool("b", false), ...tool("c", false), objectives(false), event("session.task_complete", { success: true }), event("session.result")]);
    const before = structuredClone(session);
    const expected = computeGraders(session);
    for (const name of ["readFileSync", "writeFileSync", "existsSync", "statSync"])
      vi.spyOn(fs, name).mockImplementation(() => {
        throw new Error("I/O denied");
      });
    vi.spyOn(cp, "spawnSync").mockImplementation(() => {
      throw new Error("subprocess denied");
    });
    expect(computeGraders(session)).toEqual(expected);
    expect(session).toEqual(before);
    vi.restoreAllMocks();
    const source = fs.readFileSync(path.join(__dirname, "failure_graders.cjs"), "utf8");
    expect([...source.matchAll(/require\("([^"]+)"\)/g)].map(match => match[1])).toEqual(["./grader_session.cjs"]);
    expect(source).not.toMatch(/\b(?:process|fetch|Date)\b|\bMath\.random\b/);
  });

  it("bounds observations and linear runtime on large sessions", () => {
    const session = Array.from({ length: 12000 }, (_, index) => tool(String(index), false)).flat();
    const started = performance.now();
    const computed = computeGraders(session);
    expect(performance.now() - started).toBeLessThan(5000);
    for (const id of ["progress-stagnation", "ineffective-retries"]) {
      const result = computed.output.results.find(result => result.id === id);
      expect(result.diagnostics.failureObservations).toHaveLength(MAX_FAILURE_OBSERVATIONS);
      expect(result.diagnostics.failureObservationsTruncated).toBe(true);
      expect(result.value).toBe(id === "progress-stagnation" ? 11998 : 11999);
      expect(result.diagnostics.totalFailureObservations).toBe(result.value);
    }
  });

  it("publishes optional typed derived events and preserves diagnostics without duplicates", async () => {
    const root = path.join(__dirname, `.failure-observations-${process.pid}`);
    fs.mkdirSync(root, { recursive: true });
    try {
      const session = [...tool("a", false), ...tool("b", false), ...tool("c", false)];
      const first = writeGraderArtifacts(session, root, []);
      const second = writeGraderArtifacts(first, root, []);
      expect(second).toEqual(first);
      const generated = entry => entry.type.startsWith("grader.") || entry.type === "failure.observation";
      const interleaved = [first[0], ...first.filter(generated), ...first.slice(1).filter(entry => !generated(entry))];
      const regraded = writeGraderArtifacts(interleaved, root, []);
      expect(regraded).toEqual(first);
      for (const entry of regraded.filter(entry => entry.type === "failure.observation")) {
        for (const reference of entry.data.evidence) {
          expect(regraded[reference.eventIndex].type).toBe(reference.type);
          expect(regraded[reference.eventIndex].provenance).toMatchObject(reference.provenance);
        }
      }
      expect(first.filter(entry => entry.type === "failure.observation")).toHaveLength(3);
      expect(first.find(entry => entry.type === "grader.result").data.results.find(result => result.id === "progress-stagnation").diagnostics.failureObservations).toHaveLength(1);
      const schemas = await generateSessionSchemas();
      const schema = JSON.parse(schemas["unified-session.schema.json"]);
      const validate = new Ajv({ strict: false }).compile({ ...schema.items, definitions: schema.definitions });
      for (const entry of first) {
        expect(validate(entry), JSON.stringify(validate.errors)).toBe(true);
      }
      expect(validate({ ...first.at(-1), data: { ...first.at(-1).data, schemaVersion: 2 } })).toBe(false);
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  it("derives from the same redacted session at the real conclusion boundary", () => {
    const root = path.join(__dirname, `.failure-conclusion-${process.pid}`);
    fs.mkdirSync(root, { recursive: true });
    try {
      const session = [...tool("a", false), ...tool("b", false), ...tool("c", false)];
      fs.writeFileSync(path.join(root, "agent-session.jsonl"), session.map(entry => JSON.stringify({ ...entry, data: { ...entry.data, privateField: "PRIVATE_SECRET" } })).join("\n"));
      const outputPath = path.join(root, "usage/aw_session.jsonl");
      const first = writeUnifiedSession({ rootDir: root, outputPath, graders: [], warn: () => {} });
      const second = writeUnifiedSession({ rootDir: root, outputPath, graders: [], warn: () => {} });
      expect(first.filter(entry => entry.type === "failure.observation")).toHaveLength(3);
      expect(second.filter(entry => entry.type === "failure.observation")).toEqual(first.filter(entry => entry.type === "failure.observation"));
      expect(JSON.stringify(first.filter(entry => entry.type === "failure.observation"))).not.toContain("PRIVATE_SECRET");
      const result = first.find(entry => entry.type === "grader.result").data.results.find(result => result.id === "ineffective-retries");
      expect(result.diagnostics.failureObservations).toHaveLength(2);
      expect(fs.readFileSync(outputPath, "utf8")).not.toContain("PRIVATE_SECRET");
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });
});
