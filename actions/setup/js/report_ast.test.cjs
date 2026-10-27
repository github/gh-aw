// @ts-check

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { createRequire } from "module";

const require = createRequire(import.meta.url);
const { normalizeReport, normalizeReportTitle, redactReportForRendering, assertRedactedReport, renderReport } = require("./report_ast.cjs");
const { parseReportMarkdown } = require("./report_markdown.cjs");

/** @param {import("./report_ast.cjs").ReportBlock[]} children */
const report = children => ({ type: /** @type {const} */ "report", title: " [aw] Smoke:\r\nfailed ", children });
const paragraph = value => ({ type: /** @type {const} */ "paragraph", children: [{ type: /** @type {const} */ "text", value }] });

beforeEach(() => vi.stubEnv("GH_AW_ALLOWED_DOMAINS", "github.com,example.com,example.org"));
afterEach(() => vi.unstubAllEnvs());

describe("report AST", () => {
  it("preserves compact marker identities and substitutes metadata scalars without parsing their contents", () => {
    const children = parseReportMarkdown("<!-- gh-aw-work-issue:{work_id} -->", { context: { work_id: "work_123" }, allowMetadata: true, agentTainted: false });
    expect(children).toEqual([{ type: "xml_marker", name: "gh-aw-work-issue", value: "work_123", compact: true, agentTainted: false }]);
    expect(renderReport(report(children)).body).toBe("<!-- gh-aw-work-issue:work_123 -->\n");
  });

  it("imports a durable review marker as its own block and retains its encoded identity", () => {
    const source = 'Review summary\n[gh-aw-workflow-call-id]: # "owner%2Frepo%2FWorkflow%20name"';
    const children = parseReportMarkdown(source, { allowMetadata: true, agentTainted: false });
    expect(children[1]).toEqual({ type: "review_marker", workflowCallId: "owner/repo/Workflow name", agentTainted: false });
    expect(renderReport(report(children)).body).toBe(source.replace("\n[", "\n\n[") + "\n");
  });

  it.each(["%", "%FF", "%E0%A4"])("keeps malformed review metadata %s literal instead of failing publication", encoded => {
    const children = parseReportMarkdown(`[gh-aw-workflow-call-id]: # "${encoded}"`, { allowMetadata: true, agentTainted: true });
    expect(children).toHaveLength(1);
    expect(children[0].type).toBe("paragraph");
    expect(renderReport(report(children)).body).not.toContain("[gh-aw-workflow-call-id]:");
  });

  it("keeps emphasis and subscript structure when templates use underscored scalar names", () => {
    const children = parseReportMarkdown("<sub><em>{workflow_name}</em></sub>\n\n*{workflow_name}*", { context: { workflow_name: "Worker" }, agentTainted: false });
    expect(renderReport(report(children)).body).toBe("<sub><em>Worker</em></sub>\n\n*Worker*\n");
  });

  it("does not turn multiline literal data into lists or separators", () => {
    const children = parseReportMarkdown("Value: {value}", { context: { value: "first\n- item\n+ another\n1. numbered\n2) alternate\n---" }, agentTainted: true });
    expect(children).toHaveLength(1);
    expect(renderReport(report(children)).body).toBe("Value: first - item + another 1. numbered 2) alternate ---\n");
    expect(renderReport(report([paragraph("Value: first\n- item\n+ another\n1. numbered\n2) alternate\n---")])).body).toBe("Value: first - item + another 1. numbered 2) alternate ---\n");
  });

  it("roundtrips tagged code blocks with a minimum fence longer than embedded backticks", () => {
    const value = "text\n````````\n<!-- gh-aw-execution: literal example -->";
    const body = renderReport(report([{ type: "code_block", language: "gh-aw-comment-memory:notes", fenceLength: 6, value, agentTainted: true }])).body;
    expect(body).toBe("`````````gh-aw-comment-memory:notes\n" + value + "\n`````````\n");
    const imported = parseReportMarkdown(body, { allowMetadata: true, agentTainted: true });
    expect(imported).toEqual([{ type: "code_block", language: "gh-aw-comment-memory:notes", fenceLength: 9, value, agentTainted: true }]);
    expect(renderReport(report(imported)).body).toBe(body);
  });

  it.each([2, 3.5, 129])("rejects invalid minimum fence length %s", fenceLength => {
    expect(() => renderReport(report([{ type: "code_block", language: "text", value: "example", fenceLength }]))).toThrow(TypeError);
  });

  it("requires a non-forgeable redaction pass before a tree can be concretized", () => {
    const input = { ...report([paragraph("Safe agent text")]), agentTainted: false };
    expect(() => assertRedactedReport(input)).toThrow("must pass redaction");
    expect(() => assertRedactedReport(normalizeReport(input))).toThrow("must pass redaction");
    const redacted = redactReportForRendering(input);
    expect(() => assertRedactedReport(redacted)).not.toThrow();
    expect(() => assertRedactedReport(structuredClone(redacted))).toThrow("must pass redaction");
    expect(renderReport(input).body).toBe("Safe agent text\n");
  });

  it("redacts all string-bearing tree surfaces before serialization without changing origin", () => {
    const secret = "tree-redaction-secret-fixture";
    vi.stubEnv("GH_AW_SECRET_NAMES", "TREE_ASSERTION");
    vi.stubEnv("SECRET_TREE_ASSERTION", secret);
    /** @type {import("./report_ast.cjs").Report} */
    const input = {
      type: "report",
      title: secret,
      agentTainted: true,
      preamble: [paragraph(secret)],
      children: [
        {
          type: "paragraph",
          agentTainted: false,
          children: [
            { type: "text", value: "tree-redaction-", agentTainted: false },
            { type: "strong", agentTainted: true, children: [{ type: "code", value: "secret-fixture", agentTainted: true }] },
          ],
        },
        { type: "details", summary: secret, children: [paragraph(secret)] },
        { type: "code_block", language: "text", value: secret },
        { type: "list", ordered: false, items: [[{ type: "task", checked: false, children: [{ type: "text", value: secret }] }]] },
        { type: "table", headers: [[{ type: "text", value: secret }]], rows: [[[{ type: "code", value: secret }]]] },
        { type: "structured_data", data: { [secret]: [secret, { nested: secret }] } },
        { type: "json_marker", name: "evidence", data: { [secret]: secret } },
        { type: "xml_marker", name: "evidence", value: secret, fields: [{ key: "context", value: secret }] },
      ],
      footer: { type: "footer", children: [paragraph(secret)], markers: [{ type: "xml_marker", name: "gh-aw-agentic-workflow", value: secret, fields: [{ key: "context", value: secret }] }] },
    };
    const copy = structuredClone(input);
    const redacted = redactReportForRendering(input);
    expect(() => assertRedactedReport(redacted)).not.toThrow();
    expect(JSON.stringify(redacted)).not.toContain(secret);
    expect(redacted.agentTainted).toBe(true);
    expect(redacted.children[0].agentTainted).toBe(false);
    expect(input).toEqual(copy);
    expect(JSON.stringify(renderReport(input))).not.toContain(secret);
  });

  it("rejects a tainted scalar added after the redaction pass without exposing it in the error", () => {
    const secret = "mutated-tree-secret-fixture";
    vi.stubEnv("GH_AW_SECRET_NAMES", "TREE_ASSERTION");
    vi.stubEnv("SECRET_TREE_ASSERTION", secret);
    const redacted = redactReportForRendering(report([paragraph("Safe")]));
    redacted.title = secret;
    expect(() => assertRedactedReport(redacted)).toThrow("contains an unredacted string");
    try {
      assertRedactedReport(redacted);
    } catch (error) {
      expect(String(error)).not.toContain(secret);
    }
  });

  it("rejects new secrets reassembled across inline formatting after the redaction pass", () => {
    vi.stubEnv("GH_AW_SECRET_NAMES", "TREE_ASSERTION");
    vi.stubEnv("SECRET_TREE_ASSERTION", "mutated-run-secret-fixture");
    const redacted = redactReportForRendering(report([paragraph("Safe")]));
    redacted.children = [
      {
        type: "paragraph",
        children: [
          { type: "text", value: "mutated-run-", agentTainted: false },
          { type: "strong", children: [{ type: "code", value: "secret-fixture", agentTainted: true }] },
        ],
      },
    ];
    expect(() => assertRedactedReport(redacted)).toThrow("unredacted inline run");
  });

  it("rejects encoded strings and nested metadata keys carrying secrets", () => {
    const secret = "encoded-tree-secret-fixture";
    vi.stubEnv("GH_AW_SECRET_NAMES", "TREE_ASSERTION");
    vi.stubEnv("SECRET_TREE_ASSERTION", secret);
    const encoded = Array.from(secret, character => `&#${character.charCodeAt(0)};`).join("");
    const title = redactReportForRendering(report([paragraph("Safe")]));
    title.title = encoded;
    expect(() => assertRedactedReport(title)).toThrow("unredacted string");
    const metadata = redactReportForRendering(report([paragraph("Safe")]));
    metadata.children.push({ type: "structured_data", data: { safe: [{ [encoded]: "value" }] } });
    expect(() => assertRedactedReport(metadata)).toThrow("unredacted string");
  });

  it("keeps safe agent strings taint-checked without relabeling their origin", () => {
    const redacted = redactReportForRendering(
      report([
        {
          type: "paragraph",
          agentTainted: false,
          children: [
            { type: "text", value: "Framework: ", agentTainted: false },
            { type: "text", value: "benign agent content", agentTainted: true },
          ],
        },
      ])
    );
    expect(() => assertRedactedReport(redacted)).not.toThrow();
    expect(redacted.children[0]).toEqual({
      type: "paragraph",
      agentTainted: false,
      children: [
        { type: "text", value: "Framework: ", agentTainted: false },
        { type: "text", value: "benign agent content", agentTainted: true },
      ],
    });
  });

  it("rechecks current masks rather than trusting a stale redaction proof", () => {
    const secret = "newly-registered-secret-fixture";
    const redacted = redactReportForRendering(report([paragraph(secret)]));
    vi.stubEnv("GH_AW_SECRET_NAMES", "TREE_ASSERTION");
    vi.stubEnv("SECRET_TREE_ASSERTION", secret);
    expect(() => assertRedactedReport(redacted)).toThrow("unredacted string");
    expect(renderReport(redacted).body).not.toContain(secret);
  });

  it("rejects cycles added after the redaction pass", () => {
    const redacted = redactReportForRendering(report([paragraph("Safe")]));
    redacted.children.push({ type: "quote", children: redacted.children });
    expect(() => assertRedactedReport(redacted)).toThrow("must not contain cycles");
  });

  it("fails closed if escaping reconstructs a secret in the concrete output", () => {
    vi.stubEnv("GH_AW_SECRET_NAMES", "TREE_ASSERTION");
    vi.stubEnv("SECRET_TREE_ASSERTION", "\\*literal\\*");
    expect(() => renderReport(report([paragraph("*literal*")]))).toThrow("unredacted string");
  });

  it("normalizes deterministically without mutating the input", () => {
    const input = report([
      paragraph(""),
      { type: "heading", level: 1, children: [{ type: "text", value: "What happened" }] },
      { type: "details", summary: " Runtime\n evidence ", children: [paragraph("  e\u0301vidence\r\nhere  ")] },
      { type: "details", summary: "Empty", children: [] },
    ]);
    const copy = structuredClone(input);
    const normalized = normalizeReport(input);
    expect(input).toEqual(copy);
    expect(normalizeReport(normalized)).toEqual(normalized);
    expect(renderReport(input)).toEqual({
      title: "[aw] Smoke: failed",
      body: "## What happened\n\n<details>\n<summary>Runtime evidence</summary>\n\n\u00e9vidence here\n\n</details>\n",
    });
  });

  it("escapes literal metadata and HTML rather than allowing new sections or mentions", () => {
    const { body } = renderReport(report([paragraph("# injected\n</details><img src=x> **bold** [click](javascript:alert(1)) @octocat &amp; text")]));
    expect(body).not.toContain("\n# injected");
    expect(body).not.toContain("<img");
    expect(body).not.toContain("</details>");
    expect(body).toContain("\\# injected &lt;/details&gt;&lt;img src=x&gt;");
    expect(body).toContain("\\*\\*bold\\*\\*");
    expect(body).toContain("\\[click\\]");
    expect(body).toContain("`@octocat` &amp; text");
  });

  it("chooses fences after normalization and redaction", () => {
    vi.stubEnv("GH_AW_SECRET_NAMES", "REPORT_TEST");
    vi.stubEnv("SECRET_REPORT_TEST", "report-secret-fixture");
    const { body } = renderReport(
      report([
        { type: "code_block", language: "text", value: "report-secret-fixture\r\n&#96;&#96;&#96;&#96;\n</details>\n# injected" },
        { type: "paragraph", children: [{ type: "code", value: "`edge`" }] },
      ])
    );
    expect(body).not.toContain("report-secret-fixture");
    expect(body).toContain("`````text\n");
    expect(body).toContain("\n````\n</details>\n# injected\n`````");
    expect(body).toContain("`` `edge` ``");
  });

  it("escapes HTML summaries without Markdown-specific backslashes", () => {
    const { body } = renderReport(report([{ type: "details", summary: 'Evidence [1] </summary><img src="x"> & data', children: [paragraph("details")] }]));
    expect(body).toContain('<summary>Evidence [1] &lt;/summary&gt;&lt;img src="x"&gt; &amp; data</summary>');
  });

  it.each(["<details><summary>", "<details>\n<summary>"])("preserves bold summaries imported from %s", opening => {
    const source = `${opening}<b>View Detailed Findings</b></summary>\n\nFindings\n\n</details>`;
    const { body } = renderReport(report(parseReportMarkdown(source)));
    expect(body).toBe("<details>\n<summary><b>View Detailed Findings</b></summary>\n\nFindings\n\n</details>\n");
    expect(renderReport(report(parseReportMarkdown(body))).body).toBe(body);
  });

  it("preserves nested summary formatting without interpreting Markdown or scalar markup", () => {
    const source = "<details><summary><strong>View <b>nested</b> <em>findings</em> {label} & data [1] **literal**</strong></summary>\n\nFindings\n\n</details>";
    const { body } = renderReport(report(parseReportMarkdown(source, { context: { label: "</b><img src=x onerror=attack()>@user" } })));
    expect(body).toContain("<summary><b>View <b>nested</b> <em>findings</em> &lt;/b&gt;&lt;img src=x onerror=attack()&gt;<code>@user</code> &amp; data [1] **literal**</b></summary>");
    expect(body).not.toContain("<img");
  });

  it("does not promote encoded tags or scalar substitutions into summary formatting on round trips", () => {
    const source = "<details><summary><b>{label} &lt;b&gt;literal&lt;/b&gt;</b></summary>\n\nFindings\n\n</details>";
    const { body } = renderReport(report(parseReportMarkdown(source, { context: { label: "<b>scalar</b>" } })));
    expect(body).toContain("<summary><b>&lt;b&gt;scalar&lt;/b&gt; &lt;b&gt;literal&lt;/b&gt;</b></summary>");
    expect(renderReport(report(parseReportMarkdown(body))).body).toBe(body);
  });

  it.each(["<b onclick=attack()>Label</b>", "<script>attack()</script>", "<b>Unclosed", "</summary><img src=x>"])("keeps unsupported or unmatched summary markup literal: %s", value => {
    const source = `<details><summary>${value}</summary>\n\nFindings\n\n</details>`;
    const { body } = renderReport(report(parseReportMarkdown(source)));
    expect(body).toContain(`<summary>${value.replace(/</g, "&lt;").replace(/>/g, "&gt;")}</summary>`);
  });

  it("keeps mentions and code neutralized inside formatted summaries across round trips", () => {
    const source = "<details><summary><b>@admin <code>&lt;img&gt; @user</code> admin@example.com</b></summary>\n\nFindings\n\n</details>";
    const { body } = renderReport(report(parseReportMarkdown(source)));
    expect(body).toContain("<summary><b><code>@admin</code> <code>&lt;img&gt; @user</code> admin@example.com</b></summary>");
    expect(renderReport(report(parseReportMarkdown(body))).body).toBe(body);
  });

  it("redacts secrets split across summary formatting and rejects stale redaction proofs", () => {
    const secret = "summary-secret-fixture";
    vi.stubEnv("GH_AW_SECRET_NAMES", "SUMMARY_SECRET");
    vi.stubEnv("SECRET_SUMMARY_SECRET", secret);
    const input = report([
      {
        type: "details",
        summary: [
          { type: "text", value: "summary-" },
          { type: "strong", children: [{ type: "text", value: "secret-fixture" }] },
        ],
        children: [paragraph("Findings")],
      },
    ]);
    expect(renderReport(input).body).not.toContain(secret);
    expect(renderReport(input).body).not.toContain("secret-fixture");
    const redacted = redactReportForRendering(input);
    redacted.children[0].summary = [
      { type: "text", value: "summary-" },
      { type: "strong", children: [{ type: "text", value: "secret-fixture" }] },
    ];
    expect(() => assertRedactedReport(redacted)).toThrow("unredacted inline run");
  });

  it("rejects excessively nested summary formatting", () => {
    expect(() => parseReportMarkdown(`<details><summary>${"<b>".repeat(18)}Label${"</b>".repeat(18)}</summary>\n\nFindings\n\n</details>`)).toThrow("maximum depth");
  });

  it("escapes actual mentions without corrupting email addresses or action versions", () => {
    const value = "@admin admin@example.com actions/checkout@v5 (@team/dev) @last";
    const { body } = renderReport(report([paragraph(value), { type: "details", summary: value, children: [paragraph("Evidence")] }]));
    expect(body).toContain("`@admin` admin@example.com actions/checkout@v5 (`@team/dev`) `@last`");
    expect(body).toContain("<summary><code>@admin</code> admin@example.com actions/checkout@v5 (<code>@team/dev</code>) <code>@last</code></summary>");
    expect(renderReport(report([paragraph("@first@second")])).body).toBe("`@first`@second\n");
  });

  it("validates images and escapes alt text without introducing HTML or notifications", () => {
    vi.stubEnv("GH_AW_ALLOWED_DOMAINS", "example.com");
    const input = report([{ type: "paragraph", children: [{ type: "image", url: "https://example.com/chart(a).png", alt: "Chart ] <img> @user & data" }] }]);
    const { body } = renderReport(input);
    expect(body).toBe("![Chart \\] &lt;img&gt; &#64;user &amp; data](https://example.com/chart%28a%29.png)\n");
    expect(renderReport(report(parseReportMarkdown(body))).body).toBe(body);
    expect(normalizeReport(normalizeReport(input))).toEqual(normalizeReport(input));
    expect(() => renderReport(report([{ type: "paragraph", children: [{ type: "image", url: "http://example.com/chart.png", alt: "Chart" }] }]))).toThrow("HTTPS");
  });

  it("renders only explicit links and encodes Markdown destination delimiters", () => {
    const { body } = renderReport(report([{ type: "paragraph", children: [{ type: "link", url: "https://example.com/run(a)?q='value'", children: [{ type: "text", value: "Run [1]" }] }] }]));
    expect(body).toBe("[Run \\[1\\]](https://example.com/run%28a%29?q=%27value%27)\n");
  });

  it.each(["javascript:alert(1)", "data:text/html,test", "file:///tmp/test", "http://example.com", "https://user:password@example.com", "https://example.com/\n#injected", "//example.com"])("rejects unsafe link %s", url => {
    expect(() => renderReport(report([{ type: "paragraph", children: [{ type: "link", url, children: [{ type: "text", value: "Run" }] }] }]))).toThrow();
  });

  it("normalizes lists, alerts, tables, and nested heading levels", () => {
    const { body } = renderReport(
      report([
        { type: "list", ordered: true, items: [[paragraph("First")], [], [paragraph("Second")]] },
        { type: "alert", kind: "WARNING", children: [paragraph("Failure"), { type: "heading", level: 1, children: [{ type: "text", value: "Evidence" }] }] },
        { type: "table", headers: [[{ type: "text", value: "Tool" }]], rows: [[[{ type: "code", value: "a|b" }]], [[{ type: "text", value: "c|d" }]]] },
      ])
    );
    expect(body).toContain("1. First\n2. Second");
    expect(body).toContain("> [!WARNING]\n> Failure\n>\n> ### Evidence");
    expect(body).toContain("| `a\\|b` |");
    expect(body).toContain("| c\\|d |");
  });

  it("rejects raw nodes, malformed tables, invalid languages, and excessive nesting", () => {
    expect(() => renderReport(report([/** @type {never} */ { type: "raw", value: "<img>" }]))).toThrow("Unsupported report block");
    expect(() => renderReport(report([{ type: "table", headers: [[{ type: "text", value: "One" }]], rows: [[]] }]))).toThrow("header width");
    expect(() => renderReport(report([{ type: "code_block", language: "text\n</details>", value: "payload" }]))).toThrow("code language");
    expect(() => renderReport(report([{ type: "alert", kind: "INVALID", children: [paragraph("data")] }]))).toThrow("alert kind");
    const children = [];
    children.push({ type: "details", summary: "Cycle", children });
    expect(() => renderReport(report(children))).toThrow("maximum depth");
  });

  it("normalizes and bounds titles without Markdown escaping or broken Unicode", () => {
    expect(normalizeReportTitle("[aw]\u202e Smoke\r\n[model]:\x1b[31m failed\x1b[0m")).toBe("[aw] Smoke [model]: failed");
    expect(Array.from(normalizeReportTitle("\u{1f680}".repeat(300)))).toHaveLength(256);
  });

  it("stabilizes entity decoding, merged inline whitespace, and secrets exposed by controls", () => {
    vi.stubEnv("GH_AW_SECRET_NAMES", "REPORT_TEST");
    vi.stubEnv("SECRET_REPORT_TEST", "report-secret-fixture");
    const input = report([
      {
        type: "paragraph",
        children: [
          { type: "text", value: "&amp;amp;amp;amp;amp;lt;tag&amp;amp;amp;amp;amp;gt; " },
          { type: "text", value: " report-secret-\u0000fixture" },
          { type: "code", value: "one " },
          { type: "code", value: " two" },
        ],
      },
    ]);
    const normalized = normalizeReport(input);
    expect(normalizeReport(normalized)).toEqual(normalized);
    expect(renderReport(input).body).not.toContain("report-secret-fixture");
    expect(renderReport(input).body).toContain("&lt;tag&gt;");
    expect(renderReport(input).body).toContain("`one two`");
  });

  it.each(["text", "code"])("redacts complete %s rendering runs without changing provenance", type => {
    vi.stubEnv("GH_AW_SECRET_NAMES", "RENDER_PROVENANCE");
    vi.stubEnv("SECRET_RENDER_PROVENANCE", "render-provenance-secret-fixture");
    for (const values of [
      ["render-provenance-", "se", "cret-fixture"],
      ["render-provenance-&", "#115;", "ecret-fixture"],
    ]) {
      const children = values.map((value, index) => ({ type, value, ...(index === 1 ? {} : { agentTainted: index === 2 }) }));
      const input = report([{ type: "paragraph", children }]);
      const copy = structuredClone(input);
      const normalized = normalizeReport(input);
      expect(normalized.children[0].children).toEqual(children);
      const body = renderReport(input).body;
      expect(body).not.toContain("render-provenance-");
      expect(body).not.toContain("secret-fixture");
      expect(input).toEqual(copy);
      expect(normalizeReport(normalized)).toEqual(normalized);
    }
  });

  it("redacts registered credentials across complete rendering runs and singleton AST nodes", () => {
    const openAISecret = `sk-proj-${"a".repeat(64)}${"b".repeat(16)}`;
    const githubSecret = `ghs_${"A".repeat(36)}.TAIL`;
    const crossTypeSecret = "cross-type-render-secret-fixture";
    const crossFormatSecret = "cross-format-render-secret-fixture";
    const strongSecret = "nested-strong-secret-fixture";
    vi.stubEnv("GH_AW_SECRET_NAMES", "RENDER_OPENAI_SECRET,RENDER_GITHUB_SECRET,RENDER_TEXT_CODE_SECRET,RENDER_FORMAT_SECRET,RENDER_STRONG_SECRET");
    vi.stubEnv("SECRET_RENDER_OPENAI_SECRET", openAISecret);
    vi.stubEnv("SECRET_RENDER_GITHUB_SECRET", githubSecret);
    vi.stubEnv("SECRET_RENDER_TEXT_CODE_SECRET", crossTypeSecret);
    vi.stubEnv("SECRET_RENDER_FORMAT_SECRET", crossFormatSecret);
    vi.stubEnv("SECRET_RENDER_STRONG_SECRET", strongSecret);
    const input = report([
      {
        type: "paragraph",
        children: [
          { type: "text", value: "sk-proj-", agentTainted: false },
          { type: "text", value: "a".repeat(64), agentTainted: true },
          { type: "text", value: "b".repeat(16), agentTainted: false },
        ],
      },
      {
        type: "paragraph",
        children: [
          { type: "code", value: `ghs_${"A".repeat(36)}`, agentTainted: true },
          { type: "code", value: ".TAIL", agentTainted: true },
        ],
      },
      {
        type: "paragraph",
        children: [
          { type: "text", value: "cross-type-render-", agentTainted: false },
          { type: "code", value: "secret-fixture", agentTainted: true },
        ],
      },
      {
        type: "paragraph",
        children: [
          { type: "text", value: "cross-format-render-secret-", agentTainted: false },
          { type: "strong", agentTainted: true, children: [{ type: "text", value: "fixture", agentTainted: true }] },
          { type: "strong", agentTainted: false, children: [{ type: "text", value: "Safe emphasis", agentTainted: false }] },
          { type: "text", value: " ", agentTainted: false },
          { type: "link", url: "https://docs.example/", agentTainted: false, children: [{ type: "text", value: "docs", agentTainted: false }] },
        ],
      },
      {
        type: "paragraph",
        children: [
          { type: "strong", agentTainted: true, children: [{ type: "text", value: strongSecret, agentTainted: true }] },
          { type: "text", value: " ", agentTainted: false },
          { type: "emphasis", agentTainted: false, children: [{ type: "text", value: "Independent emphasis", agentTainted: false }] },
          { type: "text", value: " ", agentTainted: false },
          { type: "link", url: "https://guide.example/", agentTainted: false, children: [{ type: "text", value: "guide", agentTainted: false }] },
        ],
      },
    ]);
    const body = renderReport(input).body;
    expect(body).not.toContain(openAISecret);
    expect(body).not.toContain("b".repeat(16));
    expect(body).not.toContain(githubSecret);
    expect(body).not.toContain(".TAIL");
    expect(body).not.toContain(crossTypeSecret);
    expect(body).not.toContain("cross-type-render-");
    expect(body).not.toContain(crossFormatSecret);
    expect(body).not.toContain("cross-format-render-secret-");
    expect(body).not.toContain(strongSecret);
    expect(body).toContain("**Safe emphasis** [docs](https://docs.example/)");
    expect(body).toContain("*Independent emphasis* [guide](https://guide.example/)");
  });

  it.each(
    ["text", "code"].flatMap(type =>
      [
        { origin: "trusted", provenance: [false, false, false] },
        { origin: "tainted", provenance: [true, true, true] },
        { origin: "unspecified", provenance: [undefined, undefined, undefined] },
        { origin: "alternating", provenance: [false, true, false] },
      ].map(scenario => ({ type, ...scenario }))
    )
  )("redacts prefix-matching credentials in complete $origin $type runs and singletons", ({ type, provenance }) => {
    vi.stubEnv("GH_AW_SECRET_NAMES", "RENDER_PREFIX_SECRET");
    for (const parts of [
      ["sk-proj-", "a".repeat(64), "b".repeat(16)],
      ["ghs_", "A".repeat(36), ".TAIL"],
    ]) {
      const secret = parts.join("");
      vi.stubEnv("SECRET_RENDER_PREFIX_SECRET", secret);
      const children = parts.map((value, index) => ({ type, value, ...(provenance[index] === undefined ? {} : { agentTainted: provenance[index] }) }));
      const input = report([{ type: "paragraph", children }]);
      const copy = structuredClone(input);
      const normalized = normalizeReport(input);
      const singleton = report([{ type: "paragraph", children: [{ type, value: secret, ...(provenance[0] === undefined ? {} : { agentTainted: provenance[0] }) }] }]);
      for (const candidate of [input, normalized, singleton]) {
        const body = renderReport(candidate).body;
        expect(body).not.toContain(secret);
        expect(body).not.toContain(parts[0]);
        expect(body).not.toContain(parts[2]);
      }
      expect(input).toEqual(copy);
      expect(normalizeReport(normalized)).toEqual(normalized);
      if (provenance[0] !== provenance[1]) expect(normalized.children[0].children).toEqual(children);
      else expect(normalized.children[0].children).toHaveLength(1);
    }
  });

  it.each([
    ["@", "octocat"],
    ["&", "#64;octocat"],
    ["&", "#x1b;[31m@octocat"],
  ])("neutralizes mentions and controls split across text provenance: %s + %s", (prefix, suffix) => {
    const input = report([
      {
        type: "paragraph",
        children: [
          { type: "text", value: prefix, agentTainted: false },
          { type: "text", value: suffix, agentTainted: true },
        ],
      },
    ]);
    expect(renderReport(input).body).toBe("`@octocat`\n");
    expect(normalizeReport(input).children[0].children).toHaveLength(2);
  });

  it("uses one valid code span across provenance boundaries without merging AST nodes", () => {
    const input = report([
      {
        type: "paragraph",
        children: [
          { type: "text", value: "Value: ", agentTainted: false },
          { type: "code", value: "prefix`", agentTainted: false },
          { type: "code", value: "`suffix", agentTainted: true },
        ],
      },
    ]);
    const copy = structuredClone(input);
    const body = renderReport(input).body;
    expect(body).toBe("Value: ```prefix``suffix```\n");
    expect(parseReportMarkdown(body)[0]).toEqual({
      type: "paragraph",
      children: [
        { type: "text", value: "Value: " },
        { type: "code", value: "prefix``suffix" },
      ],
    });
    expect(normalizeReport(input).children[0].children).toEqual(input.children[0].children);
    expect(input).toEqual(copy);
  });

  it("escapes ordered-list syntax in literal paragraphs and refuses nested links", () => {
    expect(renderReport(report([paragraph("1. not a list")])).body).toBe("1\\. not a list\n");
    expect(() =>
      renderReport(
        report([
          {
            type: "paragraph",
            children: [{ type: "link", url: "https://example.com", children: [{ type: "link", url: "https://example.org", children: [{ type: "text", value: "nested" }] }] }],
          },
        ])
      )
    ).toThrow("cannot contain other links");
  });
});

