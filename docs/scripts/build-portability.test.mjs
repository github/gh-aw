import assert from "node:assert/strict";
import { execFileSync, execSync, spawnSync } from "node:child_process";
import { copyFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { pathToFileURL } from "node:url";

function fixture(t) {
  const dir = mkdtempSync(join(tmpdir(), "docs portability "));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  for (const subdir of ["scripts", "src/styles", "src/content/docs"]) {
    mkdirSync(join(dir, subdir), { recursive: true });
  }
  for (const script of ["check-reading-measure.mjs", "check-redirects.mjs"]) {
    copyFileSync(new URL(script, import.meta.url), join(dir, "scripts", script));
  }
  return dir;
}

for (const windowsPaths of [false, true]) {
  test(`reading measure accepts ${windowsPaths ? "Windows" : "native"} relative paths`, t => {
    const dir = fixture(t);
    for (const file of ["custom.css", "chrome.css"]) {
      copyFileSync(new URL(`../src/styles/${file}`, import.meta.url), join(dir, "src/styles", file));
    }
    writeFileSync(join(dir, "astro.config.mjs"), "");
    const script = pathToFileURL(join(dir, "scripts/check-reading-measure.mjs")).href;
    const output = execFileSync(
      process.execPath,
      [
        "--input-type=module",
        "-e",
        `
      import path from "node:path";
      import { syncBuiltinESMExports } from "node:module";
      const relative = path.relative;
      if (${windowsPaths}) {
        path.relative = (...args) => relative(...args).replaceAll("/", "\\\\");
        syncBuiltinESMExports();
      }
      await import(process.argv[1]);
    `,
        script,
      ],
      { encoding: "utf8", cwd: tmpdir() }
    );
    assert.match(output, /Reading measure check passed/);
  });
}

for (const newline of ["\n", "\r\n"]) {
  test(`redirects accept ${newline === "\n" ? "LF" : "CRLF"} and still reject missing pages`, t => {
    const dir = fixture(t);
    writeFileSync(join(dir, "astro.config.mjs"), ["export default {", "  redirects: {", '    "/old/": "/gh-aw/page/",', '    "/external/": "https://example.com/",', "  },", "};", ""].join(newline));
    const page = join(dir, "src/content/docs/page.md");
    writeFileSync(page, "# Page\n");
    const script = join(dir, "scripts/check-redirects.mjs");
    assert.match(execFileSync(process.execPath, [script], { encoding: "utf8" }), /Redirect check passed \(2 redirects\)/);
    rmSync(page);
    const result = spawnSync(process.execPath, [script], { encoding: "utf8" });
    assert.equal(result.status, 1);
    assert.match(result.stderr, /no page at src\/content\/docs\/page\.md\(x\)/);
  });
}

test("build cleanup removes a nested dist directory and tolerates a missing directory", t => {
  const dir = fixture(t);
  mkdirSync(join(dir, "dist/nested"), { recursive: true });
  writeFileSync(join(dir, "dist/nested/stale.html"), "stale");
  const { scripts } = JSON.parse(readFileSync(new URL("../package.json", import.meta.url), "utf8"));
  const cleanup = scripts.build.split(" && ")[0];
  execSync(cleanup, { cwd: dir });
  assert.equal(existsSync(join(dir, "dist")), false);
  execSync(cleanup, { cwd: dir });
});

for (const systemChromium of [true, false]) {
  test(`Playwright uses ${systemChromium ? "system" : "bundled"} Chromium when the system path ${systemChromium ? "exists" : "is absent"}`, () => {
    const config = new URL("../playwright.config.ts", import.meta.url).href;
    const output = execFileSync(
      process.execPath,
      [
        "--input-type=module",
        "-e",
        `
      import fs from "node:fs";
      import { syncBuiltinESMExports } from "node:module";
      const existsSync = fs.existsSync;
      fs.existsSync = (path) => path === "/usr/bin/chromium-browser" ? ${systemChromium} : existsSync(path);
      syncBuiltinESMExports();
      const { default: config } = await import(process.argv[1]);
      console.log(JSON.stringify(config.projects[0].use));
    `,
        config,
      ],
      { encoding: "utf8" }
    );
    const use = JSON.parse(output);
    assert.equal(use.launchOptions?.executablePath, systemChromium ? "/usr/bin/chromium-browser" : undefined);
    assert.equal(use.defaultBrowserType, "chromium");
  });
}
