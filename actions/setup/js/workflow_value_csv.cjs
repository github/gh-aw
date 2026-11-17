// @ts-check

const fs = require("node:fs");
const path = require("node:path");
const { ERR_PARSE, ERR_SYSTEM, ERR_VALIDATION } = require("./error_codes.cjs");
const { loadWorkflowInventory } = require("./workflow_value_report.cjs");
const { collectGitProvenance, pairGitProvenance } = require("./workflow_value_csv_git.cjs");
const { MAX_WORKFLOW_VALUE_CSV_FILE_BYTES, MAX_WORKFLOW_VALUE_CSV_TOTAL_BYTES, WORKFLOW_VALUE_CSV_SCHEMA, serializeCsv, sha256, validateWorkflowValueCsvFiles } = require("./workflow_value_csv_format.cjs");

const MAX_REPORT_INPUT_BYTES = 512 * 1024;
const CONTRACT_VERSION = "gh-aw-workflow-value-contract-v1";
const EXCLUDED_PROXY_EVIDENCE = "legacy_safe_output_outcomes;run_success;labels;output_counts;merge_rates;grader_scores;agent_claims";

/** @param {string} filename */
function readBounded(filename) {
  let stat;
  try {
    stat = fs.lstatSync(filename);
  } catch (error) {
    throw new Error(`${ERR_SYSTEM}: Missing workflow value CSV input: ${filename}`, { cause: error });
  }
  if (!stat.isFile() || stat.size > MAX_REPORT_INPUT_BYTES) throw new Error(`${ERR_VALIDATION}: Invalid workflow value CSV input: ${filename}`);
  try {
    return fs.readFileSync(filename);
  } catch (error) {
    throw new Error(`${ERR_SYSTEM}: Failed to read workflow value CSV input: ${filename}`, { cause: error });
  }
}

/** @param {string} filename */
function readJson(filename) {
  const content = readBounded(filename);
  try {
    return { content, value: JSON.parse(content.toString("utf8")) };
  } catch (error) {
    throw new Error(`${ERR_PARSE}: Invalid workflow value CSV input JSON: ${filename}`, { cause: error });
  }
}

/** @param {string} filename @param {string} content */
function writeAtomic(filename, content) {
  try {
    fs.mkdirSync(path.dirname(filename), { recursive: true });
    fs.writeFileSync(`${filename}.tmp`, content, { mode: 0o600 });
    fs.renameSync(`${filename}.tmp`, filename);
  } catch (error) {
    throw new Error(`${ERR_SYSTEM}: Failed to write workflow value CSV output: ${filename}`, { cause: error });
  }
}

/** @param {string} source */
function frontmatter(source) {
  if (!source.startsWith("---\n")) throw new Error(`${ERR_VALIDATION}: Agentic workflow source must start with frontmatter`);
  const end = source.indexOf("\n---", 4);
  if (end < 0) throw new Error(`${ERR_VALIDATION}: Agentic workflow source has unterminated frontmatter`);
  return source.slice(4, end);
}

