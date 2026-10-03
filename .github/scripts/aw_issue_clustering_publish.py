#!/usr/bin/env python3
"""Validate/reconcile the essential ten inside a write-isolated safe-output job."""

import argparse
from copy import deepcopy
import html
import json
import math
import os
from pathlib import Path
import re

from aw_issue_clustering import (
    CLUSTER_LABEL, END, OWNER_MARKER, PREFIX, START, build_corpus, collect_discussions,
    collect_issues, gh_json, graphql, managed, metadata, run_cutoff,
)


def require(condition, message):
    if not condition:
        raise ValueError(message)


def text(value, field, limit=4000):
    require(isinstance(value, str) and 10 <= len(value) <= limit, f"Invalid {field}")
    require(
        "<!--" not in value and not re.search(r"[\x00-\x08\x0b\x0c\x0e-\x1f]", value),
        f"Control characters or metadata injection in {field}",
    )


def score(cluster):
    return cluster["impact"] * cluster["confidence"] * math.log2(1 + len(cluster["members"])) / cluster["effort"]


def refresh_closed_sources(plan, corpus):
    """Drop only verified sources closed since this run began, not unverified IDs."""
    require(isinstance(plan, dict) and isinstance(plan.get("clusters"), list), "Invalid clustering plan")
    require(isinstance(plan.get("deferred"), list), "Missing deferred coverage")
    result = deepcopy(plan)
    closed = set(corpus.get("recently_closed", []))
    assigned = {metadata(item)["key"] for item in corpus["managed"] if item.get("assignees")}
    retained, dropped = [], []
    removed = 0
    for cluster in result["clusters"]:
        require(isinstance(cluster, dict) and isinstance(cluster.get("members"), list), "Invalid cluster members")
        if cluster.get("key") in assigned:
            retained.append(cluster)
            continue
        require(all(type(number) is int for number in cluster["members"]), "Invalid source numbers")
        members = [number for number in cluster["members"] if number not in closed]
        removed += len(cluster["members"]) - len(members)
        if cluster["members"] and not members:
            dropped.append(cluster["key"])
        else:
            cluster["members"] = members
            retained.append(cluster)
    result["clusters"] = retained
    require(all(isinstance(item, dict) and type(item.get("number")) is int for item in result["deferred"]),
            "Invalid deferred entries")
    result["deferred"] = [item for item in result["deferred"] if item["number"] not in closed]
    if dropped:
        result["shortfall_reason"] = (
            f"{len(dropped)} planned assignments lost all open sources during analysis. "
            "Their source closures are not proof of a fix; the next daily pass reassesses the remaining backlog."
        )
    if removed:
        print(f"Reconciliation: removed {removed} verified AW sources closed after run start; "
              f"retired {len(dropped)} empty candidate assignments")
    return result


def validate_plan(plan, corpus):
    require(isinstance(plan, dict), "Plan must be an object")
    clusters = plan.get("clusters")
    deferred = plan.get("deferred")
    require(isinstance(clusters, list) and len(clusters) <= 10, "At most ten clusters allowed")
    require(isinstance(deferred, list), "Missing deferred coverage")
    eligible = {item["number"] for item in corpus["issues"]}
    reports = {item["number"] for item in corpus["reports"]}
    existing = {metadata(item)["key"]: item for item in corpus["managed"]}
    require(len(existing) == len(corpus["managed"]), "Duplicate managed keys; reconcile manually")
    require(len(existing) <= 10, "More than ten owned summaries open; reconcile manually")
    seen, keys = set(), set()
    for cluster in clusters:
        require(isinstance(cluster, dict), "Cluster must be an object")
        key = cluster.get("key")
        require(isinstance(key, str) and re.fullmatch(r"[a-z][a-z0-9-]{2,63}", key), "Invalid cluster key")
        require(key not in keys, "Duplicate cluster key")
        keys.add(key)
        for field in ("title", "summary", "fix", "rationale"):
            text(cluster.get(field), field, 120 if field == "title" else 4000)
        criteria = cluster.get("acceptance")
        require(isinstance(criteria, list) and 1 <= len(criteria) <= 6, "Missing acceptance criteria")
        for criterion in criteria:
            text(criterion, "acceptance criterion", 1000)
        for field in ("impact", "confidence", "effort"):
            require(type(cluster.get(field)) is int and 1 <= cluster[field] <= 5, f"Invalid {field}")
        members = cluster.get("members")
        require(
            isinstance(members, list) and members
            and all(type(number) is int for number in members)
            and len(set(members)) == len(members),
            "Members must be unique issue numbers",
        )
        previous = existing.get(key)
        if previous and previous.get("assignees"):
            require(cluster == metadata(previous), f"Assigned cluster {key} must remain unchanged")
            live_members = set(members) & eligible
        else:
            require(set(members) <= eligible, "Cluster includes human, closed, or unverified issues")
            live_members = set(members)
            if previous:
                require(
                    set(metadata(previous)["members"]) & live_members,
                    f"Cannot repurpose cluster identity {key}",
                )
        require(not seen & live_members, "Issue appears in multiple clusters")
        seen.update(live_members)
        cited = cluster.get("reports")
        require(
            isinstance(cited, list) and all(type(number) is int for number in cited)
            and len(set(cited)) == len(cited),
            "Reports must be unique discussion numbers",
        )
        if not (previous and previous.get("assignees")):
            require(set(cited) <= reports, "Report lacks AW provenance or is outside the evidence window")
    for item in deferred:
        require(isinstance(item, dict), "Deferred entry must be an object")
        number = item.get("number")
        require(type(number) is int and number in eligible and number not in seen, "Invalid deferred issue")
        text(item.get("reason"), "deferral reason", 1000)
        seen.add(number)
    require(seen == eligible, "Every eligible AW issue must be clustered or explicitly deferred")
    require(
        all(metadata(item)["key"] in keys for item in corpus["managed"] if item.get("assignees")),
        "Assigned clusters cannot be retired",
    )
    if len(clusters) < min(10, len(eligible)):
        text(plan.get("shortfall_reason"), "shortfall reason")
    return sorted(clusters, key=lambda cluster: (-score(cluster), cluster["key"]))


