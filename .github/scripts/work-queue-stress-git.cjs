"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { spawn } = require("node:child_process");
const { createHash } = require("node:crypto");
const { registerQueueLedgerClient } = require("../../actions/setup/js/work_queue_git_transport.cjs");
const { ASSIGNMENT_BYTES, CHECKPOINT_AFTER_LINES, GRAPH_NODES, LEDGER_BYTES, MAX_PENDING_WORK, OPERATIONS_PER_COMMIT } = require("../../actions/setup/js/work_queue_limits.cjs");
const { actorFromContext, validatePolicyEnv, workerWorkflow } = require("../../actions/setup/js/work_queue_policy.cjs");

const REPOSITORY = "local/queue";
const [OWNER, REPO] = REPOSITORY.split("/");
// The one approved worker ID; its compiled lock file is the launched workflow.
const WORKER_ID = "worker";
const WORKFLOW = workerWorkflow(WORKER_ID);
const PUBLISHER_WORKFLOW = ".github/workflows/publisher.lock.yml";
// Workers launch at the branch ref, like a dispatcher's GITHUB_REF; v4 pins the resolved revision.
const LAUNCH_REF = "refs/heads/main";
const AT = 1800000000000;
// v4 wire-format constants the stress profiles run against; none is raised for the harness.
const PROTOCOL_LIMITS = Object.freeze({
  ledger_bytes: LEDGER_BYTES,
  operations: OPERATIONS_PER_COMMIT,
  graph_nodes: GRAPH_NODES,
  pending_work: MAX_PENDING_WORK,
  assignment_bytes: ASSIGNMENT_BYTES,
  checkpoint_after_lines: CHECKPOINT_AFTER_LINES,
  claims_per_dispatch: 1,
});
const METHODS = {
  repos: ["get"],
  git: ["getRef", "getCommit", "getTree", "getBlob", "createBlob", "createTree", "createCommit", "createRef", "updateRef"],
  actions: ["getWorkflowRun", "createWorkflowDispatch"],
};

/**
 * A publishing actor built from a job context the way production jobs build it. v4 needs
 * no API proof of the actor: write access to the queue branch is the only authority.
 */
function queueActor(role, runId = "100", runAttempt = 1, workflow = role === "worker" ? WORKFLOW : PUBLISHER_WORKFLOW) {
  return actorFromContext({ repo: { owner: OWNER, repo: REPO }, runId, runAttempt }, role, { GITHUB_WORKFLOW_REF: `${REPOSITORY}/${workflow}@${LAUNCH_REF}` });
}

/**
 * The compiled-policy shape a dispatcher lock carries in GH_AW_WORK_QUEUE_POLICY, checked by the
 * production validator. Only scheduling preferences are configurable; protocol bounds are constants.
 * @param {Record<string, {concurrency: number, per_account_limit: number, max_attempts: number}>} pools
 */
function stressPolicy(pools) {
  return validatePolicyEnv({ version: 4, pending_limit: MAX_PENDING_WORK, issues: false, effect_scope: REPOSITORY, workers: [WORKER_ID], pools });
}

/** dispatch_next parameters as dispatchQueueIntent derives them from the policy pool and allowlist. */
function dispatchParameters(policy, pool, maxClaims) {
  const { concurrency, per_account_limit } = policy.pools[pool];
  return { pool, max_claims: maxClaims, max_dispatches: maxClaims, concurrency, per_account_limit, profiles: { [WORKER_ID]: { workflow: WORKFLOW } } };
}

function clientFor(call) {
  const client = { rest: Object.fromEntries(Object.entries(METHODS).map(([group, names]) => [group, Object.fromEntries(names.map(name => [name, args => call(`${group}.${name}`, args)]))])) };
  // Both parent and worker RPC clients must keep ledger traffic on the local Git fixture.
  registerQueueLedgerClient(client, client);
  return client;
}

function apiError(status, message) {
  return Object.assign(new Error(message), { status });
}

