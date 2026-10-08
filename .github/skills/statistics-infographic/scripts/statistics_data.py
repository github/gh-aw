"""Validated statistics and exact, count-derived display values."""

from __future__ import annotations

from dataclasses import dataclass
from datetime import date, datetime, timedelta
from fractions import Fraction
import json
import math
from pathlib import Path
import re
from types import MappingProxyType
from typing import Mapping


class DataError(ValueError):
    """An input dataset violates the documented statistical contract."""


def require(condition: bool, message: str) -> None:
    if not condition:
        raise DataError(message)


def mapping(value: object, name: str) -> dict[str, object]:
    require(isinstance(value, dict), f"{name}: expected an object")
    if not isinstance(value, dict):
        raise DataError(name)
    result: dict[str, object] = {}
    for key, item in value.items():
        require(isinstance(key, str), f"{name}: keys must be strings")
        result[str(key)] = item
    return result


def rows(value: object, name: str) -> list[dict[str, object]]:
    require(isinstance(value, list), f"{name}: expected an array")
    if not isinstance(value, list):
        raise DataError(name)
    return [mapping(item, name) for item in value]


def string(value: object, name: str) -> str:
    if not isinstance(value, str) or not value.strip():
        raise DataError(f"{name}: expected a nonempty string")
    return value


def count(value: object, name: str) -> int:
    if not isinstance(value, int) or isinstance(value, bool) or value < 0:
        raise DataError(f"{name}: expected a nonnegative integer")
    return value


def number(value: object, name: str) -> float:
    if not isinstance(value, (int, float)) or isinstance(value, bool):
        raise DataError(f"{name}: expected a finite number")
    result = float(value)
    require(math.isfinite(result) and result >= 0, f"{name}: expected a finite nonnegative number")
    return result


def decimal(value: Fraction, places: int = 1) -> str:
    """Round an exact rational half up, without binary-float intermediates."""
    require(places >= 0, "decimal places must be nonnegative")
    sign = "-" if value < 0 else ""
    value = abs(value)
    scale = 10 ** places
    whole, remainder = divmod(value.numerator * scale, value.denominator)
    if 2 * remainder >= value.denominator:
        whole += 1
    if places == 0:
        return f"{sign}{whole}"
    return f"{sign}{whole // scale}.{whole % scale:0{places}d}"


def percent(part: int, total: int, places: int = 1) -> str:
    require(0 <= part <= total, "percentage numerator must be within its denominator")
    return "n/a" if total == 0 else decimal(Fraction(100 * part, total), places) + "%"


def check_published(record: dict[str, object], key: str, expected: str) -> None:
    if key not in record:
        return
    value = record[key]
    if expected == "n/a":
        require(value is None, f"{key}: undefined values must be null")
    else:
        actual = number(value, key)
        require(math.isclose(actual, float(expected.rstrip("%")), abs_tol=1e-10),
                f"{key}: stored {actual} disagrees with derived {expected}")


@dataclass(frozen=True)
class Outcome:
    label: str
    merged: int
    closed: int
    opened: int

    @property
    def total(self) -> int:
        return self.merged + self.closed + self.opened

    @property
    def merge_rate(self) -> str:
        return percent(self.merged, self.merged + self.closed)

    @property
    def ratio(self) -> str:
        return "n/a" if self.closed == 0 else decimal(Fraction(self.merged, self.closed), 2) + " : 1"


@dataclass(frozen=True)
class Bin:
    label: str
    lower: float
    upper: float | None
    count: int


@dataclass(frozen=True)
class Group:
    language: str
    role: str
    files: int
    lines: int


@dataclass(frozen=True)
class Week:
    start: date
    added: int
    deleted: int


