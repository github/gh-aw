// Explicit maintainer-only refresh; normal Go tests use the checked-in snapshot.
import { execFileSync } from "node:child_process";
import { mkdirSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { parseAllDocuments } from "yaml";
import { oracle } from "./oracle.bundle.mjs";

const revision = "4043eda158e16579cc5fb1b0b07a4bce2a76f0b5";
const root = fileURLToPath(new URL("../testdata/", import.meta.url));
const upstreamTree = JSON.parse(execFileSync("gh", ["api", `repos/actions/languageservices/git/trees/${revision}?recursive=1`])).tree;
const cases = [];
for (const entry of upstreamTree) {
  const expression = /^expressions\/testdata\/[^/]+\.json$/.test(entry.path);
  const workflow = /^workflow-parser\/testdata\/reader\/[^/]+\.yml$/.test(entry.path);
  // Generated resource-exhaustion inputs are covered by explicit local limits,
  // not megabytes of fixture data.
  if ((!expression && !workflow) || entry.size > 100000) continue;
  const response = JSON.parse(execFileSync("gh", ["api", `repos/actions/languageservices/contents/${entry.path}?ref=${revision}`]));
  const content = Buffer.from(response.content, "base64").toString("utf8");
  if (expression) {
    for (const [group, tests] of Object.entries(JSON.parse(content))) {
      for (const [index, test] of tests.entries()) {
        cases.push({
          name: `${entry.path}/${group}/${index}`,
          kind: "expression",
          source: test.expr,
          contexts: Object.keys(test.contexts || {}),
        });
      }
    }
  } else {
    // Upstream reader fixtures contain configuration, input, and expected
    // output as three YAML documents. Preserve the input's spelling exactly.
    const documents = parseAllDocuments(content, { keepSourceTokens: true });
    const input = documents[1];
    if (!input) throw new Error(`Missing input document: ${entry.path}`);
    const start = content.indexOf("---") + 3;
    const end = content.indexOf("\n---", start);
    cases.push({
      name: entry.path,
      kind: "workflow",
      source: content.slice(start, end < 0 ? undefined : end).replace(/^\r?\n/, ""),
      contexts: [],
    });
  }
}
mkdirSync(root, { recursive: true });
writeFileSync(
  `${root}conformance.json`,
  `${JSON.stringify(
    {
      revision,
      cases: cases.map(input => {
        const { accepted, tree } = oracle(input);
        return {
          ...input,
          accepted,
          ...(input.kind === "expression" && accepted ? { tree } : {}),
        };
      }),
    },
    null,
    2
  )}\n`
);
console.log(`Recorded ${cases.length} official parser conformance cases`);
