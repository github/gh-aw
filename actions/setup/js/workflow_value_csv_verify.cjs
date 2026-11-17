// @ts-check

const fs = require("node:fs");
const path = require("node:path");
const { ERR_PARSE, ERR_SYSTEM, ERR_VALIDATION } = require("./error_codes.cjs");
const { loadWorkflowInventory } = require("./workflow_value_report.cjs");
const { EXCLUDED_PROXY_EVIDENCE } = require("./workflow_value_csv.cjs");
const { MAX_WORKFLOW_VALUE_CSV_FILE_BYTES, MAX_WORKFLOW_VALUE_CSV_TOTAL_BYTES, WORKFLOW_VALUE_CSV_SCHEMA, parseCsv, sha256 } = require("./workflow_value_csv_format.cjs");

const EXPECTED_FILES = ["manifest.json", "schema.json", ...Object.values(WORKFLOW_VALUE_CSV_SCHEMA.tables).map(table => table.path)].sort();

/** @param {string} filename */
function read(filename) {
  let stat;
  try {
    stat = fs.lstatSync(filename);
  } catch (error) {
    throw new Error(`${ERR_SYSTEM}: Missing workflow value CSV artifact file: ${filename}`, { cause: error });
  }
  if (!stat.isFile() || stat.size > MAX_WORKFLOW_VALUE_CSV_FILE_BYTES) throw new Error(`${ERR_VALIDATION}: Invalid workflow value CSV artifact file: ${filename}`);
  try {
    return fs.readFileSync(filename, "utf8");
  } catch (error) {
    throw new Error(`${ERR_SYSTEM}: Failed to read workflow value CSV artifact file: ${filename}`, { cause: error });
  }
}

/** @param {string} filename */
function json(filename) {
  try {
    return JSON.parse(read(filename));
  } catch (error) {
    throw new Error(`${ERR_PARSE}: Invalid workflow value CSV artifact JSON: ${filename}`, { cause: error });
  }
}

/** @param {Array<Record<string, string | null>>} rows @param {string[]} keys @param {string} table */
function unique(rows, keys, table) {
  const seen = new Set();
  for (const row of rows) {
    const key = keys.map(name => row[name] ?? "").join("\0");
    if (seen.has(key)) throw new Error(`${ERR_VALIDATION}: Duplicate workflow value CSV key in ${table}: ${key}`);
    seen.add(key);
  }
}

/** @param {string | null} value */
function integer(value) {
  return value !== null && /^(?:0|[1-9][0-9]*)$/.test(value) ? Number(value) : null;
}

/** @param {Array<Record<string, string | null>>} rows @param {string} field */
function counts(rows, field) {
  const result = Object.create(null);
  for (const row of rows) {
    const value = String(row[field] ?? "null");
    result[value] = (result[value] || 0) + 1;
  }
  return Object.fromEntries(Object.entries(result).sort(([a], [b]) => a.localeCompare(b)));
}

