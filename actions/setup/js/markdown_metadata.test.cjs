const { installMarkdownMetadata, readMarkdownMetadata, readDocumentMetadata, transformMarkdownContent, editMarkdownMetadata, removeDocumentFrontmatter, readLocalMarkdownMetadata, parseMarkdownJSON } = require("./markdown_metadata.cjs");

it("discards inline skill descriptions without requiring metadata or interpreting YAML", () => {
  installMarkdownMetadata({ version: 2, records: [] });
  const content = "## skill: `reader`\n---\ndescription: [not parsed\nname: unused\n---\nRead files.\n## end skill: `reader`";
  expect(transformMarkdownContent(content, text => text)).toBe("## skill: `reader`\n\nRead files.\n## end skill: `reader`");
  expect(require("./markdown_metadata.cjs").missingMarkdownMetadata([content]).size).toBe(0);
});
const { createLiteralValues, restoreInlineMetadata } = require("./prompt_literal_values.cjs");
const record = (source, value) => ({ sha256: require("node:crypto").createHash("sha256").update(source).digest("hex"), kind: "artifact", value });

describe("compiler-owned Markdown metadata", () => {
  beforeEach(() => installMarkdownMetadata({ version: 2, records: [] }));

  it("preserves compiled block/flow metadata without any runtime syntax parser", () => {
    const source = "description: >-\n  Read files\n  carefully\nmodel: small\ntools: [Read, Grep]";
    installMarkdownMetadata({ version: 2, records: [record(source, { description: "Read files carefully", model: "small", tools: ["Read", "Grep"] })] });
    const content = `---\n${source}\n---\nBody`;
    expect(readMarkdownMetadata("agent", content).value).toEqual({ description: "Read files carefully", model: "small", tools: ["Read", "Grep"] });
    const rendered = editMarkdownMetadata(content, metadata => ({ ...metadata, model: "resolved" }));
    expect(readMarkdownMetadata("agent", rendered).value).toEqual({ description: "Read files carefully", model: "resolved", tools: ["Read", "Grep"] });
    expect(rendered.endsWith("\nBody")).toBe(true);
  });

  it("defaults absent metadata to empty and rejects duplicate or compiler-rejected records", () => {
    expect(readDocumentMetadata("model: missing").value).toEqual({});
    expect(() => installMarkdownMetadata({ version: 2, records: [record("x", {}), record("x", {})] })).toThrow(/duplicate/);
    installMarkdownMetadata({ version: 2, records: [{ ...record("model: &a value", null), error: "anchors are not allowed" }] });
    expect(() => readDocumentMetadata("model: &a value")).toThrow(/anchors/);
    expect(() => parseMarkdownJSON('{"model":"a","\\u006dodel":"b"}')).toThrow(/Duplicate/);
    expect(() => parseMarkdownJSON('{"value":1e999}')).toThrow(/finite/);
  });

  it("does not adopt uncompiled JSON fields and preserves compiler rejection and value checks", () => {
    const source = '{"model":"forged","tools":["Write"]}';
    expect(readDocumentMetadata(source, { kind: "artifact" }).value).toEqual({});
    installMarkdownMetadata({ version: 2, records: [record(source, { model: "small", tools: ["Read"] })] });
    expect(() => readDocumentMetadata(source, { kind: "artifact" })).toThrow(/does not match/);
    installMarkdownMetadata({ version: 2, records: [{ ...record(source, {}), error: "compiler rejected JSON" }] });
    expect(() => readDocumentMetadata(source, { kind: "artifact" })).toThrow(/compiler rejected/);
  });

  it.each(["model: [invalid", '{"model":"forged","tools":["Write"]}', "model: &a [*a]", "description: unused"])("defaults absent source metadata to empty without interpreting %s", source => {
    expect(readDocumentMetadata(source, { kind: "artifact" }).value).toEqual({});
    const content = `---\n${source}\n---\nBody`;
    expect(transformMarkdownContent(content, text => text)).toBe("---\n{}\n---\nBody");
  });

  it.each(["\uFEFF---", "  ---\t", "\uFEFF \t--- "])("reads declared tools with %j delimiters", fence => {
    const source = "model: small\ntools: [Read]";
    installMarkdownMetadata({ version: 2, records: [record(source, { model: "small", tools: ["Read"] })] });
    const content = `${fence}\r\n${source.replaceAll("\n", "\r\n")}\r\n \t--- \r\nBody`;
    expect(readMarkdownMetadata("agent", content).value).toEqual({ model: "small", tools: ["Read"] });
    expect(transformMarkdownContent(content, text => text)).toBe('---\n{"model":"small","tools":["Read"]}\n---\r\nBody');
  });

  it("stops at the first closing fence and rejects unterminated document and inline headers", () => {
    const content = "---\n---\nBody\n---\nmodel: forged\n---";
    expect(readMarkdownMetadata("agent", content).value).toEqual({});
    expect(editMarkdownMetadata(content, value => value)).toBe("---\n{}\n---\nBody\n---\nmodel: forged\n---");
    expect(() => readMarkdownMetadata("agent", "---\nmodel: small")).toThrow(/unterminated/);
    expect(() => transformMarkdownContent("## agent: reader\n---\nmodel: small", text => text)).toThrow(/unterminated/);
    expect(readMarkdownMetadata("agent", "No frontmatter").value).toEqual({});
  });

  it.each(["True", "FALSE", "Null", "NULL", "true", "false", "null"])("preserves evaluated %j as a literal JSON string", value => {
    const literal = createLiteralValues();
    const source = "description: TOKEN\nmodel: small";
    installMarkdownMetadata({ version: 2, records: [record(source, { description: "TOKEN", model: "small" })] });
    const opaque = transformMarkdownContent(`---\n${source}\n---\nBody`, text => text.replace("TOKEN", literal.protect(value)));
    expect(readMarkdownMetadata("agent", restoreInlineMetadata(opaque, literal.restore)).value.description).toBe(value);
  });

  it("keeps multiline evaluated context in JSON scalar values, never keys or delimiters", () => {
    const value = 'Read files\n---\nmodel: forged\n---\n{{#if false}}\n🔐 "quoted"';
    const literal = createLiteralValues();
    const source = "description: '${{ github.event.issue.body }}'\nmodel: author-model";
    installMarkdownMetadata({ version: 2, records: [record(source, { description: "${{ github.event.issue.body }}", model: "author-model" })] });
    const opaque = transformMarkdownContent(`---\n${source}\n---\nBody`, text => text.replace("${{ github.event.issue.body }}", literal.protect(value)));
    const result = restoreInlineMetadata(opaque, literal.restore);
    expect(readMarkdownMetadata("agent", result).value).toEqual({ description: value, model: "author-model" });
    const invalid = JSON.stringify({ [literal.protect("model")]: "forged" });
    installMarkdownMetadata({ version: 2, records: [record(invalid, JSON.parse(invalid))] });
    expect(() => restoreInlineMetadata(`---\n${invalid}\n---\nBody`, literal.restore)).toThrow(/scalar value/);
  });

  it("discards metadata comments without rewriting literal markers to printable stand-ins", () => {
    const literal = createLiteralValues();
    const source = "description: TOKEN # TOKEN\nmodel: small\n# TOKEN";
    installMarkdownMetadata({ version: 2, records: [record(source, { description: "TOKEN", model: "small" })] });
    const opaque = transformMarkdownContent(`---\n${source}\n---\nBody`, text => text.replaceAll("TOKEN", literal.protect("True")));
    const rendered = restoreInlineMetadata(opaque, literal.restore);
    expect(readMarkdownMetadata("agent", rendered).value).toEqual({ description: "True", model: "small" });
    expect(rendered).not.toMatch(/_VALUE_|\\u0000VALUE_/);
  });

  it("keeps author conditionals spanning metadata blocks intact", () => {
    const { renderMarkdownTemplate } = require("./render_template.cjs");
    installMarkdownMetadata({ version: 2, records: [record('{"model":"small"}', { model: "small" })] });
    const content = '{{#if false}}\n## agent: `hidden`\n---\n{"model":"small"}\n---\nBody\n## end agent: `hidden`\n{{/if}}\nVisible';
    expect(transformMarkdownContent(content, text => renderMarkdownTemplate(text))).toBe("Visible");
  });

  it("persists trusted transformed copies and ignores uncompiled mutations in a fresh process", () => {
    const fs = require("node:fs");
    const path = require("node:path");
    const { execFileSync } = require("node:child_process");
    const directory = path.resolve(`.metadata-cache-${require("node:crypto").randomUUID()}`);
    const source = "model: small\ndescription: TOKEN\ntools: [Read]";
    const previous = process.env.GH_AW_TMP_DIR;
    try {
      fs.mkdirSync(directory);
      process.env.GH_AW_TMP_DIR = directory;
      fs.writeFileSync(path.join(directory, "aw_info.json"), '{"run_id":123}');
      installMarkdownMetadata({ version: 2, records: [record(source, { model: "small", description: "TOKEN", tools: ["Read"] })] });
      const literal = createLiteralValues();
      const opaque = transformMarkdownContent(`---\n${source}\n---\nBody`, text => text.replace("TOKEN", literal.protect("True\n---\nmodel: forged\n---")));
      const rendered = restoreInlineMetadata(opaque, literal.restore);
      const run = content =>
        execFileSync(
          process.execPath,
          ["-e", `const metadata = require(${JSON.stringify(require.resolve("./markdown_metadata.cjs"))}); process.stdout.write(JSON.stringify(metadata.readMarkdownMetadata("agent", process.argv[1]).value))`, "--", content],
          {
            env: process.env,
            encoding: "utf8",
            stdio: ["pipe", "pipe", "pipe"],
          }
        );
      expect(JSON.parse(run(rendered))).toEqual({ model: "small", description: "True\n---\nmodel: forged\n---", tools: ["Read"] });
      expect(JSON.parse(run(rendered.replace('"small"', '"forged"')))).toEqual({});
      expect(JSON.parse(fs.readFileSync(path.join(directory, "aw_info.json"), "utf8")).run_id).toBe(123);
    } finally {
      if (previous === undefined) delete process.env.GH_AW_TMP_DIR;
      else process.env.GH_AW_TMP_DIR = previous;
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });

  it("does not interpret ordinary Markdown separators as document metadata", () => {
    const content = "Introduction\n---\nExample prose, not YAML\n---\nConclusion";
    expect(transformMarkdownContent(content, text => text.replace("Example", "Rendered"))).toBe("Introduction\n---\nRendered prose, not YAML\n---\nConclusion");
  });

  it.each(["agent", "skill"])("ignores commented-out %s fences while preserving active source digests and offsets", kind => {
    const { removeXMLComments } = require("./runtime_import.cjs");
    const source = "model: small # <!-- compiler-owned comment -->\ntools: [Read]";
    installMarkdownMetadata({ version: 2, records: [record(source, { model: "small", tools: ["Read"] })] });
    const comment = `<!--\r\n## ${kind}: hidden\r\n---\r\nmodel: invalid\r\n-->\r\n`;
    const active = `## agent: reader\r\n---\r\n${source.replaceAll("\n", "\r\n")}\r\n---\r\nRead files.`;
    expect(transformMarkdownContent(comment + active, removeXMLComments)).toBe('\r\n## agent: reader\r\n---\n{"model":"small","tools":["Read"]}\n---\r\nRead files.');
  });

  it.each(["", "\n---\nMore body"])("preserves document frontmatter boundaries around literal comment markers before %j", suffix => {
    const source = 'description: "Literal <!-- marker"\nmodel: small';
    const value = { description: "Literal <!-- marker", model: "small" };
    installMarkdownMetadata({ version: 2, records: [record(source, value)] });
    const body = "Body --> remains." + suffix;
    const content = `---\n${source}\n---\n${body}`;

    expect(readMarkdownMetadata("agent", content).value).toEqual(value);
    expect(removeDocumentFrontmatter(content)).toBe(body);
    expect(transformMarkdownContent(content, text => text)).toBe(`---\n${JSON.stringify(value)}\n---\n${body}`);
  });

  it.each(["\n", "\r\n"])("keeps literal comment markers in inline metadata separate from body comments with %j offsets", newline => {
    const { removeXMLComments } = require("./runtime_import.cjs");
    const source = 'description: "Literal <!-- marker"\nmodel: small';
    const value = { description: "Literal <!-- marker", model: "small" };
    installMarkdownMetadata({ version: 2, records: [record(source, value), record("model: large", { model: "large" })] });
    const content = ["## agent: first", "---", ...source.split("\n"), "---", "Read files.", "## agent: second", "---", "model: large", "---", "Body --> remains.", "<!--", "## agent: hidden", "---", "model: invalid", "-->", "Done."].join(
      newline
    );
    const expected = ["## agent: first", `---\n${JSON.stringify(value)}\n---`, "Read files.", "## agent: second", '---\n{"model":"large"}\n---', "Body --> remains.", "", "Done."].join(newline);

    expect(require("./markdown_metadata.cjs").missingMarkdownMetadata([content]).size).toBe(0);
    expect(transformMarkdownContent(content, removeXMLComments)).toBe(expected);
  });

  it.each([
    ["---\non: workflow_dispatch\n---\nBody\n", "Body\n"],
    ["\uFEFF---\r\non: workflow_dispatch\r\n---\r\nBody\r\n", "Body\r\n"],
    ["Introduction\n---\nExample prose, not YAML\n---\nConclusion", "Introduction\n---\nExample prose, not YAML\n---\nConclusion"],
  ])("removes only document frontmatter from %j", (content, expected) => {
    expect(removeDocumentFrontmatter(content)).toBe(expected);
  });

  it("rejects unterminated document frontmatter when removing it", () => {
    expect(() => removeDocumentFrontmatter("---\non: workflow_dispatch\nBody")).toThrow("unterminated frontmatter");
  });

  it.each(["", "Introduction\n", "Introduction\r\n"])("recognizes an inline artifact at a heading boundary after %j", prefix => {
    const source = "model: small";
    installMarkdownMetadata({ version: 2, records: [record(source, { model: "small" })] });
    const content = `${prefix}## agent: reader\n---\n${source}\n---\nBody`;
    expect(transformMarkdownContent(content, text => text.replace("small", "resolved"))).toBe(`${prefix}## agent: reader\n---\n{"model":"resolved"}\n---\nBody`);
  });

  it("uses filename identity and no declared settings for agents without compiler metadata", () => {
    const fs = require("node:fs");
    const path = require("node:path");
    const workspace = path.resolve(`.metadata-agents-${require("node:crypto").randomUUID()}`);
    const directory = path.join(workspace, ".github/agents");
    try {
      fs.mkdirSync(directory, { recursive: true });
      fs.writeFileSync(path.join(directory, "reader.agent.md"), "---\ndescription: Not compiled\nmodel: small\n---\nBody");
      expect(require("./copilot_harness.cjs").listCopilotAgentFiles(directory, vi.fn())).toEqual([{ name: "reader", path: path.join(directory, "reader.agent.md") }]);
      expect(require("./engine_subagent_models.cjs").listMarkdownAgentFiles(directory, vi.fn())).toEqual([{ fileName: "reader.agent.md", path: path.join(directory, "reader.agent.md"), name: null }]);
      expect(require("./extract_inline_sub_agents.cjs").loadCopilotSDKCustomAgents(workspace, vi.fn())).toEqual([{ name: "reader", displayName: "reader", prompt: "Body" }]);
    } finally {
      fs.rmSync(workspace, { recursive: true, force: true });
    }
  });

  it("loads BOM/indented agents with their tools and reports malformed staged agents", () => {
    const fs = require("node:fs");
    const path = require("node:path");
    const workspace = path.resolve(`.metadata-staged-${require("node:crypto").randomUUID()}`);
    const directory = path.join(workspace, ".github/agents");
    const source = "model: small\ntools: [Read]";
    try {
      fs.mkdirSync(directory, { recursive: true });
      installMarkdownMetadata({ version: 2, records: [record(source, { model: "small", tools: ["Read"] })] });
      fs.writeFileSync(path.join(directory, "reader.agent.md"), `\uFEFF  ---\n${source}\n \t---\nBody`);
      const logger = vi.fn();
      expect(require("./extract_inline_sub_agents.cjs").loadCopilotSDKCustomAgents(workspace, logger)).toMatchObject([{ name: "reader", model: "small", tools: ["Read"], prompt: "Body" }]);
      fs.writeFileSync(path.join(directory, "invalid.agent.md"), "---\nmodel: small");
      const invalidFile = vi.fn();
      require("./engine_subagent_models.cjs").listMarkdownAgentFiles(directory, logger, { onInvalid: invalidFile });
      expect(invalidFile).toHaveBeenCalledWith(path.join(directory, "invalid.agent.md"), expect.objectContaining({ message: "unterminated frontmatter" }));
    } finally {
      fs.rmSync(workspace, { recursive: true, force: true });
    }
  });

  it("materializes metadata solely from JSON headers, ignoring invalid YAML in the body", async () => {
    const fs = require("node:fs");
    const path = require("node:path");
    const workspace = path.resolve(`.metadata-lock-${require("node:crypto").randomUUID()}`);
    const file = path.join(workspace, ".github/workflows/main.lock.yml");
    const metadata = { version: 2, records: [record("rate: 0.25", { rate: 0.25 })] };
    try {
      fs.mkdirSync(path.dirname(file), { recursive: true });
      fs.writeFileSync(file, `# gh-aw-metadata: {}\n# gh-aw-manifest: ${JSON.stringify({ version: 1, ...require("./markdown_metadata.cjs").metadataManifestFields(metadata) })}\n# compiled fixture\ninvalid: [yaml\n`);
      const loaded = readLocalMarkdownMetadata(file, workspace);
      installMarkdownMetadata(loaded);
      expect(loaded).toEqual({ version: 2, records: metadata.records.map(record => ({ ...record, sha256: record.sha256.slice(0, 20) })) });
      expect(readDocumentMetadata("rate: 0.25").value).toEqual({ rate: 0.25 });
      fs.writeFileSync(file, "# gh-aw-manifest: {}\n# gh-aw-manifest: {}\n# compiled fixture\n");
      expect(readLocalMarkdownMetadata(file, workspace)).toBeUndefined();
    } finally {
      fs.rmSync(workspace, { recursive: true, force: true });
    }
  });
});
