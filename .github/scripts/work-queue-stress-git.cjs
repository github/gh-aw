"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { spawn } = require("node:child_process");
const { createHash } = require("node:crypto");

const REPOSITORY = "local/queue";
const PRINCIPAL = "1001";
const WORKFLOW = ".github/workflows/worker.lock.yml";
const PUBLISHER_WORKFLOW = ".github/workflows/publisher.lock.yml";
const METHODS = {
  repos: ["get", "getContent"],
  git: ["getRef", "getCommit", "getTree", "getBlob", "createBlob", "createTree", "createCommit", "createRef", "updateRef"],
  actions: ["getWorkflow", "getWorkflowRun", "getWorkflowRunAttempt", "createWorkflowDispatch"],
};

function clientFor(call) {
  return { rest: Object.fromEntries(Object.entries(METHODS).map(([group, names]) => [group, Object.fromEntries(names.map(name => [name, args => call(`${group}.${name}`, args)]))])) };
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
    this.originalRuns = new Map();
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
    const workflow =
      "name: Local worker\non:\n  workflow_dispatch:\n    inputs:\n      work_queue_assignment:\n        type: string\n        required: true\njobs:\n  worker:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo local-only\n";
    const blob = (await this.git(["hash-object", "-w", "--stdin"], workflow)).toString().trim();
    const workflows = (await this.git(["mktree"], `100644 blob ${blob}\tworker.lock.yml\n100644 blob ${blob}\tpublisher.lock.yml\n`)).toString().trim();
    const github = (await this.git(["mktree"], `040000 tree ${workflows}\tworkflows\n`)).toString().trim();
    const tree = (await this.git(["mktree"], `040000 tree ${github}\t.github\n`)).toString().trim();
    this.ref = (await this.git(["commit-tree", tree], "Local immutable workflow revision\n")).toString().trim();
    await this.git(["update-ref", "refs/heads/main", this.ref]);
    const publisher = this.run("100", PUBLISHER_WORKFLOW, "schedule");
    this.runs.set("100", publisher);
    this.originalRuns.set("100", structuredClone(publisher));
    return this;
  }

  run(id, workflow, event, assignment) {
    return {
      id,
      run_attempt: 1,
      repository: { full_name: REPOSITORY, id: 7 },
      workflow_id: workflow === WORKFLOW ? 10 : 11,
      path: workflow,
      event,
      head_sha: this.ref,
      actor: { id: PRINCIPAL },
      triggering_actor: { id: PRINCIPAL },
      display_title: assignment ? `local work-queue ${assignment.dispatch_id}` : "local publisher",
      status: "in_progress",
      conclusion: null,
      created_at: "2026-10-08T00:00:00Z",
      ...(assignment ? { assignment: structuredClone(assignment) } : {}),
    };
  }

  nativeContext(runId = "100", attempt = 1) {
    const run = this.runs.get(String(runId));
    assert.ok(run, "unknown simulated native run");
    return {
      repo: { owner: "local", repo: "queue" },
      runId: run.id,
      runAttempt: attempt,
      actorId: PRINCIPAL,
      sha: this.ref,
      eventName: run.event,
      payload: { repository: { id: 7 } },
    };
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
      case "repos.getContent": {
        assert.equal(args.ref, this.ref, "workflow content must use real immutable revision");
        assert.ok([WORKFLOW, PUBLISHER_WORKFLOW].includes(args.path));
        const contents = await this.git(["show", `${args.ref}:${args.path}`]);
        return data({ type: "file", path: args.path, encoding: "base64", content: contents.toString("base64"), size: contents.length });
      }
      case "git.getRef":
        return data({ object: { sha: await this.reference(`refs/${args.ref}`) } });
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
          return data({ object: { sha: args.sha } });
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
      case "actions.getWorkflow":
        assert.ok([WORKFLOW, path.basename(WORKFLOW)].includes(args.workflow_id));
        return data({ id: 10, path: WORKFLOW, state: "active" });
      case "actions.getWorkflowRun":
      case "actions.getWorkflowRunAttempt": {
        const attemptRead = method.endsWith("Attempt");
        if (attemptRead && args.attempt_number !== 1) throw apiError(404, "only original simulated attempt exists");
        const run = (attemptRead ? this.originalRuns : this.runs).get(String(args.run_id));
        if (!run) throw apiError(404, "native run not found");
        return data(structuredClone(run));
      }
      case "actions.createWorkflowDispatch": {
        assert.equal(args.ref, this.ref);
        assert.equal(args.workflow_id, path.basename(WORKFLOW));
        assert.equal(args.headers["X-GitHub-Api-Version"], "2026-03-10");
        assert.equal(args.request.retries, 0);
        const assignment = JSON.parse(args.inputs.work_queue_assignment);
        const count = (this.launches.get(assignment.dispatch_id)?.count || 0) + 1;
        assert.equal(count, 1, "duplicate native POST for immutable assignment");
        const id = String(1000 + this.metrics.native_posts);
        const run = this.run(id, WORKFLOW, "workflow_dispatch", assignment);
        this.runs.set(id, run);
        this.originalRuns.set(id, structuredClone(run));
        this.launches.set(assignment.dispatch_id, { count, run_id: id, assignment });
        this.metrics.native_posts++;
        return data({ workflow_run_id: id, run_url: `https://api.github.com/repos/${REPOSITORY}/actions/runs/${id}`, html_url: `https://github.com/${REPOSITORY}/actions/runs/${id}` });
      }
      case "sim.nativeContext":
        return this.nativeContext(args.run_id);
      case "sim.lookupLaunch":
        return structuredClone(this.launches.get(args.dispatch_id) || null);
      case "sim.receipt": {
        const run = this.runs.get(args.run_id);
        assert.equal(run.run_attempt, 1);
        const member = run.assignment.claims.find(member => member.handle === args.handle);
        assert.equal(member?.work_id, args.work_id);
        assert.equal(run.assignment.dispatch_id, args.dispatch_id);
        const record = { ...args, effect: "none", descriptor: { local_noop: args.work_id } };
        const receipt = `local_receipt:${createHash("sha256").update(JSON.stringify(record)).digest("hex")}`;
        this.receipts.set(receipt, record);
        return { receipt, descriptor: record.descriptor };
      }
      case "sim.completeRun": {
        const run = this.runs.get(args.run_id);
        assert.ok(run?.assignment);
        for (const member of run.assignment.claims) assert.ok([...this.receipts.values()].some(record => record.run_id === run.id && record.work_id === member.work_id));
        run.status = "completed";
        run.conclusion = "success";
        this.originalRuns.get(args.run_id).status = "completed";
        this.originalRuns.get(args.run_id).conclusion = "success";
        return { completed: true };
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

module.exports = { LocalGitHub, clientFor, REPOSITORY, PRINCIPAL, WORKFLOW, PUBLISHER_WORKFLOW };
