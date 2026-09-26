// @ts-check

// Friction-cost measurement for the usage activity summary.
//
// Friction cost is the estimated avoidable marginal cost attributable to an
// execution-friction event, relative to the counterfactual execution in which
// that event did not occur. The conclusion job precomputes it so usage-only
// consumers never have to re-derive it from raw logs.
//
// Canonical unit: AI credits (AIC). Token classes, turns, tool calls and latency
// are reported additionally, but only for drivers whose data source supports them
// (see FRICTION_DRIVERS below and docs/reference/artifacts.md for the matrix).
//
// Every cost carries an attribution state:
//   measured     — read directly off the friction record itself
//   causal       — taken from a deterministically linked follow-up invocation
//   statistical  — apportioned from the mean cost of a non-friction invocation
//   unavailable  — the data required for that dimension was not present
//
// Double counting is prevented by deterministic causal grouping: drivers that
// observe the same underlying friction from different log sources share a causal
// group, and only the highest-fidelity source contributes cost for the overlapping
// occurrences. Model invocations are consumed at most once across all events.

/** @typedef {"measured" | "causal" | "statistical" | "unavailable"} FrictionState */
/** @typedef {"measured" | "causal" | "statistical" | "unavailable" | "unsupported"} FrictionDimensionState */

const STATE_MEASURED = "measured";
const STATE_CAUSAL = "causal";
const STATE_STATISTICAL = "statistical";
const STATE_UNAVAILABLE = "unavailable";
const DIMENSION_UNSUPPORTED = "unsupported";

/** Higher rank wins when several states contribute to the same dimension. */
const STATE_RANK = { measured: 3, causal: 2, statistical: 1, unavailable: 0 };

const TOKEN_CLASSES = ["input", "output", "cache_read", "cache_write", "reasoning"];
const COST_DIMENSIONS = ["aic", "tokens", "turns", "tool_calls", "latency_ms"];

const SOURCE_MCP_GATEWAY = "mcp_gateway";
const SOURCE_AGENT_SESSION = "agent_session";
const SOURCE_FIREWALL = "firewall";
const SOURCE_AGENT_TOKEN_USAGE = "agent_token_usage";

// Fidelity ranking used by causal grouping: when two sources report the same
// underlying friction, the higher-ranked source owns the overlapping occurrences.
const SOURCE_FIDELITY = {
  [SOURCE_AGENT_TOKEN_USAGE]: 4,
  [SOURCE_MCP_GATEWAY]: 3,
  [SOURCE_AGENT_SESSION]: 2,
  [SOURCE_FIREWALL]: 1,
};

// Cap on the number of event records embedded in the artifact. Aggregates always
// reflect every event; only the per-event listing is truncated.
const MAX_FRICTION_EVENTS = 200;

/**
 * Static driver matrix. `derived` means the dimension is attributed causally when a
 * follow-up model invocation can be linked, and statistically otherwise.
 *
 * @type {Record<string, { class: string, source: string, description: string, dimensions: Record<string, "measured"|"derived"|"unsupported"> }>}
 */
const FRICTION_DRIVERS = {
  mcp_tool_error: {
    class: "tool_failure",
    source: SOURCE_MCP_GATEWAY,
    description: "MCP tool call returned an error or error result",
    dimensions: { aic: "derived", tokens: "derived", turns: "unsupported", tool_calls: "measured", latency_ms: "measured" },
  },
  session_tool_failure: {
    class: "tool_failure",
    source: SOURCE_AGENT_SESSION,
    description: "Agent session recorded a failed tool execution",
    dimensions: { aic: "derived", tokens: "derived", turns: "unsupported", tool_calls: "measured", latency_ms: "unsupported" },
  },
  integrity_filter: {
    class: "integrity_filter",
    source: SOURCE_MCP_GATEWAY,
    description: "DIFC integrity filtering removed a tool response",
    dimensions: { aic: "derived", tokens: "derived", turns: "unsupported", tool_calls: "measured", latency_ms: "unsupported" },
  },
  firewall_block: {
    class: "network_block",
    source: SOURCE_FIREWALL,
    description: "Egress request blocked by the firewall",
    dimensions: { aic: "derived", tokens: "derived", turns: "unsupported", tool_calls: "unsupported", latency_ms: "unsupported" },
  },
  agent_api_error: {
    class: "model_error",
    source: SOURCE_AGENT_TOKEN_USAGE,
    description: "Model invocation failed or was retried",
    dimensions: { aic: "measured", tokens: "measured", turns: "measured", tool_calls: "unsupported", latency_ms: "measured" },
  },
};

/**
 * @returns {Record<string, number>}
 */
function emptyTokens() {
  /** @type {Record<string, number>} */
  const tokens = {};
  for (const tokenClass of TOKEN_CLASSES) {
    tokens[tokenClass] = 0;
  }
  tokens.total = 0;
  return tokens;
}

/**
 * @param {Record<string, number>} target
 * @param {Record<string, number>} addition
 */
function addTokens(target, addition) {
  for (const tokenClass of TOKEN_CLASSES) {
    target[tokenClass] += addition[tokenClass] || 0;
  }
  target.total += addition.total || 0;
}

