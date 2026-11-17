// @ts-check

const crypto = require("node:crypto");
const fs = require("node:fs");
const path = require("node:path");
const { ERR_CONFIG, ERR_PARSE, ERR_SYSTEM, ERR_VALIDATION } = require("./error_codes.cjs");
const { readJsonlFileEvents } = require("./jsonl_helpers.cjs");
const { renderCharts, renderMarkdown } = require("./workflow_value_report_render.cjs");

const MAX_INPUT_FILE_BYTES = 512 * 1024;
const MAX_LEDGER_BYTES = 16 * 1024 * 1024;
const MAX_OUTPUT_FILE_BYTES = 512 * 1024;
const MAX_OUTPUT_BYTES = 2 * 1024 * 1024;
const RETAINED_UPLOAD_DAYS = 30;
const DELIVERY_FIELDS = ["delivered"];
const ADOPTION_FIELDS = ["accepted", "accepted_human", "accepted_delivery_fallback", "rejected", "not_applicable", "pending", "lifecycle", "unknown"];
const RETAINED_FIELDS = [...DELIVERY_FIELDS, ...ADOPTION_FIELDS.filter(field => field !== "accepted"), "survival_observed", "survival_reversed", "survival_unknown"];

/** @param {Map<string, string>} files */
function validateArtifactFiles(files) {
  let totalBytes = 0;
  for (const [name, content] of files) {
    const bytes = Buffer.byteLength(content);
    if (bytes > MAX_OUTPUT_FILE_BYTES) {
      throw new Error(`${ERR_VALIDATION}: Complete workflow value artifact file exceeds 512 KiB without truncation: ${name} (${bytes} bytes)`);
    }
    totalBytes += bytes;
  }
  if (totalBytes > MAX_OUTPUT_BYTES) throw new Error(`${ERR_VALIDATION}: Complete workflow value artifact exceeds 2 MiB without truncation`);
  return totalBytes;
}

/** @param {string | Buffer} value */
function sha256(value) {
  return crypto.createHash("sha256").update(value).digest("hex");
}

/** @param {string} filename @param {number} maximum */
function readBounded(filename, maximum) {
  let stat;
  try {
    stat = fs.lstatSync(filename);
  } catch (error) {
    throw new Error(`${ERR_SYSTEM}: Failed to inspect workflow value input ${filename}`, { cause: error });
  }
  if (!stat.isFile() || stat.size > maximum) throw new Error(`${ERR_VALIDATION}: Workflow value input exceeds its file budget: ${filename}`);
  try {
    return fs.readFileSync(filename);
  } catch (error) {
    throw new Error(`${ERR_SYSTEM}: Failed to read workflow value input ${filename}`, { cause: error });
  }
}

/** @param {string} filename @param {number} maximum */
function readJSON(filename, maximum = MAX_INPUT_FILE_BYTES) {
  const content = readBounded(filename, maximum);
  try {
    return { content, value: JSON.parse(content.toString("utf8")) };
  } catch (error) {
    throw new Error(`${ERR_PARSE}: Invalid workflow value JSON: ${filename}`, { cause: error });
  }
}

/** @param {string} filename @param {string} content */
function writeAtomic(filename, content) {
  try {
    fs.mkdirSync(path.dirname(filename), { recursive: true });
    fs.writeFileSync(`${filename}.tmp`, content, { mode: 0o600 });
    fs.renameSync(`${filename}.tmp`, filename);
  } catch (error) {
    throw new Error(`${ERR_SYSTEM}: Failed to write workflow value output ${filename}`, { cause: error });
  }
}

/** @param {string} source */
function extractFrontmatter(source) {
  if (!source.startsWith("---\n")) throw new Error(`${ERR_VALIDATION}: Agentic workflow source must start with frontmatter`);
  const end = source.indexOf("\n---", 4);
  if (end < 0) throw new Error(`${ERR_VALIDATION}: Agentic workflow source has unterminated frontmatter`);
  return source.slice(4, end);
}

/** @param {string} frontmatter */
function declaresOperationalValue(frontmatter) {
  let gradersIndent = -1;
  let operationalIndent = -1;
  for (const line of frontmatter.split("\n")) {
    if (!line.trim() || line.trimStart().startsWith("#")) continue;
    const indent = line.length - line.trimStart().length;
    const trimmed = line.trim();
    if (gradersIndent >= 0 && indent <= gradersIndent && trimmed !== "graders:") {
      gradersIndent = -1;
      operationalIndent = -1;
    }
    if (trimmed === "graders:") {
      gradersIndent = indent;
      operationalIndent = -1;
      continue;
    }
    if (gradersIndent >= 0 && indent > gradersIndent && trimmed === "operational-value:") {
      operationalIndent = indent;
      continue;
    }
    if (operationalIndent >= 0 && indent <= operationalIndent) operationalIndent = -1;
    if (operationalIndent >= 0 && indent > operationalIndent && /^script:\s*(?:[|>].*|\S.*)$/.test(trimmed)) return true;
  }
  return false;
}

/** @param {string} repoRoot */
function loadWorkflowInventory(repoRoot) {
  const directory = path.join(repoRoot, ".github", "workflows");
  let entries;
  try {
    entries = fs.readdirSync(directory, { withFileTypes: true });
  } catch (error) {
    throw new Error(`${ERR_SYSTEM}: Failed to list top-level agentic workflows`, { cause: error });
  }
  const sources = new Map();
  const locks = new Map();
  for (const entry of entries) {
    if (!entry.isFile()) continue;
    if (entry.name.endsWith(".lock.yml")) locks.set(entry.name.slice(0, -9), entry.name);
    else if (entry.name.endsWith(".md")) sources.set(entry.name.slice(0, -3), entry.name);
  }
  const ids = [...new Set([...sources.keys(), ...locks.keys()])].sort();
  const folded = new Map();
  for (const id of ids) {
    const key = id.toLowerCase();
    if (folded.has(key) && folded.get(key) !== id) throw new Error(`${ERR_VALIDATION}: Duplicate case-insensitive workflow identity: ${folded.get(key)} and ${id}`);
    folded.set(key, id);
  }
  const missingSources = ids.filter(id => !sources.has(id));
  const missingLocks = ids.filter(id => !locks.has(id));
  if (missingSources.length || missingLocks.length) {
    throw new Error(`${ERR_VALIDATION}: Every top-level workflow must have one source and lock pair; missing sources: ${missingSources.join(", ") || "none"}; missing locks: ${missingLocks.join(", ") || "none"}`);
  }
  const workflows = ids.map(id => {
    const sourcePath = `.github/workflows/${sources.get(id)}`;
    const lockPath = `.github/workflows/${locks.get(id)}`;
    const source = readBounded(path.join(repoRoot, sourcePath), MAX_INPUT_FILE_BYTES);
    const lock = readBounded(path.join(repoRoot, lockPath), MAX_INPUT_FILE_BYTES);
    const frontmatter = extractFrontmatter(source.toString("utf8"));
    return {
      id,
      source_path: sourcePath,
      lock_path: lockPath,
      source_sha256: sha256(source),
      lock_sha256: sha256(lock),
      operational_value: declaresOperationalValue(frontmatter)
        ? {
            state: "declared_run_scoped",
            scope: "run_scoped_optional",
            frontmatter_sha256: sha256(frontmatter),
          }
        : { state: "not_declared", scope: "run_scoped_optional" },
    };
  });
  return {
    scope: "top_level_paired_agentic_workflows",
    count: workflows.length,
    sha256: sha256(workflows.map(workflow => `${workflow.id}\0${workflow.source_sha256}\0${workflow.lock_sha256}\n`).join("")),
    workflows,
  };
}

