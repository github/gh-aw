// @ts-check
"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { ESLINT_WORKERS } = require("./eslint_factory_portfolio.cjs");
const { replayTransactions } = require("./work_queue_replay.cjs");
const { REF, REPOSITORY } = require("./work_queue_lifecycle.test_helpers.cjs");

function compiledFactoryPolicyEnvironments(compiled) {
  return [...compiled.matchAll(/(?:^ +GH_AW_WORK_QUEUE_POLICY(?:_PARTS|_[0-9]+)?: "[^\n]+"\n)+/gm)].map(([block]) =>
    Object.fromEntries([...block.matchAll(/(GH_AW_WORK_QUEUE_POLICY(?:_PARTS|_[0-9]+)?): ("[^\n]+")/g)].map(([, name, value]) => [name, JSON.parse(value)]))
  );
}

function installFactoryNativeAPI(fake, fixture) {
  const runs = new Map();
  const tags = new Map();
  const tagCreations = [];
  const posts = [];
  const fileReads = [];
  fake.githubClient.rest.repos.get = async () => ({
    status: 200,
    data: { full_name: REPOSITORY, id: 7, default_branch: "main", size: 1, permissions: { push: true } },
  });
  const getRef = fake.githubClient.rest.git.getRef;
  fake.githubClient.rest.git.getRef = async parameters => {
    if (parameters.ref.startsWith("tags/")) {
      const sha = tags.get(parameters.ref);
      if (!sha) throw Object.assign(new Error("Not found"), { status: 404 });
      return { status: 200, data: { ref: `refs/${parameters.ref}`, object: { type: "commit", sha } } };
    }
    const response = await getRef(parameters);
    return { ...response, data: { ...response.data, ref: `refs/${parameters.ref}` } };
  };
  const createRef = fake.githubClient.rest.git.createRef;
  fake.githubClient.rest.git.createRef = async parameters => {
    if (!parameters.ref.startsWith("refs/tags/")) return createRef(parameters);
    assert.equal(parameters.ref, `refs/tags/gh-aw/work-queue/${parameters.sha}`);
    assert.ok([REF, fake.state.defaultRevision].includes(parameters.sha));
    tagCreations.push(parameters);
    tags.set(parameters.ref.slice("refs/".length), parameters.sha);
    return { status: 201, data: {} };
  };
  fake.githubClient.rest.users = {
    ...fixture.githubClient.rest.users,
    getByUsername: async ({ username }) => {
      assert.equal(username, "github-actions[bot]");
      return { status: 200, data: { id: "12", type: "Bot", login: username } };
    },
  };
  Object.assign(fake.githubClient, { auth: async () => ({ type: "token", token: "fixture-token" }) });
  const workerFiles = ESLINT_WORKERS.map(profile => `.github/workflows/${profile}.lock.yml`);
  fake.githubClient.rest.repos.getContent = async ({ path: filename, ref }) => {
    assert.ok([REF, fake.state.defaultRevision].includes(ref));
    assert.ok(workerFiles.includes(filename));
    fileReads.push(filename);
    const content = fs.readFileSync(path.resolve(__dirname, "../../..", filename));
    return { data: { type: "file", path: filename, encoding: "base64", content: content.toString("base64") } };
  };
  fake.githubClient.rest.actions.getWorkflow = async ({ workflow_id }) => {
    assert.ok(ESLINT_WORKERS.some(worker => workflow_id === `${worker}.lock.yml`));
    return { data: { path: `.github/workflows/${workflow_id}`, state: "active" } };
  };
  const getRun = async ({ run_id }) => {
    const run = String(run_id) === "15" ? fixture.nativeRun(fixture.dispatcherContext) : runs.get(String(run_id));
    assert.ok(run, "only fixture workflow runs may be read");
    return { status: 200, data: run };
  };
  fake.githubClient.rest.actions.getWorkflowRun = getRun;
  fake.githubClient.rest.actions.getWorkflowRunAttempt = getRun;
  fake.githubClient.rest.actions.createWorkflowDispatch = async parameters => {
    const assignment = JSON.parse(parameters.inputs.work_queue_assignment);
    const state = replayTransactions(fake.log());
    assert.equal(state.dispatches.get(assignment.dispatch_id).state, "started");
    const revision = tags.get(parameters.ref.slice("refs/".length));
    assert.ok(revision, "worker dispatch must use a verified fixture tag");
    assert.equal(assignment.claims.length, 1);
    assert.equal(parameters.workflow_id, `${assignment.worker_profile}.lock.yml`);
    const id = String(42 + posts.length);
    posts.push(parameters);
    runs.set(id, {
      ...fixture.nativeRun(fixture.workerContext),
      id,
      path: `.github/workflows/${parameters.workflow_id}`,
      head_sha: revision,
      display_title: `gh-aw work-queue ${assignment.dispatch_id}`,
    });
    return { status: 200, data: { workflow_run_id: id, run_url: `https://api.github.com/repos/${REPOSITORY}/actions/runs/${id}`, html_url: `https://github.com/${REPOSITORY}/actions/runs/${id}` } };
  };
  return { runs, tags, tagCreations, posts, fileReads, workerFiles };
}

module.exports = { compiledFactoryPolicyEnvironments, installFactoryNativeAPI };
