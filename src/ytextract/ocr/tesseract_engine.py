"""Tesseract engine (secondary), invoked through the Phase 1 subprocess wrapper.

The image is written to a temp PNG and ``tesseract`` is run with the ``tsv``
output mode (per-word confidence). :func:`parse_tsv` reconstructs lines by
grouping words on ``block/paragraph/line``; it is pure and unit-tested
directly.
"""

from __future__ import annotations

import logging
import tempfile
from pathlib import Path

import cv2
import numpy as np

from ..subprocess_utils import run_command
from .base import OcrText

logger = logging.getLogger("ytextract.ocr.tesseract")

_TSV_HEADER = "level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext"
# Column indexes in tesseract TSV output (header order above).
_COL_BLOCK, _COL_PAR, _COL_LINE, _COL_CONF, _COL_TEXT = 2, 3, 4, 10, 11


def parse_tsv(tsv: str) -> list[OcrText]:
    """Parse tesseract ``tsv`` output into grouped line-level OcrText entries."""
    lines: list[OcrText] = []
    # words grouped by (block, par, line)
    groups: dict[tuple[int, int, int], tuple[list[str], list[float]]] = {}
    for raw in tsv.splitlines():
        parts = raw.split("\t")
        if len(parts) != len(_TSV_HEADER.split("\t")) or parts[0] == "level":
            continue
        try:
            conf = float(parts[_COL_CONF])
            text = parts[_COL_TEXT]
            key = (int(parts[_COL_BLOCK]), int(parts[_COL_PAR]), int(parts[_COL_LINE]))
        except ValueError:
            continue
        if conf < 0 or not text.strip():
            continue
        words, confs = groups.setdefault(key, ([], []))
        words.append(text.strip())
        confs.append(conf)
    for key in sorted(groups):
        words, confs = groups[key]
        lines.append(OcrText(text=" ".join(words), confidence=sum(confs) / len(confs)))
    return lines


class TesseractEngine:
    """Tesseract-based engine via ``run_command``; ``psm`` defaults to 6 (block)."""

    name = "tesseract"

    def __init__(self, psm: str = "6") -> None:
        self._psm = psm

    def ocr(self, image: np.ndarray) -> list[OcrText]:
        with tempfile.TemporaryDirectory() as tmp:
            png = Path(tmp) / "frame.png"
            cv2.imwrite(str(png), image)
            result = run_command(
                ["tesseract", str(png), "stdout", "--psm", self._psm, "tsv"],
                check=False,
            )
        return parse_tsv(result.stdout)