/**
 * Round fractional token estimates after attribution, distributing remainder
 * tokens deterministically and rebuilding aggregates from rounded event costs.
 *
 * @param {Array<Record<string, any>>} eventRecords
 * @param {Record<string, any>} totalCost
 * @param {Map<string, Record<string, any>>} driverTotals
 */
function roundAttributedTokens(eventRecords, totalCost, driverTotals) {
  for (const tokenClass of TOKEN_CLASSES) {
    const exactValues = eventRecords.map(event => event.cost.tokens[tokenClass]);
    const normalizedValues = exactValues.map(value => {
      const nearestInteger = Math.round(value);
      const tolerance = Number.EPSILON * Math.max(1, Math.abs(value)) * 4;
      return Math.abs(value - nearestInteger) <= tolerance ? nearestInteger : value;
    });
    const roundedValues = normalizedValues.map(Math.floor);
    const remainder = Math.round(normalizedValues.reduce((sum, value) => sum + value, 0)) - roundedValues.reduce((sum, value) => sum + value, 0);
    const fractions = normalizedValues.map((value, index) => ({ index, fraction: value - roundedValues[index] })).sort((left, right) => right.fraction - left.fraction || left.index - right.index);
    for (let index = 0; index < Math.min(remainder, fractions.length); index += 1) {
      roundedValues[fractions[index].index] += 1;
    }
    eventRecords.forEach((event, index) => {
      event.cost.tokens[tokenClass] = roundedValues[index];
    });
  }

  totalCost.tokens = emptyTokens();
  for (const driver of driverTotals.values()) {
    driver.cost.tokens = emptyTokens();
  }
  for (const event of eventRecords) {
    event.cost.tokens.total = TOKEN_CLASSES.reduce((sum, tokenClass) => sum + event.cost.tokens[tokenClass], 0);
    addTokens(totalCost.tokens, event.cost.tokens);
    const driver = driverTotals.get(event.driver);
    if (!driver) {
      throw new Error(`missing friction driver total for ${event.driver}`);
    }
    addTokens(driver.cost.tokens, event.cost.tokens);
  }
}

/**
 * @param {unknown} value
 * @returns {number}
 */
function nonNegativeNumber(value) {
  return typeof value === "number" && Number.isFinite(value) && value > 0 ? value : 0;
}

/**
 * @param {unknown} value
 * @returns {number}
 */
function nonNegativeInteger(value) {
  return Math.round(nonNegativeNumber(value));
}

/**
 * @param {unknown} value
 * @returns {number | null}
 */
function optionalNonNegativeNumber(value) {
  if (typeof value === "number" && Number.isFinite(value) && value >= 0) {
    return value;
  }
  return null;
}

/**
 * @param {unknown} value
 * @returns {number | null}
 */
function parseTimestampMs(value) {
  if (typeof value !== "string" || !value.trim()) {
    return null;
  }
  const parsed = Date.parse(value);
  return Number.isFinite(parsed) ? parsed : null;
}

/**
 * Classify a token-usage record as a friction (errored/retried) invocation.
 *
 * @param {Record<string, any>} entry
 * @returns {string} friction reason, or "" when the invocation was healthy
 */
function classifyInvocationFriction(entry) {
  for (const field of ["status", "status_code", "http_status", "response_status"]) {
    const value = entry[field];
    const numeric = typeof value === "number" ? value : typeof value === "string" ? Number.parseInt(value, 10) : Number.NaN;
    if (Number.isFinite(numeric) && numeric >= 400) {
      return `http_${numeric}`;
    }
  }
  const status = String(entry.status ?? "")
    .trim()
    .toLowerCase();
  if (status === "error" || status === "failed" || status === "failure") {
    return "status_error";
  }
  const error = entry.error;
  if (typeof error === "string" && error.trim()) {
    return "error";
  }
  if (error && typeof error === "object") {
    return "error";
  }
  if (entry.retry === true || entry.is_retry === true || entry.retried === true) {
    return "retry";
  }
  const event = String(entry.event ?? "")
    .trim()
    .toLowerCase();
  if (event.includes("error") || event.includes("retry")) {
    return event;
  }
  return "";
}

/**
 * @typedef {object} FrictionInvocation
 * @property {number} index
 * @property {number | null} timestampMs
 * @property {number | null} aic
 * @property {Record<string, number>} tokens
 * @property {number} durationMs
 * @property {string} frictionReason
 * @property {boolean} consumed
 */

/**
 * Parse agent token-usage JSONL into per-invocation records.
 *
 * @param {string} content
 * @returns {{ invocations: FrictionInvocation[], ignoredRecords: number }}
 */
