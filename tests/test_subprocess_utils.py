"""Tests for subprocess_utils.py — subprocess.run is mocked throughout."""

import subprocess
from unittest import mock

import pytest

from ytextract.subprocess_utils import CommandError, CommandResult, run_command


class FakeProc:
    def __init__(self, stdout="", stderr="", returncode=0):
        self.stdout = stdout
        self.stderr = stderr
        self.returncode = returncode


def test_success_returns_captured_output():
    fake = FakeProc(stdout="out", stderr="err", returncode=0)
    with mock.patch(
        "ytextract.subprocess_utils.subprocess.run", return_value=fake
    ) as run:
        result = run_command(["tool", "arg"], timeout=10.0, cwd="/tmp", env={"A": "1"})
    assert result == CommandResult("out", "err", 0)
    run.assert_called_once_with(
        ["tool", "arg"],
        capture_output=True,
        text=True,
        timeout=10.0,
        cwd="/tmp",
        env={"A": "1"},
        check=False,
    )


def test_nonzero_exit_raises_with_detail():
    fake = FakeProc(stdout="", stderr="boom", returncode=3)
    with (
        mock.patch("ytextract.subprocess_utils.subprocess.run", return_value=fake),
        pytest.raises(CommandError, match=r"exit 3.*boom"),
    ):
        run_command(["tool"])


def test_nonzero_without_output_still_raises():
    fake = FakeProc(returncode=1)
    with (
        mock.patch("ytextract.subprocess_utils.subprocess.run", return_value=fake),
        pytest.raises(CommandError, match="exit 1"),
    ):
        run_command(["tool"])


def test_check_false_returns_nonzero_result():
    fake = FakeProc(stdout="partial", stderr="warn", returncode=1)
    with mock.patch("ytextract.subprocess_utils.subprocess.run", return_value=fake):
        result = run_command(["tool"], check=False)
    assert result == CommandResult("partial", "warn", 1)


def test_missing_binary_raises():
    with (
        mock.patch(
            "ytextract.subprocess_utils.subprocess.run",
            side_effect=FileNotFoundError,
        ),
        pytest.raises(CommandError, match="binary not found"),
    ):
        run_command(["missing-tool"])


def test_timeout_raises():
    with (
        mock.patch(
            "ytextract.subprocess_utils.subprocess.run",
            side_effect=subprocess.TimeoutExpired(cmd=["tool"], timeout=5.0),
        ),
        pytest.raises(CommandError, match="timed out"),
    ):
        run_command(["tool"], timeout=5.0)