@dataclass(frozen=True)
class Statistics:
    repository: str
    snapshot: date
    community_collected: datetime
    commit: str
    outcomes: tuple[Outcome, ...]
    components: Mapping[str, int]
    assumption: str
    bins: tuple[Bin, ...]
    median_hours: float | None
    p90_hours: float | None
    labelled_issues: int
    eligible_issues: int
    authors: int
    landed: int
    closed_no_link: int
    open_issues: int
    delivery_prs: int
    cca_delivery: int
    groups: tuple[Group, ...]
    weeks: tuple[Week, ...]

    @property
    def all_prs(self) -> Outcome:
        return Outcome("All PRs", sum(o.merged for o in self.outcomes),
                       sum(o.closed for o in self.outcomes), sum(o.opened for o in self.outcomes))

    @property
    def lines(self) -> int:
        return sum(g.lines for g in self.groups)

    def under(self, hours: float) -> int:
        return sum(b.count for b in self.bins if b.upper is not None and b.upper <= hours)

    @classmethod
    def from_sources(cls, sources: dict[str, object]) -> "Statistics":
        o, h, c, s, w = [mapping(sources.get(key), key) for key in
                         ("origins", "histogram", "community", "structure", "history")]
        repo = string(o.get("repository"), "repository")
        require(all(source.get("repository") == repo for source in (h, c, s, w)),
                "all source repositories must match")
        snapshot = date.fromisoformat(string(o.get("snapshot_date"), "snapshot_date"))
        require(h.get("snapshot_date") == snapshot.isoformat(), "PR and histogram snapshot dates must match")
        collected = datetime.fromisoformat(string(c.get("collected_at"), "collected_at").replace("Z", "+00:00"))
        require(collected.utcoffset() is not None, "community collected_at must include a timezone")
        commit = string(s.get("commit"), "commit")
        require(re.fullmatch(r"(?:[0-9a-fA-F]{40}|[0-9a-fA-F]{64})", commit) is not None, "commit must be a full hexadecimal object ID")
        require(w.get("commit") == commit, "code map and history commits must match")
        labels = ("AW-started", "Human-started, including assumed direct sessions", "Other / unattributed")
        outcome_records = rows(o.get("outcomes"), "outcomes")
        indexed = {string(r.get("origin"), "outcome.origin"): r for r in outcome_records}
        require(len(indexed) == len(outcome_records), "duplicate outcome origins")
        require(set(indexed) in (set(labels), set(labels) | {"All PRs"}), "expected AW, human and other outcomes")
        outcomes = tuple(Outcome(label, count(indexed[label].get("merged"), "merged"),
                                 count(indexed[label].get("closed_without_merging"), "closed_without_merging"),
                                 count(indexed[label].get("open"), "open")) for label in labels)
        total = sum(row.total for row in outcomes)
        require(total == count(o.get("total_prs"), "total_prs") and total > 0, "PR totals must reconcile and be positive")
        breakdown = rows(o.get("breakdown"), "breakdown")
        require(len(breakdown) == 3 and {string(r.get("origin"), "breakdown.origin") for r in breakdown} == set(labels), "expected three distinct origin buckets")
        for outcome in outcomes:
            row = next(r for r in breakdown if r.get("origin") == outcome.label)
            require(count(row.get("prs"), "origin.prs") == outcome.total, "origin and outcome counts disagree")
            check_published(row, "percent", percent(outcome.total, total))
        all_prs = Outcome("All PRs", sum(r.merged for r in outcomes), sum(r.closed for r in outcomes), sum(r.opened for r in outcomes))
        for outcome in (*outcomes, all_prs):
            if outcome.label not in indexed:
                continue
            row = indexed[outcome.label]
            require((count(row.get("merged"), "merged"), count(row.get("closed_without_merging"), "closed_without_merging"), count(row.get("open"), "open")) ==
                    (outcome.merged, outcome.closed, outcome.opened), "all-PR outcome totals disagree")
            check_published(row, "merge_rate_completed_percent", outcome.merge_rate)
            check_published(row, "merged_to_closed_ratio", outcome.ratio.split(" : ")[0])
        component_data = mapping(o.get("components"), "components")
        component_keys = ("direct_aw_prs", "cca_aw_issue_prs", "cca_human_issue_prs",
                          "cca_no_issue_assumed_human_prs", "other_account_prs", "dependabot_prs", "mix_prs")
        components = {key: count(component_data.get(key), key) for key in component_keys}
        require(components["direct_aw_prs"] + components["cca_aw_issue_prs"] == outcomes[0].total, "AW components disagree")
        require(components["cca_human_issue_prs"] + components["cca_no_issue_assumed_human_prs"] == outcomes[1].total, "human components disagree")
        require(sum(components[key] for key in component_keys[4:]) == outcomes[2].total, "other components disagree")
        assumption = string(o.get("assumption"), "assumption")
        require(count(h.get("merged_pr_count"), "merged_pr_count") == all_prs.merged, "histogram population disagrees with merged PRs")
        for key, expected in (("excluded_closed_unmerged_count", all_prs.closed), ("excluded_open_count", all_prs.opened)):
            if key in h:
                require(count(h[key], key) == expected, f"{key} disagrees with PR census")
        bins = tuple(Bin(string(r.get("bin"), "bin"), number(r.get("lower_hours_inclusive"), "lower_hours"),
                         None if r.get("upper_hours_exclusive") is None else number(r["upper_hours_exclusive"], "upper_hours"),
                         count(r.get("count"), "bin.count")) for r in rows(h.get("histogram"), "histogram"))
        require(len({b.label for b in bins}) == len(bins), "histogram labels must be distinct")
        require(len(bins) >= 1 and bins[0].lower == 0 and bins[-1].upper is None, "histogram must cover [0, infinity)")
        for i, bucket in enumerate(bins):
            require(bucket.upper is None or bucket.upper > bucket.lower, "bin upper bound must exceed lower bound")
            if i:
                require(bins[i - 1].upper == bucket.lower, "histogram bins must be contiguous and ordered")
        require(sum(b.count for b in bins) == all_prs.merged, "histogram counts must sum to merged PRs")
        require({1, 6, 24} <= {b.upper for b in bins}, "histogram needs boundaries at 1, 6 and 24 hours")
        for r, bucket in zip(rows(h["histogram"], "histogram"), bins):
            check_published(r, "percent", percent(bucket.count, all_prs.merged))
        for key, threshold in (("under_one_hour_percent", 1), ("under_six_hours_percent", 6), ("under_24_hours_percent", 24)):
            under = sum(b.count for b in bins if b.upper is not None and b.upper <= threshold)
            check_published(h, key, percent(under, all_prs.merged))
        quantiles = mapping(h.get("summary_hours"), "summary_hours")
        median = None if all_prs.merged == 0 else number(quantiles.get("median"), "median")
        p90 = None if all_prs.merged == 0 else number(quantiles.get("p90"), "p90")
        if median is not None and p90 is not None:
            require(median <= p90, "median must not exceed P90")
            for q, value in ((Fraction(1, 2), median), (Fraction(9, 10), p90)):
                position = (all_prs.merged - 1) * q
                lower_index = position.numerator // position.denominator
                fraction = position - lower_index
                intervals = []
                for index in (lower_index, min(lower_index + 1, all_prs.merged - 1)):
                    offset = 0
                    for bucket in bins:
                        offset += bucket.count
                        if index < offset:
                            intervals.append(bucket)
                            break
                lower_bound = (1 - fraction) * Fraction(str(intervals[0].lower)) + fraction * Fraction(str(intervals[1].lower))
                require(value >= float(lower_bound), "timing quantile is below its histogram-implied minimum")
                if intervals[0].upper is not None and (fraction == 0 or intervals[1].upper is not None):
                    upper_bound = (1 - fraction) * intervals[0].upper
                    if fraction and intervals[1].upper is not None:
                        upper_bound += fraction * intervals[1].upper
                    require(value < float(upper_bound), "timing quantile exceeds its histogram-implied maximum")
        else:
            require(quantiles.get("median") is None and quantiles.get("p90") is None, "empty timing populations require null quantiles")
        community_keys = ("all_labelled_issues", "third_party_issues", "third_party_author_accounts",
                          "third_party_issues_landed_main", "closed_without_merged_main_link",
                          "third_party_open_issues", "unique_merged_main_delivery_prs", "copilot_authored_delivery_prs")
        labelled, eligible, authors, landed, closed, opened, delivery, cca = [count(c.get(key), key) for key in community_keys]
        require(landed + closed + opened == eligible <= labelled, "community issue counts do not reconcile")
        require(authors <= eligible and (eligible == 0 or authors > 0), "author count must fit eligible issue population")
        require(cca <= delivery and ((landed == 0) == (delivery == 0)), "delivery PR counts do not reconcile")
        check_published(c, "landed_percent", percent(landed, eligible))
        check_published(c, "copilot_delivery_percent", percent(cca, delivery))
        groups = tuple(Group(string(r.get("language"), "language"), string(r.get("role"), "role"),
                             count(r.get("files"), "files"), count(r.get("nonblank_lines"), "nonblank_lines"))
                       for r in rows(s.get("groups"), "groups"))
        require(len(groups) == 4 and {(g.language, g.role) for g in groups} ==
                {(lang, role) for lang in ("Go", "JavaScript") for role in ("Implementation", "Tests")}, "expected four distinct Go/JS role groups")
        require(all(g.files > 0 or g.lines == 0 for g in groups), "nonzero lines require files")
        require(sum(g.lines for g in groups) == count(s.get("nonblank_lines"), "nonblank_lines"), "code line totals disagree")
        require(sum(g.files for g in groups) == count(s.get("included_files"), "included_files"), "code file totals disagree")
        for record, group in zip(rows(s["groups"], "groups"), groups):
            check_published(record, "percent", percent(group.lines, sum(g.lines for g in groups)))
        weeks = tuple(Week(date.fromisoformat(string(r.get("week_start"), "week_start")),
                           count(r.get("added"), "added"), count(r.get("deleted"), "deleted"))
                      for r in rows(w.get("weeks"), "weeks"))
        require(all(week.start.weekday() == 0 for week in weeks), "weeks must start on Monday")
        require(all(b.start - a.start == timedelta(days=7) for a, b in zip(weeks, weeks[1:])), "weeks must be contiguous and ordered")
        require(len(weeks) == count(w.get("week_count"), "week_count"), "week count disagrees")
        require(sum(r.added for r in weeks) == count(w.get("total_added"), "total_added"), "weekly addition totals disagree")
        require(sum(r.deleted for r in weeks) == count(w.get("total_deleted"), "total_deleted"), "weekly deletion totals disagree")
        return cls(repo, snapshot, collected, commit, outcomes, MappingProxyType(components), assumption, bins, median, p90,
                   labelled, eligible, authors, landed, closed, opened, delivery, cca, groups, weeks)


FILENAMES = {
    "origins": "pr-initiation-assumption-summary.json",
    "histogram": "pr-time-to-merge-histogram.json",
    "community": "community-statistics-summary.json",
    "structure": "repo-structure-statistics.json",
    "history": "weekly-code-history.json",
}


def load_sources(path: Path, directory: bool = False) -> dict[str, object]:
    if directory:
        return {key: json.loads((path / name).read_text(encoding="utf-8")) for key, name in FILENAMES.items()}
    return mapping(json.loads(path.read_text(encoding="utf-8")), "input")
