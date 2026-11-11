---
name: Auditor
description: Hourly, budgeted collection of newly completed agentic failures and one deduplicated root-cause fix.
intent: Reduce recurring agentic workflow failures without duplicating work already under review.
on:
  schedule: hourly
  workflow_dispatch:
    inputs:
      recovery_cache_key:
        description: Exact restored auditor-collection cache key from the failed collection log
        type: string
        default: ""
      recovery_checkpoint:
        description: Exact checkpoint timestamp from that cache's checkpoint.json
        type: string
        default: ""
      acknowledge_gap:
        description: Accept uncollected history from that checkpoint until this recovery starts (no backfill)
        type: boolean
        default: false
permissions:
  contents: read
  actions: read
  pull-requests: read
  copilot-requests: write
concurrency:
  group: auditor-${{ github.repository }}
  cancel-in-progress: false
timeout-minutes: 40
network:
  allowed: [defaults, go, node]
tools:
  bash: ["*"]
  edit:
  github:
    mode: gh-proxy
    toolsets: [pull_requests]
safe-outputs:
  create-pull-request:
    title-prefix: "[auditor] "
    draft: true
    max: 1
    max-patch-files: 30
    allowed-files:
      - "pkg/**"
      - "cmd/**"
      - "internal/**"
      - "actions/**"
      - "scripts/**"
      - "schemas/**"
      - ".github/workflows/**/*.md"
    excluded-files:
      - "**/*.lock.yml"
    github-token-for-extra-empty-commit: none
    protected-files: request-review