/** @param {string} value */
function scalar(value) {
  const trimmed = value.trim();
  if (!trimmed) return null;
  if (trimmed.startsWith('"') && trimmed.endsWith('"')) {
    try {
      return JSON.parse(trimmed);
    } catch {
      return trimmed.slice(1, -1);
    }
  }
  if (trimmed.startsWith("'") && trimmed.endsWith("'")) return trimmed.slice(1, -1).replaceAll("''", "'");
  return trimmed.replace(/\s+#.*$/, "").trim() || null;
}

/** @param {string} text @param {string} key */
function topLevelScalar(text, key) {
  const lines = text.split("\n");
  for (let index = 0; index < lines.length; index++) {
    const match = new RegExp(`^${key.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}:\\s*(.*)$`).exec(lines[index]);
    if (!match) continue;
    const raw = match[1].trim();
    if (raw !== "|" && raw !== ">") return scalar(raw);
    const content = [];
    for (let child = index + 1; child < lines.length; child++) {
      if (/^\S/.test(lines[child])) break;
      content.push(lines[child].replace(/^\s{1,2}/, ""));
    }
    const rendered =
      raw === ">"
        ? content
            .map(line => line.trim())
            .join(" ")
            .trim()
        : content.join("\n").trim();
    return rendered || null;
  }
  return null;
}

/** @param {Record<string, any>} encoded @param {string} workflowId */
function observationFor(encoded, workflowId) {
  if (!encoded || typeof encoded !== "object" || Array.isArray(encoded)) return null;
  return encoded.overrides?.[workflowId] ?? encoded.default ?? null;
}

/** @param {Record<string, any>} period @param {string} workflowId */
function workflowInPeriod(period, workflowId) {
  const membership = period.inventory?.membership;
  if (!membership || !["current", "present", "missing_current", "none"].includes(membership.mode)) return false;
  if (membership.mode === "current") return true;
  if (membership.mode === "none") return false;
  const ids = Array.isArray(membership.workflow_ids) ? membership.workflow_ids : [];
  return membership.mode === "present" ? ids.includes(workflowId) : !ids.includes(workflowId);
}

/** @param {Record<string, any>} period @param {string | null} introducedAt */
function periodPrecedesIntroduction(period, introducedAt) {
  if (!introducedAt || !Number.isFinite(Date.parse(introducedAt))) return false;
  if (typeof period.timestamp === "string" && Number.isFinite(Date.parse(period.timestamp))) return Date.parse(period.timestamp) < Date.parse(introducedAt);
  return typeof period.date === "string" && period.date < introducedAt.slice(0, 10);
}

/** @param {Record<string, any>} period @param {string} workflowId @param {string | null} introducedAt */
function applicability(period, workflowId, introducedAt) {
  if (periodPrecedesIntroduction(period, introducedAt)) {
    return { value: "not_applicable", reason_code: "workflow_not_introduced" };
  }
  if (!workflowInPeriod(period, workflowId)) {
    return { value: "not_applicable", reason_code: "workflow_not_in_period_inventory" };
  }
  return { value: "applicable", reason_code: null };
}

/** @param {Array<Record<string, any>>} rows @param {string} field */
function counts(rows, field) {
  const result = Object.create(null);
  for (const row of rows) {
    const value = String(row[field] ?? "null");
    result[value] = (result[value] || 0) + 1;
  }
  return Object.fromEntries(Object.entries(result).sort(([a], [b]) => a.localeCompare(b)));
}

/** @param {Array<Record<string, any>>} rows */
function scopedCoverage(rows) {
  const result = Object.create(null);
  for (const row of rows) {
    const key = `${row.observation_scope}:${row.coverage}`;
    result[key] = (result[key] || 0) + 1;
  }
  return Object.fromEntries(Object.entries(result).sort(([a], [b]) => a.localeCompare(b)));
}

/**
 * @param {{
 *   repoRoot: string,
 *   reportDir: string,
 *   outputDir: string,
 *   provenanceRepoRoot?: string,
 *   provenanceRevision?: string,
 * }} options
 */
function generateWorkflowValueCsv({ repoRoot, reportDir, outputDir, provenanceRepoRoot = repoRoot, provenanceRevision = "HEAD" }) {
  const portfolioInput = readJson(path.join(reportDir, "workflow-value-portfolio.json"));
  const historyInput = readJson(path.join(reportDir, "workflow-value-history.json"));
  const portfolio = portfolioInput.value;
  const history = historyInput.value;
  const inventory = loadWorkflowInventory(repoRoot);
  if (
    portfolio?.schema_version !== 1 ||
    history?.schema_version !== 1 ||
    portfolio.repository !== history.repository ||
    portfolio.generated_at !== history.generated_at ||
    portfolio.inventory?.count !== inventory.count ||
    portfolio.inventory?.sha256 !== inventory.sha256 ||
    history.inventory?.sha256 !== inventory.sha256 ||
    !Array.isArray(portfolio.workflows) ||
    portfolio.workflows.length !== inventory.count ||
    !Array.isArray(history.periods) ||
    !Number.isSafeInteger(history.current_period_index) ||
    !history.periods[history.current_period_index]
  ) {
    throw new Error(`${ERR_VALIDATION}: Workflow value CSV inputs do not describe the current paired workflow inventory`);
  }
  const inventoryIds = inventory.workflows.map(workflow => workflow.id);
  if (portfolio.workflows.some((workflow, index) => workflow.id !== inventoryIds[index])) {
    throw new Error(`${ERR_VALIDATION}: Workflow value CSV portfolio inventory is incomplete or out of order`);
  }
  const dates = history.periods.map(period => period.date);
  if (dates.some((date, index) => typeof date !== "string" || (index > 0 && date <= dates[index - 1]))) {
    throw new Error(`${ERR_VALIDATION}: Workflow value CSV history periods must be uniquely sorted`);
  }
  const overrideIds = history.periods.flatMap(period => [
    ...Object.keys(period.selected_window?.delivery?.overrides || {}),
    ...Object.keys(period.selected_window?.adoption?.overrides || {}),
    ...Object.keys(period.retained_population?.observations?.overrides || {}),
    ...Object.keys(period.attributed_value?.overrides || {}),
  ]);
  const unexpectedOverrides = [...new Set(overrideIds.filter(id => !inventoryIds.includes(id)))].sort();
  if (unexpectedOverrides.length) throw new Error(`${ERR_VALIDATION}: Workflow value CSV history contains unknown workflow overrides: ${unexpectedOverrides.join(", ")}`);

  const gitProvenance = collectGitProvenance({
    repoRoot: provenanceRepoRoot,
    revision: provenanceRevision,
    paths: inventory.workflows.flatMap(workflow => [workflow.source_path, workflow.lock_path]),
  });
  const portfolioById = new Map(portfolio.workflows.map(workflow => [workflow.id, workflow]));
  /** @type {Array<Record<string, any>>} */
  const workflowRows = [];
  for (const workflow of inventory.workflows) {
    const reported = portfolioById.get(workflow.id);
    if (!reported || reported.source_path !== workflow.source_path || reported.lock_path !== workflow.lock_path || reported.source_sha256 !== workflow.source_sha256 || reported.lock_sha256 !== workflow.lock_sha256) {
      throw new Error(`${ERR_VALIDATION}: Workflow value CSV inventory digest mismatch: ${workflow.id}`);
    }
    const source = readBounded(path.join(repoRoot, workflow.source_path)).toString("utf8");
    const metadata = frontmatter(source);
    const sourceGit = gitProvenance.files[workflow.source_path];
    const lockGit = gitProvenance.files[workflow.lock_path];
    const pairGit = pairGitProvenance(sourceGit, lockGit);
    workflowRows.push({
      workflow_id: workflow.id,
      name: topLevelScalar(metadata, "name") || workflow.id,
      description: topLevelScalar(metadata, "description"),
      source_reference: topLevelScalar(metadata, "source"),
      source_path: workflow.source_path,
      lock_path: workflow.lock_path,
      source_sha256: workflow.source_sha256,
      lock_sha256: workflow.lock_sha256,
      frontmatter_sha256: sha256(metadata),
      operational_value_state: workflow.operational_value.state,
      operational_value_scope: workflow.operational_value.scope,
      git_history_status: pairGit.history_status,
      source_introduced_commit: sourceGit.introduced_commit,
      source_introduced_at: sourceGit.introduced_at,
      source_revised_commit: sourceGit.revised_commit,
      source_revised_at: sourceGit.revised_at,
      lock_introduced_commit: lockGit.introduced_commit,
      lock_introduced_at: lockGit.introduced_at,
      lock_revised_commit: lockGit.revised_commit,
      lock_revised_at: lockGit.revised_at,
      workflow_introduced_commit: pairGit.introduced_commit,
      workflow_introduced_at: pairGit.introduced_at,
      workflow_revised_commit: pairGit.revised_commit,
      workflow_revised_at: pairGit.revised_at,
    });
  }

  const introducedById = new Map(workflowRows.map(workflow => [workflow.workflow_id, workflow.workflow_introduced_at]));
  /** @type {Array<Record<string, any>>} */
  const periodRows = [];
  /** @type {Array<Record<string, any>>} */
  const deliveryRows = [];
  /** @type {Array<Record<string, any>>} */
  const adoptionRows = [];
  /** @type {Array<Record<string, any>>} */
  const valueRows = [];
  for (const [periodIndex, period] of history.periods.entries()) {
    const periodRole = periodIndex === history.current_period_index ? "current" : "retained_history";
    const selectedWindow = period.selected_window?.window || null;
    periodRows.push({
      period_date: period.date,
      period_role: periodRole,
      timestamp: period.timestamp ?? null,
      collection_status: period.collection_status || "unavailable",
      comparability: period.comparability || "unavailable",
      session_evidence_source: period.session_evidence?.source ?? null,
      session_reported_workflow_count: period.session_evidence?.reported_workflows ?? 0,
      session_delivery_workflow_count: period.session_evidence?.delivery_workflows ?? 0,
      session_adoption_workflow_count: period.session_evidence?.adoption_workflows ?? 0,
      inventory_coverage: period.inventory?.coverage || "unavailable",
      inventory_membership_mode: period.inventory?.membership?.mode || "none",
      reported_workflow_count: period.inventory?.reported_count ?? 0,
      current_reported_workflow_count: period.inventory?.current_reported_count ?? 0,
      unknown_workflow_count: Array.isArray(period.inventory?.unknown_workflow_ids) ? period.inventory.unknown_workflow_ids.length : 0,
      metrics_source: period.metrics_source ?? null,
      lifecycle_source: period.lifecycle_source ?? null,
      selected_window_source: period.selected_window_source || "recorded_daily_metrics",
      selected_window_start: selectedWindow?.start ?? null,
      selected_window_end: selectedWindow?.end ?? null,
    });
    for (const workflowId of inventoryIds) {
      const applies = applicability(period, workflowId, introducedById.get(workflowId) || null);
      const scopes = [
        {
          scope: "selected_window",
          delivery: observationFor(period.selected_window?.delivery, workflowId),
          adoption: observationFor(period.selected_window?.adoption, workflowId),
          sourceKind: period.selected_window_source === "canonical_retained_lifecycle_ledger" ? "canonical_lifecycle_ledger" : period.session_evidence?.source ? "agent_session_metrics" : "daily_metrics_snapshot",
          sourcePath: period.selected_window_source === "canonical_retained_lifecycle_ledger" ? (portfolio.sources?.retained_lifecycle?.ledger?.path ?? null) : (period.metrics_source ?? null),
        },
        {
          scope: "retained_population",
          delivery: observationFor(period.retained_population?.observations, workflowId),
          adoption: observationFor(period.retained_population?.observations, workflowId),
          sourceKind: "retained_lifecycle_snapshot",
          sourcePath: period.lifecycle_source ?? null,
        },
      ];
      for (const item of scopes) {
        const unavailable = applies.value === "not_applicable";
        const delivery = unavailable ? null : item.delivery;
        const adoption = unavailable ? null : item.adoption;
        deliveryRows.push({
          period_date: period.date,
          period_role: periodRole,
          workflow_id: workflowId,
          observation_scope: item.scope,
          workflow_applicability: applies.value,
          coverage: unavailable ? "unavailable" : delivery?.coverage || "unavailable",
          delivered: unavailable || delivery?.coverage === "unavailable" ? null : (delivery?.delivered ?? null),
          native_unit: "delivered_effects",
          reason_code: unavailable ? applies.reason_code : (delivery?.reason_code ?? null),
          source_kind: item.sourceKind,
          source_path: item.sourcePath,
        });
        adoptionRows.push({
          period_date: period.date,
          period_role: periodRole,
          workflow_id: workflowId,
          observation_scope: item.scope,
          workflow_applicability: applies.value,
          coverage: unavailable ? "unavailable" : adoption?.coverage || "unavailable",
          accepted: unavailable || adoption?.coverage === "unavailable" ? null : (adoption?.accepted ?? null),
          accepted_human: unavailable || adoption?.coverage === "unavailable" ? null : (adoption?.accepted_human ?? null),
          accepted_delivery_fallback: unavailable || adoption?.coverage === "unavailable" ? null : (adoption?.accepted_delivery_fallback ?? null),
          rejected: unavailable || adoption?.coverage === "unavailable" ? null : (adoption?.rejected ?? null),
          not_applicable: unavailable || adoption?.coverage === "unavailable" ? null : (adoption?.not_applicable ?? null),
          pending: unavailable || adoption?.coverage === "unavailable" ? null : (adoption?.pending ?? null),
          lifecycle: unavailable || adoption?.coverage === "unavailable" ? null : (adoption?.lifecycle ?? null),
          unknown: unavailable || adoption?.coverage === "unavailable" ? null : (adoption?.unknown ?? null),
          native_unit: "delivery_identities",
          reason_code: unavailable ? applies.reason_code : (adoption?.reason_code ?? null),
          source_kind: item.sourceKind,
          source_path: item.sourcePath,
        });
      }
      const value = applies.value === "not_applicable" ? null : observationFor(period.attributed_value, workflowId);
      valueRows.push({
        period_date: period.date,
        period_role: periodRole,
        workflow_id: workflowId,
        workflow_applicability: applies.value,
        status_coverage: applies.value === "not_applicable" ? "unavailable" : period.attributed_value?.status_coverage || "unavailable",
        measurement_coverage: applies.value === "not_applicable" ? "unavailable" : period.attributed_value?.measurement_coverage || "unavailable",
        state: applies.value === "not_applicable" ? "not_applicable" : value?.state || "unavailable",
        value: applies.value === "not_applicable" ? null : (value?.value ?? null),
        native_unit: applies.value === "not_applicable" ? null : (value?.unit ?? null),
        reason_code: applies.value === "not_applicable" ? applies.reason_code : (value?.reason_code ?? "value_evidence_unavailable"),
        definition_source: value?.definition_source ?? portfolio.sources?.attributed_value?.definition_source ?? null,
        attribution_evidence_status: value?.attribution_evidence_status ?? "unavailable",
        survival_window_status: value?.survival_window_status ?? "unavailable",
      });
    }
  }

  const workflowRowById = new Map(workflowRows.map(workflow => [workflow.workflow_id, workflow]));
  const periodRowByDate = new Map(periodRows.map(period => [period.period_date, period]));
  /** @type {Array<Record<string, any>>} */
  const contractRows = [];
  for (const workflowId of inventoryIds) {
    const workflow = workflowRowById.get(workflowId);
    const workflowDelivery = deliveryRows.filter(row => row.workflow_id === workflowId && row.workflow_applicability === "applicable");
    const workflowAdoption = adoptionRows.filter(row => row.workflow_id === workflowId && row.workflow_applicability === "applicable");
    const availableDelivery = workflowDelivery.filter(row => row.coverage !== "unavailable");
    const availableAdoption = workflowAdoption.filter(row => row.coverage !== "unavailable");
    const applicablePeriodDates = new Set(workflowDelivery.filter(row => row.observation_scope === "selected_window").map(row => row.period_date));
    const sessionSources = [...new Set([...applicablePeriodDates].map(date => periodRowByDate.get(date)?.session_evidence_source).filter(source => typeof source === "string" && source))].sort();
    const deliverySources = [...new Set(availableDelivery.map(row => row.source_kind))].sort();
    const adoptionSources = [...new Set(availableAdoption.map(row => row.source_kind))].sort();
    const currentValue = valueRows.find(row => row.workflow_id === workflowId && row.period_role === "current");
    const contract = {
      workflow_id: workflowId,
      contract_version: CONTRACT_VERSION,
      contract_basis: "recorded_agent_session_evidence_only",
      session_evidence_source: sessionSources.length === 1 ? sessionSources[0] : sessionSources.length > 1 ? "mixed" : null,
      delivery_evidence_status: availableDelivery.length ? "available" : "unavailable",
      delivery_evidence_sources: deliverySources.length ? deliverySources.join(";") : null,
      delivery_native_unit: "delivered_effects",
      adoption_evidence_status: availableAdoption.length ? "available" : "unavailable",
      adoption_evidence_sources: adoptionSources.length ? adoptionSources.join(";") : null,
      adoption_native_unit: "delivery_identities",
      attributed_value_definition_state: currentValue?.state || "unavailable",
      attributed_value_native_unit: currentValue?.native_unit ?? null,
      attribution_evidence_status: currentValue?.attribution_evidence_status || "unavailable",
      survival_window_status: currentValue?.survival_window_status || "unavailable",
      operational_value_declaration_state: workflow?.operational_value_state || "not_declared",
      operational_value_scope: workflow?.operational_value_scope || "run_scoped_optional",
      excluded_proxy_evidence: EXCLUDED_PROXY_EVIDENCE,
    };
    contractRows.push({
      workflow_id: contract.workflow_id,
      contract_version: contract.contract_version,
      contract_sha256: sha256(JSON.stringify(contract)),
      contract_basis: contract.contract_basis,
      session_evidence_source: contract.session_evidence_source,
      delivery_evidence_status: contract.delivery_evidence_status,
      delivery_evidence_sources: contract.delivery_evidence_sources,
      delivery_native_unit: contract.delivery_native_unit,
      adoption_evidence_status: contract.adoption_evidence_status,
      adoption_evidence_sources: contract.adoption_evidence_sources,
      adoption_native_unit: contract.adoption_native_unit,
      attributed_value_definition_state: contract.attributed_value_definition_state,
      attributed_value_native_unit: contract.attributed_value_native_unit,
      attribution_evidence_status: contract.attribution_evidence_status,
      survival_window_status: contract.survival_window_status,
      operational_value_declaration_state: contract.operational_value_declaration_state,
      operational_value_scope: contract.operational_value_scope,
      excluded_proxy_evidence: contract.excluded_proxy_evidence,
    });
  }

  const tables = {
    workflows: workflowRows,
    contracts: contractRows,
    periods: periodRows,
    delivery_observations: deliveryRows,
    adoption_observations: adoptionRows,
    attributed_value_observations: valueRows,
  };
  const files = new Map();
  const schemaContent = `${JSON.stringify(WORKFLOW_VALUE_CSV_SCHEMA, null, 2)}\n`;
  files.set("schema.json", schemaContent);
  for (const [tableName, rows] of Object.entries(tables)) {
    const tableSchema = WORKFLOW_VALUE_CSV_SCHEMA.tables[tableName];
    files.set(tableSchema.path, serializeCsv(rows, tableSchema.columns));
  }
  const payloadBytes = validateWorkflowValueCsvFiles(files);
  const fileManifest = Object.fromEntries(
    [...files.entries()].map(([name, content]) => [
      name,
      {
        bytes: Buffer.byteLength(content),
        sha256: sha256(content),
        rows: name.endsWith(".csv") ? tables[Object.keys(WORKFLOW_VALUE_CSV_SCHEMA.tables).find(table => WORKFLOW_VALUE_CSV_SCHEMA.tables[table].path === name)].length : null,
      },
    ])
  );
  const manifest = {
    schema_version: 1,
    dataset: WORKFLOW_VALUE_CSV_SCHEMA.dataset,
    repository: portfolio.repository,
    generated_at: portfolio.generated_at,
    source_revision: portfolio.source_revision,
    inventory: portfolio.inventory,
    periods: {
      count: history.periods.length,
      current_date: history.periods[history.current_period_index].date,
      start_date: history.periods[0]?.date ?? null,
      end_date: history.periods.at(-1)?.date ?? null,
    },
    source_inputs: {
      "workflow-value-portfolio.json": { bytes: portfolioInput.content.length, sha256: sha256(portfolioInput.content) },
      "workflow-value-history.json": { bytes: historyInput.content.length, sha256: sha256(historyInput.content) },
    },
    git_provenance: {
      status: gitProvenance.status,
      revision: gitProvenance.revision,
      shallow: gitProvenance.shallow,
      error: gitProvenance.error,
    },
    row_counts: Object.fromEntries(Object.entries(tables).map(([name, rows]) => [name, rows.length])),
    expected_row_counts: {
      workflows: inventory.count,
      contracts: inventory.count,
      periods: history.periods.length,
      delivery_observations: inventory.count * history.periods.length * 2,
      adoption_observations: inventory.count * history.periods.length * 2,
      attributed_value_observations: inventory.count * history.periods.length,
    },
    coverage: {
      workflow_git_history_status: counts(workflowRows, "git_history_status"),
      period_inventory: counts(periodRows, "inventory_coverage"),
      period_session_evidence_source: counts(periodRows, "session_evidence_source"),
      contract_delivery_evidence: counts(contractRows, "delivery_evidence_status"),
      contract_adoption_evidence: counts(contractRows, "adoption_evidence_status"),
      delivery: scopedCoverage(deliveryRows),
      adoption: scopedCoverage(adoptionRows),
      workflow_applicability: counts(deliveryRows, "workflow_applicability"),
      attributed_value_state: counts(valueRows, "state"),
      attributed_value_measurement: counts(valueRows, "measurement_coverage"),
    },
    evidence_boundaries: {
      excluded_proxy_evidence: EXCLUDED_PROXY_EVIDENCE.split(";"),
      contract_basis: "recorded_agent_session_evidence_only",
      missing_lifecycle_evidence: "null_with_unavailable_coverage",
      missing_value_evidence: "null_with_unavailable_coverage",
      historical_scope: "agent_session_metrics_reconstructed_from_current_portfolio_and_retained_history_default_override_encoding",
      pre_introduction_periods: "not_applicable",
      native_units: "kept_separate_by_table_and_workflow_contract",
    },
    limits: {
      maximum_file_bytes: MAX_WORKFLOW_VALUE_CSV_FILE_BYTES,
      maximum_total_bytes: MAX_WORKFLOW_VALUE_CSV_TOTAL_BYTES,
      payload_bytes: payloadBytes,
      rows_dropped: 0,
    },
    files: fileManifest,
  };
  const manifestContent = `${JSON.stringify(manifest, null, 2)}\n`;
  files.set("manifest.json", manifestContent);
  const totalBytes = validateWorkflowValueCsvFiles(files);
  for (const [name, content] of files) writeAtomic(path.join(outputDir, name), content);
  return { manifest, tables, output_bytes: totalBytes };
}

if (require.main === module) {
  const repoRoot = process.env.GH_AW_WORKFLOW_VALUE_REPO_ROOT || process.env.GITHUB_WORKSPACE || path.resolve(__dirname, "../../..");
  const reportDir = process.env.GH_AW_WORKFLOW_VALUE_REPORT_DIR || process.env.GH_AW_WORKFLOW_VALUE_OUTPUT_DIR || path.join(repoRoot, "workflow-value-report");
  const outputDir = process.env.GH_AW_WORKFLOW_VALUE_CSV_DIR || path.join(reportDir, "csv");
  const result = generateWorkflowValueCsv({
    repoRoot,
    reportDir,
    outputDir,
    provenanceRepoRoot: process.env.GH_AW_WORKFLOW_VALUE_PROVENANCE_REPO || repoRoot,
    provenanceRevision: process.env.GH_AW_WORKFLOW_VALUE_PROVENANCE_REVISION || "HEAD",
  });
  process.stdout.write(
    `Workflow value CSV generated for ${result.manifest.inventory.count} workflow(s), ${result.manifest.periods.count} retained period(s), ${result.output_bytes} byte(s), git history ${result.manifest.git_provenance.status}.\n`
  );
}

module.exports = {
  CONTRACT_VERSION,
  EXCLUDED_PROXY_EVIDENCE,
  applicability,
  generateWorkflowValueCsv,
  observationFor,
  periodPrecedesIntroduction,
  topLevelScalar,
  workflowInPeriod,
};
