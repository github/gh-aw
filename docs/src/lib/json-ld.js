/** Escape serialized JSON for HTML's script parser without changing its values. @param {string} json */
export function escapeJsonLd(json) {
  return json.replace(/</g, "\\u003c");
}
