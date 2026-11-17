"use strict";

const assert = require("node:assert/strict");
const crypto = require("node:crypto");
const fs = require("node:fs");
const path = require("node:path");
const { test } = require("node:test");
const { isPiBashAllowed } = require("../actions/setup/js/pi_tool_policy.cjs");
const { extractInlineSubAgents } = require("../actions/setup/js/extract_inline_sub_agents.cjs");
const { extractFrontmatterAndBody } = require("../actions/setup/js/frontmatter_hash_pure.cjs");

const workflow = fs.readFileSync(path.join(__dirname, "..", ".github", "workflows", "dependabot-burner.md"), "utf8");
const script = workflow.match(/      script: \|\n([\s\S]*?)\nevals:/);
assert.ok(script, "Missing burner prefetch script");
const AsyncFunction = Object.getPrototypeOf(async function () {}).constructor;
const prefetch = new AsyncFunction("github", "context", "require", "process", "console", script[1].replace(/^        /gm, ""));

function compiledBashPolicy() {
  const lock = fs.readFileSync(path.join(__dirname, "..", ".github", "workflows", "dependabot-burner.lock.yml"), "utf8");
  const toolPolicy = lock.match(/GH_AW_PI_TOOL_POLICY: ('[^'\n]+'|"(?:\\.|[^"\\])*")/);
  assert.ok(toolPolicy, "Missing compiled Pi tool policy");
  const policyJSON = toolPolicy[1].startsWith('"') ? JSON.parse(toolPolicy[1]) : toolPolicy[1].slice(1, -1);
  const { bash } = JSON.parse(policyJSON);
  assert.ok(Array.isArray(bash) && bash.includes("*"), "Missing wildcard Bash policy");
  return command => isPiBashAllowed(command, bash);
}

test("burner tool-contract commands are executable under the compiled Pi policy", () => {
  const contract = workflow.match(/## Tool contract for every agent\n([\s\S]*?)\n## Read first/);
  assert.ok(contract, "Missing shared tool contract for the orchestrator and subagents");
  const allowed = compiledBashPolicy();
  const { agents } = extractInlineSubAgents(workflow);
  const contracts = [contract[1], ...agents.map(agent => agent.content.match(/#{2,3} Tool contract\n([\s\S]*?)(?=\n## |Return compact JSON|$)/)?.[1] || "")];
  const commands = contracts.flatMap(content => [...content.matchAll(/`((?:cat|git|safeoutputs) [^`]+)`/g)].map(match => match[1]));
  assert.ok(commands.length > 0, "Missing executable command examples");
  for (const command of commands) {
    assert.ok(allowed(command), command);
  }
});

test("runtime extraction keeps the complete remediator contract and bounded prompts", t => {
  const { withCompilerFixtures } = require("./markdown_metadata_test_adapter.cjs");
  const { writeInlineSubAgents, loadCopilotSDKCustomAgents } = withCompilerFixtures(require("../actions/setup/js/extract_inline_sub_agents.cjs"));
  const directory = fs.mkdtempSync(path.join(__dirname, ".burner-agents-"));
  const previousCore = global.core;
  global.core = { info() {} };
  t.after(() => {
    global.core = previousCore;
    fs.rmSync(directory, { recursive: true, force: true });
  });
  const { markdown } = extractFrontmatterAndBody(workflow);
  const mainContent = writeInlineSubAgents(markdown, directory, directory, "copilot");
  const agents = loadCopilotSDKCustomAgents(directory, directory, message => assert.fail(message));
  assert.deepEqual(
    agents.map(agent => agent.name),
    ["dependabot-remediator"]
  );
  assert.ok(Buffer.byteLength(mainContent) <= 7500, "Orchestrator contract exceeds the instruction-prefix budget");
  const remediator = agents.find(agent => agent.name === "dependabot-remediator");
  assert.ok(mainContent.includes("Never invoke GitHub tools through Bash"), "Orchestrator is missing GitHub tool guidance");
  assert.ok(remediator.prompt.includes("You have no GitHub MCP tools"), "Remediator must not assume GitHub tools are available");
  for (const section of ["Tool contract", "Deterministic result", "Required approach", "Required validation", "Pull request rule", "Output"]) {
    assert.ok(remediator.prompt.includes(`## ${section}`), `Runtime discarded ${section}`);
    assert.ok(!mainContent.includes(`## ${section}\n`), `${section} leaked out of the remediator`);
  }
  assert.ok(remediator.prompt.includes("unaddressed_prs"), "Blocked PR reporting must reach the remediator");
  for (const agent of agents) {
    assert.ok(Buffer.byteLength(agent.prompt) <= 7500, `${agent.name} exceeds the instruction-prefix budget`);
    if (agent.tools.includes("bash")) {
      assert.ok(agent.prompt.includes("You have no GitHub MCP tools") || agent.prompt.includes("GitHub tools are not shell commands"), `${agent.name} is missing GitHub tool guidance`);
      assert.ok(agent.prompt.includes("After the first permission denial, stop shell calls"), `${agent.name} is missing a denial stop condition`);
    }
  }
  assert.deepEqual(remediator.tools, ["read", "ls", "edit", "write", "bash"]);
  assert.ok(mainContent.includes("Do not launch a separate dependency analysis or feedback synthesis task"), "First attempts must not require retry synthesis");
  assert.ok(mainContent.includes("do not search the filesystem for history or relaunch the analyzer"));
});

test("safe outputs are executable while the prompt keeps GitHub reads off the shell", () => {
  const allowed = compiledBashPolicy();
  assert.ok(allowed(`safeoutputs noop '{"message":"No Dependabot updates (0 PRs) #none"}'`));
  assert.match(workflow, /Never invoke GitHub tools through Bash|GitHub tools are not shell commands: never\s+invoke them through Bash/);
});

test("wildcard Bash supports grouped validation and local inspection", () => {
  const allowed = compiledBashPolicy();
  for (const command of [
    "git status --short && git diff --stat",
    "make dependabot && make build",
    "npm --prefix .github/workflows install --package-lock-only --ignore-scripts",
    "git -C docs diff --stat",
    "find docs -name package.json -print | sort",
  ]) {
    assert.ok(allowed(command), command);
  }
});

test("one remediation worker replaces analysis fan-out and the parent owns publication", () => {
  const agents = [...workflow.matchAll(/^## agent: `([^`]+)`/gm)].map(match => match[1]);
  assert.deepEqual(agents, ["dependabot-remediator"]);
  assert.match(workflow, /complete Tool contract for every agent above, copied verbatim/);
  assert.match(workflow, /The remediator must not delegate further/);
  assert.match(workflow, /orchestrator owns the terminal safe output/);
  assert.match(workflow, /report incomplete work for a blocked or failed wave/);
  assert.match(workflow, /Leave `replacement_pr_created` false/);
  assert.match(workflow, /Do not submit create-pull-request or noop safe outputs from this worker/);
  assert.match(workflow, /npm --prefix \.github\/workflows install --package-lock-only --ignore-scripts/);
  assert.doesNotMatch(workflow, /npm install --package-lock-only(?:["`]|\n)/);
});

function pull(number, files, author = "dependabot[bot]") {
  return {
    number,
    title: `Bump dependency-${number} from 1.0.0 to 2.0.0`,
    user: { login: author },
    state: "open",
    created_at: `2026-11-${String(number).padStart(2, "0")}T00:00:00Z`,
    html_url: `https://github.com/example/repo/pull/${number}`,
    files,
  };
}

async function select(pulls, trigger = null, fallback = null) {
  let payload;
  const list = Symbol("pulls.list");
  const listFiles = Symbol("pulls.listFiles");
  const github = {
    rest: {
      pulls: {
        list,
        listFiles,
        get: async ({ pull_number }) => {
          const pr = pulls.find(item => item.number === pull_number) || fallback;
          assert.equal(pull_number, pr?.number);
          return { data: pr };
        },
      },
    },
    paginate: async (method, options) => {
      assert.equal(options.per_page, 100);
      if (method === list) return options.state === "closed" ? [] : pulls;
      assert.equal(method, listFiles);
      const pr = pulls.find(item => item.number === options.pull_number) || fallback;
      assert.ok(pr, "Unexpected PR file request");
      return pr.files.map(filename => ({ filename }));
    },
  };
  await prefetch(
    github,
    { repo: { owner: "example", repo: "repo" }, eventName: trigger ? "issue_comment" : "schedule", payload: trigger ? { issue: { number: trigger, pull_request: {} } } : {} },
    name => {
      if (name === "crypto") return crypto;
      if (name === "path") return path;
      if (name === "crypto") return require("node:crypto");
      assert.equal(name, "fs");
      return {
        mkdirSync() {},
        writeFileSync(filename, data) {
          if (filename === "/tmp/gh-aw/agent/dependabot-burner/context.json") {
            payload = JSON.parse(data);
            return;
          }
          assert.match(filename, /^\/tmp\/gh-aw\/agent\/dependabot-burner\/diffs\/\d+\.diff$/);
          assert.equal(typeof data, "string");
        },
      };
    },
    { env: {} },
    { log() {} }
  );
  return payload;
}

test("standalone AgentRx locks are selected as Python dependencies", async () => {
  const result = await select([pull(1, [".github/workflows/requirements-agentrx.txt"]), pull(2, [".github/workflows/requirements-agentrx.txt"])]);
  assert.equal(result.open_dependabot_pr_count, 2);
  assert.equal(result.open_pr_count, 2);
  assert.deepEqual(result.selected_batch_pr_numbers, [1, 2]);
  assert.deepEqual(result.excluded_prs, []);
  assert.deepEqual(
    result.selected_batch_dependencies.map(({ changed_files, manifest_families }) => ({ changed_files, manifest_families })),
    [1, 2].map(() => ({ changed_files: [".github/workflows/requirements-agentrx.txt"], manifest_families: ["pip"] }))
  );
});

test("all Dependabot ecosystems retain oldest-first ordering and ignore other authors", async () => {
  const result = await select([
    pull(3, [".github/workflows/package.json", ".github/workflows/package-lock.json"], "app/dependabot"),
    pull(1, [".github/workflows/requirements.txt"]),
    pull(2, ["go.mod"]),
    pull(4, [".github/workflows/go.mod"], "contributor"),
  ]);
  assert.equal(result.open_dependabot_pr_count, 3);
  assert.equal(result.open_pr_count, 3);
  assert.equal(result.selection_reason, "bundle-all-open-dependabot-prs");
  assert.deepEqual(result.selected_batch_pr_numbers, [1, 2, 3]);
  assert.deepEqual(
    result.selected_batch_dependencies.map(item => item.manifest_families),
    [["pip"], ["go"], ["npm"]]
  );
  assert.deepEqual(result.excluded_prs, []);
});

test("mixed source and generated manifest PRs are selected with all paths preserved", async () => {
  const files = [".github/workflows/package.json", "docs/package.json"];
  const result = await select([pull(1, files)]);
  assert.deepEqual(result.selected_batch_pr_numbers, [1]);
  assert.deepEqual(result.excluded_prs, []);
  assert.deepEqual(result.selected_batch_dependencies[0].changed_files, files);
  assert.deepEqual(result.selected_batch_dependencies[0].manifest_files, files);
  assert.deepEqual(result.selected_batch_dependencies[0].compiler_generated_files, [".github/workflows/package.json"]);
});

test("compiler provenance distinguishes named Python locks from generated declarations", async () => {
  const generated = [".github/workflows/package.json", ".github/workflows/package-lock.json", ".github/workflows/requirements.txt", ".github/workflows/go.mod", ".github/workflows/smoke.lock.yml"];
  const standalone = [
    ".github/workflows/requirements-agentrx.txt",
    ".github/workflows/requirements-geo-optimizer.txt",
    ".github/workflows/requirements-harbor.in",
    ".github/workflows/requirements-harbor.txt",
    "docs/package.json",
    "docs/package-lock.json",
    "go.mod",
    "go.sum",
    "Dockerfile",
  ];
  const result = await select([pull(1, [...generated, ...standalone]), pull(2, standalone)]);
  assert.deepEqual(result.selected_batch_pr_numbers, [1, 2]);
  assert.deepEqual(
    result.selected_batch_dependencies.map(item => item.compiler_generated_files),
    [generated, []]
  );
  assert.deepEqual(result.excluded_prs, []);
});

test("compiled workflow lock YAML does not dilute shared-file grouping", async () => {
  const result = await select([pull(1, [".github/workflows/package.json", ".github/workflows/dependabot-burner.lock.yml"])]);
  assert.deepEqual(result.selected_batch_pr_numbers, [1]);
  assert.deepEqual(result.selected_batch_dependencies[0].manifest_files, [".github/workflows/package.json"]);
  assert.match(
    workflow,
    /create-pull-request:\n\s+title-prefix: "\[dependabot-burner\] "\n\s+expires: "3d"\n\s+labels: \[automation, dependencies, dependabot\]\n\s+reviewers: \[copilot\]\n\s+protected-files: request_review\n\s+exclude-lock-yml: true/
  );
});

test("slash-command grouping still uses only shared files", async () => {
  const result = await select([pull(1, [".github/workflows/package.json"]), pull(2, [".github/workflows/package.json", ".github/workflows/package-lock.json"]), pull(3, [".github/workflows/requirements.txt"])], 1);
  assert.equal(result.selection_reason, "slash-command-similar-prs");
  assert.deepEqual(result.selected_batch_pr_numbers, [1, 2]);
  assert.deepEqual(
    result.related_prs.map(item => item.number),
    [2]
  );
});

test("slash-command grouping includes standalone lockfiles but not unrelated ecosystems", async () => {
  for (const files of [[".github/workflows/requirements-agentrx.txt"], ["docs/package-lock.json"], ["go.mod", "go.sum"], ["Dockerfile"]]) {
    const result = await select([pull(1, files), pull(2, files), pull(3, [".github/workflows/requirements.txt"])], 1);
    assert.equal(result.selection_reason, "slash-command-similar-prs");
    assert.deepEqual(result.selected_batch_pr_numbers, [1, 2]);
    assert.deepEqual(
      result.related_prs.map(item => item.number),
      [2]
    );
  }
});

test("non-Dependabot slash-command triggers do not select unrelated Dependabot PRs", async () => {
  const result = await select([pull(1, ["docs/package.json"], "contributor"), pull(2, [".github/workflows/requirements.txt"])], 1);
  assert.equal(result.selection_reason, "slash-command-trigger-not-dependabot");
  assert.deepEqual(result.selected_batch_pr_numbers, []);
  assert.equal(result.trigger_pr, null);
  assert.equal(result.open_dependabot_pr_count, 1);
});

test("slash-command fallback cannot admit closed or non-Dependabot PRs", async () => {
  for (const fallback of [pull(1, [".github/workflows/requirements.txt"], "contributor"), { ...pull(1, [".github/workflows/requirements.txt"]), state: "closed" }]) {
    const result = await select([], 1, fallback);
    assert.equal(result.trigger_pr, null);
    assert.deepEqual(result.selected_batch_pr_numbers, []);
  }
});

test("slash-command fallback ignores compiled workflow lock YAML", async () => {
  const result = await select([], 1, pull(1, [".github/workflows/package.json", ".github/workflows/dependabot-burner.lock.yml"]));
  assert.equal(result.trigger_pr.number, 1);
  assert.equal(result.open_dependabot_pr_count, 1);
  assert.equal(result.open_pr_count, 1);
  assert.deepEqual(result.selected_batch_pr_numbers, [1]);
});

test("slash-command fallback admits source dependencies and groups existing related PRs", async () => {
  const result = await select([pull(2, ["docs/package-lock.json"]), pull(3, ["go.mod"])], 1, pull(1, ["docs/package.json", "docs/package-lock.json"]));
  assert.equal(result.open_dependabot_pr_count, 3);
  assert.equal(result.open_pr_count, 3);
  assert.deepEqual([...result.selected_batch_pr_numbers].sort(), [1, 2]);
  assert.deepEqual(result.trigger_pr.manifest_families, ["npm"]);
  assert.deepEqual(result.trigger_pr.compiler_generated_files, []);
  assert.deepEqual(
    result.selected_batch_dependencies.map(item => item.compiler_generated_files),
    [[], []]
  );
});

test("all fourteen PR file sets excluded by run 43439265743 are eligible", async () => {
  const fileSets = [
    [".github/workflows/requirements-geo-optimizer.txt", ".github/workflows/requirements-harbor.txt"],
    ["actions/setup/js/package-lock.json"],
    ["docs/package-lock.json"],
    ["Dockerfile"],
    ["docs/package-lock.json", "docs/package.json"],
    ["actions/setup/js/package-lock.json", "actions/setup/js/package.json"],
    [".github/workflows/requirements-agentrx.txt"],
    ["docs/package-lock.json", "docs/package.json"],
    ["go.mod", "go.sum"],
    ["actions/setup/js/package-lock.json", "actions/setup/js/package.json"],
    ["go.mod", "go.sum"],
    ["docs/package-lock.json", "docs/package.json"],
    [".github/workflows/requirements-agentrx.txt"],
    ["docs/package-lock.json", "docs/package.json"],
  ];
  const result = await select(fileSets.map((files, index) => pull(index + 1, files)).reverse());
  assert.equal(result.open_dependabot_pr_count, 14);
  assert.equal(result.open_pr_count, 14);
  assert.deepEqual(result.excluded_prs, []);
  assert.deepEqual(
    result.selected_batch_pr_numbers,
    fileSets.map((_, index) => index + 1)
  );
  assert.deepEqual(
    result.selected_batch_dependencies.map(item => item.manifest_files),
    fileSets
  );
  assert.deepEqual(
    result.selected_batch_dependencies.map(item => item.manifest_families),
    [["pip"], ["npm"], ["npm"], ["docker"], ["npm"], ["npm"], ["pip"], ["npm"], ["go"], ["npm"], ["go"], ["npm"], ["pip"], ["npm"]]
  );
});

test("conventional commit titles retain dependency versions while grouped titles require diff inspection", async () => {
  const result = await select([
    { ...pull(1, ["docs/package-lock.json"]), title: "build(deps): bump fast-xml-parser from 5.7.1 to 5.12.0 in /docs in the npm_and_yarn group across 1 directory" },
    { ...pull(2, [".github/workflows/requirements-harbor.txt"]), title: "build(deps): bump the pip group across 1 directory with 2 updates" },
  ]);
  const [single, group] = result.selected_batch_dependencies;
  assert.equal(single.title_parse_mode, "parsed");
  assert.equal(single.dependency_name, "fast-xml-parser");
  assert.equal(single.current_version, "5.7.1");
  assert.equal(single.target_version, "5.12.0");
  assert.equal(group.title_parse_mode, "fallback");
  assert.equal(group.current_version, "");
  assert.equal(group.target_version, "");
});

test("action updates including compiled-lock-only PRs remain eligible", async () => {
  const result = await select([pull(1, [".github/workflows/ci.yml"]), pull(2, [".github/workflows/smoke.lock.yml"])]);
  assert.deepEqual(result.selected_batch_pr_numbers, [1, 2]);
  assert.deepEqual(
    result.selected_batch_dependencies.map(item => item.manifest_families),
    [["github-actions"], ["github-actions"]]
  );
  assert.deepEqual(result.selected_batch_dependencies[1].manifest_files, [".github/workflows/smoke.lock.yml"]);
});

test("truly empty selection stays distinct and exclusion explanations cannot be skipped", async () => {
  const result = await select([]);
  assert.equal(result.open_dependabot_pr_count, 0);
  assert.deepEqual(result.excluded_prs, []);
  assert.deepEqual(result.selected_batch_pr_numbers, []);
  assert.match(workflow, /selected remediation batch and excluded_prs are both empty/);
  assert.match(workflow, /Do not claim that all dependency updates are handled/);
});
