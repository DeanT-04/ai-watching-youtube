"""Tests for cli.py — every external call (download, whisper, OCR) is mocked;
the full pipeline wiring is what gets verified."""

import subprocess
import sys
from pathlib import Path
from unittest import mock

import numpy as np
import pytest

from ytextract.cli import (
    OCR_LANGUAGE,
    _to_code_block,
    _to_code_block_group,
    build_parser,
    main,
    pipeline,
)
from ytextract.config import Config
from ytextract.downloader import DownloadedMedia, VideoMetadata
from ytextract.keyframes import Frame
from ytextract.ocr import OcrResult, OcrText
from ytextract.storage import CodeBlock, VideoResult
from ytextract.transcriber import Transcript, TranscriptSegment

URL = "https://youtu.be/abc123def45"


def _media(video_id="abc123def45"):
    return DownloadedMedia(
        video_dir=Path("vdir"),
        video_path=Path("vdir/v.mp4"),
        audio_path=Path("vdir/a.wav"),
        metadata=VideoMetadata(
            video_id=video_id,
            title="Tutorial",
            description="desc",
            uploader="Channel",
            webpage_url=URL,
        ),
    )


def _frames():
    return [
        Frame(0.0, np.zeros((8, 8), dtype=np.uint8)),
        Frame(5.0, np.zeros((8, 8), dtype=np.uint8)),
    ]


def _fake_ocr(*args, **kwargs):
    return OcrResult("paddle+tesseract", [OcrText("x = 1", 0.9), OcrText("y = 2", 0.8)])


def _fake_eng():
    eng = mock.Mock()
    eng.name = "fake"
    return eng


def test_build_parser_requires_url():
    parser = build_parser()
    with pytest.raises(SystemExit):
        parser.parse_args([])
    args = parser.parse_args([URL])
    assert args.url == URL


def test_to_code_block_runs_repair():
    block = _to_code_block(Frame(3.0, np.zeros((4, 4), dtype=np.uint8)), _fake_ocr())
    assert isinstance(block, CodeBlock)
    assert block.timestamp == 3.0
    assert block.text == "x = 1\ny = 2"
    assert block.language == OCR_LANGUAGE
    assert block.source == "paddle+tesseract"
    assert block.valid is True
    assert block.issues == []


def test_to_code_block_group_size_one_matches_single_frame_path():
    """A group of one frame must behave identically to the pre-consensus
    single-frame path -- no line numbers to anchor on, nothing to vote
    across, so it should be a pure passthrough."""
    frame = Frame(3.0, np.zeros((4, 4), dtype=np.uint8))
    engines = [_fake_eng()]
    with mock.patch("ytextract.cli.run_ocr", side_effect=_fake_ocr):
        block = _to_code_block_group([frame], Config.load(env={}), engines)
    assert block.timestamp == 3.0
    assert block.text == "x = 1\ny = 2"
    assert block.source == "paddle+tesseract"


def test_to_code_block_group_merges_multiple_frames_via_consensus():
    """The real integration point: multiple frames of the same stable region,
    each OCR'd independently (one has a gutter-digit misread), should merge
    into one clean, more-complete block -- not just repeat one frame's text.
    """
    frames = [Frame(10.0, np.zeros((4, 4), dtype=np.uint8)) for _ in range(3)]
    ocr_results = [
        OcrResult("tesseract", [OcrText("46 struct Foo {", 0.9), OcrText("47 int a;", 0.9)]),
        OcrResult("tesseract", [OcrText("46 struct Foo {", 0.9), OcrText("48 int b;", 0.9)]),
        # third frame's OCR garbled line 47's digit into "boo1"-style noise
        OcrResult("tesseract", [OcrText("46 struct Foo {", 0.9), OcrText("47 int a;", 0.85)]),
    ]
    engines = [_fake_eng()]
    with mock.patch("ytextract.cli.run_ocr", side_effect=ocr_results):
        block = _to_code_block_group(frames, Config.load(env={}), engines)
    assert block.timestamp == 10.0  # first frame's timestamp anchors the block
    assert "struct Foo {" in block.text
    assert "int a;" in block.text
    assert "int b;" in block.text  # recovered from a frame the other two missed
    assert "consensus x3" in block.source


def test_to_code_block_group_falls_back_when_no_line_numbers():
    """No gutter numbers anywhere in the group -> nothing to anchor consensus
    on; fall back to the first frame's reading rather than guessing at merge
    order across unnumbered text.
    """
    frames = [Frame(1.0, np.zeros((4, 4), dtype=np.uint8)) for _ in range(2)]
    ocr_results = [_fake_ocr(), _fake_ocr()]
    engines = [_fake_eng()]
    with mock.patch("ytextract.cli.run_ocr", side_effect=ocr_results):
        block = _to_code_block_group(frames, Config.load(env={}), engines)
    assert block.text == "x = 1\ny = 2"


