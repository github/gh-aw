#!/usr/bin/env python3
"""Generate detection feature comparison chart from pre-computed metrics JSON.

Usage:
    python3 detection_comparison.py --metrics <path-to-metrics.json> --output <output.png>

Input JSON keys:
    regular_runs, detection_runs, regular_success_rate, detection_success_rate,
    regular_failure_count, detection_failure_count, misconfigured_count
"""

import argparse
import json
import os

import matplotlib.pyplot as plt
import numpy as np
import seaborn as sns


def main() -> None:
    parser = argparse.ArgumentParser(description="Generate detection feature comparison chart")
    parser.add_argument("--metrics", required=True, help="Path to metrics JSON file")
    parser.add_argument("--output", required=True, help="Output PNG file path")
    args = parser.parse_args()

    with open(args.metrics) as f:
        m = json.load(f)

    def get_int(key: str, default: int = 0) -> int:
        return int(m.get(key, default))

    def get_float(key: str, default: float = 0.0) -> float:
        return float(m.get(key, default))

    regular_runs = get_int("regular_runs")
    detection_runs = get_int("detection_runs")
    regular_success_rate = get_float("regular_success_rate")
    detection_success_rate = get_float("detection_success_rate")
    regular_failure = get_int("regular_failure_count")
    detection_failure = get_int("detection_failure_count")
    misconfigured_count = get_int("misconfigured_count")

    regular_success = regular_runs - regular_failure
    detection_success = detection_runs - detection_failure

    sns.set_style("whitegrid")
    palette = sns.color_palette("muted")

    fig, ax1 = plt.subplots(figsize=(10, 6), dpi=150)
    ax2 = ax1.twinx()

    x = np.array([0, 1])
    width = 0.5

    # Stacked bars: success (bottom) + failure (top)
    ax1.bar(
        x, [regular_success, detection_success], width,
        label="Success", color=palette[2], alpha=0.85,
    )
    ax1.bar(
        x, [regular_failure, detection_failure], width,
        bottom=[regular_success, detection_success],
        label="Failure", color=palette[3], alpha=0.85,
    )

    # Success-rate line on secondary axis
    ax2.plot(
        x, [regular_success_rate, detection_success_rate],
        color=palette[0], marker="o", linewidth=2, markersize=8,
        label="Success Rate %",
    )

    # Annotation band when misconfigured workflows are present
    if misconfigured_count > 0:
        max_y = max(regular_runs, detection_runs, 1)
        ax1.axhspan(
            0, max_y * 0.12, alpha=0.12, color="red",
            label=f"{misconfigured_count} misconfigured workflow(s)",
        )

    ax1.set_xticks(x)
    ax1.set_xticklabels(["Regular Runs", "Detection Runs"], fontsize=12)
    ax1.set_ylabel("Run Count", fontsize=12)
    ax1.set_title("Detection Feature Comparison — Last 24h", fontsize=14, fontweight="bold")
    ax2.set_ylabel("Success Rate (%)", fontsize=12)
    ax2.set_ylim(0, 110)

    def combined_legend_entries(*axes):
        handles, labels = [], []
        for ax in axes:
            ax_handles, ax_labels = ax.get_legend_handles_labels()
            handles += ax_handles
            labels += ax_labels
        return handles, labels

    handles, labels = combined_legend_entries(ax1, ax2)
    ax1.legend(handles, labels, loc="upper right", fontsize=10)

    plt.tight_layout()
    os.makedirs(os.path.dirname(os.path.abspath(args.output)), exist_ok=True)
    plt.savefig(args.output, dpi=150, bbox_inches="tight")
    print(f"Chart saved to {args.output}")


if __name__ == "__main__":
    main()