/** @param {unknown} value */
function timestamp(value) {
  if (typeof value !== "string") return null;
  const parsed = Date.parse(value);
  return Number.isFinite(parsed) ? parsed : null;
}

/** @param {unknown} value */
function dateString(value) {
  const parsed = timestamp(value);
  if (parsed === null) return null;
  const date = new Date(parsed);
  if (!Number.isFinite(date.getTime())) return null;
  return date.toISOString().slice(0, 10);
}

/** @param {string} metricsDir @param {"metrics" | "lifecycle"} kind */
function readDatedCandidates(metricsDir, kind) {
  const directory = kind === "metrics" ? path.join(metricsDir, "daily") : path.join(metricsDir, "lifecycle", "daily");
  /** @type {Array<Record<string, any>>} */
  const candidates = [];
  if (fs.existsSync(directory)) {
    let names;
    try {
      names = fs.readdirSync(directory).sort();
    } catch (error) {
      throw new Error(`${ERR_SYSTEM}: Failed to list retained workflow value history: ${directory}`, { cause: error });
    }
    for (const name of names) {
      if (!/^\d{4}-\d{2}-\d{2}\.json$/.test(name)) continue;
      const filename = path.join(directory, name);
      const relative = path.relative(path.dirname(metricsDir), filename).split(path.sep).join("/");
      const { content, value } = readJSON(filename);
      const observed = kind === "metrics" ? value?.timestamp : value?.observed_at;
      candidates.push({
        date: name.slice(0, 10),
        path: relative,
        digest: sha256(content),
        observed_at: observed,
        observed_ms: timestamp(observed),
        value,
      });
    }
  }
  const latest = kind === "metrics" ? path.join(metricsDir, "latest.json") : path.join(metricsDir, "lifecycle.json");
  if (fs.existsSync(latest)) {
    const relative = path.relative(path.dirname(metricsDir), latest).split(path.sep).join("/");
    const { content, value } = readJSON(latest);
    const observed = kind === "metrics" ? value?.timestamp : value?.observed_at;
    const date = dateString(observed);
    if (!date) throw new Error(`${ERR_VALIDATION}: Current ${kind} snapshot has no valid observation timestamp`);
    candidates.push({ date, path: relative, digest: sha256(content), observed_at: observed, observed_ms: timestamp(observed), value });
  }
  return candidates;
}

/** @param {Array<Record<string, any>>} candidates */
function selectLatestByDate(candidates) {
  const grouped = new Map();
  for (const candidate of candidates) {
    const current = grouped.get(candidate.date);
    if (!current || (candidate.observed_ms ?? -1) > (current.observed_ms ?? -1)) {
      grouped.set(candidate.date, candidate);
      continue;
    }
    if ((candidate.observed_ms ?? -1) === (current.observed_ms ?? -1) && candidate.digest !== current.digest) {
      throw new Error(`${ERR_VALIDATION}: Conflicting same-clock retained snapshots for ${candidate.date}`);
    }
  }
  return grouped;
}

/** @param {Record<string, any>} snapshot */
function exactDailyWindow(snapshot) {
  if (snapshot?.period !== "daily") return false;
  const start = timestamp(snapshot?.collection_window?.start);
  const end = timestamp(snapshot?.collection_window?.end);
  return start !== null && end !== null && end - start === 24 * 60 * 60 * 1000;
}

/** @param {Record<string, any> | undefined} value */
function normalizeDelivery(value) {
  if (!value) return { coverage: "unavailable", delivered: null, reason_code: "delivery_evidence_absent" };
  if (!["complete", "partial", "unavailable"].includes(value.coverage)) throw new Error(`${ERR_VALIDATION}: Invalid selected-window delivery coverage`);
  if (value.coverage === "unavailable") {
    if (value.delivered !== null && value.delivered !== undefined) throw new Error(`${ERR_VALIDATION}: Unavailable delivery evidence must not contain a count`);
    return { coverage: "unavailable", delivered: null, reason_code: "delivery_evidence_unavailable" };
  }
  if (!Number.isSafeInteger(value.delivered) || value.delivered < 0) throw new Error(`${ERR_VALIDATION}: Delivery count must be a non-negative integer`);
  return { coverage: value.coverage, delivered: value.delivered, reason_code: null };
}

/** @param {Record<string, any> | undefined} value */
function normalizeAdoption(value) {
  if (!value) {
    return Object.fromEntries([["coverage", "unavailable"], ...ADOPTION_FIELDS.map(field => [field, null]), ["reason_code", "adoption_evidence_absent"]]);
  }
  if (!["complete", "partial", "unavailable"].includes(value.coverage)) throw new Error(`${ERR_VALIDATION}: Invalid selected-window adoption coverage`);
  if (value.coverage === "unavailable") {
    for (const field of ADOPTION_FIELDS) {
      if (value[field] !== null && value[field] !== undefined) throw new Error(`${ERR_VALIDATION}: Unavailable adoption evidence must not contain counts`);
    }
    return Object.fromEntries([["coverage", "unavailable"], ...ADOPTION_FIELDS.map(field => [field, null]), ["reason_code", "adoption_evidence_unavailable"]]);
  }
  for (const field of ADOPTION_FIELDS) {
    if (!Number.isSafeInteger(value[field]) || value[field] < 0) throw new Error(`${ERR_VALIDATION}: Adoption counts must be non-negative integers`);
  }
  if (value.accepted !== value.accepted_human + value.accepted_delivery_fallback) {
    throw new Error(`${ERR_VALIDATION}: Adoption acceptance total must equal human and delivery-fallback acceptance`);
  }
  return Object.fromEntries([["coverage", value.coverage], ...ADOPTION_FIELDS.map(field => [field, value[field]]), ["reason_code", null]]);
}