function parseInvocationsFromJSONL(content) {
  /** @type {FrictionInvocation[]} */
  const invocations = [];
  let ignoredRecords = 0;
  let index = 0;

  for (const raw of String(content || "").split("\n")) {
    const line = raw.trim();
    if (!line) continue;
    /** @type {any} */
    let entry;
    try {
      entry = JSON.parse(line);
    } catch {
      ignoredRecords += 1;
      continue;
    }
    if (!entry || typeof entry !== "object" || Array.isArray(entry)) {
      ignoredRecords += 1;
      continue;
    }

    const tokens = emptyTokens();
    tokens.input = nonNegativeInteger(entry.input_tokens);
    tokens.output = nonNegativeInteger(entry.output_tokens);
    tokens.cache_read = nonNegativeInteger(entry.cache_read_tokens);
    tokens.cache_write = nonNegativeInteger(entry.cache_write_tokens);
    tokens.reasoning = nonNegativeInteger(entry.reasoning_tokens);
    tokens.total = TOKEN_CLASSES.reduce((sum, tokenClass) => sum + tokens[tokenClass], 0);

    invocations.push({
      index,
      timestampMs: parseTimestampMs(entry.timestamp),
      aic: optionalNonNegativeNumber(entry.ai_credits_this_response ?? entry.ai_credits ?? entry.aic),
      tokens,
      durationMs: nonNegativeInteger(entry.duration_ms),
      frictionReason: classifyInvocationFriction(entry),
      consumed: false,
    });
    index += 1;
  }

  // Deterministic order: timestamp when available, file order otherwise.
  invocations.sort((left, right) => {
    const leftTs = left.timestampMs === null ? Number.POSITIVE_INFINITY : left.timestampMs;
    const rightTs = right.timestampMs === null ? Number.POSITIVE_INFINITY : right.timestampMs;
    return leftTs - rightTs || left.index - right.index;
  });

  return { invocations, ignoredRecords };
}

/**
 * Sample mean and relative standard error of the mean.
 *
 * @param {number[]} values
 * @returns {{ mean: number | null, relativeError: number | null, sampleSize: number }}
 */
function meanAndRelativeError(values) {
  const sampleSize = values.length;
  if (sampleSize === 0) {
    return { mean: null, relativeError: null, sampleSize: 0 };
  }
  const mean = values.reduce((sum, value) => sum + value, 0) / sampleSize;
  if (sampleSize < 2 || mean <= 0) {
    return { mean, relativeError: null, sampleSize };
  }
  const variance = values.reduce((sum, value) => sum + (value - mean) ** 2, 0) / (sampleSize - 1);
  return { mean, relativeError: Math.sqrt(variance / sampleSize) / mean, sampleSize };
}

/**
 * Baseline statistics used for statistical apportionment. Friction invocations are
 * excluded so the baseline describes the cost of a healthy invocation.
 *
 * @param {FrictionInvocation[]} invocations
 */
function computeBaselineStats(invocations) {
  const healthy = invocations.filter(invocation => !invocation.frictionReason);
  const aicStats = meanAndRelativeError(healthy.filter(invocation => invocation.aic !== null).map(invocation => Number(invocation.aic)));
  const tokenStats = meanAndRelativeError(healthy.map(invocation => invocation.tokens.total));
  const meanTokens = emptyTokens();
  if (healthy.length > 0) {
    for (const tokenClass of TOKEN_CLASSES) {
      meanTokens[tokenClass] = healthy.reduce((sum, invocation) => sum + invocation.tokens[tokenClass], 0) / healthy.length;
    }
    meanTokens.total = TOKEN_CLASSES.reduce((sum, tokenClass) => sum + meanTokens[tokenClass], 0);
  }
  return {
    healthyCount: healthy.length,
    meanAIC: aicStats.mean,
    aicRelativeError: aicStats.relativeError,
    aicSampleSize: aicStats.sampleSize,
    meanTokens,
    tokensRelativeError: tokenStats.relativeError,
    tokensSampleSize: tokenStats.sampleSize,
    hasTokenBaseline: healthy.length > 0 && meanTokens.total > 0,
  };
}

/**
 * @typedef {object} FrictionEvent
 * @property {string} id
 * @property {string} driver
 * @property {string} source
 * @property {string} group_id
 * @property {string} label
 * @property {string} [timestamp]
 * @property {number | null} timestampMs
 * @property {number} occurrences
 * @property {number} counted_occurrences
 * @property {number} suppressed_occurrences
 * @property {string} [suppressed_by]
 * @property {number} latencyMs
 * @property {FrictionInvocation | null} [invocation]
 * @property {string} [detail]
 */

/**
 * Build the deterministic friction event list from the already-parsed activity sections.
 *
 * @param {{ gateway: any, integrity: any, session: any, firewall: any, invocations: FrictionInvocation[] }} inputs
 * @returns {FrictionEvent[]}
 */
