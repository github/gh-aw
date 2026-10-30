#!/usr/bin/env python3
"""Consume a relocated gh-aw export with the pinned, unmodified Harbor package."""

import hashlib
import importlib.metadata
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import time
import unittest
from urllib.parse import quote
from urllib.request import ProxyHandler, build_opener


class HarborConformance(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        if importlib.metadata.version("harbor") != "0.25.0":
            raise RuntimeError("Conformance requires harbor==0.25.0; install the pinned dependency")
        # These imports must fail, not skip, if the installed dependency is broken.
        from harbor.models.job.config import JobConfig
        from harbor.models.job.result import JobResult
        from harbor.models.trial.config import TrialConfig
        from harbor.models.trial.result import TrialResult
        from harbor.models.trial.paths import TrialPaths
        from harbor.models.trajectories.trajectory import Trajectory
        from harbor.viewer.scanner import JobScanner

        cls.JobConfig = JobConfig
        cls.JobResult = JobResult
        cls.TrialConfig = TrialConfig
        cls.TrialResult = TrialResult
        cls.TrialPaths = TrialPaths
        cls.Trajectory = Trajectory
        cls.scanner = JobScanner(JOBS)

    def read_native(self, path, model):
        data = json.loads(path.read_text())
        self.assertLessEqual(set(data), set(model.model_fields), str(path))
        return model.model_validate(data)

    def test_upstream_golden(self):
        fixture = (
            Path(__file__).resolve().parents[1]
            / "pkg/cli/testdata/harbor/hello-world-invalid-json.trajectory.json"
        )
        # Verify the pinned upstream Git blob, ignoring our documented final newline.
        original = fixture.read_bytes().removesuffix(b"\n")
        git_blob = b"blob " + str(len(original)).encode() + b"\0" + original
        self.assertEqual(
            hashlib.sha1(git_blob, usedforsecurity=False).hexdigest(),
            "a6ea0ce8715916d8c6d437503f2be003d3375df9",
        )
        trajectory = self.Trajectory.model_validate_json(original)
        self.assertEqual(trajectory.agent.name, "terminus-2")
        self.assertEqual(len(trajectory.steps), 5)
        self.assertEqual(trajectory.final_metrics.total_prompt_tokens, 2417)

    def test_native_validation_rejects_malformed_data(self):
        trial = self.scanner.list_trials(JOB)[0]
        paths = self.TrialPaths(JOBS / JOB / trial)
        trajectory = json.loads((paths.agent_dir / "trajectory.json").read_text())
        trajectory["steps"][0]["source"] = "not-a-valid-role"
        with self.assertRaises(ValueError):
            self.Trajectory.model_validate(trajectory)
        result = json.loads(paths.result_path.read_text())
        result["verifier_result"] = {"rewards": {"gh-aw.quality": "not-a-number"}}
        with self.assertRaises(ValueError):
            self.TrialResult.model_validate(result)

    def test_native_models_and_scanner(self):
        self.assertEqual(self.scanner.list_jobs(), [JOB])
        config = self.read_native(JOBS / JOB / "config.json", self.JobConfig)
        job_result = self.read_native(JOBS / JOB / "result.json", self.JobResult)
        self.assertEqual(self.scanner.get_job_config(JOB), config)
        self.assertEqual(self.scanner.get_job_result(JOB), job_result)
        marker = json.loads((JOBS / JOB / "gh-aw-export.json").read_text())
        self.assertEqual(marker["mode"], "recorded-evidence")
        self.assertFalse(marker["runnable"])
        trials = self.scanner.list_trials(JOB)
        self.assertEqual(len(trials), 3)
        self.assertEqual(job_result.n_total_trials, len(trials))
        self.assertEqual(len(job_result.trial_results), len(trials))
        embedded_results = {result.trial_name: result for result in job_result.trial_results}
        observed_graders = []
        observed_tools = 0
        for name in trials:
            with self.subTest(trial=name):
                paths = self.TrialPaths(JOBS / JOB / name)
                config = self.read_native(paths.config_path, self.TrialConfig)
                result = self.read_native(paths.result_path, self.TrialResult)
                self.assertEqual(self.scanner.get_trial_config(JOB, name), config)
                self.assertEqual(self.scanner.get_trial_result(JOB, name), result)
                self.assertEqual(embedded_results[name], result)
                self.assertEqual(result.trial_name, name)
                self.assertEqual(result.config, config)
                self.assertEqual(result.task_checksum, "")
                trajectory = self.read_native(paths.agent_dir / "trajectory.json", self.Trajectory)
                self.assertGreater(len(trajectory.steps), 0)
                calls = {
                    call.tool_call_id: call
                    for step in trajectory.steps for call in step.tool_calls or []
                }
                observations = {
                    observation.source_call_id: observation
                    for step in trajectory.steps if step.observation
                    for observation in step.observation.results
                }
                session = paths.trial_dir / "gh-aw/evidence/usage/aw_session.jsonl"
                for line in session.read_text().splitlines():
                    event = json.loads(line)
                    if event["type"] == "tool.execution_start":
                        data = event["data"]
                        call = calls[data["toolCallId"]]
                        self.assertEqual(call.function_name, data["toolName"])
                        self.assertEqual(call.arguments, data["input"])
                        observed_tools += 1
                    elif event["type"] == "tool.execution_complete":
                        data = event["data"]
                        observation = observations[data["toolCallId"]]
                        self.assertEqual(json.loads(observation.content), data["output"])
                rewards = result.verifier_result.rewards if result.verifier_result else None
                metadata = json.loads((paths.trial_dir / "gh-aw/metadata.json").read_text())
                self.assertEqual(metadata["experiments"]["assignments"], {"strategy": "candidate"})
                self.assertEqual(metadata["session_status"], "recorded")
                self.assertIn("not a runnable Harbor task", metadata["task_identity"])
                observed_graders.append(metadata["graders"])
                for evidence in metadata["evidence_files"]:
                    self.assertTrue((paths.trial_dir / evidence).is_file(), evidence)
                if metadata["graders"] is not None:
                    graders = metadata["graders"]["results"]
                    self.assertEqual(len(graders), 7)
                    quality = next(g for g in graders if g["id"] == "quality")
                    self.assertIn(quality["value"], [0.25, 0.75])
                    self.assertEqual(quality["passed"], quality["value"] == 0.75)
                    self.assertEqual(quality["status"], "pass" if quality["passed"] else "fail")
                    self.assertEqual(quality["threshold"], 0.5)
                    self.assertEqual(quality["direction"], "maximize")
                    self.assertEqual(rewards, {"gh-aw.quality": quality["value"]})
                    self.assertEqual(json.loads(paths.reward_json_path.read_text()), rewards)
                    mapping = metadata["reward_mapping"]["metrics"]["gh-aw.quality"]
                    self.assertEqual(mapping["grader_id"], "quality")
                    self.assertEqual(mapping["status"], quality["status"])
                    self.assertEqual(mapping["source_field"], "value")
                    self.assertEqual(
                        mapping["source_file"], "gh-aw/evidence/graders/grader_results.json"
                    )
                    self.assertEqual(mapping["transformation"], "none; original numeric JSON value")
                    raw = json.loads(
                        (paths.trial_dir / "gh-aw/evidence/graders/grader_results.json").read_text()
                    )
                    self.assertEqual(len(raw["results"]), 7)
                    self.assertEqual(raw["results"][0]["value"], quality["value"])
                    self.assertEqual(raw["results"][1]["value"], 999)
                    self.assertIs(raw["results"][4]["value"], True)
                    self.assertEqual(
                        [g["value"] for g in raw["results"] if g["id"] == "duplicate"],
                        [0.1, 0.9],
                    )
                    manifest = json.loads(
                        (paths.trial_dir / "gh-aw/evidence/graders/grader_manifest.json").read_text()
                    )
                    self.assertEqual(manifest["graders"][0]["threshold"], 0.5)
                else:
                    self.assertIsNone(rewards, "missing grading must remain unknown")
                    self.assertFalse(paths.reward_json_path.exists())
                    self.assertIsNone(metadata["reward_mapping"])
        self.assertEqual(sum(g is None for g in observed_graders), 1)
        self.assertEqual(sum(g is not None for g in observed_graders), 2)
        self.assertGreater(observed_tools, 0, "real fixture tool calls must survive export")
        self.assertEqual(
            sorted(
                result["value"]
                for graders in observed_graders if graders is not None
                for result in graders["results"] if result["id"] == "quality"
            ),
            [0.25, 0.75],
        )

    def test_cli_viewer_http(self):
        env = {
            key: value
            for key, value in os.environ.items()
            if not key.startswith(("GH_", "GITHUB_", "HARBOR_"))
            and not key.endswith(("_TOKEN", "_KEY", "_SECRET"))
        }
        home = JOBS.parent / "empty-home"
        home.mkdir(exist_ok=True)
        env.update(
            HOME=str(home), XDG_CONFIG_HOME=str(home), HARBOR_TELEMETRY="0",
            PYTHONDONTWRITEBYTECODE="1",
        )
        command = [sys.executable, "-m", "harbor.cli.main"]
        version = subprocess.run(
            command + ["--version"], env=env, text=True, capture_output=True, timeout=30
        )
        self.assertEqual(version.returncode, 0, version.stdout + version.stderr)
        self.assertIn("0.25.0", version.stdout + version.stderr)
        with socket.socket() as available:
            available.bind(("127.0.0.1", 0))
            port = available.getsockname()[1]
        log_path = JOBS.parent / "harbor-view.log"
        with log_path.open("w+") as log:
            process = subprocess.Popen(
                command + [
                    "view", str(JOBS), "--jobs", "--no-build",
                    "--host", "127.0.0.1", "--port", str(port),
                ],
                env=env, stdout=log, stderr=subprocess.STDOUT, cwd=JOBS.parent,
            )
            try:
                # Disable ambient HTTP proxies even for loopback requests.
                opener = build_opener(ProxyHandler({}))

                def get(path):
                    with opener.open(f"http://127.0.0.1:{port}{path}", timeout=5) as response:
                        self.assertEqual(response.status, 200)
                        return json.load(response)

                deadline = time.monotonic() + 40
                while True:
                    try:
                        get("/api/health")
                        break
                    except (OSError, ValueError):
                        if process.poll() is not None or time.monotonic() >= deadline:
                            log.flush()
                            self.fail(f"harbor view failed to start:\n{log_path.read_text()}")
                        time.sleep(0.1)
                jobs = get("/api/jobs")
                self.assertEqual(jobs["total"], 1)
                self.assertEqual(jobs["items"][0]["name"], JOB)
                prefix = "/api/jobs/" + quote(JOB, safe="")
                self.assertEqual(get(prefix)["n_total_trials"], 3)
                self.assertIsInstance(get(prefix + "/config"), dict)
                trials = get(prefix + "/trials")
                self.assertEqual(trials["total"], 3)
                for trial in trials["items"]:
                    self.assertIn(trial["reward"], [None, 0.25, 0.75])
                    path = prefix + "/trials/" + quote(trial["name"], safe="")
                    result = self.TrialResult.model_validate(get(path))
                    self.assertEqual(result.trial_name, trial["name"])
                    trajectory = self.Trajectory.model_validate(get(path + "/trajectory"))
                    self.assertTrue(trajectory.steps)
            finally:
                process.terminate()
                try:
                    process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=10)


if __name__ == "__main__":
    if len(sys.argv) != 2:
        raise SystemExit("usage: test_harbor_conformance.py RELOCATED_JOB_DIRECTORY")
    job = Path(sys.argv.pop()).resolve()
    JOBS, JOB = job.parent, job.name
    unittest.main()