if: github.event_name == 'schedule' && needs.collect.outputs.has_new_failures == 'true'
jobs:
  collect:
    name: Collect failure evidence
    if: github.event_name == 'schedule' || (github.event_name == 'workflow_dispatch' && github.ref_name == github.event.repository.default_branch && inputs.acknowledge_gap == true)
    runs-on: ubuntu-latest
    timeout-minutes: 20
    permissions:
      contents: read
      actions: read
      pull-requests: read
    outputs:
      has_new_failures: ${{ steps.collect.outputs.has_new_failures }}
    steps:
      - name: Checkout trusted workflow source
        uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
      - name: Install gh-aw CLI
        uses: ./actions/setup-cli
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
      - name: Restore collection checkpoint and query cache
        id: restore_checkpoint
        uses: actions/cache/restore@55cc8345863c7cc4c66a329aec7e433d2d1c52a9 # v6.1.0
        with:
          path: ${{ runner.temp }}/auditor-cache
          key: auditor-collection-v1-${{ github.repository_id }}-${{ github.ref_name }}-${{ github.run_id }}-${{ github.run_attempt }}
          restore-keys: |
            auditor-collection-v1-${{ github.repository_id }}-${{ github.ref_name }}-
      - name: Collect health failures, cached logs and bounded audits
        id: collect
        uses: actions/github-script@3a2844b7e9c422d3c10d287c895573f7108da1b3 # v9.0.0
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          CACHE_DIR: ${{ runner.temp }}/auditor-cache
          DATA_DIR: ${{ runner.temp }}/auditor-data
          RECOVERY_REQUESTED: ${{ github.event_name == 'workflow_dispatch' }}
          ACKNOWLEDGE_GAP: ${{ inputs.acknowledge_gap }}
          RECOVERY_CHECKPOINT: ${{ inputs.recovery_checkpoint }}
          RECOVERY_CACHE_KEY: ${{ inputs.recovery_cache_key }}
          RESTORED_CACHE_KEY: ${{ steps.restore_checkpoint.outputs.cache-matched-key }}
        with:
          retries: 0
          script: |
            const fs = require('node:fs');
            const path = require('node:path');
            const crypto = require('node:crypto');
            const {execFileSync} = require('node:child_process');
            const cache = process.env.CACHE_DIR, data = process.env.DATA_DIR;
            fs.mkdirSync(cache, {recursive: true});
            fs.mkdirSync(data, {recursive: true});
            const read = file => JSON.parse(fs.readFileSync(file, 'utf8'));
            const write = (file, value) => fs.writeFileSync(file, JSON.stringify(value));
            const stateFile = path.join(cache, 'checkpoint.json');
            const end = new Date().toISOString();
            const bootstrap = !fs.existsSync(stateFile);
            const repository = context.repo.owner + '/' + context.repo.repo;
            core.setOutput('has_new_failures', 'false');
            let requests = 0;
            const request = async (method, params) => {
              if (++requests > 1025) throw new Error('Metadata request budget exceeded');
              return method({...context.repo, ...params});
            };
            if (bootstrap) {
              const attempt = Number(process.env.GITHUB_RUN_ATTEMPT);
              const prior = await request(github.rest.actions.listWorkflowRuns, {
                workflow_id: 'auditor.lock.yml', per_page: 2
              });
              if (!Array.isArray(prior.data.workflow_runs) ||
                  !Number.isSafeInteger(prior.data.total_count) || prior.data.total_count < 0 ||
                  prior.data.total_count < prior.data.workflow_runs.length ||
                  (prior.data.total_count > 0) !== (prior.data.workflow_runs.length > 0) ||
                  !Number.isSafeInteger(context.runId) || !Number.isSafeInteger(attempt) || attempt < 1 ||
                  prior.data.workflow_runs.some(run => !Number.isSafeInteger(run.id))) {
                throw new Error('Invalid prior collection history; refusing to initialize checkpoint');
              }
              if (attempt > 1 || prior.data.total_count > prior.data.workflow_runs.length ||
                  prior.data.workflow_runs.some(run => run.id !== context.runId)) {
                throw new Error('Collection checkpoint cache is missing with prior Auditor execution history; ' +
                  'restore the previous checkpoint and recovery files before resuming; history is not reset');
              }
            }
            const state = fs.existsSync(stateFile) ? read(stateFile) : {
              version: 1, repository: context.repo.owner + '/' + context.repo.repo,
              checkpoint: new Date(Date.parse(end) - 3600000).toISOString(), seen: [], backlog: [], pending: []
            };
            if (state.version !== 1 || state.repository !== repository ||
                !Number.isFinite(Date.parse(state.checkpoint)) || Date.parse(state.checkpoint) > Date.parse(end) ||
                !Array.isArray(state.seen) || !Array.isArray(state.backlog) ||
                (state.pending !== undefined && !Array.isArray(state.pending)) ||
                (state.sampling_gaps !== undefined && !Array.isArray(state.sampling_gaps)) ||
                (state.recoveries !== undefined && !Array.isArray(state.recoveries))) {
              throw new Error('Invalid collection checkpoint; refusing to reset it silently');
            }
            const retentionStart = new Date(Date.parse(end) - 86400000).toISOString();
            const samplingGaps = [...(state.sampling_gaps || [])];
            for (const gap of samplingGaps) {
              if (!gap || !Number.isFinite(Date.parse(gap.start)) ||
                  !Number.isFinite(Date.parse(gap.end)) ||
                  !Number.isFinite(Date.parse(gap.observed_at)) ||
                  Date.parse(gap.start) >= Date.parse(gap.end) ||
                  Date.parse(gap.end) > Date.parse(gap.observed_at) ||
                  Date.parse(gap.observed_at) > Date.parse(end) ||
                  !/^checkpoint-[a-f0-9]{64}\.json$/.test(gap.state_file)) {
                throw new Error('Invalid sampling gap record; checkpoint unchanged');
              }
            }
            if (process.env.RECOVERY_REQUESTED === 'true') {
              const matchedKey = process.env.RESTORED_CACHE_KEY;
              if (bootstrap || process.env.ACKNOWLEDGE_GAP !== 'true' ||
                  !matchedKey || process.env.RECOVERY_CACHE_KEY !== matchedKey ||
                  process.env.RECOVERY_CHECKPOINT !== state.checkpoint ||
                  Date.parse(end) - Date.parse(state.checkpoint) <= 86400000) {
                throw new Error('Recovery requires explicit gap acceptance matching the restored cache key and stale checkpoint; checkpoint unchanged');
              }
              const recoveryID = crypto.createHash('sha256')
                .update(JSON.stringify([matchedKey, state.checkpoint, end])).digest('hex');
              const archive = path.join(cache, 'recoveries', recoveryID);
              fs.mkdirSync(archive, {recursive: true});
              for (const item of state.backlog) {
                const relative = item.report_file;
                if (typeof relative !== 'string' || !relative.startsWith('queries/') ||
                    path.relative(cache, path.resolve(cache, relative)) !== relative ||
                    path.dirname(relative) === 'queries') {
                  throw new Error('Invalid recovery evidence path; checkpoint unchanged');
                }
                const directory = path.dirname(relative);
                if (!fs.statSync(path.join(cache, relative)).isFile()) {
                  throw new Error('Missing recovery evidence; checkpoint unchanged');
                }
                fs.cpSync(path.join(cache, directory), path.join(archive, directory), {recursive: true});
              }
              fs.copyFileSync(stateFile, path.join(archive, 'checkpoint.json'));
              const recovery = {
                kind: 'acknowledged_gap', restored_cache_key: matchedKey,
                gap_start: state.checkpoint, gap_end: end,
                acknowledged_by: context.actor, run_id: context.runId,
                archive: path.relative(cache, archive), historical_coverage: false
              };
              write(path.join(archive, 'recovery.json'), recovery);
              write(path.join(data, 'recovery.json'), recovery);
              await core.summary.addRaw(`Explicitly acknowledged an uncollected historical gap from ` +
                `${state.checkpoint} to ${end}. Original state and referenced query evidence archived in ` +
                `${recovery.archive}. No health discovery, backfill, or agent activation performed. ` +
                'Recovery is durable only if the cache save succeeds.').write();
              write(stateFile, {...state, checkpoint: end,
                recoveries: [...(state.recoveries || []), recovery]});
              return;
            }
            // This checkpoint records a bounded health sample, not a census cursor.
            // Preserve missed intervals and their full state instead of claiming backfill.
            const previousState = JSON.stringify(state);
            const missedSample = Date.parse(state.checkpoint) < Date.parse(retentionStart);
            let newSnapshot;
            if (missedSample) {
              const reports = {};
              for (const item of state.backlog) {
                if (!item || typeof item.report_file !== 'string' ||
                    !/^queries\/[a-zA-Z0-9][a-zA-Z0-9._-]*\/report\.json$/.test(item.report_file)) {
                  throw new Error('Invalid prior report reference; checkpoint unchanged');
                }
                reports[item.report_file] = read(path.join(cache, item.report_file));
              }
              newSnapshot = JSON.stringify({state: JSON.parse(previousState), reports});
              samplingGaps.push({
                start: state.checkpoint, end: retentionStart, observed_at: end,
                state_file: `checkpoint-${crypto.createHash('sha256').update(newSnapshot).digest('hex')}.json`
              });
            }
            const healthTimeoutSeconds = 300;
            const health = JSON.parse(execFileSync('gh', ['aw', 'health',
              '--repo', repository, '--start-date', retentionStart, '--end-date', end,
              '--logs-dir', path.join(cache, 'health'), '--timeout', `${healthTimeoutSeconds}s`, '--json'], {
              encoding: 'utf8', timeout: (healthTimeoutSeconds + 30) * 1000, // 30s slack beyond the CLI deadline
              maxBuffer: 16 * 1024 * 1024
            }));
            const discovery = health.collection;
            if (!discovery || !Array.isArray(health.workflows) ||
                (discovery.partial !== false &&
                  !(discovery.partial === true && discovery.reason === 'failure_listing_limit')) ||
                (discovery.partial === false &&
                  (discovery.failure_listing_complete !== true || discovery.failure_pass_complete !== true)) ||
                (discovery.potential_issues !== undefined && !Array.isArray(discovery.potential_issues))) {
              throw new Error('Incomplete or invalid health discovery; checkpoint unchanged: ' +
                JSON.stringify({partial: discovery?.partial, reason: discovery?.reason,
                  failure_listing_complete: discovery?.failure_listing_complete,
                  failure_pass_complete: discovery?.failure_pass_complete,
                  workflows_array: Array.isArray(health.workflows)}));
            }
            // Health is a bounded current-revision sample, not an all-run census.
            // Only verified representatives can activate collection, even when
            // the cheap failure inventory reaches its documented page limit.
            const candidates = (discovery.potential_issues || []).filter(issue =>
              issue.representative_status === 'revision_checked' &&
              issue.workflow_path !== '.github/workflows/auditor.lock.yml' &&
              health.workflows.some(w => w.workflow_path === issue.workflow_path && !w.intentional_failure));
            const currentIDs = new Set(health.workflows.filter(w => !w.intentional_failure &&
              w.workflow_path !== '.github/workflows/auditor.lock.yml').flatMap(w =>
              (w.revisions || []).flatMap(revision => revision.current?.run_ids || [])));
            const pendingIDs = [...new Set([...(state.pending || []),
              ...state.backlog.filter(item => Date.parse(item.run.updated_at) >= Date.parse(retentionStart))
                .map(item => item.run.id)])].filter(id => currentIDs.has(id));
            const selectedIDs = [...new Set([...candidates.map(issue => issue.representative_run_id), ...pendingIDs])];
            if (selectedIDs.length >= 1000 || selectedIDs.some(id => !Number.isSafeInteger(id) || id <= 0)) {
              throw new Error('Invalid or oversized tracked run scope');
            }
            const observed = [];
            for (const id of selectedIDs) {
              const response = await request(github.rest.actions.getWorkflowRun, {run_id: id});
              if (response.data.id !== id) throw new Error('Tracked run identity changed');
              const candidate = candidates.find(issue => issue.representative_run_id === id);
              if (candidate && (response.data.path || '').split('@')[0] !== candidate.workflow_path) {
                throw new Error('Health representative workflow changed');
              }
              observed.push(response.data);
            }
            if (observed.length >= 1000) throw new Error('Combined recent/tracked metadata scope exceeded');
            const openPRs = [];
            let prsComplete = false;
            for (let page = 1; page <= 10; page++) {
              const response = await request(github.rest.pulls.list, {
                state: 'open', sort: 'created', direction: 'asc', per_page: 100, page
              });
              if (!Array.isArray(response.data)) throw new Error('Invalid PR listing');
              openPRs.push(...response.data.map(pr => ({
                number: pr.number, title: pr.title, body: pr.body, url: pr.html_url,
                head: pr.head.sha, base: pr.base.ref, draft: pr.draft
              })));
              if (!/\brel="next"/.test(response.headers.link || '')) {
                prsComplete = true; break;
              }
            }
            if (!prsComplete || new Set(openPRs.map(pr => pr.number)).size !== openPRs.length) {
              throw new Error('Incomplete or changing open PR listing; checkpoint unchanged');
            }
            const identity = r => `${r.id}-${r.run_attempt}-${r.updated_at}`;
            const seen = new Set(state.seen);
            const agentic = observed.filter(r => {
              const workflow = (r.path || '').split('@')[0];
              return workflow.endsWith('.lock.yml') &&
               workflow !== '.github/workflows/auditor.lock.yml';
            });
            for (const r of agentic) {
              if (!Number.isSafeInteger(r.id) || !Number.isInteger(r.run_attempt) ||
                 !Number.isFinite(Date.parse(r.updated_at))) throw new Error('Invalid run identity');
            }
            const eligible = agentic.filter(r => r.status === 'completed' && r.conclusion === 'failure');
            // Attempt-aware identities handle ties; FIFO batching prevents starvation.
            const fresh = eligible.filter(r =>
              Date.parse(r.updated_at) <= Date.parse(end) && !seen.has(identity(r)));
            fresh.sort((a, b) => Date.parse(a.updated_at) - Date.parse(b.updated_at) || a.id - b.id);
            const batch = fresh.slice(0, 25);
            // Retain unresolved opportunities even though only NEW failures activate
            // the agent. Drop old attempts once a newer attempt replaces them.
            const live = new Map(eligible.map(r => [identity(r), r]));
            const backlog = state.backlog.filter(item => live.has(item.key) &&
              Date.parse(item.run.updated_at) >= Date.parse(retentionStart));
            const cliVersion = execFileSync('gh', ['aw', 'version'], {encoding: 'utf8', timeout: 30000}).trim();
            const versionKey = crypto.createHash('sha256').update(cliVersion).digest('hex').slice(0, 16);
            const rate = (await github.rest.rateLimit.get()).data.resources.core;
            if (rate.remaining < 350) throw new Error('Insufficient API reserve for collection');
            // gh-aw's positive budget is absolute requests USED in this rate window,
            // not a per-query allowance. All queries share the same ceiling.
            const ceiling = rate.used + 150;
            const cli = args => {
              const stdout = execFileSync('gh', args, {
                encoding: 'utf8', timeout: 120000, maxBuffer: 16 * 1024 * 1024
              });
              const report = JSON.parse(stdout);
              if (report.partial === true || report.complete === false ||
                  (report.coverage_status && report.coverage_status !== 'complete') ||
                  report.continuation || (report.continuations || []).length) {
                throw new Error('Incomplete gh-aw evidence; checkpoint unchanged');
              }
              return report;
            };
            for (const run of batch) {
              const currentRate = (await github.rest.rateLimit.get()).data.resources.core;
              if (currentRate.used >= ceiling - 25 || currentRate.reset !== rate.reset) {
                if (currentRate.reset !== rate.reset) throw new Error('API rate window reset');
                break; // Persist unprocessed identities for the next hourly batch.
              }
              const key = identity(run);
              const dir = path.join(cache, 'queries', key.replaceAll(':', '-'));
              fs.mkdirSync(dir, {recursive: true});
              const reportFile = path.join(dir, 'report.json');
              // Per-attempt, per-version JSONL avoids reusing stale data for reruns.
              let report;
              if (fs.existsSync(reportFile) && read(reportFile).cli_version === cliVersion) {
                report = read(reportFile).report;
              } else {
                report = cli(['aw', 'logs', (run.path || '').split('@')[0],
                  '--repo', repository, '--after-run-id', String(run.id - 1),
                  '--before-run-id', String(run.id + 1), '--count', '2',
                  '--artifacts', 'info,usage', '--json', '--verbose',
                  '--cached-jsonl', path.join(dir, `logs-${versionKey}.jsonl`),
                  '--output', path.join(dir, 'artifacts'), '--timeout', '2',
                  '--max-storage', '256', '--max-github-api-rate-limit', String(ceiling)]);
                write(reportFile, {cli_version: cliVersion, report});
              }
              if (!Array.isArray(report.runs) || report.runs.length !== 1 ||
                  report.runs[0].run_id !== run.id || report.runs[0].status !== 'completed' ||
                  report.runs[0].conclusion !== 'failure' ||
                  report.runs[0].run_attempt !== String(run.run_attempt) ||
                  Date.parse(report.runs[0].updated_at) !== Date.parse(run.updated_at)) {
                throw new Error('Selected run missing or changed during collection');
              }
              if (!report.runs[0].intentional_failure) {
                if (!Array.isArray(report.failure_clusters) || !report.failure_clusters.length) {
                  throw new Error('Missing failure clustering evidence');
                }
                backlog.push({key, run, report_file: path.relative(cache, reportFile)});
              }
              seen.add(key);
            }
            const pending = agentic.filter(r => r.status !== 'completed' ||
              (r.conclusion === 'failure' && !seen.has(identity(r)))).map(r => r.id);
            const deferred = fresh.filter(r => !seen.has(identity(r))).length;
            // Recompute the union of query-local clusters; never add overlapping
            // counts. Cluster IDs identify symptoms, NOT PR root-cause fingerprints.
            const clusters = new Map();
            for (const item of backlog) {
              const report = read(path.join(cache, item.report_file)).report;
              for (const cluster of report.failure_clusters) {
                if (cluster.intentional_failure) continue;
                const signature = JSON.stringify([cluster.signal, cluster.failure_kind, cluster.signal_role]);
                if (!clusters.has(signature)) clusters.set(signature, {
                  id: crypto.createHash('sha256').update(signature).digest('hex').slice(0, 16),
                  signal: cluster.signal, signal_role: cluster.signal_role,
                  evidence: cluster.evidence, members: [], source_cluster_ids: []
                });
                const group = clusters.get(signature);
                if (!group.members.some(m => m.run.id === item.run.id)) group.members.push(item);
                if (!group.source_cluster_ids.includes(cluster.id)) group.source_cluster_ids.push(cluster.id);
              }
            }
            const ranked = [...clusters.values()].sort((a, b) =>
              b.members.length - a.members.length ||
              Number(b.signal_role === 'failure_boundary') - Number(a.signal_role === 'failure_boundary') ||
              a.id.localeCompare(b.id));
            const newFailures = fresh.filter(r => backlog.some(m => m.key === identity(r))).length;
            let auditCount = 0;
            for (const cluster of ranked) {
              if (newFailures === 0 || deferred > 0 || auditCount >= 3) break;
              const member = cluster.members[0];
              const dir = path.join(cache, path.dirname(member.report_file));
              const currentRate = (await github.rest.rateLimit.get()).data.resources.core;
              if (currentRate.used >= ceiling - 25 || currentRate.reset !== rate.reset) {
                throw new Error('Shared API budget exhausted or reset; checkpoint unchanged');
              }
              // Native --audit generates audit.json LOCALLY from these downloaded
              // runs, without automatic baseline or extra remote audit downloads.
              // Discovery/cache validation still shares the logs API ceiling.
              const audited = cli(['aw', 'logs', (member.run.path || '').split('@')[0],
                '--repo', repository, '--after-run-id', String(member.run.id - 1),
                '--before-run-id', String(member.run.id + 1), '--count', '2',
                '--artifacts', 'info,usage', '--audit', '--json', '--verbose',
                '--cached-jsonl', path.join(dir, `logs-${versionKey}.jsonl`),
                '--output', path.join(dir, 'artifacts'), '--timeout', '2',
                '--max-storage', '256', '--max-github-api-rate-limit', String(ceiling)]);
              const selected = audited.runs?.[0];
              if (audited.runs?.length !== 1 || selected.run_id !== member.run.id ||
                  selected.run_attempt !== String(member.run.run_attempt) ||
                  Date.parse(selected.updated_at) !== Date.parse(member.run.updated_at) ||
                  selected.conclusion !== 'failure' || !selected.audit_path) {
                throw new Error('Selected audit missing or changed during collection');
              }
              const auditFile = path.resolve(selected.audit_path);
              if (!auditFile.startsWith(path.resolve(dir) + path.sep)) {
                throw new Error('Audit path escaped query cache');
              }
              const audit = read(auditFile);
              if (audit.partial === true || audit.complete === false ||
                  audit.overview?.run_id !== member.run.id || audit.overview?.conclusion !== 'failure') {
                throw new Error('Invalid cached audit; checkpoint unchanged');
              }
              cluster.audit_file = auditFile;
              auditCount++;
            }
            // Copy ONLY this collection's compact reports and selected audit
            // evidence. Do not upload the whole historical download cache.
            const manifest = {
              complete: true, repository, window_start: retentionStart, window_end: end,
              retention_start: retentionStart, bootstrap, cli_version: cliVersion,
              discovery, sampling_gaps: samplingGaps,
              new_failures: newFailures,
              evidence_complete: deferred === 0, pending_runs: pending.length, deferred_failures: deferred,
              open_prs_complete: true, open_prs: openPRs, clusters: ranked,
              limits: {health_timeout_seconds: healthTimeoutSeconds, metadata_requests: 1025, metadata_runs: 999,
                pr_pages: 10, new_runs: 25, audits: 3, api_used_ceiling: ceiling}
            };
            for (const [index, cluster] of manifest.clusters.entries()) {
              cluster.count = cluster.members.length;
              cluster.members = cluster.members.map((member, n) => {
                const file = `cluster-${index}-run-${n}.json`;
                fs.copyFileSync(path.join(cache, member.report_file), path.join(data, file));
                return {run: member.run, report_file: file};
              });
              if (cluster.audit_file) {
                const file = `cluster-${index}-audit.json`;
                fs.copyFileSync(cluster.audit_file, path.join(data, file));
                cluster.audit_file = file;
              }
            }
            const finalRate = (await github.rest.rateLimit.get()).data.resources.core;
            if (finalRate.used > ceiling || finalRate.reset !== rate.reset) {
              throw new Error('Shared API budget exceeded or reset; checkpoint unchanged');
            }
            const recoveryDir = path.join(cache, 'recovery');
            for (const [index, gap] of samplingGaps.entries()) {
              const isNew = missedSample && index === samplingGaps.length - 1;
              const snapshot = isNew ? newSnapshot :
                fs.readFileSync(path.join(recoveryDir, gap.state_file), 'utf8');
              const expected = `checkpoint-${crypto.createHash('sha256').update(snapshot).digest('hex')}.json`;
              if (gap.state_file !== expected) {
                throw new Error('Sampling gap snapshot changed; checkpoint unchanged');
              }
              fs.mkdirSync(recoveryDir, {recursive: true});
              fs.mkdirSync(path.join(data, 'recovery'), {recursive: true});
              if (isNew) fs.writeFileSync(path.join(recoveryDir, gap.state_file), snapshot);
              fs.writeFileSync(path.join(data, 'recovery', gap.state_file), snapshot);
            }
            write(path.join(data, 'manifest.json'), manifest);
            // Verified health sample and complete PR scope; evidence may be a FIFO batch.
            // Never mark deferred attempts seen. Cache save must also succeed.
            write(stateFile, {...state, checkpoint: end, sampling_gaps: samplingGaps,
              seen: eligible.filter(r => Date.parse(r.updated_at) >= Date.parse(retentionStart))
                .map(identity).filter(key => seen.has(key)), backlog, pending});
            if (samplingGaps.length) {
              core.warning(`${samplingGaps.length} historical sampling gap(s) remain unresolved. ` +
                'Previous checkpoint states are preserved in recovery/; current health evidence does not backfill them.');
            }
            core.setOutput('has_new_failures',
              manifest.new_failures > 0 && manifest.evidence_complete ? 'true' : 'false');
            await core.summary.addRaw(`Collected ${manifest.new_failures} new non-intentional failures; ` +
              `${ranked.length} symptom clusters; ${pending.length} deferred runs. Health window ${retentionStart} to ${end}. ` +
              `Discovery is a bounded sample (partial=${discovery.partial}). ` +
              `${samplingGaps.length} unresolved historical sampling gaps; prior states preserved in recovery/. ` +
              'No new failures means no agent, not proof that the backlog is healthy.').write();
      - name: Upload compact collection
        uses: actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9 # v7.0.2
        with:
          name: auditor-collection
          path: ${{ runner.temp }}/auditor-data
          if-no-files-found: error
          retention-days: 2
      - name: Save successful collection checkpoint and query cache
        uses: actions/cache/save@55cc8345863c7cc4c66a329aec7e433d2d1c52a9 # v6.1.0
        with:
          path: ${{ runner.temp }}/auditor-cache
          key: auditor-collection-v1-${{ github.repository_id }}-${{ github.ref_name }}-${{ github.run_id }}-${{ github.run_attempt }}
