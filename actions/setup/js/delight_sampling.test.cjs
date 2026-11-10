import { spawnSync } from "node:child_process";
import { randomUUID } from "node:crypto";
import { mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { readDocumentMetadata } from "../../../scripts/markdown_metadata_test_adapter.cjs";

const root = resolve(import.meta.dirname, "../../..");
const source = readFileSync(join(root, ".github/workflows/delight.md"), "utf8");
const frontmatter = source.match(/^---\r?\n([\s\S]*?)\r?\n---/)[1];
const workflow = readDocumentMetadata(frontmatter, { version: "1.2", uniqueKeys: true }).value;
const script = workflow["pre-agent-steps"].find(step => step.name === "Sample files and load memory").run;
const helpScript = workflow["pre-agent-steps"].find(step => step.name === "Build CLI and pre-collect help output").run;
const fixtures = [];
const samples = ["doc-samples", "workflow-samples", "validation-sample"];

afterEach(() => {
  for (const fixture of fixtures.splice(0)) {
    rmSync(fixture, { recursive: true, force: true });
  }
});

function fixture() {
  const directory = join(root, `.delight-sampling-${randomUUID()}`);
  fixtures.push(directory);
  for (const path of ["docs/src/content/docs", ".github/workflows", "pkg", "bin"]) {
    mkdirSync(join(directory, path), { recursive: true });
  }
  const write = (path, content) => {
    mkdirSync(dirname(join(directory, path)), { recursive: true });
    writeFileSync(join(directory, path), content);
  };
  const command = (name, body) => {
    writeFileSync(join(directory, "bin", name), `#!/bin/bash\n${body}\n`, { mode: 0o755 });
  };
  // A deterministic, fully consuming shuf stand-in also works on hosts without GNU coreutils.
  command("shuf", 'test "$1" = -n || exit 2\nhead -n "$2" "$3"');
  return {
    directory,
    write,
    command,
    read: name => readFileSync(join(directory, "runtime", "agent", name), "utf8"),
    run: (preparation = script) =>
      spawnSync("bash", ["-c", preparation.replaceAll("/tmp/gh-aw", join(directory, "runtime"))], {
        cwd: directory,
        env: { ...process.env, PATH: `${join(directory, "bin")}:${process.env.PATH}` },
        encoding: "utf8",
        timeout: 10000,
      }),
  };
}

describe("Delight pre-agent sampling", () => {
  it("completes an empty cold start with empty samples and memory defaults", () => {
    const setup = fixture();
    setup.write(".github/workflows/no-messages.md", "description: No messages configured\n");
    const result = setup.run();
    expect(result.status, result.stderr).toBe(0);
    for (const sample of samples) {
      expect(setup.read(`${sample}.txt`)).toBe("");
      expect(setup.read(`${sample}-content.txt`)).toBe("");
    }
    expect(JSON.parse(setup.read("previous-findings.json"))).toEqual([]);
    expect(JSON.parse(setup.read("improvements.json"))).toEqual([]);
  });

  it("samples matching files, reads paths with spaces, and restores existing memory", () => {
    const setup = fixture();
    setup.write("docs/src/content/docs/user guide.md", "Documentation\n");
    setup.write(".github/workflows/example.md", "messages: configured\n");
    setup.write("pkg/example_validation.go", "package example\n");
    const findings = { date: "2026-11-10", single_file_improvements: [{ file: "guide.md" }] };
    const improvements = { last_updated: "2026-11-10", pending_tasks: [{ file: "guide.md" }] };
    setup.write("runtime/repo-memory/default/findings-2026-11-10.json", JSON.stringify(findings));
    setup.write("runtime/repo-memory/default/findings-2026-11-09.json", '{"date":"2026-11-09"}\n');
    setup.write("runtime/repo-memory/default/improvements.json", JSON.stringify(improvements));
    const result = setup.run();
    expect(result.status, result.stderr).toBe(0);
    expect(setup.read("doc-samples-content.txt")).toBe("Documentation\n");
    expect(setup.read("workflow-samples-content.txt")).toBe("messages: configured\n");
    expect(setup.read("validation-sample-content.txt")).toBe("package example\n");
    expect(JSON.parse(setup.read("previous-findings.json"))).toEqual(findings);
    expect(JSON.parse(setup.read("improvements.json"))).toEqual(improvements);
  });

  it("allows an existing empty memory directory as a cold start", () => {
    const setup = fixture();
    mkdirSync(join(setup.directory, "runtime/repo-memory/default"), { recursive: true });
    const result = setup.run();
    expect(result.status, result.stderr).toBe(0);
    expect(JSON.parse(setup.read("previous-findings.json"))).toEqual([]);
    expect(JSON.parse(setup.read("improvements.json"))).toEqual([]);
  });

  it.each(["findings-2026-11-10.json", "improvements.json"])("propagates restored %s read failures", name => {
    const setup = fixture();
    setup.write(`runtime/repo-memory/default/${name}`, "{}\n");
    setup.command("cat", `case "$1" in */repo-memory/default/*) echo 'memory: fixture I/O failure' >&2; exit 2 ;; esac\nexec /bin/cat "$@"`);
    const result = setup.run();
    expect(result.status).toBe(2);
    expect(result.stderr).toContain("memory: fixture I/O failure");
  });

  it("rejects an invalid restored memory directory instead of treating it as absent", () => {
    const setup = fixture();
    setup.write("runtime/repo-memory/default", "not a directory");
    const result = setup.run();
    expect(result.status).toBe(1);
    expect(result.stderr).toContain("Cannot read restored memory directory:");
  });

  it.each(["find", "grep", "shuf", "cat"])("propagates actual %s failures", name => {
    const setup = fixture();
    setup.write("docs/src/content/docs/guide.md", "Documentation\n");
    setup.command(name, `echo '${name}: fixture I/O failure' >&2\nexit 2`);
    const result = setup.run();
    expect(result.status).toBe(2);
    expect(result.stderr).toContain(`${name}: fixture I/O failure`);
  });

  it("fully drains large producer output before sampling", () => {
    const setup = fixture();
    // Exceeds a pipe buffer; an early-closing sampler would SIGPIPE this producer.
    setup.command("find", 'for ((i = 0; i < 12000; i++)); do printf "docs/src/content/docs/guide.md\\n"; done');
    setup.write("docs/src/content/docs/guide.md", "Documentation\n");
    const result = setup.run();
    expect(result.status, result.stderr).toBe(0);
    expect(setup.read("doc-samples.txt").trim().split("\n")).toHaveLength(2);
    expect(setup.read("validation-sample.txt").trim().split("\n")).toHaveLength(1);
    expect(setup.read("doc-samples-content.txt")).toBe("Documentation\nDocumentation\n");
  });
});

function helpFixture(binary = 'printf "Help for %s\\n" "$*"') {
  const setup = fixture();
  setup.command("built-gh-aw", `printf '%s\\n' "$*" >> help-calls.txt\n${binary}`);
  setup.command("go", 'test "$*" = "build -o ./gh-aw ./cmd/gh-aw" || exit 3\ncp bin/built-gh-aw ./gh-aw');
  return setup;
}

describe("Delight pre-agent CLI help", () => {
  it("builds the missing CLI and collects all three finite help invocations", () => {
    const setup = helpFixture();
    const result = setup.run(helpScript);
    expect(result.status, result.stderr).toBe(0);
    expect(setup.read("help-output/main.txt")).toBe("Help for --help\n");
    expect(setup.read("help-output/compile.txt")).toBe("Help for compile --help\n");
    expect(setup.read("help-output/run.txt")).toBe("Help for run --help\n");
    expect(readFileSync(join(setup.directory, "help-calls.txt"), "utf8")).toBe("--help\ncompile --help\nrun --help\n");
  });

  it("stops when the CLI build fails", () => {
    const setup = helpFixture();
    setup.command("go", "echo 'go: fixture build failure' >&2\nexit 2");
    const result = setup.run(helpScript);
    expect(result.status).toBe(2);
    expect(result.stderr).toContain("go: fixture build failure");
  });

  it.each(["--help", "compile --help", "run --help"])("propagates failure collecting %s", command => {
    const setup = helpFixture(`if [[ "$*" = "${command}" ]]; then exit 7; fi\nprintf 'Help\\n'`);
    const result = setup.run(helpScript);
    expect(result.status).toBe(7);
  });

  it("rejects empty help output even when the CLI exits successfully", () => {
    const setup = helpFixture("exit 0");
    const result = setup.run(helpScript);
    expect(result.status).toBe(1);
    expect(result.stderr).toContain("Missing CLI help output: main");
  });
});
