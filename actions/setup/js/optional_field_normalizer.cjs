// @ts-check

/**
 * Blank optional typed/constrained fields mean "not supplied"; free-text clears do not.
 * Accepts either a JSON Schema property or a collector field validation rule.
 * @param {any} value
 * @param {any} field
 * @param {boolean} [required]
 * @param {string} [fieldName]
 * @returns {boolean}
 */
function isBlankOptionalField(value, field, required = false, fieldName) {
  if (!field || required || field.required === true || typeof value !== "string" || value.trim() !== "") {
    return false;
  }
  return (
    // Stack roots are branch references, not free text that can be cleared.
    fieldName === "stack_root" ||
    (field.type !== undefined && field.type !== "string") ||
    field.positiveInteger ||
    field.optionalPositiveInteger ||
    field.issueOrPRNumber ||
    field.issueNumberOrTemporaryId ||
    field.pattern !== undefined ||
    field.enum !== undefined ||
    field.format !== undefined ||
    field.minLength > 0
  );
}

/**
 * @param {any} args
 * @param {Record<string, any>} fields
 * @param {string[]} [required]
 * @returns {any}
 */
function normalizeBlankOptionalFields(args, fields, required = []) {
  const normalized = { ...args };
  for (const [name, field] of Object.entries(fields)) {
    if (isBlankOptionalField(normalized[name], field, required.includes(name), name)) {
      delete normalized[name];
    }
  }
  return normalized;
}

module.exports = { isBlankOptionalField, normalizeBlankOptionalFields };