/** @param {Record<string, any>} counts @param {string} coverage */
function normalizeRetained(counts, coverage) {
  if (!["complete", "partial", "unavailable"].includes(coverage)) throw new Error(`${ERR_VALIDATION}: Invalid retained lifecycle coverage`);
  if (coverage === "unavailable") {
    return Object.fromEntries([["coverage", "unavailable"], ...RETAINED_FIELDS.map(field => [field, null]), ["accepted", null], ["reason_code", "retained_population_unavailable"]]);
  }
  for (const field of RETAINED_FIELDS) {
    if (!Number.isSafeInteger(counts?.[field]) || counts[field] < 0) throw new Error(`${ERR_VALIDATION}: Retained lifecycle counts must be non-negative integers`);
  }
  return {
    coverage,
    ...Object.fromEntries(RETAINED_FIELDS.map(field => [field, counts[field]])),
    accepted: counts.accepted_human + counts.accepted_delivery_fallback,
    reason_code: null,
  };
}

/**
 * @param {string[]} presentIds
 * @param {Map<string, Record<string, any>>} observations
 * @param {string[]} fields
 * @param {number} inventoryCount
 * @returns {Record<string, any>}
 */
function encodeObservations(presentIds, observations, fields, inventoryCount) {
  const frequencies = new Map();
  for (const id of presentIds) {
    const encoded = JSON.stringify(observations.get(id));
    frequencies.set(encoded, (frequencies.get(encoded) || 0) + 1);
  }
  const defaultEncoded =
    [...frequencies.entries()].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))[0]?.[0] ||
    JSON.stringify(Object.fromEntries([["coverage", "unavailable"], ...fields.map(field => [field, null]), ["reason_code", "workflow_not_in_period_inventory"]]));
  let defaultObservation;
  try {
    defaultObservation = JSON.parse(defaultEncoded);
  } catch (error) {
    throw new Error(`${ERR_PARSE}: Failed to decode a normalized workflow observation`, { cause: error });
  }
  const overrides = Object.create(null);
  for (const id of presentIds) {
    const observation = observations.get(id);
    if (!observation) throw new Error(`${ERR_VALIDATION}: Workflow observation is missing: ${id}`);
    if (JSON.stringify(observation) !== defaultEncoded) overrides[id] = observation;
  }
  const counts = Object.fromEntries(fields.map(field => [field, 0]));
  let complete = 0;
  let partial = 0;
  let unavailable = inventoryCount - presentIds.length;
  for (const id of presentIds) {
    const observation = observations.get(id);
    if (!observation) throw new Error(`${ERR_VALIDATION}: Workflow observation is missing: ${id}`);
    if (observation.coverage === "complete") complete++;
    else if (observation.coverage === "partial") partial++;
    else unavailable++;
    if (observation.coverage !== "unavailable") {
      for (const field of fields) counts[field] += observation[field];
    }
  }
  const coverage = complete === inventoryCount ? "complete" : complete + partial > 0 ? "partial" : "unavailable";
  return {
    default: defaultObservation,
    overrides,
    aggregate: {
      coverage,
      ...Object.fromEntries(fields.map(field => [field, coverage === "unavailable" ? null : counts[field]])),
      count_semantics: coverage === "complete" ? "complete_current_inventory" : coverage === "partial" ? "observed_only" : "unavailable",
      workflow_coverage: { complete, partial, unavailable },
    },
  };
}

/** @param {string[]} presentIds @param {string[]} inventoryIds @param {string[]} unknownIds */
function inventoryMembership(presentIds, inventoryIds, unknownIds) {
  const present = new Set(presentIds);
  const missing = inventoryIds.filter(id => !present.has(id));
  let membership;
  if (presentIds.length === 0) membership = { mode: "none", workflow_ids: [] };
  else if (presentIds.length === inventoryIds.length && unknownIds.length === 0) membership = { mode: "current", workflow_ids: [] };
  else if (presentIds.length <= missing.length) membership = { mode: "present", workflow_ids: presentIds };
  else membership = { mode: "missing_current", workflow_ids: missing };
  return {
    reported_count: presentIds.length + unknownIds.length,
    current_reported_count: presentIds.length,
    coverage: presentIds.length === inventoryIds.length && unknownIds.length === 0 ? "complete" : presentIds.length || unknownIds.length ? "partial" : "unavailable",
    membership,
    unknown_workflow_ids: unknownIds,
  };
}

/** @param {Record<string, any>} candidate @param {Record<string, any>} inventory */
function normalizeMetricPeriod(candidate, inventory) {
  const snapshot = candidate.value;
  const inventoryIds = inventory.workflows.map(workflow => workflow.id);
  const known = new Set(inventoryIds);
  if (!snapshot || !snapshot.workflows || Array.isArray(snapshot.workflows) || typeof snapshot.workflows !== "object") {
    const empty = encodeObservations([], new Map(), DELIVERY_FIELDS, inventory.count);
    const emptyAdoption = encodeObservations([], new Map(), ADOPTION_FIELDS, inventory.count);
    return {
      date: candidate.date,
      timestamp: null,
      metrics_source: candidate.path,
      collection_status: snapshot?.status === "no-data" ? "legacy_unavailable" : "unavailable",
      comparability: "unavailable",
      session_evidence: {
        source: null,
        reported_workflows: 0,
        delivery_workflows: 0,
        adoption_workflows: 0,
      },
      inventory: inventoryMembership([], inventoryIds, []),
      selected_window: {
        delivery: empty,
        adoption: emptyAdoption,
        aggregate: { ...empty.aggregate, ...Object.fromEntries(ADOPTION_FIELDS.map(field => [field, null])) },
      },
    };
  }
  const keys = Object.keys(snapshot.workflows).sort();
  const presentIds = keys.filter(id => known.has(id));
  const unknownIds = keys.filter(id => !known.has(id));
  const comparable = exactDailyWindow(snapshot);
  const deliveries = new Map();
  const adoptions = new Map();
  const sessionSources = new Set();
  let sessionReportedWorkflows = 0;
  let sessionDeliveryWorkflows = 0;
  let sessionAdoptionWorkflows = 0;
  for (const id of presentIds) {
    const workflow = snapshot.workflows[id];
    if (!workflow || typeof workflow !== "object" || Array.isArray(workflow)) throw new Error(`${ERR_VALIDATION}: Invalid workflow metrics record: ${id}`);
    if (typeof workflow.safe_outputs_source === "string" && workflow.safe_outputs_source) {
      sessionSources.add(workflow.safe_outputs_source);
      sessionReportedWorkflows++;
    }
    if (Object.hasOwn(workflow, "safe_output_delivery")) sessionDeliveryWorkflows++;
    if (Object.hasOwn(workflow, "safe_output_adoption")) sessionAdoptionWorkflows++;
    deliveries.set(id, comparable ? normalizeDelivery(workflow.safe_output_delivery) : normalizeDelivery(undefined));
    adoptions.set(id, comparable ? normalizeAdoption(workflow.safe_output_adoption) : normalizeAdoption(undefined));
  }
  const delivery = encodeObservations(presentIds, deliveries, DELIVERY_FIELDS, inventory.count);
  const adoption = encodeObservations(presentIds, adoptions, ADOPTION_FIELDS, inventory.count);
  const combinedCoverage =
    delivery.aggregate.coverage === "complete" && adoption.aggregate.coverage === "complete" ? "complete" : delivery.aggregate.coverage === "unavailable" && adoption.aggregate.coverage === "unavailable" ? "unavailable" : "partial";
  return {
    date: candidate.date,
    timestamp: typeof snapshot.timestamp === "string" ? snapshot.timestamp : null,
    metrics_source: candidate.path,
    collection_status: ["complete", "partial"].includes(snapshot.collection_status) ? snapshot.collection_status : "unavailable",
    comparability: comparable ? "exact_24h" : "incomparable",
    session_evidence: {
      source: sessionSources.size === 1 ? [...sessionSources][0] : sessionSources.size > 1 ? "mixed" : null,
      reported_workflows: sessionReportedWorkflows,
      delivery_workflows: sessionDeliveryWorkflows,
      adoption_workflows: sessionAdoptionWorkflows,
    },
    inventory: inventoryMembership(presentIds, inventoryIds, unknownIds),
    selected_window: {
      delivery,
      adoption,
      aggregate: {
        coverage: combinedCoverage,
        delivered: delivery.aggregate.delivered,
        ...Object.fromEntries(ADOPTION_FIELDS.map(field => [field, adoption.aggregate[field]])),
        count_semantics: combinedCoverage === "complete" ? "complete_current_inventory" : combinedCoverage === "partial" ? "observed_only" : "unavailable",
        workflow_coverage: {
          complete: Math.min(delivery.aggregate.workflow_coverage.complete, adoption.aggregate.workflow_coverage.complete),
          partial: Math.max(delivery.aggregate.workflow_coverage.partial, adoption.aggregate.workflow_coverage.partial),
          unavailable: Math.max(delivery.aggregate.workflow_coverage.unavailable, adoption.aggregate.workflow_coverage.unavailable),
        },
      },
    },
  };
}

