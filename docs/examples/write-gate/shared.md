---
jobs:
  safe_outputs:
    gate-steps:
      - name: Evaluate control repository write policy
        uses: actions/github-script@3a2844b7e9c422d3c10d287c895573f7108da1b3 # v9
        env:
          CONTROL_WORKFLOW_REF: ${{ github.workflow_ref }}
          CONTROL_WORKFLOW_SHA: ${{ github.workflow_sha }}
          CONTROL_RUN_ID: ${{ github.run_id }}
          CONTROL_RUN_ATTEMPT: ${{ github.run_attempt }}
        with:
          script: |
            const fs = require("node:fs");
            const { TextDecoder } = require("node:util");
            const object = value => value !== null && typeof value === "object" && !Array.isArray(value);
            const decisions = new Set(["apply", "hold", "review", "stage", "drop"]);
            const codeTypes = new Set(["create_pull_request", "push_to_pull_request_branch"]);
            const pathValid = path => typeof path === "string" && path.length > 0 &&
              !path.startsWith("/") && !/[\\\x00-\x1f\x7f]/.test(path) &&
              path.split("/").every(part => part !== "." && part !== "..");
            const write = decision => fs.writeFileSync(process.env.GH_AW_WRITE_DECISIONS,
              JSON.stringify(decision), { mode: 0o600 });
            try {
              const source = /^([^/]+)\/([^/]+)\/\.github\/workflows\/[^@]+@.+$/.exec(process.env.CONTROL_WORKFLOW_REF || "");
              const ref = process.env.CONTROL_WORKFLOW_SHA;
              if (!source || !/^[a-f0-9]{40}$/.test(ref || "")) throw new Error("workflow_identity");
              const response = await github.rest.repos.getContent({
                owner: source[1], repo: source[2],
                path: ".github/write-policy.json", ref
              });
              const data = response.data;
              if (!object(data) || data.type !== "file" || data.encoding !== "base64" ||
                  !Number.isSafeInteger(data.size) || data.size < 1 || data.size > 65536 ||
                  typeof data.content !== "string") throw new Error("policy_file");
              const bytes = Buffer.from(data.content, "base64");
              if (bytes.length !== data.size) throw new Error("policy_size");
              const policy = JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(bytes));
              if (!object(policy) || policy.version !== 1 || !object(policy.types) || !object(policy.paths) ||
                  Object.values(policy.types).some(value => !decisions.has(value)) ||
                  Object.keys(policy.paths).some(key => !["apply", "review", "hold"].includes(key)) ||
                  ["apply", "review", "hold"].some(key => !Array.isArray(policy.paths[key]) ||
                    policy.paths[key].some(path => !pathValid(path)))) throw new Error("policy_schema");
              const fd = fs.openSync(process.env.GH_AW_PENDING_WRITES,
                fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW | fs.constants.O_NONBLOCK);
              let manifest;
              try {
                const stat = fs.fstatSync(fd);
                if (!stat.isFile() || stat.nlink !== 1 || stat.size < 1 || stat.size > 1048576) {
                  throw new Error("manifest_file");
                }
                const bytes = fs.readFileSync(fd);
                if (bytes.length !== stat.size || bytes.length > 1048576) throw new Error("manifest_size");
                manifest = JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(bytes));
              } finally {
                fs.closeSync(fd);
              }
              if (!object(manifest) || manifest.schema !== "gh-aw/pending-writes/v1" ||
                  !object(manifest.run) || manifest.run.workflow_ref !== process.env.CONTROL_WORKFLOW_REF ||
                  manifest.run.workflow_sha !== ref || manifest.run.run_id !== process.env.CONTROL_RUN_ID ||
                  manifest.run.run_attempt !== process.env.CONTROL_RUN_ATTEMPT ||
                  manifest.complete !== true || !Array.isArray(manifest.inputs) || !Array.isArray(manifest.items) ||
                  manifest.items.length > 1000) throw new Error("manifest_schema");
              const seen = new Set();
              const items = manifest.items.map(item => {
                if (!object(item) || item.complete !== true || typeof item.type !== "string" ||
                    typeof item.output_id !== "string" || !/^(so_[A-Za-z0-9_-]{12}|job_[a-f0-9]{16})$/.test(item.output_id) ||
                    !/^[a-f0-9]{64}$/.test(item.item_sha256 || "") || seen.has(item.output_id)) {
                  throw new Error("manifest_item");
                }
                seen.add(item.output_id);
                let decision = Object.hasOwn(policy.types, item.type) ? policy.types[item.type] : "drop";
                if (codeTypes.has(item.type)) {
                  if (!object(item.code) || item.code.complete !== true ||
                      !Array.isArray(item.code.files) || item.code.files.length === 0 ||
                      !Array.isArray(item.code.evaluated_paths) || item.code.evaluated_paths.length === 0 ||
                      item.code.evaluated_paths.some(path => !pathValid(path))) throw new Error("code_evidence");
                  const matches = (path, category) => policy.paths[category].some(rule =>
                    rule.endsWith("/") ? path.startsWith(rule) : path === rule);
                  const rank = { apply: 0, hold: 1, review: 2, stage: 3, drop: 4 };
                  for (const path of item.code.evaluated_paths) {
                    const categories = ["apply", "hold", "review"].filter(category => matches(path, category));
                    if (categories.length === 0) categories.push("drop");
                    for (const category of categories) {
                      if (rank[category] > rank[decision]) decision = category;
                    }
                  }
                }
                return { output_id: item.output_id, decision, reason: `policy_${decision}` };
              });
              write({ decision: "partial", reason: "policy_evaluated", items });
            } catch {
              write({ decision: "deny", reason: "policy_error" });
              core.setFailed("Reference write gate: policy or manifest validation failed; writes denied.");
            }
---

The deterministic gate reads `.github/write-policy.json` from the control workflow's exact commit.
This shared component is an example, not an active repository workflow.
