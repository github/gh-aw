import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { createRequire } from "module";
import { Parser } from "commonmark";

const require = createRequire(import.meta.url);
const { normalizeReport, normalizeReportImage, redactReportForRendering, assertRedactedReport, renderReport } = require("./report_ast.cjs");
const { parseReportMarkdown } = require("./report_markdown.cjs");
const { redactReport } = require("./report_redaction.cjs");
const { renderSizedReport } = require("./report_size.cjs");
const { sanitizeContent } = require("./sanitize_content.cjs");
const { clearRedactedDomains } = require("./sanitize_content_core.cjs");

const report = children => ({ type: "report", title: "HTML fidelity", agentTainted: true, children });
const parse = (source, agentTainted = true) => report(parseReportMarkdown(source, { agentTainted }));
const image = fields => ({ type: "image", url: "https://github.com/o/r/image.png", alt: "Mascot", ...fields });
const attribute = value => value.replace(/[&<>"']/g, character => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&apos;" })[character]);

beforeEach(() => {
  vi.stubGlobal("core", { info: () => {}, warning: () => {}, debug: () => {} });
  vi.stubEnv("GH_AW_ALLOWED_DOMAINS", "github.com");
  vi.stubEnv("GH_AW_SAFE_OUTPUTS_URLS", "allowed-only");
  vi.stubEnv("GITHUB_SERVER_URL", "");
  vi.stubEnv("GITHUB_API_URL", "");
  vi.stubEnv("GH_AW_SECRET_NAMES", "");
});

afterEach(() => {
  clearRedactedDomains();
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
});