steps:
  - name: Download this run's compact collection
    uses: actions/download-artifact@9000827ccba6bdab643e8b6fd33ac0654aef8333 # v8.0.2
    with:
      name: auditor-collection
      path: /tmp/gh-aw/auditor
  - name: Setup Go for focused regression tests
    uses: actions/setup-go@v7.0.0
    with:
      go-version-file: go.mod
      cache: true
  - name: Setup Node.js for focused regression tests
    uses: actions/setup-node@v7.1.0
    with:
      node-version: "24"
---

# Auditor

Read `/tmp/gh-aw/auditor/manifest.json` first. Call `noop` if it is missing,
invalid, incomplete, has no **new** non-intentional failures, lacks a complete
open-PR listing, or has `evidence_complete: false`. Collection failure is not an
empty result; deferred failures remain checkpointed for later batches. Unfinished
runs do not block ranking completed failures.

Read `sampling_gaps` separately: a gap does not invalidate current evidence or
prove historical coverage or recovery. Preserved `recovery/` snapshots retain
the previous seen/backlog/pending state for separately authorized reconciliation.

## Success criteria and self-check

Success is one validated, novel draft PR for the largest actionable evidenced
cluster, or a `noop` explaining why no safe fix can be published. Before finishing,
check the evidence and coverage limitations, confirm all required validation and
fresh PR deduplication for any fix, and ensure the result distinguishes observed
facts from inference. Never publish when a required check is incomplete.

