"""Tests for resource_monitor.py — psutil and time.sleep are mocked."""

from unittest import mock

import pytest

from ytextract.config import Config
from ytextract.resource_monitor import bounded_map, wait_if_busy

CFG = Config.load(
    env={"YTEXTRACT_MAX_WORKERS": "2", "YTEXTRACT_CPU_SLEEP_SECONDS": "1.0"}
)


def test_wait_if_busy_below_threshold_no_sleep():
    with (
        mock.patch("ytextract.resource_monitor.psutil.cpu_percent", return_value=10.0),
        mock.patch("ytextract.resource_monitor.time.sleep") as sleep,
    ):
        wait_if_busy(CFG)
    sleep.assert_not_called()


def test_wait_if_busy_at_threshold_sleeps():
    with (
        mock.patch("ytextract.resource_monitor.psutil.cpu_percent", return_value=85.0),
        mock.patch("ytextract.resource_monitor.time.sleep") as sleep,
    ):
        wait_if_busy(CFG)
    sleep.assert_called_once_with(1.0)


def test_wait_if_busy_above_threshold_sleeps():
    with (
        mock.patch("ytextract.resource_monitor.psutil.cpu_percent", return_value=99.0),
        mock.patch("ytextract.resource_monitor.time.sleep") as sleep,
    ):
        wait_if_busy(CFG)
    sleep.assert_called_once()


def test_bounded_map_empty_items():
    called = []
    with (
        mock.patch("ytextract.resource_monitor.psutil.cpu_percent", return_value=10.0),
        mock.patch("ytextract.resource_monitor.time.sleep"),
    ):
        result = bounded_map(lambda x: called.append(x) or x, [], CFG)
    assert result == []
    assert called == []


def test_bounded_map_preserves_order_and_throttles():
    with (
        mock.patch("ytextract.resource_monitor.psutil.cpu_percent", return_value=95.0),
        mock.patch("ytextract.resource_monitor.time.sleep") as sleep,
    ):
        result = bounded_map(lambda x: x * 2, [1, 2, 3], CFG)
    assert result == [2, 4, 6]
    assert sleep.call_count == 3  # one wait_if_busy per item


def test_bounded_map_propagates_exception():
    def boom(_):
        raise ValueError("nope")

    with (
        mock.patch("ytextract.resource_monitor.psutil.cpu_percent", return_value=10.0),
        mock.patch("ytextract.resource_monitor.time.sleep"),
        mock.patch("ytextract.resource_monitor.ThreadPoolExecutor") as pool_cls,
    ):
        pool_cls.return_value.__enter__.return_value.map.side_effect = ValueError(
            "nope"
        )
        with pytest.raises(ValueError, match="nope"):
            bounded_map(boom, [1], CFG)