describe("typed report image presentation", () => {
  it("renders only validated fields and escapes attribute text rather than accepting raw HTML", () => {
    const node = image({
      width: 120,
      height: 80,
      align: "right",
      alt: '"Mascot" <img src=x onerror=attack()> @user | & data',
      url: "https://github.com/o/r/image.png?a=1&b=2",
      onerror: "attack()",
      style: "display:none",
      agentTainted: true,
    });
    const input = report([{ type: "paragraph", agentTainted: true, children: [node] }]);
    const copy = structuredClone(input);
    const normalized = normalizeReport(input);
    expect(normalized.children[0].children[0]).toEqual({
      type: "image",
      url: node.url,
      alt: node.alt,
      width: 120,
      height: 80,
      align: "right",
      agentTainted: true,
    });
    const body = renderReport(input).body;
    expect(body).toBe('<img src="https://github.com/o/r/image.png?a=1&amp;b=2" alt="&quot;Mascot&quot; &lt;img src=x onerror=attack()&gt; &#64;user &#124; &amp; data" width="120" height="80" align="right">\n');
    expect(input).toEqual(copy);
    expect(normalizeReport(normalized)).toEqual(normalized);
    expect(renderReport(parse(body)).body).toBe(body);
  });

  it("retains Markdown serialization for images without layout fields", () => {
    expect(renderReport(report([{ type: "paragraph", children: [image({})] }])).body).toBe("![Mascot](https://github.com/o/r/image.png)\n");
  });

  it.each([
    "https://outside.invalid/image.png?data=sentinel",
    "https://github.com.outside.invalid/image.png",
    "https://github.com@outside.invalid/image.png",
    "https://%6futside.invalid/image.png",
    "https://[::1]/image.png",
    "//github.com/image.png",
    "http://github.com/image.png",
    "data:image/png;base64,c2VudGluZWw=",
    "DATA:image/svg+xml,<svg/>",
    "data&#58;image/png;base64,c2VudGluZWw=",
    "d&#97;ta:image/png;base64,c2VudGluZWw=",
    "data%3Aimage/png;base64,c2VudGluZWw=",
    "javascript:attack()",
  ])("rejects untrusted image sources in typed AST nodes without layout fields: %s", url => {
    for (const agentTainted of [true, false, undefined]) {
      const input = report([{ type: "paragraph", children: [image({ url, agentTainted })] }]);
      expect(() => normalizeReport(input)).toThrow(TypeError);
      expect(() => renderReport(input)).toThrow(TypeError);
    }
  });

  it("rejects image destinations introduced through inline AST slots", () => {
    const children = parseReportMarkdown("Framework {image}", { inlineSlots: { image: [image({ url: "https://outside.invalid/leak", agentTainted: false })] }, agentTainted: false });
    expect(() => renderReport(report(children))).toThrow("Report URLs must use an allowed HTTPS domain");
  });

  it("revalidates image domains after policy changes before rendering", () => {
    const input = redactReportForRendering(report([{ type: "paragraph", children: [image({})] }]));
    vi.stubEnv("GH_AW_ALLOWED_DOMAINS", "trusted.invalid");
    expect(() => renderReport(input)).toThrow("Report URLs must use an allowed HTTPS domain");
  });

  it.each([0, -1, 10001, 1.5, NaN, Infinity, "120", '120" onerror="attack()', null, {}, []])("rejects invalid image dimensions %j without echoing them", dimension => {
    for (const field of ["width", "height"]) {
      expect(() => normalizeReportImage(image({ [field]: dimension }))).toThrow("Report image dimensions must be integers between 1 and 10000");
    }
  });

  it("requires primitive numbers for both dimensions without coercion", () => {
    const valueOf = vi.fn(() => 120);
    for (const dimension of [true, false, 120n, Object(120), { valueOf }, () => 120, Symbol("dimension")]) {
      for (const field of ["width", "height"]) {
        expect(() => normalizeReportImage(image({ [field]: dimension }))).toThrow("Report image dimensions must be integers between 1 and 10000");
      }
    }
    expect(valueOf).not.toHaveBeenCalled();
  });

  it.each([1, 120, 10000])("retains valid width and height as numbers: %i", dimension => {
    const normalized = normalizeReportImage(image({ width: dimension, height: dimension }));
    expect(normalized.width).toBe(dimension);
    expect(normalized.height).toBe(dimension);
    expect(typeof normalized.width).toBe("number");
    expect(typeof normalized.height).toBe("number");
  });

  it.each(["LEFT", "top", 'right" onload="attack()', "", null, 1])("rejects invalid typed alignment %j", align => {
    expect(() => normalizeReportImage(image({ align }))).toThrow("Invalid report image alignment");
  });

  it.each([true, false, undefined])("redacts image attributes and code regardless of provenance %s", agentTainted => {
    vi.stubEnv("GH_AW_SECRET_NAMES", "IMAGE_FIDELITY");
    vi.stubEnv("SECRET_IMAGE_FIDELITY", 'image-fidelity-"secret"-fixture');
    const secret = process.env.SECRET_IMAGE_FIDELITY;
    const children = [
      { type: "paragraph", agentTainted, children: [image({ width: 120, alt: secret, agentTainted }), { type: "code", value: secret, agentTainted }] },
      { type: "code_block", language: "html", value: `<img alt='${secret}'>`, agentTainted },
    ];
    const prepared = redactReportForRendering(report(children));
    expect(() => assertRedactedReport(prepared)).not.toThrow();
    const body = renderReport(report(children)).body;
    expect(body).not.toContain("image-fidelity-");
    expect(body).toContain("***");
    prepared.children[0].children[0].alt = secret;
    expect(() => assertRedactedReport(prepared)).toThrow("unredacted string");
  });

  it("offers imported media to coarse redaction without swallowing framework siblings", () => {
    const agent = parseReportMarkdown('<img src="https://github.com/o/r/image.png" width="120" alt="Mascot">', { agentTainted: true });
    const input = report([...agent, { type: "paragraph", agentTainted: false, children: [{ type: "text", value: "Framework attribution", agentTainted: false }] }]);
    const redacted = redactReport(input, node => node.type === "image");
    expect(renderReport(redacted).body).toBe("Content redacted.\n\nFramework attribution\n");
    expect(agent[0].children[0].agentTainted).toBe(true);
  });

  it("measures escaped HTML against publication limits and redacts the complete agent subtree", () => {
    const input = report([
      { type: "paragraph", agentTainted: true, children: [image({ width: 120, alt: "<&".repeat(1000), agentTainted: true })] },
      { type: "paragraph", agentTainted: false, children: [{ type: "text", value: "Complete framework attribution", agentTainted: false }] },
    ]);
    expect(renderReport(input).body.length).toBeGreaterThan(8000);
    const fitted = renderSizedReport(input, 200);
    expect(fitted.body.length).toBeLessThanOrEqual(200);
    expect(fitted.body).toContain("Content redacted to fit size limit.");
    expect(fitted.body).toContain("Complete framework attribution");
    expect(fitted.body).not.toContain("<img ");
  });
});

