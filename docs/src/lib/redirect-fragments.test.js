import assert from "node:assert/strict";
import { mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { pathToFileURL } from "node:url";
import { runInNewContext } from "node:vm";
import redirectFragments, { addFragmentScript, projectRedirects, readMetaRefresh } from "./redirect-fragments.js";

// Representative Astro static redirect HTML, including URL and attribute encoding.
const page = (destination, delay = 0) => {
  const url = (destination.startsWith("/") ? encodeURI(destination) : destination).replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
  return `<!doctype html><title>Redirecting</title><meta http-equiv="refresh" content="${delay};url=${url}"><meta name="robots" content="noindex"><a href="${url}">Redirect</a>`;
};

test("inserts the script before Astro's refresh without changing the fallback", () => {
  for (const destination of ["/gh-aw/new/", "/gh-aw/new/#section", "/gh-aw/new/?a=1&b=2", "/gh-aw/café/", "https://example.com", "https://EXAMPLE.com/x"]) {
    for (const delay of [0, 2]) {
      const original = page(destination, delay);
      const html = addFragmentScript(original, destination);
      assert.ok(html.indexOf("<script data-redirect-fragments>") < html.indexOf('http-equiv="refresh"'), destination);
      assert.equal(html.replace(/<script data-redirect-fragments>.*?<\/script>/, ""), original);
    }
  }
});

test("forwards incoming fragments unless the destination sets one", () => {
  for (const [destination, hash, expected] of [
    ["/gh-aw/guides/workshop/", "#x", "/gh-aw/guides/workshop/#x"],
    ["/gh-aw/guides/workshop/", "#j=terminal&s=daily-status&t=00-welcome", "/gh-aw/guides/workshop/#j=terminal&s=daily-status&t=00-welcome"],
    ["/gh-aw/reference/connectors/#servicenow", "#other", "/gh-aw/reference/connectors/#servicenow"],
    ["/gh-aw/reference/connectors/#servicenow", "", "/gh-aw/reference/connectors/#servicenow"],
    ["/gh-aw/new/", "", "/gh-aw/new/"],
    ["/gh-aw/new/#", "#other", "/gh-aw/new/#"],
    ["/gh-aw/new/?a=1&b=2", "#caf%C3%A9", "/gh-aw/new/?a=1&b=2#caf%C3%A9"],
    ["https://example.com/new/", "#x", "https://example.com/new/#x"],
  ]) {
    const html = addFragmentScript(page(destination), destination);
    const script = /<script data-redirect-fragments>(.*?)<\/script>/.exec(html)[1];
    let replaced;
    runInNewContext(script, {
      URL,
      location: { href: `https://docs.example/gh-aw/old/${hash}`, hash, replace: url => (replaced = url) },
    });
    assert.equal(replaced, new URL(expected, "https://docs.example").href);
  }
});

test("refuses mismatched, missing or duplicate refreshes and repeated injection", () => {
  assert.throws(() => addFragmentScript(page("/gh-aw/other/"), "/gh-aw/new/"), /meta refresh points at/);
  assert.throws(() => addFragmentScript("<title>Normal page</title>", "/gh-aw/new/"), /found 0/);
  assert.throws(() => addFragmentScript(page("/gh-aw/new/") + page("/gh-aw/new/"), "/gh-aw/new/"), /found 2/);
  const html = addFragmentScript(page("/gh-aw/new/"), "/gh-aw/new/");
  assert.throws(() => addFragmentScript(html, "/gh-aw/new/"), /already contains/);
});

test("escapes destinations so they cannot close the inline script", () => {
  const destination = "/gh-aw/new/</script><script>alert(1)//";
  assert.equal(addFragmentScript(page(destination), destination).match(/<\/script>/g).length, 1);
});

test("rejects unsupported or ambiguous destinations", () => {
  for (const [destination, error] of [
    ["javascript:alert(1)", /destinations must be root-relative paths or absolute http\(s\) URLs/],
    [" javascript:alert(1)", /whitespace, control characters and backslashes are not supported/],
    ["java\nscript:alert(1)", /whitespace, control characters and backslashes are not supported/],
    ["//evil.example/x", /destinations must be root-relative paths or absolute http\(s\) URLs/],
    ["/\\evil.example/x", /whitespace, control characters and backslashes are not supported/],
    ["relative/path", /destinations must be root-relative paths or absolute http\(s\) URLs/],
    ["mailto:a@b.c", /destinations must be root-relative paths or absolute http\(s\) URLs/],
    ["/a b/", /whitespace, control characters and backslashes are not supported/],
  ]) {
    assert.throws(() => addFragmentScript("", destination), error, destination);
  }
});

test("only selects static string redirects", () => {
  const route = (extra = {}) => ({ type: "redirect", entrypoint: "/old/", params: [], redirect: "/gh-aw/new/", ...extra });
  assert.deepEqual(projectRedirects([route(), { type: "page" }]), [{ source: "/old/", destination: "/gh-aw/new/" }]);
  assert.throws(() => projectRedirects([route({ redirect: { status: 302, destination: "/gh-aw/new/" } })]), /object-form/);
  assert.throws(() => projectRedirects([route({ params: ["slug"] })]), /dynamic/);
});

test("reads spaced, quoted and HTML-encoded refresh attributes", () => {
  const read = content => readMetaRefresh(`<meta http-equiv="refresh" content="${content}">`).url;
  assert.equal(read("0;url=/gh-aw/new/"), "/gh-aw/new/");
  assert.equal(read(" \n 5 ; URL =\t/gh-aw/new/ \n"), "/gh-aw/new/");
  assert.equal(read("0;url='/gh-aw/a b/'"), "/gh-aw/a b/");
  assert.equal(read("0;url=&#47;gh-aw&#x2F;new/?a=1&amp;b=2"), "/gh-aw/new/?a=1&b=2");
  for (const content of ["0;url=", "0;url=   ", "0;url=/a\n/b", "x;url=/a", "0url=/a", `0;url=${" ".repeat(5000)}x\nz`]) {
    assert.throws(() => read(content), /meta refresh has no url/);
  }
});

test("the build hook rewrites redirect files and leaves ordinary pages unchanged", async t => {
  const directory = await mkdtemp(join(tmpdir(), "gh-aw-redirect-fragments-"));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const dir = pathToFileURL(`${directory}/`);
  const html = page("/gh-aw/new/");
  for (const source of ["old", "without-slash", "ordinary"]) {
    await mkdir(new URL(`${source}/`, dir));
    await writeFile(new URL(`${source}/index.html`, dir), html);
  }
  const integration = redirectFragments();
  integration.hooks["astro:routes:resolved"]({
    routes: [
      { type: "redirect", entrypoint: "/old/", params: [], redirect: "/gh-aw/new/" },
      { type: "redirect", entrypoint: "/without-slash", params: [], redirect: "/gh-aw/new/" },
      { type: "page", entrypoint: "/ordinary/" },
    ],
  });
  const build = { dir, logger: { info() {} } };
  await integration.hooks["astro:build:done"](build);
  for (const source of ["old", "without-slash"]) {
    assert.equal(await readFile(new URL(`${source}/index.html`, dir), "utf8"), addFragmentScript(html, "/gh-aw/new/"));
  }
  assert.equal(await readFile(new URL("ordinary/index.html", dir), "utf8"), html);

  await assert.rejects(redirectFragments().hooks["astro:build:done"](build), /routes were not resolved/);
  integration.hooks["astro:routes:resolved"]({ routes: [{ type: "redirect", entrypoint: "/missing/", params: [], redirect: "/gh-aw/new/" }] });
  await assert.rejects(integration.hooks["astro:build:done"](build), /redirect-fragments: \/missing\//);
});
