"""Tests for the OCR ensemble — engines are faked; merge logic is the target."""

from unittest import mock

import numpy as np
import pytest

from ytextract.config import Config
from ytextract.ocr.base import OcrError, OcrResult, OcrText
from ytextract.ocr.ensemble import create_engine, merge_results, run_ocr
from ytextract.ocr.paddle_engine import PaddleEngine
from ytextract.ocr.tesseract_engine import TesseractEngine

CFG = Config.load(env={"YTEXTRACT_MAX_WORKERS": "2"})


class FakeEngine:
    def __init__(self, name, lines):
        self.name = name
        self._lines = lines
        self.calls = 0

    def ocr(self, image):
        self.calls += 1
        return self._lines


def _lines(*pairs):
    return [OcrText(text=t, confidence=c) for t, c in pairs]


def test_create_engine_known_names():
    assert isinstance(create_engine("paddle"), PaddleEngine)
    assert isinstance(create_engine("tesseract"), TesseractEngine)


def test_create_engine_unknown_raises():
    with pytest.raises(OcrError, match="unknown OCR engine"):
        create_engine("gpt-vision")


def test_merge_results_keeps_primary_always():
    primary = _lines(("import os", 0.3), ("x = 1", 0.9))
    assert merge_results(primary, [], 0.5) == primary


def test_merge_results_drops_low_confidence_secondary():
    primary = _lines(("x = 1", 0.9))
    secondary = _lines(("import os", 0.2))
    assert merge_results(primary, secondary, 0.5) == primary


def test_merge_results_adds_unmatched_high_confidence_secondary():
    primary = _lines(("x = 1", 0.9))
    secondary = _lines(("import os", 0.8))
    result = merge_results(primary, secondary, 0.5)
    assert [l.text for l in result] == ["x = 1", "import os"]


def test_merge_results_drops_normalized_duplicates():
    primary = _lines(("Print ('Hi')", 0.9))
    secondary = _lines(("print ('hi')", 0.95))
    assert merge_results(primary, secondary, 0.5) == primary


def test_merge_results_drops_overlapping_lines():
    primary = _lines(("def main():", 0.9))
    secondary = _lines(("def main", 0.95), ("def", 0.8))
    result = merge_results(primary, secondary, 0.5)
    assert result == primary


def test_merge_results_empty_primary():
    secondary = _lines(("hello", 0.9))
    assert merge_results([], secondary, 0.5) == secondary


def test_run_ocr_merges_two_engines():
    paddle = FakeEngine("paddle", _lines(("x = 1", 0.9)))
    tess = FakeEngine("tesseract", _lines(("import os", 0.8)))
    with (
        mock.patch(
            "ytextract.ocr.ensemble.create_engine",
            side_effect=[paddle, tess],
        ),
        mock.patch("ytextract.ocr.ensemble.wait_if_busy") as busy,
    ):
        result = run_ocr(np.zeros((4, 4), dtype=np.uint8), CFG)
    assert isinstance(result, OcrResult)
    assert result.engine == "paddle+tesseract"
    assert [l.text for l in result.lines] == ["x = 1", "import os"]
    assert busy.call_count == 2
    assert paddle.calls == 1 and tess.calls == 1


def test_run_ocr_primary_failure_falls_back():
    tess = FakeEngine("tesseract", _lines(("survived", 0.9)))
    failing = FakeEngine("paddle", [])
    failing.ocr = mock.Mock(side_effect=RuntimeError("paddle crashed"))
    with (
        mock.patch("ytextract.ocr.ensemble.create_engine", side_effect=[failing, tess]),
        mock.patch("ytextract.ocr.ensemble.wait_if_busy"),
        mock.patch("ytextract.ocr.ensemble.logger") as logger,
    ):
        result = run_ocr(np.zeros((4, 4), dtype=np.uint8), CFG)
    assert result.engine == "tesseract"
    assert [l.text for l in result.lines] == ["survived"]
    logger.warning.assert_called_once()


def test_run_ocr_all_engines_fail_raises():
    failing = FakeEngine("paddle", [])
    failing.ocr = mock.Mock(side_effect=RuntimeError("boom"))
    with (
        mock.patch("ytextract.ocr.ensemble.create_engine", side_effect=[failing]),
        mock.patch("ytextract.ocr.ensemble.wait_if_busy"),
        pytest.raises(OcrError, match="all OCR engines failed"),
    ):
        run_ocr(np.zeros((4, 4), dtype=np.uint8), CFG, engines=("paddle",))


def test_run_ocr_no_engines_raises():
    with (
        mock.patch("ytextract.ocr.ensemble.create_engine"),
        mock.patch("ytextract.ocr.ensemble.wait_if_busy"),
        pytest.raises(OcrError, match="all OCR engines failed"),
    ):
        run_ocr(np.zeros((4, 4), dtype=np.uint8), CFG, engines=())


def test_run_ocr_accepts_engine_instances():
    paddle = FakeEngine("paddle", _lines(("x = 1", 0.9)))
    tess = FakeEngine("tesseract", _lines(("import os", 0.8)))
    with (
        mock.patch("ytextract.ocr.ensemble.create_engine") as create,
        mock.patch("ytextract.ocr.ensemble.wait_if_busy"),
    ):
        result = run_ocr(np.zeros((4, 4), dtype=np.uint8), CFG, engines=[paddle, tess])
    assert result.engine == "paddle+tesseract"
    assert [l.text for l in result.lines] == ["x = 1", "import os"]
    create.assert_not_called()  # instances passed through, no re-construction
    assert paddle.calls == 1 and tess.calls == 1


def test_run_ocr_single_engine_is_primary():
    tess = FakeEngine("tesseract", _lines(("hello", 0.9)))
    with (
        mock.patch("ytextract.ocr.ensemble.create_engine", side_effect=[tess]),
        mock.patch("ytextract.ocr.ensemble.wait_if_busy"),
    ):
        result = run_ocr(np.zeros((4, 4), dtype=np.uint8), CFG, engines=("tesseract",))
    assert result.engine == "tesseract"
    assert [l.text for l in result.lines] == ["hello"]
