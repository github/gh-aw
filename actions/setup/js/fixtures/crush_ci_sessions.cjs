// @ts-check

// Failure run https://github.com/github/gh-aw/actions/runs/37866184524,
// agent artifact 11588688740/agent-stdio.log. Infrastructure-only excerpt;
// host paths and endpoint details are replaced. The trailing result was added
// by the old parser bootstrap, not emitted by Crush.
const infrastructureOnly = [
  "[INFO] Executing agent command...\n",
  "[entrypoint] Agentic Workflow Firewall - Agent Container\n",
  "[entrypoint] Chroot mode enabled - dropping CAP_SYS_CHROOT and CAP_SYS_ADMIN\n",
  "[health-check] API Proxy Pre-flight Check\n",
  "[health-check] All API proxy health checks passed\n",
  "\n",
  "[crush-harness] awf-reflect: fetching http://sanitized-endpoint/reflect (timeout=60000ms)\n",
  "[crush-harness] resolved crush to /sanitized/bin/crush\n",
].join("");
const bootstrapResult = { type: "result", num_turns: 0 };

// Success https://github.com/github/gh-aw/actions/runs/37553337234 has only
// info/usage artifacts and skipped Agent. No native success fixture is claimed.
// Failure 37829720290 has neither accessible artifacts nor accessible job logs.
module.exports = { infrastructureOnly, bootstrapResult };
