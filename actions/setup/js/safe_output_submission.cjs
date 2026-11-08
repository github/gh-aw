// @ts-check

const { AsyncLocalStorage } = require("node:async_hooks");
const { randomUUID } = require("node:crypto");

const OUTPUT_ID_PATTERN = "^so_[A-Za-z0-9_-]{12}$";
const outputIdRegex = new RegExp(OUTPUT_ID_PATTERN);
const DEPENDENCY_UNSUPPORTED_TYPES = new Set(["upload_asset", "noop", "call_workflow", "push_repo_memory", "report_incomplete", "report_missing_tool", "report_missing_data"]);
const DELEGATED_EFFECT_TYPES = new Set([...DEPENDENCY_UNSUPPORTED_TYPES, "upload_code_coverage", "dispatch_workflow", "dispatch_repository"]);

/**
 * @param {string|undefined} type
 */
function isDependencyUnsupportedType(type) {
  return typeof type === "string" && DEPENDENCY_UNSUPPORTED_TYPES.has(type.replace(/-/g, "_").toLowerCase());
}

/**
 * @param {string|undefined} type
 */
function isDependencyDelegatedType(type) {
  return typeof type === "string" && DELEGATED_EFFECT_TYPES.has(type.replace(/-/g, "_").toLowerCase());
}

/**
 * Validate metadata again when reading the untrusted JSONL artifact.
 * @param {any} input
 * @returns {string|undefined}
 */
function validateSubmissionMetadata(input) {
  if (input.output_id !== undefined && (typeof input.output_id !== "string" || !outputIdRegex.test(input.output_id))) return "output_id must be a safe-output identifier returned by a safe-output call.";
  if (input["depends-on"] !== undefined && input.depends_on !== undefined) return "Provide depends-on only once, not together with depends_on.";
  const dependency = input["depends-on"] !== undefined ? input["depends-on"] : input.depends_on;
  if (dependency !== undefined) {
    const ids = Array.isArray(dependency) ? dependency : [dependency];
    if (!ids.length || ids.some(id => typeof id !== "string" || !outputIdRegex.test(id)) || new Set(ids).size !== ids.length) {
      return "depends-on must be an output_id or a nonempty array of distinct output_id values returned by safe-output calls.";
    }
  }
  return undefined;
}

/**
 * Add the shared dependency parameter without changing tool-specific inputs.
 * @param {any} tool
 */
function addDependencySchema(tool) {
  tool.inputSchema ||= { type: "object" };
  tool.inputSchema.properties ||= {};
  const dependencySchema = {
    description:
      "One output_id, or a nonempty array of output_id values returned by earlier safe-output calls. Execute only after every prerequisite succeeds; otherwise this output fails. These are not temporary resource IDs. Do not use ledger output IDs as prerequisites: ledger writes are queued until the later persistence job. Submit related ledger operations sequentially without depends-on; use temporary record IDs for same-batch references.",
    "x-synonyms": ["depends_on"],
    oneOf: [
      { type: "string", pattern: OUTPUT_ID_PATTERN },
      { type: "array", minItems: 1, uniqueItems: true, items: { type: "string", pattern: OUTPUT_ID_PATTERN } },
    ],
  };
  const properties = tool.inputSchema.properties;
  for (const reservedName of ["depends_on", "output_id"]) {
    if (Object.hasOwn(properties, reservedName)) {
      throw new TypeError(`Safe-output tool '${tool.name}' declares reserved input '${reservedName}'.`);
    }
  }
  if (Object.hasOwn(properties, "depends-on") && JSON.stringify(properties["depends-on"]) !== JSON.stringify(dependencySchema)) {
    throw new TypeError(`Safe-output tool '${tool.name}' declares reserved input 'depends-on'.`);
  }
  properties["depends-on"] = dependencySchema;
}

/**
 * Keep submission metadata outside handler-specific argument transformations.
 * @returns {{wrap: (handler: Function, supportsDependencies?: boolean) => ((...args: any[]) => any), append: (entry: any, write: Function) => void}}
 */
function createSubmissionTracker() {
  const submissions = new AsyncLocalStorage();

  function wrap(handler, supportsDependencies = true) {
    return (...args) => {
      const input = args[0] || {};
      const dependency = input["depends-on"] !== undefined ? input["depends-on"] : input.depends_on;
      const error = validateSubmissionMetadata({ ...input, output_id: undefined });
      if (error) {
        return { isError: true, content: [{ type: "text", text: JSON.stringify({ result: "error", error }) }] };
      }
      if (dependency !== undefined && !supportsDependencies) {
        return { isError: true, content: [{ type: "text", text: JSON.stringify({ result: "error", error: "depends-on is not supported for outputs that execute outside the safe-output handler manager." }) }] };
      }
      const submission = { dependency, outputIds: [] };
      const finish = response => {
        if (response?.isError || !submission.outputIds.length) return response;
        const metadata = { output_id: submission.outputIds[0], ...(submission.outputIds.length > 1 ? { output_ids: submission.outputIds } : {}) };
        return {
          ...response,
          ...(response.structuredContent && { structuredContent: { ...response.structuredContent, ...metadata } }),
          content: response.content.map(item => {
            if (item.type !== "text") return item;
            try {
              const payload = JSON.parse(item.text);
              if (payload && typeof payload === "object" && !Array.isArray(payload)) return { ...item, text: JSON.stringify({ ...payload, ...metadata }) };
            } catch {
              // Preserve non-JSON tool responses.
            }
            return item;
          }),
        };
      };
      return submissions.run(submission, () => {
        const handlerArgs = [...args];
        if (input && typeof input === "object" && !Array.isArray(input)) {
          handlerArgs[0] = Object.fromEntries(Object.entries(input).filter(([key]) => !["output_id", "depends-on", "depends_on"].includes(key)));
        }
        const response = handler(...handlerArgs);
        return response && typeof response.then === "function" ? response.then(finish) : finish(response);
      });
    };
  }

  function append(entry, write) {
    const submission = submissions.getStore();
    const dependency = submission?.dependency ?? entry["depends-on"] ?? entry.depends_on;
    if (dependency !== undefined && isDependencyUnsupportedType(entry.type)) {
      throw new Error(`depends-on is not supported for ${entry.type}, which executes outside the safe-output handler manager.`);
    }
    const outputId = `so_${Buffer.from(randomUUID().replace(/-/g, ""), "hex").toString("base64url").slice(0, 12)}`;
    const recorded = { ...entry, output_id: outputId };
    delete recorded.depends_on;
    if (dependency !== undefined) recorded["depends-on"] = dependency;
    write(recorded);
    submission?.outputIds.push(outputId);
  }

  return { wrap, append };
}

module.exports = { addDependencySchema, createSubmissionTracker, isDependencyDelegatedType, isDependencyUnsupportedType, validateSubmissionMetadata };
