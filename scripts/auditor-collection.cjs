const fs = require("node:fs");
const path = require("node:path");
const { execFileSync } = require("node:child_process");

module.exports = async function collect({ github, context, core, env = process.env, now = new Date() }) {
  const cache = env.CACHE_DIR;
  const data = env.DATA_DIR;
  fs.mkdirSync(cache, { recursive: true });
  fs.mkdirSync(data, { recursive: true });
  core.setOutput("has_new_failures", "false");
  const repository = `${context.repo.owner}/${context.repo.repo}`;
  const end = now.toISOString();
  const start = new Date(now.getTime() - 86400000).toISOString();
  const stateFile = path.join(cache, "health-checkpoint.json");
  const state = fs.existsSync(stateFile) ? JSON.parse(fs.readFileSync(stateFile, "utf8")) : { version: 2, repository, seen: [] };
  if (state.version !== 2 || state.repository !== repository || !Array.isArray(state.seen) || state.seen.some(item => typeof item.key !== "string" || !Number.isFinite(Date.parse(item.updated_at)))) {
    throw new Error("Invalid health collection checkpoint; refusing to reset it silently");
  }
  const write = (file, value) => fs.writeFileSync(file, JSON.stringify(value));
  const cli = (args, input) => {
    const report = JSON.parse(
      execFileSync("gh", ["aw", ...args], {
        encoding: "utf8",
        timeout: 150000,
        maxBuffer: 16 * 1024 * 1024,
        input,
      })
    );
    if (report.partial === true || report.complete === false) {
      throw new Error("Incomplete CLI evidence; checkpoint unchanged");
    }
    return report;
  };
  const initialRate = (await github.rest.rateLimit.get()).data.resources.core;
  if (initialRate.remaining < 350) throw new Error("Insufficient API reserve for health collection");
  const health = cli(["health", "--start-date", start, "--end-date", end, "--timeout", "2m", "--logs-dir", path.join(cache, "audits"), "--json"]);
  if (!health.collection || health.collection.partial !== false || health.collection.failure_listing_complete !== true || health.collection.failure_pass_complete !== true) {
    throw new Error("Incomplete health discovery; checkpoint unchanged");
  }
  const openPRs = [];
  let prsComplete = false;
  for (let page = 1; page <= 10; page++) {
    const response = await github.rest.pulls.list({ ...context.repo, state: "open", sort: "created", direction: "asc", per_page: 100, page });
    if (!Array.isArray(response.data)) throw new Error("Invalid PR listing");
    openPRs.push(
      ...response.data.map(pr => ({
        number: pr.number,
        title: pr.title,
        body: pr.body,
        url: pr.html_url,
        head: pr.head.sha,
        base: pr.base.ref,
        draft: pr.draft,
      }))
    );
    if (!/\brel="next"/.test(response.headers.link || "")) {
      prsComplete = true;
      break;
    }
  }
  if (!prsComplete || new Set(openPRs.map(pr => pr.number)).size !== openPRs.length) {
    throw new Error("Incomplete or changing open PR listing; checkpoint unchanged");
  }
  const clusters = (health.failure_clusters || []).filter(cluster => !cluster.intentional_failure);
  if (clusters.length && !Array.isArray(health.failure_investigation?.selected_runs)) {
    throw new Error("Missing health investigation plan; checkpoint unchanged");
  }
  const identity = target => `${target.run_id}-${target.attempt}-${target.updated_at}`;
  const seen = new Map(state.seen.filter(item => Date.parse(item.updated_at) >= Date.parse(start)).map(item => [item.key, item]));
  const targets = (health.failure_investigation?.selected_runs || []).filter(target => target.workflow_path !== ".github/workflows/auditor.lock.yml");
  if (
    targets.length > 5 ||
    new Set(targets.map(target => target.run_id)).size !== targets.length ||
    targets.some(
      target =>
        !Number.isSafeInteger(target.run_id) ||
        target.run_id <= 0 ||
        !/^[1-9]\d*$/.test(target.attempt) ||
        !Number.isFinite(Date.parse(target.updated_at)) ||
        target.repository !== repository ||
        !Array.isArray(target.cluster_ids) ||
        target.cluster_ids.length === 0 ||
        target.cluster_ids.some(id => !clusters.some(cluster => cluster.id === id))
    )
  ) {
    throw new Error("Invalid health investigation targets; checkpoint unchanged");
  }
  const fresh = targets.filter(target => !seen.has(identity(target)));
  const audits = [];
  const verifyAttempt = async target => {
    const run = (await github.rest.actions.getWorkflowRun({ ...context.repo, run_id: target.run_id })).data;
    if (run.id !== target.run_id || String(run.run_attempt) !== target.attempt || run.status !== "completed" || run.conclusion !== "failure" || Date.parse(run.updated_at) !== Date.parse(target.updated_at)) {
      throw new Error("Selected health attempt changed during audit; checkpoint unchanged");
    }
  };
  for (const target of fresh.slice(0, 3)) {
    const rate = (await github.rest.rateLimit.get()).data.resources.core;
    if (rate.reset !== initialRate.reset || rate.remaining < 200) {
      throw new Error("API reserve exhausted or reset; checkpoint unchanged");
    }
    await verifyAttempt(target);
    // One stdin target per call produces an audit, never an accidental multi-run diff.
    const report = cli(["audit", "--stdin", "--repo", repository, "--no-baseline", "--timeout", "2m", "--output", path.join(cache, "audits"), "--json"], `${target.run_id}\n`);
    if (report.overview?.run_id !== target.run_id || report.overview.conclusion !== "failure" || Date.parse(report.overview.updated_at) !== Date.parse(target.updated_at)) {
      throw new Error("Selected health run changed during audit; checkpoint unchanged");
    }
    await verifyAttempt(target);
    const reportFile = `audit-${target.run_id}.json`;
    write(path.join(data, reportFile), report);
    audits.push({ target, report_file: reportFile });
    for (const id of target.cluster_ids) {
      const cluster = clusters.find(item => item.id === id);
      if (!cluster) throw new Error("Health target references a missing cluster");
      cluster.audit_file ||= reportFile;
    }
    seen.set(identity(target), { key: identity(target), updated_at: target.updated_at });
  }
  const finalRate = (await github.rest.rateLimit.get()).data.resources.core;
  if (finalRate.reset !== initialRate.reset || finalRate.remaining < 200) {
    throw new Error("API reserve exhausted or reset; checkpoint unchanged");
  }
  write(path.join(data, "health.json"), health);
  const manifest = {
    complete: true,
    evidence_complete: true,
    repository,
    window_start: start,
    window_end: end,
    health_file: "health.json",
    discovery: "bounded_current_revision_sample",
    new_failures: audits.length,
    remaining_targets: fresh.length - audits.length,
    open_prs_complete: true,
    open_prs: openPRs,
    clusters,
    audits,
    limits: { health_timeout: "2m", health_metadata_requests: 1000, selected_targets: 5, audits: 3, pr_pages: 10 },
  };
  write(path.join(data, "manifest.json"), manifest);
  write(stateFile, { version: 2, repository, checkpoint: end, seen: [...seen.values()] });
  core.setOutput("has_new_failures", audits.length > 0 ? "true" : "false");
  await core.summary.addRaw(`Audited ${audits.length} new health-selected failures; ` + `${manifest.remaining_targets} selected targets remain. Health is a bounded current-revision sample, not an all-run census.`).write();
};
