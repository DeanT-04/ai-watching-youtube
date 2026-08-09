"""Tests for logging_setup.py."""

import logging

import pytest

from ytextract.logging_setup import setup_logging


def test_setup_returns_ytextract_logger():
    logger = setup_logging()
    assert logger.name == "ytextract"
    assert logger.level == logging.INFO


def test_setup_is_idempotent():
    first = setup_logging("INFO")
    before = len(first.handlers)
    second = setup_logging("DEBUG")
    assert first is second
    assert len(second.handlers) == before  # no handler duplicated
    assert first.level == logging.DEBUG
    assert first.propagate is False


def test_invalid_level_raises():
    with pytest.raises(ValueError):
        setup_logging("NOT-A-LEVEL")
