// @ts-check

const crypto = require("node:crypto");
const { ERR_PARSE, ERR_VALIDATION } = require("./error_codes.cjs");

const MAX_WORKFLOW_VALUE_CSV_FILE_BYTES = 16 * 1024 * 1024;
const MAX_WORKFLOW_VALUE_CSV_TOTAL_BYTES = 48 * 1024 * 1024;

const column = (name, type, nullable, description) => ({ name, type, nullable, description });

const WORKFLOW_VALUE_CSV_SCHEMA = {
  schema_version: 1,
  dataset: "gh-aw-workflow-value",
  format: {
    media_type: "text/csv",
    dialect: "RFC 4180",
    encoding: "UTF-8",
    header: true,
    null: "empty field",
    line_ending: "LF",
  },
  tables: {
    workflows: {
      path: "workflows.csv",
      primary_key: ["workflow_id"],
      columns: [
        column("workflow_id", "string", false, "Stable top-level paired workflow identity."),
        column("name", "string", false, "Workflow name extracted from source frontmatter."),
        column("description", "string", true, "Workflow description extracted from source frontmatter."),
        column("source_reference", "string", true, "Upstream source declaration when present."),
        column("source_path", "string", false, "Repository-relative workflow source path."),
        column("lock_path", "string", false, "Repository-relative compiled lock path."),
        column("source_sha256", "sha256", false, "SHA-256 of the current source bytes."),
        column("lock_sha256", "sha256", false, "SHA-256 of the current lock bytes."),
        column("frontmatter_sha256", "sha256", false, "SHA-256 of the current source frontmatter."),
        column("operational_value_state", "string", false, "Optional run-scoped operational-value declaration state."),
        column("operational_value_scope", "string", false, "Scope of the optional operational-value declaration."),
        column("git_history_status", "enum:complete|shallow|missing", false, "Availability of git introduction history."),
        column("source_introduced_commit", "git_sha", true, "Commit that introduced the source path, following renames."),
        column("source_introduced_at", "datetime", true, "Committer time of source introduction."),
        column("source_revised_commit", "git_sha", true, "Most recent commit that revised the source path."),
        column("source_revised_at", "datetime", true, "Committer time of the most recent source revision."),
        column("lock_introduced_commit", "git_sha", true, "Commit that introduced the lock path, following renames."),
        column("lock_introduced_at", "datetime", true, "Committer time of lock introduction."),
        column("lock_revised_commit", "git_sha", true, "Most recent commit that revised the lock path."),
        column("lock_revised_at", "datetime", true, "Committer time of the most recent lock revision."),
        column("workflow_introduced_commit", "git_sha", true, "Later introduction commit of the required source/lock pair."),
        column("workflow_introduced_at", "datetime", true, "Time when both source and lock existed as a pair."),
        column("workflow_revised_commit", "git_sha", true, "Most recent source-or-lock revision commit."),
        column("workflow_revised_at", "datetime", true, "Most recent source-or-lock revision time."),
      ],
    },
    contracts: {
      path: "contracts.csv",
      primary_key: ["workflow_id"],
      foreign_keys: [{ columns: ["workflow_id"], references: "workflows(workflow_id)" }],
      columns: [
        column("workflow_id", "string", false, "Workflow identity."),
        column("contract_version", "string", false, "Version of the deterministic derived contract."),
        column("contract_sha256", "sha256", false, "Digest of the canonical contract fields."),
        column("contract_basis", "string", false, "Recorded evidence used to derive the contract."),
        column("session_evidence_source", "string", true, "Recorded agent-session evidence source."),
        column("delivery_evidence_status", "enum:available|unavailable", false, "Availability of recorded delivery observations."),
        column("delivery_evidence_sources", "string", true, "Recorded delivery evidence source classes."),
        column("delivery_native_unit", "string", false, "Native delivery unit."),
        column("adoption_evidence_status", "enum:available|unavailable", false, "Availability of recorded adoption observations."),
        column("adoption_evidence_sources", "string", true, "Recorded adoption evidence source classes."),
        column("adoption_native_unit", "string", false, "Native adoption unit."),
        column("attributed_value_definition_state", "string", false, "Availability of a reviewed longitudinal value definition."),
        column("attributed_value_native_unit", "string", true, "Native value unit when a definition exists."),
        column("attribution_evidence_status", "string", false, "Recorded attribution evidence status."),
        column("survival_window_status", "string", false, "Recorded complete-window survival status."),
        column("operational_value_declaration_state", "string", false, "Workflow declaration state for optional run-scoped operational value."),
        column("operational_value_scope", "string", false, "Relationship of optional run-scoped graders to this contract."),
        column("excluded_proxy_evidence", "string", false, "Inputs forbidden as adoption or attributed-value substitutes."),
      ],
    },
    periods: {
      path: "periods.csv",
      primary_key: ["period_date"],
      columns: [
        column("period_date", "date", false, "UTC retained period identity."),
        column("period_role", "enum:current|retained_history", false, "Current period or retained historical period."),
        column("timestamp", "datetime", true, "Metrics observation time."),
        column("collection_status", "string", false, "Metrics collection status."),
        column("comparability", "string", false, "Selected-window comparability."),
        column("session_evidence_source", "string", true, "Recorded source of the workflow metrics."),
        column("session_reported_workflow_count", "integer", false, "Workflows reporting an agent-session source."),
        column("session_delivery_workflow_count", "integer", false, "Workflows with recorded delivery fields."),
        column("session_adoption_workflow_count", "integer", false, "Workflows with recorded adoption fields."),
        column("inventory_coverage", "enum:complete|partial|unavailable", false, "Coverage of current workflow identities."),
        column("inventory_membership_mode", "enum:current|present|missing_current|none", false, "Compact inventory membership encoding."),
        column("reported_workflow_count", "integer", false, "All workflow identities reported in the period."),
        column("current_reported_workflow_count", "integer", false, "Current workflow identities reported in the period."),
        column("unknown_workflow_count", "integer", false, "Reported identities absent from the current inventory."),
        column("metrics_source", "string", true, "Retained metrics source path."),
        column("lifecycle_source", "string", true, "Retained lifecycle source path."),
        column("selected_window_source", "string", false, "Evidence source for selected-window observations."),
        column("selected_window_start", "datetime", true, "Selected-window start when available."),
        column("selected_window_end", "datetime", true, "Selected-window end when available."),
      ],
    },
    delivery_observations: {
      path: "delivery_observations.csv",
      primary_key: ["period_date", "workflow_id", "observation_scope"],
      foreign_keys: [
        { columns: ["period_date"], references: "periods(period_date)" },
        { columns: ["workflow_id"], references: "workflows(workflow_id)" },
      ],
      columns: [
        column("period_date", "date", false, "UTC retained period identity."),
        column("period_role", "enum:current|retained_history", false, "Current period or retained historical period."),
        column("workflow_id", "string", false, "Workflow identity."),
        column("observation_scope", "enum:selected_window|retained_population", false, "Selected cohort or retained population."),
        column("workflow_applicability", "enum:applicable|not_applicable", false, "Whether the workflow existed in the period."),
        column("coverage", "enum:complete|partial|unavailable", false, "Evidence coverage."),
        column("delivered", "integer", true, "Delivered effects in the native unit."),
        column("native_unit", "string", false, "Native delivery unit."),
        column("reason_code", "string", true, "Reason for unavailable evidence."),
        column("source_kind", "string", false, "Evidence source class."),
        column("source_path", "string", true, "Retained source path or canonical ledger path."),
      ],
    },
    adoption_observations: {
      path: "adoption_observations.csv",
      primary_key: ["period_date", "workflow_id", "observation_scope"],
      foreign_keys: [
        { columns: ["period_date"], references: "periods(period_date)" },
        { columns: ["workflow_id"], references: "workflows(workflow_id)" },
      ],
      columns: [
        column("period_date", "date", false, "UTC retained period identity."),
        column("period_role", "enum:current|retained_history", false, "Current period or retained historical period."),
        column("workflow_id", "string", false, "Workflow identity."),
        column("observation_scope", "enum:selected_window|retained_population", false, "Selected cohort or retained population."),
        column("workflow_applicability", "enum:applicable|not_applicable", false, "Whether the workflow existed in the period."),
        column("coverage", "enum:complete|partial|unavailable", false, "Evidence coverage."),
        column("accepted", "integer", true, "All accepted delivery identities."),
        column("accepted_human", "integer", true, "Human-verified accepted delivery identities."),
        column("accepted_delivery_fallback", "integer", true, "Delivery-time fallback accepted identities."),
        column("rejected", "integer", true, "Rejected or reversed identities."),
        column("not_applicable", "integer", true, "Infrastructure identities classified as not applicable."),
        column("pending", "integer", true, "Pending identities."),
        column("lifecycle", "integer", true, "Automated lifecycle decisions without verified human adoption."),
        column("unknown", "integer", true, "Identities without sufficient adoption evidence."),
        column("native_unit", "string", false, "Native adoption unit."),
        column("reason_code", "string", true, "Reason for unavailable evidence."),
        column("source_kind", "string", false, "Evidence source class."),
        column("source_path", "string", true, "Retained source path or canonical ledger path."),
      ],
    },
    attributed_value_observations: {
      path: "attributed_value_observations.csv",
      primary_key: ["period_date", "workflow_id"],
      foreign_keys: [
        { columns: ["period_date"], references: "periods(period_date)" },
        { columns: ["workflow_id"], references: "workflows(workflow_id)" },
      ],
      columns: [
        column("period_date", "date", false, "UTC retained period identity."),
        column("period_role", "enum:current|retained_history", false, "Current period or retained historical period."),
        column("workflow_id", "string", false, "Workflow identity."),
        column("workflow_applicability", "enum:applicable|not_applicable", false, "Whether the workflow existed in the period."),
        column("status_coverage", "enum:complete|partial|unavailable", false, "Coverage of value evidence state."),
        column("measurement_coverage", "enum:complete|partial|unavailable", false, "Coverage of native-unit measurements."),
        column("state", "enum:measured|needs_definition|unmeasured|unavailable|not_applicable", false, "Attributed-value evidence state."),
        column("value", "number", true, "Native-unit attributed value."),
        column("native_unit", "string", true, "Native value unit; never normalized across workflows."),
        column("reason_code", "string", true, "Reason for the evidence state."),
        column("definition_source", "string", true, "Reviewed frozen definition source."),
        column("attribution_evidence_status", "string", false, "Availability of required attribution evidence."),
        column("survival_window_status", "string", false, "Availability of complete post-adoption-window survival."),
      ],
    },
  },
};

