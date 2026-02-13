---
description: Investigates suspicious repository activity and maintains a single triage issue
on:
  schedule:
    - cron: "0 */6 * * *"
  workflow_dispatch:
permissions:
  contents: read
  pull-requests: read
  issues: read
  actions: read
tools:
  github:
    mode: local
    read-only: true
    toolsets: [default]
if: needs.precompute.outputs.action != 'none'
jobs:
  precompute:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      pull-requests: read
      issues: read
      actions: read
    outputs:
      action: ${{ steps.precompute.outputs.action }}
      issue_number: ${{ steps.precompute.outputs.issue_number }}
      issue_title: ${{ steps.precompute.outputs.issue_title }}
      issue_body: ${{ steps.precompute.outputs.issue_body }}
    steps:
      - name: Precompute deterministic findings
        id: precompute
        uses: actions/github-script@v7
        with:
          github-token: ${{ secrets.GH_AW_BOT_DETECTION_TOKEN || secrets.GITHUB_TOKEN }}
          script: |
            const { owner, repo } = context.repo;
            const HOURS_BACK = 6;
            const ISSUE_TITLE = "🚨 Bot Detection: Suspicious Activity";
            const MIN_ACCOUNT_AGE_DAYS = 14;
            const MAX_PR = 50;
            const MAX_COMMENT_EXAMPLES = 10;
            const ALLOWED_DOMAINS = new Set([
              // GitHub docs + blog
              "docs.github.com",
              "github.blog",
              // Marketplace + package registries
              "marketplace.visualstudio.com",
              "npmjs.com",
              "pkg.go.dev",
              // Language vendor sites
              "golang.org",
              "go.dev",
              "nodejs.org",
            ]);
            const ALLOWED_ACCOUNTS = new Set([
              // Bots and service accounts
              "github-actions[bot]",
              "dependabot[bot]",
              "renovate[bot]",
              "copilot",
              "copilot-swe-agent",
            ]);
            const TRUSTED_ORGS = [
              // Orgs whose members should be allowlisted
              "github",
            ];
            const MEMBER_ACCOUNTS = new Set();

            function parseJsonList(envName) {
              try {
                const raw = process.env[envName];
                if (!raw) return [];
                const parsed = JSON.parse(raw);
                return Array.isArray(parsed) ? parsed : [];
              } catch {
                return [];
              }
            }

            function toISO(d) {
              return new Date(d).toISOString();
            }

            function normalizeForDup(s) {
              return (s || "")
                .toString()
                .replace(/https?:\/\/\S+/g, "")
                .toLowerCase()
                .replace(/\s+/g, " ")
                .trim()
                .slice(0, 240);
            }

            function extractDomains(text) {
              const domains = [];
              const urlRe = /https?:\/\/[^\s)\]]+/g;
              const matches = text.match(urlRe) || [];
              for (const raw of matches) {
                try {
                  const u = new URL(raw);
                  domains.push(u.hostname.toLowerCase());
                } catch {
                  // ignore parse failures
                }
              }
              return domains;
            }

            function isExternalDomain(host) {
              const allowed = new Set([
                "github.com",
                "raw.githubusercontent.com",
                "avatars.githubusercontent.com",
                "api.github.com",
              ]);
              return host && !allowed.has(host) && !ALLOWED_DOMAINS.has(host);
            }

            function isAllowedAccount(login) {
              const normalized = String(login || "").toLowerCase();
              return ALLOWED_ACCOUNTS.has(normalized) || MEMBER_ACCOUNTS.has(normalized);
            }

            async function loadMemberAccounts() {
              try {
                const collaborators = await github.paginate(github.rest.repos.listCollaborators, {
                  owner,
                  repo,
                  per_page: 100,
                });
                for (const collaborator of collaborators) {
                  if (collaborator?.login) {
                    MEMBER_ACCOUNTS.add(String(collaborator.login).toLowerCase());
                  }
                }
              } catch {
                // If collaborator lookup fails, continue without member allowlist.
              }
            }

            async function loadOrgMembers() {
              for (const org of TRUSTED_ORGS) {
                try {
                  const members = await github.paginate(github.rest.orgs.listMembers, {
                    org,
                    per_page: 100,
                  });
                  for (const member of members) {
                    if (member?.login) {
                      MEMBER_ACCOUNTS.add(String(member.login).toLowerCase());
                    }
                  }
                } catch {
                  // If org member lookup fails, continue without org allowlist.
                }
              }
            }

            function isShortener(host) {
              const shorteners = new Set(["bit.ly", "tinyurl.com", "t.co", "is.gd", "goo.gl"]);
              return shorteners.has(host);
            }

            function isNonGitHubBinaryUrl(urlStr) {
              try {
                const u = new URL(urlStr);
                const host = u.hostname.toLowerCase();
                if (!isExternalDomain(host)) return false;
                const path = u.pathname.toLowerCase();
                return (
                  path.endsWith(".exe") ||
                  path.endsWith(".msi") ||
                  path.endsWith(".pkg") ||
                  path.endsWith(".dmg") ||
                  path.endsWith(".zip") ||
                  path.endsWith(".tar.gz")
                );
              } catch {
                return false;
              }
            }

            async function getRunCreatedAt() {
              const runId = context.runId;
              const { data } = await github.rest.actions.getWorkflowRun({
                owner,
                repo,
                run_id: runId,
              });
              return new Date(data.created_at);
            }

            const end = await getRunCreatedAt();
            const start = new Date(end.getTime() - HOURS_BACK * 60 * 60 * 1000);

            for (const domain of parseJsonList("BOT_DETECTION_ALLOWED_DOMAINS")) {
              if (domain) ALLOWED_DOMAINS.add(String(domain).toLowerCase());
            }
            for (const account of parseJsonList("BOT_DETECTION_ALLOWED_ACCOUNTS")) {
              if (account) ALLOWED_ACCOUNTS.add(String(account).toLowerCase());
            }
            for (const org of parseJsonList("BOT_DETECTION_TRUSTED_ORGS")) {
              if (org) TRUSTED_ORGS.push(String(org));
            }

            await loadMemberAccounts();
            await loadOrgMembers();

            // Search issues + PRs updated in window
            const q = `repo:${owner}/${repo} updated:>=${toISO(start)}`;
            const search = await github.rest.search.issuesAndPullRequests({
              q,
              per_page: 100,
              sort: "updated",
              order: "desc",
            });

            const items = (search.data.items || [])
              .filter(i => new Date(i.updated_at) >= start && new Date(i.updated_at) <= end)
              .map(i => ({
                number: i.number,
                title: i.title || "",
                body: i.body || "",
                url: i.html_url,
                created_at: i.created_at,
                updated_at: i.updated_at,
                is_pr: Boolean(i.pull_request),
                author: i.user?.login || "",
              }));

            // Deterministic ordering for any downstream processing
            items.sort((a, b) => {
              const at = a.updated_at.localeCompare(b.updated_at);
              if (at !== 0) return at;
              const an = a.number - b.number;
              if (an !== 0) return an;
              return a.url.localeCompare(b.url);
            });

            // Collect per-author signals
            const perAuthor = new Map();
            const domainAccounts = new Map(); // domain -> Set(logins)
            const userCreatedAt = new Map();

            async function ensureUserCreatedAt(login) {
              if (!login || userCreatedAt.has(login)) return;
              try {
                const { data: userInfo } = await github.rest.users.getByUsername({ username: login });
                userCreatedAt.set(login, new Date(userInfo.created_at));
              } catch {
                userCreatedAt.set(login, null);
              }
            }

            function ensureAuthor(login) {
              if (!perAuthor.has(login)) {
                perAuthor.set(login, {
                  login,
                  itemCount: 0,
                  commentCount: 0,
                  reviewCount: 0,
                  accountAgeDays: null,
                  externalDomains: new Set(),
                  hasShortener: false,
                  hasNonGitHubBinary: false,
                  touchesWorkflows: false,
                  touchesCI: false,
                  touchesDeps: false,
                  dupTexts: new Map(),
                  examples: [],
                });
              }
              return perAuthor.get(login);
            }

            for (const it of items) {
              const login = it.author;
              if (!login) continue;
              if (isAllowedAccount(login)) continue;
              const s = ensureAuthor(login);
              await ensureUserCreatedAt(login);
              s.itemCount += 1;
              if (s.examples.length < 5) {
                s.examples.push({ url: it.url, is_pr: it.is_pr, number: it.number });
              }

              const text = `${it.title}\n\n${it.body}`;
              const domains = extractDomains(text);
              for (const host of domains) {
                if (!host) continue;
                if (isExternalDomain(host)) {
                  s.externalDomains.add(host);
                  if (!domainAccounts.has(host)) domainAccounts.set(host, new Set());
                  domainAccounts.get(host).add(login);
                }
                if (isShortener(host)) s.hasShortener = true;
              }

              // Non-GitHub binary/download links
              const urlRe = /https?:\/\/[^\s)\]]+/g;
              const urlMatches = (text.match(urlRe) || []);
              for (const u of urlMatches) {
                if (isNonGitHubBinaryUrl(u)) {
                  s.hasNonGitHubBinary = true;
                }
              }

              // Duplicate-ish content detection (within items we fetched)
              const norm = normalizeForDup(text);
              if (norm) {
                s.dupTexts.set(norm, (s.dupTexts.get(norm) || 0) + 1);
              }
            }

            // PR comments + reviews (deterministic and bounded)
            const prItems = items.filter(i => i.is_pr).slice(0, MAX_PR);
            for (const it of prItems) {
              const login = it.author;
              if (login) {
                if (isAllowedAccount(login)) continue;
                await ensureUserCreatedAt(login);
              }

              let issueComments = [];
              try {
                let total = 0;
                issueComments = await github.paginate(
                  github.rest.issues.listComments,
                  {
                    owner,
                    repo,
                    issue_number: it.number,
                    per_page: 100,
                  },
                  (response, done) => {
                    const remaining = 500 - total;
                    if (remaining <= 0) {
                      done();
                      return [];
                    }
                    if (total + response.data.length >= 500) {
                      total = 500;
                      done();
                      return response.data.slice(0, remaining);
                    }
                    total += response.data.length;
                    return response.data;
                  }
                );
              } catch {
                // ignore
              }

              let reviewComments = [];
              try {
                let total = 0;
                reviewComments = await github.paginate(
                  github.rest.pulls.listReviewComments,
                  {
                    owner,
                    repo,
                    pull_number: it.number,
                    per_page: 100,
                  },
                  (response, done) => {
                    const remaining = 500 - total;
                    if (remaining <= 0) {
                      done();
                      return [];
                    }
                    if (total + response.data.length >= 500) {
                      total = 500;
                      done();
                      return response.data.slice(0, remaining);
                    }
                    total += response.data.length;
                    return response.data;
                  }
                );
              } catch {
                // ignore
              }

              let reviews = [];
              try {
                let total = 0;
                reviews = await github.paginate(
                  github.rest.pulls.listReviews,
                  {
                    owner,
                    repo,
                    pull_number: it.number,
                    per_page: 100,
                  },
                  (response, done) => {
                    const remaining = 500 - total;
                    if (remaining <= 0) {
                      done();
                      return [];
                    }
                    if (total + response.data.length >= 500) {
                      total = 500;
                      done();
                      return response.data.slice(0, remaining);
                    }
                    total += response.data.length;
                    return response.data;
                  }
                );
              } catch {
                // ignore
              }

              const commentCandidates = [...issueComments, ...reviewComments]
                .filter(c => c?.created_at)
                .filter(c => new Date(c.created_at) >= start && new Date(c.created_at) <= end)
                .sort((a, b) => a.created_at.localeCompare(b.created_at));

              for (const c of commentCandidates) {
                const commenter = c.user?.login || "";
                if (!commenter) continue;
                if (isAllowedAccount(commenter)) continue;
                await ensureUserCreatedAt(commenter);
                const s = ensureAuthor(commenter);
                s.commentCount += 1;
                if (s.examples.length < MAX_COMMENT_EXAMPLES) {
                  s.examples.push({ url: c.html_url, is_pr: true, number: it.number });
                }
              }

              const reviewCandidates = reviews
                .map(r => ({
                  user: r.user,
                  submitted_at: r.submitted_at || r.submittedAt,
                  url: r.html_url || `${it.url}#pullrequestreview-${r.id}`,
                }))
                .filter(r => r.submitted_at)
                .filter(r => new Date(r.submitted_at) >= start && new Date(r.submitted_at) <= end)
                .sort((a, b) => a.submitted_at.localeCompare(b.submitted_at));

              for (const r of reviewCandidates) {
                const reviewer = r.user?.login || "";
                if (!reviewer) continue;
                if (isAllowedAccount(reviewer)) continue;
                await ensureUserCreatedAt(reviewer);
                const s = ensureAuthor(reviewer);
                s.reviewCount += 1;
                if (s.examples.length < MAX_COMMENT_EXAMPLES) {
                  s.examples.push({ url: r.url, is_pr: true, number: it.number });
                }
              }
            }

            // PR file touches (sensitive paths) - deterministic and bounded
            for (const it of prItems) {
              const login = it.author;
              if (!login) continue;
              if (isAllowedAccount(login)) continue;
              const s = ensureAuthor(login);

              try {
                let total = 0;
                const files = await github.paginate(
                  github.rest.pulls.listFiles,
                  {
                    owner,
                    repo,
                    pull_number: it.number,
                    per_page: 100,
                  },
                  (response, done) => {
                    const remaining = 500 - total;
                    if (remaining <= 0) {
                      done();
                      return [];
                    }
                    if (total + response.data.length >= 500) {
                      total = 500;
                      done();
                      return response.data.slice(0, remaining);
                    }
                    total += response.data.length;
                    return response.data;
                  }
                );
                const filenames = files.map(f => f.filename);
                for (const fn of filenames) {
                  if (fn.startsWith(".github/workflows/") || fn.startsWith(".github/actions/")) s.touchesWorkflows = true;
                  if (fn === "Dockerfile" || fn === "Makefile" || fn.startsWith("scripts/") || fn.startsWith("actions/")) s.touchesCI = true;
                  if (
                    fn === "package.json" ||
                    fn === "package-lock.json" ||
                    fn === "pnpm-lock.yaml" ||
                    fn === "yarn.lock" ||
                    fn === "go.mod" ||
                    fn === "go.sum" ||
                    fn.startsWith("requirements")
                  ) {
                    s.touchesDeps = true;
                  }
                }
              } catch (e) {
                // If file listing fails, do not infer.
              }
            }

            // Score + severity
            const accounts = Array.from(perAuthor.values()).map(s => {
              if (userCreatedAt.has(s.login)) {
                const createdAt = userCreatedAt.get(s.login);
                if (createdAt) {
                  const now = new Date(end);
                  s.accountAgeDays = Math.max(0, Math.floor((now - createdAt) / (24 * 60 * 60 * 1000)));
                }
              }
              let score = 0;

              const extDomains = Array.from(s.externalDomains);
              score += Math.min(9, extDomains.length * 3);
              if (s.hasShortener) score += 8;
              if (s.hasNonGitHubBinary) score += 10;
              if (s.touchesWorkflows) score += 15;
              if (s.touchesCI) score += 10;
              if (s.touchesDeps) score += 6;
              if (s.itemCount >= 5) score += 6;
              if (s.accountAgeDays !== null && s.accountAgeDays < MIN_ACCOUNT_AGE_DAYS) score += 8;

              let hasDup3 = false;
              for (const [, c] of s.dupTexts) {
                if (c >= 3) {
                  hasDup3 = true;
                  break;
                }
              }
              if (hasDup3) score += 8;

              score = Math.min(100, score);

              let severity = "None";
              if (score >= 20) severity = "High";
              else if (score >= 10) severity = "Medium";
              else if (score >= 1) severity = "Low";

              // Deterministic signal summary
              const signals = [];
              if (extDomains.length > 0) signals.push(`external_domains=${extDomains.length}`);
              if (s.hasShortener) signals.push("shortener");
              if (s.hasNonGitHubBinary) signals.push("non_github_binary_link");
              if (s.touchesWorkflows) signals.push("touches_workflows");
              if (s.touchesCI) signals.push("touches_ci_or_scripts");
              if (s.touchesDeps) signals.push("touches_dependencies");
              if (s.itemCount >= 5) signals.push(`burst_items=${s.itemCount}`);
              if (hasDup3) signals.push("dup_text>=3");
              if (s.commentCount > 0) signals.push(`comments=${s.commentCount}`);
              if (s.reviewCount > 0) signals.push(`reviews=${s.reviewCount}`);
              if (s.accountAgeDays !== null && s.accountAgeDays < MIN_ACCOUNT_AGE_DAYS) {
                signals.push(`new_account=${s.accountAgeDays}d`);
              }

              return {
                login: s.login,
                risk_score: score,
                severity,
                signals,
                external_domains: extDomains.sort((a, b) => a.localeCompare(b)),
                examples: s.examples,
              };
            });

            // Stable sorting
            accounts.sort((a, b) => {
              if (b.risk_score !== a.risk_score) return b.risk_score - a.risk_score;
              return a.login.localeCompare(b.login);
            });

            const domains = Array.from(domainAccounts.entries())
              .map(([domain, logins]) => ({ domain, count: logins.size, accounts: Array.from(logins).sort((a, b) => a.localeCompare(b)) }))
              .sort((a, b) => {
                if (b.count !== a.count) return b.count - a.count;
                return a.domain.localeCompare(b.domain);
              });

            const topSeverity = accounts.find(a => a.severity !== "None")?.severity || "None";
            const hasFindings = accounts.some(a => a.risk_score >= 10) || domains.some(d => d.count >= 2);

            // Find existing triage issue (exact title match)
            let existingIssueNumber = "";
            try {
              const openIssues = await github.rest.issues.listForRepo({
                owner,
                repo,
                state: "open",
                per_page: 100,
              });
              const existing = (openIssues.data || []).find(i => (i.title || "") === ISSUE_TITLE);
              if (existing?.number) existingIssueNumber = String(existing.number);
            } catch (e) {
              // ignore
            }

            // Render deterministic markdown body
            function renderBody(includeMention) {
              const lines = [];
              if (includeMention) lines.push("@pelikhan", "");
              lines.push(
                `**Window:** ${toISO(start)} → ${toISO(end)}`,
                `**Assessment:** ${topSeverity}`,
                ""
              );

              if (!hasFindings) {
                lines.push("No meaningful suspicious activity detected in this window.");
                return lines.join("\n");
              }

              if (domains.length > 0) {
                lines.push("## Domains (external)", "", "| Domain | Accounts | Logins |", "| --- | ---: | --- |");
                for (const d of domains.slice(0, 20)) {
                  const maxLogins = 5;
                  const shown = d.accounts.slice(0, maxLogins).map(login => `@${login}`);
                  const overflow = d.accounts.length > maxLogins ? ` +${d.accounts.length - maxLogins} more` : "";
                  lines.push(`| ${d.domain} | ${d.count} | ${shown.join(", ")}${overflow} |`);
                }
                lines.push("");
              }

              const high = accounts.filter(a => a.severity === "High");
              const med = accounts.filter(a => a.severity === "Medium");
              const low = accounts.filter(a => a.severity === "Low");

              function renderAccounts(title, arr) {
                if (arr.length === 0) return;
                lines.push(`## ${title}`, "");
                for (const a of arr.slice(0, 25)) {
                  const sig = a.signals.join(", ");
                  lines.push(`- @${a.login} — score=${a.risk_score} — ${sig}`);
                  if (a.examples && a.examples.length > 0) {
                    lines.push("  <details><summary>Evidence</summary>", "");
                    for (const ex of a.examples.slice(0, 5)) {
                      lines.push(`  - ${ex.url}`);
                    }
                    if (a.examples.length > 5) {
                      lines.push(`  - ... and ${a.examples.length - 5} more`);
                    }
                    lines.push("", "  </details>");
                  }
                }
                lines.push("");
              }

              renderAccounts("Accounts (High)", high);
              renderAccounts("Accounts (Medium)", med);
              renderAccounts("Accounts (Low)", low);

              lines.push("## Notes", "", "- This report is computed deterministically from GitHub Search + PR file listings + PR comments/reviews within the window.");
              return lines.join("\n");
            }

            let action = "none";
            let issueBody = "";
            let issueNumber = "";

            if (hasFindings) {
              if (existingIssueNumber) {
                action = "update";
                issueNumber = existingIssueNumber;
                issueBody = renderBody(false);
              } else {
                action = "create";
                issueBody = renderBody(true);
              }
            }

            core.setOutput("action", action);
            core.setOutput("issue_number", issueNumber);
            core.setOutput("issue_title", ISSUE_TITLE);
            core.setOutput("issue_body", issueBody);
