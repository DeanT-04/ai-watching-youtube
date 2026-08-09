"""Tests for subprocess_utils.py — subprocess.run is mocked throughout."""

import subprocess
from unittest import mock

import pytest

from ytextract.subprocess_utils import CommandError, CommandResult, run_command


class FakeProc:
    def __init__(self, stdout=b"", stderr=b"", returncode=0):
        self.stdout = stdout
        self.stderr = stderr
        self.returncode = returncode


def test_success_returns_captured_output():
    fake = FakeProc(stdout=b"out", stderr=b"err", returncode=0)
    with mock.patch(
        "ytextract.subprocess_utils.subprocess.run", return_value=fake
    ) as run:
        result = run_command(["tool", "arg"], timeout=10.0, cwd="/tmp", env={"A": "1"})
    assert result == CommandResult("out", "err", 0)
    run.assert_called_once_with(
        ["tool", "arg"],
        capture_output=True,
        text=False,
        timeout=10.0,
        cwd="/tmp",
        env={"A": "1"},
        check=False,
    )


def test_non_ascii_output_decodes_without_crashing():
    # yt-dlp/ffmpeg emit titles/emoji outside cp1252; decode must not raise.
    # Raw bytes: b"\xff" and b"\x80" are invalid UTF-8 → replaced with U+FFFD.
    fake = FakeProc(
        stdout=b"caf\xc3\xa9 \xe2\x98\x95\xff", stderr=b"\x80", returncode=0
    )
    with mock.patch("ytextract.subprocess_utils.subprocess.run", return_value=fake):
        result = run_command(["tool"])
    assert result.stdout == "café ☕\ufffd"
    assert result.stderr == "\ufffd"


def test_nonzero_exit_raises_with_detail():
    fake = FakeProc(stdout=b"", stderr=b"boom", returncode=3)
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
    fake = FakeProc(stdout=b"partial", stderr=b"warn", returncode=1)
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
