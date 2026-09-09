import { RuleTester } from "eslint";
import { describe, it } from "vitest";
import { requireHttpResponseErrorListenerRule } from "./require-http-response-error-listener";

const cjsRuleTester = new RuleTester({
  languageOptions: {
    ecmaVersion: 2022,
    sourceType: "commonjs",
  },
});

describe("require-http-response-error-listener", () => {
  it("valid: response callbacks that attach an 'error' listener pass", () => {
    cjsRuleTester.run("require-http-response-error-listener", requireHttpResponseErrorListenerRule, {
      valid: [
        `const http = require("http"); http.request(options, res => { res.on("data", () => {}); res.on("error", reject); });`,
        `const https = require("https"); https.get(url, res => { res.once("error", err => { reject(err); }); res.on("end", () => {}); });`,
        `const http = require("node:http"); http.request(options, function (res) { res.on("error", reject); });`,
        `const https = require("node:https"); https.request(options, res => { res.on("end", () => {}); if (res.statusCode !== 200) { res.resume(); } res.on("error", reject); });`,
      ],
      invalid: [],
    });
  });

  it("invalid: response callbacks without an 'error' listener are reported", () => {
    cjsRuleTester.run("require-http-response-error-listener", requireHttpResponseErrorListenerRule, {
      valid: [],
      invalid: [
        {
          code: `const http = require("http"); const req = http.request(options, res => { let data = ""; res.on("data", chunk => { data += chunk; }); res.on("end", () => { resolve(data); }); }); req.on("error", reject);`,
          errors: [{ messageId: "missingResponseErrorListener" }],
        },
        {
          code: `const https = require("https"); https.get(url, res => { res.on("end", () => {}); });`,
          errors: [{ messageId: "missingResponseErrorListener" }],
        },
        {
          code: `const http = require("node:http"); http.request(options, function (res) { res.resume(); });`,
          errors: [{ messageId: "missingResponseErrorListener" }],
        },
        {
          code: `
            const https = require("https");
            const http = require("http");
            const protocol = url.startsWith("https") ? https : http;
            protocol
              .get(url, res => {
                if (res.statusCode !== 200) {
                  reject(new Error(\`Failed to fetch URL \${url}: HTTP \${res.statusCode}\`));
                  return;
                }
                let data = "";
                res.on("data", chunk => { data += chunk; });
                res.on("end", () => { resolve(data); });
              })
              .on("error", err => {
                reject(new Error(\`Failed to fetch URL \${url}: \${err.message}\`));
              });
          `,
          errors: [{ messageId: "missingResponseErrorListener" }],
        },
      ],
    });
  });

  it("valid: identifiers not bound to Node's http/https modules are ignored", () => {
    cjsRuleTester.run("require-http-response-error-listener", requireHttpResponseErrorListenerRule, {
      valid: [
        `const http = require("./my-http-helper.cjs"); http.request(options, res => { res.on("end", () => {}); });`,
        `const https = { request(options, cb) {} }; https.request(options, res => { res.on("data", () => {}); });`,
        `http.request(options, res => { res.on("end", () => {}); });`,
      ],
      invalid: [],
    });
  });

  it("valid: request calls without a response callback are ignored", () => {
    cjsRuleTester.run("require-http-response-error-listener", requireHttpResponseErrorListenerRule, {
      valid: [`const http = require("http"); const req = http.request(options); req.on("error", reject);`, `const https = require("https"); https.get(url);`],
      invalid: [],
    });
  });

  it("valid: bindings from a shadowed require or reassigned after initialization are ignored", () => {
    cjsRuleTester.run("require-http-response-error-listener", requireHttpResponseErrorListenerRule, {
      valid: [
        `function factory(require) { const http = require("http"); http.request(options, res => { res.on("end", () => {}); }); }`,
        `const require = createRequire("./x.cjs"); const http = require("http"); http.request(options, res => { res.on("end", () => {}); });`,
        `let http = require("http"); http = makeClient(); http.request(options, res => { res.on("end", () => {}); });`,
      ],
      invalid: [],
    });
  });

  it("valid: nested error listener registrations count", () => {
    cjsRuleTester.run("require-http-response-error-listener", requireHttpResponseErrorListenerRule, {
      valid: [`const http = require("http"); http.request(options, res => { setImmediate(() => { res.on("error", reject); }); res.on("end", () => {}); });`],
      invalid: [],
    });
  });

  it("valid: '.addListener(\"error\", ...)' is treated as an 'error' listener", () => {
    cjsRuleTester.run("require-http-response-error-listener", requireHttpResponseErrorListenerRule, {
      valid: [
        `const http = require("http"); http.request(options, res => { res.on("data", () => {}); res.addListener("error", reject); });`,
        `const https = require("https"); https.get(url, res => { res.addListener("error", err => { reject(err); }); res.on("end", () => {}); });`,
      ],
      invalid: [],
    });
  });

  it("valid: request bindings reassigned to an unrelated object are ignored", () => {
    cjsRuleTester.run("require-http-response-error-listener", requireHttpResponseErrorListenerRule, {
      valid: [
        `const http = require("http"); let req = http.request(options); req = makeClient(); req.on("response", res => { res.resume(); });`,
        `const http = require("http"); let req = makeClient(); req = http.request(options); req.on("response", res => { res.resume(); });`,
      ],
      invalid: [],
    });
  });

  it("invalid: the 'req.on(\"response\", cb)' idiom is checked for a response 'error' listener", () => {
    cjsRuleTester.run("require-http-response-error-listener", requireHttpResponseErrorListenerRule, {
      valid: [
        `const http = require("http"); const req = http.request(options); req.on("response", res => { res.on("data", () => {}); res.on("error", reject); }); req.on("error", reject);`,
        `const https = require("https"); const req = https.get(url); req.once("response", res => { res.on("error", reject); }); req.on("error", reject);`,
        `const http = require("http"); http.request(options).on("response", res => { res.on("error", reject); }).on("error", reject);`,
      ],
      invalid: [
        {
          code: `const http = require("http"); const req = http.request(options); req.on("response", res => { let data = ""; res.on("data", chunk => { data += chunk; }); }); req.on("error", reject);`,
          errors: [{ messageId: "missingResponseErrorListener" }],
        },
        {
          code: `const http = require("http"); http.request(options).on("response", res => { res.resume(); }).on("error", reject);`,
          errors: [{ messageId: "missingResponseErrorListener" }],
        },
      ],
    });
  });
});