/** @param {{repoRoot: string, outputDir: string}} options */
function verifyWorkflowValueCsv({ repoRoot, outputDir }) {
  let names;
  try {
    names = fs
      .readdirSync(outputDir, { withFileTypes: true })
      .filter(entry => entry.isFile())
      .map(entry => entry.name)
      .sort();
  } catch (error) {
    throw new Error(`${ERR_SYSTEM}: Failed to list workflow value CSV artifact: ${outputDir}`, { cause: error });
  }
  if (JSON.stringify(names) !== JSON.stringify(EXPECTED_FILES)) {
    throw new Error(`${ERR_VALIDATION}: Workflow value CSV artifact files differ from the schema; expected ${EXPECTED_FILES.join(", ")}; found ${names.join(", ")}`);
  }
  let totalBytes = 0;
  for (const name of names) totalBytes += Buffer.byteLength(read(path.join(outputDir, name)));
  if (totalBytes > MAX_WORKFLOW_VALUE_CSV_TOTAL_BYTES) throw new Error(`${ERR_VALIDATION}: Workflow value CSV artifact exceeds 48 MiB`);

  const schema = json(path.join(outputDir, "schema.json"));
  const manifest = json(path.join(outputDir, "manifest.json"));
  if (JSON.stringify(schema) !== JSON.stringify(WORKFLOW_VALUE_CSV_SCHEMA)) {
    throw new Error(`${ERR_VALIDATION}: Workflow value CSV schema does not match the supported schema`);
  }
  const inventory = loadWorkflowInventory(repoRoot);
  if (
    manifest.schema_version !== 1 ||
    manifest.dataset !== WORKFLOW_VALUE_CSV_SCHEMA.dataset ||
    manifest.inventory?.count !== inventory.count ||
    manifest.inventory?.sha256 !== inventory.sha256 ||
    manifest.limits?.rows_dropped !== 0 ||
    manifest.limits?.maximum_file_bytes !== MAX_WORKFLOW_VALUE_CSV_FILE_BYTES ||
    manifest.limits?.maximum_total_bytes !== MAX_WORKFLOW_VALUE_CSV_TOTAL_BYTES
  ) {
    throw new Error(`${ERR_VALIDATION}: Workflow value CSV manifest does not match the repository inventory or bounded export contract`);
  }

  /** @type {Record<string, Array<Record<string, string | null>>>} */
  const tables = Object.create(null);
  for (const [tableName, tableSchema] of Object.entries(WORKFLOW_VALUE_CSV_SCHEMA.tables)) {
    const filename = path.join(outputDir, tableSchema.path);
    const content = read(filename);
    const listed = manifest.files?.[tableSchema.path];
    if (!listed || listed.bytes !== Buffer.byteLength(content) || listed.sha256 !== sha256(content) || listed.rows !== manifest.row_counts?.[tableName]) {
      throw new Error(`${ERR_VALIDATION}: Workflow value CSV manifest file metadata mismatch: ${tableSchema.path}`);
    }
    const parsed = parseCsv(content);
    const expectedHeader = tableSchema.columns.map(column => column.name);
    if (JSON.stringify(parsed.header) !== JSON.stringify(expectedHeader)) {
      throw new Error(`${ERR_VALIDATION}: Workflow value CSV columns do not match schema: ${tableSchema.path}`);
    }
    if (parsed.rows.length !== manifest.row_counts?.[tableName] || parsed.rows.length !== manifest.expected_row_counts?.[tableName]) {
      throw new Error(`${ERR_VALIDATION}: Workflow value CSV row count mismatch: ${tableSchema.path}`);
    }
    unique(parsed.rows, tableSchema.primary_key, tableName);
    tables[tableName] = parsed.rows;
  }
  const schemaContent = read(path.join(outputDir, "schema.json"));
  if (manifest.files?.["schema.json"]?.bytes !== Buffer.byteLength(schemaContent) || manifest.files?.["schema.json"]?.sha256 !== sha256(schemaContent)) {
    throw new Error(`${ERR_VALIDATION}: Workflow value CSV schema metadata mismatch`);
  }

  const workflowIds = inventory.workflows.map(workflow => workflow.id);
  if (JSON.stringify(tables.workflows.map(row => row.workflow_id)) !== JSON.stringify(workflowIds)) {
    throw new Error(`${ERR_VALIDATION}: Workflow value CSV does not cover the exact ordered paired workflow inventory`);
  }
  if (
    tables.contracts.some(
      row =>
        row.contract_basis !== "recorded_agent_session_evidence_only" ||
        row.excluded_proxy_evidence !== EXCLUDED_PROXY_EVIDENCE ||
        (row.delivery_evidence_status === "unavailable" && row.delivery_evidence_sources !== null) ||
        (row.adoption_evidence_status === "unavailable" && row.adoption_evidence_sources !== null)
    )
  ) {
    throw new Error(`${ERR_VALIDATION}: Workflow value CSV contract is not derived from recorded session evidence or substitutes proxy evidence`);
  }
  const periodDates = tables.periods.map(row => row.period_date);
  if (periodDates.length !== manifest.periods?.count || periodDates[manifest.periods.count - 1] !== manifest.periods.end_date) {
    throw new Error(`${ERR_VALIDATION}: Workflow value CSV period dimension does not match the manifest`);
  }
  const expectedScoped = new Set(periodDates.flatMap(date => workflowIds.flatMap(workflowId => ["selected_window", "retained_population"].map(scope => `${date}\0${workflowId}\0${scope}`))));
  for (const tableName of ["delivery_observations", "adoption_observations"]) {
    const actual = new Set(tables[tableName].map(row => `${row.period_date}\0${row.workflow_id}\0${row.observation_scope}`));
    if (actual.size !== expectedScoped.size || [...expectedScoped].some(key => !actual.has(key))) {
      throw new Error(`${ERR_VALIDATION}: Workflow value CSV ${tableName} does not contain every workflow-period-scope row`);
    }
  }
  const expectedValue = new Set(periodDates.flatMap(date => workflowIds.map(workflowId => `${date}\0${workflowId}`)));
  const actualValue = new Set(tables.attributed_value_observations.map(row => `${row.period_date}\0${row.workflow_id}`));
  if (actualValue.size !== expectedValue.size || [...expectedValue].some(key => !actualValue.has(key))) {
    throw new Error(`${ERR_VALIDATION}: Workflow value CSV attributed value table does not contain every workflow-period row`);
  }

  for (const row of tables.delivery_observations) {
    const delivered = integer(row.delivered);
    if ((row.workflow_applicability === "not_applicable" || row.coverage === "unavailable") !== (row.delivered === null) || (row.delivered !== null && delivered === null)) {
      throw new Error(`${ERR_VALIDATION}: Workflow value CSV delivery nullability is inconsistent`);
    }
  }
  const adoptionFields = ["accepted", "accepted_human", "accepted_delivery_fallback", "rejected", "not_applicable", "pending", "lifecycle", "unknown"];
  for (const row of tables.adoption_observations) {
    const unavailable = row.workflow_applicability === "not_applicable" || row.coverage === "unavailable";
    const values = adoptionFields.map(field => integer(row[field]));
    if (adoptionFields.some((field, index) => (unavailable ? row[field] !== null : values[index] === null))) {
      throw new Error(`${ERR_VALIDATION}: Workflow value CSV adoption nullability is inconsistent`);
    }
    if (!unavailable) {
      const [accepted, acceptedHuman, acceptedFallback] = values;
      if (accepted === null || acceptedHuman === null || acceptedFallback === null || accepted !== acceptedHuman + acceptedFallback) {
        throw new Error(`${ERR_VALIDATION}: Workflow value CSV adoption acceptance total is inconsistent`);
      }
    }
  }
  for (const row of tables.attributed_value_observations) {
    if (row.workflow_applicability === "not_applicable" && (row.state !== "not_applicable" || row.value !== null || row.native_unit !== null)) {
      throw new Error(`${ERR_VALIDATION}: Workflow value CSV pre-introduction value row is not marked not applicable`);
    }
    if (row.state === "measured") {
      if (row.value === null || !Number.isFinite(Number(row.value)) || row.native_unit === null) {
        throw new Error(`${ERR_VALIDATION}: Workflow value CSV measured value lacks a finite native-unit measurement`);
      }
    } else if (row.value !== null) {
      throw new Error(`${ERR_VALIDATION}: Workflow value CSV fabricated an attributed value for an unmeasured state`);
    }
  }
  if (
    JSON.stringify(manifest.coverage?.workflow_git_history_status) !== JSON.stringify(counts(tables.workflows, "git_history_status")) ||
    JSON.stringify(manifest.coverage?.attributed_value_state) !== JSON.stringify(counts(tables.attributed_value_observations, "state")) ||
    JSON.stringify(manifest.coverage?.contract_delivery_evidence) !== JSON.stringify(counts(tables.contracts, "delivery_evidence_status")) ||
    JSON.stringify(manifest.coverage?.contract_adoption_evidence) !== JSON.stringify(counts(tables.contracts, "adoption_evidence_status"))
  ) {
    throw new Error(`${ERR_VALIDATION}: Workflow value CSV coverage summary does not match its rows`);
  }
  return {
    workflow_count: workflowIds.length,
    period_count: periodDates.length,
    row_counts: manifest.row_counts,
    coverage: manifest.coverage,
    bytes: totalBytes,
    git_provenance: manifest.git_provenance,
  };
}

if (require.main === module) {
  const repoRoot = process.env.GH_AW_WORKFLOW_VALUE_REPO_ROOT || process.env.GITHUB_WORKSPACE || path.resolve(__dirname, "../../..");
  const reportDir = process.env.GH_AW_WORKFLOW_VALUE_REPORT_DIR || process.env.GH_AW_WORKFLOW_VALUE_OUTPUT_DIR || path.join(repoRoot, "workflow-value-report");
  const outputDir = process.env.GH_AW_WORKFLOW_VALUE_CSV_DIR || path.join(reportDir, "csv");
  const result = verifyWorkflowValueCsv({ repoRoot, outputDir });
  process.stdout.write(`Workflow value CSV verified: ${result.workflow_count} workflow(s), ${result.period_count} period(s), ${Object.values(result.row_counts).reduce((sum, value) => sum + value, 0)} row(s), ${result.bytes} byte(s).\n`);
}

module.exports = { verifyWorkflowValueCsv };
