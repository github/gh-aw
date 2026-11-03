// @ts-check

const EVENT_EVALUATION_VERSION = "gh-aw/event-predicates/v1";
const EVENT_EVALUATION_LIMIT = 64 * 100;
const object = value => value !== null && typeof value === "object" && !Array.isArray(value);
const pointer = value => typeof value === "string" && Buffer.byteLength(value) <= 1024 && /^\/(?:[^~]|~[01])*$/.test(value);
const own = (value, key) => Object.prototype.hasOwnProperty.call(value, key);
const comparable = value => typeof value === "boolean" || (typeof value === "string" && Array.from(value).length <= 1024) || (typeof value === "number" && Number.isSafeInteger(value));

function validEventPredicate(rule) {
  let count = 0;
  const visit = (node, depth) => {
    if (!object(node) || ++count > 64 || depth > 6) return false;
    if (Object.keys(node).some(key => !["all", "any", "path", "op", "value", "other", "fallback", "values", "separator", "root", "items"].includes(key))) return false;
    if (own(node, "all") || own(node, "any")) {
      const key = own(node, "all") ? "all" : "any";
      return Object.keys(node).length === 1 && Array.isArray(node[key]) && node[key].length > 0 && node[key].every(child => visit(child, depth + 1));
    }
    if (!pointer(node.path) || (node.other !== undefined && !pointer(node.other)) || (node.fallback !== undefined && !pointer(node.fallback)) || (node.root !== undefined && typeof node.root !== "boolean")) return false;
    const values = node.values || [];
    if (!Array.isArray(values) || values.length > 100 || !values.every(value => typeof value === "string" && value.length > 0 && Buffer.byteLength(value) <= 1024 && !value.includes("${{"))) return false;
    const op = node.op;
    if (values.length && !["enters", "leaves", "prefix", "path-prefix", "member-prefix", "url-id"].includes(op)) return false;
    if (node.separator !== undefined && (!["set-added", "set-removed", "member-prefix"].includes(op) || typeof node.separator !== "string" || Array.from(node.separator).length !== 1 || /[\r\n]/.test(node.separator))) return false;
    if (node.items !== undefined && op !== "any-item") return false;
    const primitive = value => typeof value === "boolean" || (typeof value === "string" && Array.from(value).length <= 1024 && !value.includes("${{")) || (typeof value === "number" && Number.isSafeInteger(value));
    switch (op) {
      case "equals":
        return (node.value !== undefined) !== (node.other !== undefined) && (node.value === undefined || primitive(node.value));
      case "present":
      case "absent":
      case "null":
      case "timestamp":
      case "text":
        return node.value === undefined && node.other === undefined;
      case "changed":
      case "set-added":
      case "set-removed":
        return node.other !== undefined && node.value === undefined;
      case "enters":
      case "leaves":
        return node.other !== undefined && node.value === undefined;
      case "prefix":
      case "path-prefix":
      case "member-prefix":
        return node.other === undefined && node.value === undefined;
      case "url-id":
        try {
          const parsed = new URL(node.value);
          return (
            node.other !== undefined &&
            typeof node.value === "string" &&
            Buffer.byteLength(node.value) <= 1024 &&
            node.value.endsWith("/") &&
            parsed.protocol === "https:" &&
            !parsed.username &&
            !parsed.password &&
            !parsed.search &&
            !parsed.hash &&
            !node.value.includes("${")
          );
        } catch {
          return false;
        }
      case "length":
        return Number.isInteger(node.value) && node.value >= 0 && node.value <= 100 && node.other === undefined;
      case "any-item":
        return node.items !== undefined && node.value === undefined && node.other === undefined && visit(node.items, depth + 1);
      default:
        return false;
    }
  };
  return visit(rule, 0);
}

function lookup(document, path) {
  if (!pointer(path)) return { present: false };
  let value = document;
  for (const raw of path.slice(1).split("/")) {
    const key = raw.replaceAll("~1", "/").replaceAll("~0", "~");
    if ((!object(value) && !Array.isArray(value)) || (Array.isArray(value) && !/^(?:0|[1-9][0-9]*)$/.test(key)) || !own(value, key)) return { present: false };
    value = value[key];
  }
  return { present: true, value };
}