/** @param {Record<string, any>} inventory */
function lifecycleLookup(inventory) {
  const lookup = new Map();
  for (const workflow of inventory.workflows) {
    for (const key of [workflow.id, workflow.source_path, workflow.lock_path]) {
      if (lookup.has(key)) throw new Error(`${ERR_VALIDATION}: Duplicate lifecycle workflow selector: ${key}`);
      lookup.set(key, workflow.id);
    }
  }
  return lookup;
}

/** @param {Record<string, any>} candidate @param {Record<string, any>} inventory */
function normalizeLifecyclePeriod(candidate, inventory) {
  const snapshot = candidate.value;
  const inventoryIds = inventory.workflows.map(workflow => workflow.id);
  if (!snapshot || ![1, 2].includes(snapshot.schema_version) || snapshot.scope !== "retained_deliveries" || !["complete", "partial"].includes(snapshot.coverage)) {
    const unavailable = new Map(inventoryIds.map(id => [id, normalizeRetained({}, "unavailable")]));
    const empty = encodeObservations(inventoryIds, unavailable, [...RETAINED_FIELDS, "accepted"], inventory.count);
    return {
      lifecycle_source: candidate.path,
      retained_population: {
        observations: empty,
        aggregate: { ...empty.aggregate, accepted: null },
        source_totals: normalizeRetained({}, "unavailable"),
        unmatched_workflow_sources: [],
        ledger: null,
      },
    };
  }
  const lookup = lifecycleLookup(inventory);
  const mapped = new Map();
  const unmatched = [];
  for (const key of Object.keys(snapshot.workflows || {}).sort()) {
    const id = lookup.get(key);
    if (!id) {
      unmatched.push(key);
      continue;
    }
    if (mapped.has(id)) throw new Error(`${ERR_VALIDATION}: Retained lifecycle snapshot duplicates workflow identity: ${id}`);
    mapped.set(id, normalizeRetained(snapshot.workflows[key], snapshot.coverage === "complete" ? "complete" : "partial"));
  }
  const observations = new Map();
  for (const id of inventoryIds) {
    if (mapped.has(id)) observations.set(id, mapped.get(id));
    else if (snapshot.coverage === "complete") observations.set(id, normalizeRetained(Object.fromEntries(RETAINED_FIELDS.map(field => [field, 0])), "complete"));
    else observations.set(id, normalizeRetained({}, "unavailable"));
  }
  const encoded = encodeObservations(inventoryIds, observations, [...RETAINED_FIELDS, "accepted"], inventory.count);
  const sourceTotals = normalizeRetained(snapshot.totals, snapshot.coverage);
  return {
    lifecycle_source: candidate.path,
    retained_population: {
      observations: encoded,
      aggregate: encoded.aggregate,
      source_totals: sourceTotals,
      unmatched_workflow_sources: unmatched,
      ledger: snapshot.ledger || null,
      discovery_complete: snapshot.discovery_complete === true,
      discovery_completed_until: snapshot.discovery_completed_until ?? null,
      unavailable_runs: Number.isSafeInteger(snapshot.unavailable_runs) ? snapshot.unavailable_runs : null,
      deferred: Number.isSafeInteger(snapshot.deferred) ? snapshot.deferred : null,
    },
  };
}

/** @param {Record<string, any>} encoded @param {string} id */
function observationFor(encoded, id) {
  return encoded.overrides[id] || encoded.default;
}

/** @param {Record<string, any>} inventory @param {{start: string, end: string, coverage: string} | null} selection */
function unavailableLifecycleLedger(inventory, selection) {
  const inventoryIds = inventory.workflows.map(workflow => workflow.id);
  const deliveries = new Map(
    inventoryIds.map(id => [
      id,
      {
        coverage: "unavailable",
        delivered: null,
        reason_code: "canonical_lifecycle_ledger_missing",
      },
    ])
  );
  const adoptions = new Map(
    inventoryIds.map(id => [
      id,
      {
        coverage: "unavailable",
        ...Object.fromEntries(ADOPTION_FIELDS.map(field => [field, null])),
        reason_code: "canonical_lifecycle_ledger_missing",
      },
    ])
  );
  const delivery = encodeObservations(inventoryIds, deliveries, DELIVERY_FIELDS, inventory.count);
  const adoption = encodeObservations(inventoryIds, adoptions, ADOPTION_FIELDS, inventory.count);
  return {
    descriptor: {
      memory_id: "lifecycle",
      branch: "memory/output-lifecycle",
      path: "lifecycle.jsonl",
      schema_version: 7,
      availability: "unavailable",
      reason_code: "canonical_lifecycle_ledger_missing",
      sha256: null,
      bytes: null,
      records: null,
      identity_fields: ["host", "repository", "workflow", "run_id", "run_attempt", "invocation_id", "evidence_id", "type", "target_url", "target_repository"],
      clock_fields: ["delivery.at", "delivery.observed_at", "adoption.at", "adoption.observed_at", "survival.observed_at", "reversed_at"],
    },
    workflows: {},
    unmatched_workflow_sources: {},
    selected_window: {
      source: "canonical_retained_lifecycle_ledger",
      window: selection ? { start: selection.start, end: selection.end } : null,
      semantics: "deliveries_in_window_with_current_adoption_snapshot",
      delivery,
      adoption,
      aggregate: {
        coverage: "unavailable",
        delivered: null,
        ...Object.fromEntries(ADOPTION_FIELDS.map(field => [field, null])),
        count_semantics: "unavailable",
        workflow_coverage: { complete: 0, partial: 0, unavailable: inventory.count },
      },
    },
  };
}

