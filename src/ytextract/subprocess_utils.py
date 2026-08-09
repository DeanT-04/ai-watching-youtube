"""Safe subprocess wrapper.

Every call to an external binary (``yt-dlp``, ``ffmpeg``, ``tesseract``)
goes through :func:`run_command` so timeouts and output capture behave
consistently and failures produce actionable errors instead of raw
tracebacks.
"""

from __future__ import annotations

import os
import subprocess
from collections.abc import Sequence
from dataclasses import dataclass

PathLike = str | os.PathLike


class CommandError(RuntimeError):
    """Raised when an external command cannot run or exits unsuccessfully."""


@dataclass(frozen=True)
class CommandResult:
    """Captured stdout/stderr of a successfully checked command."""

    stdout: str
    stderr: str
    returncode: int


def run_command(
    argv: Sequence[str],
    *,
    timeout: float = 300.0,
    cwd: PathLike | None = None,
    env: dict | None = None,
    check: bool = True,
) -> CommandResult:
    """Run ``argv`` capturing stdout/stderr, enforcing ``timeout``.

    Raises :class:`CommandError` if the binary is missing, times out, or
    (when ``check`` is true) exits non-zero. With ``check=False`` non-zero
    exits are returned as a result so callers can treat them as data.
    """
    try:
        proc = subprocess.run(
            list(argv),
            capture_output=True,
            text=True,
            timeout=timeout,
            cwd=cwd,
            env=env,
            check=False,  # returncode inspected below; nonzero handled by caller policy
        )
    except FileNotFoundError as exc:
        raise CommandError(f"binary not found for command: {argv[0]}") from exc
    except subprocess.TimeoutExpired as exc:
        raise CommandError(f"command timed out after {timeout:g}s: {argv[0]}") from exc

    result = CommandResult(proc.stdout, proc.stderr, proc.returncode)
    if check and proc.returncode != 0:
        detail = (proc.stderr or proc.stdout).strip()
        suffix = f": {detail}" if detail else ""
        raise CommandError(
            f"command failed (exit {proc.returncode}): {' '.join(argv)}{suffix}"
        )
    return result
