// @ts-check
import { describe, expect, it } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { ESLINT_WORKERS, buildESLintFactoryPlan, buildESLintFactoryPolicy } from "./eslint_factory_portfolio.cjs";
import { normalizeDispatchParameters, normalizeSubmitParameters } from "./work_queue_intents.cjs";
import { newState, newRequest, generateRequestOperations, replayTransactions } from "./work_queue_replay.cjs";
import { frozenResourceScope } from "./work_queue_resource_scope.cjs";
import { validateDeliveryContract } from "./work_queue_delivery.cjs";
import { main as processControls } from "./work_queue_control_adapter.cjs";
import { main as snapshotQueue } from "./write_work_queue_snapshot.cjs";
import { fakeGitHub } from "./work_queue_store_checks.cjs";
import { queueFixture, DISPATCHER, REF, REPOSITORY } from "./work_queue_lifecycle.test_helpers.cjs";

const repository = "owner/repo";
const planOptions = { date: "2026-10-08", repository, repositoryId: "7" };
const policyOptions = { repository, ref: "a".repeat(40), producerPrincipal: "11", workerPrincipal: "12" };

function ledger() {
  let state = newState();
  const transactions = [];
  let at = 1000;
  const append = (kind, parameters, actor) => {
    const request = newRequest(`factory:${transactions.length}:${++at}`, kind, actor, parameters);
    const id = `commit:${at}`;
    const decision = generateRequestOperations(state, request, actor, at, id);
    if (decision.operations.length) {
      transactions.push({ version: 3, id, previous: state.tip || null, request, actor, policy_epoch: "factory-v1", at, operations: decision.operations });
      state = replayTransactions(transactions);
    }
    return state;
  };
  const policy = buildESLintFactoryPolicy(policyOptions);
  append("policy", { operations: [{ kind: "Policy", epoch: "factory-v1", policy }] }, { role: "administrator", repository, principal: "11" });
  return { append, policy, state: () => state };
}