## Evidence and selection

The trusted `collect` job uses `gh aw health` and `gh aw logs`, caches
attempt-specific evidence, and locally generates up to three audits. Read its
compact reports and audit JSON before source inspection. Only the manifest's
relative files were transferred; report audit paths refer to the collection
runner. Do not rerun logs, download all artifacts, broaden the window, or
perform further remote audits.

Rank unresolved clusters by unique affected run count (descending), breaking
ties with first failing boundaries before aftermath. Counts overlap: do not sum
them. A cluster is a symptom grouping, not a proven shared cause. Compare the
typed job/step, normalized error, failing component, engine/model, revision and
configuration. Exclude intentional tests, this auditor, obsolete attempts,
already-fixed default-branch behavior and self-recovered failures. Missing
diagnostics remain unknown; do not invent a fix from metadata alone.

Inspect clusters in ranked order, first checking PR coverage. Select the largest
unresolved cluster whose supplied audit proves an actionable root cause. Only
the first three clusters have audit coverage; if the largest unresolved cluster
has no audit or insufficient evidence, `noop` with the collection limitation
rather than fix a smaller convenient cluster. Never retry, rerun or dispatch
workflows, upload live tests, change credentials, or weaken security gates.

## Deduplicate against every open PR

Read **all** `open_prs` in the manifest, including drafts, human PRs and PRs from
other automations; do not filter by author, prefix, label or workflow marker.
Compare titles and bodies semantically with the diagnosed cause and proposed
repair. For plausible matches, inspect the PR diff through `gh` (at most five
PRs); if ambiguity remains, stop without creating a PR.