/** @param {string | Buffer} value */
function sha256(value) {
  return crypto.createHash("sha256").update(value).digest("hex");
}

/** @param {unknown} value */
function csvCell(value) {
  if (value === null || value === undefined) return "";
  const text = String(value);
  return /[",\r\n]/.test(text) ? `"${text.replaceAll('"', '""')}"` : text;
}

/** @param {Array<Record<string, unknown>>} rows @param {Array<{name: string}>} columns */
function serializeCsv(rows, columns) {
  const names = columns.map(item => item.name);
  const lines = [names.map(csvCell).join(",")];
  for (const row of rows) {
    const extra = Object.keys(row).filter(key => !names.includes(key));
    if (extra.length) throw new Error(`${ERR_VALIDATION}: Workflow value CSV row contains unknown columns: ${extra.join(", ")}`);
    lines.push(names.map(name => csvCell(row[name])).join(","));
  }
  return `${lines.join("\n")}\n`;
}

/** @param {string} content */
function parseCsv(content) {
  /** @type {string[][]} */
  const records = [];
  let record = [];
  let field = "";
  let quoted = false;
  for (let index = 0; index < content.length; index++) {
    const character = content[index];
    if (quoted) {
      if (character === '"') {
        if (content[index + 1] === '"') {
          field += '"';
          index++;
        } else {
          quoted = false;
        }
      } else {
        field += character;
      }
      continue;
    }
    if (character === '"' && field === "") {
      quoted = true;
    } else if (character === ",") {
      record.push(field);
      field = "";
    } else if (character === "\n") {
      record.push(field);
      records.push(record);
      record = [];
      field = "";
    } else if (character !== "\r") {
      field += character;
    }
  }
  if (quoted) throw new Error(`${ERR_PARSE}: Unterminated quoted field in workflow value CSV`);
  if (field || record.length) {
    record.push(field);
    records.push(record);
  }
  if (!records.length) throw new Error(`${ERR_PARSE}: Empty workflow value CSV`);
  const header = records[0];
  if (!header.length || new Set(header).size !== header.length) throw new Error(`${ERR_PARSE}: Invalid workflow value CSV header`);
  return {
    header,
    rows: records.slice(1).map(values => {
      if (values.length !== header.length) throw new Error(`${ERR_PARSE}: Workflow value CSV row has ${values.length} fields; expected ${header.length}`);
      return Object.fromEntries(header.map((name, index) => [name, values[index] === "" ? null : values[index]]));
    }),
  };
}

/** @param {Map<string, string>} files */
function validateWorkflowValueCsvFiles(files) {
  let totalBytes = 0;
  for (const [name, content] of files) {
    const bytes = Buffer.byteLength(content);
    if (bytes > MAX_WORKFLOW_VALUE_CSV_FILE_BYTES) {
      throw new Error(`${ERR_VALIDATION}: Complete workflow value CSV file exceeds 16 MiB without dropping rows: ${name} (${bytes} bytes)`);
    }
    totalBytes += bytes;
  }
  if (totalBytes > MAX_WORKFLOW_VALUE_CSV_TOTAL_BYTES) {
    throw new Error(`${ERR_VALIDATION}: Complete workflow value CSV dataset exceeds 48 MiB without dropping rows (${totalBytes} bytes)`);
  }
  return totalBytes;
}

module.exports = {
  MAX_WORKFLOW_VALUE_CSV_FILE_BYTES,
  MAX_WORKFLOW_VALUE_CSV_TOTAL_BYTES,
  WORKFLOW_VALUE_CSV_SCHEMA,
  parseCsv,
  serializeCsv,
  sha256,
  validateWorkflowValueCsvFiles,
};
