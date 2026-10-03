// @ts-check
"use strict";

const { replayTransactions } = require("./work_queue_replay.cjs");

const pointer = { type: "string", pattern: "^/(?:[^~]|~[01])*$" };
const scalarSchema = { type: ["string", "number", "boolean", "null"] };
const SELECTION_SCHEMA = {
  type: "object",
  properties: {
    filter: {
      type: "array",
      maxItems: 32,
      items: {
        type: "object",
        properties: {
          field: pointer,
          op: { type: "string", enum: ["eq", "ne", "lt", "lte", "gt", "gte", "in", "exists"] },
          value: { anyOf: [scalarSchema, { type: "array", items: scalarSchema, minItems: 1, maxItems: 100 }] },
        },
        required: ["field", "op", "value"],
        additionalProperties: false,
      },
    },
    sort: {
      type: "array",
      maxItems: 32,
      items: {
        type: "object",
        properties: { field: pointer, direction: { type: "string", enum: ["asc", "desc"] } },
        required: ["field"],
        additionalProperties: false,
      },
    },
    group: {
      type: "object",
      properties: { fields: { type: "array", items: pointer, minItems: 1, maxItems: 8 }, max_active: { type: "integer", minimum: 1, maximum: 100 } },
      required: ["fields"],
      additionalProperties: false,
    },
  },
  additionalProperties: false,
};

function record(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function scalar(value) {
  return value === null || ["string", "boolean"].includes(typeof value) || (typeof value === "number" && Number.isFinite(value));
}

function validPointer(value) {
  return typeof value === "string" && /^\/(?:[^~]|~[01])*$/.test(value);
}

function onlyFields(value, fields) {
  return record(value) && Object.keys(value).every(field => fields.includes(field));
}

function validateSelection(selection) {
  if (!onlyFields(selection, ["filter", "sort", "group"])) throw new TypeError("selection must be an object containing only filter, sort, and group");
  for (const key of ["filter", "sort"]) {
    if (selection[key] !== undefined && (!Array.isArray(selection[key]) || selection[key].length > 32)) throw new TypeError(`${key} must be an array of at most 32 fields`);
  }
  for (const condition of selection.filter || []) {
    if (!onlyFields(condition, ["field", "op", "value"]) || !validPointer(condition.field) || !Object.hasOwn(condition, "value")) throw new TypeError("filter requires a JSON Pointer field, operator, and value");
    if (!["eq", "ne", "lt", "lte", "gt", "gte", "in", "exists"].includes(condition.op)) throw new TypeError("unknown filter operator");
    if (condition.op === "in") {
      if (!Array.isArray(condition.value) || condition.value.length < 1 || condition.value.length > 100 || !condition.value.every(scalar)) throw new TypeError("in filter requires 1-100 scalar values");
    } else if (condition.op === "exists") {
      if (typeof condition.value !== "boolean") throw new TypeError("exists filter requires a boolean value");
    } else if (!scalar(condition.value)) throw new TypeError("filter value must be a scalar");
  }
  for (const field of selection.sort || []) {
    if (!onlyFields(field, ["field", "direction"]) || !validPointer(field.field) || (field.direction !== undefined && !["asc", "desc"].includes(field.direction)))
      throw new TypeError("sort requires a JSON Pointer field and asc or desc direction");
  }
  if (selection.group !== undefined) {
    const group = selection.group;
    if (!onlyFields(group, ["fields", "max_active"]) || !Array.isArray(group.fields) || group.fields.length < 1 || group.fields.length > 8 || !group.fields.every(validPointer)) throw new TypeError("group requires 1-8 JSON Pointer fields");
    if (group.max_active !== undefined && (!Number.isInteger(group.max_active) || group.max_active < 1 || group.max_active > 100)) throw new TypeError("group max_active must be between 1 and 100");
  }
  return selection;
}

function lookup(payload, pointer) {
  let value = payload;
  for (const segment of pointer.slice(1).split("/")) {
    const key = segment.replace(/~1/g, "/").replace(/~0/g, "~");
    if (Array.isArray(value)) {
      if (!/^(0|[1-9][0-9]*)$/.test(key) || Number(key) >= value.length) return { value: null, present: false };
    } else if (!record(value) || !Object.hasOwn(value, key)) return { value: null, present: false };
    value = value[key];
  }
  return { value, present: true };
}

function rank(value) {
  return value === null ? 0 : typeof value === "boolean" ? 1 : typeof value === "number" ? 2 : 3;
}

function compare(left, right) {
  const difference = rank(left) - rank(right);
  if (difference) return difference;
  if (typeof left === "string") return Buffer.compare(Buffer.from(left), Buffer.from(right));
  return left < right ? -1 : left > right ? 1 : 0;
}

function matches(payload, condition) {
  const field = lookup(payload, condition.field);
  if (condition.op === "exists") return field.present === condition.value;
  if (!field.present || !scalar(field.value)) return false;
  if (condition.op === "in") return condition.value.some(value => compare(field.value, value) === 0);
  const comparison = compare(field.value, condition.value);
  if (condition.op === "eq") return comparison === 0;
  if (condition.op === "ne") return comparison !== 0;
  if (rank(field.value) !== rank(condition.value)) return false;
  return condition.op === "lt" ? comparison < 0 : condition.op === "lte" ? comparison <= 0 : condition.op === "gt" ? comparison > 0 : comparison >= 0;
}

function selectNext(transactions, selection = {}) {
  validateSelection(selection);
  const projection = replayTransactions(transactions);
  const active = new Map();
  const candidates = [];
  for (const work of projection.transactions.filter(transaction => transaction.kind === "Work")) {
    const state = projection.work[work.work_id];
    if (!["available", "claimed"].includes(state)) continue;
    const values = (selection.group?.fields || []).map(field => {
      const value = lookup(work.work, field);
      if (!scalar(value.value)) throw new TypeError("group fields must contain scalar values");
      return value;
    });
    const group = JSON.stringify(values);
    if (state === "claimed") {
      active.set(group, (active.get(group) || 0) + 1);
      continue;
    }
    if (!(selection.filter || []).every(condition => matches(work.work, condition))) continue;
    const sort = (selection.sort || []).map(field => {
      const value = lookup(work.work, field.field);
      if (!scalar(value.value)) throw new TypeError("sort fields must contain scalar values");
      return value;
    });
    candidates.push({ work, group, sort });
  }
  candidates.sort((left, right) => {
    for (const [index, field] of (selection.sort || []).entries()) {
      const a = left.sort[index];
      const b = right.sort[index];
      if (a.present !== b.present) return a.present ? -1 : 1;
      const comparison = compare(a.value, b.value) * (field.direction === "desc" ? -1 : 1);
      if (comparison) return comparison;
    }
    return left.work.sequence - right.work.sequence || compare(left.work.work_id, right.work.work_id);
  });
  return candidates.find(candidate => !selection.group || (active.get(candidate.group) || 0) < (selection.group.max_active || 1))?.work || null;
}

module.exports = { SELECTION_SCHEMA, selectNext, validateSelection };
