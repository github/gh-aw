// @ts-check
"use strict";

/** @param {"SUB_AGENT"|"SKILL"} kind @param {{dir: string, ext: string}} fallback */
function inlineArtifactTarget(kind, fallback) {
  const dir = process.env[`GH_AW_${kind}_DIR`];
  const ext = process.env[`GH_AW_${kind}_EXT`];
  if (dir === undefined && ext === undefined) return fallback;
  if (!dir || !/^[.a-zA-Z0-9_-]+(?:\/[.a-zA-Z0-9_-]+)*$/.test(dir) || dir.split("/").some(part => part === "." || part === "..")) {
    throw new Error(`GH_AW_${kind}_DIR must be a workspace-relative directory without traversal`);
  }
  if (!ext || ![".md", ".agent.md", "/SKILL.md"].includes(ext)) throw new Error(`Invalid GH_AW_${kind}_EXT`);
  return { dir, ext };
}

module.exports = { inlineArtifactTarget };
