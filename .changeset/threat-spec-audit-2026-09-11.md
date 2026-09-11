---
"gh-aw": patch
---

Daily compiler threat spec audit (2026-09-11): reviewed issue #59894 (`[sighthound] Security findings in github/gh-aw`), which claimed a Critical CWE-78 command-injection vulnerability in `actions/setup/js/close_issue.cjs` via `exec` tainted by `commentBody`/`params`. Verification found this to be a false positive: the file has no `exec`/`child_process`/`spawn` call, is only 402 lines (the reported line 1110 does not exist), and the claimed `pkg/workflow/js/close_issue.cjs` mirror does not exist in the repository; `commentBody` flows only into the authenticated GitHub API client, never a shell. No new `CTR-*` rule or implementation change was required. Open high/critical code-scanning alerts (#675–#677) remain the previously-dismissed `go/allocation-size-overflow` capacity-hint pattern. No `threat-detection-suppress` annotations exist in any workflow. Bumped spec to 1.0.33.
