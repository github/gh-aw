// @ts-check

const { generateXMLMarker } = require("./generate_footer.cjs");

/**
 * Remove reserved execution markers before adding trusted runtime attribution.
 * @param {string} body
 * @param {{stripGroupingMarkers?: boolean}} [options] Strip automation markers from raw agent input, including code examples.
 * @returns {string}
 */
function stripExecutionProvenance(body, { stripGroupingMarkers = false } = {}) {
  const names = stripGroupingMarkers ? "(?:execution|agentic-workflow|workflow-id|workflow-call-id|tracker-id|close-key|comment-type)" : "(?:execution|agentic-workflow)";
  const reservedMarker = new RegExp(`<!--[-!\\s]*gh-aw-${names}\\s*:`, "i");
  const unclosedReservedMarker = new RegExp(`(<!--[-!\\s]*)(gh-aw-)(?=${names}\\s*:)`, "gi");
  let result = "";
  let position = 0;

  while (position < body.length) {
    const start = body.indexOf("<!--", position);
    if (start === -1) {
      result += body.slice(position);
      break;
    }
    result += body.slice(position, start);

    let depth = 0;
    let end = start;
    while (end < body.length) {
      if (body.startsWith("<!--", end)) {
        depth++;
        end += 4;
      } else if (depth > 0 && body.startsWith("--!>", end)) {
        depth--;
        end += 4;
      } else if (depth > 0 && body.startsWith("-->", end)) {
        depth--;
        end += 3;
      } else {
        end++;
      }
      if (depth === 0) break;
    }

    const comment = body.slice(start, end);
    if (depth > 0) {
      result += reservedMarker.test(comment) ? comment.replace(unclosedReservedMarker, "$1gh-aw&#45;") : comment;
      break;
    }
    if (!reservedMarker.test(comment)) result += comment;
    position = end;
  }

  return result.trimEnd();
}

/**
 * Append execution provenance independently of presentation templates.
 * check_run_id identifies the publishing execution, not the logical GITHUB_JOB.
 * @param {string} body
 * @param {string} workflowName
 * @param {string} _runUrl
 * @returns {string}
 */
function appendExecutionProvenance(body, workflowName, _runUrl) {
  const required = name => {
    const value = process.env[name];
    if (!value || value.trim() !== value) throw new Error(`Missing or invalid safe-output execution provenance: ${name}`);
    return value;
  };
  const numeric = name => {
    const value = required(name);
    if (!/^[1-9]\d*$/.test(value)) throw new Error(`Invalid safe-output execution provenance: ${name} must be a positive integer`);
    return value;
  };
  const repository = required("GITHUB_REPOSITORY");
  if (!/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(repository)) {
    throw new Error("Invalid safe-output execution provenance: GITHUB_REPOSITORY must be owner/repo");
  }
  let server;
  try {
    server = new URL(process.env.GITHUB_SERVER_URL || "https://github.com");
  } catch (error) {
    throw new Error("Invalid safe-output execution provenance: GITHUB_SERVER_URL must be an HTTPS origin", { cause: error });
  }
  if (server.protocol !== "https:" || server.username || server.password || server.pathname !== "/" || server.search || server.hash) {
    throw new Error("Invalid safe-output execution provenance: GITHUB_SERVER_URL must be an HTTPS origin");
  }
  const runId = numeric("GITHUB_RUN_ID");
  const runUrl = `${server.origin}/${repository}/actions/runs/${runId}`;
  const metadata = {
    version: 1,
    repo_id: numeric("GITHUB_REPOSITORY_ID"),
    repository,
    aw_workflow_id: required("GH_AW_WORKFLOW_ID"),
    run_id: runId,
    run_attempt: numeric("GITHUB_RUN_ATTEMPT"),
    run_url: runUrl,
    publishing_job: {
      key: required("GITHUB_JOB"),
      check_run_id: numeric("GH_AW_JOB_CHECK_RUN_ID"),
    },
  };
  // Escape HTML delimiters (including "--") without changing the decoded JSON.
  const payload = JSON.stringify(metadata).replace(/[<>&-]/g, character => `\\u${character.charCodeAt(0).toString(16).padStart(4, "0")}`);
  return `${stripExecutionProvenance(body)}\n\n${generateXMLMarker(workflowName, runUrl)}\n<!-- gh-aw-execution: ${payload} -->`;
}

