"""Tests for the PaddleOCR engine (paddle itself never imported)."""

import sys
import types
from unittest import mock

import numpy as np

from ytextract.ocr.base import OcrText
from ytextract.ocr.paddle_engine import PaddleEngine


def test_ocr_none_result_returns_empty():
    engine = PaddleEngine()
    engine._model = mock.Mock(ocr=mock.Mock(return_value=None))
    assert engine.ocr(np.zeros((4, 4), dtype=np.uint8)) == []


def test_ocr_parses_pages_and_skips_none():
    engine = PaddleEngine()
    result = [
        [
            [[[0, 0], [1, 0], [1, 1], [0, 1]], ("print('hi')", 0.98)],
            [[[0, 0], [1, 0], [1, 1], [0, 1]], ("", 0.5)],
        ],
        None,
    ]
    engine._model = mock.Mock(ocr=mock.Mock(return_value=result))
    lines = engine.ocr(np.zeros((4, 4), dtype=np.uint8))
    assert lines == [OcrText(text="print('hi')", confidence=0.98)]


def test_get_model_constructs_lazily_and_caches():
    fake_model = mock.Mock(ocr=mock.Mock(return_value=None))
    fake_cls = mock.Mock(return_value=fake_model)
    fake_module = types.SimpleNamespace(PaddleOCR=fake_cls)
    engine = PaddleEngine(lang="en")
    with mock.patch.dict(sys.modules, {"paddleocr": fake_module}):
        first = engine._get_model()
        second = engine._get_model()
    assert first is fake_model
    assert second is fake_model
    fake_cls.assert_called_once_with(use_angle_cls=True, lang="en", show_log=False)


def test_ocr_through_lazy_model_load():
    fake_model = mock.Mock(
        ocr=mock.Mock(
            return_value=[[[[[0, 0], [1, 0], [1, 1], [0, 1]], ("x = 1", 0.9)]]]
        )
    )
    fake_module = types.SimpleNamespace(PaddleOCR=mock.Mock(return_value=fake_model))
    engine = PaddleEngine()
    with mock.patch.dict(sys.modules, {"paddleocr": fake_module}):
        lines = engine.ocr(np.zeros((4, 4), dtype=np.uint8))
    assert lines == [OcrText(text="x = 1", confidence=0.9)]
