---
name: "squad-version-check"
description: "Internals of how @bradygaster/squad-cli stamps its version, how `squad upgrade` works (what it preserves vs overwrites), and how to probe the npm registry for the latest version from a coordinator prompt."
allowedTools: []
confidence: medium
domain: squad-internals
source: "Discovered by Data; validated in github/squad#1173 recon (2026-05-26)."
---

# SKILL: Squad CLI Internals — Version Stamping & Upgrade Mechanics

**Confidence:** medium
**Discovered by:** Data
**Date:** 2026-05-26
**Validated in:** Issue #1173 recon (github/squad)

---

## What This Skill Covers

Reusable knowledge about how `@bradygaster/squad-cli` stamps its version into `squad.agent.md`, how `squad upgrade` works, what it preserves vs. overwrites, and how to probe the npm registry for the latest version from a coordinator prompt.

---

## Package & Registry Facts

- **Package name:** `@bradygaster/squad-cli`
- **Registry:** npm (public)
- **CLI binary:** `squad` (registered via `package.json#bin.squad`)
- **Node version requirement:** Node ≥22.5.0 (ESM-only codebase)

---

## Version Stamping Mechanism

**Source file:** `dist/cli/core/version.js`

Three functions:

### `getPackageVersion()`
Walks up from the compiled JS file to find `package.json`. Returns `pkg.version`. Works from both `dist/cli/core/version.js` and a bundled root `cli.js`. Returns `'0.0.0'` as fallback if not found.

### `stampVersion(filePath, version)`
Mutates `squad.agent.md` in three places:
1. HTML comment: `<!-- version: {version} -->` (must be on the line immediately after frontmatter `---`)
2. Identity line: `- **Version:** {version}`
3. Greeting instruction: backtick-quoted `` `Squad v{version}` ``

**Called by:** both `init` and `upgrade` — after copying the template to the destination.

### `readInstalledVersion(filePath)`
Reads the stamped version back from `squad.agent.md`:
1. First tries HTML comment format: `/<!-- version: ([0-9.]+(?:-[a-z]+(?:\.\d+)?)?) -->/`
2. Falls back to old frontmatter format: `/^version:\s*"([^"]+)"/m`
3. Returns `'0.0.0'` on any error

---

## `squad upgrade` Behavior

**Source file:** `dist/cli/core/upgrade.js`

### What gets overwritten:
- `squad.agent.md` — full overwrite from template, then `stampVersion()`
- Files with `overwriteOnUpgrade: true` in `TEMPLATE_MANIFEST`: casting JSON files, template .md files, `copilot-instructions.md` (if @copilot enabled)
- GitHub Actions workflows — from `templates/workflows/`; non-npm projects get type-aware stubs
- Runs `runMigrations()` after file copy

### What is PRESERVED:
- `team.md`, `routing.md`, `decisions.md`, `ceremonies.md` (user-owned)
- `agents/*/history.md` (individual agent memory)
- `.squad/config.json` — **never touched**; `stateBackend` survives intact
- User-added files not in TEMPLATE_MANIFEST

### Self-upgrade path (`selfUpgradeCli()`):
Detects npm/pnpm/yarn via `npm_execpath` and `npm_config_user_agent`. Runs:
- npm: `npm install -g @bradygaster/squad-cli@latest`
- pnpm: `pnpm add -g @bradygaster/squad-cli@latest`
- yarn: `yarn global add @bradygaster/squad-cli@latest`
Use `@insider` tag for insider builds.

### `compareSemver(a, b)` utility (in upgrade.js):
Returns -1/0/1. Handles pre-release: strips pre-release for base comparison, then treats pre-release as less than release (e.g., `0.9.5-insider.1` < `0.9.5`). Can be ported directly if needed in prompt logic.

---

## `.squad/config.json` — What It Holds

```json
{
  "version": 1,
  "stateBackend": "worktree"
}
```

Other optional fields added by the coordinator at runtime:
- `defaultModel` — global model override for all agent spawns
- `agentModelOverrides.{agentName}` — per-agent model override

The file is read-only from the upgrade path's perspective. Only the coordinator writes to it (for model preferences).

---

## Version-Check Probe (npm Registry)

Use this one-liner from inside a coordinator prompt to fetch dist-tags:

```
npm view @bradygaster/squad-cli dist-tags --json
```

- Timeout: **5 seconds.** If no response within 5 seconds, abandon and show normal greeting.
- On success: extract `dist-tags[channel]` (e.g., `dist-tags["insider"]`).
- On any error (network failure, registry unreachable, parse error): show normal greeting.

---

## Upstream OS-Specific Cache

The CLI (`self-update.ts`) writes `latest` version info to an OS-specific path with a 24h TTL.

**One-liner to read the upstream cache:**
```
node -e "const p=require('path'),o=require('os');const b=process.env.APPDATA||(process.platform==='darwin'?p.join(o.homedir(),'Library','Application Support'):p.join(o.homedir(),'.config'));const f=p.join(b,'squad-cli','update-check.json');try{const d=JSON.parse(require('fs').readFileSync(f,'utf8'));const age=Date.now()-d.checkedAt;if(age<86400000)console.log(JSON.stringify(d));else console.log('STALE')}catch{console.log('MISS')}"
```

Output semantics:
- Valid JSON `{"latestVersion":"X.Y.Z","checkedAt":N}` → cache hit; use `latestVersion`
- `STALE` → cache expired (older than 24h); treat as no data
- `MISS` → cache missing or corrupt; treat as no data

**OS-specific cache path:**
- Windows: `%APPDATA%\squad-cli\update-check.json`
- Linux: `~/.config/squad-cli/update-check.json`
- macOS: `~/Library/Application Support/squad-cli/update-check.json`

---

## Repo-Local Cache Convention: `.squad/.cache/version-check.json`

Used by coordinator for `insider`/`preview` channels (the upstream cache only stores `latest`).

**Schema:**
```json
{
  "checkedAt": "2026-05-26T14:13:28.492Z",
  "currentVersion": "0.9.6-insider.2",
  "channel": "insider",
  "channelVersion": "0.9.7-insider.1"
}
```

**TTL:** 24 hours from `checkedAt`.
**Gitignore:** `.squad/.cache/` is listed in `.gitignore` — cache files are never committed.

---

## Key File Paths (installed CLI)

| Purpose | Path |
|---|---|
| Version utilities | `dist/cli/core/version.js` |
| Upgrade logic | `dist/cli/core/upgrade.js` |
| Init logic | `dist/cli/core/init.js` |
| Template manifest | `dist/cli/core/templates.js` |
| Copilot install helper | `dist/cli/copilot-install.js` |
| squad.agent.md template | `templates/squad.agent.md.template` |
| Session init reference | `templates/session-init-reference.md` |
| All templates | `templates/` |
