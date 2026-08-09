"""Shared fixtures for the ytextract test suite."""

from unittest import mock

import pytest


@pytest.fixture
def no_dotenv():
    """Keep Config.load() from touching any real .env file on disk."""
    with mock.patch("ytextract.config.load_dotenv"):
        yield
