"""OCR ensemble: run configured engines, merge their line lists.

Merge policy (documented, deliberately simple):

- every primary line is kept as-is (low-confidence primary text is still
  valuable for the Phase 7 repair step);
- a secondary line is added only when its confidence meets the configured
  threshold AND no primary line already covers it (case-insensitive
  normalized equality or one-contains-the-other).
"""

from __future__ import annotations

import logging
import re
from collections.abc import Sequence

import numpy as np

from ..config import Config
from ..resource_monitor import wait_if_busy
from .base import OcrEngine, OcrError, OcrResult, OcrText
from .paddle_engine import PaddleEngine
from .tesseract_engine import TesseractEngine

logger = logging.getLogger("ytextract.ocr.ensemble")

_WHITESPACE = re.compile(r"\s+")


def create_engine(name: str) -> OcrEngine:
    """Return an engine instance by name; unknown names raise :class:`OcrError`."""
    registry: dict[str, type] = {
        "paddle": PaddleEngine,
        "tesseract": TesseractEngine,
    }
    try:
        return registry[name]()
    except KeyError as exc:
        raise OcrError(f"unknown OCR engine: {name!r}") from exc


def _normalize(text: str) -> str:
    return _WHITESPACE.sub(" ", text).strip().lower()


def merge_results(
    primary: Sequence[OcrText],
    secondary: Sequence[OcrText],
    confidence_threshold: float,
) -> list[OcrText]:
    """Merge primary + secondary line lists per the policy above."""
    merged: list[OcrText] = list(primary)
    seen: set[str] = {_normalize(t.text) for t in primary}
    for line in secondary:
        if line.confidence < confidence_threshold:
            continue
        key = _normalize(line.text)
        if not key or key in seen:
            continue
        if any(key in s or s in key for s in seen):
            continue
        merged.append(line)
        seen.add(key)
    return merged


def run_ocr(
    image: np.ndarray,
    cfg: Config,
    engines: Sequence[str] = ("paddle", "tesseract"),
) -> OcrResult:
    """Run the named engines over ``image`` and merge their outputs.

    Engine failures are logged and skipped; if every engine fails an
    :class:`OcrError` is raised.
    """
    by_engine: dict[str, list[OcrText]] = {}
    errors: list[str] = []
    for name in engines:
        engine = create_engine(name)
        wait_if_busy(cfg)
        try:
            by_engine[name] = engine.ocr(image)
        except Exception as exc:  # noqa: BLE001 - engine failures must not kill the pipeline
            logger.warning("OCR engine %s failed: %s", name, exc)
            errors.append(f"{name}: {exc}")

    if not by_engine:
        raise OcrError(f"all OCR engines failed: {'; '.join(errors)}")

    primary = by_engine.get(engines[0], [])
    secondary = [
        line
        for name, lines in by_engine.items()
        if name != engines[0]
        for line in lines
    ]
    merged = merge_results(primary, secondary, cfg.ocr_confidence_threshold)
    return OcrResult(engine="+".join(by_engine), lines=merged)
