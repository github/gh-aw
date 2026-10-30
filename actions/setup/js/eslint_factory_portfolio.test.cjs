// @ts-check
import { describe, expect, it } from "vitest";
import fs from "node:fs";
import { randomUUID } from "node:crypto";
import path from "node:path";
import { ESLINT_WORKERS, buildESLintFactoryPlan, buildESLintFactoryPolicy } from "./eslint_factory_portfolio.cjs";
import { normalizeDispatchParameters, normalizeSubmitParameters, stagePlanIntents } from "./work_queue_intents.cjs";
import { newState, newRequest, generateRequestOperations, replayTransactions } from "./work_queue_replay.cjs";
import { frozenResourceScope } from "./work_queue_resource_scope.cjs";
import { validateDeliveryContract } from "./work_queue_delivery.cjs";
import { main as processControls } from "./work_queue_control_adapter.cjs";
import { main as snapshotQueue } from "./write_work_queue_snapshot.cjs";
import { fakeGitHub } from "./work_queue_store_checks.cjs";
import { queueFixture, DISPATCHER, REF, REPOSITORY } from "./work_queue_lifecycle.test_helpers.cjs";
import { readWorkQueuePolicyConfig } from "./work_queue_policy_config.cjs";
import { bindWorkerAssignment } from "./work_queue_binding.cjs";
import { authorizeWorkerClaim, reconcileWorkerClaim } from "./finish_work_queue_claim.cjs";
import { reconcileDispatch } from "./work_queue_reconciler.cjs";
import { compiledFactoryPolicyEnvironments, installFactoryNativeAPI } from "./eslint_factory_native.test_helpers.cjs";
import { checkFileProtection, checkFileProtectionPostApply } from "./manifest_file_helpers.cjs";