Derive the publication fingerprint only after confirming the root cause:
`auditor-root-cause:v1:<sha256>`, using a lowercase, whitespace-normalized tuple
of repository, defective source component/path, violated invariant and repair
mechanism. Do not hash run IDs, dates, head SHAs, transient paths, raw error
messages, or query-local cluster IDs. Include the canonical tuple and
`<!-- auditor-root-cause:v1:<sha256> -->` in the PR body. Match exact fingerprints
AND semantically equivalent fixes without markers. If covered, skip that cluster
and consider the next largest unresolved cluster within the audit budget.

Immediately before publication refresh the entire open-PR listing with
`gh api repos/${{ github.repository }}/pulls?state=open&per_page=100&page=N`,
with the endpoint quoted, one page at a time, using response Link headers.
Stop after ten pages; if `rel="next"` remains, any request fails, identities
repeat, or a possible semantic match cannot be ruled out, call `noop`.
Reapply both fingerprint and semantic dedup to this fresh complete listing.
Do not mutate, close, merge, comment on or push to existing PRs. Concurrency
serializes this auditor, but cannot prevent an external author racing publication.

## Make one focused fix

Read `AGENTS.md`, `CONTRIBUTING.md` and `.github/aw/instructions.md`. Inspect the
defective source at the failed revision (using supplied evidence and bounded
read-only GitHub queries when unavailable locally), then confirm the same
defect exists in this checkout. Never fetch/pull/push authenticated git remotes:
checkout credentials are deliberately not persisted.