function evaluateEventPredicate(rule, document) {
  if (!validEventPredicate(rule)) return { matched: false, known: false };
  const root = document;
  let remaining = EVENT_EVALUATION_LIMIT;
  const result = (matched, known = true) => ({ matched: matched && known, known });
  const visit = (node, current) => {
    if (--remaining < 0) return result(false, false);
    if (node.all || node.any) {
      const all = !!node.all;
      let unknown = false;
      for (const child of node.all || node.any) {
        const value = visit(child, current);
        if (remaining < 0) return result(false, false);
        if (value.known && value.matched !== all) return result(!all);
        unknown ||= !value.known;
      }
      return result(all, !unknown);
    }
    let value = lookup(node.root ? root : current, node.path);
    if (!value.present && node.fallback) value = lookup(root, node.fallback);
    if (node.op === "present" || node.op === "absent") return result(value.present === (node.op === "present"));
    if (!value.present) return result(false, false);
    const other = lookup(root, node.other);
    const actual = value.value;
    switch (node.op) {
      case "null":
        return result(actual === null);
      case "timestamp": {
        if (actual === null) return result(false);
        const valid = validTimestamp(actual);
        return result(valid, valid);
      }
      case "text":
        return result(typeof actual === "string" && actual.trim().length > 0 && Buffer.byteLength(actual) <= 1024, typeof actual === "string");
      case "equals":
        return result(actual === (node.other ? other.value : node.value), comparable(actual) && comparable(node.other ? other.value : node.value) && (!node.other || other.present));
      case "length":
        return result(Array.isArray(actual) && actual.length === node.value, Array.isArray(actual) && actual.length <= 100);
      case "prefix":
      case "path-prefix":
      case "member-prefix":
      case "url-id":
        return evaluatePrefix(node, actual, other, result, () => --remaining >= 0);
      case "any-item": {
        if (!Array.isArray(actual) || actual.length > 100) return result(false, false);
        let unknown = false;
        for (const item of actual) {
          const value = visit(node.items, item);
          if (remaining < 0) return result(false, false);
          if (value.known && value.matched) return result(true);
          unknown ||= !value.known;
        }
        return result(false, !unknown);
      }
      case "changed":
        return result(actual !== other.value, other.present && comparable(actual) && comparable(other.value) && typeof actual === typeof other.value);
      case "enters":
      case "leaves": {
        if (!other.present || typeof actual !== "string" || typeof other.value !== "string") return result(false, false);
        const values = node.values || [];
        const before = values.includes(other.value);
        const after = values.includes(actual);
        return result(node.op === "enters" ? !before && after : before && !after);
      }
      case "set-added":
      case "set-removed": {
        let before = stringSet(other.value, node.separator);
        let after = stringSet(actual, node.separator);
        if (!other.present || !before || !after) return result(false, false);
        if (node.op === "set-removed") [before, after] = [after, before];
        return result([...after].some(value => !before.has(value)));
      }
    }
    return result(false, false);
  };
  const evaluated = visit(rule, document);
  return remaining < 0 ? result(false, false) : evaluated;
}

