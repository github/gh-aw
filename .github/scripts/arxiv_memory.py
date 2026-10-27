#!/usr/bin/env python3
"""Bounded, append-only paper history for daily-arxiv-researcher.

Legacy paper-ledger.md and paper-index.json remain read-only. New records use
paired numbered shards; rotation never copies history into the daily patch.
"""

import argparse
import json
from datetime import datetime, timezone
from pathlib import Path


MAX_FILE_BYTES = 102400
SHARD_BYTES = 81920
MAX_FILES = 100
MAX_PATCH_BYTES = 51200
LEDGER_HEADER = (
    "# arXiv Paper Ledger\n\n"
    "Papers investigated for GitHub Agentic Workflows improvement opportunities.\n"
)


def json_text(value):
    return json.dumps(value, ensure_ascii=False, indent=2) + "\n"


def load_json(file):
    return json.loads(file.read_text(encoding="utf-8"))


def indices(memory):
    return sorted(memory.glob("paper-index*.json"))


def known_ids(memory):
    return {paper["id"] for file in indices(memory) for paper in load_json(file)["papers"]}


def prepare(memory, data, cache):
    fetched = load_json(data / "papers.json")
    # Cache persistence and repo-memory pushes are independent. Only committed
    # indices prove processing survived a previous run's push boundary.
    seen = known_ids(memory)
    papers = [paper for paper in fetched["papers"] if paper["id"] not in seen]
    (data / "new-papers.json").write_text(json_text({
        "total_fetched": len(fetched["papers"]),
        "already_seen": len(fetched["papers"]) - len(papers),
        "new_count": len(papers),
        "fetched_at": fetched["fetched_at"],
        "papers": papers,
    }), encoding="utf-8")
    cache.mkdir(parents=True, exist_ok=True)
    (cache / "seen-paper-ids.json").write_text(json_text({"ids": sorted(seen)}), encoding="utf-8")


def ledger_entry(paper):
    def line(value):
        return " ".join(str(value).split())

    text = (
        f"\n### [{line(paper['title'])}]({line(paper['url'])})\n"
        f"- **ID**: {paper['id']}\n"
        f"- **Published**: {line(paper['published'])}\n"
        f"- **Categories**: {line(', '.join(paper['categories']))}\n"
        f"- **Relevant**: {'Yes' if paper['relevant'] else 'No'}\n"
    )
    for field in ("opportunity", "area"):
        if paper["relevant"] and paper.get(field):
            text += f"- **{field.title()}**: {line(paper[field])}\n"
    return text


def update(memory, data, cache):
    new_papers = load_json(data / "new-papers.json")["papers"]
    results = load_json(data / "screened-papers.json")["papers"]
    by_id = {result["id"]: result for result in results}
    expected = {paper["id"] for paper in new_papers}
    if len(expected) != len(new_papers) or len(by_id) != len(results) or set(by_id) != expected:
        raise ValueError("Screening results must contain each new paper ID exactly once")
    for result in results:
        if type(result.get("relevant")) is not bool:
            raise ValueError(f"Screening result for {result['id']} needs a boolean relevant field")
        for field in ("opportunity", "area"):
            if field in result and not isinstance(result[field], str):
                raise ValueError(f"Screening result {field} must be a string")

    seen = known_ids(memory)
    files = {file.name: file.read_text(encoding="utf-8")
             for file in memory.glob("*") if file.is_file() and file.suffix in (".json", ".md")}
    if any(len(text.encode("utf-8")) > MAX_FILE_BYTES for text in files.values()):
        raise ValueError("Existing memory exceeds 100 KiB; repair it before adding papers")
    planned = dict(files)
    shard_numbers = sorted(int(file.stem.removeprefix("paper-index-"))
                           for file in indices(memory) if file.name != "paper-index.json")
    number = shard_numbers[-1] if shard_numbers else 1
    timestamp = datetime.now(timezone.utc).strftime("%Y-%m-%d-%H-%M-%S")
    for paper in new_papers:
        if paper["id"] in seen:
            continue
        record = {key: paper[key] for key in ("id", "title", "published", "url", "categories")}
        record["relevant"] = by_id[paper["id"]]["relevant"]
        record["analyzed_at"] = timestamp
        for field in ("opportunity", "area"):
            if field in by_id[paper["id"]]:
                record[field] = by_id[paper["id"]][field]
        entry = ledger_entry(record)
        while True:
            index_name = f"paper-index-{number:04d}.json"
            ledger_name = f"paper-ledger-{number:04d}.md"
            if (index_name in planned) != (ledger_name in planned):
                raise ValueError(f"Incomplete memory shard {number}")
            history = json.loads(planned.get(index_name, '{"papers": []}'))["papers"]
            ledger = planned.get(ledger_name, LEDGER_HEADER)
            index_text = json_text({"papers": history + [record]})
            ledger_text = ledger + entry
            if max(len(index_text.encode("utf-8")), len(ledger_text.encode("utf-8"))) <= SHARD_BYTES:
                planned[index_name] = index_text
                planned[ledger_name] = ledger_text
                break
            if not history:
                raise ValueError(f"Paper {paper['id']} exceeds the 80 KiB shard budget")
            number += 1
        seen.add(paper["id"])

    if len(planned) > MAX_FILES:
        raise ValueError("Paper memory reached the 100-file budget; archive history before continuing")
    growth = sum(max(0, len(text.encode("utf-8")) - len(files.get(name, "").encode("utf-8")))
                 for name, text in planned.items())
    if growth > MAX_PATCH_BYTES:
        raise ValueError("Paper update exceeds the 50 KiB patch budget; process a smaller batch")
    # Validate the entire candidate before touching memory or advancing dedup.
    memory.mkdir(parents=True, exist_ok=True)
    for name, text in planned.items():
        if files.get(name) != text:
            (memory / name).write_text(text, encoding="utf-8")
    cache.mkdir(parents=True, exist_ok=True)
    (cache / "seen-paper-ids.json").write_text(json_text({"ids": sorted(seen)}), encoding="utf-8")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("prepare", "update"))
    parser.add_argument("--memory", type=Path, default=Path("/tmp/gh-aw/repo-memory/default"))
    parser.add_argument("--data", type=Path, default=Path("/tmp/gh-aw/agent/arxiv"))
    parser.add_argument("--cache", type=Path, default=Path("/tmp/gh-aw/cache-memory"))
    args = parser.parse_args()
    if args.command == "prepare":
        prepare(args.memory, args.data, args.cache)
    else:
        update(args.memory, args.data, args.cache)


if __name__ == "__main__":
    main()
