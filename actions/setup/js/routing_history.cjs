// @ts-check
"use strict";

/**
 * The routing history's reducer: applies one finished run to a workflow's `state.json` and
 * `runs.jsonl` on its `memory/adaptive-routing/<workflow>` branch.
 *
 * This is the reference bandit's record() with the v2 product rules (gh-aw adaptive routing):
 * a run counts only once, behind the reset and generation fences; a live run counts only with a
 * selection receipt bound to it; a suspended pair settles on any result; and the state keeps
 * each pair's newest 50 outcomes within 60 days. The oracle vectors in
 * fixtures/routing-history/aw2-reducer.json pin every result. The function is pure, so the save
 * loop can re-apply it to the newest files when another writer moved the branch.
 */

const DAY_SECONDS = 86400;
const MEMORY_RUNS = 50;
const MEMORY_SECONDS = 60 * DAY_SECONDS;
const PRIOR_WINS = 4.5;
const PRIOR_LOSSES = 0.5;
const CAP = 0.999;
const DECAY = 0.98;
const STREAK_NEW = 3;
const STREAK_PROVEN = 5;
const PROVEN_RUNS = 20;

/** Contract 5's bounds. Sizes are UTF-8 bytes of JSON.stringify output. */
const BOUNDS = Object.freeze({ history: MEMORY_RUNS, pairs: 32, scopes: 4, state_bytes: 512 * 1024, runs_lines: 500, runs_bytes: 512 * 1024, unresolved_runs: 200 });

/** Notes on runs.jsonl lines that did not count, besides the observation's skip reasons. */
const NOTES = Object.freeze(["before_reset", "older_definition", "unsaved", "no_agent", "receipt_missing", "receipt_mismatch", "awaiting_success"]);

/**
 * @typedef {[number, string, boolean, number | null, number | null, (number | null)[] | null]} HistoryEntry
 * @typedef {{ since_seq: number, pending: number }} Suspension
 * @typedef {{ history: HistoryEntry[], credits: number | null, model_credits: number | null, bills: number, streak: number, tests: number, suspended: Suspension | null, seen: string, summary: { runs: number, rate: number, hopeful: number } | null }} PairRecord
 * @typedef {{ incumbent: string | null, last_role: string | null, last_success_seq: number, pairs: Record<string, PairRecord> }} ScopeRecord
 * @typedef {{ v: number, workflow: string, seq: number, generation: { hash: string, since_run: number } | null, reset_after_run: number, last_run: number, reconciled_through?: number, unresolved_runs?: number[], unresolved_run_ids?: Record<string, string>, unexplained: number, scopes: Record<string, ScopeRecord>, updated: { run: string | null, at: string } | null }} HistoryState
 * @typedef {{ run: string, number: number, attempt: number | null, at: string, generation: string | null, scope: string | null, mode: string | null, role: string | null, pair: string | null, outcome: string | null, credits: number | null, counted: boolean, note: string | null }} RunLine
 * @typedef {{ credits?: number | null, model_credits?: number | null, tokens?: (number | null)[] | null }} RunCost
 * @typedef {{ id: string, number: number, conclusion: string, agent_ran?: boolean | null, generation?: string | null }} ListedRun
 * @typedef {{ verified: boolean, ceiling?: number, unresolved?: number[], unresolved_ids?: Record<string, string>, resolved?: number[] }} ReconciliationCoverage
 * @typedef {{ workflow: string, run: { id: string, number: number, attempt: number }, at: string, generation: string, mode: string, scope: string, observation: { outcome: string, reason?: string } | null, pair: string | null, cost?: RunCost | null, receipt?: Record<string, any> | null, prepare?: { explore_allowed?: boolean } | null, listing?: ListedRun[], reconciliation?: ReconciliationCoverage | null }} RunEvidence
 */

/** @param {string} at */
function seconds(at) {
  return Date.parse(at) / 1000;
}