Fix the confirmed cause, not cascades. Add a focused regression test and follow
repository format/lint/build/test gates. Do not modify upstream-managed `source:`
workflows locally; report their upstream fix requirement with `noop`. Do not
add dependencies, unrelated cleanup, instruction changes, or generated-file-only
fixes. If workflow Markdown changes, compile and run `make recompile`, inspect
the generated result and discard unrelated generation drift. The native
`excluded-files: ["**/*.lock.yml"]` removes **all** lock YAML from PR output;
explain any required maintainer recompilation of source workflow changes in the
PR. Do not bypass the exclusion or commit a generated lock under another name.

Create at most one draft PR via `create_pull_request` only after validation and
fresh dedup pass. Use `###` or lower headings, with `### Summary`, `### Root
cause`, `### Validation`, and `### Coverage and limitations`. Keep the overview,
root cause, key impact, and next actions visible; follow the sequence overview →
findings and metrics → collapsible detailed evidence → next actions or
limitations. Put lengthy logs and per-run evidence in
`<details><summary><b>...</b></summary>...</details>`. Include the stable
fingerprint/tuple, source cluster IDs, unique affected count, collection window,
remaining clusters, up to three linked run references, and actual test results.
Format run references as `[§12345](https://github.com/${{ github.repository }}/actions/runs/12345)`.
Separate observed evidence from inference and local validation from unperformed
live checks.
If no safe, novel, evidenced fix is available, call `noop` with a concise reason.