describe("report HTML import boundary", () => {
  it.each([
    '<img src="https://github.com/o/r/image.png" alt="Mascot" width="120" height="80" align="right" />',
    "<IMG ALIGN='RIGHT' HEIGHT='80' WIDTH='120' ALT='Mascot' SRC='https://github.com/o/r/image.png'>",
    "<img\tsrc=https://github.com/o/r/image.png alt=Mascot width=120 height=80 align=right>",
  ])("imports safe presentation as tainted typed media: %s", source => {
    const input = parse(source);
    expect(input.children).toEqual([{ type: "paragraph", agentTainted: true, children: [image({ width: 120, height: 80, align: "right", agentTainted: true })] }]);
    expect(renderReport(input).body).toBe('<img src="https://github.com/o/r/image.png" alt="Mascot" width="120" height="80" align="right">\n');
  });

  it("keeps entities and markup inside alt text from becoming attributes or report structure", () => {
    const alt = '" onerror="attack()" > </details> <script>payload</script> `code` **bold** @user';
    const source = `<img src="https://github.com/o/r/image.png" width="120" alt="${attribute(alt)}">`;
    const input = parse(source);
    expect(input.children[0].children).toEqual([image({ width: 120, alt, agentTainted: true })]);
    const body = renderReport(input).body;
    expect(body.match(/<img /g)).toHaveLength(1);
    expect(body).not.toContain("<script>");
    expect(body).not.toContain("</details>");
    expect(body).toContain("&quot; onerror=&quot;attack()&quot;");
    expect(renderReport(parse(body)).body).toBe(body);
  });

  it("substitutes scalar fields only after parsing attributes", () => {
    const children = parseReportMarkdown('<img src="{url}" alt="{alt}" width="{width}">', {
      context: { url: "https://github.com/o/r/image.png", alt: '"> <img src=x onerror=attack()>', width: 120 },
      agentTainted: true,
    });
    const body = renderReport(report(children)).body;
    expect(body).toContain('alt="&quot;&gt; &lt;img src=x onerror=attack()&gt;"');
    expect(body.match(/<img /g)).toHaveLength(1);
  });

  it.each([
    "<img src=x width=120>",
    '<img src="http://github.com/image.png" width="120">',
    '<img src="//github.com/image.png" width="120">',
    '<img src="javascript:attack()" width="120">',
    '<img src="data:image/png;base64,payload" width="120">',
    '<img src="https://github.com@outside.invalid/image.png" width="120">',
    '<img src="https://github.com.outside.invalid/image.png" width="120">',
    '<img src="https://[::1]/image.png" width="120">',
    '<img src="https://%6futside.invalid/image.png" width="120">',
    '<img src="https://github.com./image.png" width="120">',
    '<img src="https://github.com/image.png" src="https://outside.invalid/image.png" width="120">',
    '<img SRC="https://github.com/image.png" src="https://github.com/other.png" width="120">',
    '<img src="https://github.com/image.png" width="120" onerror="attack()">',
    '<img src="https://github.com/image.png" width="120" style="display:none">',
    '<img src="https://github.com/image.png" width="120" title="hidden">',
    '<img src="https://github.com/image.png" width="120" data-payload="hidden">',
    '<img src="https://github.com/image.png" width="120" srcset="https://outside.invalid/leak">',
    '<img src="https://github.com/image.png" width="120" agentTainted="false">',
    '<img src="https://github.com/image.png" width="120" sizeProtected="true">',
    '<img src="https://github.com/image.png" width="120%" >',
    '<img src="https://github.com/image.png" width="1e3">',
    '<img src="https://github.com/image.png" width="-1">',
    '<img src="https://github.com/image.png" width="10001">',
    '<img src="https://github.com/image.png" align="top">',
    '<img src="https://github.com/image.png" width="120"disabled>',
    '<img src="https://github.com/image.png" width="120',
    '<img src="https://github.com/image.png"\nwidth="120">',
    '<img-other src="https://github.com/image.png" width="120">',
    '<img:other src="https://github.com/image.png" width="120">',
    '<picture><source srcset="https://github.com/image.png"></picture>',
    '<video src="https://github.com/video.mp4"></video>',
  ])("leaves unsupported or unsafe HTML visible and inert: %s", source => {
    const body = renderReport(parse(source)).body;
    expect(body).not.toMatch(/<(?:img|picture|source|video)\b/i);
    expect(body).toContain("&lt;");
  });

  it.each(["allowed-only", "allowed-or-code-region"])("enforces domain policy for media even with backticks in attributes under %s", policy => {
    vi.stubEnv("GH_AW_SAFE_OUTPUTS_URLS", policy);
    const source = '<img alt="`" width="120" src="https://outside.invalid/image.png?leak=sentinel">`';
    expect(renderReport(parse(sanitizeContent(source))).body).not.toContain("<img ");
    const body = renderReport(parse(source)).body;
    expect(body).not.toContain("<img ");
    expect(body).not.toContain("https://outside.invalid");
  });

  it.each(["allowed-only", "allowed-or-code-region"])("rechecks Markdown image domains after HTML attributes change code boundaries under %s", policy => {
    vi.stubEnv("GH_AW_SAFE_OUTPUTS_URLS", policy);
    const source = '<img alt="`" width="120" src="https://github.com/i.png">![payload](https://outside.invalid/collect?data=sentinel)`';
    const body = renderReport(parse(sanitizeContent(source))).body;
    expect(body).toContain(policy === "allowed-or-code-region" ? "image unavailable: URL must use an allowed HTTPS domain" : "outside.invalid/redacted");
    expect(body).not.toContain("https://outside.invalid");
    const walker = new Parser().parse(body).walker();
    let event;
    while ((event = walker.next())) {
      if (event.node.type === "image") expect(event.node.destination).not.toContain("outside.invalid");
      if (event.node.type.startsWith("html_")) expect(event.node.literal).not.toContain("outside.invalid");
    }
  });

  it.each([
    "data:image/png;base64,c2VudGluZWw=",
    "DATA:image/svg+xml,PHN2Zz48L3N2Zz4=",
    "data&#58;image/png;base64,c2VudGluZWw=",
    "d&#97;ta:image/png;base64,c2VudGluZWw=",
    "data%3Aimage/png;base64,c2VudGluZWw=",
    "https://outside.invalid/image.png",
    "https://github.com.outside.invalid/image.png",
    "https://github.com@outside.invalid/image.png",
  ])("refuses an untrusted image destination through Markdown and HTML import: %s", url => {
    for (const source of [`![payload](${url})`, `<img src="${url}" width="120" alt="payload">`]) {
      const input = parse(source);
      expect(JSON.stringify(input)).not.toContain('"type":"image"');
      const body = renderReport(input).body;
      const walker = new Parser().parse(body).walker();
      let event;
      while ((event = walker.next())) {
        expect(event.node.type).not.toBe("image");
        if (event.node.type.startsWith("html_")) expect(event.node.literal).not.toContain("<img");
      }
    }
  });

  it.each(["https://github.com/a.png", "https://media.github.com/a.png"])("allows Markdown images on configured domains: %s", url => {
    expect(parse(`![payload](${url})`).children[0].children).toEqual([image({ url, alt: "payload", agentTainted: true })]);
  });

  it.each(["[payload](https://outside.invalid/leak)", "<https://outside.invalid/leak>", "https://outside.invalid/leak"])("rechecks other renderable destinations after HTML consumes an attribute backtick: %s", link => {
    vi.stubEnv("GH_AW_SAFE_OUTPUTS_URLS", "allowed-or-code-region");
    const source = `<img alt="\`" width="120" src="https://github.com/i.png">${link}\``;
    const body = renderReport(parse(sanitizeContent(source))).body;
    expect(body).toContain("unavailable");
    const walker = new Parser().parse(body).walker();
    let event;
    while ((event = walker.next())) {
      if (event.node.type === "link") expect(new URL(event.node.destination).hostname).toBe("github.com");
    }
  });

  it("uses the configured allowlist, including permitted subdomains", () => {
    vi.stubEnv("GH_AW_ALLOWED_DOMAINS", "*.trusted.invalid");
    const body = renderReport(parse('<img src="https://media.trusted.invalid/a.png" width="120">')).body;
    expect(body).toBe('<img src="https://media.trusted.invalid/a.png" alt="" width="120">\n');
    expect(renderReport(parse('<img src="https://media.trusted.invalid.attacker.invalid/a.png" width="120">')).body).not.toContain("<img ");
  });

  it("retains configured domain restrictions while preserving literal code under the default URL policy", () => {
    const source = '<img src="https://outside.invalid/image.png?leak=sentinel" width="120">\n\n```html\nList<T> <!-- comment --> https://outside.invalid/leak\n```\n\n`Array<T>`';
    const body = renderReport(parse(sanitizeContent(source))).body;
    expect(body).not.toContain("<img ");
    expect(body).not.toContain("https://outside.invalid");
    expect(body).toContain("List<T> <!-- comment -->");
    expect(body).toContain("`Array<T>`");
  });

  it.each(["`List<T>`", "``List<T> `example` ``", "```List<T>```", "~~~html\n<img src=x onerror=attack()>\n<!-- example -->\n~~~", "````html\nList<T>\n```\n````"])("keeps code literal rather than importing its HTML: %s", source => {
    const input = parse(source);
    expect(JSON.stringify(input)).not.toContain('"type":"image"');
    expect(renderReport(input).body).toContain(source.includes("List<T>") ? "List<T>" : "<img src=x onerror=attack()>");
  });

  it("does not mistake inline triple-backtick code followed by prose for a fence", () => {
    expect(renderReport(parse("```List<T>``` then <Abc>")).body).toBe("`List<T>` then &lt;Abc&gt;\n");
  });

  it("does not reinterpret a suffix of an unmatched backtick run as an opening delimiter", () => {
    const input = parse("````List<T> `single`");
    expect(input.children[0].children).toEqual([
      { type: "text", value: "````List<T> ", agentTainted: true },
      { type: "code", value: "single", agentTainted: true },
    ]);
  });

  it("separates code delimiters from a following neutralized mention", () => {
    const input = report([
      {
        type: "paragraph",
        children: [
          { type: "code", value: '` <img src="https://outside.invalid/a.png" onerror=attack()>' },
          { type: "text", value: "@user" },
        ],
      },
    ]);
    const body = renderReport(input).body;
    expect(body).toContain("`` ` <img");
    expect(body).toContain("`` `@user`");
    const walker = new Parser().parse(body).walker();
    let event;
    while ((event = walker.next())) expect(event.node.type).not.toMatch(/^html_/);
  });

  it.each(["soft_break", "hard_break"])("keeps code after an image-only line in inline Markdown context with %s", type => {
    const input = report([{ type: "paragraph", children: [image({ width: 120 }), { type }, { type: "code", value: "<script>attack()</script>" }] }]);
    const body = renderReport(input).body;
    expect(body).toContain('&#32;<img src="https://github.com/o/r/image.png" alt="Mascot" width="120">');
    const walker = new Parser().parse(body).walker();
    let event;
    const code = [];
    while ((event = walker.next())) {
      if (event.node.type.startsWith("html_")) expect(event.node.literal).not.toContain("<script>");
      if (event.node.type === "code") code.push(event.node.literal);
    }
    expect(code).toContain("<script>attack()</script>");
    expect(renderReport(parse(body)).body).toBe(body);
  });

  it.each(["https://outside.invalid/collect?data=sentinel", "data:image/svg+xml,payload"])("keeps HTML image examples inert after multiline subscript markup: %s", url => {
    vi.stubEnv("GH_AW_SAFE_OUTPUTS_URLS", "allowed-or-code-region");
    const example = `<img src="${url}">`;
    const source = `<sub>\ntext</sub>\n\`${example}\``;
    const body = renderReport(parse(sanitizeContent(source))).body;
    expect(body).toContain("<sub> text</sub>");
    const walker = new Parser().parse(body).walker();
    let event;
    const code = [];
    while ((event = walker.next())) {
      if (event.node.type === "code") code.push(event.node.literal);
      if (event.node.type.startsWith("html_")) expect(event.node.literal).not.toContain("<img");
      expect(event.node.type).not.toBe("image");
    }
    expect(code).toContain(example);
  });

  it("rejects secret-bearing fence info strings rather than publishing an unredacted field", () => {
    vi.stubEnv("GH_AW_SECRET_NAMES", "HTML_FUZZ");
    vi.stubEnv("SECRET_HTML_FUZZ", "html-fuzz-secret-fixture");
    expect(() => renderReport(parse("```html-fuzz-secret-fixture\n<T>\n```"))).toThrow("Report contains an unredacted string");
  });

  it("keeps an escaped opening bracket literal instead of importing media", () => {
    expect(renderReport(parse('\\<img src="https://github.com/image.png" width="120">')).body).not.toContain("<img ");
    expect(renderReport(parse("\\<T>")).body).toBe("&lt;T&gt;\n");
  });

  it("handles long malformed tags, quote storms, and unmatched delimiters without granting raw HTML", () => {
    for (const source of [
      "<img ".repeat(10000),
      '<img alt="' + "<img ".repeat(10000),
      '<img src="https://github.com/a.png" alt=' + "'\"".repeat(10000),
      "`".repeat(10000) + "<img src=x>",
      '<img src="https://github.com/a.png" width="' + "9".repeat(10000) + '">',
    ]) {
      const body = renderReport(parse(source)).body;
      expect(body).not.toContain("<img ");
    }
  });
});