function buildFrictionEvents({ gateway, integrity, session, firewall, invocations }) {
  /** @type {FrictionEvent[]} */
  const events = [];

  // Failed MCP tool calls: identified per call, with measured latency.
  const toolCalls = gateway && Array.isArray(gateway.tool_calls) ? gateway.tool_calls : [];
  for (const call of toolCalls) {
    if (String(call?.outcome || "").toLowerCase() !== "failure") {
      continue;
    }
    const label = `${String(call.server_name || "unknown")}/${String(call.tool_name || "unknown")}`;
    events.push({
      id: `mcp_tool_error:${String(call.tool_call_id || `${label}-${events.length + 1}`)}`,
      driver: "mcp_tool_error",
      source: SOURCE_MCP_GATEWAY,
      group_id: "tool_failure",
      label,
      timestamp: typeof call.timestamp === "string" && call.timestamp ? call.timestamp : undefined,
      timestampMs: parseTimestampMs(call.timestamp),
      occurrences: 1,
      counted_occurrences: 1,
      suppressed_occurrences: 0,
      latencyMs: nonNegativeInteger(call.duration_ms),
    });
  }

  // Session-reported tool failures: aggregate count only, no per-call identity.
  const sessionFailures = session ? nonNegativeNumber(session.failed_tool_executions) : 0;
  if (sessionFailures > 0) {
    events.push({
      id: "session_tool_failure:aggregate",
      driver: "session_tool_failure",
      source: SOURCE_AGENT_SESSION,
      group_id: "tool_failure",
      label: "agent session tool executions",
      timestampMs: null,
      occurrences: sessionFailures,
      counted_occurrences: sessionFailures,
      suppressed_occurrences: 0,
      latencyMs: 0,
    });
  }

  // Integrity-filtered tool responses, keyed by tool for a stable identity.
  const filteredToolCounts = integrity && integrity.filtered_tool_counts ? integrity.filtered_tool_counts : null;
  if (filteredToolCounts) {
    for (const [toolName, count] of Object.entries(filteredToolCounts).sort(([left], [right]) => left.localeCompare(right))) {
      const occurrences = nonNegativeNumber(count);
      if (occurrences === 0) continue;
      events.push({
        id: `integrity_filter:${toolName}`,
        driver: "integrity_filter",
        source: SOURCE_MCP_GATEWAY,
        group_id: "integrity_filter",
        label: toolName,
        timestampMs: null,
        occurrences,
        counted_occurrences: occurrences,
        suppressed_occurrences: 0,
        latencyMs: 0,
      });
    }
  }

  // Firewall blocks, keyed by domain. Without per-request linkage, their cost is
  // statistically estimated from healthy invocations in the same run.
  const requestsByDomain = firewall && firewall.requests_by_domain ? firewall.requests_by_domain : null;
  if (requestsByDomain) {
    for (const [domain, stats] of Object.entries(requestsByDomain).sort(([left], [right]) => left.localeCompare(right))) {
      const blocked = nonNegativeNumber(/** @type {any} */ stats?.blocked);
      if (blocked === 0) continue;
      events.push({
        id: `firewall_block:${domain}`,
        driver: "firewall_block",
        source: SOURCE_FIREWALL,
        group_id: "network_block",
        label: domain,
        timestampMs: null,
        occurrences: blocked,
        counted_occurrences: blocked,
        suppressed_occurrences: 0,
        latencyMs: 0,
      });
    }
  }

  // Errored / retried model invocations carry their own measured cost.
  for (const invocation of invocations) {
    if (!invocation.frictionReason) continue;
    events.push({
      id: `agent_api_error:${invocation.index}`,
      driver: "agent_api_error",
      source: SOURCE_AGENT_TOKEN_USAGE,
      group_id: "model_error",
      label: invocation.frictionReason,
      timestampMs: invocation.timestampMs,
      occurrences: 1,
      counted_occurrences: 1,
      suppressed_occurrences: 0,
      latencyMs: invocation.durationMs,
      invocation,
      detail: invocation.frictionReason,
    });
  }

  return sortFrictionEvents(events);
}

/**
 * @param {FrictionEvent[]} events
 * @returns {FrictionEvent[]}
 */
function sortFrictionEvents(events) {
  return events.slice().sort((left, right) => {
    const leftTs = left.timestampMs === null ? Number.POSITIVE_INFINITY : left.timestampMs;
    const rightTs = right.timestampMs === null ? Number.POSITIVE_INFINITY : right.timestampMs;
    return leftTs - rightTs || left.id.localeCompare(right.id);
  });
}

/**
 * Apply deterministic causal grouping so overlapping observations of the same
 * underlying friction are counted exactly once. Within a group the highest-fidelity
 * source owns its occurrences; lower-fidelity sources contribute only their excess.
 *
 * @param {FrictionEvent[]} events
 * @returns {Array<Record<string, any>>}
 */
