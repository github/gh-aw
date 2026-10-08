// @ts-check
"use strict";

const { canonical, closed } = require("./work_queue_codec.cjs");

const BUILTIN_EFFECT_FIELDS = Object.freeze({
  create_issue: ["title", "body", "labels", "assignees", "milestone"],
  update_issue: ["title", "body", "labels", "assignees", "milestone", "state", "status", "state_reason"],
  close_issue: ["state_reason"],
  add_comment: ["body"],
  add_labels: ["labels"],
  remove_labels: ["labels"],
  replace_label: ["label_to_add", "label_to_remove"],
});
const RESOURCE_NUMBER_FIELDS = ["item_number", "issue_number", "pull_request_number"];

function builtinAdapterFields(type) {
  const fields = BUILTIN_EFFECT_FIELDS[type];
  return fields && [...fields, ...(type === "create_issue" ? [] : RESOURCE_NUMBER_FIELDS)];
}

function builtinTargetNumber(message) {
  const numbers = RESOURCE_NUMBER_FIELDS.filter(field => Object.hasOwn(message, field)).map(field => {
    const value = message[field];
    if (typeof value === "number" ? !Number.isSafeInteger(value) || value < 1 : typeof value !== "string" || !/^[1-9][0-9]{0,255}$/.test(value)) {
      throw new Error("Native builtin Claim effect requires a positive canonical resource number");
    }
    return String(value);
  });
  if (new Set(numbers).size > 1) throw new Error("Native builtin Claim effect contains conflicting explicit resource numbers");
  return numbers[0];
}

function adapterVerifierId(adapter, type) {
  return adapter["verifier-id"] || type;
}

function adapterEffectFields(adapter, message) {
  return Object.fromEntries([...Object.keys(adapter.expected || {}), ...Object.keys(adapter["field-map"] || {})].map(field => [field, structuredClone(message[field])]));
}

function matchesDeclaredAdapterExpected(adapter, fields, verification) {
  if (verification === undefined) return true;
  closed(verification, ["verifier_id", "expected"], [], "declared adapter verification");
  const required = [...Object.keys(adapter.expected || {}), ...Object.keys(adapter["field-map"] || {})];
  closed(verification.expected, required, [], "declared adapter expected effect fields");
  return canonical(verification.expected) === canonical(fields);
}

module.exports = { BUILTIN_EFFECT_FIELDS, builtinAdapterFields, builtinTargetNumber, adapterVerifierId, adapterEffectFields, matchesDeclaredAdapterExpected };