safe-outputs:
  create-issue:
    max: 1
    labels: [security, bot-detection]
  update-issue:
    max: 1
    target: "*"
    body:
  mentions:
    allowed: ["@pelikhan"]
  threat-detection: false
timeout-minutes: 10
strict: true
---

# Bot Detection

You are a repository security triage agent whose job is to detect and summarize **suspicious activity** in ${{ github.repository }} over the last **6 hours**.

## What to Investigate

Focus on signals that often indicate spam, bot activity, or supply-chain probing. Consider (at minimum):

- **Link / domain risk**: URL shorteners, brand-new domains, non-GitHub download links, repeated domains across accounts.
- **Behavior anomalies**: bursts of comments/issues, repeated low-content bumps, copy/paste text across multiple threads.
- **Repo-touch risk (PRs)**: changes touching `.github/workflows/*`, CI scripts, release/publish configs, dependency update patterns, `curl | bash`, or install scripts.
- **Coordination signals**: multiple accounts posting similar content, same domains, same targets/labels, tight time clustering.

Do not rely only on account age.

## Data Collection Requirements

Using the GitHub toolset, collect enough evidence to support each suspicion:

- Recently updated PRs and their files changed (if available).
- Recently created issues.
- Recent comments/reviews on those issues/PRs.
- Links and repeated text patterns.

