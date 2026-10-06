# Extracted Code-Quality Tasks (flat-copy workaround, started 2026-10-06 — nested extracted-tasks.md frozen at 2026-09-07 snapshot)

## 2026-10-06 (cycle 8) — tasks filed as issues this cycle
1. push_repo_memory nested-subdir glob bug still live despite #65642/#65657 closed (HIGH — fixes this workflow's own data loss)
2. Anthropic WIF YAML snippet missing in quick-start.mdx (chronic since 2026-08-22)
3. exec.Command→exec.CommandContext in pkg/cli/git.go + pr_command.go (20+20 sites, verified)
4. Verify execcommandwithoutcontext/ctxbackground linter enforcement against pkg/cli
5. Remove hidden context.Background() in github_cli.go wrappers + log-parsing chain
6. Replace 4 production time.Sleep calls with ctx-aware waits
7. Replace grep-based secrets job/step-level heuristic with YAML parsing (daily-secrets-analysis)

(Older task history lives in the nested `deep-report/extracted-tasks.md`, frozen at 2026-09-07.)
