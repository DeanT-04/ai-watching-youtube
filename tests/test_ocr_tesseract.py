"""Tests for the Tesseract engine + TSV parsing (tesseract binary not needed)."""

from pathlib import Path
from unittest import mock

import numpy as np

from ytextract.ocr.base import OcrText
from ytextract.ocr.tesseract_engine import TesseractEngine, parse_tsv
from ytextract.subprocess_utils import CommandResult

TSV = (
    "level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext\n"
    "5\t1\t1\t1\t1\t1\t0\t0\t100\t20\t95\tdef\n"
    "5\t1\t1\t1\t1\t2\t100\t0\t50\t20\t90\thello\n"
    "5\t1\t1\t1\t2\t1\t0\t20\t200\t20\t-1\t\n"
    "5\t1\t2\t1\t1\t1\t0\t40\t80\t20\t80\tworld\n"
)


def test_parse_tsv_groups_words_into_lines():
    lines = parse_tsv(TSV)
    assert lines == [
        OcrText(text="def hello", confidence=92.5),
        OcrText(text="world", confidence=80.0),
    ]


def test_parse_tsv_empty_input():
    assert parse_tsv("") == []


def test_parse_tsv_header_only():
    assert parse_tsv(TSV.splitlines()[0]) == []


def test_parse_tsv_skips_malformed_rows():
    malformed = (
        TSV
        + "5\t1\t1\t1\t1\t1\t0\t0\t10\t10\tabc\n"  # 11 fields: length check skips
        + "5\t1\t3\t1\t1\t1\t0\t0\t10\t10\tnot-a-number\tword\n"  # conf parse fails
    )
    lines = parse_tsv(malformed)
    assert [l.text for l in lines] == ["def hello", "world"]


def test_tesseract_engine_uses_run_command_and_parses():
    engine = TesseractEngine(psm="6")
    image = np.full((20, 20), 255, dtype=np.uint8)
    with mock.patch(
        "ytextract.ocr.tesseract_engine.run_command",
        return_value=CommandResult(stdout=TSV, stderr="", returncode=0),
    ) as run:
        lines = engine.ocr(image)
    assert [l.text for l in lines] == ["def hello", "world"]
    argv = run.call_args.args[0]
    assert argv[0] == "tesseract"
    assert argv[2] == "stdout"
    assert "--psm" in argv and argv[argv.index("--psm") + 1] == "6"
    assert "tsv" in argv
    assert Path(argv[1]).suffix == ".png"
    assert run.call_args.kwargs["check"] is False


def test_tesseract_engine_warns_on_nonzero_exit():
    engine = TesseractEngine()
    image = np.full((20, 20), 255, dtype=np.uint8)
    with (
        mock.patch(
            "ytextract.ocr.tesseract_engine.run_command",
            return_value=CommandResult(stdout="", stderr="lang error", returncode=1),
        ),
        mock.patch("ytextract.ocr.tesseract_engine.logger") as logger,
    ):
        engine.ocr(image)
    logger.warning.assert_called_once()