describe("seeded adversarial HTML fidelity fuzzing", () => {
  function generator(seed) {
    return maximum => {
      seed ^= seed << 13;
      seed ^= seed >>> 17;
      seed ^= seed << 5;
      return (seed >>> 0) % maximum;
    };
  }

  it("round-trips 1000 valid media variants without losing presentation, provenance, or redaction", () => {
    const next = generator(0x18829290);
    const alts = ["Mascot", "x > y", "<T>", '"quoted" & text', "literal onerror=example", "`code` **bold**", "@user | table", "</details><script>attack()</script>"];
    for (let index = 0; index < 1000; index++) {
      const width = 1 + next(10000);
      const height = 1 + next(10000);
      const align = ["left", "right", "center"][next(3)];
      const alt = alts[next(alts.length)];
      const fields = [
        ["src", "https://github.com/o/r/image.png?a=1&b=2"],
        ["alt", alt],
        ["width", String(width)],
        ["height", String(height)],
        ["align", align],
      ];
      for (let i = fields.length - 1; i > 0; i--) {
        const j = next(i + 1);
        [fields[i], fields[j]] = [fields[j], fields[i]];
      }
      const source = `<${next(2) ? "IMG" : "img"}${fields
        .map(([key, value]) => {
          const quote = next(2) ? '"' : "'";
          return `${next(2) ? " " : "\t"}${next(2) ? key : key.toUpperCase()}=${quote}${attribute(value)}${quote}`;
        })
        .join("")}${next(2) ? " />" : ">"}`;
      const input = parse(source);
      expect(input.children[0].children).toEqual([image({ url: fields.find(([key]) => key === "src")[1], alt, width, height, align, agentTainted: true })]);
      const body = renderReport(input).body;
      expect(renderReport(parse(body)).body).toBe(body);
    }
  }, 20000);

  it.each([0x29009188, 0x18829290, 0x12345678, 0xabcdef01])(
    "keeps 2000 malformed or encoded mutations within publication boundaries for seed %i",
    seed => {
      vi.stubEnv("GH_AW_SECRET_NAMES", "HTML_FUZZ");
      vi.stubEnv("SECRET_HTML_FUZZ", "html-fuzz-secret-fixture");
      const next = generator(seed);
      const atoms = [
        "<img",
        ">",
        ' src="https://github.com/a.png"',
        ' src="https://outside.invalid/leak"',
        ' width="120"',
        " onerror=attack()",
        ' alt="` > <script>attack()</script>"',
        " title=hidden",
        " data-payload=hidden",
        "\n",
        "\r\n",
        "'",
        '"',
        " ",
        "/",
        "\\",
        "`",
        "```",
        "&lt;",
        "&#34;",
        "&#96;",
        "\u200b",
        "html-fuzz-secret-fixture",
        "&#104;tml-fuzz-secret-fixture",
        '<img src="https://github.com/a.png" width="120">',
        '<img src="https://outside.invalid/a.png" width="120">',
        '<img src="https://github.com/a.png" alt="&#34; width=&#34;120">',
        "<img src=https://github.com/a.png alt=&#34;&#32;width&#61;&#34;120>",
        '<b title="`">text</b>',
        "![payload](https://outside.invalid/leak)",
        "![payload](data:image/png;base64,c2VudGluZWw=)",
        "![payload](https://github.com/image.png)",
        "@user",
        "**",
        "{body}",
        "<sub>\ntext</sub>\n",
      ];
      for (let index = 0; index < 2000; index++) {
        let source = "";
        for (let part = 0, count = 1 + next(20); part < count; part++) source += atoms[next(atoms.length)];
        const input = parse(source);
        if (/"language":"[^"]*html-fuzz-secret-fixture/.test(JSON.stringify(normalizeReport(input)))) {
          expect(() => renderReport(input)).toThrow("Report contains an unredacted string");
          continue;
        }
        const prepared = redactReportForRendering(input);
        expect(() => assertRedactedReport(prepared)).not.toThrow();
        expect(normalizeReport(normalizeReport(input))).toEqual(normalizeReport(input));
        const body = renderReport(input).body;
        expect(body).not.toContain("html-fuzz-secret-fixture");
        const walker = new Parser().parse(body).walker();
        let event;
        while ((event = walker.next())) {
          if (event.entering && event.node.type === "image") {
            expect(new URL(event.node.destination).protocol).toBe("https:");
            expect(new URL(event.node.destination).hostname).toBe("github.com");
          }
          if (event.entering && event.node.type === "link") {
            expect(new URL(event.node.destination).protocol).toBe("https:");
            expect(new URL(event.node.destination).hostname).toBe("github.com");
          }
          if (!event.entering || !["html_inline", "html_block"].includes(event.node.type)) continue;
          const literal = event.node.literal;
          for (const tag of literal.matchAll(/<[^>]*>/g)) {
            expect(tag[0], `seed ${seed}, case ${index}: ${JSON.stringify(source)}`).toMatch(
              /^(?:<img src="https:\/\/github\.com\/[^"<>]*" alt="[^"<>]*"(?: width="[1-9]\d{0,4}")?(?: height="[1-9]\d{0,4}")?(?: align="(?:left|right|center)")?>|<\/?sub>)$/
            );
          }
          expect(literal.replace(/<[^>]*>/g, ""), `seed ${seed}, case ${index}: ${JSON.stringify(source)}`).not.toContain("<");
        }
      }
    },
    20000
  );
});