/** @param {unknown} value */
function sizeOf(value) {
  return Buffer.byteLength(JSON.stringify(value), "utf8");
}

/**
 * The entries a rate counts: the newest 50 within 60 days of `now` (seconds).
 * @param {HistoryEntry[]} history
 * @param {number} now
 */
function memory(history, now) {
  return history.filter(entry => now - seconds(entry[1]) < MEMORY_SECONDS).slice(-MEMORY_RUNS);
}

/**
 * Discounted wins and losses: the newest entry weighs 1 and each earlier one 0.98 times the next.
 * @param {HistoryEntry[]} entries
 */
function counts(entries) {
  let wins = 0;
  let losses = 0;
  for (let k = 0; k < entries.length; k += 1) {
    const weight = Math.pow(DECAY, k);
    const ok = entries[entries.length - 1 - k][2];
    wins += weight * (ok ? 1 : 0);
    losses += weight * (ok ? 0 : 1);
  }
  return { wins, losses };
}

/**
 * @param {number} wins
 * @param {number} losses
 * @param {boolean} hopeful
 */
function rate(wins, losses, hopeful) {
  const a = wins + PRIOR_WINS;
  const b = losses + PRIOR_LOSSES;
  const p = a / (a + b);
  return hopeful ? Math.min(CAP, p + Math.sqrt((p * (1 - p)) / (a + b + 1))) : p;
}

/**
 * A pair's average cost over its entries, oldest first: the first known cost sets it and each
 * later one moves it by 0.9 x old + 0.1 x new. `model_credits` is averaged over the exact same
 * entries that have a known total cost, falling back to total cost when the model share is null,
 * so `0 <= model_credits <= credits` holds even for legacy partial-bill entries.
 * @param {HistoryEntry[]} entries
 */
function averages(entries) {
  /** @type {number | null} */
  let credits = null;
  /** @type {number | null} */
  let modelCredits = null;
  let bills = 0;
  for (const entry of entries) {
    const cost = entry[3];
    if (cost !== null) {
      credits = credits === null ? cost : 0.9 * credits + 0.1 * cost;
      bills += 1;
      const model = entry[4] ?? cost;
      modelCredits = modelCredits === null ? model : 0.9 * modelCredits + 0.1 * model;
    }
  }
  return { credits, modelCredits, bills };
}

/** @param {string} workflow @returns {HistoryState} */
function emptyState(workflow) {
  return { v: 2, workflow, seq: 0, generation: null, reset_after_run: 0, last_run: 0, unexplained: 0, scopes: {}, updated: null };
}

/** @param {string} at @returns {PairRecord} */
function newPair(at) {
  return { history: [], credits: null, model_credits: null, bills: 0, streak: 0, tests: 0, suspended: null, seen: at, summary: null };
}

/** @returns {ScopeRecord} */
function newScope() {
  return { incumbent: null, last_role: null, last_success_seq: 0, pairs: {} };
}

/**
 * The bandit's edit rule, for a new definition: keep successes, drop failures, and clear
 * streaks, tests and suspensions.
 * @param {ScopeRecord} scope
 */
function editScope(scope) {
  for (const pair of Object.values(scope.pairs)) {
    pair.history = pair.history.filter(entry => entry[2]);
    pair.streak = 0;
    pair.tests = 0;
    pair.suspended = null;
  }
}

/**
 * Whether a selection receipt belongs to this run: the same workflow, run, attempt, generation,
 * mode and scope, and the pair AWF's own selection record names.
 * @param {Record<string, any> | null | undefined} receipt
 * @param {RunEvidence} run
 */
function receiptBound(receipt, run) {
  return (
    !!receipt &&
    receipt.workflow === run.workflow &&
    receipt.run?.id === run.run.id &&
    receipt.run?.attempt === run.run.attempt &&
    receipt.generation === run.generation &&
    receipt.mode === run.mode &&
    receipt.scope === run.scope &&
    receipt.selected === run.pair
  );
}

