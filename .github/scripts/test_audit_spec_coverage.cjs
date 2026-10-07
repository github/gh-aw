const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { execFileSync } = require("node:child_process");
const { auditSpecCoverage } = require("./audit_spec_coverage.cjs");
const { buildCopilotSDKPermissionHandler } = require("../../actions/setup/js/copilot_sdk_permissions.cjs");

function fixture(t, files) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "spec-coverage-"));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  fs.mkdirSync(path.join(root, ".github", "aw"), { recursive: true });
  for (const [filename, content] of Object.entries(files)) {
    const target = path.join(root, filename);
    fs.mkdirSync(path.dirname(target), { recursive: true });
    fs.writeFileSync(target, content);
  }
  return root;
}

const description = "---\ndescription: Fixture specification\n---\n";

test("scans all specifications and links beyond the old 30-file and truncated-output limits", t => {
  const files = {};
  for (let index = 0; index < 100; index++) {
    files[`.github/aw/spec-${String(index).padStart(3, "0")}.md`] = `${description}[target](target.md)\n[target again](target.md)\n`;
  }
  files[".github/aw/target.md"] = description;
  files[".github/aw/spec-099.md"] += "[missing](missing.md)\n";
  files[".github/aw/logs/ignored.md"] = "[irrelevant](absent.md)";
  const result = auditSpecCoverage(fixture(t, files));
  assert.equal(result.files_scanned, 101);
  assert.equal(result.links_checked, 201);
  assert.deepEqual(result.missing_descriptions, []);
  assert.deepEqual(result.broken_links, [{ file: "spec-099.md", line: 6, target: "missing.md", reason: "missing or outside repository" }]);
});

test("resolves relative and encoded links without inspecting external URLs or illustrative code", t => {
  const root = fixture(t, {
    ".github/aw/source.md":
      description +
      '[local](target.md#section)\n[relative](../../docs/target.md)\n[encoded](target%20file.md "title")\n' +
      "[external](https://example.com/absent.md)\n[anchor](#section)\n[site](/reference/absent.md)\n" +
      "`[example](absent.md)`\n```markdown\n[example](absent.md)\n```\n~~~\n[example](absent.md)\n~~~\n",
    ".github/aw/target.md": description,
    ".github/aw/target file.md": description,
    "docs/target.md": "A documentation target.",
  });
  assert.deepEqual(auditSpecCoverage(root), {
    files_scanned: 3,
    links_checked: 3,
    broken_links: [],
    missing_descriptions: [],
  });
});

test("requires a frontmatter description rather than a matching line in the body", t => {
  const root = fixture(t, {
    ".github/aw/body.md": "# Body\ndescription: Not frontmatter\n",
    ".github/aw/empty.md": "---\ntitle: Fixture\n---\n",
    ".github/aw/windows.md": "---\r\ndescription: Fixture\r\n---\r\n",
  });
  assert.deepEqual(auditSpecCoverage(root).missing_descriptions, ["body.md", "empty.md"]);
});

test("distinguishes closing fences from language markers and single-line code spans", t => {
  const root = fixture(t, {
    ".github/aw/source.md": description + "```markdown\n```not-a-closing-fence\n[example](absent.md)\n```\n" + "```[inline example](absent.md)```\n[real link](missing.md)\n",
  });
  const result = auditSpecCoverage(root);
  assert.equal(result.links_checked, 1);
  assert.equal(result.broken_links[0].target, "missing.md");
});

test("reports malformed local links and prevents traversing outside the repository", t => {
  const root = fixture(t, {
    ".github/aw/source.md": `${description}[invalid](bad%ZZ.md)\n[outside](../../../outside.md)\n`,
  });
  const result = auditSpecCoverage(root);
  assert.equal(result.links_checked, 2);
  assert.deepEqual(
    result.broken_links.map(link => link.reason),
    ["invalid URI encoding", "missing or outside repository"]
  );
});

test("fails explicitly when the specification directory is missing", t => {
  const root = fixture(t, {});
  fs.rmdirSync(path.join(root, ".github", "aw"));
  assert.throws(() => auditSpecCoverage(root), { code: "ENOENT" });
});

test("does not read a specification directory symlinked outside the repository", t => {
  const root = fixture(t, {});
  const outside = fixture(t, { ".github/aw/private.md": "Private fixture content" });
  fs.rmdirSync(path.join(root, ".github", "aw"));
  fs.symlinkSync(path.join(outside, ".github", "aw"), path.join(root, ".github", "aw"));
  assert.throws(() => auditSpecCoverage(root), /Specification directory is outside the repository/);
});

test("does not follow local links to files symlinked outside the repository", t => {
  const root = fixture(t, { ".github/aw/source.md": `${description}[outside](../../linked.md)\n` });
  const outside = fixture(t, { "private.md": "Private fixture content" });
  fs.symlinkSync(path.join(outside, "private.md"), path.join(root, "linked.md"));
  assert.equal(auditSpecCoverage(root).broken_links.length, 1);
});

test("the exact scanner command is permitted without granting arbitrary Node execution", () => {
  const command = "node .github/scripts/audit_spec_coverage.cjs";
  const allowedTools = ["shell(grep)", "shell(find)", "shell(cat)", `shell(${command})`];
  const handler = buildCopilotSDKPermissionHandler({ allowedTools }, () => ({ kind: "approve-once" }));
  assert.equal(handler({ kind: "shell", fullCommandText: command }).kind, "approve-once");
  assert.equal(handler({ kind: "shell", fullCommandText: "node -e 'process.exit(0)'" }).kind, "reject");
  assert.equal(handler({ kind: "shell", fullCommandText: `${command}; echo extra` }).kind, "reject");
  assert.equal(handler({ kind: "shell", fullCommandText: "grep -rh '\\[.*\\]([a-z].*\\.md)' .github/aw/ | tr -d '()' | while read f; do [ -f \"$f\" ]; done" }).kind, "reject");
});

test("the generated workflow authorizes the scanner through the real SDK permission handler", () => {
  const lock = fs.readFileSync(path.join(__dirname, "../workflows/daily-spec-coverage-kiro.lock.yml"), "utf8");
  const match = lock.match(/^\s+GH_AW_COPILOT_SDK_TOOL_CONFIG: '(.*)'$/m);
  assert.ok(match, "compiled workflow must configure SDK permissions");
  const config = JSON.parse(match[1].replace(/''/g, "'"));
  const handler = buildCopilotSDKPermissionHandler(config.permissions, () => ({ kind: "approve-once" }));
  assert.equal(handler({ kind: "shell", fullCommandText: "node .github/scripts/audit_spec_coverage.cjs" }).kind, "approve-once");
  assert.equal(handler({ kind: "shell", fullCommandText: "node -e 'process.exit(0)'" }).kind, "reject");
});

test("CLI executes the real scan without credentials and emits complete JSON", t => {
  const root = fixture(t, { ".github/aw/source.md": description });
  const output = execFileSync(process.execPath, [path.join(__dirname, "audit_spec_coverage.cjs")], {
    cwd: root,
    env: { PATH: process.env.PATH },
    encoding: "utf8",
    timeout: 5000,
  });
  assert.deepEqual(JSON.parse(output), auditSpecCoverage(root));
});
