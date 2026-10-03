#!/usr/bin/env node

export default async function validateAgentSessions(ctx) {
  const fs = await import("node:fs/promises");
  const path = await import("node:path");
  const { execFile } = await import("node:child_process");
  const { promisify } = await import("node:util");
  const { createHash } = await import("node:crypto");
  const exec = promisify(execFile);
  const { repo, repoPath, outputRoot, reuseRoot, count = 50, days = 7 } = ctx.args;
  if (!/^[\w.-]+\/[\w.-]+$/.test(repo) || !path.isAbsolute(repoPath) || !path.isAbsolute(outputRoot)) throw new Error("Expected owner/repo and absolute checkout/artifact paths");
  if (!Number.isSafeInteger(count) || count < 1 || !Number.isSafeInteger(days) || days < 1) throw new Error("count and days must be positive integers");
  const artifactNames = ["agent", "usage", "info", "safe-outputs-items", "detection", "evals"];
  const validId = value => Number.isSafeInteger(value) && value > 0;
  const validateManifest = manifest => {
    if (manifest.repo !== repo || !Array.isArray(manifest.runs) || manifest.runs.length !== count || new Set(manifest.runs.map(run => run.id)).size !== count)
      throw new Error("Saved manifest does not match repository or requested distinct-run coverage");
    for (const run of manifest.runs) {
      if (
        !validId(run.id) ||
        typeof run.path !== "string" ||
        !run.path.startsWith(".github/workflows/") ||
        !run.path.endsWith(".lock.yml") ||
        run.path.split("/").includes("..") ||
        !Array.isArray(run.artifacts) ||
        !run.artifacts.some(artifact => artifact.name === "agent")
      )
        throw new Error("Invalid run in saved manifest");
      for (const artifact of run.artifacts) if (!validId(artifact.id) || !artifactNames.includes(artifact.name)) throw new Error("Invalid artifact in saved manifest");
    }
  };
  const root = path.join(outputRoot, ctx.runId);
  await fs.mkdir(root, { recursive: true, mode: 0o700 });
  const copyEvidence = async (source, target) => {
    const stat = await fs.lstat(source);
    if (stat.isSymbolicLink()) throw new Error(`Refusing artifact symbolic link: ${source}`);
    if (stat.isDirectory()) {
      await fs.mkdir(target, { recursive: true, mode: 0o700 });
      for (const name of await fs.readdir(source)) await copyEvidence(path.join(source, name), path.join(target, name));
    } else if (stat.isFile()) {
      const content = await fs.readFile(source);
      try {
        await fs.writeFile(target, content, { flag: "wx", mode: 0o600 });
      } catch (error) {
        if (error.code !== "EEXIST") throw error;
        if (!(await fs.readFile(target)).equals(content)) throw new Error(`Conflicting artifact evidence: ${target}`);
      }
    } else throw new Error(`Unsupported artifact file: ${source}`);
  };
  const command = async (file, argv) => {
    try {
      return (await exec(file, argv, { cwd: repoPath, signal: ctx.signal, maxBuffer: 32 * 1024 * 1024, env: { ...process.env, GH_PAGER: "cat", NO_COLOR: "1" } })).stdout;
    } catch (error) {
      if (ctx.signal.aborted) throw error;
      throw new Error(`${file} ${argv[0]} failed (exit ${error.code ?? "unknown"}): ${String(error.stderr ?? error.message).slice(0, 1500)}`);
    }
  };
  const api = async endpoint => {
    for (let attempt = 0; ; attempt++) {
      try {
        return JSON.parse(await command("gh", ["api", endpoint]));
      } catch (error) {
        if (ctx.signal.aborted || attempt >= 2 || !/500|502|503|504|unexpected end of JSON/i.test(error.message)) throw error;
        ctx.log(`Retrying transient API failure: ${endpoint} (attempt ${attempt + 2})`);
        await new Promise(resolve => setTimeout(resolve, 1000 * (attempt + 1)));
      }
    }
  };
  const head = (await command("git", ["rev-parse", "HEAD"])).trim();
  const dirty = (await command("git", ["status", "--porcelain", "--", "actions/setup/js"])).trim();
  const fingerprint = async () => {
    const hash = createHash("sha256");
    hash.update(validateAgentSessions.toString());
    const directory = path.join(repoPath, "actions/setup/js");
    for (const file of (await fs.readdir(directory)).filter(file => file.endsWith(".cjs") && !file.endsWith(".test.cjs")).sort()) {
      hash.update(file + "\0");
      hash.update(await fs.readFile(path.join(directory, file)));
    }
    return hash.digest("hex");
  };
  const parserFingerprint = await fingerprint();
  const since = new Date(Date.now() - days * 86400000).toISOString();
  const acceptable = run => run.status === "completed" && ["success", "failure", "cancelled", "timed_out"].includes(run.conclusion) && run.path?.endsWith(".lock.yml");
  const compact = run => ({ id: run.id, name: run.name, path: run.path, conclusion: run.conclusion, createdAt: run.created_at, attempt: run.run_attempt, url: run.html_url, headSha: run.head_sha });
  ctx.phase("Select runs");
  const selection = await ctx.step(`selection-v4:${parserFingerprint}`, async () => {
    if (reuseRoot !== undefined) {
      if (!path.isAbsolute(reuseRoot)) throw new Error("reuseRoot must be an absolute prior run directory");
      const previous = JSON.parse(await fs.readFile(path.join(reuseRoot, "manifest.json"), "utf8"));
      // Older investigations downloaded the fallback even when an agent artifact existed.
      if (Array.isArray(previous.runs)) for (const run of previous.runs) if (Array.isArray(run.artifacts)) run.artifacts = run.artifacts.filter(artifact => artifact.name !== "agent-output-fallback");
      validateManifest(previous);
      const selected = { ...previous, head, parserFingerprint, dirty: !!dirty, previousValidationHead: previous.head };
      await fs.writeFile(path.join(root, "manifest.json"), JSON.stringify(selected, null, 2) + "\n", { mode: 0o600 });
      ctx.log(`Reusing ${count} selected runs and their downloaded artifacts from ${reuseRoot}`);
      return selected;
    }
    const workflows = ["smoke-claude", "smoke-copilot", "smoke-codex", "smoke-pi", "smoke-gemini", "smoke-opencode"];
    const inventories = await ctx.parallel(
      workflows.map(workflow => async () => {
        try {
          const data = await api(`repos/${repo}/actions/workflows/${workflow}.lock.yml/runs?status=completed&per_page=100&created=${encodeURIComponent(">=" + since)}`);
          return { workflow, runs: data.workflow_runs.filter(acceptable).map(compact), error: null };
        } catch (error) {
          if (ctx.signal.aborted) throw error;
          return { workflow, runs: [], error: error.message };
        }
      })
    );
    if (inventories.some(value => value === null)) throw new Error("Workflow inventory task failed without a result");
    const candidates = [];
    for (let i = 0; i < Math.max(...inventories.map(value => value.runs.length)); i++) {
      for (const inventory of inventories) if (inventory.runs[i]) candidates.push(inventory.runs[i]);
    }
    const runs = [];
    const excluded = [];
    const seen = new Set();
    const consider = async candidate => {
      if (seen.has(candidate.id) || runs.length >= count) return;
      seen.add(candidate.id);
      const data = await api(`repos/${repo}/actions/runs/${candidate.id}/artifacts?per_page=100`);
      if (data.total_count > 100) throw new Error(`Artifact inventory truncated for run ${candidate.id}`);
      const artifacts = data.artifacts.filter(artifact => artifactNames.includes(artifact.name) && !artifact.expired);
      if (!artifacts.some(artifact => artifact.name === "agent")) {
        excluded.push({ id: candidate.id, reason: "no_retained_agent_artifact" });
        return;
      }
      runs.push({ ...candidate, artifacts: artifacts.map(artifact => ({ id: artifact.id, name: artifact.name, size: artifact.size_in_bytes })) });
    };
    for (const candidate of candidates) await consider(candidate);
    for (let page = 1; runs.length < count; page++) {
      const data = await api(`repos/${repo}/actions/runs?status=completed&per_page=100&page=${page}&created=${encodeURIComponent(">=" + since)}`);
      for (const run of data.workflow_runs.filter(acceptable)) await consider(compact(run));
      if (data.workflow_runs.length < 100) break;
    }
    const selected = { repo, head, parserFingerprint, dirty: !!dirty, since, requested: count, runs, excluded, inventories: inventories.map(({ runs, ...inventory }) => ({ ...inventory, candidates: runs.length })) };
    await fs.writeFile(path.join(root, "manifest.json"), JSON.stringify(selected, null, 2) + "\n", { mode: 0o600 });
    ctx.log(`Selected ${runs.length}/${count} distinct artifact-bearing runs; excluded ${excluded.length} without retained agent evidence`);
    return selected;
  });
  if (selection.head !== head) throw new Error("Checkout revision changed since selection; start a fresh workflow");
  if (selection.runs.length !== count) throw new Error(`Coverage shortfall: only ${selection.runs.length}/${count} runs in ${days} days; see manifest.json`);
  validateManifest(selection);

  function validateRun(repoPath, runDir, seedEngine) {
    const fs = require("node:fs");
    const path = require("node:path");
    const { isDeepStrictEqual } = require("node:util");
    const { createRequire } = require("node:module");
    const req = createRequire(path.join(repoPath, "actions/setup/js/unified_session.cjs"));
    const { parseEngineSession, collectUnifiedSession, writeUnifiedSession, sessionTimestamp } = req("./unified_session.cjs");
    const { normalizeAgentSession, isSessionEvent, sessionToolSuccess } = req("./agent_session.cjs");
    const { normalizeUnifiedSessionEvent } = req("./unified_session_payload.cjs");
    const { serializeSessionArtifact } = req("./session_artifact.cjs");
    const { renderUnifiedSession } = req("./unified_session_render.cjs");
    const runtime = path.join(runDir, "runtime");
    const findings = [];
    const observations = [];
    const add = (severity, code, source, detail, index) =>
      findings.push({ severity, code, source, detail, scope: source === "agent-session.jsonl" || source.startsWith("published:") ? "historical" : "current", ...(index === undefined ? {} : { index }) });
    const jsonl = (file, unified = false) => {
      const content = fs.readFileSync(file, "utf8");
      const values = [];
      const lines = content.split(/\r?\n/);
      const relative = path.relative(runtime, file);
      const source = relative === "usage/aw_session.jsonl" ? "published:usage/aw_session.jsonl" : relative;
      if (unified && content && !content.endsWith("\n")) add("error", "T-UAS-054:trailing_newline", source, "Missing trailing newline");
      for (let i = 0; i < lines.length; i++) {
        if (!lines[i].trim()) {
          if (unified && i < lines.length - 1) add("error", "T-UAS-054:blank_line", source, "Blank record", i);
          continue;
        }
        try {
          const value = JSON.parse(lines[i]);
          values.push(value);
          if (unified && lines[i] !== JSON.stringify(value)) add("error", "T-UAS-054:compact_jsonl", source, "Record is not compact JSON", i);
        } catch {
          add(unified ? "error" : "warning", "malformed_jsonl", source, "Invalid JSON record; payload omitted", i);
        }
      }
      return values;
    };
    const shapes = (events, source) => {
      for (let i = 0; i < events.length; i++) {
        const event = events[i];
        if (!isSessionEvent(event)) {
          add("error", "T-UAS-003:event_shape", source, "Expected namespaced type and object data", i);
          continue;
        }
        if (event.type === "tool.execution_complete" && Object.hasOwn(event.data, "success") && typeof event.data.success !== "boolean") add("error", "T-UAS-013:success_type", source, "Tool success must be boolean when supplied", i);
        for (const field of ["durationMs", "totalCostUsd"]) {
          if (Object.hasOwn(event.data, field) && (typeof event.data[field] !== "number" || !Number.isFinite(event.data[field]) || event.data[field] < 0)) add("error", "invalid_metric", source, `Invalid ${field}`, i);
        }
        if (Object.hasOwn(event.data, "numTurns") && (!Number.isSafeInteger(event.data.numTurns) || event.data.numTurns < 0)) add("error", "T-UAS-015:turn_count", source, "Invalid turn count", i);
        if (event.type === "session.result" && event.data.usage && typeof event.data.usage === "object") {
          for (const field of ["input_tokens", "output_tokens", "total_tokens", "cache_read_input_tokens", "cache_creation_input_tokens"]) {
            if (Object.hasOwn(event.data.usage, field) && (!Number.isSafeInteger(event.data.usage[field]) || event.data.usage[field] < 0)) add("error", "invalid_token_count", source, `Invalid ${field}`, i);
          }
        }
      }
    };
    const checkUnified = (events, source) => {
      shapes(events, source);
      const first = events[0];
      if (
        first?.type !== "session.format" ||
        first.data?.version !== 1 ||
        first.provenance?.component !== "collector" ||
        first.provenance?.phase !== "conclusion" ||
        first.provenance?.path !== "usage/aw_session.jsonl" ||
        first.provenance?.index !== 0 ||
        Object.hasOwn(first, "timestamp")
      )
        add("error", "T-UAS-064:format_header", source, "Missing or invalid format-1 collector header");
      let previous = -Infinity;
      const groups = new Map();
      for (let i = 0; i < events.length; i++) {
        const event = events[i];
        const p = event?.provenance;
        if (!p || typeof p.component !== "string" || typeof p.phase !== "string" || typeof p.path !== "string" || path.isAbsolute(p.path) || p.path.split(/[\\/]/).includes("..") || !Number.isSafeInteger(p.index) || p.index < 0) {
          add("error", "T-UAS-055:provenance", source, "Invalid relative source provenance", i);
          continue;
        }
        if (i === 0) continue;
        const time = p.timestampMs ?? Infinity;
        if (time < previous || (p.timestampMs !== undefined && !Number.isFinite(time))) add("error", "T-UAS-056:ordering", source, "Timestamp order or timestamp validity violation", i);
        previous = time;
        const key = JSON.stringify([p.component, p.phase, p.path, p.timestampMs ?? null]);
        if (groups.has(key) && p.index <= groups.get(key)) add("error", "T-UAS-057:stable_order", source, "Equal-time source positions are reordered or duplicated", i);
        groups.set(key, p.index);
      }
      const validEvents = events.filter(isSessionEvent);
      const summaries = validEvents.filter(event => event.type === "session.collection" && event.provenance?.component === "collector");
      if (summaries.length !== 1) add("error", "T-UAS-063:collection", source, "Expected one collection summary");
      else {
        const summary = summaries[0].data;
        const actual = validEvents.filter(event => event.provenance?.component !== "collector");
        const expected = Array.isArray(summary.sources) ? summary.sources.reduce((n, item) => n + item.events, 0) : -1;
        if (expected !== actual.length) add("error", "T-UAS-063:source_counts", source, "Selected source counts do not match emitted observations");
        const warnings = validEvents.filter(event => event.type === "session.collection_warning" && event.provenance?.component === "collector");
        if (summary.warnings !== warnings.length) add("error", "T-UAS-063:warning_counts", source, "Warning count does not match diagnostics");
        if (summary.untimedEvents !== actual.filter(event => event.provenance.timestampMs === undefined).length) add("error", "T-UAS-063:untimed_counts", source, "Untimed count does not match observations");
        observations.push({ source, collection: summary });
      }
    };
    const metadataFiles = ["aw_info.json", "usage/aw_info.json"];
    let sourceEngine = seedEngine;
    for (const relative of metadataFiles) {
      const file = path.join(runtime, relative);
      if (fs.existsSync(file)) {
        const info = JSON.parse(fs.readFileSync(file, "utf8"));
        if (typeof info.engine_id === "string") sourceEngine = info.engine_id;
        break;
      }
    }
    const engine = sourceEngine;
    const list = directory => {
      if (!fs.existsSync(directory)) return [];
      return fs
        .readdirSync(directory, { withFileTypes: true })
        .flatMap(entry => {
          const file = path.join(directory, entry.name);
          if (entry.isSymbolicLink()) throw new Error("Artifact contains a symbolic link");
          return entry.isDirectory() ? list(file) : entry.isFile() ? [file] : [];
        })
        .sort();
    };
    const rawFiles = list(path.join(runtime, "sandbox/agent/logs/copilot-session-state")).filter(file => path.basename(file) === "events.jsonl");
    for (const relative of ["pi-streaming.jsonl", "agent-stdio.log"]) if (fs.existsSync(path.join(runtime, relative))) rawFiles.push(path.join(runtime, relative));
    let parserEvents = 0;
    const rawResults = [];
    for (const file of rawFiles) {
      const source = path.relative(runtime, file);
      const content = fs.readFileSync(file, "utf8");
      const events = JSON.parse(JSON.stringify(parseEngineSession(content, engine)));
      parserEvents += events.length;
      shapes(events, source);
      if (!isDeepStrictEqual(events, JSON.parse(JSON.stringify(parseEngineSession(content, engine))))) add("error", "T-UAS-025:parser_determinism", source, "Repeated parser execution produced different events");
      const copy = structuredClone(events);
      const normalized = normalizeAgentSession(events, { sourceEngine: engine });
      if (!isDeepStrictEqual(events, copy)) add("error", "T-UAS-027:input_mutation", source, "Normalizer mutated its input");
      if (!isDeepStrictEqual(normalized, events)) add("error", "T-UAS-006:canonical_idempotence", source, "Canonical normalization lost or altered events");
      if (events.length === 0 && content.trim()) add("warning", "no_supported_raw_observations", source, "Nonempty raw input produced no supported events; setup-only logs are not parser failures");
      let native;
      try {
        const value = JSON.parse(content);
        native = Array.isArray(value) ? value : [value];
      } catch {
        native = content.split(/\r?\n/).flatMap(line => {
          try {
            return [JSON.parse(line)];
          } catch {
            return [];
          }
        });
      }
      const unknownNative = native.filter(
        value => isSessionEvent(value) && !["session.init", "session.start", "user.message", "assistant.message", "assistant.reasoning", "tool.execution_start", "tool.execution_complete", "session.result"].includes(value.type)
      );
      let cursor = 0;
      for (const record of unknownNative) {
        const match = events.findIndex((event, index) => index >= cursor && isDeepStrictEqual(event, record));
        if (match < 0) add("error", "T-UAS-006:native_extension_loss", source, "Native extension or its metadata was lost or reordered");
        else cursor = match + 1;
      }
      rawResults.push({ source, bytes: Buffer.byteLength(content), events: events.length, types: [...new Set(events.map(event => event.type))] });
      if (events.length) fs.writeFileSync(path.join(runDir, `parsed-${rawResults.length}.jsonl`), serializeSessionArtifact(events), { mode: 0o600 });
    }
    const canonicalFile = path.join(runtime, "agent-session.jsonl");
    let canonical = [];
    if (fs.existsSync(canonicalFile)) {
      canonical = jsonl(canonicalFile);
      shapes(canonical, "agent-session.jsonl");
      if (!isDeepStrictEqual(normalizeAgentSession(canonical), canonical)) add("error", "T-UAS-006:persisted_canonical", "agent-session.jsonl", "Persisted trace changes when normalized");
    }
    const deployed = path.join(runtime, "usage/aw_session.jsonl");
    const publishedExists = fs.existsSync(deployed);
    if (publishedExists) checkUnified(jsonl(deployed, true), "published:usage/aw_session.jsonl");
    else add("info", "historical_unified_artifact_absent", "usage", "Run predates publication or has no usage artifact; replay is tested independently");
    const warnings = [];
    const collected = collectUnifiedSession({ rootDir: runtime, engine, warn: value => warnings.push(value) });
    const before = JSON.stringify(collected.events);
    const generated = path.join(runDir, "normalized/aw_session.jsonl");
    writeUnifiedSession({ rootDir: runtime, engine, outputPath: generated, warn: () => {} });
    const bytes = fs.readFileSync(generated, "utf8");
    const persisted = jsonl(generated, true);
    checkUnified(persisted, "replayed:aw_session.jsonl");
    writeUnifiedSession({ rootDir: runtime, engine, outputPath: generated, warn: () => {} });
    if (fs.readFileSync(generated, "utf8") !== bytes) add("error", "repeat_collection_changed", "replayed", "Repeated collection was not byte-identical");
    if (JSON.stringify(collected.events) !== before) add("error", "T-UAS-055:source_mutation", "replayed", "Serialization mutated collected events");
    const expected = JSON.parse("[" + serializeSessionArtifact(collected.events, collected.maskedValues).trim().split("\n").join(",") + "]");
    if (!isDeepStrictEqual(expected, persisted)) add("error", "serialization_mismatch", "replayed", "Serialized events differ from redacted in-memory collection");
    const agent = persisted.filter(event => event.provenance?.component === "agent");
    const validCanonical = canonical.filter(isSessionEvent);
    if (validCanonical.length && agent.length !== validCanonical.length) add("error", "T-UAS-058:duplicate_or_lost_agent", "replayed", "Canonical authoritative event count was not retained");
    for (const warning of persisted.filter(event => event.type === "session.collection_warning")) add("warning", `collection:${warning.data.code}`, warning.data.path, "Collector reported incomplete source coverage", warning.data.line);
    if (!agent.length) add("warning", "no_agent_observations", "replayed", "No supported agent observations; this run does not establish engine-parser coverage");
    for (const event of agent) {
      const p = event.provenance;
      if (event.type === "tool.execution_complete" && sessionToolSuccess(event.data) === false) {
        const rendered = renderUnifiedSession([persisted[0], event], { markdown: false, maxBytes: 64000, maxLineBytes: 10000, agentStatistics: () => [] });
        if (!rendered.includes("[failed]")) add("error", "T-UAS-014:failure_rendering", p.path, "Chronological renderer does not label an explicit tool failure as failed", p.index);
      }
      if (p.path === "agent-session.jsonl" && validCanonical[p.index]) {
        const projected = normalizeUnifiedSessionEvent(validCanonical[p.index]);
        const redacted = JSON.parse(serializeSessionArtifact([projected], collected.maskedValues));
        const { provenance, ...withoutProvenance } = event;
        if (!isDeepStrictEqual(redacted, withoutProvenance)) add("error", "T-UAS-054:essential_projection", p.path, "Essential source payload or native metadata changed", p.index);
        const ms = sessionTimestamp(validCanonical[p.index]);
        if (ms !== p.timestampMs) add("error", "T-UAS-056:timestamp_units", p.path, "Timestamp ordering key does not match source schema", p.index);
      }
    }
    const diagnostics = {
      sourceEngine,
      engine,
      parserEvents,
      canonicalEvents: canonical.length,
      agentEvents: agent.length,
      unifiedEvents: persisted.length,
      publishedExists,
      rawResults,
      observations,
      findings,
      status: findings.some(finding => finding.severity === "error") ? "failed" : findings.some(finding => finding.severity === "warning") ? "warning" : "passed",
    };
    fs.writeFileSync(path.join(runDir, "result.json"), JSON.stringify(diagnostics, null, 2) + "\n", { mode: 0o600 });
    return diagnostics;
  }

  function probeContract(repoPath) {
    const { createRequire } = require("node:module");
    const req = createRequire(require("node:path").join(repoPath, "actions/setup/js/unified_session.cjs"));
    const { normalizeUnifiedSessionEvent } = req("./unified_session_payload.cjs");
    const { generatePlainTextSummary } = req("./log_parser_shared.cjs");
    const cases = [
      { type: "experiment.assignment", data: { assignments: null }, field: "assignments", lines: "179-181" },
      { type: "safe_output.error", data: { errors: null, failures: [{ type: "add_comment", error: "example failure" }] }, field: "errors", lines: "185-187" },
      { type: "usage.report", data: { usage: null, input_tokens: 5 }, field: "usage", lines: "173-176" },
    ];
    const findings = cases.flatMap(({ type, data, field, lines }) => {
      const actual = normalizeUnifiedSessionEvent({ type, data }).data[field];
      return actual === null
        ? []
        : [{ code: "T-UAS-054:null_precedence", file: "actions/setup/js/unified_session_payload.cjs", lines, detail: `Explicit ${field}: null is replaced by a fallback`, synthetic: true, input: { type, data }, actual }];
    });
    const header = { type: "session.format", data: { version: 1 }, provenance: { component: "collector", phase: "conclusion", path: "usage/aw_session.jsonl", index: 0 } };
    const completion = {
      type: "tool.execution_complete",
      data: { toolCallId: "example", toolName: "bash", success: true, exitCode: 1, status: "failed" },
      provenance: { component: "agent", phase: "agent", path: "example.jsonl", index: 0 },
    };
    const rendered = generatePlainTextSummary([header, completion]);
    if (rendered.includes("[succeeded]"))
      findings.push({
        code: "T-UAS-014:failure_rendering",
        file: "actions/setup/js/unified_session_render.cjs",
        lines: "79-81",
        detail: "Chronological trace reports succeeded while explicit failure controls and statistics report a failed tool",
        synthetic: true,
        input: completion,
        actual: rendered.split("\n").filter(line => line.includes("tool.execution_complete") || line.includes("Failed Tools")),
      });
    return findings;
  }

  ctx.phase("Download and normalize");
  const results = await ctx.pipeline(
    selection.runs,
    async (_previous, run) =>
      ctx.step(`download-v2:${run.id}`, async () => {
        const directory = path.join(root, String(run.id));
        await fs.mkdir(path.join(directory, "raw"), { recursive: true, mode: 0o700 });
        const failures = [];
        for (const artifact of run.artifacts) {
          try {
            const target = path.join(directory, "raw", artifact.name);
            await fs.mkdir(target, { recursive: true, mode: 0o700 });
            if (reuseRoot !== undefined) await copyEvidence(path.join(reuseRoot, String(run.id), "raw", artifact.name), target);
            else await command("gh", ["run", "download", String(run.id), "-R", repo, "-n", artifact.name, "-D", target]);
          } catch (error) {
            if (ctx.signal.aborted) throw error;
            failures.push({ severity: "error", code: "artifact_download_failed", source: artifact.name, detail: error.message });
          }
        }
        if (failures.length) return { directory, failures };
        const runtime = path.join(directory, "runtime");
        await fs.mkdir(runtime, { recursive: true, mode: 0o700 });
        const destinations = { agent: "", usage: "usage", info: "", "safe-outputs-items": "", detection: "threat-detection", evals: "evals" };
        try {
          for (const [name, destination] of Object.entries(destinations)) {
            if (run.artifacts.some(artifact => artifact.name === name)) await copyEvidence(path.join(directory, "raw", name), path.join(runtime, destination));
          }
        } catch (error) {
          if (ctx.signal.aborted) throw error;
          failures.push({ severity: "error", code: "artifact_assembly_failed", source: "runtime", detail: error.message });
        }
        return { directory, failures };
      }),
    async (download, run) =>
      ctx.step(`validate-v4:${parserFingerprint}:${run.id}`, async () => {
        if (download.failures.length) return { ...run, status: "blocked", findings: download.failures, directory: download.directory };
        let engine = "custom";
        const match = run.path.match(/smoke-(claude|copilot|codex|pi|gemini)\.lock\.yml$/);
        if (match) engine = match[1];
        try {
          const script = `const result=(${validateRun.toString()})(...process.argv.slice(1));process.stdout.write(JSON.stringify(result));`;
          const value = JSON.parse(await command("node", ["-e", script, repoPath, download.directory, engine]));
          ctx.log(`Run ${run.id} (${value.sourceEngine}): ${value.status}; ${value.agentEvents} agent events, ${value.findings.length} diagnostics`);
          return { ...run, ...value, directory: download.directory };
        } catch (error) {
          if (ctx.signal.aborted) throw error;
          ctx.log(`Run ${run.id}: validation failed`);
          return { ...run, status: "blocked", findings: [{ severity: "error", code: "validation_execution_failed", source: "validator", detail: error.message }], directory: download.directory };
        }
      })
  );
  ctx.phase("Report problems");
  if ((await fingerprint()) !== parserFingerprint) throw new Error("Parser sources changed during validation; rerun against a stable checkout");
  const report = await ctx.step(`report-v4:${parserFingerprint}`, async () => {
    const probeScript = `process.stdout.write(JSON.stringify((${probeContract.toString()})(process.argv[1])));`;
    const contractProbes = JSON.parse(await command("node", ["-e", probeScript, repoPath]));
    await fs.writeFile(path.join(root, "contract-probes.json"), JSON.stringify(contractProbes, null, 2) + "\n", { mode: 0o600 });
    const missing = results.flatMap((value, i) => (value === null ? [selection.runs[i].id] : []));
    const checked = results.filter(value => value !== null);
    const engines = {};
    const problems = {};
    for (const result of checked) {
      const key = result.sourceEngine ?? "unknown";
      engines[key] ??= { runs: 0, withAgentEvents: 0, rawParserRuns: 0 };
      engines[key].runs++;
      if (result.agentEvents > 0) engines[key].withAgentEvents++;
      if (result.parserEvents > 0) engines[key].rawParserRuns++;
      for (const finding of result.findings) {
        const key = `${finding.severity}:${finding.code}`;
        problems[key] ??= { severity: finding.severity, code: finding.code, occurrences: 0, runs: [] };
        problems[key].occurrences++;
        if (!problems[key].runs.includes(result.id)) problems[key].runs.push(result.id);
      }
    }
    const summary = {
      requested: count,
      selected: selection.runs.length,
      completed: checked.filter(result => result.status !== "blocked").length,
      passed: checked.filter(result => result.status === "passed").length,
      warnings: checked.filter(result => result.status === "warning").length,
      failed: checked.filter(result => result.status === "failed").length,
      currentFailed: checked.filter(result => result.findings.some(finding => finding.severity === "error" && finding.scope !== "historical")).length,
      historicalFailed: checked.filter(result => result.findings.some(finding => finding.severity === "error" && finding.scope === "historical")).length,
      blocked: checked.filter(result => result.status === "blocked").length,
      missing,
      engines,
      agentEvidenceRuns: checked.filter(result => result.agentEvents > 0).length,
      substantiveParserRuns: checked.filter(result => result.rawResults?.some(raw => raw.types.some(type => ["user.message", "assistant.message", "assistant.reasoning", "tool.execution_start", "tool.execution_complete"].includes(type))))
        .length,
      contractProbes,
      accountingOnlyRuns: checked.filter(result => result.parserEvents > 0 && result.rawResults?.every(raw => raw.types.every(type => type === "session.result"))).map(result => ({ id: result.id, engine: result.sourceEngine })),
      publishedRuns: checked.filter(result => result.publishedExists).length,
      problems: Object.values(problems),
      scope: "Local replay of retained CI evidence at the pinned checkout revision; no CI dispatch and no claim of full specification conformance",
    };
    const result = { runId: ctx.runId, repo, head, parserFingerprint, dirty: !!dirty, root, summary, selection, runs: checked };
    await fs.writeFile(path.join(root, "report.json"), JSON.stringify(result, null, 2) + "\n", { mode: 0o600 });
    const rows = checked.map(run => `| [${run.id}](${run.url}) | ${run.sourceEngine ?? "unknown"} | ${run.status} | ${run.agentEvents ?? "unavailable"} | ${run.findings.filter(finding => finding.severity === "error").length} |`);
    const text = [
      "# Unified agent session validation",
      "",
      `Checkout: \`${head}\`. ${summary.completed}/${count} completed; ${summary.passed} passed, ${summary.warnings} with warnings, ${summary.failed} failed, ${summary.blocked} blocked.`,
      "",
      `Parser fingerprint: \`${parserFingerprint}\`${dirty ? " (uncommitted runtime changes included)" : ""}. Current parser/merger failures: ${summary.currentFailed}; historical artifact failures: ${summary.historicalFailed}; independent contract-probe failures: ${contractProbes.length}.`,
      "",
      summary.scope,
      "",
      "Checks: canonical event and metric shapes, native-extension preservation, normalization idempotence, source mutation, compact JSONL, format header, relative provenance, timestamp ordering, tie stability, coverage counts, authoritative selection, essential-payload projection, and repeatable redacted serialization.",
      "",
      "Optional fields and components are not required. Empty/partial traces, orphan tool calls and downstream CI failures are not automatically parser errors. Replayed output and historically published output are assessed separately. Essential projection uses the production projector; it is not an independent proof of every essential-field mapping.",
      "",
      "## Engine coverage",
      "",
      "| Engine | Runs | Agent evidence | Raw parser exercised |",
      "| --- | ---: | ---: | ---: |",
      ...Object.entries(engines).map(([engine, value]) => `| ${engine} | ${value.runs} | ${value.withAgentEvents} | ${value.rawParserRuns} |`),
      "",
      `${summary.agentEvidenceRuns}/${count} runs contain supported agent evidence; ${summary.substantiveParserRuns}/${count} exercise message, reasoning, or tool parsing. ${summary.accountingOnlyRuns.length} runs produce accounting-only traces; these do not validate message/tool protocol mappings. Historical artifact absence is not a format failure.`,
      "",
      "## Problems",
      "",
      ...Object.values(problems).map(
        problem =>
          `- **${problem.severity}: ${problem.code}** - ${problem.occurrences} observations across ${problem.runs.length} runs. Examples: ${problem.runs
            .slice(0, 3)
            .map(id => `[${id}](https://github.com/${repo}/actions/runs/${id})`)
            .join(", ")}.`
      ),
      "",
      "## Independent contract probes",
      "",
      "These sanitized synthetic checks are separate from the 50 CI runs, and identify gaps that the sampled inputs do not exercise.",
      "",
      ...contractProbes.map(finding => `- **${finding.code}** in \`${finding.file}:${finding.lines}\`: ${finding.detail}.`),
      "",
      "## CI-backed root cause",
      "",
      "Historical canonical artifacts are assessed unchanged. A legacy artifact can still fail after its producer is fixed: replay must not rewrite source history. The shared bootstrap normalizes inline parser returns before canonical persistence. A historical legacy assistant record can be infrastructure text rather than model evidence; its presence is not proof that a model answered.",
      "",
      "## Run matrix",
      "",
      "| Run | Engine | Status | Agent events | Errors |",
      "| --- | --- | --- | ---: | ---: |",
      ...rows,
      "",
      "See report.json and each run's result.json for source-local diagnostics. Raw artifacts remain local; do not publish transcripts, prompts, or tool outputs.",
      "",
    ].join("\n");
    await fs.writeFile(path.join(root, "report.md"), text, { mode: 0o600 });
    ctx.log(JSON.stringify(summary));
    return { ...summary, root, reportJson: path.join(root, "report.json"), reportMarkdown: path.join(root, "report.md") };
  });
  return report;
}

