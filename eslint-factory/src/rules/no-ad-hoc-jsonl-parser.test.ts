import { RuleTester } from "eslint";
import { describe, expect, it } from "vitest";
import { noAdHocJsonlParserRule } from "./no-ad-hoc-jsonl-parser";

const cjs = new RuleTester({ languageOptions: { ecmaVersion: 2022, sourceType: "commonjs" } });
const esm = new RuleTester({ languageOptions: { ecmaVersion: 2022, sourceType: "module" } });
const helpers = `const {iterateLines, iterateJsonl, iterateJsonlEvents, JsonlParser, LineFramer} = require("./jsonl_helpers.cjs");`;

describe("no-ad-hoc-jsonl-parser", () => {
  it("uses the correct docs URL", () => {
    expect(noAdHocJsonlParserRule.meta.docs.url).toBe("https://github.com/github/gh-aw/tree/main/eslint-factory#no-ad-hoc-jsonl-parser");
  });

  it("detects manual line parsing without flagging documents or normal iterator consumption", () => {
    cjs.run("no-ad-hoc-jsonl-parser", noAdHocJsonlParserRule, {
      valid: [
        `JSON.parse(document);`,
        `JSON.parse(JSON.stringify(value));`,
        `documents.map(JSON.parse);`,
        `for (const file of files) JSON.parse(fs.readFileSync(file, "utf8"));`,
        `for (const item of data.split(",")) JSON.parse(item);`,
        String.raw`for (const item of data.split(/\\n/)) JSON.parse(item);`,
        `const lines = documents; for (const line of lines) JSON.parse(line);`,
        `const lines = cond ? documents : otherDocuments; lines.map(line => JSON.parse(line));`,
        `JSON.parse(cond ? document : otherDocument);`,
        `const lines = cond ? otherLines : []; const otherLines = lines; lines.map(line => JSON.parse(line));`,
        `const lines = text.split("\\n"); lines.map(line => { function document(line) { JSON.parse(line); } });`,
        `for (const line of text.split("\\n")) { JSON.parse(document); }`,
        `const JSON = custom; for (const line of text.split("\\n")) JSON.parse(line);`,
        `let lines = text.split("\\n"); lines = documents; for (const value of lines) JSON.parse(value);`,
        `const first = second; const second = first; for (const line of first) JSON.parse(line);`,
        `function iterateJsonl(value) {} for (const line of text.split("\\n")) iterateJsonl([line]);`,
        `${helpers} for (const record of iterateJsonl(text)) consume(record);`,
        `${helpers} for (const file of files) for (const record of iterateJsonl(read(file))) consume(record);`,
        `${helpers} for (const file of fileList.split("\\n")) { for (const record of iterateJsonl(fs.readFileSync(file, "utf8"))) consume(record); }`,
        `${helpers} fileList.split("\\n").forEach(file => { for (const record of iterateJsonlEvents(fs.readFileSync(file, "utf8"))) consume(record); });`,
        `const {parseJsonlContent: parse} = require("./jsonl_helpers.cjs"); function visit(file) { return parse(fs.readFileSync(file, "utf8")); } fileList.split("\\n").forEach(visit);`,
        `${helpers} for (const record of iterateJsonlEvents(text)) consume(record);`,
        `${helpers} const parser = new JsonlParser(); source.on("data", chunk => { for (const event of parser.push(chunk)) consume(event); });`,
        `${helpers} for (const line of iterateLines(text)) { function parseDocument(text) { return iterateJsonl(text); } }`,
        { filename: "/runtime/jsonl_helpers.cjs", code: `for (const line of text.split("\\n")) JSON.parse(line);` },
      ],
      invalid: [
        { code: `for (const line of text.split("\\n")) JSON.parse(line);`, errors: [{ messageId: "adHoc" }] },
        { code: `const lines = text.split(/\\r?\\n/); for (const raw of lines) { const line = raw.trim(); JSON.parse(line); }`, errors: [{ messageId: "adHoc" }] },
        { code: `const lines = cond ? [] : text.split("\\n"); lines.map(line => JSON.parse(line));`, errors: [{ messageId: "adHoc" }] },
        { code: `const lines = cond ? text.split("\\n") : []; lines.map(line => JSON.parse(line));`, errors: [{ messageId: "adHoc" }] },
        { code: `const lines = cond ? [] : otherCond ? text.split("\\n") : []; for (const line of lines) JSON.parse(line);`, errors: [{ messageId: "adHoc" }] },
        { code: `text.split("\\n").map(line => JSON.parse(cond ? line : document));`, errors: [{ messageId: "adHoc" }] },
        { code: `text.split("\\n").map(line => JSON.parse(cond ? document : line));`, errors: [{ messageId: "adHoc" }] },
        { code: `function decode(value) { return JSON.parse(value); } text.split("\\n").map(line => decode(cond ? document : line));`, errors: [{ messageId: "adHoc" }] },
        { code: `${helpers} text.split("\\n").map(line => iterateJsonl(cond ? document : line));`, errors: [{ messageId: "restart" }] },
        { code: `const rows = text.split(/\\r\\n|\\r|\\n/).filter(Boolean); rows.forEach(row => JSON.parse(row.slice(row.indexOf("{"))));`, errors: [{ messageId: "adHoc" }] },
        { code: `text.split("\\n").map(JSON.parse);`, errors: [{ messageId: "adHoc" }] },
        { code: `const decode = JSON.parse; text.split("\\n").map(decode);`, errors: [{ messageId: "adHoc" }] },
        { code: `text["split"]("\\n").map(line => JSON["parse"](line));`, errors: [{ messageId: "adHoc" }] },
        { code: `function decode(line) { return JSON.parse(line); } text.split("\\n").map(decode);`, errors: [{ messageId: "adHoc" }] },
        { code: `const decode = line => JSON.parse(line); text.split("\\n").forEach(decode);`, errors: [{ messageId: "adHoc" }] },
        { code: `function decode(value) { return JSON.parse(value); } for (const line of text.split("\\n")) decode(line.trim());`, errors: [{ messageId: "adHoc" }] },
        { code: `${helpers} function decode(value) { return iterateJsonl([value]).next().value; } for (const line of iterateLines(text)) decode(line);`, errors: [{ messageId: "restart" }] },
        { code: `${helpers} function decode(value) { return iterateJsonlEvents(value); } function visit(value) { return decode(value); } text.split("\\n").forEach(visit);`, errors: [{ messageId: "restart" }] },
        { code: `reader.on("line", line => JSON.parse(line.trim()));`, errors: [{ messageId: "adHoc" }] },
        { code: `reader.once("line", JSON.parse);`, errors: [{ messageId: "adHoc" }] },
        { code: `const lines = text.split("\\n"); for (let i = 0; i < lines.length; i++) JSON.parse(lines[i]);`, errors: [{ messageId: "adHoc" }] },
        { code: `const lines = text.split("\\n"); let line; while ((line = lines.shift()) !== undefined) JSON.parse(line);`, errors: [{ messageId: "adHoc" }] },
        { code: `${helpers} for (const line of iterateLines(text)) JSON.parse(line);`, errors: [{ messageId: "adHoc" }] },
        { code: `const {readLines: lines} = require("./jsonl_helpers.cjs"); for (const line of lines(file)) JSON.parse(line);`, errors: [{ messageId: "adHoc" }] },
        { code: `${helpers} const framer = new LineFramer(); for (const line of framer.push(chunk)) JSON.parse(line);`, errors: [{ messageId: "adHoc" }] },
        { code: `const readline = require("node:readline"); async function run() { const reader = readline.createInterface({input}); for await (const line of reader) JSON.parse(line); }`, errors: [{ messageId: "adHoc" }] },
        { code: `${helpers} for (const line of iterateLines(text)) iterateJsonl([line]).next();`, errors: [{ messageId: "restart" }] },
        { code: `${helpers} text.split("\\n").map(line => iterateJsonlEvents(line));`, errors: [{ messageId: "restart" }] },
        { code: `${helpers} for (const line of iterateLines(text)) { const parser = new JsonlParser(); consume(parser.push(line)); }`, errors: [{ messageId: "restart" }] },
        { code: `${helpers} text.split("\\n").forEach(line => { const parser = new JsonlParser(); consume(parser.push(line)); });`, errors: [{ messageId: "restart" }] },
        { code: `const {JsonlParser: Parser} = require("./jsonl_helpers.cjs"); function decode(line) { const parser = new Parser(); return parser.push(line); } text.split("\\n").forEach(decode);`, errors: [{ messageId: "restart" }] },
        { code: `const {parseJsonlContent: parse} = require("./jsonl_helpers.cjs"); function decode(value) { const record = value.trim(); return parse(record); } text.split("\\n").forEach(decode);`, errors: [{ messageId: "restart" }] },
        { code: `const {iterateJsonl: parse} = require("./jsonl_helpers.cjs"); for (const line of text.split("\\n")) parse([line]);`, errors: [{ messageId: "restart" }] },
        { code: `const jsonl = require("./jsonl_helpers.cjs"); for (const line of text.split("\\n")) jsonl.iterateJsonl(line);`, errors: [{ messageId: "restart" }] },
      ],
    });
  });

  it("resolves ESM imports and async line iterators", () => {
    esm.run("no-ad-hoc-jsonl-parser", noAdHocJsonlParserRule, {
      valid: [`import {streamJsonl} from "./jsonl_helpers.cjs"; for await (const value of streamJsonl(input)) consume(value);`],
      invalid: [
        { code: `import {streamLines} from "./jsonl_helpers.cjs"; for await (const line of streamLines(input)) JSON.parse(line);`, errors: [{ messageId: "adHoc" }] },
        { code: `import {iterateJsonl as parse} from "./jsonl_helpers.cjs"; for (const line of text.split("\\n")) parse(line);`, errors: [{ messageId: "restart" }] },
        { code: `import * as jsonl from "./jsonl_helpers.cjs"; for (const line of text.split("\\n")) jsonl.iterateJsonlEvents([line]);`, errors: [{ messageId: "restart" }] },
      ],
    });
  });
});
