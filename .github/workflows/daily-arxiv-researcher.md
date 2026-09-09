---
emoji: "🔬"
description: Daily scan of latest arXiv papers for actionable improvements to GitHub Agentic Workflows
on:
  schedule:
    - cron: "daily around 8:00"
  workflow_dispatch:

permissions:
  contents: read
  copilot-requests: write

engine: copilot
model: large

timeout-minutes: 20
max-ai-credits: 300

tools:
  cache-memory:
    key: arxiv-paper-dedup
    retention-days: 90
    allowed-extensions: [".json"]
  repo-memory:
    branch-name: memory/arxiv-paper-ledger
    allowed-extensions: [".json", ".md"]
    format-json: true
  bash:
    - "cat *"
    - "ls *"
    - "curl *"
    - "base64 *"

network:
  allowed:
    - defaults
    - arxiv.org
    - export.arxiv.org

imports:
  - shared/mcp/kreuzberg.md

safe-outputs:
  allowed-domains: ["arxiv.org", "export.arxiv.org"]
  create-discussion:
    category: "research"
    expires: 14d
    max: 1

steps:
  - name: Fetch and parse arXiv papers
    uses: actions/github-script@v9.0.0
    with:
      script: |
        const fs = require('fs');
        const https = require('https');

        const BASE_DIR = '/tmp/gh-aw/agent/arxiv';
        const PAPERS_JSON = `${BASE_DIR}/papers.json`;
        const NEW_PAPERS_JSON = `${BASE_DIR}/new-papers.json`;
        const SEEN_IDS_JSON = '/tmp/gh-aw/cache-memory/seen-paper-ids.json';

        fs.mkdirSync(BASE_DIR, { recursive: true });

        const ARXIV_URL = 'https://export.arxiv.org/api/query?search_query=(cat:cs.AI+OR+cat:cs.SE+OR+cat:cs.LG)+AND+(agentic+OR+%22multi-agent%22+OR+%22llm+agent%22+OR+%22workflow+automation%22+OR+%22code+generation%22+OR+%22ai+agent%22)&max_results=25&sortBy=submittedDate&sortOrder=descending';

        let xml = '';
        try {
          xml = await new Promise((resolve, reject) => {
            const req = https.get(ARXIV_URL, { timeout: 30000 }, res => {
              const chunks = [];
              res.on('data', c => chunks.push(c));
              res.on('end', () => resolve(Buffer.concat(chunks).toString('utf8')));
            });
            req.on('error', reject);
            req.on('timeout', () => { req.destroy(); reject(new Error('request timed out')); });
          });
        } catch (e) {
          core.warning(`arXiv fetch failed: ${e.message}`);
        }

        const decodeEntities = s => s.replace(/&amp;/g, '&').replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&quot;/g, '"');
        const getText = (tag, s) => {
          const m = s.match(new RegExp(`<${tag}[^>]*>([\\s\\S]*?)</${tag}>`));
          return m ? decodeEntities(m[1].replace(/\s+/g, ' ').trim()) : '';
        };
        const getAllText = (tag, s) => {
          const re = new RegExp(`<${tag}[^>]*>([\\s\\S]*?)</${tag}>`, 'g');
          const out = []; let m;
          while ((m = re.exec(s)) !== null) out.push(decodeEntities(m[1].replace(/\s+/g, ' ').trim()));
          return out;
        };
        const getAllAttr = (tag, attr, s) => {
          const re = new RegExp(`<${tag}[^>]*${attr}="([^"]*)"`, 'g');
          const out = []; let m;
          while ((m = re.exec(s)) !== null) out.push(m[1]);
          return out;
        };

        const papers = [];
        const entryRe = /<entry>([\s\S]*?)<\/entry>/g;
        let em;
        while ((em = entryRe.exec(xml)) !== null) {
          const entry = em[1];
          const idUrl = getText('id', entry);
          const arxivId = idUrl.replace(/.*abs\//, '').trim();
          if (!arxivId) continue;
          papers.push({
            id: arxivId,
            title: getText('title', entry),
            abstract: getText('summary', entry).slice(0, 800),
            authors: getAllText('name', entry).slice(0, 3),
            published: getText('published', entry).slice(0, 10),
            categories: getAllAttr('category', 'term', entry).slice(0, 3),
            url: `https://arxiv.org/abs/${arxivId}`
          });
        }

        const fetchedAt = new Date().toISOString().slice(0, 10);
        fs.writeFileSync(PAPERS_JSON, JSON.stringify({ fetched_at: fetchedAt, count: papers.length, papers }, null, 2));

        const seenIds = new Set();
        if (fs.existsSync(SEEN_IDS_JSON)) {
          try {
            const d = JSON.parse(fs.readFileSync(SEEN_IDS_JSON, 'utf8'));
            for (const id of (d.ids || [])) seenIds.add(id);
          } catch (_) {}
        }

        const newPapers = papers.filter(p => !seenIds.has(p.id));
        fs.writeFileSync(NEW_PAPERS_JSON, JSON.stringify({
          total_fetched: papers.length,
          already_seen: papers.length - newPapers.length,
          new_count: newPapers.length,
          fetched_at: fetchedAt,
          papers: newPapers
        }, null, 2));

        core.info(`Parsed ${papers.length} papers, ${newPapers.length} new`);

sandbox:
  agent:
    id: awf
    runtime: cloud-hypervisor
features:
  gh-aw-detection: true
---

# arXiv Paper Researcher: GitHub Agentic Workflows

You are a research agent scanning the latest arXiv papers for actionable improvements to GitHub Agentic Workflows (gh-aw) — a system that compiles markdown workflows into GitHub Actions with pluggable AI engines (Claude, Copilot, Gemini, Codex).

## Context

- **Repository**: ${{ github.repository }}
- **Run Date**: $(date +%Y-%m-%d)
- **gh-aw features**: workflow compiler (Go), safe-outputs (typed write operations), network firewall (AWF), token optimization, sub-agents, cache-memory, repo-memory, agentic engine integration, inline prompts, shared imports

## Step 1: Load Pre-Fetched Data

Read `/tmp/gh-aw/agent/arxiv/new-papers.json`.

If `new_count` is 0, call `noop` with message:
"No new arXiv papers today — all N previously processed."
Then stop immediately.

## Step 1b: Investigate Paper PDFs

For each paper in `papers`, invoke the `preliminary-investigator` sub-agent with its full paper object. Merge the returned `pdf_fetched` and `preliminary_note` fields into that paper object.

PDF extraction failures are non-fatal: continue screening and ranking with an empty `preliminary_note` when `pdf_fetched` is `false`.

## Step 2: Screen Papers for Relevance

For each paper in `papers`, invoke the `paper-screener` sub-agent with input:
```json
{"title": "...", "abstract": "...", "preliminary_note": "..."}
```

Collect only papers where the screener returns `{"relevant": true, ...}`.

If no papers are relevant, proceed to Step 4 to update the ledger, then call `noop`:
"N papers screened, none relevant to gh-aw today."
Stop after the ledger update.

## Step 2b: Rank Relevant Papers

For each relevant paper, invoke the `relevance-ranker` sub-agent with:
```json
{"title": "...", "abstract": "...", "preliminary_note": "..."}
```

Sort ranked papers by `score` descending. Keep only the top 3.

## Step 3: Extract Improvement Opportunities

For each top-ranked paper (max 3), invoke the `opportunity-extractor` sub-agent with the full paper object.

Collect the returned opportunity objects.

## Step 4: Update the Paper Ledger

Load `/tmp/gh-aw/repo-memory/default/paper-ledger.md` if it exists; otherwise start with:
```markdown
# arXiv Paper Ledger

Papers investigated for GitHub Agentic Workflows improvement opportunities.
```

Load `/tmp/gh-aw/repo-memory/default/paper-index.json` if it exists; otherwise use `{"papers": []}`.

For every paper processed in Step 2 (all new papers regardless of relevance), append to the ledger:

```markdown
### [TITLE](URL)
- **ID**: arxiv_id
- **Published**: YYYY-MM-DD
- **Categories**: cat1, cat2
- **Relevant**: Yes / No
- **Opportunity**: (if relevant) concise opportunity text; omit line if not relevant
- **Area**: (if relevant) area; omit line if not relevant
```

Append to the index JSON array:
`{"id": "...", "title": "...", "published": "...", "relevant": true/false, "analyzed_at": "YYYY-MM-DD-HH-MM-SS"}`

Write the updated ledger to `/tmp/gh-aw/repo-memory/default/paper-ledger.md`.
Write the updated index to `/tmp/gh-aw/repo-memory/default/paper-index.json`.

Update the dedup cache at `/tmp/gh-aw/cache-memory/seen-paper-ids.json`:
Load existing `{"ids": [...]}` or start with `{"ids": []}`.
Append all paper IDs from `new-papers.json` (whether relevant or not).
Write back — no colons in filenames.

## Step 5: Create Discussion or Noop

**If actionable opportunities were found**: create a discussion titled:
`[arXiv Research] Agentic Workflow Improvements — YYYY-MM-DD`

Use the `discussion-template` skill to format the discussion body.

**If no actionable opportunities were found** (but papers were processed and ledger updated):
call `noop` with message: "Processed N papers (M relevant), no actionable gh-aw improvements identified today."

## Completion Requirement

You MUST call exactly one safe-output tool before finishing:
- `create_discussion` when actionable opportunities are found.
- `noop` when no actionable opportunities are found, when `new_count` is 0, or when analysis cannot produce actionable output.

Do not end with a plain-text summary only. The final action in the run must be one safe-output call.

---

## agent: `preliminary-investigator`
---
description: Extracts a concise evidence-based note from an arXiv paper PDF
model: small
---

Investigate an arXiv paper beyond its abstract before relevance screening.

Input: a full paper JSON object containing `id`, `title`, and `url`.

1. Download `https://arxiv.org/pdf/<id>` to `/tmp/gh-aw/agent/arxiv/<id>.pdf` with `curl`. If the download fails, return `{"pdf_fetched": false, "preliminary_note": ""}`.
2. Use the `kreuzberg` MCP server's `extract_bytes` tool with the PDF's base64 content to extract its text. If extraction fails or produces no text, return `{"pdf_fetched": false, "preliminary_note": ""}`.
3. Read only enough extracted text to identify techniques, mechanisms, evaluation results, or implementation details that could apply to gh-aw but are absent from the abstract.

Output exactly one line of valid JSON — no other text:
`{"pdf_fetched": true, "preliminary_note": "one or two concise sentences"}`

## agent: `paper-screener`
---
description: Fast relevance screening of arXiv paper abstracts for GitHub Agentic Workflows
model: small
---

Screen an arXiv paper abstract for relevance to GitHub Agentic Workflows (gh-aw).

gh-aw compiles markdown workflow files into GitHub Actions YAML, uses AI agents as workflow engines, manages tool access, enforces network firewalls, routes writes through typed safe-outputs, optimizes tokens, and supports multi-agent orchestration.

**Relevant** (any of):
- Agentic AI systems, multi-agent coordination, LLM agent workflows
- Prompt engineering, context management, or caching for LLM agents
- Token efficiency, structured output generation from LLMs
- AI-driven CI/CD, automated code review, workflow automation
- Security in agentic systems: sandboxing, tool access control, policy enforcement
- Orchestration patterns for decomposing tasks across AI agents

**Not relevant**:
- Pure mathematical theory with no LLM/agent application
- Hardware, systems, or networking research unrelated to AI
- Computer vision, speech, or domain-specific scientific tasks
- Medical, biological, or physical science applications

Input: `{"title": "...", "abstract": "...", "preliminary_note": "..."}` as a JSON string. Consider the preliminary note when present; do not reject a paper solely because the abstract is weak when the note identifies a relevant technique.

Output: exactly one line of valid JSON — no other text:
`{"relevant": true, "reason": "one sentence"}` or `{"relevant": false, "reason": "one sentence"}`

## agent: `relevance-ranker`
---
description: Scores a relevant arXiv paper by actionability for GitHub Agentic Workflows
model: small
---

Score a relevant arXiv paper by how actionable it is for GitHub Agentic Workflows (gh-aw).

gh-aw compiles markdown workflows into GitHub Actions YAML with pluggable AI engines, safe-outputs typed writes, network firewall, token optimization, sub-agents, cache-memory, repo-memory, and multi-agent orchestration.

Score 1–5:
- 5: describes a new technique, pattern, or mechanism directly applicable to a specific gh-aw component
- 4: strong connection to gh-aw but requires adaptation
- 3: loosely related; possible indirect improvement
- 2: tangential; only marginally relevant to gh-aw
- 1: relevant to AI/agents generally but no clear gh-aw application

Input: `{"title": "...", "abstract": "...", "preliminary_note": "..."}` as a JSON string. Consider the preliminary note when present.

Output: exactly one line of valid JSON — no other text:
`{"score": <1-5>, "reason": "one sentence"}`

## agent: `opportunity-extractor`
---
description: Extracts a specific actionable improvement for gh-aw from a relevant arXiv paper
model: large
---

Extract one specific actionable improvement for GitHub Agentic Workflows (gh-aw) from an arXiv paper.

gh-aw components (Go compiler + TypeScript runtime):
- **Workflow compiler**: parses markdown frontmatter + body into GitHub Actions YAML
- **Safe-outputs**: typed write operations (create-issue, create-discussion, add-comment, create-pull-request, upload-artifact)
- **Network firewall (AWF)**: blocks outbound domains not in `network.allowed`
- **Token optimization**: DataOps pre-steps, gh-proxy, cache-memory, sub-agent fan-out, prompt caching
- **Sub-agents**: inline agents invoked by the orchestrator, with model aliases (small/large)
- **Memory**: cache-memory (ephemeral, up to 90 days), repo-memory (Git branch, indefinite), comment-memory (issue/PR comment)
- **Engines**: Claude, Copilot, Gemini, Codex — pluggable via `engine:` field
- **Imports**: shared workflow components, skill files, MCP server configs

Input: full paper JSON object.

Identify the single most actionable improvement the paper suggests for gh-aw — a new feature, optimization, design pattern, or new workflow type.

Output: exactly one line of valid JSON — no other text:
`{"opportunity": "concise one-sentence action", "area": "token-optimization|safe-outputs|workflow-compilation|multi-agent|prompt-engineering|network|security|other", "effort": "low|medium|high", "rationale": "2-3 sentences naming the paper mechanism and the specific gh-aw component it improves"}`

## skill: `discussion-template`
---
description: Discussion body structure and formatting rules for the arXiv research output
---

Use the `reporting` skill's header and progressive-disclosure conventions. Use `###` or lower
for all headers inside the discussion body — never `#` or `##`. Keep the summary and actionable
opportunities visible; wrap the full papers-analyzed table (and any other long/verbose list) in a
`<details><summary><b>...</b></summary>` block.

Discussion body structure:

```
### Summary

N papers screened, M relevant, K opportunities identified.

---

### Actionable Opportunities

(one section per opportunity, grouped by area when there are multiple in the same area)

#### [AREA] — Short Opportunity Title

**Paper**: [Title](URL)
**Authors**: Author A, Author B
**Published**: YYYY-MM-DD
**Effort**: low / medium / high
**Rationale**: 2-3 sentences mapping the paper's mechanism to a specific gh-aw component.

---

<details>
<summary><b>Papers Analyzed</b></summary>

| Paper | Published | Relevant | Area |
|---|---|---|---|
| [Title](URL) | YYYY-MM-DD | Yes / No | area or — |

</details>

---

### Next Steps

- [ ] Investigate: opportunity 1 (effort: low)
- [ ] Investigate: opportunity 2 (effort: medium)

<details>
<summary><b>Quick-Win Agentic Prompts</b></summary>

Paste one of these as a new issue or comment to kick off implementation with `@copilot`
(one prompt per opportunity, in the same order as above):

> @copilot Implement: <opportunity text> in gh-aw's <area> component. Rationale: <rationale>. Source: [Title](URL).

</details>
```

For each opportunity (max 3), render one `@copilot` prompt blockquote inside the `<details>` block
by substituting `<opportunity text>`, `<area>`, `<rationale>`, `Title`, and `URL` with that
opportunity's own values — do not paraphrase or shorten them. Omit the `<details>` block entirely
when there are no opportunities.