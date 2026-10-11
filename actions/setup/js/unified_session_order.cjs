// @ts-check

/** @typedef {"seconds" | "milliseconds" | "nanoseconds"} TimestampUnit */

/**
 * Source schemas, not timestamp magnitude, determine numeric units.
 * Keep nanoseconds as integers until producing the display-only millisecond key.
 * @param {any} record
 * @param {TimestampUnit} [unit]
 * @returns {bigint | undefined}
 */
function sessionTimestampNs(record, unit = "milliseconds") {
  const value = record.timestamp ?? record.ts ?? record.time ?? record.created_at ?? record.message?.timestamp;
  if (unit === "nanoseconds") {
    if (typeof value !== "string" || !/^\d{1,22}$/.test(value)) return undefined;
    const ns = BigInt(value);
    return ns <= 8640000000000000000000n ? ns : undefined;
  }
  if (typeof value === "number") {
    const ms = unit === "seconds" ? value * 1000 : value;
    if (!Number.isFinite(ms) || Math.abs(ms) > 8640000000000000) return undefined;
    const whole = Math.trunc(ms);
    return BigInt(whole) * 1000000n + BigInt(Math.round((ms - whole) * 1000000));
  }
  if (typeof value !== "string") return undefined;
  const match = /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(?:\.(\d+))?(Z|[+-]\d{2}:\d{2})$/i.exec(value);
  if (!match) return undefined;
  const ms = Date.parse(`${match[1]}${match[3]}`);
  return Number.isFinite(ms) ? BigInt(ms) * 1000000n + BigInt((match[2] ?? "").padEnd(9, "0").slice(0, 9)) : undefined;
}

/** @param {any} record @param {TimestampUnit} [unit] @returns {number | undefined} */
function sessionTimestamp(record, unit = "milliseconds") {
  const ns = sessionTimestampNs(record, unit);
  return ns === undefined ? undefined : Number(ns / 1000000n) + Number(ns % 1000000n) / 1000000;
}

/**
 * Missing clocks inherit an ordering anchor, never an observed timestamp.
 * Clock regressions cannot reverse a sequential source. OTLP exports are
 * unordered batches of spans/logs, rather than an execution sequence.
 * @template T
 * @param {Array<{component: string, events: T[], timestampUnit?: TimestampUnit}>} sources
 * @returns {Array<{event: T, time: bigint | undefined}>}
 */
function sessionOrderingEntries(sources) {
  return sources.flatMap(source => {
    const times = source.events.map(event => sessionTimestampNs(event, source.timestampUnit));
    let anchor = times.find(time => time !== undefined);
    return source.events.map((event, index) => {
      const time = times[index];
      if (time !== undefined && (anchor === undefined || time > anchor)) anchor = time;
      return { event, time: source.component === "otel" ? time : anchor };
    });
  });
}

/**
 * @template T
 * @param {Array<{component: string, events: T[], timestampUnit?: TimestampUnit}>} sources
 * @returns {T[]}
 */
function orderSessionSources(sources) {
  const entries = sessionOrderingEntries(sources);
  entries.sort((left, right) => {
    if (left.time === right.time) return 0;
    if (left.time === undefined) return 1;
    if (right.time === undefined) return -1;
    return left.time < right.time ? -1 : 1;
  });
  return entries.map(entry => entry.event);
}

module.exports = { sessionTimestamp, sessionTimestampNs, sessionOrderingEntries, orderSessionSources };