/**
 * @param {string} ledgerFile
 * @param {Record<string, any>} inventory
 * @param {{start: string, end: string, coverage: string} | null} selection
 */
function inspectLifecycleLedger(ledgerFile, inventory, selection) {
  if (!fs.existsSync(ledgerFile)) throw new Error(`${ERR_VALIDATION}: Retained lifecycle ledger is unavailable: ${ledgerFile}`);
  const content = readBounded(ledgerFile, MAX_LEDGER_BYTES);
  const seen = new Set();
  const lookup = lifecycleLookup(inventory);
  const evidence = new Map();
  const unmatched = new Map();
  const selected = new Map();
  const selectionStart = timestamp(selection?.start);
  const selectionEnd = timestamp(selection?.end);
  const hasSelection = selectionStart !== null && selectionEnd !== null && selectionEnd > selectionStart;
  const updateClock = (record, field, value, mode) => {
    const parsed = timestamp(value);
    if (parsed === null) return;
    const current = timestamp(record[field]);
    if (current === null || (mode === "minimum" ? parsed < current : parsed > current)) {
      const date = new Date(parsed);
      if (!Number.isFinite(date.getTime())) return;
      record[field] = date.toISOString();
    }
  };
  for (const event of readJsonlFileEvents(ledgerFile, { fatalUTF8: true, jsonWhitespaceOnly: true })) {
    if (event.type !== "json") {
      if (event.reason === "blank") continue;
      throw new Error(`${ERR_PARSE}: Invalid retained lifecycle JSONL at line ${event.lineNumber}`, { cause: event.error });
    }
    const row = event.value;
    const record = row?.lifecycle;
    if (row?.schema_version !== 7 || row.kind !== "output_lifecycle" || typeof record?.id !== "string" || seen.has(record.id)) {
      throw new Error(`${ERR_VALIDATION}: Invalid or duplicate retained lifecycle ledger record`);
    }
    seen.add(record.id);
    const id = lookup.get(record.workflow);
    if (!id) {
      unmatched.set(record.workflow || "<missing>", (unmatched.get(record.workflow || "<missing>") || 0) + 1);
      continue;
    }
    const deliveredAt = timestamp(record.delivery?.at);
    if (hasSelection && deliveredAt !== null && deliveredAt >= selectionStart && deliveredAt < selectionEnd) {
      const counts = selected.get(id) || Object.fromEntries([...RETAINED_FIELDS.map(field => [field, 0]), ["accepted", 0]]);
      counts.delivered++;
      const adoption = record.adoption;
      if (adoption?.coverage !== "available") counts.unknown++;
      else if (adoption.state === "accepted") {
        counts.accepted++;
        if (adoption.acceptance_kind === "explicit_human" && adoption.human_verified === true) counts.accepted_human++;
        else if (adoption.acceptance_kind === "fallback_delivery" && adoption.human_verified === false) counts.accepted_delivery_fallback++;
        else throw new Error(`${ERR_VALIDATION}: Invalid retained adoption acceptance in canonical ledger`);
      } else if (["rejected", "pending", "lifecycle", "not_applicable", "unknown"].includes(adoption.state)) counts[adoption.state]++;
      else throw new Error(`${ERR_VALIDATION}: Invalid retained adoption state in canonical ledger`);
      selected.set(id, counts);
    }
    const item = evidence.get(id) || {
      ledger_selector: { workflow: record.workflow },
      delivery_identities: 0,
      producer_runs: new Set(),
      hosts: new Set(),
      source_repositories: new Set(),
      target_repositories: new Set(),
      clocks: {
        first_delivery_at: null,
        last_delivery_at: null,
        last_delivery_observed_at: null,
        last_decision_at: null,
        last_adoption_observed_at: null,
        last_survival_observed_at: null,
        last_reversal_at: null,
      },
    };
    item.delivery_identities++;
    item.producer_runs.add(`${record.host}/${record.repository}/${record.workflow}/${record.run_id}/${record.run_attempt}`);
    if (typeof record.host === "string" && record.host) item.hosts.add(record.host);
    if (typeof record.repository === "string" && record.repository) item.source_repositories.add(record.repository);
    if (typeof record.target_repository === "string" && record.target_repository) item.target_repositories.add(record.target_repository);
    updateClock(item.clocks, "first_delivery_at", record.delivery?.at, "minimum");
    updateClock(item.clocks, "last_delivery_at", record.delivery?.at, "maximum");
    updateClock(item.clocks, "last_delivery_observed_at", record.delivery?.observed_at, "maximum");
    updateClock(item.clocks, "last_decision_at", record.adoption?.at, "maximum");
    updateClock(item.clocks, "last_adoption_observed_at", record.adoption?.observed_at, "maximum");
    updateClock(item.clocks, "last_survival_observed_at", record.survival?.observed_at, "maximum");
    updateClock(item.clocks, "last_reversal_at", record.reversed_at, "maximum");
    evidence.set(id, item);
  }
  const selectionCoverage = selection?.coverage === "complete" ? "complete" : selection?.coverage === "partial" ? "partial" : "unavailable";
  const selectedDeliveries = new Map();
  const selectedAdoptions = new Map();
  const inventoryIds = inventory.workflows.map(workflow => workflow.id);
  for (const id of inventoryIds) {
    const counts = selected.get(id);
    if (counts) {
      selectedDeliveries.set(id, { coverage: selectionCoverage, delivered: counts.delivered, reason_code: null });
      selectedAdoptions.set(id, {
        coverage: selectionCoverage,
        ...Object.fromEntries(ADOPTION_FIELDS.map(field => [field, counts[field]])),
        reason_code: null,
      });
    } else if (selectionCoverage === "complete") {
      selectedDeliveries.set(id, { coverage: "complete", delivered: 0, reason_code: null });
      selectedAdoptions.set(id, {
        coverage: "complete",
        ...Object.fromEntries(ADOPTION_FIELDS.map(field => [field, 0])),
        reason_code: null,
      });
    } else {
      selectedDeliveries.set(id, { coverage: "unavailable", delivered: null, reason_code: "selected_ledger_window_incomplete" });
      selectedAdoptions.set(id, {
        coverage: "unavailable",
        ...Object.fromEntries(ADOPTION_FIELDS.map(field => [field, null])),
        reason_code: "selected_ledger_window_incomplete",
      });
    }
  }
  const selectedDelivery = encodeObservations(inventoryIds, selectedDeliveries, DELIVERY_FIELDS, inventory.count);
  const selectedAdoption = encodeObservations(inventoryIds, selectedAdoptions, ADOPTION_FIELDS, inventory.count);
  const combinedCoverage =
    selectedDelivery.aggregate.coverage === "complete" && selectedAdoption.aggregate.coverage === "complete"
      ? "complete"
      : selectedDelivery.aggregate.coverage === "unavailable" && selectedAdoption.aggregate.coverage === "unavailable"
        ? "unavailable"
        : "partial";
  return {
    descriptor: {
      memory_id: "lifecycle",
      branch: "memory/output-lifecycle",
      path: "lifecycle.jsonl",
      schema_version: 7,
      availability: "available",
      reason_code: null,
      sha256: sha256(content),
      bytes: content.length,
      records: seen.size,
      identity_fields: ["host", "repository", "workflow", "run_id", "run_attempt", "invocation_id", "evidence_id", "type", "target_url", "target_repository"],
      clock_fields: ["delivery.at", "delivery.observed_at", "adoption.at", "adoption.observed_at", "survival.observed_at", "reversed_at"],
    },
    workflows: Object.fromEntries(
      [...evidence.entries()]
        .sort(([a], [b]) => a.localeCompare(b))
        .map(([id, item]) => [
          id,
          {
            ledger_selector: item.ledger_selector,
            delivery_identities: item.delivery_identities,
            producer_runs: item.producer_runs.size,
            hosts: [...item.hosts].sort(),
            source_repositories: [...item.source_repositories].sort(),
            target_repositories: [...item.target_repositories].sort(),
            clocks: item.clocks,
          },
        ])
    ),
    unmatched_workflow_sources: Object.fromEntries([...unmatched.entries()].sort(([a], [b]) => a.localeCompare(b))),
    selected_window: {
      source: "canonical_retained_lifecycle_ledger",
      window: hasSelection && selection ? { start: selection.start, end: selection.end } : null,
      semantics: "deliveries_in_window_with_current_adoption_snapshot",
      delivery: selectedDelivery,
      adoption: selectedAdoption,
      aggregate: {
        coverage: combinedCoverage,
        delivered: selectedDelivery.aggregate.delivered,
        ...Object.fromEntries(ADOPTION_FIELDS.map(field => [field, selectedAdoption.aggregate[field]])),
        count_semantics: combinedCoverage === "complete" ? "complete_current_inventory" : combinedCoverage === "partial" ? "observed_only" : "unavailable",
        workflow_coverage: {
          complete: Math.min(selectedDelivery.aggregate.workflow_coverage.complete, selectedAdoption.aggregate.workflow_coverage.complete),
          partial: Math.max(selectedDelivery.aggregate.workflow_coverage.partial, selectedAdoption.aggregate.workflow_coverage.partial),
          unavailable: Math.max(selectedDelivery.aggregate.workflow_coverage.unavailable, selectedAdoption.aggregate.workflow_coverage.unavailable),
        },
      },
    },
  };
}

