"""One logging setup, used everywhere — no scattered print() calls."""

from __future__ import annotations

import logging

_FORMAT = "%(asctime)s %(levelname)s %(name)s: %(message)s"
_handler_installed = False


def setup_logging(level: str = "INFO") -> logging.Logger:
    """Configure the ``ytextract`` logger once and return it (idempotent).

    Calling this repeatedly (e.g. from tests or the CLI entrypoint) never
    duplicates handlers. A module-level flag (not ``logger.handlers``) is
    the guard because other tooling (e.g. pytest's logging plugin) may
    attach its own handlers to the logger between calls.
    """
    global _handler_installed
    logger = logging.getLogger("ytextract")
    if not _handler_installed:
        handler = logging.StreamHandler()
        handler.setFormatter(logging.Formatter(_FORMAT))
        logger.addHandler(handler)
        _handler_installed = True
    logger.setLevel(level.upper())
    logger.propagate = False
    return logger