const repository = "owner/repo";
const planOptions = { date: "2026-10-08", repository, repositoryId: "7" };
const settings = JSON.parse(fs.readFileSync(new URL("../../../.github/workflows/aw.json", import.meta.url), "utf8")).work_queue;
const policyOptions = { repository, ref: "a".repeat(40), settings };

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

  it("allows the miner's rule README without bypassing other file protections", () => {
    const compiled = fs.readFileSync(new URL("../../../.github/workflows/eslint-miner.lock.yml", import.meta.url), "utf8");
    const configs = [...compiled.matchAll(/GH_AW_SAFE_OUTPUTS_(?:HANDLER_)?CONFIG: ("[^\n]+")/g)].map(([, value]) => JSON.parse(JSON.parse(value)).create_pull_request);
    expect(configs).toHaveLength(2);
    for (const config of configs) {
      expect(config.allowed_files).toEqual(["eslint-factory/**"]);
      expect(config.protected_files_policy).toBe("fallback-to-issue");
      expect(config.protected_files_path_exclude).toEqual(["eslint-factory/README.md", ".changeset/*.md"]);
      expect(config.protected_files).toContain("README.md");
      for (const [file, action] of [
        ["eslint-factory/README.md", "allow"],
        ["eslint-factory/src/rules/new-rule.ts", "allow"],
        ["eslint-factory/docs/README.md", "fallback"],
        ["eslint-factory/package.json", "fallback"],
        ["README.md", "deny"],
        [".github/workflows/ci.yml", "deny"],
      ]) {
        const patch = `diff --git a/${file} b/${file}\n`;
        expect(checkFileProtection(patch, config).action, file).toBe(action);
        expect(checkFileProtectionPostApply([file], config).action, file).toBe(action);
      }
    }
  });

  it.each(["2026-02-29", "2026-02-30", "2026-13-01", "1969-12-31", "2026-1-01", "tomorrow"])("rejects invalid dates: %s", date => {
    expect(() => buildESLintFactoryPlan({ ...planOptions, date })).toThrow(/date/);
  });

  it("requires canonical repository identity and immutable AW routes without enrollment", () => {
    for (const repositoryId of ["0", "007", "github-actions[bot]"]) expect(() => buildESLintFactoryPlan({ ...planOptions, repositoryId })).toThrow(/identity/);
    expect(() => buildESLintFactoryPlan({ ...planOptions, repository: "not-a-repo" })).toThrow();
    expect(() => buildESLintFactoryPolicy({ ...policyOptions, ref: "main" })).toThrow();
    const policy = buildESLintFactoryPolicy(policyOptions);
    expect(policy.authorization).toBe("aw");
    expect(policy.producers).toEqual({});
    expect(policy.pools.default).toMatchObject({ logical_limit: 3, native_limit: 3, retry: { max_attempts: 3, backoff_ms: 30000 } });
    for (const profile of ESLINT_WORKERS) {
      expect(policy.pools.default.profiles[profile]).toMatchObject({ ref: policyOptions.ref, max_claims: 1, share_keys: false });
      expect(policy.pools.default.profiles[profile]).not.toHaveProperty("principal");
    }
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

  it("admits another trusted AW identity without producer enrollment", () => {
    const queue = ledger();
    const plan = buildESLintFactoryPlan(planOptions);
    queue.append("submit", normalizeSubmitParameters({ nodes: plan.nodes }, queue.policy, 2000, queue.state()), { role: "producer", repository, principal: "13" });
    expect(queue.state().works.size).toBe(3);
  });

  it("wires trusted plan preparation, submission before dispatch, and immutable run dates", () => {
    const source = fs.readFileSync(new URL("../../../.github/workflows/eslint-factory-dispatcher.md", import.meta.url), "utf8");
    expect(source).toContain("actions: read");
    expect(source).toContain("buildESLintFactoryPlan");
    expect(source).toContain("run.created_at");
    expect(source).toContain("cat /tmp/gh-aw/agent/eslint-factory-plan.json");
    expect(source).toContain("stagePlanIntents(");
    expect(source).toContain("shared/work-queue-staged-plan.md");
    expect(source).toContain("will atomically bootstrap the branch with its compiler-approved Policy and Work.");
    expect(source).toContain("Safe-output processing uses only the compiler-approved Policy proposal; never");
    expect(source).toContain("no administrator seed is required.");
    expect(source).not.toContain("producers:");
    expect(source).not.toContain("work-queue-policy:");
    expect(source).not.toMatch(/branch protection|queue-branch protections/i);
  });

  it("embeds the complete factory bootstrap Policy in activation and trusted control processing", () => {
    const source = fs.readFileSync(new URL("../../../.github/workflows/eslint-factory-dispatcher.md", import.meta.url), "utf8");
    const compiled = fs.readFileSync(new URL("../../../.github/workflows/eslint-factory-dispatcher.lock.yml", import.meta.url), "utf8");
    const environments = compiledFactoryPolicyEnvironments(compiled);
    const proposals = environments.map(environment => JSON.parse(readWorkQueuePolicyConfig(environment).replaceAll("${{ github.repository }}", "github/gh-aw").replaceAll("${{ github.sha }}", policyOptions.ref)));
    expect(proposals.length).toBeGreaterThanOrEqual(2);
    for (const environment of environments) {
      expect(Object.values(environment).every(value => Buffer.byteLength(value) < 21 * 1024)).toBe(true);
    }
    for (const proposal of proposals) {
      expect(proposal).toEqual(proposals[0]);
      expect(proposal.authorization).toBe("aw");
      expect(proposal.producers).toEqual({});
      expect(proposal.limits.pending_nodes).toBe(settings.pending_limit);
      expect(proposal.pools.default.logical_limit).toBe(settings.concurrency);
      for (const worker of ESLINT_WORKERS) {
        expect(proposal.pools.default.profiles[worker]).toMatchObject({ workflow: `.github/workflows/${worker}.lock.yml`, ref: policyOptions.ref, max_claims: 1 });
        expect(proposal.pools.default.profiles[worker]).not.toHaveProperty("principal");
        expect(proposal.pools.default.profiles[worker].logical_contract).toMatch(/^[a-f0-9]{64}$/);
      }
    }
    expect(source).toContain("github-token: ${{ secrets.GITHUB_TOKEN }}");
    expect(source).not.toMatch(/secrets\.(?!GITHUB_TOKEN\b)/);
    const configs = [...compiled.matchAll(/(?:GH_AW_WORK_QUEUE_CONTROL_CONFIG|GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG): ("[^\n]+")/g)].map(([, value]) => {
      const config = JSON.parse(JSON.parse(value));
      return config.dispatch_workflow || config;
    });
    expect(configs.length).toBeGreaterThan(1);
    for (const config of configs) {
      expect(config["github-token"]).toBe("${{ secrets.GITHUB_TOKEN }}");
      expect(config.work_queue_dispatch_credential).toEqual({ kind: "github_token" });
      expect([...config.work_queue_workflows].sort()).toEqual([...ESLINT_WORKERS].sort());
    }
    const controls = compiled.match(/      - name: Process trusted work queue controls\n([\s\S]*?)(?=      - name:)/);
    expect(controls).not.toBeNull();
    expect(controls[1]).toContain("github-token: ${{ secrets.GITHUB_TOKEN }}");
    expect(controls[1]).not.toContain("secrets.GH_AW_GITHUB_TOKEN");
  });

  it.each(["completed", "cancelled"])("bootstraps, launches, executes and finishes all three workers (%s) without duplicate launches", async outcome => {
    const directory = path.resolve(`.gh-aw-eslint-dispatch-${randomUUID()}`);
    fs.mkdirSync(directory);
    const fixture = queueFixture({ granted: false, workerPrincipal: "12" });
    const fake = fakeGitHub();
    const { runs, tagCreations, posts, fileReads, workerFiles } = installFactoryNativeAPI(fake, fixture);
    const failures = [];
    const compiled = fs.readFileSync(new URL("../../../.github/workflows/eslint-factory-dispatcher.lock.yml", import.meta.url), "utf8");
    const configMatch = compiled.match(/GH_AW_WORK_QUEUE_CONTROL_CONFIG: ("[^\n]+")/);
    expect(configMatch).not.toBeNull();
    const dispatchConfig = JSON.parse(JSON.parse(configMatch[1]));
    const [environment] = compiledFactoryPolicyEnvironments(compiled);
    expect(environment).toBeDefined();
    const policy = JSON.parse(readWorkQueuePolicyConfig(environment).replaceAll("${{ github.repository }}", REPOSITORY).replaceAll("${{ github.sha }}", REF));
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
      stagePlanIntents(intentPath, plan);
      const controls = {
        ...options,
        intentOrigin: snapshot.origin,
        intentPath,
        getOctokit: token => {
          expect(token).toBe("fixture-token");
          return fake.githubClient;
        },
        config: {
          ...dispatchConfig,
          "github-token": "fixture-token",
        },
      };
      const result = await processControls(controls);
      expect(result.success, JSON.stringify({ result, failures })).toBe(true);
      expect(result.receipts.map(receipt => receipt.status)).toEqual(["durable", "durable"]);
      expect(posts).toHaveLength(3);
      expect(tagCreations).toHaveLength(1);
      expect(new Set(posts.map(post => post.workflow_id))).toEqual(new Set(ESLINT_WORKERS.map(profile => `${profile}.lock.yml`)));
      const state = replayTransactions(fake.log());
      expect(new Set(fileReads)).toEqual(new Set(workerFiles));
      expect(fake.log()[0].operations.map(operation => operation.kind)).toEqual(["Policy", "Work", "Work", "Work"]);
      expect(state.policy).toEqual(policy);
      expect(state.works.size).toBe(3);
      expect(state.claims.size).toBe(3);
      expect([...state.dispatches.values()].every(dispatch => dispatch.state === "bound")).toBe(true);
      const readsBeforeReplay = [...fileReads];
      expect((await processControls(controls)).success).toBe(true);
      expect(fileReads).toEqual(readsBeforeReplay);
      expect(posts).toHaveLength(3);
      expect(tagCreations).toHaveLength(1);
      expect(replayTransactions(fake.log()).works.size).toBe(3);
      for (const [index, post] of posts.entries()) {
        const assignment = JSON.parse(post.inputs.work_queue_assignment);
        const member = assignment.claims[0];
        const runId = String(42 + index);
        const workerContext = {
          ...fixture.workerContext,
          runId,
          payload: { repository: { id: 7 }, inputs: post.inputs },
        };
        const finishIntentPath = path.join(directory, `${assignment.worker_profile}.finish.jsonl`);
        const worker = {
          ...options,
          context: workerContext,
          workflowRef: `${REPOSITORY}/.github/workflows/${assignment.worker_profile}.lock.yml@${REF}`,
          assignment,
          logicalContract: policy.pools.default.profiles[assignment.worker_profile].logical_contract,
          role: "worker",
          requireAssignment: true,
          finishIntentPath,
        };
        expect((await bindWorkerAssignment(worker)).binding.run_id).toBe(runId);
        expect(await authorizeWorkerClaim({ ...worker, claim_handle: member.handle, requireCompletion: false })).toMatchObject({ authorized: true, state: "open", work_id: member.work_id });
        expect(await authorizeWorkerClaim({ ...worker, claim_handle: member.handle })).toMatchObject({ authorized: false, state: "open" });
        fs.writeFileSync(finishIntentPath, JSON.stringify({ version: 3, kind: "finish", intent_id: `factory-finish:${assignment.worker_profile}`, claim_handle: member.handle, parameters: { outcome } }) + "\n");
        const finished = await reconcileWorkerClaim(worker);
        expect(finished.claims[member.handle]).toMatchObject({ state: outcome, authorized: outcome === "completed" });
        expect(await authorizeWorkerClaim({ ...worker, claim_handle: member.handle })).toMatchObject({ authorized: outcome === "completed", state: outcome });
        const updates = fake.state.updates;
        expect((await reconcileWorkerClaim(worker)).claims[member.handle].state).toBe(outcome);
        expect(fake.state.updates).toBe(updates);
        expect(posts).toHaveLength(3);
        const work = replayTransactions(fake.log()).works.get(member.work_id);
        expect(work.barrier).toBe(outcome === "completed" ? "pending" : "none");
        expect(
          fake
            .log()
            .flatMap(transaction => transaction.operations)
            .some(operation => operation.kind === "Result")
        ).toBe(false);
        runs.set(runId, { ...runs.get(runId), status: "completed", conclusion: "success" });
        expect(await reconcileDispatch({ ...options, assignment })).toMatchObject({ released: true });
      }
      expect([...replayTransactions(fake.log()).dispatches.values()].every(dispatch => dispatch.released)).toBe(true);
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
    const intents = [];
    const fakeRequire = name => {
      if (name === "node:fs")
        return {
          mkdirSync: directory => expect(directory).toBe("/tmp/gh-aw/agent"),
          writeFileSync: (filename, content) => files.set(filename, content),
        };
      if (name === "node:path") return { join: (...parts) => parts.join("/") };
      if (name === "/fixture/gh-aw/actions/eslint_factory_portfolio.cjs") return { buildESLintFactoryPlan };
      if (name === "/fixture/gh-aw/actions/work_queue_intents.cjs")
        return {
          stagePlanIntents: (filename, plan) => {
            expect(filename).toBe("/fixture/gh-aw/safeoutputs/work-queue/work-queue.intents.jsonl");
            intents.push(plan);
          },
        };
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
    expect(intents).toEqual([buildESLintFactoryPlan(planOptions)]);
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