/** @param {Record<string, any>} metricPeriod @param {Record<string, any> | undefined} lifecyclePeriod @param {Record<string, any>} inventory @param {boolean} current */
function combinePeriod(metricPeriod, lifecyclePeriod, inventory, current) {
  const inventoryIds = inventory.workflows.map(workflow => workflow.id);
  const unavailable = new Map(inventoryIds.map(id => [id, normalizeRetained({}, "unavailable")]));
  const unavailableRetained = encodeObservations(inventoryIds, unavailable, [...RETAINED_FIELDS, "accepted"], inventory.count);
  const retained = lifecyclePeriod?.retained_population || {
    observations: unavailableRetained,
    aggregate: unavailableRetained.aggregate,
    source_totals: normalizeRetained({}, "unavailable"),
    unmatched_workflow_sources: [],
    ledger: null,
    discovery_complete: false,
    discovery_completed_until: null,
    unavailable_runs: null,
    deferred: null,
  };
  return {
    ...metricPeriod,
    lifecycle_source: lifecyclePeriod?.lifecycle_source ?? null,
    retained_population: retained,
    attributed_value: current
      ? {
          status_coverage: "complete",
          measurement_coverage: "unavailable",
          default: { state: "needs_definition", reason_code: "reviewed_longitudinal_definition_missing", unit: null, value: null },
          overrides: {},
          state_counts: { measured: 0, needs_definition: inventory.count, unmeasured: 0, unavailable: 0 },
        }
      : {
          status_coverage: "unavailable",
          measurement_coverage: "unavailable",
          default: { state: "unavailable", reason_code: "historical_definition_and_observation_evidence_absent", unit: null, value: null },
          overrides: {},
          state_counts: { measured: null, needs_definition: null, unmeasured: null, unavailable: metricPeriod.inventory.reported_count || null },
        },
  };
}

/** @param {string} metricsDir @param {string} currentDate */
function pruneUploadHistory(metricsDir, currentDate) {
  const cutoff = new Date(`${currentDate}T00:00:00Z`);
  if (!Number.isFinite(cutoff.getTime())) throw new Error(`${ERR_VALIDATION}: Invalid workflow value retention date`);
  cutoff.setUTCDate(cutoff.getUTCDate() - (RETAINED_UPLOAD_DAYS - 1));
  const oldestDay = cutoff.toISOString().slice(0, 10);
  let removed = 0;
  for (const directory of [path.join(metricsDir, "daily"), path.join(metricsDir, "lifecycle", "daily")]) {
    if (!fs.existsSync(directory)) continue;
    let names;
    try {
      names = fs.readdirSync(directory);
    } catch (error) {
      throw new Error(`${ERR_SYSTEM}: Failed to list workflow value upload history: ${directory}`, { cause: error });
    }
    for (const name of names) {
      if (!/^\d{4}-\d{2}-\d{2}\.json$/.test(name)) continue;
      const date = name.slice(0, 10);
      if (date > currentDate) throw new Error(`${ERR_VALIDATION}: Retained workflow value history contains a future period: ${name}`);
      if (date < oldestDay) {
        try {
          fs.unlinkSync(path.join(directory, name));
        } catch (error) {
          throw new Error(`${ERR_SYSTEM}: Failed to prune workflow value upload history: ${path.join(directory, name)}`, { cause: error });
        }
        removed++;
      }
    }
  }
  return { oldest_day: oldestDay, retained_days: RETAINED_UPLOAD_DAYS, removed_files: removed };
}

/**
 * @param {{
 *   repoRoot: string,
 *   metricsDir: string,
 *   lifecycleLedgerFile: string,
 *   outputDir: string,
 *   repository: string,
 *   stepSummaryPath?: string,
 *   prune?: boolean,
 *   allowMissingLifecycle?: boolean,
 * }} options
 */