// Object writes and reference changes use Git itself, including its atomic
// expected-old update-ref. No in-memory ref/commit store substitutes for Git.
class LocalGitHub {
  constructor(directory, { seed = 1, gitTimeoutMs = 30000 } = {}) {
    this.directory = directory;
    this.seed = seed;
    this.gitTimeoutMs = gitTimeoutMs;
    this.abort = new AbortController();
    this.sequence = 0;
    this.runs = new Map();
    // dispatch ID -> every worker run POSTed for it; only the recorded launch may bind.
    this.launches = new Map();
    this.receipts = new Map();
    this.faults = new Map();
    this.children = new Set();
    this.metrics = { git_commands: 0, api_calls: 0, update_attempts: 0, cas_conflicts: 0, lost_success_responses: 0, forced_races: 0, native_posts: 0 };
    this.client = clientFor((method, args) => this.call(method, args));
  }

  async git(args, input = "") {
    this.metrics.git_commands++;
    return new Promise((resolve, reject) => {
      const child = spawn("git", ["--git-dir", this.directory, ...args], {
        env: {
          PATH: process.env.PATH,
          HOME: this.directory,
          GIT_CONFIG_NOSYSTEM: "1",
          GIT_CONFIG_GLOBAL: path.join(this.directory, "absent-global-config"),
          GIT_TERMINAL_PROMPT: "0",
          GIT_AUTHOR_NAME: "Local queue simulator",
          GIT_AUTHOR_EMAIL: "simulator@localhost",
          GIT_COMMITTER_NAME: "Local queue simulator",
          GIT_COMMITTER_EMAIL: "simulator@localhost",
          GIT_AUTHOR_DATE: `@${1800000000 + this.sequence} +0000`,
          GIT_COMMITTER_DATE: `@${1800000000 + this.sequence} +0000`,
        },
        stdio: ["pipe", "pipe", "pipe"],
        signal: this.abort.signal,
      });
      this.children.add(child);
      let bytes = 0;
      const out = [];
      const err = [];
      const timer = setTimeout(() => child.kill("SIGKILL"), this.gitTimeoutMs);
      child.stdout.on("data", chunk => {
        bytes += chunk.length;
        if (bytes > 112 * 1024 * 1024) child.kill("SIGKILL");
        else out.push(chunk);
      });
      child.stderr.on("data", chunk => err.push(chunk));
      child.stdin.on("error", () => {});
      child.on("error", reject);
      child.on("close", code => {
        clearTimeout(timer);
        this.children.delete(child);
        if (code === 0) resolve(Buffer.concat(out));
        else reject(Object.assign(new Error(`git ${args[0]} failed (${code}): ${Buffer.concat(err).toString("utf8").trim()}`), { gitCode: code }));
      });
      child.stdin.end(input);
    });
  }

  async initialize() {
    await fs.mkdir(this.directory, { recursive: true });
    await this.git(["init", "--bare", "--quiet", "--initial-branch=main", this.directory]);
    const workflow = `name: Local worker\non:\n  workflow_dispatch:\n    inputs:\n      work_queue_assignment:\n        type: string\n        required: true\njobs:\n  worker:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo local-only\n`;
    const blob = (await this.git(["hash-object", "-w", "--stdin"], workflow)).toString().trim();
    const workflows = (await this.git(["mktree"], `100644 blob ${blob}\tworker.lock.yml\n100644 blob ${blob}\tpublisher.lock.yml\n`)).toString().trim();
    const github = (await this.git(["mktree"], `040000 tree ${workflows}\tworkflows\n`)).toString().trim();
    const tree = (await this.git(["mktree"], `040000 tree ${github}\t.github\n`)).toString().trim();
    this.ref = (await this.git(["commit-tree", tree], "Local immutable workflow revision\n")).toString().trim();
    await this.git(["update-ref", LAUNCH_REF, this.ref]);
    this.runs.set("100", this.run("100", PUBLISHER_WORKFLOW, "schedule", this.ref));
    return this;
  }

