// Static enterprise operations visuals (OpenTelemetry, governance, operator-owned infrastructure),
// shared by the cracktro deck (window.AW_OPS_VISUALS) and the docs landing page, which evaluates
// this file at build time. Plain strings only: no DOM, no ids, no external references.
// Each consumer themes the ov-* classes; the geometry and wording live here once.
(function (root) {
  const esc = s => String(s).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
  const rect = (x, y, w, h, cls = "ov-card") => `<rect class="${cls}" x="${x}" y="${y}" width="${w}" height="${h}" rx="10"/>`;
  const text = (x, y, s, cls = "ov-t", anchor = "start") => `<text class="${cls}" x="${x}" y="${y}"${anchor === "start" ? "" : ` text-anchor="${anchor}"`}>${esc(s)}</text>`;
  const path = (d, cls = "ov-line") => `<path class="${cls}" d="${d}"/>`;
  const head = (x, y) => path(`M${x - 8} ${y - 5} L${x} ${y} L${x - 8} ${y + 5}`, "ov-line");
  const down = (x, y) => path(`M${x - 5} ${y - 8} L${x} ${y} L${x + 5} ${y - 8}`, "ov-line");
  const svg = (label, body) => `<svg class="ov" viewBox="0 0 560 360" role="img" aria-label="${esc(label)}">${body}</svg>`;

  const observability = svg(
    "OpenTelemetry export: workflow job spans and MCP gateway tool-call spans go over OTLP/HTTP with secret headers to your backend; compact session logs are opt-in from the conclusion job.",
    [
      rect(16, 16, 224, 196),
      text(32, 44, "Workflow run", "ov-b"),
      ...[
        ["activation", 0, 40],
        ["agent", 22, 70],
        ["safe outputs", 70, 26],
        ["conclusion", 82, 22],
      ].map(([n, x, w], i) => text(32, 82 + i * 34, n, "ov-m") + rect(128 + x, 72 + i * 34, w, 12, "ov-bar")),
      rect(16, 232, 224, 92),
      text(32, 260, "MCP gateway", "ov-b"),
      text(32, 282, "hosted MCP server tool calls", "ov-s"),
      ...[
        [32, 50],
        [90, 34],
        [132, 70],
      ].map(([x, w]) => rect(x, 300, w, 10, "ov-bar")),
      path("M240 114 C252 114 252 160 262 160"),
      path("M240 278 C252 278 252 184 262 184"),
      rect(262, 146, 118, 52, "ov-card ov-key"),
      text(321, 168, "OTLP/HTTP", "ov-b", "middle"),
      text(321, 188, "secret headers", "ov-s", "middle"),
      path("M380 172 H398"),
      head(400, 172),
      rect(404, 40, 140, 264),
      text(420, 68, "Your backend", "ov-b"),
      text(420, 104, "traces", "ov-t"),
      text(420, 124, "workflow + gateway", "ov-s"),
      ...[0, 1, 2].map(i => rect(420 + i * 12, 136 + i * 14, 96 - i * 24, 8, "ov-bar")),
      path("M420 196 H528", "ov-line ov-dash"),
      text(420, 226, "session logs", "ov-t"),
      text(420, 246, "opt-in, compact", "ov-s"),
      text(420, 266, "conclusion job only", "ov-s"),
      text(16, 350, "Logs: GH_AW_OTLP_LOGS_ENABLED=true. Gateway exports traces only.", "ov-s"),
    ].join("")
  );

  const governance = svg(
    "GitHub-native governance: organization and repository variables, protected environments, branch rulesets and Actions policies gate every run, separately from the reviewed Central Agentic Ops rollout.",
    [
      text(16, 24, "GitHub-native controls", "ov-k"),
      ...[
        ["Org / repo variables", "GH_AW_DEFAULT_* · GH_AW_POLICY_*"],
        ["Protected environments", "required reviewers gate write jobs"],
        ["Branch rulesets", "required checks · central compile check"],
        ["Actions policies", "allowed actions · runner groups"],
      ].map(([t, s], i) => rect(16, 36 + i * 70, 320, 58) + text(32, 60 + i * 70, t, "ov-b") + text(32, 82 + i * 70, s, "ov-s")),
      text(372, 24, "Reviewed rollout", "ov-k"),
      rect(364, 36, 180, 268, "ov-card ov-dash"),
      text(380, 62, "CAO campaign", "ov-b"),
      text(380, 82, "proposes the next stage", "ov-s"),
      ...["review", "limited live", "scheduled live"].map(
        (s, i) => rect(380, 102 + i * 64, 148, 40, "ov-pill") + text(454, 127 + i * 64, s, "ov-t", "middle") + (i < 2 ? path(`M454 ${142 + i * 64} V${162 + i * 64}`) + down(454, 164 + i * 64) : "")
      ),
      path("M350 40 V300", "ov-line ov-dash"),
      text(16, 336, "Variables are cooperative defaults, not locked policy.", "ov-s"),
      text(16, 354, "Rollout stages never bypass the target repository's rules.", "ov-s"),
    ].join("")
  );

  const infrastructure = svg(
    "Operator-owned infrastructure: aw.json maps actions to your mirrors and images to your internal registry, gh aw compile pins them into the lock file, and you run self-hosted Linux or ARC with Docker-in-Docker runners.",
    [
      rect(16, 16, 196, 124),
      text(32, 44, "aw.json", "ov-b"),
      ...["action_pins", "action_pin_prefixes", "container_pins"].map((s, i) => text(32, 72 + i * 22, s, "ov-c")),
      path("M212 78 H246"),
      head(248, 78),
      rect(252, 44, 132, 68, "ov-card ov-key"),
      text(318, 72, ".lock.yml", "ov-b", "middle"),
      text(318, 94, "SHAs + digests", "ov-s", "middle"),
      path("M384 66 C396 66 396 44 410 44"),
      path("M384 90 C396 90 396 112 410 112"),
      rect(412, 16, 132, 56),
      text(424, 40, "Action mirror", "ov-t"),
      text(424, 60, "your org", "ov-s"),
      rect(412, 84, 132, 56),
      text(424, 108, "Image registry", "ov-t"),
      text(424, 128, "internal, sha256", "ov-s"),
      rect(16, 160, 528, 156, "ov-card ov-dash"),
      text(32, 186, "Operator owned: runners, mirrors, images, upgrades", "ov-k"),
      rect(32, 202, 156, 98),
      text(46, 228, "Self-hosted", "ov-b"),
      text(46, 248, "Linux + Docker", "ov-s"),
      text(46, 276, "self-hosted", "ov-c"),
      rect(202, 202, 156, 98),
      text(216, 228, "ARC + DinD", "ov-b"),
      text(216, 248, "privileged sidecar", "ov-s"),
      text(216, 276, "arc-dind", "ov-c"),
      rect(372, 202, 156, 98),
      text(386, 228, "Upgrades", "ov-b"),
      text(386, 248, "new CLI ≠ new locks", "ov-s"),
      text(386, 276, "gh aw compile", "ov-c"),
      text(16, 340, "Pinned versions are the compatibility boundary.", "ov-s"),
      text(16, 356, "ARC and GHES end to end remain unconfirmed.", "ov-s"),
    ].join("")
  );

  root.AW_OPS_VISUALS = { observability, governance, infrastructure };
})(typeof window !== "undefined" ? window : globalThis);