function applyCausalGrouping(events) {
  /** @type {Map<string, FrictionEvent[]>} */
  const byGroup = new Map();
  for (const event of events) {
    const members = byGroup.get(event.group_id);
    if (members) {
      members.push(event);
    } else {
      byGroup.set(event.group_id, [event]);
    }
  }

  const groups = [];
  for (const [groupID, members] of Array.from(byGroup.entries()).sort(([left], [right]) => left.localeCompare(right))) {
    const ranked = members.slice().sort((left, right) => {
      const fidelity = (SOURCE_FIDELITY[right.source] || 0) - (SOURCE_FIDELITY[left.source] || 0);
      return fidelity || left.source.localeCompare(right.source) || left.id.localeCompare(right.id);
    });
    const primarySource = ranked[0].source;

    // Distinct occurrences in a group are the largest count reported by any single
    // source. The highest-fidelity source counts all of its occurrences; every lower
    // tier contributes only the excess it observed beyond what is already counted.
    let countedInGroup = 0;
    for (let index = 0; index < ranked.length;) {
      const tierSource = ranked[index].source;
      const tier = [];
      while (index < ranked.length && ranked[index].source === tierSource) {
        tier.push(ranked[index]);
        index += 1;
      }
      const tierTotal = tier.reduce((sum, event) => sum + event.occurrences, 0);
      let allowance = Math.max(0, tierTotal - countedInGroup);
      for (const event of tier) {
        const counted = Math.min(event.occurrences, allowance);
        allowance -= counted;
        event.counted_occurrences = counted;
        event.suppressed_occurrences = event.occurrences - counted;
        if (event.suppressed_occurrences > 0) {
          event.suppressed_by = `causal-group:${groupID}`;
        }
      }
      countedInGroup += Math.max(0, tierTotal - countedInGroup);
    }

    const totalOccurrences = members.reduce((sum, event) => sum + event.occurrences, 0);
    const countedOccurrences = members.reduce((sum, event) => sum + event.counted_occurrences, 0);
    groups.push({
      group_id: groupID,
      primary_source: primarySource,
      event_ids: ranked.map(event => event.id),
      total_occurrences: totalOccurrences,
      counted_occurrences: countedOccurrences,
      suppressed_occurrences: totalOccurrences - countedOccurrences,
      rule: "highest-fidelity source owns overlapping occurrences; lower-fidelity sources contribute only their excess",
    });
  }
  return groups;
}

/**
 * Link the earliest unconsumed healthy invocation that could have paid for a
 * friction event. Invocations are consumed at most once across all events.
 *
 * @param {FrictionInvocation[]} invocations
 * @param {number | null} afterMs
 * @returns {FrictionInvocation | null}
 */
function takeLinkedInvocation(invocations, afterMs) {
  if (afterMs === null) {
    return null;
  }
  for (const invocation of invocations) {
    if (invocation.consumed || invocation.frictionReason) continue;
    if (invocation.timestampMs === null || invocation.timestampMs < afterMs) continue;
    invocation.consumed = true;
    return invocation;
  }
  return null;
}

/**
 * @returns {{ aic: number, tokens: Record<string, number>, turns: number, tool_calls: number, latency_ms: number }}
 */
function emptyCost() {
  return { aic: 0, tokens: emptyTokens(), turns: 0, tool_calls: 0, latency_ms: 0 };
}

/**
 * @param {string} left
 * @param {string} right
 * @returns {string}
 */
function weakerState(left, right) {
  if (left === DIMENSION_UNSUPPORTED) return right;
  if (right === DIMENSION_UNSUPPORTED) return left;
  return (STATE_RANK[left] ?? 0) <= (STATE_RANK[right] ?? 0) ? left : right;
}

/**
 * Attribute cost to a single event, mutating the shared invocation pool.
 *
 * @param {FrictionEvent} event
 * @param {FrictionInvocation[]} invocations
 * @param {ReturnType<typeof computeBaselineStats>} baseline
 */
function attributeEventCost(event, invocations, baseline) {
  const matrix = FRICTION_DRIVERS[event.driver].dimensions;
  const cost = emptyCost();
  /** @type {Record<string, string>} */
  const dimensionStates = {};
  for (const dimension of COST_DIMENSIONS) {
    dimensionStates[dimension] = matrix[dimension] === "unsupported" ? DIMENSION_UNSUPPORTED : STATE_UNAVAILABLE;
  }
  let statisticalAIC = 0;
  let statisticalTokens = 0;
  let linkedInvocations = 0;
  let unattributedOccurrences = 0;

  if (matrix.tool_calls === "measured" && event.counted_occurrences > 0) {
    cost.tool_calls = event.counted_occurrences;
    dimensionStates.tool_calls = STATE_MEASURED;
  }
  if (matrix.latency_ms === "measured" && event.latencyMs > 0) {
    cost.latency_ms = event.latencyMs;
    dimensionStates.latency_ms = STATE_MEASURED;
  }

  if (event.driver === "agent_api_error") {
    const invocation = event.invocation;
    if (invocation && event.counted_occurrences > 0) {
      if (invocation.aic !== null) {
        cost.aic = invocation.aic;
        dimensionStates.aic = STATE_MEASURED;
      } else {
        unattributedOccurrences += 1;
      }
      addTokens(cost.tokens, invocation.tokens);
      dimensionStates.tokens = STATE_MEASURED;
      cost.turns = 1;
      dimensionStates.turns = STATE_MEASURED;
    }
    return { cost, dimensionStates, statisticalAIC, statisticalTokens, linkedInvocations, unattributedOccurrences };
  }

  if (matrix.aic !== "derived" && matrix.tokens !== "derived") {
    return { cost, dimensionStates, statisticalAIC, statisticalTokens, linkedInvocations, unattributedOccurrences };
  }

  /** @type {string[]} */
  const aicStates = [];
  /** @type {string[]} */
  const tokenStates = [];
  for (let occurrence = 0; occurrence < event.counted_occurrences; occurrence += 1) {
    const linked = takeLinkedInvocation(invocations, event.timestampMs);
    if (linked) {
      linkedInvocations += 1;
      addTokens(cost.tokens, linked.tokens);
      tokenStates.push(STATE_CAUSAL);
      if (linked.aic !== null) {
        cost.aic += linked.aic;
        aicStates.push(STATE_CAUSAL);
      } else if (baseline.meanAIC !== null) {
        cost.aic += baseline.meanAIC;
        statisticalAIC += baseline.meanAIC;
        aicStates.push(STATE_STATISTICAL);
      } else {
        aicStates.push(STATE_UNAVAILABLE);
        unattributedOccurrences += 1;
      }
      continue;
    }
    if (baseline.hasTokenBaseline) {
      addTokens(cost.tokens, baseline.meanTokens);
      statisticalTokens += baseline.meanTokens.total;
      tokenStates.push(STATE_STATISTICAL);
    } else {
      tokenStates.push(STATE_UNAVAILABLE);
    }
    if (baseline.meanAIC !== null) {
      cost.aic += baseline.meanAIC;
      statisticalAIC += baseline.meanAIC;
      aicStates.push(STATE_STATISTICAL);
    } else {
      aicStates.push(STATE_UNAVAILABLE);
      unattributedOccurrences += 1;
    }
  }

  dimensionStates.aic = aicStates.length === 0 ? STATE_UNAVAILABLE : aicStates.reduce(weakerState);
  dimensionStates.tokens = tokenStates.length === 0 ? STATE_UNAVAILABLE : tokenStates.reduce(weakerState);
  return { cost, dimensionStates, statisticalAIC, statisticalTokens, linkedInvocations, unattributedOccurrences };
}