def safe_text(value):
    # Keep prose, not user-controlled Markdown links, mentions, or bot commands.
    value = re.sub(r"https?://\S+", "[URL omitted; use evidence links below]", value)
    value = value.replace("@", "@\u200b")
    value = re.sub(r"(?<!\w)/(?=[a-zA-Z])", "/\u200b", value)
    return html.escape(value)


def island(cluster, rank, repo):
    links = " ".join(f"[#{number}](https://github.com/{repo}/issues/{number})" for number in cluster["members"])
    reports = " ".join(
        f"[discussion #{number}](https://github.com/{repo}/discussions/{number})"
        for number in cluster["reports"]
    ) or "No corroborating AW discussion; evidence comes from the source issues."
    criteria = "\n".join(f"- [ ] {safe_text(value)}" for value in cluster["acceptance"])
    return (
        f"{START}\n"
        f"**Priority {rank}/10** | **{len(cluster['members'])} source issues** | "
        f"Impact {cluster['impact']}/5 | Confidence {cluster['confidence']}/5 | Effort {cluster['effort']}/5\n\n"
        f"### One assignment, one coherent fix\n{safe_text(cluster['summary'])}\n\n"
        f"### Implementation scope\n{safe_text(cluster['fix'])}\n\n"
        f"### Done when\n{criteria}\n\n"
        f"### Why now\n{safe_text(cluster['rationale'])}\n\n"
        f"<details><summary>AW source issues and corroborating reports</summary>\n\n"
        f"{links}\n\n{reports}\n\n</details>\n\n"
        "Source issues remain untouched. Closing this summary does not close its sources. "
        "Assigned summaries are frozen; unassign to allow reclustering.\n\n"
        f"<!-- aw-essential-meta: {json.dumps(cluster, ensure_ascii=True, sort_keys=True)} -->\n"
        f"{END}"
    )


def replace_island(body, replacement):
    require(body.count(START) == 1 and body.count(END) == 1, "Malformed managed island")
    before, rest = body.split(START, 1)
    _, after = rest.split(END, 1)
    return before + replacement + after


