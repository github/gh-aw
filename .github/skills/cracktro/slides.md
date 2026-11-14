# Cracktro slide list

The single ordered list of slides. A slide's number is its row position here (first row = slide 1, `#1`); never write slide numbers anywhere else. Refer to a slide by its heading (and CSS prefix); link previews as `#N` with N looked up here.

`docs/tests/talk.spec.ts` reads this file: it checks the deck has exactly these slides, in this order, with these headings, and that speaker notes of every `illustrative` row match `/illustrative|synthetic|simulated/i`.

Editing: add, remove or move one row per slide in the same change as `deck.js`. One row per line keeps concurrent PRs merge-friendly; do not renumber anything.

| Heading | Notes |
|---|---|
| GitHubAgenticWorkflows |  |
| Agentic Workflows |  |
| CI/CD toContinuousAI | illustrative |
| Anatomy of a workflow | illustrative |
| Turn Intent to Automation |  |
| Turn Automation into Value | illustrative |
| Security |  |
| Agentic attacks | illustrative |
| Safety and control |  |
| Zero-secret agents | illustrative |
| Supply chain hardening | illustrative |
| Safe outputs | illustrative |
| Continuous Security | illustrative |
| Need-to-know enclaves | illustrative |
| Stop manually translating intent into code. |  |
| Human agent processes |  |
| Human Agent Processes | illustrative |
| Pull Request Review² | illustrative |
| Issues as intent | illustrative |
| Repo Assist | illustrative |
| Daily Unsloppers | illustrative |
| Merge fast, unslop daily | illustrative |
| Research · Assign · Observe | illustrative |
| Query · Ask · Grow | illustrative |
| Mine · Refine · Apply | illustrative |
| Explore · Reuse · Lint | illustrative |
| Semantic clustering | illustrative |
| Spec · Model · Test | illustrative |
| Go Fan | illustrative |
| Spec-driven testing | illustrative |
| Docs that heal | illustrative |
| Speak · Map · Grep | illustrative |
| Scan · Fix · Learn | illustrative |
| For PMs | illustrative |
| If every PR needs deep human review, you don’t trust your process. | illustrative |
| Cost & context |  |
| Cost management | illustrative |
| Model routing | illustrative |
| Agents that debug agents | illustrative |
| Agentic Engineering |  |
| Any agent, any provider | illustrative |
| Agent memory | illustrative |
| Ephemerals | illustrative |
| Index & search | illustrative |
| Experiments, graders & evals | illustrative |
| Audit & Optimize | illustrative |
| Enterprise |  |
| Enterprise infrastructure as code | illustrative |
| OpenTelemetry | illustrative |
| GitHub-native governance | illustrative |
| Operator-owned infrastructure | illustrative |
| Centralized campaigns | illustrative |
| Push SDLC | illustrative |
| Connectors | illustrative |
| AW factory |  |
| First commit to GA |  |
| How PRs land |  |
| How workflows evolve |  |
| Patch to PR | illustrative |
| GıtHub Agentic Workflows | illustrative |
