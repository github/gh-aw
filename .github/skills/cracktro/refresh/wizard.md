# Refresh: wizard (W)

## 1. Scope

The cracktro wizard (`docs/public/cracktro/wizard.js`, `wz-` classes) replays the public githubnext/gh-aw-wizard flow as a choose-your-own-adventure. `wizard.js` is hand-written; its data and prompt generator live in the generated `docs/public/cracktro/wizard-data.js` (`window.CracktroWizardData`). Never edit the generated file by hand.

## 2. Source

- Data: `https://githubnext.github.io/gh-aw-wizard/wizard-data.json` (steps, archetypes, engines, prompt templates).
- Generator modules: `src/js/{patterns,engines,summary,workflow}.js` from `githubnext/gh-aw-wizard` (MIT), read with `gh api`. They keep upstream function names (`buildWorkflowSummary`, `generateAgentPrompt`, ...) that `wizard.js` calls.

## 3. Run

From the repository root:

```bash
node .github/skills/cracktro/refresh/wizard.mjs
node --check docs/public/cracktro/wizard-data.js
```

The script drops the upstream assistant, footer links, `top_repos` and `anti_patterns`, strips ESM `import`/`export`, keeps the MIT notice and fails on any 40-hex SHA.

## 4. Update steps

1. Run the script and diff `wizard-data.js`.
2. If upstream added steps, option fields or generator exports, update `wizard.js` (chapters `CH`, `answers()`, option renderers) to match the upstream `src/js/main.js` flow.
3. Map new archetype or option icons in the `OCTI` table of `wizard.js`; unknown icons fall back to the `tools` octicon.
4. Update the wizard Exception bullet in `content-rules.md` if new URLs or names appear.

## 5. Validation

- Preview loop (SKILL.md): press W, walk every chapter, roll the die (R), open "More companions", copy (C) and restart (N); check the tale reads cleanly and `window.__errs` stays empty.
- `npx playwright test tests/talk.spec.ts -g wizard` from `docs/`.

## 6. Pitfalls

- Engine names and companies are copied as published upstream (for example the Pi engine's company); report errors upstream rather than patching the generated file.
- Option text may end with punctuation; the tale strips it before joining sentences.