/**
 * @param {string} state
 * @param {number | null} relativeError
 * @returns {string}
 */
function confidenceForState(state, relativeError) {
  switch (state) {
    case STATE_MEASURED:
      return "high";
    case STATE_CAUSAL:
      return "medium";
    case STATE_STATISTICAL:
      if (relativeError === null) return "low";
      return relativeError <= 0.25 ? "medium" : "low";
    default:
      return "none";
  }
}

/**
 * @param {string} state
 * @returns {string}
 */
function methodForState(state) {
  switch (state) {
    case STATE_MEASURED:
      return "direct_record";
    case STATE_CAUSAL:
      return "next_invocation_linkage";
    case STATE_STATISTICAL:
      return "mean_invocation_apportionment";
    default:
      return "none";
  }
}

/**
 * @param {string} state
 * @param {number | null} relativeError
 * @param {number} sampleSize
 * @param {string} basis
 */
function buildUncertainty(state, relativeError, sampleSize, basis, pointEstimate) {
  const uncertainty = {
    state,
    method: methodForState(state),
    confidence: confidenceForState(state, relativeError),
    sample_size: sampleSize,
    basis,
  };
  if (state === STATE_MEASURED) {
    uncertainty.relative_error = 0;
    uncertainty.lower_bound = pointEstimate;
    uncertainty.upper_bound = pointEstimate;
  } else if (state === STATE_CAUSAL) {
    uncertainty.lower_bound = 0;
    uncertainty.upper_bound = pointEstimate;
  } else if (state === STATE_STATISTICAL && relativeError !== null) {
    uncertainty.relative_error = relativeError;
    uncertainty.confidence_level = 0.95;
    uncertainty.lower_bound = Math.max(0, pointEstimate * (1 - 1.96 * relativeError));
    uncertainty.upper_bound = pointEstimate * (1 + 1.96 * relativeError);
  }
  return uncertainty;
}

/**
 * Compute the precomputed friction-cost section of the usage activity summary.
 *
 * @param {object} inputs
 * @param {any} [inputs.gateway]
 * @param {any} [inputs.integrity]
 * @param {any} [inputs.session]
 * @param {any} [inputs.firewall]
 * @param {string} [inputs.tokenUsageContent]
 * @param {boolean} [inputs.tokenUsageAvailable]
 * @returns {{ friction: Record<string, any>, warnings: string[] }}
 */