def _groups():
    return [[f] for f in _frames()]


def test_pipeline_wires_all_phases(tmp_path, no_dotenv):
    cfg = Config.load(
        env={"YTEXTRACT_DATA_DIR": str(tmp_path), "YTEXTRACT_MAX_WORKERS": "2"}
    )
    transcript = Transcript(
        language="en",
        segments=[TranscriptSegment(0.0, 1.5, "hello")],
    )
    with (
        mock.patch("ytextract.cli.download_video", return_value=_media()) as dl,
        mock.patch("ytextract.cli.transcribe", return_value=transcript) as tr,
        mock.patch(
            "ytextract.cli.sample_and_group", return_value=_groups()
        ) as kf,
        mock.patch("ytextract.cli.run_ocr", side_effect=_fake_ocr) as ocr,
        mock.patch(
            "ytextract.cli.create_engine", side_effect=[_fake_eng(), _fake_eng()]
        ) as ce,
    ):
        result = pipeline(URL, cfg)

    assert isinstance(result, VideoResult)
    assert result.video_id == "abc123def45"
    assert result.title == "Tutorial"
    assert result.transcript_language == "en"
    assert len(result.segments) == 1
    assert len(result.code_blocks) == 2  # one per stable-run group
    assert result.code_blocks[0].timestamp == 0.0
    assert result.code_blocks[1].timestamp == 5.0

    dl.assert_called_once_with(URL, cfg)
    tr.assert_called_once()
    kf.assert_called_once_with(str(Path("vdir/v.mp4")), cfg)
    assert ocr.call_count == 2
    # Engines are built once (one per name), not once per keyframe.
    assert ce.call_count == 2

    # Storage was really written (tmp data dir).
    outdir = cfg.output_dir / "abc123def45"
    assert (outdir / "transcript.json").exists()
    assert (outdir / "code_blocks.json").exists()


def test_pipeline_ocr_failure_aborts(tmp_path, no_dotenv):
    cfg = Config.load(env={"YTEXTRACT_DATA_DIR": str(tmp_path)})
    with (
        mock.patch("ytextract.cli.download_video", return_value=_media()),
        mock.patch(
            "ytextract.cli.transcribe",
            return_value=Transcript("en", [TranscriptSegment(0, 1, "hi")]),
        ),
        mock.patch("ytextract.cli.sample_and_group", return_value=_groups()),
        mock.patch(
            "ytextract.cli.run_ocr",
            side_effect=RuntimeError("ocr crashed"),
        ),
        pytest.raises(RuntimeError, match="ocr crashed"),
    ):
        pipeline(URL, cfg)


def _result():
    return VideoResult(
        video_id="abc123def45",
        title="Tutorial",
        uploader="Channel",
        webpage_url=URL,
        transcript_language="en",
        segments=[TranscriptSegment(0, 1, "hi")],
        code_blocks=[],
    )


def test_main_success(no_dotenv):
    with (
        mock.patch("ytextract.cli.pipeline", return_value=_result()) as pl,
        mock.patch("ytextract.cli.setup_logging"),
    ):
        code = main([URL])
    assert code == 0
    pl.assert_called_once()
    assert pl.call_args.args[0] == URL


def test_main_pipeline_error_returns_one(no_dotenv):
    with (
        mock.patch("ytextract.cli.pipeline", side_effect=RuntimeError("boom")),
        mock.patch("ytextract.cli.setup_logging"),
        mock.patch("ytextract.cli.logger") as logger,
    ):
        code = main([URL])
    assert code == 1
    logger.error.assert_called_once()


def test_main_missing_url_exits(no_dotenv):
    with mock.patch("ytextract.cli.setup_logging"), pytest.raises(SystemExit):
        main([])


def test_cli_module_main_block(no_dotenv):
    # `python -m ytextract.cli` entry point wiring: no URL → argparse exits 2,
    # proving the __main__ block resolves and runs main(). (runpy re-execution
    # deadlocks under pytest, so these blocks are smoke-tested via subprocess.)
    proc = subprocess.run(
        [sys.executable, "-m", "ytextract.cli"],
        capture_output=True,
        text=True,
        timeout=60,
        check=False,  # exit code 2 is the expected outcome here
    )
    assert proc.returncode == 2
    assert "the following arguments are required: url" in proc.stderr


def test_package_main_block(no_dotenv):
    # `python -m ytextract` runs ytextract/__main__.py → main() → argparse.
    proc = subprocess.run(
        [sys.executable, "-m", "ytextract"],
        capture_output=True,
        text=True,
        timeout=60,
        check=False,  # exit code 2 is the expected outcome here
    )
    assert proc.returncode == 2
    assert "the following arguments are required: url" in proc.stderr