## Output Policy

Maintain a **single** open triage issue with the exact title:

`🚨 Bot Detection: Suspicious Activity`

### When to Create vs Update

- If an **open** issue with that exact title does **not** exist and you found **meaningful suspicious activity**, **create** it.
  - The body MUST start with a single line `@pelikhan` (this is the only time you should mention anyone).
- If it **does** exist, **update the existing issue body**, but **do not include any @mentions**.
- If you found **no meaningful suspicious activity**, emit **no safe outputs**.

## Report Format (Issue Body)

Produce a concise, evidence-driven report:

- **Window**: last 6 hours (include timestamps)
- **Overall assessment**: High / Medium / Low
- **Top findings**: 3–10 bullets with links
- **Accounts of interest** (if any): grouped by High / Medium with short “why”
- **Recommended actions**: least-invasive first (label/close/lock/report), with rationale

Keep it tight; avoid speculation without evidence.

## Determinism Contract (MUST FOLLOW)

This workflow enforces determinism via a **precompute job**.

- Do **not** query GitHub tools for additional data.
- Use only these precomputed values:
  - `action`: `${{ needs.precompute.outputs.action }}`
  - `issue_number`: `${{ needs.precompute.outputs.issue_number }}`
  - `issue_title`: `${{ needs.precompute.outputs.issue_title }}`
  - `issue_body`: (verbatim) `${{ needs.precompute.outputs.issue_body }}`