  run(id, workflow, event, revision, dispatchId = undefined) {
    return {
      id: Number(id),
      run_attempt: 1,
      repository: { full_name: REPOSITORY, id: 7 },
      path: workflow,
      event,
      head_sha: revision,
      display_title: dispatchId ? `local work-queue ${dispatchId}` : "local publisher",
      status: "in_progress",
      conclusion: null,
      created_at: "2026-10-08T00:00:00Z",
      ...(dispatchId ? { dispatch_id: dispatchId } : {}),
    };
  }

  /** The job context a run sees: its repository, run attempt and checked-out revision. */
  nativeContext(runId = "100", attempt = 1) {
    const run = this.runs.get(String(runId));
    assert.ok(run, "unknown simulated native run");
    return { repo: { owner: OWNER, repo: REPO }, runId: String(run.id), runAttempt: attempt, sha: run.head_sha, eventName: run.event };
  }

  armFaults(branch) {
    assert.ok(!this.faults.has(branch));
    this.faults.set(branch, { arrivals: [], passed: false, lost: false });
  }

  async barrier(branch, meta) {
    const fault = this.faults.get(branch);
    if (!fault || fault.passed) return;
    return new Promise((resolve, reject) => {
      const entry = { resolve, reject, worker: meta.worker ?? 0 };
      entry.timer = setTimeout(() => reject(new Error("forced CAS race deadline exceeded: need two concurrent publishers")), this.gitTimeoutMs);
      fault.arrivals.push(entry);
      if (fault.arrivals.length === 2) {
        fault.passed = true;
        this.metrics.forced_races++;
        // The seed chooses the first candidate, not a probabilistic timing race.
        fault.arrivals.sort((a, b) => ((a.worker + this.seed) % 2) - ((b.worker + this.seed) % 2));
        for (const waiting of fault.arrivals) clearTimeout(waiting.timer);
        fault.arrivals[0].resolve();
        fault.next = fault.arrivals[1];
      }
    });
  }

  async reference(ref) {
    assert.match(ref, /^refs\/heads\/[A-Za-z0-9/_-]+$/);
    try {
      return (await this.git(["show-ref", "--verify", "--hash", ref])).toString().trim();
    } catch (error) {
      if (error.gitCode === 1 || error.gitCode === 128) throw apiError(404, "reference does not exist");
      throw error;
    }
  }

  async entries(tree) {
    assert.match(tree, /^[a-f0-9]{40}$/);
    const output = (await this.git(["ls-tree", "-z", tree])).toString("utf8");
    return output
      .split("\0")
      .filter(Boolean)
      .map(line => {
        const [, mode, type, sha, name] = /^(\d+) (\w+) ([a-f0-9]+)\t([\s\S]+)$/.exec(line);
        return { mode, type, sha, path: name };
      });
  }

