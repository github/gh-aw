"""Exercise the workflow's chart selectors without plotting or external reads."""

import ast
import math
import re
import sys
import unittest
from pathlib import Path


SOURCE = (
    Path(__file__).resolve().parent.parent
    / ".github/workflows/daily-spending-forecast.md"
)


def select_chart_data(chart, forecast):
    scripts = re.findall(r"```python\n(.*?)\n```", SOURCE.read_text(), re.DOTALL)
    tree = ast.parse(scripts[chart])
    result = "samples" if chart == 0 else "rows"
    start = next(
        index
        for index, node in enumerate(tree.body)
        if isinstance(node, ast.Assign)
        and isinstance(node.targets[0], ast.Name)
        and node.targets[0].id == result
    )
    selector = ast.Module(body=tree.body[start : start + 2], type_ignores=[])
    scope = {"forecast": forecast, "math": math, "sys": sys}
    exec(compile(selector, str(SOURCE), "exec"), scope)
    return scope[result]


def workflow(samples=None, percentiles=(1, 2, 3), **fields):
    return {
        "workflow_id": "daily-example",
        "run_samples": samples or [],
        "weekly_monte_carlo": dict(
            zip(
                ("p10_projected_aic", "p50_projected_aic", "p90_projected_aic"),
                percentiles,
            )
        ),
        **fields,
    }


class SpendingForecastChartTests(unittest.TestCase):
    def test_numeric_forecast_alone_is_not_usage_or_coverage_evidence(self):
        forecast = {
            "workflows": [
                workflow([{"date": "2026-11-01", "aic": 0}]),
                workflow([{"date": "2026-11-02", "aic": 5}]),
            ]
        }
        self.assertEqual(select_chart_data(0, forecast), [])
        self.assertEqual(select_chart_data(1, forecast), [])

    def test_partial_recovery_retains_verified_usage_including_genuine_zero(self):
        forecast = {
            "workflows": [
                workflow(
                    [
                        {"date": "2026-11-01", "aic": 0},
                        {"date": "2026-11-02", "aic": 5, "usage_verified": True},
                        {"date": "2026-11-03", "aic": 0, "usage_verified": True},
                    ]
                )
            ]
        }
        samples = select_chart_data(0, forecast)
        self.assertEqual([sample["aic"] for sample in samples], [5, 0])
        self.assertEqual(select_chart_data(1, forecast), [])

    def test_invalid_usage_is_excluded_even_with_verification_flag(self):
        for aic in (None, "0", True, -1, math.inf, math.nan, 10**400):
            with self.subTest(aic=aic):
                forecast = {
                    "workflows": [
                        workflow(
                            [{"date": "2026-11-01", "aic": aic, "usage_verified": True}]
                        )
                    ]
                }
                self.assertEqual(select_chart_data(0, forecast), [])

    def test_projection_requires_verified_coverage_and_valid_percentiles(self):
        forecast = {
            "workflows": [
                workflow(coverage_verified=True),
                workflow(percentiles=(0, 0, 0), coverage_verified=True),
                workflow(coverage_verified=False),
                workflow(coverage_verified=1),
                workflow(percentiles=(True, 2, 3), coverage_verified=True),
                workflow(percentiles=(-1, 2, 3), coverage_verified=True),
                workflow(percentiles=(3, 2, 1), coverage_verified=True),
                workflow(percentiles=(1, 2, math.inf), coverage_verified=True),
                workflow(percentiles=(1, 2, 10**400), coverage_verified=True),
                workflow(percentiles=(1, 2, None), coverage_verified=True),
            ]
        }
        rows = select_chart_data(1, forecast)
        self.assertEqual([row["p50"] for row in rows], [2, 0])

    def test_malformed_forecast_containers_are_unavailable(self):
        for forecast in (
            None,
            [],
            1,
            {},
            {"workflows": None},
            {"workflows": 1},
            {"workflows": {"workflow_id": "daily-example"}},
            {"workflows": "daily-example"},
        ):
            with self.subTest(forecast=forecast):
                self.assertEqual(select_chart_data(0, forecast), [])
                self.assertEqual(select_chart_data(1, forecast), [])

    def test_malformed_workflow_ids_and_sample_shapes_are_excluded(self):
        sample = {"date": "2026-11-01", "aic": 0, "usage_verified": True}
        for workflow_id in (None, 1, True, [], {}, "", " "):
            with self.subTest(workflow_id=workflow_id):
                forecast = {
                    "workflows": [
                        workflow(
                            [sample],
                            workflow_id=workflow_id,
                            coverage_verified=True,
                        )
                    ]
                }
                self.assertEqual(select_chart_data(0, forecast), [])
                self.assertEqual(select_chart_data(1, forecast), [])
        for samples in (1, "samples", {"date": "2026-11-01"}, [None, 1, []]):
            with self.subTest(samples=samples):
                forecast = {"workflows": [workflow(run_samples=samples)]}
                self.assertEqual(select_chart_data(0, forecast), [])
        for date in (None, 1, [], {}, "", " "):
            with self.subTest(date=date):
                forecast = {
                    "workflows": [workflow([{**sample, "date": date}])]
                }
                self.assertEqual(select_chart_data(0, forecast), [])

    def test_malformed_percentile_shapes_are_excluded(self):
        for weekly in (None, 1, "percentiles", [], [1, 2, 3], {}, {"p10_projected_aic": 0}):
            with self.subTest(weekly=weekly):
                forecast = {
                    "workflows": [
                        workflow(coverage_verified=True, weekly_monte_carlo=weekly)
                    ]
                }
                self.assertEqual(select_chart_data(1, forecast), [])

    def test_recovered_zero_projects_only_plotting_fields(self):
        forecast = {
            "workflows": [
                workflow(
                    [
                        {
                            "date": "2026-11-01",
                            "aic": 0,
                            "usage_verified": True,
                            "run_id": 123,
                            "workflow_id": "untrusted-sample-id",
                            "metadata": {"private": "not plotting data"},
                        }
                    ]
                )
            ]
        }
        self.assertEqual(
            select_chart_data(0, forecast),
            [{"date": "2026-11-01", "aic": 0, "workflow_id": "daily-example"}],
        )


if __name__ == "__main__":
    unittest.main()