describe("ESLint factory producer and dispatcher", () => {
  it("prepares three independent date-keyed tasks with actual worker contracts", () => {
    const plan = buildESLintFactoryPlan(planOptions);
    expect(buildESLintFactoryPlan(planOptions)).toEqual(plan);
    expect(plan.nodes.map(node => node.worker_profile)).toEqual(ESLINT_WORKERS);
    expect(plan.dispatch).toEqual({ pool: "default", max_claims: 3, max_dispatches: 3 });
    expect(Buffer.byteLength(JSON.stringify(plan))).toBeLessThan(8192);
    for (const node of plan.nodes) {
      expect(node).toMatchObject({ graph_id: "eslint-factory-cohort:2026-10-08", node_key: node.worker_profile, priority: 3, fairness_key: "", pool: "default", depends_on: [] });
      expect(frozenResourceScope(node.payload)).toEqual({ version: 1, resources: [{ host: "github.com", repository, repository_id: "7" }] });
      expect(validateDeliveryContract(node.payload.effect_contract)).toEqual(node.payload.effect_contract);
    }
    expect(plan.nodes[0].payload.effect_contract.outputs).toContainEqual({ type: "create_pull_request", min: 0, max: 1 });
    expect(plan.nodes.every(node => node.payload.plan.includes("cancel the Claim"))).toBe(true);
    expect(plan.nodes[1].payload.effect_contract.outputs).toContainEqual({ type: "persist_eslint_memory", min: 1, max: 1 });
    expect(plan.nodes[2].payload.effect_contract.outputs).toContainEqual({ type: "assign_to_agent", min: 0, max: 3 });
  });

  it.each(["2026-02-29", "2026-02-30", "2026-13-01", "1969-12-31", "2026-1-01", "tomorrow"])("rejects invalid dates: %s", date => {
    expect(() => buildESLintFactoryPlan({ ...planOptions, date })).toThrow(/date/);
  });

  it("requires canonical repository identity, immutable profiles and explicit producer entitlement", () => {
    for (const repositoryId of ["0", "007", "github-actions[bot]"]) expect(() => buildESLintFactoryPlan({ ...planOptions, repositoryId })).toThrow(/identity/);
    expect(() => buildESLintFactoryPlan({ ...planOptions, repository: "not-a-repo" })).toThrow();
    expect(() => buildESLintFactoryPolicy({ ...policyOptions, ref: "main" })).toThrow();
    expect(() => buildESLintFactoryPolicy({ ...policyOptions, producerPrincipal: "0" })).toThrow();
    expect(() => buildESLintFactoryPolicy({ ...policyOptions, workerPrincipal: "github-actions[bot]" })).toThrow();
    const policy = buildESLintFactoryPolicy(policyOptions);
    expect(policy.producers["11"]).toEqual({ pools: ["default"], priorities: [3], fairness_keys: [""] });
    expect(policy.pools.default).toMatchObject({ logical_limit: 3, native_limit: 3, retry: { max_attempts: 1 } });
    for (const profile of ESLINT_WORKERS) expect(policy.pools.default.profiles[profile]).toMatchObject({ ref: policyOptions.ref, principal: "12", max_claims: 1, share_keys: false });
  });

  it("admits three tasks once per UTC day, then grants three singleton worker assignments", () => {
    const queue = ledger();
    const plan = buildESLintFactoryPlan(planOptions);
    const producer = { role: "producer", repository, principal: "11" };
    const dispatcher = { role: "dispatcher", repository, principal: "11", workflow: ".github/workflows/eslint-factory-dispatcher.lock.yml", run_id: "15", run_attempt: 1 };
    queue.append("submit", normalizeSubmitParameters({ nodes: plan.nodes }, queue.policy, 2000, queue.state()), producer);
    const original = [...queue.state().works.values()];
    queue.append("submit", normalizeSubmitParameters({ nodes: plan.nodes }, queue.policy, 3000, queue.state()), producer);
    expect([...queue.state().works.values()]).toEqual(original);
    expect(queue.state().works.size).toBe(3);
    queue.append("dispatch_next", normalizeDispatchParameters(plan.dispatch, queue.policy, 3), dispatcher);
    expect(queue.state().claims.size).toBe(3);
    expect(queue.state().dispatches.size).toBe(3);
    for (const dispatch of queue.state().dispatches.values()) expect(dispatch.claims).toHaveLength(1);
    queue.append("submit", normalizeSubmitParameters({ nodes: plan.nodes }, queue.policy, 4000, queue.state()), producer);
    queue.append("dispatch_next", normalizeDispatchParameters(plan.dispatch, queue.policy, 3), dispatcher);
    expect(queue.state().claims.size).toBe(3);
    const tomorrow = buildESLintFactoryPlan({ ...planOptions, date: "2026-10-09" });
    queue.append("submit", normalizeSubmitParameters({ nodes: tomorrow.nodes }, queue.policy, 5000, queue.state()), producer);
    expect(queue.state().works.size).toBe(6);
    queue.append("dispatch_next", normalizeDispatchParameters(tomorrow.dispatch, queue.policy, 3), { ...dispatcher, run_id: "16" });
    expect(queue.state().claims.size).toBe(3);
  });

  it("rejects admission by an identity without producer entitlement", () => {
    const queue = ledger();
    const plan = buildESLintFactoryPlan(planOptions);
    expect(() => queue.append("submit", normalizeSubmitParameters({ nodes: plan.nodes }, queue.policy, 2000, queue.state()), { role: "producer", repository, principal: "13" })).toThrow(/entitle|producer/);
    expect(queue.state().works.size).toBe(0);
  });

  it("wires trusted plan preparation, submission before dispatch, and immutable run dates", () => {
    const source = fs.readFileSync(new URL("../../../.github/workflows/eslint-factory-dispatcher.md", import.meta.url), "utf8");
    expect(source).toContain("actions: read");
    expect(source).toContain("buildESLintFactoryPlan");
    expect(source).toContain("run.created_at");
    expect(source).toContain("cat /tmp/gh-aw/agent/eslint-factory-plan.json");
    expect(source.indexOf("Call `work_queue_submit` once")).toBeLessThan(source.indexOf("request the trusted scheduler"));
    expect(source).toContain("will atomically bootstrap the branch with its compiler-approved Policy and Work.");
    expect(source).toContain("Safe-output processing uses only the compiler-approved Policy proposal; never");
    expect(source).toContain("no administrator seed is required.");
    expect(source).toContain("Agentic Workflows authenticates participants at the trusted boundary.");
    expect(source).not.toMatch(/branch protection|queue-branch protections/i);
  });

  it("embeds the complete factory bootstrap Policy in activation and trusted control processing", () => {
    const source = fs.readFileSync(new URL("../../../.github/workflows/eslint-factory-dispatcher.md", import.meta.url), "utf8");
    const compiled = fs.readFileSync(new URL("../../../.github/workflows/eslint-factory-dispatcher.lock.yml", import.meta.url), "utf8");
    const proposals = [...compiled.matchAll(/GH_AW_WORK_QUEUE_POLICY: ("[^\n]+")/g)].map(match => JSON.parse(JSON.parse(match[1]).replaceAll("${{ github.repository }}", "github/gh-aw")));
    expect(proposals.length).toBeGreaterThanOrEqual(2);
    const expected = buildESLintFactoryPolicy({
      repository: "github/gh-aw",
      ref: "6976a375a288e2ac4ca5102fd54bb9adafcf2f5e",
      producerPrincipal: "4175913",
      workerPrincipal: "4175913",
    });
    for (const proposal of proposals) expect(proposal).toEqual(expected);
    expect(source).toContain("github-token: ${{ secrets.GH_AW_GITHUB_TOKEN }}");
    const configMatch = compiled.match(/GH_AW_WORK_QUEUE_CONTROL_CONFIG: ("[^\n]+")/);
    expect(configMatch).not.toBeNull();
    const config = JSON.parse(JSON.parse(configMatch[1]));
    expect(config["github-token"]).toBe("${{ secrets.GH_AW_GITHUB_TOKEN }}");
    expect(config.work_queue_dispatch_credential).toEqual({ kind: "authenticated" });
    expect(config.work_queue_workflows).toEqual([...ESLINT_WORKERS].sort());
  });

  it("bootstraps an absent queue and launches all three workers through the real trusted control adapter without duplicate launches", async () => {
    const directory = fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()), "gh-aw-eslint-dispatch-"));
    const fixture = queueFixture({ granted: false, workerPrincipal: policyOptions.workerPrincipal });
    const fake = fakeGitHub();
    const runs = new Map();
    const posts = [];
    const failures = [];
    const policy = buildESLintFactoryPolicy(policyOptions);
    fake.githubClient.rest.repos.get = async () => ({
      status: 200,
      data: { full_name: REPOSITORY, id: 7, default_branch: "main", size: 1 },
    });
    fake.githubClient.rest.users = fixture.githubClient.rest.users;
    fake.githubClient.rest.repos.getContent = async ({ path: filename, ref }) => {
      expect(ref).toBe(REF);
      expect(ESLINT_WORKERS.map(profile => `.github/workflows/${profile}.lock.yml`)).toContain(filename);
      const content = fs.readFileSync(new URL(`../../../${filename}`, import.meta.url));
      return { data: { type: "file", path: filename, encoding: "base64", content: content.toString("base64") } };
    };
    fake.githubClient.rest.actions.getWorkflow = async ({ workflow_id }) => ({ data: { path: `.github/workflows/${workflow_id}`, state: "active" } });
    fake.githubClient.rest.actions.getWorkflowRun = async ({ run_id }) => ({ status: 200, data: String(run_id) === "15" ? fixture.nativeRun(fixture.dispatcherContext) : runs.get(String(run_id)) });
    fake.githubClient.rest.actions.getWorkflowRunAttempt = async ({ run_id }) => fake.githubClient.rest.actions.getWorkflowRun({ run_id });
    fake.githubClient.rest.actions.createWorkflowDispatch = async parameters => {
      const assignment = JSON.parse(parameters.inputs.work_queue_assignment);
      const state = replayTransactions(fake.log());
      expect(state.dispatches.get(assignment.dispatch_id).state).toBe("started");
      expect(parameters.ref).toBe(REF);
      expect(assignment.claims).toHaveLength(1);
      expect(parameters.workflow_id).toBe(`${assignment.worker_profile}.lock.yml`);
      const id = String(42 + posts.length);
      posts.push(parameters);
      runs.set(id, {
        ...fixture.nativeRun(fixture.workerContext),
        id,
        path: `.github/workflows/${parameters.workflow_id}`,
        display_title: `gh-aw work-queue ${assignment.dispatch_id}`,
      });
      return { status: 200, data: { workflow_run_id: id, run_url: `https://api.github.com/repos/${REPOSITORY}/actions/runs/${id}`, html_url: `https://github.com/${REPOSITORY}/actions/runs/${id}` } };
    };
    const core = { setOutput: () => {}, info: () => {}, setFailed: message => failures.push(message) };
    const options = {
      githubClient: fake.githubClient,
      context: fixture.dispatcherContext,
      workflowRef: `${REPOSITORY}/${DISPATCHER}@${REF}`,
      role: "dispatcher",
      policyProposal: policy,
      core,
      env: {},
      sleepFn: async () => {},
    };
    try {
      const snapshot = await snapshotQueue({ ...options, snapshotPath: path.join(directory, "snapshot.json") });
      expect(snapshot.sha).toBeNull();
      expect(fake.refs.has("work-queue")).toBe(false);
      const plan = buildESLintFactoryPlan(planOptions);
      const intentPath = path.join(directory, "intents.jsonl");
      fs.writeFileSync(
        intentPath,
        [
          { version: 3, intent_id: "factory-submit", kind: "submit", parameters: { nodes: plan.nodes } },
          { version: 3, intent_id: "factory-dispatch", kind: "dispatch_next", parameters: plan.dispatch },
        ]
          .map(intent => JSON.stringify(intent))
          .join("\n") + "\n"
      );
      const controls = {
        ...options,
        intentOrigin: snapshot.origin,
        intentPath,
        getOctokit: token => {
          expect(token).toBe("fixture-token");
          return fake.githubClient;
        },
        config: { max: 3, work_queue_enabled: true, work_queue_workflows: ESLINT_WORKERS, aw_context_workflows: ESLINT_WORKERS, "github-token": "fixture-token", work_queue_dispatch_credential: { kind: "authenticated" } },
      };
      const result = await processControls(controls);
      expect(result.success, JSON.stringify(result)).toBe(true);
      expect(result.receipts.map(receipt => receipt.status)).toEqual(["durable", "durable"]);
      expect(posts).toHaveLength(3);
      expect(new Set(posts.map(post => post.workflow_id))).toEqual(new Set(ESLINT_WORKERS.map(profile => `${profile}.lock.yml`)));
      const state = replayTransactions(fake.log());
      expect(fake.log()[0].operations.map(operation => operation.kind)).toEqual(["Policy", "Work", "Work", "Work"]);
      expect(state.works.size).toBe(3);
      expect(state.claims.size).toBe(3);
      expect([...state.dispatches.values()].every(dispatch => dispatch.state === "bound")).toBe(true);
      expect((await processControls(controls)).success).toBe(true);
      expect(posts).toHaveLength(3);
      expect(replayTransactions(fake.log()).works.size).toBe(3);
      expect(failures).toEqual([]);
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });

  it("executes the actual preparation step with only two metadata reads and no ambient filesystem or credentials", async () => {
    const source = fs.readFileSync(new URL("../../../.github/workflows/eslint-factory-dispatcher.md", import.meta.url), "utf8");
    const block = source.match(/      script: \|\n((?:        .*\n)+)/);
    expect(block).not.toBeNull();
    const script = block[1].replace(/^        /gm, "");
    const AsyncFunction = Object.getPrototypeOf(async function () {}).constructor;
    const calls = [];
    const files = new Map();
    const fakeRequire = name => {
      if (name === "node:fs")
        return {
          mkdirSync: directory => expect(directory).toBe("/tmp/gh-aw/agent"),
          writeFileSync: (filename, content) => files.set(filename, content),
        };
      if (name === "node:path") return { join: (...parts) => parts.join("/") };
      if (name === "/fixture/gh-aw/actions/eslint_factory_portfolio.cjs") return { buildESLintFactoryPlan };
      throw new Error(`Unreviewed preparation dependency: ${name}`);
    };
    const github = {
      rest: {
        actions: {
          getWorkflowRun: async parameters => {
            expect(parameters).toEqual({ owner: "owner", repo: "repo", run_id: 15 });
            calls.push("run");
            return { data: { created_at: "2026-10-08T23:59:59Z" } };
          },
        },
        repos: {
          get: async parameters => {
            expect(parameters).toEqual({ owner: "owner", repo: "repo" });
            calls.push("repository");
            return { data: { full_name: repository, id: 7 } };
          },
        },
      },
    };
    await new AsyncFunction("require", "github", "context", "process", "core", script)(fakeRequire, github, { repo: { owner: "owner", repo: "repo" }, runId: 15 }, { env: { RUNNER_TEMP: "/fixture" } }, { info: () => {} });
    expect(calls).toEqual(["run", "repository"]);
    expect(files.size).toBe(1);
    expect(JSON.parse(files.get("/tmp/gh-aw/agent/eslint-factory-plan.json"))).toEqual(buildESLintFactoryPlan(planOptions));
  });

  it("keeps clean scans and absent rule ideas from claiming write-capable no-write Results", () => {
    for (const name of ["eslint-miner", "eslint-monster"]) {
      const source = fs.readFileSync(new URL(`../../../.github/workflows/${name}.md`, import.meta.url), "utf8");
      expect(source).toContain('`kind: "none"` or `no_writes: true`');
      expect(source).toContain("cancel");
      expect(source).toContain("cannot settle Result from `noop` alone");
    }
  });
});