/**
 * The base model of a pair: everything before `@` (or the whole string if no `@` is present).
 * A suspension covers every effort of its base.
 * @param {string} pairName
 */
function baseOf(pairName) {
  const at = pairName.lastIndexOf("@");
  return at >= 0 ? pairName.slice(0, at) : pairName;
}

/**
 * Fold one outcome into a pair, as the bandit's record() does. Returns whether it counted.
 * @param {HistoryState} state
 * @param {ScopeRecord} scope
 * @param {string} pairName
 * @param {boolean} ok
 * @param {string} at
 * @param {RunCost} cost
 * @param {Array<[string, string]>} events
 */
function recordOutcome(state, scope, pairName, ok, at, cost, events) {
  const pair = scope.pairs[pairName] || (scope.pairs[pairName] = newPair(at));
  pair.seen = at;
  /** @returns {HistoryEntry} */
  const entry = () => {
    state.seq += 1;
    return [state.seq, at, ok, cost.credits ?? null, cost.model_credits ?? null, cost.tokens ?? null];
  };
  const suspended = pair.suspended;
  if (suspended !== null) {
    // The pending failures are the newest entries, unless pruning has already removed some.
    const pending = Math.min(suspended.pending, pair.history.length);
    if (ok) {
      pair.history = [...pair.history.slice(0, pair.history.length - pending), entry()];
      pair.suspended = null;
      pair.streak = 0;
      scope.last_success_seq = state.seq;
      events.push([pairName, "back"]);
      return true;
    }
    if (scope.last_success_seq > suspended.since_seq) {
      pair.history = [...pair.history.slice(pair.history.length - pending), entry()];
      pair.suspended = null;
      pair.streak = 0;
      events.push([pairName, "broken"]);
      return true;
    }
    return false;
  }
  // A suspension covers every effort of its base model, so another effort of a suspended base
  // settles the suspended sibling(s) when it runs (e.g. after the original effort left the rank).
  const targetBase = baseOf(pairName);
  /** @type {Array<{ record: PairRecord, suspended: Suspension }>} */
  const siblings = [];
  for (const [name, candidate] of Object.entries(scope.pairs)) {
    if (name !== pairName && candidate.suspended !== null && baseOf(name) === targetBase) {
      siblings.push({ record: candidate, suspended: candidate.suspended });
    }
  }
  if (siblings.length > 0) {
    if (ok) {
      for (const { record, suspended: siblingSuspended } of siblings) {
        const pending = Math.min(siblingSuspended.pending, record.history.length);
        record.history = record.history.slice(0, record.history.length - pending);
        record.suspended = null;
        record.streak = 0;
      }
      pair.history.push(entry());
      pair.streak = 0;
      scope.last_success_seq = state.seq;
      events.push([pairName, "back"]);
      return true;
    }
    const dueSiblings = siblings.filter(({ suspended: siblingSuspended }) => scope.last_success_seq > siblingSuspended.since_seq);
    if (dueSiblings.length > 0) {
      for (const { record, suspended: siblingSuspended } of dueSiblings) {
        const pending = Math.min(siblingSuspended.pending, record.history.length);
        record.history = record.history.slice(record.history.length - pending);
        record.suspended = null;
        record.streak = 0;
      }
      pair.history.push(entry());
      pair.streak = 0;
      events.push([pairName, "broken"]);
      return true;
    }
    return false;
  }
  pair.history.push(entry());
  if (ok) {
    pair.streak = 0;
    scope.last_success_seq = state.seq;
    return true;
  }
  pair.streak += 1;
  const runs = memory(pair.history, seconds(at)).length;
  if (pair.streak >= (runs <= PROVEN_RUNS ? STREAK_NEW : STREAK_PROVEN)) {
    pair.suspended = { since_seq: state.seq, pending: pair.streak };
    if (scope.incumbent === pairName) scope.incumbent = null;
    events.push([pairName, "suspended"]);
  }
  return true;
}