describe("report Markdown import boundary", () => {
  it("imports typed inline slots inside quotes without enabling scalar template substitution", () => {
    const children = parseReportMarkdown("<details><summary>Report {unknown}</summary>\n\n> Before {unknown}\n> {claim}\n> After\n\n</details>\n\n{toString}\n\n`{unknown}`", {
      inlineSlots: { claim: [{ type: "code", value: "`@user {unknown}`" }] },
    });
    expect(children[0]).toMatchObject({ type: "details", children: [{ type: "quote", children: [{ type: "paragraph" }] }] });
    expect(renderReport(report(children)).body).toBe("<details>\n<summary>Report {unknown}</summary>\n\n> Before {unknown}\n> `` `@user {unknown}` ``\n> After\n\n</details>\n\n{toString}\n\n`{unknown}`\n");
  });

  it("parses template structure before substituting hostile scalar values", () => {
    const children = parseReportMarkdown("## What happened\n\n**{workflow}** failed.\n\n**Branch:** `{branch}`\n\n[Run]({url})", {
      minimumHeadingLevel: 2,
      context: { workflow: "Smoke\n\n## Injected\n[evil](javascript:x)", branch: "`\n</details>", url: "https://example.com/run(test)" },
    });

    const { body } = renderReport(report(children));
    expect(body.match(/^## /gm)).toEqual(["## "]);
    expect(body).toContain("Smoke \\#\\# Injected");
    expect(body).toContain("`` ` </details> ``");
    expect(body).toContain("[Run](https://example.com/run%28test%29)");
  });

  it("preserves nested details, code, alerts, lists, tables, and inline formatting", () => {
    const markdown = [
      "<details>",
      "<summary>Runtime</summary>",
      "",
      "> [!WARNING]",
      "> **Missing tool:** `github.read`",
      "",
      "### Evidence",
      "",
      "- First",
      "  - Nested",
      "- Second",
      "",
      "| Tool | Result |",
      "| --- | --- |",
      "| `gh` | *missing* |",
      "",
      "````text",
      "</details>",
      "```",
      "````",
      "",
      "</details>",
    ].join("\n");
    const { body } = renderReport(report(parseReportMarkdown(markdown)));
    expect(body).toContain("> [!WARNING]\n> **Missing tool:** `github.read`");
    expect(body).toContain("- First\n  \n  - Nested\n- Second");
    expect(body).toContain("| `gh` | *missing* |");
    expect(body).toContain("````text\n</details>\n```\n````");
    expect(body.endsWith("\n</details>\n")).toBe(true);
  });

  it("does not promote diagnostic headings to report sections or permit arbitrary HTML", () => {
    const { body } = renderReport(report(parseReportMarkdown("## Injected\n\n<img src=x>\n\n</details>\n\n[unsafe](javascript:alert)\n\n[Run](https://example.com) · [Source](https://example.org)")));
    expect(body).toContain("### Injected");
    expect(body).not.toContain("<img");
    expect(body).not.toContain("\n</details>");
    expect(body).toContain("link unavailable: invalid HTTPS URL");
    expect(body).toContain("[Run](https://example.com/) · [Source](https://example.org/)");
  });

  it("preserves source soft breaks without allowing scalar values to introduce new lines", () => {
    const children = parseReportMarkdown("Body\r\nRelated to #42\n**{value}**", { context: { value: "First\n# injected" } });
    const input = report(children);
    expect(normalizeReport(normalizeReport(input))).toEqual(normalizeReport(input));
    expect(renderReport(input).body).toBe("Body\nRelated to #42\n**First \\# injected**\n");
    expect(renderReport(report(parseReportMarkdown("First\n1\\. literal text\n\\+ literal text"))).body).toBe("First\n1\\. literal text\n\\+ literal text\n");
  });

  it("preserves both Markdown hard-break forms while keeping scalar newlines literal", () => {
    const children = parseReportMarkdown("Agent job: {job}  \nAgent job conclusion: failure\\\nFailing step: test", { context: { job: "build  \n# injected" } });
    const input = report(children);
    expect(children[0].children.filter(node => node.type === "hard_break")).toHaveLength(2);
    expect(renderReport(input).body).toBe("Agent job: build \\# injected  \nAgent job conclusion: failure  \nFailing step: test\n");
    expect(normalizeReport(normalizeReport(input))).toEqual(normalizeReport(input));
    expect(renderReport(report(parseReportMarkdown(renderReport(input).body))).body).toBe(renderReport(input).body);
  });

  it.each(["report-secret#123", "report-secret**123**", "report-secret@user", "report-secret[run](https://example.com)"])("redacts a complete normalized fragment before tokenizing %s", secret => {
    vi.stubEnv("GH_AW_SECRET_NAMES", "FRAGMENT_REPORT");
    vi.stubEnv("SECRET_FRAGMENT_REPORT", secret);
    const body = renderReport(report(parseReportMarkdown(`Before ${secret.replace("#", "&#35;")} after`))).body;
    expect(body).not.toContain("report-secret");
    expect(body).toContain("Before ");
    expect(body).toContain(" after");
  });

  it("imports balanced and escaped URL parentheses and rejects non-HTTPS images", () => {
    vi.stubEnv("GH_AW_ALLOWED_DOMAINS", "github.com,example.com");
    const source = "[Compare](https://github.com/o/r/compare/main...fix(parser)?expand=1&title=Fix(parser)) [Escaped](https://example.com/a\\(b\\)) ![Chart](https://example.com/chart(a).png) ![Unsafe](javascript:alert(1))";
    const { body } = renderReport(report(parseReportMarkdown(source)));
    expect(body).toBe(
      "[Compare](https://github.com/o/r/compare/main...fix%28parser%29?expand=1&title=Fix%28parser%29) [Escaped](https://example.com/a%28b%29) ![Chart](https://example.com/chart%28a%29.png) Unsafe (image unavailable: URL must use an allowed HTTPS domain)\n"
    );
  });

  it("imports bare URLs without absorbing surrounding punctuation or escaping destinations as prose", () => {
    const url = "https://github.com/o/r/blob/main/create_issue.cjs#L1118";
    const source = `See (${url}), https://example.com/run(a). https://example.com/query?a=1&b=2!`;
    const { body } = renderReport(report(parseReportMarkdown(source)));
    expect(body).toBe(
      `See ([https://github.com/o/r/blob/main/create\\_issue.cjs\\#L1118](${url})), [https://example.com/run(a)](https://example.com/run%28a%29). [https://example.com/query?a=1&amp;b=2](https://example.com/query?a=1&b=2)\\!\n`
    );
    expect(renderReport(report(parseReportMarkdown(body))).body).toBe(body);
  });

  it("imports block fields explicitly and refuses missing fields or blocks in scalar positions", () => {
    expect(() => parseReportMarkdown("{missing}", { context: {} })).toThrow("Missing report template field");
    expect(() => parseReportMarkdown("{toString}", { context: {} })).toThrow("Missing report template field");
    expect(() => parseReportMarkdown("**{blocks}**", { context: { blocks: [paragraph("data")] } })).toThrow("cannot appear inline");
    const { body } = renderReport(report(parseReportMarkdown("{one}{two}", { context: { one: [paragraph("First")], two: [] } })));
    expect(body).toBe("First\n");
  });

  it("bounds recursive details and preserves unmatched inline delimiters as literal text", () => {
    expect(() => parseReportMarkdown("<details>\n<summary>Nested</summary>\n".repeat(20) + "payload")).toThrow("maximum depth");
    const { body } = renderReport(report(parseReportMarkdown("An unmatched `` delimiter and a_b_c identifier")));
    expect(body).toContain("An unmatched \\`\\` delimiter and a\\_b\\_c identifier");
  });
});

describe("semantic report nodes", () => {
  it("renders closing directives with explicit activation and validated targets", () => {
    const input = report([
      {
        type: "paragraph",
        children: [
          { type: "closing_reference", action: "closes", target: { number: 42 }, enabled: true },
          { type: "text", value: "; " },
          { type: "closing_reference", action: "resolves", target: { repository: "owner/repo", number: "57" }, enabled: false },
        ],
      },
    ]);
    expect(renderReport(input).body).toBe("Closes #42; Resolves owner/repo\\#57\n");
    expect(normalizeReport(normalizeReport(input))).toEqual(normalizeReport(input));
    expect(renderReport(report([paragraph("Closes #42")])).body).toBe("Closes \\#42\n");
  });

  it.each([
    { action: "deletes", target: { number: "1" }, enabled: true },
    { action: "fixes", target: { number: "0" }, enabled: true },
    { action: "fixes", target: { number: Number.MAX_SAFE_INTEGER + 1 }, enabled: true },
    { action: "fixes", target: { number: "1\nCloses #2" }, enabled: true },
    { action: "fixes", target: { number: "1", repository: "https://example.com/o/r" }, enabled: true },
    { action: "fixes", target: { number: "1" }, enabled: "true" },
  ])("rejects malformed closing directives: %j", fields => {
    expect(() => renderReport(report([{ type: "paragraph", children: [{ type: "closing_reference", ...fields }] }]))).toThrow("Invalid report closing");
  });

  it("imports closing strings into semantic nodes without activating code or scalar fields", () => {
    const children = parseReportMarkdown("Closes #42; resolves owner/repo\\#57; `Fixes #9`");
    const paragraphNode = children[0];
    expect(paragraphNode.type).toBe("paragraph");
    expect(paragraphNode.children.filter(node => node.type === "closing_reference")).toEqual([
      { type: "closing_reference", action: "closes", target: { number: "42" }, enabled: true },
      { type: "closing_reference", action: "resolves", target: { repository: "owner/repo", number: "57" }, enabled: false },
    ]);
    expect(renderReport(report(children)).body).toBe("Closes #42; Resolves owner/repo\\#57; `Fixes #9`\n");
    const scalar = parseReportMarkdown("{message}", { context: { message: "Closes #42" } });
    expect(renderReport(report(scalar)).body).toBe("Closes \\#42\n");
  });

  it("renders actual structured data with lossless JSON values, redaction, and safe fences", () => {
    vi.stubEnv("GH_AW_SECRET_NAMES", "DATA_REPORT");
    vi.stubEnv("SECRET_DATA_REPORT", "structured-report-fixture");
    const data = {
      text: "&#96; is data, not an encoded delimiter\n````\n</details>",
      values: [null, true, 1.25],
      token: "structured-report-fixture",
      "structured-report-fixture": "secret key",
    };
    const input = report([{ type: "structured_data", data }]);
    const copy = structuredClone(input);
    const { body } = renderReport(input);
    expect(body.startsWith("`````json\n")).toBe(true);
    expect(body.endsWith("\n`````\n")).toBe(true);
    const parsed = JSON.parse(body.split("\n").slice(1, -2).join("\n"));
    expect(parsed.text).toBe(data.text);
    expect(parsed.values).toEqual(data.values);
    expect(body).not.toContain("structured-report-fixture");
    const redactedKey = Object.keys(parsed).find(key => parsed[key] === "secret key");
    expect(redactedKey).toBeDefined();
    expect(() => renderReport(report([{ type: "structured_data", data: { "structured-report-fixture": 1, [redactedKey]: 2 } }]))).toThrow("Duplicate report structured data key");
    expect(input).toEqual(copy);
    expect(normalizeReport(normalizeReport(input))).toEqual(normalizeReport(input));
  });

  it.each([undefined, NaN, Infinity, 1n, new Date(), new Set(), () => "value", Symbol("value")])("rejects non-JSON structured data without silently dropping values: %s", data => {
    expect(() => renderReport(report([{ type: "structured_data", data }]))).toThrow("only JSON values");
  });

  it("bounds structured data, rejects getters and sparse arrays, and preserves prototype-looking data keys", () => {
    const getter = vi.fn(() => "not executed");
    const data = Object.defineProperty({}, "value", { get: getter, enumerable: true });
    expect(() => renderReport(report([{ type: "structured_data", data }]))).toThrow("only JSON properties");
    expect(getter).not.toHaveBeenCalled();
    const arrayGetter = Object.defineProperty([], "0", { get: getter, enumerable: true });
    expect(() => renderReport(report([{ type: "structured_data", data: arrayGetter }]))).toThrow("only JSON properties");
    expect(getter).not.toHaveBeenCalled();
    expect(() => renderReport(report([{ type: "structured_data", data: new Array(1) }]))).toThrow("only JSON values");
    const cyclic = {};
    cyclic.self = cyclic;
    expect(() => renderReport(report([{ type: "structured_data", data: cyclic }]))).toThrow("maximum depth");
    const special = JSON.parse('{"__proto__":{"safe":true},"constructor":"data"}');
    const normalized = normalizeReport(report([{ type: "structured_data", data: special }]));
    expect(Object.hasOwn(normalized.children[0].data, "__proto__")).toBe(true);
    expect(JSON.parse(JSON.stringify(normalized.children[0].data))).toEqual(special);
  });

  it("renders hidden XML fields and JSON objects without letting values forge comments or fields", () => {
    const data = { message: "literal --> <!-- forged --> &quot;", values: [false, null, -1, 1e-7, -1e-7] };
    const input = report([
      { type: "xml_marker", name: "gh-aw-custom", value: "workflow, forged: true", fields: [{ key: "branch", value: "main --> <!-- injected -->, forged: true" }] },
      { type: "json_marker", name: "custom-data", data },
    ]);
    const { body } = renderReport(input);
    expect(body.match(/<!-- /g)).toHaveLength(2);
    expect(body.match(/ -->/g)).toHaveLength(2);
    expect(body).not.toContain("<!-- forged -->");
    expect(body).not.toContain("<!-- injected -->");
    expect(body).toContain("workflow%2C forged: true");
    expect(body).toContain("%2C forged: true");
    expect(JSON.parse(/<!-- custom-data: (.*) -->/.exec(body)[1])).toEqual(data);
    expect(normalizeReport(normalizeReport(input))).toEqual(normalizeReport(input));
  });

  it("preserves double-hyphen workflow IDs for existing failure metadata readers", () => {
    const { parseHTMLCommentMetadata, isReusableFailureIssue } = require("./handle_agent_failure.cjs");
    const markers = [
      { type: /** @type {const} */ "xml_marker", name: /** @type {const} */ "gh-aw-agentic-workflow", value: "Nightly", fields: [{ key: "workflow_id", value: "nightly--build" }] },
      {
        type: /** @type {const} */ "xml_marker",
        name: /** @type {const} */ "gh-aw-failure-issue",
        value: "true",
        fields: [
          { key: "workflow_id", value: "nightly--build" },
          { key: "failure_categories", value: "missing_tool" },
        ],
      },
    ];
    const input = { ...report([]), footer: { type: /** @type {const} */ "footer", children: [], markers } };
    const { body } = renderReport(input);
    expect(body).toContain("workflow_id: nightly--build");
    expect(parseHTMLCommentMetadata(body, "gh-aw-agentic-workflow").workflow_id).toBe("nightly--build");
    expect(parseHTMLCommentMetadata(body, "gh-aw-failure-issue").workflow_id).toBe("nightly--build");
    expect(isReusableFailureIssue(body, { workflowId: "nightly--build", failureCategories: ["missing_tool"] })).toBe(true);
    expect(normalizeReport(normalizeReport(input))).toEqual(normalizeReport(input));
  });

  it("imports reserved metadata into field and data nodes, while diagnostics keep comments literal", () => {
    const source = '<!-- gh-aw-agentic-workflow: Workflow, workflow_id: workflow, run: https://example.com/run -->\n<!-- gh-aw-execution: {"version":1,"publishing_job":{"key":"safe_outputs"}} -->';
    const nodes = parseReportMarkdown(source, { allowMetadata: true });
    expect(nodes).toEqual([
      {
        type: "xml_marker",
        name: "gh-aw-agentic-workflow",
        value: "Workflow",
        fields: [
          { key: "workflow_id", value: "workflow" },
          { key: "run", value: "https://example.com/run" },
        ],
      },
      { type: "json_marker", name: "gh-aw-execution", data: { version: 1, publishing_job: { key: "safe_outputs" } } },
    ]);
    expect(renderReport(report(parseReportMarkdown(source))).body).not.toContain("<!--");
    expect(() => parseReportMarkdown("<!-- gh-aw-execution: not-json -->", { allowMetadata: true })).toThrow();
  });

  it("rejects invalid or duplicate hidden XML fields and unstructured raw marker nodes", () => {
    const field = { key: "branch", value: "main" };
    expect(() => renderReport(report([{ type: "xml_marker", name: "marker", fields: [field] }]))).toThrow("require a marker value");
    expect(() => renderReport(report([{ type: "xml_marker", name: "marker", value: "true", fields: [field, field] }]))).toThrow("duplicate report metadata field");
    expect(() => renderReport(report([{ type: "xml_marker", name: "marker", value: "true", fields: [{ key: "bad -->", value: "main" }] }]))).toThrow("metadata field");
    expect(() => renderReport(report([{ type: "json_marker", name: "bad -->", data: {} }]))).toThrow("metadata name");
    expect(() => renderReport(report([{ type: "metadata", value: "<!-- raw -->" }]))).toThrow("Unsupported report block");
  });
});
