import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "fs";
import path from "path";
import { execFileSync } from "child_process";
import { prepare, finish, parseRepositories, parseAllowedRepos, workspacePath, validateSingleRepository } from "./checkout_dynamic.cjs";
import { buildCheckoutManifest, readManifestEntriesFromEnv } from "./build_checkout_manifest.cjs";

describe("single repository checkout", () => {
  it.each(["org/repo", "Org/Private.Repo", " org/repo \n", "org/repo.wiki"])("accepts exactly one repository without multi-repo policy: %s", repository => {
    expect(() => validateSingleRepository(repository)).not.toThrow();
  });

  it.each([undefined, "", " \n", "org/a\norg/b", "org/a\nORG/A", "org/a,org/b", "org/*", "org/repo/extra", "org/..", "org/.", "org/a\rorg/b"])("rejects missing, multiple, or invalid repositories: %s", repository => {
    expect(() => validateSingleRepository(repository)).toThrow("exactly one owner/repo slug");
  });
});

describe("runtime repository checkout", () => {
  let temp;
  let workspace;
  let runnerTemp;
  let runtime;
  const metadata = (repository, visibility = "public") => ({
    full_name: repository,
    visibility,
    private: visibility !== "public",
    default_branch: "main",
  });

  beforeEach(() => {
    temp = fs.mkdtempSync(path.join(__dirname, ".gh-aw-checkout-"));
    workspace = path.join(temp, "workspace");
    runnerTemp = path.join(temp, "temp");
    fs.mkdirSync(workspace);
    fs.mkdirSync(runnerTemp);
    for (const [key, value] of Object.entries({
      GITHUB_WORKSPACE: workspace,
      RUNNER_TEMP: runnerTemp,
      GITHUB_REPOSITORY: "host/workflows",
      GITHUB_SERVER_URL: "https://github.com",
      GH_AW_DYNAMIC_CHECKOUT_INDEX: "0",
      GH_AW_DYNAMIC_PREFIX: "targets",
      GH_AW_DYNAMIC_REPOSITORIES: "org/a\r\norg/b\n",
      GH_AW_DYNAMIC_ALLOWED_REPOS: "org/*",
      GH_AW_DYNAMIC_MAX_REPOS: "10",
      GH_AW_DYNAMIC_ALLOW_PRIVATE_REPOS: "false",
      GH_AW_DYNAMIC_TOKEN: "secret-token",
      GH_AW_DYNAMIC_CHECKOUT_ACTION: `actions/checkout@${"a".repeat(40)}`,
      GH_AW_DYNAMIC_SUBMODULES: "false",
      GH_AW_DYNAMIC_LFS: "false",
      GH_AW_DYNAMIC_PERSIST: "false",
      GH_AW_DYNAMIC_FETCH_COUNT: "0",
      GH_AW_DYNAMIC_SPARSE: "",
    }))
      vi.stubEnv(key, value);
    runtime = {
      core: { setSecret: vi.fn(), setOutput: vi.fn() },
      github: { request: vi.fn().mockResolvedValue({ data: metadata("host/workflows") }) },
      getOctokit: vi.fn().mockReturnValue({
        request: vi.fn().mockImplementation(async (_route, { owner, repo }) => ({ data: metadata(`${owner}/${repo}`) })),
      }),
    };
  });

  afterEach(() => {
    vi.unstubAllEnvs();
    fs.rmSync(temp, { recursive: true, force: true });
  });

  it("expands and deduplicates newline-separated repositories into pinned, credential-free checkouts", async () => {
    vi.stubEnv("GH_AW_DYNAMIC_REPOSITORIES", "org/a\nORG/A\norg/b\n");
    const entries = await prepare(runtime);
    expect(entries).toEqual([
      { repository: "org/a", path: "targets/org/a", default_branch: "main" },
      { repository: "org/b", path: "targets/org/b", default_branch: "main" },
    ]);
    expect(runtime.getOctokit).toHaveBeenCalledWith("secret-token");
    const action = JSON.parse(fs.readFileSync(path.join(workspace, ".gh-aw/checkouts/0/action.yml"), "utf8"));
    expect(action.runs.steps).toHaveLength(2);
    expect(action.runs.steps[0].uses).toBe(`actions/checkout@${"a".repeat(40)}`);
    expect(action.runs.steps[0].with.repository).toBe("org/a");
    expect(action.runs.steps[0].with["persist-credentials"]).toBe("${{ env.GH_AW_DYNAMIC_PERSIST }}");
    expect(action.runs.steps.every(step => step.with.lfs === false)).toBe(true);
    expect(action.runs.steps.every(step => step.with.submodules === false)).toBe(true);
    expect(JSON.stringify(action)).not.toContain("secret-token");
    expect(JSON.stringify(entries)).not.toContain("secret-token");
  });

  it("handles an empty scheduled batch without expanding to all allowed repositories", async () => {
    vi.stubEnv("GH_AW_DYNAMIC_REPOSITORIES", "\n");
    expect(await prepare(runtime)).toEqual([]);
    const action = JSON.parse(fs.readFileSync(path.join(workspace, ".gh-aw/checkouts/0/action.yml"), "utf8"));
    expect(action.runs.steps).toEqual([{ shell: "bash", run: "true" }]);
  });

  it.each(["0", "-1", "1.5", "NaN", "Infinity", "9007199254740992", "", " 2 ", "2e1", "2repos", "${{ inputs.max }}"])("rejects invalid max-repos before authenticated reads: %s", async value => {
    vi.stubEnv("GH_AW_DYNAMIC_MAX_REPOS", value);
    await expect(prepare(runtime)).rejects.toThrow("checkout.max-repos must be a positive integer");
    expect(runtime.core.setOutput).toHaveBeenCalledWith("checkout_error", expect.stringContaining("checkout.max-repos"));
    expect(runtime.getOctokit).not.toHaveBeenCalled();
    expect(runtime.github.request).not.toHaveBeenCalled();
    expect(fs.existsSync(path.join(workspace, ".gh-aw"))).toBe(false);
    expect(fs.existsSync(path.join(runnerTemp, "gh-aw"))).toBe(false);
  });

  it.each(["public", "private", "internal"])("rejects the whole oversized batch in a %s host before authenticated reads", async visibility => {
    runtime.github.request.mockResolvedValue({ data: metadata("host/workflows", visibility) });
    vi.stubEnv("GH_AW_DYNAMIC_MAX_REPOS", "1");
    await expect(prepare(runtime)).rejects.toThrow("selected 2 repositories, exceeding checkout.max-repos (1)");
    expect(runtime.core.setOutput).toHaveBeenCalledWith("checkout_error", expect.stringContaining("exceeding checkout.max-repos (1)"));
    expect(runtime.getOctokit).not.toHaveBeenCalled();
    expect(runtime.github.request).not.toHaveBeenCalled();
    expect(fs.existsSync(path.join(workspace, ".gh-aw"))).toBe(false);
    expect(fs.existsSync(path.join(runnerTemp, "gh-aw"))).toBe(false);
  });

  it.each(["false", "true"])("counts unique slugs at the limit with persist-credentials %s", async persist => {
    vi.stubEnv("GH_AW_DYNAMIC_PERSIST", persist);
    vi.stubEnv("GH_AW_DYNAMIC_MAX_REPOS", "2");
    vi.stubEnv("GH_AW_DYNAMIC_REPOSITORIES", "org/a\nORG/A\norg/b\n\n");
    expect(await prepare(runtime)).toHaveLength(2);
  });

  it.each([undefined, "12"])("enforces the default or explicitly raised cap: %s", async max => {
    vi.stubEnv("GH_AW_DYNAMIC_MAX_REPOS", max);
    vi.stubEnv("GH_AW_DYNAMIC_REPOSITORIES", Array.from({ length: 11 }, (_, i) => `org/repo-${i}`).join("\n"));
    if (max === undefined) {
      await expect(prepare(runtime)).rejects.toThrow("exceeding checkout.max-repos (10)");
      expect(runtime.getOctokit).not.toHaveBeenCalled();
    } else {
      expect(await prepare(runtime)).toHaveLength(11);
    }
  });

  it.each(["", "  \n", "org", "https://github.com/org/a", "*", " * ", "*/*", "**/**", "org/*\n*", "org/*,*"])("rejects invalid or missing allowed-repos: %s", async allow => {
    vi.stubEnv("GH_AW_DYNAMIC_ALLOWED_REPOS", allow);
    await expect(prepare(runtime)).rejects.toThrow(/allowed-repos/);
    expect(runtime.getOctokit).not.toHaveBeenCalled();
    expect(runtime.core.setOutput).toHaveBeenCalledWith("checkout_error", expect.stringContaining("allowed-repos"));
    expect(fs.existsSync(path.join(workspace, ".gh-aw"))).toBe(false);
  });

  it("checks exact and wildcard patterns case-insensitively and denies unmatched repositories", async () => {
    expect(parseAllowedRepos("ORG/a\nother/*,*/c")).toEqual(new Set(["org/a", "other/*", "*/c"]));
    vi.stubEnv("GH_AW_DYNAMIC_ALLOWED_REPOS", "org/a");
    await expect(prepare(runtime)).rejects.toThrow("org/b is not permitted");
    expect(runtime.getOctokit).not.toHaveBeenCalled();
    expect(runtime.core.setOutput).toHaveBeenCalledWith("checkout_error", "Runtime checkout repository org/b is not permitted by allowed-repos");
    expect(fs.existsSync(path.join(workspace, ".gh-aw"))).toBe(false);
  });

  it.each(["org/*", "*/a", "org/a*"])("uses allowed-repos pattern %s only to filter explicit selections", async pattern => {
    vi.stubEnv("GH_AW_DYNAMIC_REPOSITORIES", "org/a");
    vi.stubEnv("GH_AW_DYNAMIC_ALLOWED_REPOS", pattern);
    expect(await prepare(runtime)).toEqual([{ repository: "org/a", path: "targets/org/a", default_branch: "main" }]);
  });

  it.each(["org/a\n../secret", "org/a;echo pwned", "${{ secrets.TOKEN }}", "org/..", "org/a b", "org/a/b"])("rejects malformed repository data without executing it: %s", async value => {
    vi.stubEnv("GH_AW_DYNAMIC_REPOSITORIES", value);
    await expect(prepare(runtime)).rejects.toThrow(/owner\/repo/);
    expect(runtime.core.setOutput).toHaveBeenCalledWith("checkout_error", expect.stringContaining("owner/repo"));
    expect(runtime.getOctokit).not.toHaveBeenCalled();
  });

  it.each(["*", "org/*", "*/a", "org/a*", "org/a?", "org/[ab]", "org/a\norg/*"])("rejects repository wildcards instead of expanding them: %s", async value => {
    vi.stubEnv("GH_AW_DYNAMIC_REPOSITORIES", value);
    await expect(prepare(runtime)).rejects.toThrow("wildcards are only supported in checkout.allowed-repos");
    expect(runtime.getOctokit).not.toHaveBeenCalled();
    expect(runtime.core.setOutput).toHaveBeenCalledWith("checkout_error", expect.stringContaining("wildcards are only supported in checkout.allowed-repos"));
    expect(fs.existsSync(path.join(workspace, ".gh-aw"))).toBe(false);
  });

  it.each(
    ["false", "true"].flatMap(persist => ["public", "private", "internal"].flatMap(host => ["public", "private", "internal"].flatMap(target => [undefined, "false", "true"].map(allowPrivate => [persist, host, target, allowPrivate]))))
  )("enforces visibility with persist=%s, host=%s, target=%s, allow-private-repos=%s", async (persist, host, target, allowPrivate) => {
    vi.stubEnv("GH_AW_DYNAMIC_PERSIST", persist);
    vi.stubEnv("GH_AW_DYNAMIC_ALLOW_PRIVATE_REPOS", allowPrivate);
    runtime.github.request.mockResolvedValue({ data: metadata("host/workflows", host) });
    runtime.getOctokit().request.mockImplementation(async (_route, { owner, repo }) => ({
      data: metadata(`${owner}/${repo}`, repo === "b" ? target : "public"),
    }));
    if (target === "public" || (host !== "public" && allowPrivate === "true")) {
      expect(await prepare(runtime)).toHaveLength(2);
      expect(runtime.getOctokit).toHaveBeenCalledWith("secret-token");
    } else {
      const message = host === "public" ? "Public workflows cannot check out non-public repository org/b" : "Runtime checkout of non-public repository org/b requires checkout.allow-private-repos: true";
      await expect(prepare(runtime)).rejects.toThrow(message);
      expect(runtime.core.setOutput).toHaveBeenCalledExactlyOnceWith("checkout_error", message);
      expect(fs.existsSync(path.join(workspace, ".gh-aw"))).toBe(false);
      expect(fs.existsSync(path.join(runnerTemp, "gh-aw"))).toBe(false);
    }
  });

  it.each(["", "TRUE", "1", "null", " false ", "${{ vars.ALLOW_PRIVATE_REPOS }}", "secret-token"])("rejects malformed allow-private-repos before authenticated reads: %s", async value => {
    vi.stubEnv("GH_AW_DYNAMIC_ALLOW_PRIVATE_REPOS", value);
    await expect(prepare(runtime)).rejects.toThrow("checkout.allow-private-repos must be a literal boolean");
    expect(runtime.core.setOutput).toHaveBeenCalledExactlyOnceWith("checkout_error", "checkout.allow-private-repos must be a literal boolean; use true or false");
    expect(runtime.getOctokit).not.toHaveBeenCalled();
    expect(runtime.github.request).not.toHaveBeenCalled();
    expect(fs.existsSync(path.join(workspace, ".gh-aw"))).toBe(false);
    expect(fs.existsSync(path.join(runnerTemp, "gh-aw"))).toBe(false);
  });

  it.each([
    { private: true, visibility: "public" },
    { private: false, visibility: "private" },
    { private: false, visibility: "internal" },
  ])("treats contradictory host visibility conservatively even with private opt-in: %j", async host => {
    vi.stubEnv("GH_AW_DYNAMIC_ALLOW_PRIVATE_REPOS", "true");
    runtime.github.request.mockResolvedValue({ data: { ...metadata("host/workflows"), ...host } });
    runtime.getOctokit().request.mockResolvedValue({ data: metadata("org/a", "private") });
    await expect(prepare(runtime)).rejects.toThrow("Public workflows cannot check out non-public repository org/a");
    expect(fs.existsSync(path.join(workspace, ".gh-aw"))).toBe(false);
  });

  it.each(["false", "true"])("treats private metadata as non-public regardless of the visibility label, with opt-in %s", async allowPrivate => {
    vi.stubEnv("GH_AW_DYNAMIC_ALLOW_PRIVATE_REPOS", allowPrivate);
    runtime.getOctokit().request.mockResolvedValue({ data: { ...metadata("org/a"), private: true } });
    await expect(prepare(runtime)).rejects.toThrow("Public workflows cannot check out non-public repository org/a");
    expect(fs.existsSync(path.join(workspace, ".gh-aw"))).toBe(false);
  });

  it.each(["false", "true"])("fails closed if host or target visibility cannot be verified, with opt-in %s", async allowPrivate => {
    vi.stubEnv("GH_AW_DYNAMIC_ALLOW_PRIVATE_REPOS", allowPrivate);
    runtime.github.request.mockRejectedValue(new Error("secret-token"));
    await expect(prepare(runtime)).rejects.toThrow("Unable to verify workflow repository visibility");
    expect(runtime.core.setOutput).toHaveBeenLastCalledWith("checkout_error", "Unable to verify workflow repository visibility; runtime checkout refused");
    runtime.github.request.mockResolvedValue({ data: metadata("host/workflows") });
    runtime.getOctokit().request.mockRejectedValue(new Error("secret-token"));
    await expect(prepare(runtime)).rejects.toThrow("Unable to verify runtime checkout repository org/a");
    expect(runtime.core.setOutput).toHaveBeenLastCalledWith("checkout_error", "Unable to verify runtime checkout repository org/a; checkout refused");
    runtime.getOctokit().request.mockResolvedValue({ data: { full_name: "org/a", default_branch: "main" } });
    await expect(prepare(runtime)).rejects.toThrow("Unable to verify allowlist and metadata");
    expect(JSON.stringify(runtime.core.setOutput.mock.calls)).not.toContain("secret-token");
  });

  it("reports unexpected preparation failures without publishing dependency error details or swallowing the error", async () => {
    const error = new Error("dependency diagnostic containing secret-token");
    runtime.getOctokit.mockImplementation(() => {
      throw error;
    });
    await expect(prepare(runtime)).rejects.toBe(error);
    expect(runtime.core.setSecret).toHaveBeenCalledWith("secret-token");
    expect(runtime.core.setOutput).toHaveBeenCalledExactlyOnceWith("checkout_error", "Runtime checkout preparation failed; inspect the failed step for details");
  });

  it("reports missing checkout credentials before contacting GitHub", async () => {
    vi.stubEnv("GH_AW_DYNAMIC_TOKEN", "");
    await expect(prepare(runtime)).rejects.toThrow("Runtime checkout requires a GitHub token");
    expect(runtime.core.setOutput).toHaveBeenCalledExactlyOnceWith("checkout_error", "Runtime checkout requires a GitHub token");
    expect(runtime.getOctokit).not.toHaveBeenCalled();
    expect(runtime.github.request).not.toHaveBeenCalled();
  });

  it("revalidates the canonical repository after redirects", async () => {
    runtime.getOctokit().request.mockResolvedValue({ data: metadata("outside/a") });
    await expect(prepare(runtime)).rejects.toThrow("Unable to verify allowlist");
    expect(runtime.core.setOutput).toHaveBeenCalledWith("checkout_error", "Unable to verify allowlist and metadata for runtime checkout repository org/a");
    expect(fs.existsSync(path.join(workspace, ".gh-aw"))).toBe(false);
  });

  it("fails closed when workflow repository visibility metadata is incomplete", async () => {
    runtime.github.request.mockResolvedValue({ data: { private: false } });
    await expect(prepare(runtime)).rejects.toThrow("Unable to verify workflow repository visibility");
    expect(fs.existsSync(path.join(workspace, ".gh-aw"))).toBe(false);
  });

  it.each(["public", "private", "internal"])("rejects enabled submodules before authenticated reads in a %s host", async visibility => {
    runtime.github.request.mockResolvedValue({ data: metadata("host/workflows", visibility) });
    for (const submodules of ["true", "recursive", "${{ inputs.submodules }}"]) {
      vi.stubEnv("GH_AW_DYNAMIC_SUBMODULES", submodules);
      await expect(prepare(runtime)).rejects.toThrow("submodule URLs can bypass allowed-repos");
      expect(runtime.core.setOutput).toHaveBeenLastCalledWith("checkout_error", expect.stringContaining("submodule URLs can bypass allowed-repos"));
      expect(runtime.github.request).not.toHaveBeenCalled();
      expect(runtime.getOctokit).not.toHaveBeenCalled();
      expect(fs.existsSync(path.join(workspace, ".gh-aw"))).toBe(false);
      expect(fs.existsSync(path.join(runnerTemp, "gh-aw"))).toBe(false);
    }
  });

  it.each(["public", "private", "internal"].flatMap(visibility => ["", "false"].map(submodules => [visibility, submodules])))("keeps submodules disabled in a %s host with setting %s", async (visibility, submodules) => {
    runtime.github.request.mockResolvedValue({ data: metadata("host/workflows", visibility) });
    vi.stubEnv("GH_AW_DYNAMIC_SUBMODULES", submodules);
    await prepare(runtime);
    const action = JSON.parse(fs.readFileSync(path.join(workspace, ".gh-aw/checkouts/0/action.yml"), "utf8"));
    expect(action.runs.steps.every(step => step.with.submodules === false)).toBe(true);
  });

  it.each(["public", "private", "internal"])("rejects LFS before authenticated reads in a %s host, with or without sparse checkout", async visibility => {
    runtime.github.request.mockResolvedValue({ data: metadata("host/workflows", visibility) });
    vi.stubEnv("GH_AW_DYNAMIC_LFS", "true");
    for (const sparse of ["", "assets"]) {
      vi.stubEnv("GH_AW_DYNAMIC_SPARSE", sparse);
      await expect(prepare(runtime)).rejects.toThrow("LFS endpoints can bypass allowed-repos");
      expect(runtime.core.setOutput).toHaveBeenLastCalledWith("checkout_error", expect.stringContaining("LFS endpoints can bypass allowed-repos"));
      expect(runtime.github.request).not.toHaveBeenCalled();
      expect(runtime.getOctokit).not.toHaveBeenCalled();
      expect(fs.existsSync(path.join(workspace, ".gh-aw"))).toBe(false);
      expect(fs.existsSync(path.join(runnerTemp, "gh-aw"))).toBe(false);
    }
  });

  it.each(["../outside", "/absolute", ".git/targets", ".gh-aw/targets", "${{ inputs.path }}"])("rejects unsafe path prefixes: %s", async prefix => {
    vi.stubEnv("GH_AW_DYNAMIC_PREFIX", prefix);
    await expect(prepare(runtime)).rejects.toThrow(/path|prefix/);
    expect(runtime.core.setOutput).toHaveBeenCalledWith("checkout_error", expect.stringMatching(/path|prefix/));
    expect(runtime.getOctokit).not.toHaveBeenCalled();
  });

  it("rejects symlinked ancestors, including dangling links", async () => {
    fs.symlinkSync(path.join(temp, "absent"), path.join(workspace, "targets"));
    await expect(prepare(runtime)).rejects.toThrow("symlinks");
    fs.unlinkSync(path.join(workspace, "targets"));
    fs.symlinkSync(runnerTemp, path.join(workspace, ".gh-aw"));
    await expect(prepare(runtime)).rejects.toThrow("symlinks");
  });

  it("does not let actions/checkout clean an existing checkout destination", async () => {
    fs.mkdirSync(path.join(workspace, "targets/org/a"), { recursive: true });
    await expect(prepare(runtime)).rejects.toThrow("destination already exists");
  });

  it("requires SHA-pinned checkout actions", async () => {
    vi.stubEnv("GH_AW_DYNAMIC_CHECKOUT_ACTION", "actions/checkout@v7");
    await expect(prepare(runtime)).rejects.toThrow("SHA-pinned");
  });

  it("records all runtime entries and their API-resolved default branches in the shared manifest", async () => {
    await prepare(runtime);
    vi.stubEnv("GH_AW_DYNAMIC_CHECKOUT_INDEXES", "0");
    vi.stubEnv("GH_AW_CHECKOUT_MANIFEST_COUNT", "0");
    const entries = readManifestEntriesFromEnv();
    expect(entries).toHaveLength(2);
    const runGH = vi.fn();
    const result = buildCheckoutManifest(entries, { runnerTemp, workspace, runGH });
    expect(result.manifest["org/b"]).toEqual({ repository: "org/b", path: "targets/org/b", default_branch: "main" });
    expect(runGH).not.toHaveBeenCalled();
  });

  it("rejects a missing runtime checkout plan instead of writing an incomplete manifest", () => {
    vi.stubEnv("GH_AW_DYNAMIC_CHECKOUT_INDEXES", "0");
    vi.stubEnv("GH_AW_CHECKOUT_MANIFEST_COUNT", "0");
    expect(() => readManifestEntriesFromEnv()).toThrow("Unable to read runtime checkouts");
  });

  it("fetches additional refs with transient per-checkout credentials and never places the token in arguments", async () => {
    await prepare(runtime);
    vi.stubEnv("GH_AW_DYNAMIC_FETCH_COUNT", "3");
    vi.stubEnv("GH_AW_DYNAMIC_FETCH_0", "main");
    vi.stubEnv("GH_AW_DYNAMIC_FETCH_1", "*");
    vi.stubEnv("GH_AW_DYNAMIC_FETCH_2", "refs/pulls/open/*");
    vi.stubEnv("GH_AW_DYNAMIC_FETCH_DEPTH", "0");
    const runGit = vi.fn().mockReturnValue("abc\trefs/heads/main\nabc\trefs/pull/1/head\n");
    await finish({ runGit });
    expect(runGit).toHaveBeenCalledTimes(12);
    expect(runGit.mock.calls[0][0]).toEqual(["-C", path.join(fs.realpathSync(workspace), "targets/org/a"), "ls-remote", "--exit-code", "origin", "refs/heads/main"]);
    const [args, options] = runGit.mock.calls[1];
    expect(args).toEqual(["-C", path.join(fs.realpathSync(workspace), "targets/org/a"), "fetch", "--no-recurse-submodules", "origin", "+refs/heads/main:refs/remotes/origin/main"]);
    expect(runGit.mock.calls[3][0].at(-1)).toBe("+refs/heads/*:refs/remotes/origin/*");
    expect(runGit.mock.calls[5][0].at(-1)).toBe("+refs/pull/*/head:refs/remotes/origin/pull/*/head");
    expect(JSON.stringify(args)).not.toContain("secret-token");
    expect(options.env.GIT_CONFIG_VALUE_0).toBe("");
    expect(options.env.GIT_CONFIG_VALUE_1).toContain(Buffer.from("x-access-token:secret-token").toString("base64"));
    expect(runGit.mock.calls[0][1].env).toEqual(options.env);
    expect(runGit.mock.calls[0][1].stdio).toEqual(["ignore", "pipe", "pipe"]);
    expect(options.stdio).toEqual(["ignore", "pipe", "pipe"]);
    expect(fs.existsSync(path.join(workspace, ".gh-aw/checkouts/0"))).toBe(false);
    runGit.mockImplementation(() => {
      throw new Error("secret-token");
    });
    await expect(finish({ runGit })).rejects.toThrow("Unable to finalize runtime checkout of org/a");
  });

  it.each(["missing", "missing/*", "refs/pulls/open/*"])("skips zero-match pattern %s while fetching other refs", async missing => {
    await prepare(runtime);
    vi.stubEnv("GH_AW_DYNAMIC_FETCH_COUNT", "3");
    vi.stubEnv("GH_AW_DYNAMIC_FETCH_0", missing);
    vi.stubEnv("GH_AW_DYNAMIC_FETCH_1", "main");
    vi.stubEnv("GH_AW_DYNAMIC_FETCH_2", "also-missing");
    const runGit = vi.fn().mockImplementation(args => {
      if (args.includes("ls-remote") && args.at(-1) !== "refs/heads/main") {
        throw Object.assign(new Error("no matching refs"), { status: 2 });
      }
      return "abc\trefs/heads/main\n";
    });
    await finish({ runGit });
    const fetches = runGit.mock.calls.filter(([args]) => args.includes("fetch"));
    expect(fetches).toHaveLength(2);
    for (const [args] of fetches) {
      expect(args.slice(2)).toEqual(["fetch", "--no-recurse-submodules", "origin", "--depth=1", "+refs/heads/main:refs/remotes/origin/main"]);
    }
  });

  it.each([
    ["missing", "refs/heads/prefix/refs/heads/missing"],
    ["missing/*", "refs/heads/prefix/refs/heads/missing/present"],
    ["refs/pulls/open/*", "refs/other/refs/pull/1/head"],
  ])("skips suffix-only discovery matches for %s", async (ref, collision) => {
    await prepare(runtime);
    vi.stubEnv("GH_AW_DYNAMIC_FETCH_COUNT", "2");
    vi.stubEnv("GH_AW_DYNAMIC_FETCH_0", ref);
    vi.stubEnv("GH_AW_DYNAMIC_FETCH_1", "main");
    const runGit = vi.fn().mockImplementation(args => (args.includes("ls-remote") ? `abc\t${args.at(-1) === "refs/heads/main" ? "refs/heads/main" : collision}\n` : ""));
    await finish({ runGit });
    const fetches = runGit.mock.calls.filter(([args]) => args.includes("fetch"));
    expect(fetches).toHaveLength(2);
    for (const [args] of fetches) {
      expect(args.at(-1)).toBe("+refs/heads/main:refs/remotes/origin/main");
    }
  });

  it.each([
    ["ls-remote", 128],
    ["fetch", 128],
    ["fetch", 2],
  ])("does not ignore %s failure with exit status %s", async (command, status) => {
    await prepare(runtime);
    vi.stubEnv("GH_AW_DYNAMIC_FETCH_COUNT", "1");
    vi.stubEnv("GH_AW_DYNAMIC_FETCH_0", "main");
    const runGit = vi.fn().mockImplementation(args => {
      if (args.includes(command)) throw Object.assign(new Error("secret-token"), { status });
      return "abc\trefs/heads/main\n";
    });
    await expect(finish({ runGit })).rejects.toThrow("Unable to finalize runtime checkout of org/a");
  });

  it("streams large discovery output and skips suffix collisions before fetching main", async () => {
    vi.stubEnv("GH_AW_DYNAMIC_REPOSITORIES", "org/a");
    const [entry] = await prepare(runtime);
    const source = path.join(temp, "source");
    const target = path.join(workspace, entry.path);
    fs.mkdirSync(source);
    fs.mkdirSync(target, { recursive: true });
    const git = (cwd, args, input) => execFileSync("git", ["-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null", "-C", cwd, ...args], { encoding: "utf8", input, stdio: ["pipe", "pipe", "pipe"] }).trim();
    git(source, ["init", "--initial-branch=main"]);
    git(source, ["-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "Fixture"]);
    const head = git(source, ["rev-parse", "HEAD"]);
    const refs = [
      `update refs/heads/prefix/refs/heads/absent ${head}`,
      `update refs/other/refs/pull/1/head ${head}`,
      ...Array.from({ length: 10000 }, (_, index) => `update refs/heads/prefix/refs/heads/missing/${String(index).padStart(100, "0")} ${head}`),
    ];
    git(source, ["update-ref", "--stdin"], refs.join("\n") + "\n");
    git(target, ["init"]);
    git(target, ["remote", "add", "origin", `file://${source}`]);
    vi.stubEnv("GH_AW_DYNAMIC_FETCH_COUNT", "5");
    for (const [index, ref] of ["absent", "missing/*", "refs/pulls/open/*", "truly-missing", "main"].entries()) {
      vi.stubEnv(`GH_AW_DYNAMIC_FETCH_${index}`, ref);
    }
    await finish();
    expect(git(target, ["for-each-ref", "--format=%(refname)", "refs/remotes/origin/"])).toBe("refs/remotes/origin/main");
    expect(git(target, ["rev-parse", "origin/main"])).toBe(head);
  }, 30000);

  it("fails closed when streaming discovery cannot access the remote", async () => {
    vi.stubEnv("GH_AW_DYNAMIC_REPOSITORIES", "org/a");
    const [entry] = await prepare(runtime);
    const target = path.join(workspace, entry.path);
    fs.mkdirSync(target, { recursive: true });
    execFileSync("git", ["init", target], { stdio: "ignore" });
    execFileSync("git", ["-C", target, "remote", "add", "origin", `file://${temp}/nonexistent`]);
    vi.stubEnv("GH_AW_DYNAMIC_FETCH_COUNT", "1");
    vi.stubEnv("GH_AW_DYNAMIC_FETCH_0", "main");
    await expect(finish()).rejects.toThrow("Unable to finalize runtime checkout of org/a");
  });

  it("does not accept root paths or path traversal", () => {
    expect(() => workspacePath(workspace, ".")).toThrow();
    expect(() => workspacePath(workspace, "targets/../other")).toThrow();
    expect(parseRepositories("org/a\nORG/A")).toEqual(["org/a"]);
  });

  function installCredentialHelpers(cleaner) {
    const actions = path.join(runnerTemp, "gh-aw", "actions");
    fs.mkdirSync(actions, { recursive: true });
    for (const name of ["clean_git_credentials.sh", "verify_git_credentials.sh"]) {
      fs.copyFileSync(path.join(__dirname, "..", "sh", name), path.join(actions, name));
    }
    if (cleaner) fs.writeFileSync(path.join(actions, "clean_git_credentials.sh"), cleaner);
    // The production runner has GNU realpath; adapt existing paths for macOS tests.
    const bin = path.join(temp, "bin");
    fs.mkdirSync(bin);
    fs.writeFileSync(path.join(bin, "realpath"), '#!/usr/bin/env node\nprocess.stdout.write(require("fs").realpathSync(process.argv.at(-1)) + "\\n");\n', { mode: 0o755 });
    vi.stubEnv("PATH", `${bin}${path.delimiter}${process.env.PATH}`);
    vi.stubEnv("GIT_CONFIG_GLOBAL", "/dev/null");
    vi.stubEnv("GIT_CONFIG_NOSYSTEM", "1");
  }

  it("cleans and verifies real persisted credential includes beyond discovery depth, including nested submodules", async () => {
    const prefix = Array(12).fill("nested").join("/");
    vi.stubEnv("GH_AW_DYNAMIC_PREFIX", prefix);
    vi.stubEnv("GH_AW_DYNAMIC_REPOSITORIES", "org/a");
    vi.stubEnv("GH_AW_DYNAMIC_PERSIST", "true");
    const [entry] = await prepare(runtime);
    installCredentialHelpers();
    const target = path.join(workspace, entry.path);
    execFileSync("git", ["init", "--quiet", target], { stdio: ["ignore", "pipe", "pipe"] });
    const gitDir = path.join(target, ".git");
    const module = path.join(gitDir, "modules", ...Array(8).fill("nested"), "child");
    execFileSync("git", ["init", "--bare", "--quiet", module], { stdio: ["ignore", "pipe", "pipe"] });
    const includes = [];
    for (const [index, dir] of [gitDir, module].entries()) {
      const include = path.join(fs.realpathSync(runnerTemp), `git-credentials-${index}.config`);
      includes.push(include);
      fs.writeFileSync(include, "[http]\nextraheader = Authorization: test-only-checkout-token\n");
      const config = path.join(dir, "config");
      execFileSync("git", ["config", "--file", config, `includeIf.gitdir:${fs.realpathSync(dir)}.path`, include]);
      expect(fs.readFileSync(config, "utf8")).toContain("git-credentials-");
    }
    expect(path.relative(workspace, path.join(gitDir, "config")).split(path.sep)).toHaveLength(16);
    await finish({ cleanCredentials: true });
    for (const dir of [gitDir, module]) {
      expect(fs.readFileSync(path.join(dir, "config"), "utf8")).not.toContain("git-credentials-");
    }
    for (const include of includes) expect(fs.existsSync(include)).toBe(false);
    expect(fs.existsSync(path.join(workspace, ".gh-aw/checkouts/0"))).toBe(false);
  }, 30000);

  it.each(["exit 7", "exit 0"])("fails closed when explicit cleanup returns '%s' but credentials remain", async cleaner => {
    vi.stubEnv("GH_AW_DYNAMIC_REPOSITORIES", "org/a");
    vi.stubEnv("GH_AW_DYNAMIC_FETCH_COUNT", "1");
    vi.stubEnv("GH_AW_DYNAMIC_FETCH_0", "main");
    const [entry] = await prepare(runtime);
    installCredentialHelpers(`#!/usr/bin/env bash\n${cleaner}\n`);
    const target = path.join(workspace, entry.path);
    execFileSync("git", ["init", "--quiet", target], { stdio: ["ignore", "pipe", "pipe"] });
    execFileSync("git", ["-C", target, "config", "http.extraheader", "Authorization: test-only-checkout-token"]);
    const runGit = vi.fn();
    await expect(finish({ cleanCredentials: true, runGit })).rejects.toThrow("Unable to clean and verify runtime checkout credentials; refusing agent execution");
    expect(runGit).not.toHaveBeenCalled();
    expect(fs.existsSync(path.join(workspace, ".gh-aw/checkouts/0/action.yml"))).toBe(true);
  });

  it("fails closed when an expected checkout is missing even if filesystem discovery would find no configs", async () => {
    await prepare(runtime);
    await expect(finish({ cleanCredentials: true })).rejects.toThrow("Unable to clean and verify runtime checkout credentials; refusing agent execution");
  });

  it("fails closed when the trusted cleanup helpers are unavailable", async () => {
    vi.stubEnv("GH_AW_DYNAMIC_REPOSITORIES", "org/a");
    const [entry] = await prepare(runtime);
    execFileSync("git", ["init", "--quiet", path.join(workspace, entry.path)], { stdio: ["ignore", "pipe", "pipe"] });
    await expect(finish({ cleanCredentials: true })).rejects.toThrow("Unable to clean and verify runtime checkout credentials; refusing agent execution");
  });

  it.each(["missing-config", "symlink-config", "symlink-modules"])("fails closed for an unverifiable checkout: %s", async invalid => {
    vi.stubEnv("GH_AW_DYNAMIC_REPOSITORIES", "org/a");
    const [entry] = await prepare(runtime);
    const target = path.join(workspace, entry.path);
    execFileSync("git", ["init", "--quiet", target], { stdio: ["ignore", "pipe", "pipe"] });
    const config = path.join(target, ".git", "config");
    if (invalid === "symlink-modules") {
      fs.symlinkSync(path.join(temp, "missing"), path.join(target, ".git", "modules"));
    } else {
      fs.unlinkSync(config);
      if (invalid === "symlink-config") {
        const other = path.join(temp, "config");
        fs.writeFileSync(other, "[http]\nextraheader = Authorization: test-only-checkout-token\n");
        fs.symlinkSync(other, config);
      }
    }
    await expect(finish({ cleanCredentials: true })).rejects.toThrow("Unable to clean and verify runtime checkout credentials; refusing agent execution");
    expect(fs.existsSync(path.join(workspace, ".gh-aw/checkouts/0/action.yml"))).toBe(true);
  });

  it("preserves persisted credentials when finalizing a safe-output checkout", async () => {
    vi.stubEnv("GH_AW_DYNAMIC_REPOSITORIES", "org/a");
    vi.stubEnv("GH_AW_DYNAMIC_PERSIST", "true");
    const [entry] = await prepare(runtime);
    const target = path.join(workspace, entry.path);
    execFileSync("git", ["init", "--quiet", target], { stdio: ["ignore", "pipe", "pipe"] });
    execFileSync("git", ["-C", target, "config", "http.extraheader", "Authorization: test-only-checkout-token"]);
    await finish({ cleanCredentials: false });
    expect(fs.readFileSync(path.join(target, ".git/config"), "utf8")).toContain("test-only-checkout-token");
  });

  it("fetches real local git history and removes sparse partial-clone markers", async () => {
    vi.stubEnv("GIT_CONFIG_GLOBAL", "/dev/null");
    vi.stubEnv("GIT_CONFIG_NOSYSTEM", "1");
    vi.stubEnv("GH_AW_DYNAMIC_REPOSITORIES", "org/a");
    await prepare(runtime);
    const source = path.join(temp, "source");
    fs.mkdirSync(source);
    const git = (cwd, args) => execFileSync("git", ["-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null", "-C", cwd, ...args], { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }).trim();
    git(source, ["init", "--initial-branch=main"]);
    git(source, ["-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "Initial"]);
    git(source, ["checkout", "-b", "feature"]);
    git(source, ["-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "Feature"]);
    git(source, ["checkout", "main"]);
    const target = path.join(workspace, "targets/org/a");
    fs.mkdirSync(path.dirname(target), { recursive: true });
    execFileSync("git", ["clone", `file://${source}`, target], { stdio: ["ignore", "pipe", "pipe"] });
    git(target, ["config", "remote.origin.promisor", "true"]);
    git(target, ["config", "remote.origin.partialclonefilter", "blob:none"]);
    vi.stubEnv("GH_AW_DYNAMIC_SPARSE", "src");
    vi.stubEnv("GH_AW_DYNAMIC_FETCH_COUNT", "4");
    vi.stubEnv("GH_AW_DYNAMIC_FETCH_0", "missing");
    vi.stubEnv("GH_AW_DYNAMIC_FETCH_1", "feature");
    vi.stubEnv("GH_AW_DYNAMIC_FETCH_2", "missing/*");
    vi.stubEnv("GH_AW_DYNAMIC_FETCH_3", "refs/pulls/open/*");
    await finish();
    expect(git(target, ["rev-parse", "origin/feature"])).toBe(git(source, ["rev-parse", "feature"]));
    expect(git(target, ["config", "--local", "--list"])).not.toMatch(/promisor|partialclonefilter|extraheader|secret-token/);
    expect(fs.existsSync(path.join(workspace, ".gh-aw/checkouts/0"))).toBe(false);
  }, 30000);

  it("never fetches repository-controlled .gitmodules remotes during finalization, even with recursive fetch configured", async () => {
    vi.stubEnv("GIT_CONFIG_GLOBAL", "/dev/null");
    vi.stubEnv("GIT_CONFIG_NOSYSTEM", "1");
    vi.stubEnv("GH_AW_DYNAMIC_REPOSITORIES", "org/a");
    const [entry] = await prepare(runtime);
    const action = JSON.parse(fs.readFileSync(path.join(workspace, ".gh-aw/checkouts/0/action.yml"), "utf8"));
    expect(action.runs.steps[0].with.submodules).toBe(false);
    const git = (cwd, args) => execFileSync("git", ["-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null", "-C", cwd, ...args], { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }).trim();
    const commit = cwd => git(cwd, ["-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "Fixture"]);
    const outside = path.join(temp, "non-allowlisted");
    const source = path.join(temp, "source");
    for (const dir of [outside, source]) {
      fs.mkdirSync(dir);
      git(dir, ["init", "--initial-branch=main"]);
    }
    commit(outside);
    const modules = `[submodule "outside"]\n\tpath = outside\n\turl = file://${outside}\n`;
    fs.writeFileSync(path.join(source, ".gitmodules"), modules);
    git(source, ["add", ".gitmodules"]);
    git(source, ["update-index", "--add", "--cacheinfo", `160000,${git(outside, ["rev-parse", "HEAD"])},outside`]);
    commit(source);
    const target = path.join(workspace, entry.path);
    fs.mkdirSync(path.dirname(target), { recursive: true });
    execFileSync("git", ["clone", "--no-recurse-submodules", `file://${source}`, target], { stdio: ["ignore", "pipe", "pipe"] });
    commit(outside);
    const outsideHead = git(outside, ["rev-parse", "HEAD"]);
    git(source, ["checkout", "-b", "feature"]);
    git(source, ["update-index", "--cacheinfo", `160000,${outsideHead},outside`]);
    commit(source);
    git(target, ["config", "fetch.recurseSubmodules", "true"]);
    vi.stubEnv("GH_AW_DYNAMIC_FETCH_COUNT", "1");
    vi.stubEnv("GH_AW_DYNAMIC_FETCH_0", "feature");
    const runGit = vi.fn((args, options) => execFileSync("git", args, options));
    await finish({ runGit });
    expect(runGit.mock.calls.map(([args]) => args)).toEqual([
      ["-C", fs.realpathSync(target), "ls-remote", "--exit-code", "origin", "refs/heads/feature"],
      ["-C", fs.realpathSync(target), "fetch", "--no-recurse-submodules", "origin", "--depth=1", "+refs/heads/feature:refs/remotes/origin/feature"],
    ]);
    expect(git(target, ["rev-parse", "origin/feature"])).toBe(git(source, ["rev-parse", "feature"]));
    expect(fs.readFileSync(path.join(target, ".gitmodules"), "utf8")).toBe(modules);
    expect(fs.existsSync(path.join(target, ".git/modules"))).toBe(false);
    expect(fs.existsSync(path.join(target, "outside/.git"))).toBe(false);
    expect(() => git(target, ["cat-file", "-e", outsideHead])).toThrow();
  }, 30000);
});
