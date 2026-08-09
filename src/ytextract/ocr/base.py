"""Shared OCR types and the engine interface.

A third OCR engine can be added by implementing :class:`OcrEngine` and
registering it in :func:`ytextract.ocr.ensemble.create_engine` — callers only
ever see :class:`OcrText` / :class:`OcrResult`.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Protocol, runtime_checkable

import numpy as np


class OcrError(RuntimeError):
    """Raised when every configured OCR engine fails or none is configured."""


@dataclass(frozen=True)
class OcrText:
    """One recognized line of text with its engine confidence (0..1)."""

    text: str
    confidence: float


@dataclass(frozen=True)
class OcrResult:
    """All recognized lines for one image, plus the engine(s) that produced them."""

    engine: str
    lines: list[OcrText]


@runtime_checkable
class OcrEngine(Protocol):
    """Common interface implemented by every OCR engine."""

    name: str

    def ocr(self, image: np.ndarray) -> list[OcrText]:
        """Recognize text lines in ``image``; never raises for empty results."""
        ...
