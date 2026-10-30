#!/usr/bin/env python3
"""Pinned Harbor verifier/parser integration, with no Docker or model required."""

import asyncio
from importlib.metadata import version
import json
from pathlib import Path
from types import SimpleNamespace
import tempfile
import unittest
from unittest.mock import patch

from harbor.models.task.config import TaskOS
from harbor.models.task.task import Task
from harbor.models.trial.error_policy import FailureCause, policy_for
from harbor.models.trial.paths import TrialPaths
from harbor.verifier.verifier import RewardFileNotFoundError, Verifier

from test_verify import HERE, ROOT, envelope, selection, verify


class LocalVerifierEnvironment:
    """Filesystem/exec stand-in; Harbor's real verifier and error policy are used."""

    os = TaskOS.LINUX

    def __init__(self, directory, entrypoint):
        self.directory = directory
        self.entrypoint = entrypoint

    def is_path_mounted(self, *_):
        return True

    async def exec(self, command, **_):
        if command.startswith("rm -f -- "):
            for name in ("reward.json", "reward.txt"):
                (self.directory / name).unlink(missing_ok=True)
            code = 0
        elif command.startswith("chmod "):
            code = 0
        else:
            code = self.entrypoint()
        return SimpleNamespace(return_code=code, stdout="", stderr="")


class HarborContractTests(unittest.TestCase):
    def test_pinned_verifier_reward_and_error_classification(self):
        self.assertEqual(version("harbor"), "0.25.0")
        task = Task(HERE / "task")
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            config = root / "mapping.json"
            workflow = root / "workflow.md"
            workflow.write_bytes((HERE / "task/tests/workflow.md").read_bytes())
            session = root / "session.jsonl"
            session.write_bytes((HERE / "task/environment/aw_session.jsonl").read_bytes())
            paths = TrialPaths(root / "trial")
            paths.mkdir()
            cases = [
                ("pass", [(0, envelope())], [selection()], {"reward": 1}),
                ("fail", [(0, envelope(0, False, "fail"))], [selection()], {"reward": 0}),
                ("unavailable", [(0, envelope(None, None, "unavailable"))], [selection()], None),
                ("error", [(1, envelope(None, None, "error"))], [selection()], None),
            ]
            optional = envelope(None, None, "unavailable")
            optional["definition"]["id"] = optional["result"]["id"] = "optional"
            cases.append((
                "optional-unavailable", [(0, envelope()), (0, optional)],
                [selection(), selection("raw", False, "optional", "optional")], {"reward": 1},
            ))
            for name, results, metrics, rewards in cases:
                config.write_text(json.dumps({"version": 1, "workflow": "workflow.md", "metrics": metrics}))
                environment = LocalVerifierEnvironment(
                    paths.verifier_dir,
                    lambda: verify.verify(config, session, paths.verifier_dir, ROOT / "gh-aw"),
                )
                verifier = Verifier(task, paths, environment, skip_tests_upload=True)
                with self.subTest(name=name), patch.object(
                    verify, "run_grader", side_effect=[(*result, "") for result in results]
                ):
                    if rewards is not None:
                        result = asyncio.run(verifier.verify())
                        self.assertEqual(result.rewards, rewards)
                    else:
                        with self.assertRaises(RewardFileNotFoundError) as raised:
                            asyncio.run(verifier.verify())
                        policy = policy_for(type(raised.exception).__name__)
                        self.assertEqual(policy.cause, FailureCause.VERIFIER_FAILED)
                        self.assertFalse(policy.reward_valid)
                        self.assertFalse(policy.retry)
                        report = json.loads((paths.verifier_dir / "gh-aw-graders.json").read_text())
                        self.assertEqual(report["status"], name)


if __name__ == "__main__":
    unittest.main()