export function validationExitCode(result) {
  return result.completed !== result.requested || result.failed > 0 || result.blocked > 0 || result.missing.length > 0 || result.contractProbes.length > 0 ? 1 : 0;
}

export async function main(argv = process.argv.slice(2)) {
  const { parseArgs } = await import("node:util");
  const { resolve } = await import("node:path");
  const { randomUUID } = await import("node:crypto");
  const { tmpdir } = await import("node:os");
  const { values } = parseArgs({
    args: argv,
    options: {
      repo: { type: "string", default: "github/gh-aw" },
      checkout: { type: "string", default: resolve(import.meta.dirname, "..") },
      output: { type: "string", default: resolve(tmpdir(), "gh-aw-session-validation") },
      reuse: { type: "string" },
      count: { type: "string", default: "50" },
      days: { type: "string", default: "7" },
      help: { type: "boolean", short: "h" },
    },
  });
  if (values.help) {
    console.log(
      "Usage: node scripts/validate-agent-sessions.mjs [--repo owner/repo] [--checkout path] [--output path] [--reuse prior-run-directory] [--count 50] [--days 7]\nDownloads existing CI evidence only. Requires Node.js, git and authenticated gh. Exits 1 for coverage shortfalls, invalid artifacts or contract failures; warnings alone do not fail."
    );
    return 0;
  }
  const parallel = thunks =>
    Promise.all(
      thunks.map(async thunk => {
        try {
          return await thunk();
        } catch (error) {
          console.error(error.message);
          return null;
        }
      })
    );
  const result = await validateAgentSessions({
    args: { repo: values.repo, repoPath: resolve(values.checkout), outputRoot: resolve(values.output), ...(values.reuse ? { reuseRoot: resolve(values.reuse) } : {}), count: Number(values.count), days: Number(values.days) },
    runId: randomUUID(),
    signal: new AbortController().signal,
    phase: title => console.error(title),
    log: message => console.error(message),
    step: async (_key, producer) => producer(),
    parallel,
    pipeline: (items, ...stages) =>
      parallel(
        items.map((item, index) => async () => {
          let previous;
          for (const stage of stages) previous = await stage(previous, item, index);
          return previous;
        })
      ),
  });
  console.log(JSON.stringify(result, null, 2));
  return validationExitCode(result);
}

const { pathToFileURL } = await import("node:url");
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try {
    process.exitCode = await main();
  } catch (error) {
    console.error(`Session validation failed: ${error.message}`);
    process.exitCode = 1;
  }
}