function computeFrictionCost({ gateway = null, integrity = null, session = null, firewall = null, tokenUsageContent = "", tokenUsageAvailable = undefined } = {}) {
  const warnings = [];
  const { invocations, ignoredRecords } = parseInvocationsFromJSONL(tokenUsageContent);
  if (ignoredRecords > 0) {
    warnings.push(`friction-cost measurement ignored ${ignoredRecords} malformed token-usage record(s)`);
  }
  const hasTokenUsage = tokenUsageAvailable === undefined ? invocations.length > 0 : Boolean(tokenUsageAvailable) && invocations.length > 0;

  /** @type {string[]} */
  const sources = [];
  if (gateway || integrity) sources.push(SOURCE_MCP_GATEWAY);
  if (session) sources.push(SOURCE_AGENT_SESSION);
  if (firewall) sources.push(SOURCE_FIREWALL);
  if (hasTokenUsage) sources.push(SOURCE_AGENT_TOKEN_USAGE);
  sources.sort();

  const baseline = computeBaselineStats(invocations);
  const events = buildFrictionEvents({ gateway, integrity, session, firewall, invocations });
  const groups = applyCausalGrouping(events);

  const totalCost = emptyCost();
  /** @type {Record<string, string[]>} */
  const dimensionStateSets = { aic: [], tokens: [], turns: [], tool_calls: [], latency_ms: [] };
  let statisticalAICTotal = 0;
  let statisticalTokensTotal = 0;
  let linkedInvocationTotal = 0;
  let unattributedOccurrenceTotal = 0;
  /** @type {Map<string, Record<string, any>>} */
  const driverTotals = new Map();
  /** @type {Array<Record<string, any>>} */
  const eventRecords = [];

  for (const event of events) {
    const { cost, dimensionStates, statisticalAIC, statisticalTokens, linkedInvocations, unattributedOccurrences } = attributeEventCost(event, invocations, baseline);
    statisticalAICTotal += statisticalAIC;
    statisticalTokensTotal += statisticalTokens;
    linkedInvocationTotal += linkedInvocations;
    unattributedOccurrenceTotal += unattributedOccurrences;

    totalCost.aic += cost.aic;
    addTokens(totalCost.tokens, cost.tokens);
    totalCost.turns += cost.turns;
    totalCost.tool_calls += cost.tool_calls;
    totalCost.latency_ms += cost.latency_ms;

    for (const dimension of COST_DIMENSIONS) {
      const state = dimensionStates[dimension];
      if (event.counted_occurrences > 0 && state !== DIMENSION_UNSUPPORTED) {
        dimensionStateSets[dimension].push(state);
      }
    }

    let driver = driverTotals.get(event.driver);
    if (!driver) {
      driver = {
        driver: event.driver,
        class: FRICTION_DRIVERS[event.driver].class,
        source: event.source,
        events: 0,
        occurrences: 0,
        counted_occurrences: 0,
        suppressed_occurrences: 0,
        cost: emptyCost(),
        states: [],
      };
      driverTotals.set(event.driver, driver);
    }
    driver.events += 1;
    driver.occurrences += event.occurrences;
    driver.counted_occurrences += event.counted_occurrences;
    driver.suppressed_occurrences += event.suppressed_occurrences;
    driver.cost.aic += cost.aic;
    addTokens(driver.cost.tokens, cost.tokens);
    driver.cost.turns += cost.turns;
    driver.cost.tool_calls += cost.tool_calls;
    driver.cost.latency_ms += cost.latency_ms;
    if (event.counted_occurrences > 0) {
      driver.states.push(dimensionStates.aic);
    }

    const eventState = dimensionStates.aic === DIMENSION_UNSUPPORTED ? STATE_UNAVAILABLE : dimensionStates.aic;
    /** @type {Record<string, any>} */
    const eventRecord = {
      id: event.id,
      driver: event.driver,
      source: event.source,
      group_id: event.group_id,
      label: event.label,
      ...(event.timestamp ? { timestamp: event.timestamp } : {}),
      ...(event.detail ? { detail: event.detail } : {}),
      occurrences: event.occurrences,
      counted_occurrences: event.counted_occurrences,
      suppressed_occurrences: event.suppressed_occurrences,
      ...(event.suppressed_by ? { suppressed_by: event.suppressed_by } : {}),
      attribution_class: eventState === STATE_MEASURED ? "directly_measurable" : eventState === STATE_CAUSAL ? "causally_estimable" : eventState === STATE_STATISTICAL ? "statistically_estimated" : "unsupported",
      state: eventState,
      estimation_method: methodForState(eventState),
      dimension_states: dimensionStates,
      cost,
    };
    if (eventState === STATE_MEASURED) {
      eventRecord.lower_bound_aic = cost.aic;
      eventRecord.upper_bound_aic = cost.aic;
      eventRecord.confidence = "high";
    } else if (eventState === STATE_CAUSAL) {
      eventRecord.lower_bound_aic = 0;
      eventRecord.upper_bound_aic = cost.aic;
      eventRecord.confidence = "medium";
    } else if (eventState === STATE_STATISTICAL) {
      eventRecord.confidence = confidenceForState(eventState, baseline.aicRelativeError);
      if (baseline.aicRelativeError !== null) {
        eventRecord.lower_bound_aic = Math.max(0, cost.aic * (1 - 1.96 * baseline.aicRelativeError));
        eventRecord.upper_bound_aic = cost.aic * (1 + 1.96 * baseline.aicRelativeError);
      }
    }
    eventRecords.push(eventRecord);
  }

  // Every token contribution to the totals is also represented by an event record,
  // including suppressed events; this list is complete even when output events truncate.
  roundAttributedTokens(eventRecords, totalCost, driverTotals);

  /** @type {Record<string, string>} */
  const aggregateStates = {};
  for (const dimension of COST_DIMENSIONS) {
    const states = dimensionStateSets[dimension];
    aggregateStates[dimension] = states.length === 0 ? STATE_UNAVAILABLE : states.reduce(weakerState);
  }
  // No friction at all is a measurement, not a measurement gap.
  if (events.length === 0 && sources.length > 0) {
    for (const dimension of COST_DIMENSIONS) {
      aggregateStates[dimension] = STATE_MEASURED;
    }
  }

  const aicRelativeError = totalCost.aic > 0 && baseline.aicRelativeError !== null ? (statisticalAICTotal * baseline.aicRelativeError) / totalCost.aic : baseline.aicRelativeError;
  const tokensRelativeError = totalCost.tokens.total > 0 && baseline.tokensRelativeError !== null ? (statisticalTokensTotal * baseline.tokensRelativeError) / totalCost.tokens.total : baseline.tokensRelativeError;

  const uncertainty = {
    aic: buildUncertainty(
      aggregateStates.aic,
      aicRelativeError,
      baseline.aicSampleSize,
      "AI credits of errored invocations (measured), of linked follow-up invocations (causal), or the mean healthy-invocation cost (statistical)",
      totalCost.aic
    ),
    tokens: buildUncertainty(aggregateStates.tokens, tokensRelativeError, baseline.tokensSampleSize, "token classes of the attributed invocations", totalCost.tokens.total),
    turns: buildUncertainty(aggregateStates.turns, null, totalCost.turns, "one additional turn per errored or retried model invocation", totalCost.turns),
    tool_calls: buildUncertainty(aggregateStates.tool_calls, null, totalCost.tool_calls, "counted friction occurrences that consumed a tool call", totalCost.tool_calls),
    latency_ms: buildUncertainty(aggregateStates.latency_ms, null, eventRecords.length, "wall-clock duration recorded on the failing tool call or invocation", totalCost.latency_ms),
  };

  /** @type {Array<Record<string, any>>} */
  const unmeasuredDrivers = [];
  for (const [driver, definition] of Object.entries(FRICTION_DRIVERS).sort(([left], [right]) => left.localeCompare(right))) {
    if (driverTotals.has(driver)) continue;
    unmeasuredDrivers.push({
      driver,
      reason: sources.includes(definition.source) ? "no_occurrences" : `source_unavailable:${definition.source}`,
    });
  }

  const totalOccurrences = events.reduce((sum, event) => sum + event.occurrences, 0);
  const countedOccurrences = events.reduce((sum, event) => sum + event.counted_occurrences, 0);
  const runAICValues = invocations.map(invocation => invocation.aic).filter(value => value !== null);
  const totalRunAIC = ignoredRecords === 0 && runAICValues.length > 0 ? runAICValues.reduce((sum, value) => sum + Number(value), 0) : null;
  const totalRunAICPartial = totalRunAIC !== null && runAICValues.length < invocations.length;
  const emittedEvents = eventRecords.slice(0, MAX_FRICTION_EVENTS);
  const emittedEventIDs = new Set(emittedEvents.map(event => event.id));

  const friction = {
    measurement_state: sources.length === 0 ? STATE_UNAVAILABLE : aggregateStates.aic,
    canonical_unit: "aic",
    sources,
    total_events: events.length,
    total_occurrences: totalOccurrences,
    counted_occurrences: countedOccurrences,
    suppressed_occurrences: totalOccurrences - countedOccurrences,
    linked_invocations: linkedInvocationTotal,
    unattributed_occurrences: unattributedOccurrenceTotal,
    cost: totalCost,
    ...(totalRunAIC !== null ? { total_run_aic: totalRunAIC, ...(totalRunAICPartial ? { total_run_aic_partial: true } : {}), friction_ratio: totalRunAIC > 0 ? totalCost.aic / totalRunAIC : 0 } : {}),
    dimension_states: aggregateStates,
    uncertainty,
    drivers: Array.from(driverTotals.values())
      .sort((left, right) => String(left.driver).localeCompare(String(right.driver)))
      .map(driver => ({
        driver: driver.driver,
        class: driver.class,
        source: driver.source,
        events: driver.events,
        occurrences: driver.occurrences,
        counted_occurrences: driver.counted_occurrences,
        suppressed_occurrences: driver.suppressed_occurrences,
        state: driver.states.length === 0 ? STATE_UNAVAILABLE : driver.states.map(state => (state === DIMENSION_UNSUPPORTED ? STATE_UNAVAILABLE : state)).reduce(weakerState),
        cost: driver.cost,
      })),
    groups: groups.map(group => {
      const eventIDs = group.event_ids.filter(id => emittedEventIDs.has(id));
      return {
        ...group,
        event_ids: eventIDs,
        ...(eventIDs.length < group.event_ids.length ? { event_ids_truncated: true } : {}),
      };
    }),
    events: emittedEvents,
    ...(eventRecords.length > MAX_FRICTION_EVENTS ? { events_truncated: true } : {}),
    ...(unmeasuredDrivers.length > 0 ? { unmeasured_drivers: unmeasuredDrivers } : {}),
    ...(ignoredRecords > 0 ? { ignored_token_records: ignoredRecords } : {}),
  };

  return { friction, warnings };
}

module.exports = {
  computeFrictionCost,
  parseInvocationsFromJSONL,
  classifyInvocationFriction,
  computeBaselineStats,
  applyCausalGrouping,
  buildFrictionEvents,
  meanAndRelativeError,
  FRICTION_DRIVERS,
  COST_DIMENSIONS,
  TOKEN_CLASSES,
  MAX_FRICTION_EVENTS,
};