/**
 * Prune to the bounds and recompute each pair's derived fields as of `at`.
 * @param {HistoryState} state
 * @param {string} at
 * @param {typeof BOUNDS} bounds
 */
function refresh(state, at, bounds) {
  const now = seconds(at);
  for (const scope of Object.values(state.scopes)) {
    for (const pair of Object.values(scope.pairs)) {
      pair.history = memory(pair.history, now).slice(-bounds.history);
      const { credits, modelCredits, bills } = averages(pair.history);
      pair.credits = credits;
      pair.model_credits = modelCredits;
      pair.bills = bills;
      const { wins, losses } = counts(pair.history);
      pair.summary = { runs: pair.history.length, rate: rate(wins, losses, false), hopeful: rate(wins, losses, true) };
    }
  }
  /** @param {ScopeRecord} scope @returns {Array<[string, string]>} */
  const droppable = scope =>
    Object.entries(scope.pairs)
      .filter(([name, pair]) => name !== scope.incumbent && pair.suspended === null)
      .map(([name, pair]) => [pair.seen, name]);
  /** @param {string[][]} items */
  const least = items => items.reduce((best, item) => (compareTuples(item, best) < 0 ? item : best));
  for (const scope of Object.values(state.scopes)) {
    while (Object.keys(scope.pairs).length > bounds.pairs) {
      const candidates = droppable(scope);
      if (candidates.length === 0) break;
      delete scope.pairs[least(candidates)[1]];
    }
  }
  while (sizeOf(state) > bounds.state_bytes) {
    const candidates = Object.entries(state.scopes).flatMap(([key, scope]) => droppable(scope).map(([seen, name]) => [seen, name, key]));
    if (candidates.length === 0) break;
    const [, name, key] = least(candidates);
    delete state.scopes[key].pairs[name];
  }
}

/**
 * @param {string[]} left
 * @param {string[]} right
 */
function compareTuples(left, right) {
  for (let i = 0; i < left.length; i += 1) {
    if (left[i] < right[i]) return -1;
    if (left[i] > right[i]) return 1;
  }
  return 0;
}

/**
 * runs.jsonl after a write: the newest lines within the line bound, then the byte bound (each
 * line's bytes plus a newline).
 * @param {RunLine[]} runs
 * @param {Partial<typeof BOUNDS>} [overrides]
 */
function trimRuns(runs, overrides = {}) {
  const bounds = { ...BOUNDS, ...overrides };
  let kept = runs.slice(-bounds.runs_lines);
  while (kept.length > 0 && kept.reduce((total, line) => total + sizeOf(line) + 1, 0) > bounds.runs_bytes) kept = kept.slice(1);
  return kept;
}

/**
 * Bootstrap a legacy state.json (lacking `reconciled_through`) from its generation fence when
 * `runs.jsonl` has not evicted earlier lines, or from just below the oldest retained line when
 * eviction occurred so previously charged evicted runs are not re-charged while newer unverified
 * runs stay in scope.
 * @param {HistoryState} state
 * @param {RunLine[]} runs
 * @param {Partial<typeof BOUNDS>} [overrides]
 */
function legacyReconciliationFloor(state, runs, overrides = {}) {
  const bounds = { ...BOUNDS, ...overrides };
  if (!state.generation) return 0;
  const runsTrimmed = runs.length >= bounds.runs_lines || (runs.length > 0 && runs.reduce((total, entry) => total + sizeOf(entry) + 1, 0) >= bounds.runs_bytes - 1024);
  const oldestRetained = runs.length > 0 ? runs.reduce((min, entry) => Math.min(min, entry.number), Infinity) : 0;
  return runsTrimmed && oldestRetained > 0 ? Math.max(state.generation.since_run, oldestRetained - 1) : state.generation.since_run;
}

