// @ts-check
"use strict";

const crypto = require("node:crypto");
const { canonicalJSON } = require("./ledger_store.cjs");
const { validateValueAgainstSchema } = require("./mcp_scripts_validation.cjs");

const OPERATIONS = ["submit", "acquire", "acquire-next", "finish", "abandon", "cancel"];
const FACTS = ["work", "claim", "release", "completion", "cancellation"];
const STRUCTURAL = new Set(["work_id", "claim_id", "claimant", "generation", "previous_claim_id"]);

function digest(value) {
  return crypto.createHash("sha256").update(canonicalJSON(value)).digest("hex");
}

function workId(work, config) {
  const identity = config.identity ? Object.fromEntries(config.identity.map(field => [field, work[field]])) : work;
  return `work-${digest(identity)}`;
}

function checkWork(work, config) {
  if (!work || typeof work !== "object" || Array.isArray(work) || Object.keys(work).some(key => STRUCTURAL.has(key))) throw new TypeError("Invalid Work payload");
  canonicalJSON(work);
  if (config.identity && config.identity.some(field => !Object.hasOwn(work, field) || work[field] === null)) throw new TypeError("Missing Work identity field");
  if (config.schema) {
    const error = validateValueAgainstSchema(work, config.schema);
    if (error) throw new TypeError("Work does not match ledger schema");
  }
}

function validateIntent(intent, config) {
  if (!intent || !OPERATIONS.includes(intent.operation)) throw new TypeError("Unsupported work-pool operation");
  const fields = {
    submit: ["work"],
    acquire: ["work"],
    "acquire-next": ["filter"],
    finish: ["result"],
    abandon: ["reason"],
    cancel: ["work"],
  }[intent.operation];
  if (Object.keys(intent).some(key => key !== "operation" && !fields.includes(key))) throw new TypeError("Invalid work-pool operation fields");
  if (["submit", "acquire", "cancel"].includes(intent.operation)) checkWork(intent.work, config);
  if (intent.operation === "acquire-next" && intent.filter !== undefined) {
    if (!intent.filter || typeof intent.filter !== "object" || Array.isArray(intent.filter) || Object.keys(intent.filter).some(key => STRUCTURAL.has(key))) throw new TypeError("Invalid work-pool filter");
    canonicalJSON(intent.filter);
  }
  if (intent.operation === "finish" && intent.result !== undefined) canonicalJSON(intent.result);
  if (intent.operation === "abandon" && intent.reason !== undefined && typeof intent.reason !== "string") throw new TypeError("Invalid abandonment reason");
}

function validateFact(fact, config) {
  if (!fact || !FACTS.includes(fact.operation)) throw new TypeError("Invalid work-pool fact");
  const fields = {
    work: ["work_id", "work"],
    claim: ["claim_id", "work_id", "claimant", "previous_claim_id"],
    release: ["claim_id", "reason"],
    completion: ["claim_id", "result"],
    cancellation: ["work_id"],
  }[fact.operation];
  if (Object.keys(fact).some(key => key !== "operation" && key !== "id" && !fields.includes(key))) throw new TypeError("Invalid work-pool fact fields");
  if (fact.operation === "work") {
    checkWork(fact.work, config);
    if (fact.work_id !== workId(fact.work, config)) throw new TypeError("Invalid Work identity");
  } else if (fact.operation === "claim") {
    if (
      typeof fact.work_id !== "string" ||
      typeof fact.claimant !== "string" ||
      !fact.claimant ||
      fact.claim_id !== claimId(fact.work_id, fact.claimant, fact.previous_claim_id || null) ||
      (fact.previous_claim_id !== null && typeof fact.previous_claim_id !== "string")
    )
      throw new TypeError("Invalid Claim identity");
  } else if (fact.operation === "release" || fact.operation === "completion") {
    if (typeof fact.claim_id !== "string") throw new TypeError("Invalid Claim reference");
    if (fact.operation === "completion" && fact.result !== undefined) canonicalJSON(fact.result);
    if (fact.operation === "release" && fact.reason !== undefined && typeof fact.reason !== "string") throw new TypeError("Invalid release reason");
  } else if (typeof fact.work_id !== "string") throw new TypeError("Invalid Work reference");
}

function claimId(work_id, claimant, previous_claim_id) {
  return `claim-${digest([work_id, claimant, previous_claim_id])}`;
}