function generateWorkflowValueReport({ repoRoot, metricsDir, lifecycleLedgerFile, outputDir, repository, stepSummaryPath, prune = true, allowMissingLifecycle = false }) {
  if (!repository || !/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(repository)) throw new Error(`${ERR_CONFIG}: Workflow value reporting requires OWNER/REPO`);
  const inventory = loadWorkflowInventory(repoRoot);
  const currentMetricFile = path.join(metricsDir, "latest.json");
  const currentMetric = readJSON(currentMetricFile).value;
  const currentDate = dateString(currentMetric?.timestamp);
  if (!currentDate || !currentMetric.workflows || Array.isArray(currentMetric.workflows) || typeof currentMetric.workflows !== "object") {
    throw new Error(`${ERR_VALIDATION}: Current metrics snapshot is missing a valid timestamp or workflow inventory`);
  }
  const currentKeys = Object.keys(currentMetric.workflows).sort();
  const inventoryIds = inventory.workflows.map(workflow => workflow.id);
  const missing = inventoryIds.filter(id => !Object.hasOwn(currentMetric.workflows, id));
  const extra = currentKeys.filter(id => !inventoryIds.includes(id));
  if (missing.length || extra.length) {
    throw new Error(`${ERR_VALIDATION}: Current metrics must cover the exact repository workflow inventory; missing: ${missing.join(", ") || "none"}; extra: ${extra.join(", ") || "none"}`);
  }
  if (currentMetric.ecosystem?.total_workflows !== inventory.count) {
    throw new Error(`${ERR_VALIDATION}: Current ecosystem workflow count does not match the repository inventory`);
  }

  const metricCandidates = readDatedCandidates(metricsDir, "metrics");
  const lifecycleCandidates = readDatedCandidates(metricsDir, "lifecycle");
  const selectedMetrics = selectLatestByDate(metricCandidates);
  const selectedLifecycle = selectLatestByDate(lifecycleCandidates);
  const dates = [...new Set([...selectedMetrics.keys(), ...selectedLifecycle.keys()])].sort();
  if (!dates.includes(currentDate)) throw new Error(`${ERR_VALIDATION}: Retained metrics history omits the current period`);
  /** @type {Array<Record<string, any>>} */
  const periods = dates.map(date => {
    const metricCandidate = selectedMetrics.get(date) || {
      date,
      path: null,
      value: null,
    };
    const metricPeriod = normalizeMetricPeriod(metricCandidate, inventory);
    const lifecycleCandidate = selectedLifecycle.get(date);
    const lifecyclePeriod = lifecycleCandidate ? normalizeLifecyclePeriod(lifecycleCandidate, inventory) : undefined;
    return combinePeriod(metricPeriod, lifecyclePeriod, inventory, date === currentDate);
  });
  const currentIndex = periods.findIndex(period => period.date === currentDate);
  const currentPeriod = periods[currentIndex];
  const ledgerSelection =
    exactDailyWindow(currentMetric) && typeof currentMetric.collection_window?.start === "string" && typeof currentMetric.collection_window?.end === "string"
      ? {
          start: currentMetric.collection_window.start,
          end: currentMetric.collection_window.end,
          coverage: currentPeriod.retained_population.source_totals.coverage,
        }
      : null;
  const hasLifecycleLedger = fs.existsSync(lifecycleLedgerFile);
  const ledger = hasLifecycleLedger ? inspectLifecycleLedger(lifecycleLedgerFile, inventory, ledgerSelection) : unavailableLifecycleLedger(inventory, ledgerSelection);
  if (!hasLifecycleLedger && !allowMissingLifecycle) {
    throw new Error(`${ERR_VALIDATION}: Retained lifecycle ledger is unavailable: ${lifecycleLedgerFile}`);
  }
  const projectedLedger = currentPeriod.retained_population.ledger;
  if (
    hasLifecycleLedger &&
    (!projectedLedger ||
      projectedLedger.schema_version !== ledger.descriptor.schema_version ||
      projectedLedger.sha256 !== ledger.descriptor.sha256 ||
      projectedLedger.bytes !== ledger.descriptor.bytes ||
      projectedLedger.records !== ledger.descriptor.records)
  ) {
    throw new Error(`${ERR_VALIDATION}: Retained lifecycle snapshot does not reference the restored canonical ledger`);
  }
  if (!hasLifecycleLedger && projectedLedger) throw new Error(`${ERR_VALIDATION}: Retained lifecycle snapshot references a canonical ledger that was not restored`);
  currentPeriod.selected_window = ledger.selected_window;
  currentPeriod.selected_window_source = ledger.selected_window.source;
  const missingLifecycleEvidence = ledger.descriptor.availability === "unavailable";
  const workflows = inventory.workflows.map(workflow => ({
    ...workflow,
    lifecycle_evidence: ledger.workflows[workflow.id] || {
      ledger_selector: { workflow: workflow.lock_path },
      coverage: missingLifecycleEvidence ? "unavailable" : "complete",
      reason_code: missingLifecycleEvidence ? "canonical_lifecycle_ledger_missing" : null,
      delivery_identities: missingLifecycleEvidence ? null : 0,
      producer_runs: missingLifecycleEvidence ? null : 0,
      hosts: [],
      source_repositories: [],
      target_repositories: [],
      clocks: {
        first_delivery_at: null,
        last_delivery_at: null,
        last_delivery_observed_at: null,
        last_decision_at: null,
        last_adoption_observed_at: null,
        last_survival_observed_at: null,
        last_reversal_at: null,
      },
    },
    current: {
      selected_window: {
        delivery: observationFor(currentPeriod.selected_window.delivery, workflow.id),
        adoption: observationFor(currentPeriod.selected_window.adoption, workflow.id),
      },
      retained_population: observationFor(currentPeriod.retained_population.observations, workflow.id),
      attributed_value: { state: "needs_definition", reason_code: "reviewed_longitudinal_definition_missing", unit: null, value: null },
    },
  }));
  const portfolio = {
    schema_version: 1,
    repository,
    generated_at: currentMetric.timestamp,
    source_revision: typeof process.env.GITHUB_SHA === "string" && /^[a-f0-9]{40}$/.test(process.env.GITHUB_SHA) ? process.env.GITHUB_SHA : null,
    inventory: {
      scope: inventory.scope,
      count: inventory.count,
      sha256: inventory.sha256,
    },
    sources: {
      metrics: {
        latest_path: "metrics/latest.json",
        daily_glob: "metrics/daily/YYYY-MM-DD.json",
        retained_periods: selectedMetrics.size,
        selection: "latest_observation_before_aggregation",
        current_selected_window: "canonical_lifecycle_ledger_delivery_cohort",
        historical_selected_windows: "recorded_daily_metrics_only",
        legacy_safe_output_outcomes: "ignored_not_lifecycle_or_value_evidence",
      },
      retained_lifecycle: {
        latest_path: "metrics/lifecycle.json",
        daily_glob: "metrics/lifecycle/daily/YYYY-MM-DD.json",
        retained_periods: selectedLifecycle.size,
        history: "prospective_only_unless_exact_retained_evidence_exists",
        ledger: ledger.descriptor,
      },
      attributed_value: {
        definition_source: null,
        state: "needs_definition",
        reason_code: "no_reviewed_frozen_repository_local_longitudinal_definitions",
        operational_value_scope: "run_scoped_optional_not_longitudinal_attributed_value",
      },
    },
    current: {
      date: currentDate,
      period_index: currentIndex,
      selected_window: currentPeriod.selected_window,
      retained_population: currentPeriod.retained_population,
      attributed_value: currentPeriod.attributed_value,
    },
    periods,
    workflows,
    unmatched_evidence: {
      ledger_workflow_sources: ledger.unmatched_workflow_sources,
      retained_snapshot_workflow_sources: [...new Set(periods.flatMap(period => period.retained_population.unmatched_workflow_sources))].sort(),
    },
    reconstruction: {
      workflow_history: "For each period, resolve a workflow observation from layer.overrides[workflow_id], otherwise layer.default; workflows outside period.inventory membership were not in that period inventory.",
      membership_modes: {
        current: "All current workflow identities were present.",
        present: "workflow_ids lists current identities present in the period.",
        missing_current: "workflow_ids lists current identities absent from the period.",
        none: "No workflow inventory was recorded.",
      },
    },
    artifact: {
      name: "workflow-value-report",
      files: ["workflow-value-portfolio.json", "workflow-value-history.json", "workflow-value-report.md", "plots/inventory-coverage.svg", "plots/selected-lifecycle.svg", "plots/retained-lifecycle.svg", "plots/value-evidence.svg"],
    },
  };
  const report = renderMarkdown(portfolio);
  const charts = renderCharts(portfolio);
  const historyPeriods = portfolio.periods;
  const reconstruction = portfolio.reconstruction;
  const fullWorkflows = portfolio.workflows;
  /** @type {Record<string, any>} */
  const currentPortfolio = { ...portfolio };
  delete currentPortfolio.periods;
  delete currentPortfolio.reconstruction;
  delete currentPortfolio.workflows;
  const emptyLifecycleEvidence = {
    ledger_selector: null,
    coverage: missingLifecycleEvidence ? "unavailable" : "complete",
    reason_code: missingLifecycleEvidence ? "canonical_lifecycle_ledger_missing" : null,
    delivery_identities: missingLifecycleEvidence ? null : 0,
    producer_runs: missingLifecycleEvidence ? null : 0,
    hosts: [],
    source_repositories: [],
    target_repositories: [],
    clocks: {
      first_delivery_at: null,
      last_delivery_at: null,
      last_delivery_observed_at: null,
      last_decision_at: null,
      last_adoption_observed_at: null,
      last_survival_observed_at: null,
      last_reversal_at: null,
    },
  };
  const lifecycleEvidenceOverrides = Object.fromEntries(
    fullWorkflows.filter(workflow => Number.isSafeInteger(workflow.lifecycle_evidence.delivery_identities) && workflow.lifecycle_evidence.delivery_identities > 0).map(workflow => [workflow.id, workflow.lifecycle_evidence])
  );
  currentPortfolio.workflows = fullWorkflows.map(({ current: _current, lifecycle_evidence: _lifecycleEvidence, ...workflow }) => workflow);
  currentPortfolio.lifecycle_evidence = {
    default: emptyLifecycleEvidence,
    overrides: lifecycleEvidenceOverrides,
    reconstruction: "Use overrides[workflow_id] when present; otherwise use default with ledger_selector set to the workflow lock_path.",
  };
  currentPortfolio.history = {
    path: "workflow-value-history.json",
    period_count: historyPeriods.length,
    start_date: historyPeriods[0]?.date ?? null,
    end_date: historyPeriods.at(-1)?.date ?? null,
  };
  const history = {
    schema_version: portfolio.schema_version,
    repository: portfolio.repository,
    generated_at: portfolio.generated_at,
    source_revision: portfolio.source_revision,
    inventory: portfolio.inventory,
    sources: portfolio.sources,
    current_period_index: portfolio.current.period_index,
    periods: historyPeriods,
    reconstruction,
  };
  /** @type {Array<[string, string]>} */
  const artifactFiles = [];
  artifactFiles.push(["workflow-value-portfolio.json", JSON.stringify(currentPortfolio) + "\n"], ["workflow-value-history.json", JSON.stringify(history) + "\n"], ["workflow-value-report.md", report + "\n"]);
  for (const [name, content] of Object.entries(charts)) artifactFiles.push([`plots/${name}`, content]);
  const files = new Map(artifactFiles);
  const totalBytes = validateArtifactFiles(files);
  for (const [name, content] of files) writeAtomic(path.join(outputDir, name), content);
  if (stepSummaryPath) {
    try {
      const summaryReport = report.replace(/^!\[([^\]]+)\]\(plots\/[^)]+\)$/gm, "- $1: see the `workflow-value-report` artifact.");
      fs.appendFileSync(stepSummaryPath, `${summaryReport}\n`);
    } catch (error) {
      throw new Error(`${ERR_SYSTEM}: Failed to append the workflow value step summary`, { cause: error });
    }
  }
  const retention = prune ? pruneUploadHistory(metricsDir, currentDate) : { retained_days: RETAINED_UPLOAD_DAYS, oldest_day: null, removed_files: 0 };
  return { portfolio, retention, output_bytes: totalBytes };
}