def reconcile(clusters, corpus, staged=False):
    repo = corpus["repo"]
    existing = {metadata(item)["key"]: item for item in corpus["managed"]}
    wanted = {cluster["key"] for cluster in clusters}
    retiring = [item for key, item in existing.items() if key not in wanted]
    require(len(corpus["managed"]) <= 10, "More than ten managed issues already open")
    require(all(not item.get("assignees") for item in retiring), "Cannot close assigned work")
    result = []

    # Close first so partial failures/retries can never grow the active queue above ten.
    for item in retiring:
        if not staged:
            live = gh_json(f"repos/{repo}/issues/{item['number']}")
            require(managed(live) and not live.get("assignees"), "Ownership or assignment changed; rerun")
            require(live["updated_at"] == item["updated_at"], "Managed issue changed during reconciliation")
            gh_json(f"repos/{repo}/issues/{item['number']}", "--method", "PATCH", payload={
                "state": "closed", "state_reason": "not_planned",
            })
        print(f"{'Preview: ' if staged else ''}retire summary #{item['number']} (sources untouched)")

    for rank, cluster in enumerate(clusters, 1):
        item = existing.get(cluster["key"])
        title = PREFIX + f"{rank:02d} " + cluster["title"]
        content = island(cluster, rank, repo)
        if item:
            if not staged:
                live = gh_json(f"repos/{repo}/issues/{item['number']}")
                require(managed(live), "Summary ownership changed; refusing mutation")
                require(live["updated_at"] == item["updated_at"], "Summary edited or assigned; rerun")
                if CLUSTER_LABEL not in {label["name"] for label in live.get("labels", [])}:
                    gh_json(f"repos/{repo}/issues/{item['number']}/labels", "--method", "POST",
                            payload={"labels": [CLUSTER_LABEL]})
                    # Adding our label changes updated_at; refresh the concurrency guard.
                    item = gh_json(f"repos/{repo}/issues/{item['number']}")
                    require(managed(item), "Summary ownership changed; refusing mutation")
            if item.get("assignees"):
                result.append((rank, cluster, item["html_url"]))
                continue
            body = replace_island(item["body"], content)
            if not staged and (body != item["body"] or title != item["title"]):
                live = gh_json(f"repos/{repo}/issues/{item['number']}")
                require(managed(live) and not live.get("assignees"), "Ownership or assignment changed; rerun")
                require(live["updated_at"] == item["updated_at"], "Managed issue edited; rerun to preserve changes")
                gh_json(f"repos/{repo}/issues/{item['number']}", "--method", "PATCH",
                        payload={"title": title, "body": body})
            url = item["html_url"]
        elif staged:
            url = None
        else:
            live_count = sum(managed(issue) and issue.get("state") == "open" for issue in collect_issues(repo))
            require(live_count < 10, "Active queue is full; refusing an eleventh issue")
            created = gh_json(f"repos/{repo}/issues", "--method", "POST", payload={
                "title": title,
                "body": content + f"\n\n{OWNER_MARKER}\n",
                "labels": [CLUSTER_LABEL, "automation", "agentic-workflows", "cookie"],
            })
            url = created["html_url"]
        result.append((rank, cluster, url))
        print(f"{'Preview: ' if staged else ''}{title}: {url}")
    return result


def ensure_cluster_label(repo, staged=False):
    pages = gh_json(f"repos/{repo}/labels?per_page=100", "--paginate", "--slurp")
    if not any(label["name"] == CLUSTER_LABEL for page in pages for label in page):
        if staged:
            print(f"Preview: create label {CLUSTER_LABEL}")
        else:
            gh_json(f"repos/{repo}/labels", "--method", "POST", payload={
                "name": CLUSTER_LABEL, "color": "1D76DB",
                "description": "Essential AW-generated issue clusters: assign one to resolve related findings",
            })


def dashboard_body(result, plan, corpus):
    rows = [
        START,
        "### Essential AW fixes",
        "Assign one summary issue to address its linked source findings. "
        "Only provenance-verified AW-created issues are considered; human issues are excluded.",
        f"Saved issue view: `is:issue is:open label:{CLUSTER_LABEL}`.",
        "",
        "| Rank | Assignable fix | Sources | Impact / confidence / effort |",
        "| --- | --- | ---: | --- |",
    ]
    for rank, cluster, url in result:
        title = safe_text(cluster["title"]).replace("|", "&#124;").replace("\n", " ")
        link = f"[{title}]({url})" if url else f"{title} (new in live mode)"
        rows.append(
            f"| {rank} | {link} | {len(cluster['members'])} | "
            f"{cluster['impact']} / {cluster['confidence']} / {cluster['effort']} |"
        )
    rows += [
        "", f"Coverage: {len(corpus['issues'])} AW issues, {len(corpus['reports'])} AW discussions "
        f"(including deep reports), {len(corpus['excluded'])} excluded issues, "
        f"{len(plan['deferred'])} explicitly deferred findings.",
        "", "Ranking = impact x confidence x log2(1 + source count) / effort. "
        "Corroboration is not independent proof; repeated reports do not increase source count.",
        "", "<details><summary>Deferred AW findings</summary>", "",
    ]
    rows += [
        f"- [#{item['number']}](https://github.com/{corpus['repo']}/issues/{item['number']}): "
        f"{safe_text(item['reason'])}" for item in plan["deferred"]
    ]
    if plan.get("shortfall_reason"):
        rows += ["", safe_text(plan["shortfall_reason"])]
    run_id = os.environ.get("GITHUB_RUN_ID")
    if run_id:
        rows += ["", f"[Workflow run](https://github.com/{corpus['repo']}/actions/runs/{run_id})"]
    rows += ["", "</details>", "", END, "", OWNER_MARKER]
    return "\n".join(rows)