  async call(method, args = {}, meta = {}) {
    this.metrics.api_calls++;
    if (!method.startsWith("sim.")) assert.equal(`${args.owner}/${args.repo}`, REPOSITORY, "adapter rejects foreign repositories");
    const data = value => ({ status: 200, data: value });
    switch (method) {
      case "repos.get":
        return data({ full_name: REPOSITORY, id: 7, default_branch: "main", size: 1, permissions: { pull: true } });
      case "git.getRef": {
        const ref = `refs/${args.ref}`;
        const sha = await this.reference(ref);
        return data({ ref, object: { type: "commit", sha } });
      }
      case "git.getCommit": {
        assert.match(args.commit_sha, /^[a-f0-9]{40}$/);
        const text = (await this.git(["cat-file", "-p", args.commit_sha])).toString();
        return data({ sha: args.commit_sha, tree: { sha: /^tree ([a-f0-9]+)$/m.exec(text)[1] }, parents: [...text.matchAll(/^parent ([a-f0-9]+)$/gm)].map(match => ({ sha: match[1] })) });
      }
      case "git.getTree":
        return data({ sha: args.tree_sha, truncated: false, tree: await this.entries(args.tree_sha) });
      case "git.getBlob": {
        assert.match(args.file_sha, /^[a-f0-9]{40}$/);
        const contents = await this.git(["cat-file", "blob", args.file_sha]);
        return data({ encoding: "base64", content: contents.toString("base64"), size: contents.length });
      }
      case "git.createBlob": {
        assert.ok(["utf-8", "base64"].includes(args.encoding));
        const bytes = args.encoding === "base64" ? Buffer.from(args.content, "base64") : Buffer.from(args.content);
        return data({ sha: (await this.git(["hash-object", "-w", "--stdin"], bytes)).toString().trim() });
      }
      case "git.createTree": {
        const entries = new Map((args.base_tree ? await this.entries(args.base_tree) : []).map(entry => [entry.path, entry]));
        for (const entry of args.tree) {
          assert.match(entry.path, /^[A-Za-z0-9_.-]+$/);
          assert.equal(entry.mode, "100644");
          assert.equal(entry.type, "blob");
          assert.match(entry.sha, /^[a-f0-9]{40}$/);
          entries.set(entry.path, entry);
        }
        const input = [...entries.values()].map(entry => `${entry.mode} ${entry.type} ${entry.sha}\t${entry.path}\0`).join("");
        return data({ sha: (await this.git(["mktree", "-z"], input)).toString().trim() });
      }
      case "git.createCommit": {
        assert.match(args.tree, /^[a-f0-9]{40}$/);
        for (const sha of args.parents) assert.match(sha, /^[a-f0-9]{40}$/);
        this.sequence++;
        const sha = (await this.git(["commit-tree", args.tree, ...args.parents.flatMap(parent => ["-p", parent])], `${args.message}\n`)).toString().trim();
        return data({ sha });
      }
      case "git.createRef": {
        assert.match(args.ref, /^refs\/heads\/[A-Za-z0-9/_-]+$/);
        const branch = args.ref.replace(/^refs\/heads\//, "");
        await this.barrier(branch, meta);
        const fault = this.faults.get(branch);
        let changed = false;
        try {
          try {
            await this.git(["update-ref", args.ref, args.sha, "0".repeat(40)]);
          } catch {
            this.metrics.cas_conflicts++;
            throw apiError(422, "reference already exists");
          }
          changed = true;
          if (fault && !fault.lost) {
            fault.lost = true;
            this.metrics.lost_success_responses++;
            throw apiError(503, "injected lost successful create response");
          }
          return data({ ref: args.ref, object: { type: "commit", sha: args.sha } });
        } finally {
          if (changed && fault?.next) {
            fault.next.resolve();
            delete fault.next;
          }
        }
      }
      case "git.updateRef": {
        assert.equal(args.force, false, "publisher must not force refs");
        assert.match(args.sha, /^[a-f0-9]{40}$/);
        const branch = args.ref.replace(/^heads\//, "");
        await this.barrier(branch, meta);
        this.metrics.update_attempts++;
        const fault = this.faults.get(branch);
        let changed = false;
        try {
          const ref = `refs/${args.ref}`;
          const previous = await this.reference(ref);
          try {
            await this.git(["merge-base", "--is-ancestor", previous, args.sha]);
            await this.git(["update-ref", ref, args.sha, previous]);
          } catch {
            this.metrics.cas_conflicts++;
            throw apiError(422, "not a fast-forward: reference update failed");
          }
          changed = true;
          if (fault && !fault.lost) {
            fault.lost = true;
            this.metrics.lost_success_responses++;
            throw apiError(503, "injected lost successful update response");
          }
          return data({ object: { sha: args.sha } });
        } finally {
          // Keep the second real candidate fenced until Git has advanced, so its
          // actual ancestry check (not a fabricated 422) establishes the conflict.
          if (changed && fault?.next) {
            fault.next.resolve();
            delete fault.next;
          }
        }
      }
      case "actions.getWorkflowRun": {
        const run = this.runs.get(String(args.run_id));
        if (!run) throw apiError(404, "native run not found");
        return data(structuredClone(run));
      }
      case "actions.createWorkflowDispatch": {
        // v4 launches at a branch ref; the created run carries the revision it resolved to.
        assert.equal(args.ref, LAUNCH_REF);
        const revision = await this.reference(args.ref);
        assert.equal(revision, this.ref, "workflow dispatch must resolve to the local workflow revision");
        assert.equal(args.workflow_id, path.basename(WORKFLOW));
        assert.equal(args.return_run_details, true, "a v4 launch asks GitHub for the created run");
        assert.equal(args.headers["X-GitHub-Api-Version"], "2026-03-10");
        assert.equal(args.request.retries, 0);
        const input = JSON.parse(args.inputs.work_queue_assignment);
        assert.deepEqual(Object.keys(input).sort(), ["dispatch_id", "version"]);
        assert.equal(input.version, 4);
        const id = String(1000 + this.metrics.native_posts);
        this.runs.set(id, this.run(id, WORKFLOW, "workflow_dispatch", revision, input.dispatch_id));
        // A dispatcher that POSTs twice creates two real runs; only its recorded launch may bind.
        const launch = this.launches.get(input.dispatch_id) ?? { count: 0, run_ids: [] };
        launch.count++;
        launch.run_ids.push(id);
        this.launches.set(input.dispatch_id, launch);
        this.metrics.native_posts++;
        return data({ workflow_run_id: Number(id), run_url: `https://api.github.com/repos/${REPOSITORY}/actions/runs/${id}`, html_url: `https://github.com/${REPOSITORY}/actions/runs/${id}` });
      }
      case "sim.nativeContext":
        return this.nativeContext(args.run_id);
      case "sim.receipt": {
        // The worker's simulated no-op effect; its receipt is what the Completion records.
        const run = this.runs.get(args.run_id);
        assert.equal(run?.status, "in_progress", "only a live worker run delivers effects");
        assert.equal(run.dispatch_id, args.dispatch_id);
        const record = { run_id: args.run_id, dispatch_id: args.dispatch_id, claim_id: args.claim_id, work_id: args.work_id, handle: args.handle, effect: "none" };
        const receipt = `local_receipt:${createHash("sha256").update(JSON.stringify(record)).digest("hex")}`;
        this.receipts.set(receipt, record);
        return { receipt };
      }
      case "sim.completeRun": {
        const run = this.runs.get(args.run_id);
        assert.ok(run?.dispatch_id);
        assert.ok(
          [...this.receipts.values()].some(record => record.run_id === args.run_id),
          "a successful worker run delivered its effect"
        );
        run.status = "completed";
        run.conclusion = "success";
        return { completed: true };
      }
      case "sim.cancelRun": {
        const run = this.runs.get(args.run_id);
        assert.ok(run?.dispatch_id);
        assert.ok(![...this.receipts.values()].some(record => record.run_id === args.run_id), "a refused duplicate run must not deliver effects");
        run.status = "completed";
        run.conclusion = "cancelled";
        return { cancelled: true };
      }
      default:
        throw new Error(`unsupported local API method: ${method}`);
    }
  }

  async close() {
    for (const fault of this.faults.values()) {
      for (const arrival of fault.arrivals) {
        clearTimeout(arrival.timer);
        arrival.reject(new Error("local Git adapter closing"));
      }
    }
    this.abort.abort();
    await Promise.all([...this.children].map(child => new Promise(resolve => child.once("close", resolve))));
  }
}

module.exports = { AT, LAUNCH_REF, LocalGitHub, OWNER, PROTOCOL_LIMITS, PUBLISHER_WORKFLOW, REPO, REPOSITORY, WORKER_ID, WORKFLOW, clientFor, dispatchParameters, queueActor, stressPolicy };
