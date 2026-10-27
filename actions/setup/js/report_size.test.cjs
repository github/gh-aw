// @ts-check

import { afterEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { renderSizedReport, fitMarkdownBody, SIZE_NOTICE } = require("./report_size.cjs");
const { renderReport, normalizeReport } = require("./report_ast.cjs");
const { redactReport } = require("./report_redaction.cjs");
const { parseReportMarkdown } = require("./report_markdown.cjs");
/** @param {string} value @param {boolean} [agentTainted] @returns {import("./report_ast.cjs").ReportBlock} */
const paragraph = (value, agentTainted = true) => ({ type: "paragraph", agentTainted, children: [{ type: "text", value, agentTainted }] });
/** @param {import("./report_ast.cjs").ReportBlock[]} children @returns {import("./report_ast.cjs").Report} */
const report = children => ({ type: "report", title: "Report", children });

afterEach(() => vi.unstubAllEnvs());

describe("automatic report size redaction", () => {
  it("preserves exact-boundary reports and replaces over-limit text without slicing it", () => {
    const input = report([paragraph("x".repeat(99))]);
    expect(renderSizedReport(input, 100).body).toHaveLength(100);
    const fitted = renderSizedReport(report([paragraph("x".repeat(100))]), 100);
    expect(fitted.body).toBe(`${SIZE_NOTICE}\n`);
    expect(fitted.body.length).toBeLessThanOrEqual(100);
  });

  it("fits formatted summaries without removing protected summary descendants", () => {
    const input = report([
      {
        type: "details",
        agentTainted: true,
        summary: [
          { type: "strong", agentTainted: true, children: [{ type: "text", value: "Protected summary", agentTainted: true, sizeProtected: true }] },
          { type: "text", value: "private summary".repeat(1000), agentTainted: true },
        ],
        children: [paragraph("private body".repeat(1000))],
      },
    ]);
    const copy = structuredClone(input);
    const { body } = renderSizedReport(input, 250);
    expect(body).toContain("<summary><b>Protected summary</b>");
    expect(body).toContain(SIZE_NOTICE);
    expect(body).not.toContain("private");
    expect(body.length).toBeLessThanOrEqual(250);
    expect(input).toEqual(copy);
  });

  describe("adversarial report size redaction", () => {
    const protectedNodes = [
      { type: "xml_marker", name: "custom", value: "MARKER-INTACT" },
      { type: "json_marker", name: "evidence", data: { payload: "METADATA-INTACT" } },
      { type: "review_marker", workflowCallId: "owner/repo/CALLER-INTACT" },
      { type: "marker", name: "gh-aw-threat-detected" },
      { type: "expiration", expiresAt: "2030-01-01T00:00:00.000Z" },
      { type: "paragraph", children: [{ type: "closing_reference", action: "fixes", target: { number: "42" }, enabled: true }] },
    ];

    it.each(protectedNodes.flatMap(node => [true, false, undefined].map(agentTainted => ({ ...node, agentTainted }))))("never removes protected $type with provenance $agentTainted, even inside an agent wrapper", node => {
      const protectedBody = renderReport(report([node])).body.trimEnd();
      const input = report([{ type: "details", summary: "Agent wrapper", agentTainted: true, children: [paragraph("x".repeat(4000)), node] }]);
      const copy = structuredClone(input);
      const body = renderSizedReport(input, 300).body;
      expect(body.length).toBeLessThanOrEqual(300);
      expect(body).toContain(protectedBody);
      expect(body).toContain(SIZE_NOTICE);
      expect(body.match(/<details>/g)).toHaveLength(1);
      expect(body.match(/<\/details>/g)).toHaveLength(1);
      expect(input).toEqual(copy);
      expect(() => renderSizedReport(input, protectedBody.length)).toThrow("cannot be reduced");
    });

    it.each(["paragraph", "table", "list", "details"])("preserves protected inline siblings through %s normalization and fitting", kind => {
      const children = [
        { type: "text", value: "PRIVATE-AGENT-PAYLOAD".repeat(1000), agentTainted: true },
        { type: "text", value: "PROTECTED-DIAGNOSTIC", agentTainted: true, sizeProtected: true },
        { type: "text", value: "OTHER-AGENT-PAYLOAD".repeat(1000), agentTainted: true },
      ];
      const content = { type: "paragraph", agentTainted: true, children };
      const node =
        kind === "paragraph"
          ? content
          : kind === "table"
            ? { type: "table", agentTainted: true, headers: [[{ type: "text", value: "Framework label", agentTainted: false }]], rows: [[children]] }
            : kind === "list"
              ? { type: "list", ordered: false, agentTainted: true, items: [[content]] }
              : { type: "details", summary: "Evidence", agentTainted: true, children: [content] };
      const input = report([node]);
      const copy = structuredClone(input);
      expect(normalizeReport(normalizeReport(input))).toEqual(normalizeReport(input));
      const fitted = renderSizedReport(input, 250);
      expect(fitted.body.length).toBeLessThanOrEqual(250);
      expect(fitted.body.match(/PROTECTED-DIAGNOSTIC/g)).toHaveLength(1);
      expect(fitted.body.match(/Content redacted to fit size limit\./g)).toHaveLength(2);
      expect(fitted.body).not.toContain("AGENT-PAYLOAD");
      expect(renderSizedReport(report(parseReportMarkdown(fitted.body, { agentTainted: true, allowMetadata: true })), 250)).toEqual(fitted);
      expect(input).toEqual(copy);
    });

    it.each(["text", "code", "strong"])("redacts every split of a secret across protected/framework and agent %s boundaries before fitting", kind => {
      const secret = "adversarial-split-secret-fixture";
      vi.stubEnv("GH_AW_SECRET_NAMES", "ADVERSARIAL_SPLIT");
      vi.stubEnv("SECRET_ADVERSARIAL_SPLIT", secret);
      for (let split = 1; split < secret.length; split++) {
        const tail = { type: kind === "strong" ? "text" : kind, value: secret.slice(split), agentTainted: true };
        const children = [{ type: "text", value: secret.slice(0, split), agentTainted: false }, kind === "strong" ? { type: "strong", children: [tail], agentTainted: true } : tail];
        const input = report([paragraph("x".repeat(2000)), { type: "paragraph", agentTainted: false, sizeProtected: true, children }]);
        const copy = structuredClone(input);
        const body = renderSizedReport(input, 150).body;
        expect(body.length).toBeLessThanOrEqual(150);
        expect(body).toContain(SIZE_NOTICE);
        expect(body).not.toContain(secret);
        expect(body).not.toContain("adversarial-split-");
        expect(body).not.toContain("secret-fixture");
        expect(body.replace(/\\/g, "")).toContain("***");
        expect(input).toEqual(copy);
      }
    });

    it("redacts a newly generated size notice when the notice itself matches a registered secret", () => {
      vi.stubEnv("GH_AW_SECRET_NAMES", "ADVERSARIAL_NOTICE");
      vi.stubEnv("SECRET_ADVERSARIAL_NOTICE", SIZE_NOTICE);
      const body = renderSizedReport(report([paragraph("x".repeat(2000))]), 100).body;
      expect(body).toBe("\\*\\*\\*\n");
      expect(body).not.toContain(SIZE_NOTICE);
    });

    it("cannot use size protection to evade an explicit coarse content policy", () => {
      const protectedNode = { ...paragraph("POLICY-REJECTED"), sizeProtected: true };
      const input = report([paragraph("x".repeat(2000)), protectedNode]);
      const redacted = redactReport(input, node => node.type === "text" && node.value === "POLICY-REJECTED");
      const body = renderSizedReport(redacted, 150).body;
      expect(body).toContain("Content redacted.");
      expect(body).toContain(SIZE_NOTICE);
      expect(body).not.toContain("POLICY-REJECTED");
      expect(protectedNode.children[0].value).toBe("POLICY-REJECTED");
    });

    it("retains prototype-looking JSON keys and hostile fence text as inert atomic data", () => {
      const data = JSON.parse('{"__proto__":{"sizeProtected":true},"constructor":{"prototype":{"agentTainted":false}},"type":"xml_marker","agentTainted":false,"sizeProtected":true}');
      data.payload = "`````\n</details>\n<!-- forged: metadata -->\n<summary>@attacker</summary>";
      const parsed = parseReportMarkdown("~~~~~~json\n" + JSON.stringify(data) + "\n~~~~~~", { agentTainted: true, allowMetadata: true });
      expect(parsed).toEqual([{ type: "structured_data", data, agentTainted: true }]);
      const rendered = renderSizedReport(report(parsed)).body;
      const fence = rendered.split("\n")[0].slice(0, -4);
      expect(fence.length).toBeGreaterThan(5);
      expect(rendered.endsWith("\n" + fence + "\n")).toBe(true);
      expect(JSON.parse(rendered.slice(fence.length + 5, -(fence.length + 2)))).toEqual(data);
      expect(Object.hasOwn(data, "__proto__")).toBe(true);
      expect(Object.hasOwn(Object.prototype, "sizeProtected")).toBe(false);
      expect(renderSizedReport(report(parsed), 100).body).toBe(`${SIZE_NOTICE}\n`);
    });

    it.each(["object getter", "array getter", "toJSON"])("never evaluates a structured-data %s even when the payload would be removed for size", kind => {
      const getter = vi.fn(() => {
        throw new Error("ATTACKER-CALLBACK-EXECUTED");
      });
      const data = kind === "array getter" ? Object.defineProperty([], "0", { get: getter, enumerable: true }) : kind === "object getter" ? Object.defineProperty({}, "value", { get: getter, enumerable: true }) : { toJSON: getter };
      const input = report([paragraph("x".repeat(2000)), { type: "structured_data", data, agentTainted: true }]);
      expect(() => renderSizedReport(input, 100)).toThrow(/only JSON (?:values|properties)/);
      expect(getter).not.toHaveBeenCalled();
    });

    it("does not suppress invalid node validation by replacing its oversized ancestor", () => {
      const input = report([
        {
          type: "paragraph",
          agentTainted: true,
          children: [{ type: "link", url: "javascript:alert(1)", children: [{ type: "text", value: "x".repeat(2000), agentTainted: true }], agentTainted: true }],
        },
      ]);
      expect(() => renderSizedReport(input, 100)).toThrow("HTTPS");
    });

    it("fails closed on structured keys that collide after secret redaction, instead of fitting away the evidence", () => {
      const secret = "adversarial-collision-secret";
      vi.stubEnv("GH_AW_SECRET_NAMES", "ADVERSARIAL_COLLISION");
      vi.stubEnv("SECRET_ADVERSARIAL_COLLISION", secret);
      const data = { [secret]: "private", "***": "public", payload: "x".repeat(2000) };
      const input = report([{ type: "structured_data", data, agentTainted: true }]);
      expect(() => renderSizedReport(input, 100)).toThrow("Duplicate report structured data key after redaction");
    });

    it.each(["key", "value"])("rejects encoded secrets in structured-data %ss without echoing the rejected content", location => {
      const secret = "adversarial-encoded-secret-fixture";
      vi.stubEnv("GH_AW_SECRET_NAMES", "ADVERSARIAL_ENCODED");
      vi.stubEnv("SECRET_ADVERSARIAL_ENCODED", secret);
      const encoded = Array.from(secret, character => `&#x${character.charCodeAt(0).toString(16)};`).join("");
      const data = location === "key" ? { [encoded]: "value", payload: "x".repeat(2000) } : { nested: [encoded], payload: "x".repeat(2000) };
      const input = report([{ type: "structured_data", data, agentTainted: true }]);
      expect(() => renderSizedReport(input, 100)).toThrow("unredacted string");
      try {
        renderSizedReport(input, 100);
      } catch (error) {
        expect(String(error)).not.toContain(secret);
        expect(String(error)).not.toContain(encoded);
      }
    });

    it.each(["1e309", '{"number":1e309}'])("does not fit away non-finite JSON parsed from a fence: %s", json => {
      const children = parseReportMarkdown("```json\n" + json + "\n```\n\n" + "x".repeat(2000), { agentTainted: true });
      expect(children[0].type).toBe("structured_data");
      expect(() => renderSizedReport(report(children), 100)).toThrow("only JSON values");
    });

    it("cannot fit away a secret introduced by serialization escaping", () => {
      vi.stubEnv("GH_AW_SECRET_NAMES", "ADVERSARIAL_ESCAPE");
      vi.stubEnv("SECRET_ADVERSARIAL_ESCAPE", "\\*literal\\*");
      const input = report([paragraph("x".repeat(2000) + "*literal*")]);
      expect(() => renderSizedReport(input, 100)).toThrow("unredacted string");
    });
  });

  it("measures escaped output rather than source text length", () => {
    const input = report([paragraph("<x>".repeat(20))]);
    expect(renderReport(input).body.length).toBeGreaterThan(100);
    expect(renderSizedReport(input, 100).body).toBe(`${SIZE_NOTICE}\n`);
  });

  it("preserves framework labels while replacing large agent inline slots", () => {
    const input = report([
      {
        type: "paragraph",
        agentTainted: false,
        children: [
          { type: "text", value: "Framework label: ", agentTainted: false },
          { type: "code", value: "x".repeat(1000), agentTainted: true },
        ],
      },
    ]);
    expect(renderSizedReport(input, 100).body).toBe(`Framework label: ${SIZE_NOTICE}\n`);
  });

  it("preserves markers inside agent containers and reserves the entire footer", () => {
    const marker = "<!-- custom: keep-all-marker-content -->";
    const footer = {
      type: /** @type {const} */ "footer",
      agentTainted: false,
      children: [paragraph("Footer diagnostic " + "f".repeat(100), true)],
      markers: [{ type: /** @type {const} */ "xml_marker", name: "gh-aw-agentic-workflow", value: "Workflow", fields: [] }],
    };
    const input = { ...report([{ type: "details", summary: "Evidence", agentTainted: true, children: [paragraph("x".repeat(1000)), { type: "xml_marker", name: "custom", value: "keep-all-marker-content", agentTainted: true }] }]), footer };
    const copy = structuredClone(input);
    const protectedFooter = renderReport({ ...report([]), footer }).body.trim();
    const body = renderSizedReport(input, 400).body;
    expect(body.length).toBeLessThanOrEqual(400);
    expect(body).toContain(marker);
    expect(body).toContain(protectedFooter);
    expect(body).toContain("<summary>Evidence</summary>");
    expect(input).toEqual(copy);
  });

  it("never shortens protected footer diagnostics embedded in the main tree", () => {
    const diagnostic = { ...paragraph("Footer diagnostic " + "d".repeat(200)), sizeProtected: true };
    const body = renderSizedReport(report([paragraph("x".repeat(1000)), diagnostic]), 300).body;
    expect(body).toContain("Footer diagnostic " + "d".repeat(200));
    expect(body).toContain(SIZE_NOTICE);
  });

  it("coalesces thousands of small replacements without exceeding the budget", () => {
    const body = renderSizedReport(report(Array.from({ length: 1000 }, () => paragraph("x"))), 100).body;
    expect(body).toBe(`${SIZE_NOTICE}\n`);
  });

  it.each(["😀", "&amp;lt;", "` @someone "])("fits large %s input without broken encoding or fences", value => {
    const body = renderSizedReport(report([paragraph(value.repeat(1000))]), 100).body;
    expect(body).toBe(`${SIZE_NOTICE}\n`);
    expect(body).not.toMatch(/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u);
  });

  it("fails explicitly when protected markers or footer content alone cannot fit", () => {
    expect(() => renderSizedReport(report([{ type: "xml_marker", name: "custom", value: "m".repeat(200), agentTainted: true }]), 100)).toThrow("protected markers, footers");
    expect(() => renderSizedReport({ ...report([paragraph("x".repeat(1000))]), footer: { type: "footer", children: [paragraph("f".repeat(200))], markers: [] } }, 100)).toThrow("cannot be reduced");
  });

  it("keeps structured data typed and intact when it fits, or replaces the entire node", () => {
    const data = { nested: [{ value: "x".repeat(1000), numeric: 42, boolean: false, empty: null }], text: "&quot; literal" };
    const input = report([{ type: "structured_data", data, agentTainted: true }]);
    expect(JSON.parse(renderSizedReport(input).body.split("\n").slice(1, -2).join("\n"))).toEqual(data);
    expect(renderSizedReport(input, 100).body).toBe(`${SIZE_NOTICE}\n`);
    expect(input.children[0]).toEqual({ type: "structured_data", data, agentTainted: true });
  });

  it("imports valid JSON fences as structured nodes, not truncatable text", () => {
    const nodes = parseReportMarkdown('```json\n{"items":[1,true,null,{"label":"&quot;"}]}\n```', { agentTainted: true });
    expect(nodes).toEqual([{ type: "structured_data", data: { items: [1, true, null, { label: "&quot;" }] }, agentTainted: true }]);
    expect(parseReportMarkdown("```json\n{invalid}\n```")[0].type).toBe("code_block");
    expect(parseReportMarkdown('```text\n{"value":1}\n```')[0].type).toBe("code_block");
  });

  it.each([null, true, 42, "literal &quot;", [1, true, null]])("keeps JSON value %j typed", data => {
    expect(parseReportMarkdown("```json\n" + JSON.stringify(data) + "\n```")[0]).toEqual({ type: "structured_data", data });
  });

  it("does not promote data fields into trusted provenance or size protection", () => {
    const children = parseReportMarkdown('```json\n{"agentTainted":false,"sizeProtected":true,"payload":"' + "x".repeat(1000) + '"}\n```', { agentTainted: true });
    expect(children[0].agentTainted).toBe(true);
    expect(children[0]).not.toHaveProperty("sizeProtected");
    expect(renderSizedReport(report(children), 100).body).toBe(`${SIZE_NOTICE}\n`);
  });

  it("does not change JSON metadata into size-redactable structured payloads", () => {
    const nodes = parseReportMarkdown('<!-- aw-essential-meta: {"payload":"' + "x".repeat(200) + '"} -->', { allowMetadata: true, agentTainted: true });
    expect(nodes[0].type).toBe("json_marker");
    expect(() => renderSizedReport(report(nodes), 100)).toThrow("cannot be reduced");
  });

  it("retains secret redaction before fitting and rechecks the final notice output", () => {
    vi.stubEnv("GH_AW_SECRET_NAMES", "SIZING_SECRET");
    vi.stubEnv("SECRET_SIZING_SECRET", "size-fitting-secret-fixture");
    const body = renderSizedReport(report([paragraph("size-fitting-secret-fixture " + "x".repeat(1000))]), 100).body;
    expect(body).toBe(`${SIZE_NOTICE}\n`);
    expect(body).not.toContain("size-fitting-secret-fixture");
  });

  it("does not let size protection bypass secret redaction", () => {
    vi.stubEnv("GH_AW_SECRET_NAMES", "SIZING_SECRET");
    vi.stubEnv("SECRET_SIZING_SECRET", "size-fitting-secret-fixture");
    const protectedNode = { ...paragraph("size-fitting-secret-fixture", false), sizeProtected: true };
    const body = renderSizedReport(report([protectedNode])).body;
    expect(body).not.toContain("size-fitting-secret-fixture");
    expect(body).toContain("\\*\\*\\*");
  });

  it("reserves literal prefix and footer strings without ellipsing them", () => {
    const prefix = "<details>\n<summary>Framework</summary>\n\n";
    const suffix = "\n\n</details>\nFooter in full\n<!-- marker: keep -->";
    const fitted = fitMarkdownBody("x".repeat(1000), { prefix, suffix, maxLength: 200 });
    expect(fitted).toBe(prefix + SIZE_NOTICE + suffix);
    expect(fitted.length).toBeLessThanOrEqual(200);
    expect(fitMarkdownBody("**literal**", { prefix, suffix, maxLength: 200 })).toBe(prefix + "**literal**" + suffix);
    expect(() => fitMarkdownBody("large", { suffix: "f".repeat(100), maxLength: 100 })).toThrow("Protected markers and footers");
    expect(fitMarkdownBody("x".repeat(100), { maxLength: SIZE_NOTICE.length })).toBe(SIZE_NOTICE);
  });

  it.each([0, -1, 1.5, NaN, Infinity])("rejects invalid size budgets %s", maxLength => {
    expect(() => renderSizedReport(report([paragraph("Body")]), maxLength)).toThrow("positive integer");
  });
});
