global.fetch = async url => {
  require("node:fs").writeFileSync("/home/runner/work/gh-aw/gh-aw/actions/setup/js/shell-decision-cwd-O9ayNF/decision-cwd.json", JSON.stringify(process.env.GH_AW_ENGINE_CWD));
  if (url.endsWith("/reflect")) return new Response(JSON.stringify({ endpoints: [{ configured: true, provider: "github", models_url: "http://api-proxy:10002/v1/models" }] }));
  if ("unknown" === "api-error") return new Response("", { status: 503 });
  return new Response(JSON.stringify({ answers: [{ name: "skip_1", type: "choice", choice: "unknown" }] }));
};