## Usage and collection limits

Scheduled collection samples the preceding 24 hours; manual dispatch is
default-branch recovery only and cannot activate the agent. Only revision-checked
representatives of registered agentic workflows are eligible; superseded,
jobless, unverified, intentional and auditor failures are excluded.
`discovery` describes a bounded sample, not a census or fleet failure rate.
The `failure_listing_limit` may leave rankings partial; other partial or invalid
health results fail closed without advancing the last-successful-sample checkpoint.

Gaps over 24 hours are recorded in `sampling_gaps`, with content-addressed prior
state and compact reports retained in `recovery/` and uploaded. Gaps persist and
warn; they are not backfilled or declared healthy. Reconciliation requires separately
authorized complete backfill or explicit gap acceptance. Invalid, missing or changed
snapshots fail closed. Pending/backlog IDs are refreshed only in health's
current-revision cohorts.
Fail closed at 1,000 selected metadata runs, 1,025 metadata requests, ten PR
pages, incomplete evidence results, or exhausted API reserve.
Process at most 25 new failures FIFO per hour; excess candidates are checkpointed,
never marked seen, and proceed next hour even without newly created failures.

### Explicit stale-checkpoint recovery

Do not delete the cache, change its namespace, or reset the checkpoint to make a
failed collection green. If historical backfill is required, stop: this recovery
path does **not** implement it. Health's current-revision sample cannot establish
historical coverage.