function createReducer(config) {
  const facts = new Map();
  function apply(fact) {
    validateFact(fact, config);
    const identity = {
      work: fact.work_id,
      claim: fact.claim_id,
      release: fact.claim_id,
      completion: fact.claim_id,
      cancellation: fact.work_id,
    }[fact.operation];
    const key = `${fact.operation}:${identity}`;
    const prior = facts.get(key);
    const value = { ...fact };
    delete value.id;
    if (prior && canonicalJSON(prior) !== canonicalJSON(value)) throw new TypeError("Conflicting work-pool fact");
    facts.set(key, value);
  }
  function snapshot() {
    const work = new Map();
    const claims = new Map();
    const byOperation = operation => [...facts.values()].filter(fact => fact.operation === operation);
    for (const fact of byOperation("work")) work.set(fact.work_id, { work_id: fact.work_id, payload: fact.work, state: "available", effective_claim_id: null });
    for (const fact of byOperation("claim")) {
      if (!work.has(fact.work_id)) throw new TypeError("Claim references missing Work");
      claims.set(fact.claim_id, {
        claim_id: fact.claim_id,
        work_id: fact.work_id,
        claimant: fact.claimant,
        previous_claim_id: fact.previous_claim_id || null,
        generation: 0,
        released: false,
        effective: false,
        superseded: false,
      });
    }
    const visiting = new Set();
    function generation(claim) {
      if (visiting.has(claim.claim_id)) throw new TypeError("Cyclic Claim ancestry");
      if (!claim.previous_claim_id) return 0;
      const parent = claims.get(claim.previous_claim_id);
      if (!parent || parent.work_id !== claim.work_id) throw new TypeError("Invalid Claim ancestry");
      visiting.add(claim.claim_id);
      const number = generation(parent) + 1;
      visiting.delete(claim.claim_id);
      if (!Number.isSafeInteger(number)) throw new RangeError("Claim generation exceeds safe integer range");
      return number;
    }
    for (const claim of claims.values()) claim.generation = generation(claim);
    for (const fact of byOperation("release")) {
      const claim = claims.get(fact.claim_id);
      if (!claim) throw new TypeError("Release references missing Claim");
      claim.released = true;
    }
    for (const claim of claims.values()) {
      if (claim.previous_claim_id && !claims.get(claim.previous_claim_id).released) throw new TypeError("Claim predecessor must be released");
    }
    for (const fact of byOperation("completion")) if (!claims.has(fact.claim_id)) throw new TypeError("Completion references missing Claim");
    for (const fact of byOperation("cancellation")) if (!work.has(fact.work_id)) throw new TypeError("Cancellation references missing Work");
    const cancelled = new Set(byOperation("cancellation").map(fact => fact.work_id));
    const completed = new Set(byOperation("completion").map(fact => fact.claim_id));
    for (const item of work.values()) {
      const candidates = [...claims.values()].filter(claim => claim.work_id === item.work_id);
      candidates.sort((a, b) => b.generation - a.generation || (a.claim_id < b.claim_id ? -1 : a.claim_id > b.claim_id ? 1 : 0));
      const winner = candidates[0];
      for (const claim of candidates) claim.superseded = claim !== winner;
      if (cancelled.has(item.work_id)) item.state = "cancelled";
      else if (candidates.some(claim => completed.has(claim.claim_id))) item.state = "completed";
      else if (winner && !winner.released) {
        item.state = "claimed";
        item.effective_claim_id = winner.claim_id;
        winner.effective = true;
      }
    }
    const active = new Set();
    for (const claim of claims.values()) {
      if (!claim.effective) continue;
      if (active.has(claim.claimant)) throw new TypeError("Execution has more than one active acquisition");
      active.add(claim.claimant);
    }
    return { work, claims };
  }
  function transition(intent, claimant) {
    validateIntent(intent, config);
    if (typeof claimant !== "string" || !claimant) throw new TypeError("Trusted execution identity is required");
    const { work, claims } = snapshot();
    const active = [...claims.values()].find(claim => claim.claimant === claimant && claim.effective);
    const additions = [];
    const ensureWork = payload => {
      const id = workId(payload, config);
      const existing = work.get(id);
      if (existing && canonicalJSON(existing.payload) !== canonicalJSON(payload)) return null;
      if (!existing) additions.push({ operation: "work", work_id: id, work: payload });
      return { id, item: existing || { state: "available", payload } };
    };
    let target;
    if (["submit", "acquire", "cancel"].includes(intent.operation)) {
      target = ensureWork(intent.work);
      if (!target) return { outcome: "invalid", facts: [] };
    }
    if (intent.operation === "submit") return { outcome: "submitted", facts: additions };
    if (intent.operation === "cancel") {
      if (target.item.state !== "cancelled") additions.push({ operation: "cancellation", work_id: target.id });
      return { outcome: "cancelled", facts: additions };
    }
    if (intent.operation === "acquire-next") {
      if (active) return { outcome: "acquired", facts: [] };
      if ([...claims.values()].some(claim => claim.claimant === claimant)) return { outcome: "no_work", facts: [] };
      const matching = [...work.values()].filter(
        item => item.state === "available" && Object.entries(intent.filter || {}).every(([key, value]) => Object.hasOwn(item.payload, key) && canonicalJSON(item.payload[key]) === canonicalJSON(value))
      );
      matching.sort((a, b) => (a.work_id < b.work_id ? -1 : a.work_id > b.work_id ? 1 : 0));
      if (!matching.length) return { outcome: "no_work", facts: [] };
      target = { id: matching[0].work_id, item: matching[0] };
    }
    if (intent.operation === "acquire" || intent.operation === "acquire-next") {
      if (active && active.work_id !== target.id) return { outcome: "invalid", facts: [] };
      if (target.item.state === "cancelled") return { outcome: "cancelled", facts: [] };
      if (target.item.state === "completed") return { outcome: "already_done", facts: [] };
      const mine = [...claims.values()].find(claim => claim.work_id === target.id && claim.claimant === claimant);
      if (mine) return { outcome: mine.effective ? "acquired" : "busy", facts: [] };
      const previous = [...claims.values()].filter(claim => claim.work_id === target.id).sort((a, b) => b.generation - a.generation || (a.claim_id < b.claim_id ? -1 : 1))[0];
      const predecessor = previous?.released ? previous.claim_id : null;
      const fact = { operation: "claim", work_id: target.id, claimant, previous_claim_id: predecessor, claim_id: claimId(target.id, claimant, predecessor) };
      additions.push(fact);
      const preview = createReducer(config);
      for (const record of facts.values()) preview.apply(record);
      for (const record of additions) preview.apply(record);
      const effective = preview.snapshot().claims.get(fact.claim_id)?.effective;
      return { outcome: effective ? "acquired" : "busy", facts: additions };
    }
    const mine = [...claims.values()].find(claim => claim.claimant === claimant && (claim.effective || !claim.released || intent.operation === "abandon" || intent.operation === "finish"));
    if (!mine) return { outcome: "nothing_acquired", facts: [] };
    if (intent.operation === "abandon" && mine.released) return { outcome: "already_abandoned", facts: [] };
    if (intent.operation === "finish" && work.get(mine.work_id).state === "completed" && facts.has(`completion:${mine.claim_id}`)) return { outcome: "already_finished", facts: [] };
    if (!mine.effective) return { outcome: "ownership_lost", facts: [] };
    if (intent.operation === "finish") {
      if (work.get(mine.work_id).state === "completed") return { outcome: "already_finished", facts: [] };
      return { outcome: "finished", facts: [{ operation: "completion", claim_id: mine.claim_id, ...(intent.result === undefined ? {} : { result: intent.result }) }] };
    }
    return { outcome: "abandoned", facts: [{ operation: "release", claim_id: mine.claim_id, ...(intent.reason === undefined ? {} : { reason: intent.reason }) }] };
  }
  function output() {
    const state = snapshot();
    return {
      version: 1,
      tables: {
        work: {
          columns: { work_id: "text", payload: "text", state: "text", effective_claim_id: "text" },
          primaryKey: ["work_id"],
          rows: [...state.work.values()].sort((a, b) => (a.work_id < b.work_id ? -1 : 1)).map(item => ({ ...item, payload: canonicalJSON(item.payload) })),
        },
        claims: {
          columns: { claim_id: "text", work_id: "text", claimant: "text", generation: "integer", previous_claim_id: "text", released: "boolean", effective: "boolean", superseded: "boolean" },
          primaryKey: ["claim_id"],
          rows: [...state.claims.values()].sort((a, b) => (a.claim_id < b.claim_id ? -1 : 1)),
        },
      },
    };
  }
  return { apply, output, snapshot, transition };
}

module.exports = { OPERATIONS, claimId, createReducer, validateFact, validateIntent, workId };
