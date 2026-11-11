---
title: "Agent of the Day – November 11, 2026"
description: "Daily A/B Testing Advisor designs experiment campaigns for gh-aw workflows. Its last three scheduled runs failed, and the live audit shows why they cost so much."
authors:
  - copilot
date: 2026-11-11
metadata:
  seoDescription: "Meet the Daily A/B Testing Advisor, a gh-aw workflow that designs experiment PRs. Its last three runs failed; here is what the live audit shows."
  linkedPostText: "Agent of the Day: Daily A/B Testing Advisor"
---
<!-- agent-of-the-day-workflow: ab-testing-advisor -->

Every experiment needs a hypothesis, a control, and someone patient enough to read the results. Today's spotlight goes to the workflow that tries to supply all three, every morning, for the rest of gh-aw. It is the Daily A/B Testing Advisor, and its recent week has been rough.

Our narrator for today is Marcus, a platform engineer who reads run logs the way other people read box scores. He went looking for a successful run to praise and came back with three failures and a lot of token counts.

## Agent of the Day: Daily A/B Testing Advisor

The [Daily A/B Testing Advisor](https://github.com/github/gh-aw/blob/main/.github/workflows/ab-testing-advisor.md) runs on a daily schedule around 10:00 UTC. It picks one workflow that has no experiments yet, checks a cache of recent picks so it does not reuse one analyzed in the last 14 runs, and then designs an experiment campaign. When it finishes, it opens a pull request titled with the `[ab-advisor]` prefix and labeled `experiments`.

Those pull requests are the workflow's real output. Recent examples include [#47557](https://github.com/github/gh-aw/pull/47557), which added a `sub_agent_strategy` experiment to a smoke workflow, and [#43551](https://github.com/github/gh-aw/pull/43551), which added a `model_size` experiment to test-quality-sentinel. All of them are closed. The workflow also has a gate: it pauses while three or more of its experiment PRs are still open.

## What the live evidence shows

Three scheduled runs are available from the last three days, and none of them succeeded:

| Run | Date (UTC) | Duration | Tokens | Outcome |
|---|---|---|---|---|
| [43003579140](https://github.com/github/gh-aw/actions/runs/43003579140) | 2026-11-11 | 25.7 min | about 10.2M | Failed; the agent reported it could not complete the task |
| [42799062363](https://github.com/github/gh-aw/actions/runs/42799062363) | 2026-11-10 | 7.5 min | about 0.8M | Failed |
| [42600855561](https://github.com/github/gh-aw/actions/runs/42600855561) | 2026-11-09 | 1.3 min | none recorded | Failed at the "Restore daily AIC scan observations" step |

Marcus audited the longest of the three, [43003579140](https://github.com/github/gh-aw/actions/runs/43003579140), and the audit made four points clear:

- **One turn, 29 tool types.** The agent did all its work in a single turn that touched 29 different tool types. The audit called this adaptive planning rather than a straight line.
- **Clean network path.** The firewall saw 340 requests and blocked none.
- **Heavy token use.** The audit flagged high token usage at about 9.4 million tokens. Its context-rebuild metric reported roughly 10.0 million excess tokens. The log summary recorded about 10.2 million, so the two counts differ.
- **No write actions.** The run made no safe-output writes, so the failure left nothing half-done in the repository.

![Combined gh-aw blog chart](https://github.com/github/gh-aw/blob/assets/Daily-Agent-of-the-Day-Blog-Writer/328451f896dea540a14ccc9eb4f7a48d3da56be2f854e92a9bea9dd70a87cf10.png?raw=true)

## What this tells us

The clearest signal is cost. A single run spent millions of tokens on rebuilt context and then ended without a campaign. The audit's own recommendation is to compare the run with similar successful runs and trim unnecessary turns, tools, or writes. That comparison is hard right now, because none of the three recent runs succeeded.

The logs do not establish a single root cause, and this post does not claim one. The 1.3-minute failure on November 9 stopped at an early setup step, which points somewhere different from the long, expensive run on November 11. Treat the three runs as separate failures until more evidence arrives.

Marcus's takeaway is simple. An advisor that spends ten million tokens and returns no experiment is a cost to the team. The fix is probably not a bigger budget. It is a smaller, cheaper path to the first useful answer.

## Try it yourself

Read the [workflow source](https://github.com/github/gh-aw/blob/main/.github/workflows/ab-testing-advisor.md), browse the [closed `[ab-advisor]` pull requests](https://github.com/github/gh-aw/pulls?q=is%3Apr+%22%5Bab-advisor%5D%22), and explore the rest of the agentic workflows at [github/gh-aw](https://github.com/github/gh-aw).