/**
 * Replace reserved attribution structurally before the final document render.
 * @param {import("./report_ast.cjs").Report} report
 * @param {string} workflowName
 * @param {string} runUrl
 * @returns {import("./report_ast.cjs").Report}
 */
function appendReportExecutionProvenance(report, workflowName, runUrl) {
  const { parseReportMarkdown } = require("./report_markdown.cjs");
  /** @param {import("./report_ast.cjs").ReportBlock[]} nodes @returns {import("./report_ast.cjs").ReportBlock[]} */
  const blocks = nodes =>
    nodes
      .filter(node => !((node.type === "xml_marker" || node.type === "json_marker") && ["gh-aw-agentic-workflow", "gh-aw-execution"].includes(node.name)))
      .map(node => {
        if (node.type === "quote" || node.type === "alert" || node.type === "details") return { ...node, children: blocks(node.children) };
        if (node.type === "list") return { ...node, items: node.items.map(blocks) };
        return node;
      });
  const { footer, ...document } = report;
  /** @type {import("./report_ast.cjs").ReportBlock[]} */
  const footerBlocks = [];
  if (footer) {
    footerBlocks.push(...blocks(footer.children));
    if (footer.expiresAt) footerBlocks.push({ type: "quote", agentTainted: false, children: [{ type: "expiration", expiresAt: footer.expiresAt, agentTainted: false }] });
    footerBlocks.push(...footer.markers.filter(marker => !["gh-aw-agentic-workflow", "gh-aw-execution"].includes(marker.name)));
  }
  return {
    ...document,
    children: [...blocks(report.children), ...footerBlocks, ...parseReportMarkdown(appendExecutionProvenance("", workflowName, runUrl), { allowMetadata: true, agentTainted: false })],
    ...(report.preamble ? { preamble: blocks(report.preamble) } : {}),
  };
}

/**
 * Resolve an execution marker to an Actions job without relying on job names.
 * The client must have Actions read access in the execution repository.
 * @param {any} github
 * @param {any} metadata - Parsed version 1 execution marker
 * @returns {Promise<string>} Numeric Actions job ID, distinct from check_run_id
 */
async function resolvePublishingJobId(github, metadata) {
  if (metadata.version !== 1 || !/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(metadata.repository || "")) {
    throw new Error("Invalid safe-output execution provenance for job resolution");
  }
  for (const value of [metadata.run_id, metadata.run_attempt, metadata.publishing_job?.check_run_id]) {
    if (typeof value !== "string" || !/^[1-9]\d*$/.test(value)) {
      throw new Error("Invalid safe-output execution provenance for job resolution");
    }
  }
  const [owner, repo] = metadata.repository.split("/");
  const jobs = await github.paginate(github.rest.actions.listJobsForWorkflowRunAttempt, {
    owner,
    repo,
    run_id: metadata.run_id,
    attempt_number: metadata.run_attempt,
    per_page: 100,
  });
  const apiUrl = (process.env.GITHUB_API_URL || "https://api.github.com").replace(/\/$/, "");
  const checkRunUrl = `${apiUrl}/repos/${metadata.repository}/check-runs/${metadata.publishing_job.check_run_id}`;
  const matches = jobs.filter(job => job.check_run_url === checkRunUrl);
  if (matches.length !== 1 || String(matches[0].run_id) !== metadata.run_id || (matches[0].run_attempt !== undefined && String(matches[0].run_attempt) !== metadata.run_attempt) || !/^[1-9]\d*$/.test(String(matches[0].id))) {
    throw new Error("Cannot uniquely resolve safe-output publishing job in the recorded run attempt");
  }
  return String(matches[0].id);
}

module.exports = { appendExecutionProvenance, appendReportExecutionProvenance, stripExecutionProvenance, resolvePublishingJobId };