def publish_dashboard(repo, body, staged=False):
    require(len(body) <= 60000, "Dashboard exceeds GitHub body limit")
    if staged:
        print("Preview: update the AW Essential 10 discussion")
        return
    owner, name = repo.split("/")
    data = graphql("""
      query($query:String!) {
        search(query:$query, type:DISCUSSION, first:100) {
          pageInfo { hasNextPage }
          nodes { ... on Discussion { id title body closed author { __typename } } }
        }
      }
    """, query=f'repo:{repo} in:title "AW Essential 10"')
    require(not data["search"]["pageInfo"]["hasNextPage"], "Dashboard search truncated")
    dashboards = [
        item for item in data["search"]["nodes"]
        if item and item["title"] == "AW Essential 10"
        and item["author"] and item["author"]["__typename"] == "Bot"
        and OWNER_MARKER in item["body"]
    ]
    require(len(dashboards) <= 1, "Multiple owned dashboards; reconcile manually")
    if dashboards:
        require(not dashboards[0]["closed"], "Operator closed dashboard; refusing to recreate it")
        replacement = body.split(START, 1)[1].split(END, 1)[0]
        updated = replace_island(dashboards[0]["body"], START + replacement + END)
        if dashboards[0]["body"] != updated:
            graphql("""
              mutation($id:ID!, $body:String!) {
                updateDiscussion(input:{discussionId:$id, body:$body}) { discussion { id } }
              }
            """, id=dashboards[0]["id"], body=updated)
    else:
        data = graphql("""
          query($owner:String!, $name:String!) {
            repository(owner:$owner, name:$name) {
              id discussionCategories(first:100) { nodes { id name } pageInfo { hasNextPage } }
            }
          }
        """, owner=owner, name=name)["repository"]
        require(not data["discussionCategories"]["pageInfo"]["hasNextPage"], "Category discovery truncated")
        category = next((c for c in data["discussionCategories"]["nodes"] if c["name"].lower() == "audits"), None)
        require(category is not None, "Repository needs an audits discussion category")
        graphql("""
          mutation($repo:ID!, $category:ID!, $body:String!) {
            createDiscussion(input:{repositoryId:$repo, categoryId:$category,
              title:"AW Essential 10", body:$body}) { discussion { id } }
          }
        """, repo=data["id"], category=category["id"], body=body)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo", required=True)
    parser.add_argument("--plan", type=Path)
    parser.add_argument("--corpus", type=Path)
    parser.add_argument("--agent-output", type=Path)
    args = parser.parse_args()
    if args.agent_output:
        output = json.loads(args.agent_output.read_text())
        items = [item for item in output.get("items", []) if item.get("type") == "publish_essential_issues"]
        require(len(items) == 1, "Exactly one publish_essential_issues plan required")
        plan = json.loads(items[0]["plan"])
        # Never trust an agent-editable snapshot at the write boundary.
        cutoff = run_cutoff(args.repo)
        issues = collect_issues(args.repo, cutoff)
        sources = build_corpus(args.repo, issues, [], cutoff)["issues"]
        corpus = build_corpus(args.repo, issues, collect_discussions(args.repo, sources), cutoff)
        plan = refresh_closed_sources(plan, corpus)
    else:
        require(args.plan is not None and args.corpus is not None, "--plan and --corpus required")
        plan = json.loads(args.plan.read_text())
        corpus = json.loads(args.corpus.read_text())
    require(corpus["repo"] == args.repo, "Corpus repository mismatch")
    clusters = validate_plan(plan, corpus)
    if not args.agent_output:
        print(f"Validated {len(clusters)} clusters; complete coverage of {len(corpus['issues'])} AW issues")
        return
    staged = os.environ.get("GH_AW_SAFE_OUTPUTS_STAGED") == "true"
    # Validate all rendered sizes BEFORE any mutation.
    require(all(len(island(c, i, args.repo)) < 60000 for i, c in enumerate(clusters, 1)), "Issue body too large")
    preview = [(i, c, f"https://github.com/{args.repo}/issues/999999999") for i, c in enumerate(clusters, 1)]
    require(len(dashboard_body(preview, plan, corpus)) < 60000, "Dashboard body too large")
    ensure_cluster_label(args.repo, staged)
    result = reconcile(clusters, corpus, staged)
    body = dashboard_body(result, plan, corpus)
    publish_dashboard(args.repo, body, staged)
    if os.environ.get("GITHUB_STEP_SUMMARY"):
        with open(os.environ["GITHUB_STEP_SUMMARY"], "a") as summary:
            summary.write(body + "\n")


if __name__ == "__main__":
    main()
