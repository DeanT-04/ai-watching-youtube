"""CLI orchestration: one command runs Phases 2-8 in order.

Progress is reported through the Phase 1 logger. OCR over keyframes is
sequential in v1 (one PaddleOCR model instance, CPU throttled inside
``run_ocr``); multi-frame/threaded OCR is explicitly future work.
"""

from __future__ import annotations

import argparse
import logging
import sys

from .config import Config
from .crop import preprocess
from .downloader import download_video
from .keyframes import Frame, sample_and_select
from .logging_setup import setup_logging
from .ocr import OcrEngine, OcrResult, create_engine, run_ocr
from .repair import repair
from .storage import CodeBlock, VideoResult, save_result
from .transcriber import transcribe

logger = logging.getLogger("ytextract.cli")

# Phase 7 validates Python; more languages plug in via repair.VALIDATORS.
OCR_LANGUAGE = "python"


def build_parser() -> argparse.ArgumentParser:
    """Build the CLI argument parser (one positional URL)."""
    parser = argparse.ArgumentParser(
        prog="ytextract",
        description="Extract an accurate transcript + on-screen code from a "
        "YouTube tutorial URL.",
    )
    parser.add_argument("url", help="public YouTube URL to process")
    return parser


def _ocr_keyframe(frame: Frame, cfg: Config, engines: list[OcrEngine]) -> OcrResult:
    """Crop/upscale one keyframe, then run the OCR ensemble over it."""
    prepared = preprocess(frame.image, cfg)
    return run_ocr(prepared, cfg, engines=engines)


def _to_code_block(frame: Frame, ocr: OcrResult) -> CodeBlock:
    """Assemble OCR lines into a syntax-checked code block."""
    text = "\n".join(line.text for line in ocr.lines)
    checked = repair(text, OCR_LANGUAGE)
    return CodeBlock(
        timestamp=frame.timestamp,
        text=checked.code,
        language=OCR_LANGUAGE,
        source=ocr.engine,
        valid=bool(checked.valid),
        issues=checked.issues,
    )


def pipeline(url: str, cfg: Config) -> VideoResult:
    """Run download → transcribe → keyframes → OCR → repair → store for ``url``."""
    logger.info("phase 2/8: downloading %s", url)
    media = download_video(url, cfg)

    logger.info("phase 3/8: transcribing audio")
    transcript = transcribe(media.audio_path, cfg)

    logger.info("phase 4/8: selecting keyframes")
    frames = sample_and_select(str(media.video_path), cfg)
    logger.info("phase 4/8: %d keyframes selected", len(frames))

    logger.info("phase 5-6/8: OCR over %d keyframes", len(frames))
    code_blocks: list[CodeBlock] = []
    # Engines are built once and reused across frames: constructing PaddleOCR
    # per frame would redo graph init (and re-trigger model downloads) each time.
    ocr_engines = [create_engine(name) for name in ("paddle", "tesseract")]
    for frame in frames:
        code_blocks.append(
            _to_code_block(frame, _ocr_keyframe(frame, cfg, ocr_engines))
        )

    logger.info("phase 7/8: validating %d code blocks", len(code_blocks))
    result = VideoResult(
        video_id=media.metadata.video_id,
        title=media.metadata.title,
        uploader=media.metadata.uploader,
        webpage_url=media.metadata.webpage_url,
        transcript_language=transcript.language,
        segments=transcript.segments,
        code_blocks=code_blocks,
    )

    logger.info("phase 8/8: saving result")
    save_result(result, cfg)
    logger.info("done: %s", media.metadata.video_id)
    return result


def main(argv: list[str] | None = None) -> int:
    """CLI entrypoint: parse args, run the pipeline, return an exit code."""
    cfg = Config.load()
    setup_logging(cfg.log_level)
    args = build_parser().parse_args(argv)
    try:
        result = pipeline(args.url, cfg)
    except Exception as exc:  # noqa: BLE001 - CLI boundary: report and exit 1
        logger.error("pipeline failed: %s", exc)
        return 1
    logger.info(
        "saved %d transcript segments and %d code blocks for %s",
        len(result.segments),
        len(result.code_blocks),
        result.video_id,
    )
    return 0


if (
    __name__ == "__main__"
):  # pragma: no cover - runpy re-execution deadlocks under pytest; smoke-tested via subprocess in tests/test_cli.py
    sys.exit(main())