if (require.main === module) {
  const repoRoot = process.env.GH_AW_WORKFLOW_VALUE_REPO_ROOT || process.env.GITHUB_WORKSPACE || path.resolve(__dirname, "../../..");
  const result = generateWorkflowValueReport({
    repoRoot,
    metricsDir: process.env.GH_AW_WORKFLOW_VALUE_METRICS_DIR || "/tmp/gh-aw/repo-memory/default/metrics",
    lifecycleLedgerFile: process.env.GH_AW_WORKFLOW_VALUE_LIFECYCLE_LEDGER || "/tmp/gh-aw/repo-memory/lifecycle/lifecycle.jsonl",
    outputDir: process.env.GH_AW_WORKFLOW_VALUE_OUTPUT_DIR || "/tmp/gh-aw/workflow-value-report",
    repository: process.env.EXPR_GITHUB_REPOSITORY || process.env.GITHUB_REPOSITORY || "",
    stepSummaryPath: process.env.GITHUB_STEP_SUMMARY,
    prune: process.env.GH_AW_WORKFLOW_VALUE_PRUNE !== "false",
    allowMissingLifecycle: process.env.GH_AW_WORKFLOW_VALUE_ALLOW_MISSING_LIFECYCLE === "true",
  });
  process.stdout.write(
    `Workflow value report generated for ${result.portfolio.inventory.count} workflow(s), ${result.portfolio.periods.length} retained period(s), ${result.output_bytes} byte(s); pruned ${result.retention.removed_files} upload history file(s).\n`
  );
}

module.exports = {
  declaresOperationalValue,
  generateWorkflowValueReport,
  loadWorkflowInventory,
  normalizeAdoption,
  normalizeDelivery,
  pruneUploadHistory,
  validateArtifactFiles,
};
