"""CPU/resource safety helpers (Section 6 of the build prompt).

Heavy loops (OCR over many frames, batch Whisper calls) call
:func:`wait_if_busy` between units of work so the machine stays usable;
:func:`bounded_map` applies it automatically while capping concurrency at
``Config.max_workers`` (never the full core count).
"""

from __future__ import annotations

import logging
import time
from collections.abc import Callable, Iterable
from concurrent.futures import ThreadPoolExecutor
from typing import TypeVar

import psutil

from .config import Config

logger = logging.getLogger("ytextract.resource")

T = TypeVar("T")
R = TypeVar("R")


def wait_if_busy(cfg: Config) -> None:
    """Sleep briefly when sustained CPU usage is at/above the threshold."""
    usage = psutil.cpu_percent(interval=cfg.cpu_poll_interval)
    if usage >= cfg.cpu_threshold_percent:
        logger.warning(
            "CPU at %.0f%% (>= %.0f%%), sleeping %.1fs",
            usage,
            cfg.cpu_threshold_percent,
            cfg.cpu_sleep_seconds,
        )
        time.sleep(cfg.cpu_sleep_seconds)


def bounded_map(fn: Callable[[T], R], items: Iterable[T], cfg: Config) -> list[R]:
    """Map ``fn`` over ``items`` with at most ``max_workers`` threads.

    Each unit of work is preceded by a :func:`wait_if_busy` check. Results
    are returned in input order; the first raised exception propagates.
    """
    seq = list(items)
    if not seq:
        return []

    def wrapped(item: T) -> R:
        wait_if_busy(cfg)
        return fn(item)

    with ThreadPoolExecutor(max_workers=cfg.max_workers) as pool:
        return list(pool.map(wrapped, seq))