- Emit exactly **one** safe output (or none) based on `action`:
  - `create`: create an issue with `title=issue_title` and `body=issue_body`.
  - `update`: update `issue_number` with `body=issue_body`.
  - `none`: emit no safe outputs.

- Do not modify the precomputed markdown body.

## Risk Scoring (rules-based)

`risk_score` is computed in the precompute job (cap 100) from deterministic signals:

- External link present in any issue/PR body: `+3` per unique non-GitHub domain (cap `+9`).
- URL shortener domain present (`bit.ly`, `tinyurl.com`, `t.co`, `is.gd`, `goo.gl`): `+8` (cap once).
- Non-GitHub binary/download link (e.g., `.exe`, `.msi`, `.pkg`, `.dmg`, `.zip`, `.tar.gz` on non-GitHub domain): `+10`.
- PR touches sensitive paths (any changed file matches):
  - `.github/workflows/` or `.github/actions/`: `+15`
  - `Dockerfile`, `Makefile`, `scripts/`, `actions/`: `+10`
  - dependency manifests/lockfiles (`package.json`, `package-lock.json`, `pnpm-lock.yaml`, `yarn.lock`, `go.mod`, `go.sum`, `requirements*.txt`): `+6`
- Burst behavior: `+6` if the account authored `>= 5` issues/PRs in the window.
- Near-duplicate content: `+8` if the account posted the same (or obviously templated) text `>= 3` times in the window.
- New account age: `+8` if the account is `< 14 days` old.

Map score → severity:
- `High`: `>= 20`
- `Medium`: `10–19`
- `Low`: `1–9`
- `None`: `0` (do not include)

If you cannot verify a signal from available data, score it as `0` and do not infer.
