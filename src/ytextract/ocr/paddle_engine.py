"""PaddleOCR engine (primary).

Targets the PaddleOCR 2.x API: ``PaddleOCR(use_angle_cls, lang, show_log)``
then ``ocr.ocr(image, cls=True)`` returning ``[[ [box, (text, conf)], ... ]]``.
Paddle and its model downloads are heavy, so the import + model construction
are lazy (first ``ocr()`` call).
"""

from __future__ import annotations

import logging

import numpy as np

from .base import OcrText

logger = logging.getLogger("ytextract.ocr.paddle")


class PaddleEngine:
    """PaddleOCR-based engine; ``lang`` defaults to English."""

    name = "paddle"

    def __init__(self, lang: str = "en") -> None:
        self._lang = lang
        self._model: object | None = None

    def _get_model(self) -> object:
        if self._model is None:
            # Imported lazily: paddleocr pulls in paddle (~seconds to import).
            from paddleocr import PaddleOCR

            self._model = PaddleOCR(use_angle_cls=True, lang=self._lang, show_log=False)
        return self._model

    def ocr(self, image: np.ndarray) -> list[OcrText]:
        result = self._get_model().ocr(image, cls=True)
        if result is None:
            return []
        lines: list[OcrText] = []
        for page in result:
            if page is None:
                continue
            for item in page:
                _box, (text, conf) = item
                text = str(text).strip()
                if text:
                    lines.append(OcrText(text=text, confidence=float(conf)))
        return lines