function evaluatePrefix(rule, value, other, result, spend) {
  if (rule.op === "url-id") {
    const identity = typeof other.value === "string" ? other.value : typeof other.value === "number" && Number.isSafeInteger(other.value) && other.value >= 0 ? String(other.value) : null;
    if (typeof value !== "string" || !other.present || !identity || !/^[A-Za-z0-9._-]{1,256}$/.test(identity)) return result(false, false);
    try {
      const actual = new URL(value);
      if (actual.username || actual.password || actual.hash || actual.search.includes(";") || /%(?![0-9a-f]{2})/i.test(actual.search)) return result(false, false);
      for (const [key, item] of actual.searchParams) if (!(rule.values || []).includes(key) || actual.searchParams.getAll(key).length !== 1 || Buffer.byteLength(item) > 1024) return result(false);
      return result(value.split("?")[0] === rule.value + encodeURIComponent(identity));
    } catch {
      return result(false, false);
    }
  }
  if (rule.op === "member-prefix") {
    const members = stringSet(value, rule.separator);
    if (!members) return result(false, false);
    for (const member of [...members].sort((left, right) => Buffer.compare(Buffer.from(left), Buffer.from(right))))
      for (const prefix of rule.values || []) {
        if (!spend()) return result(false, false);
        if (member.startsWith(prefix)) return result(true);
      }
    return result(false);
  }
  if (typeof value !== "string" || Buffer.byteLength(value) > 65536) return result(false, false);
  for (const prefix of rule.values || []) {
    if (!spend()) return result(false, false);
    if (rule.op === "path-prefix") {
      const current = value.trim().toLowerCase();
      const parent = prefix.trim().replace(/\\+$/, "").toLowerCase();
      if (current === parent || current.startsWith(parent + "\\")) return result(true);
    } else if (value.startsWith(prefix)) {
      return result(true);
    }
  }
  return result(false);
}

function validTimestamp(value) {
  if (typeof value !== "string") return false;
  const match = /^(\d{4})-(\d\d)-(\d\d)T(\d\d):(\d\d):(\d\d)(?:\.\d{1,9})?(?:Z|[+-](?:0\d|1\d|2[0-3]):[0-5]\d)$/.exec(value);
  if (!match) return false;
  const [year, month, day, hour, minute, second] = match.slice(1).map(Number);
  const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  return month >= 1 && month <= 12 && day >= 1 && day <= days[month - 1] && hour < 24 && minute < 60 && second < 60;
}

function stringSet(value, separator) {
  if (separator) {
    if (typeof value !== "string" || Buffer.byteLength(value) > 65536) return null;
    value = value
      .split(separator)
      .map(item => item.trim())
      .filter(Boolean);
  }
  if (!Array.isArray(value) || value.length > 100 || !value.every(item => typeof item === "string" && item.length > 0 && Buffer.byteLength(item) <= 1024)) return null;
  return new Set(value);
}

function validEventFilters(filters) {
  return (
    object(filters) &&
    Object.keys(filters).length <= 16 &&
    Object.entries(filters).every(
      ([name, filter]) =>
        /^[a-z][a-z0-9-]{0,62}$/.test(name) &&
        object(filter) &&
        Object.keys(filter).every(key => ["path", "type", "values"].includes(key)) &&
        pointer(filter.path) &&
        ["string", "boolean", "integer"].includes(filter.type) &&
        (filter.values === undefined ||
          (Array.isArray(filter.values) &&
            filter.values.length <= 100 &&
            filter.values.every(value => (filter.type === "integer" ? Number.isSafeInteger(value) : typeof value === filter.type && (typeof value !== "string" || (Array.from(value).length <= 1024 && !value.includes("${{")))))))
    )
  );
}

function eventPredicateConfigured(rule) {
  if (!rule) return true;
  if (["enters", "leaves"].includes(rule.op) && !rule.values?.length) return false;
  return [...(rule.all || []), ...(rule.any || []), ...(rule.items ? [rule.items] : [])].every(eventPredicateConfigured);
}

function validEventURLBinding(identity, prefix, normalize) {
  if (identity === undefined && prefix === undefined) return true;
  return (
    pointer(identity) &&
    pointer(normalize?.url) &&
    typeof prefix === "string" &&
    Buffer.byteLength(prefix) <= 1024 &&
    prefix.startsWith("/") &&
    !prefix.startsWith("//") &&
    prefix.endsWith("/") &&
    !/[\r\n\t?#%{}$\\]/.test(prefix) &&
    !prefix.includes("..")
  );
}

module.exports = { EVENT_EVALUATION_VERSION, validEventPredicate, evaluateEventPredicate, validEventFilters, eventPredicateConfigured, validEventURLBinding };