/**
 * Apply one finished run. Returns the new state and the lines to append to runs.jsonl; the
 * inputs are not modified.
 * @param {HistoryState} input
 * @param {RunLine[]} runs
 * @param {RunEvidence} run
 * @param {{ bounds?: Partial<typeof BOUNDS>, events?: Array<[string, string]> }} [options]
 * @returns {{ state: HistoryState, lines: RunLine[] }}
 */
function applyRun(input, runs, run, options = {}) {
  const bounds = { ...BOUNDS, ...(options.bounds || {}) };
  const events = options.events || [];
  /** @type {HistoryState} */
  const state = structuredClone(input);
  const { at } = run;
  const { id, number } = run.run;
  /** @type {RunLine[]} */
  const lines = [];

  /**
   * @param {string | null} note
   * @param {Partial<RunLine>} [fields]
   * @returns {RunLine}
   */
  const line = (note, fields = {}) => ({
    run: id,
    number,
    attempt: null,
    at,
    generation: null,
    scope: null,
    mode: null,
    role: null,
    pair: null,
    outcome: null,
    credits: null,
    counted: false,
    note,
    ...fields,
  });
  /** @param {string | null} note @param {Partial<RunLine>} [fields] */
  const own = (note, fields = {}) => line(note, { attempt: run.run.attempt, generation: run.generation, scope: run.scope, mode: run.mode, ...fields });
  const finish = () => {
    state.last_run = lines.reduce((latest, entry) => Math.max(latest, entry.number), state.last_run);
    state.updated = { run: id, at };
    refresh(state, at, bounds);
    return { state, lines };
  };

  const hasReconciliation =
    (run.reconciliation !== undefined && run.reconciliation !== null) || typeof input.reconciled_through === "number" || Array.isArray(input.unresolved_runs) || (input.unresolved_run_ids !== undefined && input.unresolved_run_ids !== null);
  const legacyFloor = legacyReconciliationFloor(state, runs, bounds);
  const prevReconciled = Math.max(typeof state.reconciled_through === "number" ? state.reconciled_through : legacyFloor, state.reset_after_run);
  const prevUnresolved = new Set((Array.isArray(state.unresolved_runs) ? state.unresolved_runs : []).filter(n => Number.isInteger(n) && n > state.reset_after_run));
  /** @type {Record<string, string>} */
  const knownUnresolvedIds = {};
  if (state.unresolved_run_ids && typeof state.unresolved_run_ids === "object") {
    for (const [key, value] of Object.entries(state.unresolved_run_ids)) {
      if (typeof value === "string" && value) knownUnresolvedIds[key] = value;
    }
  }
  if (run.reconciliation?.unresolved_ids && typeof run.reconciliation.unresolved_ids === "object") {
    for (const [key, value] of Object.entries(run.reconciliation.unresolved_ids)) {
      if (typeof value === "string" && value) knownUnresolvedIds[key] = value;
    }
  }

  /** @param {number[]} kept */
  const applyUnresolvedIds = kept => {
    /** @type {Record<string, string>} */
    const nextIds = {};
    for (const num of kept) {
      const idValue = knownUnresolvedIds[String(num)];
      if (idValue) nextIds[String(num)] = idValue;
    }
    if (Object.keys(nextIds).length > 0) {
      state.unresolved_run_ids = nextIds;
    } else {
      delete state.unresolved_run_ids;
    }
  };

  /**
   * Persist bounded reconciliation coverage without advancing `reconciled_through` on fenced-off runs,
   * while clearing this run's own number if it was previously tracked as unresolved.
   */
  const preserveReconciliation = () => {
    if (!hasReconciliation) return;
    let ceiling = prevReconciled;
    let kept = [...prevUnresolved].filter(n => n !== number && n <= ceiling).sort((a, b) => a - b);
    if (kept.length > bounds.unresolved_runs) {
      const cutoff = kept[bounds.unresolved_runs];
      ceiling = Math.max(state.reset_after_run, Math.min(ceiling, cutoff - 1));
      kept = kept.slice(0, bounds.unresolved_runs).filter(n => n <= ceiling);
    }
    state.reconciled_through = ceiling;
    state.unresolved_runs = kept;
    applyUnresolvedIds(kept);
  };

  // Written before, perhaps on an attempt whose acknowledgment was lost.
  if (runs.some(entry => entry.run === id)) return { state, lines: [] };
  if (number <= state.reset_after_run) {
    lines.push(own("before_reset"));
    preserveReconciliation();
    return finish();
  }
  const firstHistoryWrite = state.generation === null;
  if (state.generation === null) {
    state.generation = { hash: run.generation, since_run: number };
  } else if (run.generation !== state.generation.hash) {
    if (number <= state.generation.since_run) {
      lines.push(own("older_definition"));
      preserveReconciliation();
      return finish();
    }
    for (const scope of Object.values(state.scopes)) editScope(scope);
    state.unexplained = 0;
    state.generation = { hash: run.generation, since_run: number };
  }

  // Compute the clamped reconciliation ceiling before charging newly covered runs so no charge
  // lands above the persisted ceiling where runs.jsonl eviction could later duplicate it.
  const verifiedScan = run.reconciliation?.verified === true;
  let finalCeiling = prevReconciled;
  /** @type {number[]} */
  let finalUnresolved = [];
  if (hasReconciliation) {
    if (firstHistoryWrite) {
      finalCeiling = Math.max(prevReconciled, number);
      finalUnresolved = [];
    } else {
      const rec = run.reconciliation;
      const requestedCeiling = verifiedScan ? Math.max(prevReconciled, typeof rec?.ceiling === "number" && Number.isInteger(rec.ceiling) ? Math.min(number, rec.ceiling) : number) : prevReconciled;
      const writtenNumbers = new Set(runs.map(entry => entry.number));
      writtenNumbers.add(number);
      const resolved = new Set(Array.isArray(rec?.resolved) ? rec.resolved.filter(n => Number.isInteger(n) && n < number) : []);
      for (const other of run.listing || []) {
        if (other.number > state.reset_after_run && other.number < number && (prevUnresolved.has(other.number) || (verifiedScan && other.number <= requestedCeiling))) {
          resolved.add(other.number);
        }
      }
      const nextUnresolved = new Set([...prevUnresolved].filter(n => !resolved.has(n) && !writtenNumbers.has(n)));
      let ceiling = requestedCeiling;
      if (verifiedScan) {
        for (const n of Array.isArray(rec?.unresolved) ? rec.unresolved : []) {
          if (Number.isInteger(n) && n > state.reset_after_run && n < number && n <= ceiling && !writtenNumbers.has(n) && !resolved.has(n)) {
            nextUnresolved.add(n);
          }
        }
      }
      let kept = [...nextUnresolved].filter(n => n <= ceiling).sort((a, b) => a - b);
      // Bound unresolved_runs without falsely marking dropped runs as reconciled.
      if (kept.length > bounds.unresolved_runs) {
        const cutoff = kept[bounds.unresolved_runs];
        ceiling = Math.max(state.reset_after_run, Math.min(ceiling, cutoff - 1));
        kept = kept.slice(0, bounds.unresolved_runs).filter(n => n <= ceiling);
      }
      finalCeiling = ceiling;
      finalUnresolved = kept;
    }
  }

  // Completed runs that wrote no line are charged after the fences, in the right generation.
  const written = new Set(runs.map(entry => entry.run));
  const sinceRun = state.generation.since_run;
  if (!hasReconciliation || !firstHistoryWrite) {
    for (const other of [...(run.listing || [])].sort((left, right) => left.number - right.number)) {
      if (written.has(other.id) || other.id === id || other.number <= state.reset_after_run) continue;
      if (hasReconciliation) {
        // Never let an older writer consume a newer run (which may belong to a later generation),
        // and never charge a newly covered run above the final clamped ceiling.
        if (other.number >= number) continue;
        const tracked = prevUnresolved.has(other.number);
        if (!tracked && (!verifiedScan || other.number <= prevReconciled || other.number > finalCeiling)) continue;
      } else if (other.number <= sinceRun) {
        continue;
      }
      if (["success", "skipped", "neutral"].includes(other.conclusion)) continue;
      if (hasReconciliation && other.generation && other.generation !== state.generation.hash && other.number <= state.generation.since_run) {
        lines.push(line("older_definition", { run: other.id, number: other.number, attempt: 1, generation: other.generation }));
        written.add(other.id);
        continue;
      }
      if (other.agent_ran) {
        lines.push(line("unsaved", { run: other.id, number: other.number, attempt: 1 }));
        state.unexplained += 1;
      } else {
        lines.push(line("no_agent", { run: other.id, number: other.number, attempt: 1 }));
      }
      written.add(other.id);
    }
  }

  if (hasReconciliation) {
    state.reconciled_through = finalCeiling;
    state.unresolved_runs = finalUnresolved;
    applyUnresolvedIds(finalUnresolved);
  }

  if (!run.observation) {
    lines.push(own("no_agent"));
    return finish();
  }
  let scope = state.scopes[run.scope];
  if (!scope) {
    if (Object.keys(state.scopes).length >= bounds.scopes) {
      /** @type {string | undefined} */
      let oldest;
      let oldestSeen = "";
      for (const [key, candidate] of Object.entries(state.scopes)) {
        const seen = Object.values(candidate.pairs).reduce((latest, pair) => (pair.seen > latest ? pair.seen : latest), "");
        if (oldest === undefined || seen < oldestSeen) {
          oldest = key;
          oldestSeen = seen;
        }
      }
      if (oldest !== undefined) delete state.scopes[oldest];
    }
    scope = state.scopes[run.scope] = newScope();
  }

  const { receipt } = run;
  const pairName = run.pair;
  const live = run.mode === "live";
  const bound = receiptBound(receipt, run);
  /** @type {string | null} */
  let role = null;
  if (!live) {
    role = "observe";
  } else if (bound && receipt) {
    role = receipt.role;
  } else if (!run.prepare || run.prepare.explore_allowed !== false) {
    // A live run that may have explored but left no bound receipt could be a lost test.
    state.unexplained += 1;
  }
  // An unknown live role counts as a test for the share rule.
  scope.last_role = role ?? "test";
  if (role === "test" && pairName !== null) {
    const pair = scope.pairs[pairName] || (scope.pairs[pairName] = newPair(at));
    pair.tests += 1;
    pair.seen = at;
  }
  if (live && bound && receipt) {
    const { before, after } = receipt.incumbent;
    // Only a decision made on the current incumbent may switch it.
    if (before === scope.incumbent && after !== before) {
      scope.incumbent = after;
      if (scope.pairs[after]) scope.pairs[after].tests = 0;
    }
  }

  const { observation } = run;
  const cost = run.cost || {};
  // A verdict on a pair the records do not name cannot count for any pair.
  if (observation.outcome === "skip" || pairName === null) {
    const note = observation.outcome === "skip" ? (observation.reason ?? null) : "unknown_evidence";
    lines.push(own(note, { role, pair: pairName, outcome: "skip", credits: cost.credits ?? null }));
    return finish();
  }
  if (live && !bound) {
    lines.push(own(receipt ? "receipt_mismatch" : "receipt_missing", { pair: pairName, outcome: observation.outcome, credits: cost.credits ?? null }));
    return finish();
  }
  const counted = recordOutcome(state, scope, pairName, observation.outcome === "success", at, cost, events);
  lines.push(own(counted ? null : "awaiting_success", { role, pair: pairName, outcome: observation.outcome, credits: cost.credits ?? null, counted }));
  return finish();
}

module.exports = {
  BOUNDS,
  NOTES,
  applyRun,
  averages,
  counts,
  emptyState,
  legacyReconciliationFloor,
  memory,
  rate,
  receiptBound,
  trimRuns,
};
