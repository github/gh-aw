// @ts-check

const fs = require("node:fs");
const path = require("node:path");
const { ERR_PARSE, ERR_SYSTEM, ERR_VALIDATION } = require("./error_codes.cjs");
const { loadWorkflowInventory } = require("./workflow_value_report.cjs");

const MAX_FILE_BYTES = 512 * 1024;
const MAX_TOTAL_BYTES = 2 * 1024 * 1024;
const FILES = ["workflow-value-portfolio.json", "workflow-value-history.json", "workflow-value-report.md", "plots/inventory-coverage.svg", "plots/selected-lifecycle.svg", "plots/retained-lifecycle.svg", "plots/value-evidence.svg"];

/** @param {string} filename */
function read(filename) {
  let stat;
  try {
    stat = fs.lstatSync(filename);
  } catch (error) {
    throw new Error(`${ERR_SYSTEM}: Missing workflow value artifact file: ${filename}`, { cause: error });
  }
  if (!stat.isFile() || stat.size > MAX_FILE_BYTES) throw new Error(`${ERR_VALIDATION}: Invalid workflow value artifact file: ${filename}`);
  try {
    return fs.readFileSync(filename, "utf8");
  } catch (error) {
    throw new Error(`${ERR_SYSTEM}: Failed to read workflow value artifact file: ${filename}`, { cause: error });
  }
}

/** @param {string} filename */
function json(filename) {
  try {
    return JSON.parse(read(filename));
  } catch (error) {
    throw new Error(`${ERR_PARSE}: Invalid workflow value artifact JSON: ${filename}`, { cause: error });
  }
}

/** @param {{repoRoot: string, metricsDir: string, outputDir: string}} options */
function verifyWorkflowValueReport({ repoRoot, metricsDir, outputDir }) {
  let totalBytes = 0;
  for (const name of FILES) totalBytes += Buffer.byteLength(read(path.join(outputDir, name)));
  if (totalBytes > MAX_TOTAL_BYTES) throw new Error(`${ERR_VALIDATION}: Workflow value artifact exceeds 2 MiB`);

  const inventory = loadWorkflowInventory(repoRoot);
  const latest = json(path.join(metricsDir, "latest.json"));
  const lifecycle = json(path.join(metricsDir, "lifecycle.json"));
  const portfolio = json(path.join(outputDir, "workflow-value-portfolio.json"));
  const history = json(path.join(outputDir, "workflow-value-history.json"));
  if (
    portfolio.schema_version !== 1 ||
    portfolio.repository !== lifecycle.repository ||
    portfolio.generated_at !== latest.timestamp ||
    portfolio.inventory?.count !== inventory.count ||
    portfolio.inventory?.sha256 !== inventory.sha256 ||
    !Array.isArray(portfolio.workflows) ||
    portfolio.workflows.length !== inventory.count ||
    new Set(portfolio.workflows.map(workflow => workflow.id)).size !== inventory.count ||
    portfolio.history?.path !== "workflow-value-history.json" ||
    portfolio.current?.attributed_value?.measurement_coverage !== "unavailable" ||
    portfolio.current?.attributed_value?.state_counts?.needs_definition !== inventory.count ||
    portfolio.current?.attributed_value?.state_counts?.measured !== 0 ||
    portfolio.sources?.retained_lifecycle?.ledger?.sha256 !== lifecycle.ledger?.sha256
  ) {
    throw new Error(`${ERR_VALIDATION}: Workflow value portfolio does not match current repository evidence`);
  }
  const expectedIds = inventory.workflows.map(workflow => workflow.id);
  if (portfolio.workflows.some((workflow, index) => workflow.id !== expectedIds[index])) {
    throw new Error(`${ERR_VALIDATION}: Workflow value portfolio inventory is incomplete or out of order`);
  }
  if (
    history.schema_version !== 1 ||
    history.repository !== portfolio.repository ||
    history.generated_at !== portfolio.generated_at ||
    history.inventory?.sha256 !== inventory.sha256 ||
    !Array.isArray(history.periods) ||
    history.periods.length !== portfolio.history.period_count ||
    history.periods[history.current_period_index]?.date !== portfolio.current.date
  ) {
    throw new Error(`${ERR_VALIDATION}: Workflow value history does not align with the current portfolio`);
  }
  const report = read(path.join(outputDir, "workflow-value-report.md"));
  if (!report.includes(`**${inventory.count}** paired top-level workflow`) || !report.includes("Attributed value is not measured")) {
    throw new Error(`${ERR_VALIDATION}: Workflow value Markdown report is incomplete`);
  }
  for (const name of FILES.filter(name => name.endsWith(".svg"))) {
    if (!read(path.join(outputDir, name)).startsWith("<svg ")) throw new Error(`${ERR_VALIDATION}: Workflow value plot is not valid SVG: ${name}`);
  }
  return { workflow_count: inventory.count, period_count: history.periods.length, bytes: totalBytes };
}

if (require.main === module) {
  const repoRoot = process.env.GITHUB_WORKSPACE || path.resolve(__dirname, "../../..");
  const result = verifyWorkflowValueReport({
    repoRoot,
    metricsDir: process.env.GH_AW_WORKFLOW_VALUE_METRICS_DIR || "/tmp/gh-aw/repo-memory/default/metrics",
    outputDir: process.env.GH_AW_WORKFLOW_VALUE_OUTPUT_DIR || "/tmp/gh-aw/workflow-value-report",
  });
  process.stdout.write(`Workflow value report verified: ${result.workflow_count} workflow(s), ${result.period_count} period(s), ${result.bytes} byte(s).\n`);
}

module.exports = { verifyWorkflowValueReport };