An operator who explicitly accepts the missing history may manually dispatch
the default-branch workflow with `acknowledge_gap: true`, `recovery_cache_key`
equal to the exact restored key shown in the failed collection log, and
`recovery_checkpoint` equal to the exact timestamp in that cache's
`checkpoint.json`. Acceptance covers that checkpoint through recovery startup;
the receipt records both timestamps, actor, run ID and source cache key. Missing,
mismatched, already-recovered, or non-stale checkpoints are rejected. The default
unchecked input does nothing; scheduled runs never acknowledge gaps.

Recovery performs no health/log/audit queries and always leaves
`has_new_failures` false. Before advancing, it copies the original checkpoint
byte-for-byte and every backlog-referenced query directory (including cached
JSONL and downloaded evidence) under `recoveries/<receipt-id>/` in the same cache.
The active state's `seen`, `backlog`, and `pending` remain unchanged; its
`recoveries` receipts and their archives survive subsequent ordinary pruning.
Archive report paths are relative to the archived checkpoint's directory; any
absolute paths embedded in reports are historical runner paths. The compact
artifact contains only the recovery receipt, not the historical evidence.

Only a successful cache save persists recovery. Verify that save before treating
it as durable; GitHub cache expiry/eviction still applies to all cached evidence.
The next hourly collection resumes the existing bounded 24-hour health sample,
not a census or validation of the accepted gap. Health errors still fail closed;
the error now reports completeness flags and the partial reason to distinguish
schema incompatibility from time/API budgets without accepting partial failures.
Backlog retention uses a separate now-minus-24-hours boundary, regardless of discovery
start. Rankings cover retained sampled failures updated in that period plus this batch;
do not activate the agent while failures remain unprocessed. Refresh retained
processed runs outside discovery too, preserving seen identities until retention
expires and removing superseded attempts without needlessly re-auditing identical runs.
When no checkpoint is restored, the collector checks all Auditor run
history before initializing state. Any prior run or retry makes missing cache a
recovery-required error, never a fresh bootstrap: restore the checkpoint and
recovery files from retained cache/artifacts or reconcile the loss explicitly.
Only the current first attempt without any earlier execution may bootstrap;
overall workflow failure does not prove that collection saved no state.
Recovery bundles contain compact reports, not the full original raw artifacts.
The checkpoint records a successful **collection**, independently of whether
the agent later fixes anything; unresolved failures stay in the backlog and
are reconsidered only when another new failure activates the agent. Cache loss
can cause reinspection, but complete all-author semantic PR dedup remains required.
