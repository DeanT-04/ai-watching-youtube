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
from .consensus import reconstruct
from .crop import preprocess
from .downloader import download_video
from .keyframes import Frame, sample_and_group
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


def _to_code_block_group(
    group: list[Frame], cfg: Config, engines: list[OcrEngine]
) -> CodeBlock:
    """OCR every frame in a stable-run group, then consensus-merge the result.

    A group of size 1 behaves exactly like the old single-frame path (same
    output for the same input) -- consensus only has something to do once
    there's more than one independent read of the region to vote across.
    """
    results = [_ocr_keyframe(frame, cfg, engines) for frame in group]
    if len(group) == 1:
        return _to_code_block(group[0], results[0])

    merged = reconstruct([result.lines for result in results])
    if merged.lines:
        # Only the anchored (line-numbered) consensus text is used. Trying
        # to append "unanchored" scraps was tested against the real frames
        # and made things worse, not better: most unanchored text turns out
        # to be repeated OCR noise from UI chrome (toolbar labels, etc.)
        # that never carried a gutter number in the first place, not code
        # that consensus failed to anchor. Dropping it is the correct
        # default; unanchored text is still available on the
        # ReconstructedSource if a future caller wants to inspect it.
        text = merged.render()
    else:
        # No gutter numbers detected anywhere in the group (e.g. the crop
        # doesn't include the line-number column) -- nothing to anchor on,
        # so fall back to the first frame's reading in original order. This
        # matches the pre-consensus single-frame behavior exactly rather
        # than guessing at how to merge unordered text across frames.
        text = "\n".join(line.text for line in results[0].lines)

    checked = repair(text, OCR_LANGUAGE)
    # Sources may differ per frame (e.g. paddle on one, tesseract on
    # another); record the set so it's clear the block is a merge.
    sources = sorted({result.engine for result in results})
    return CodeBlock(
        timestamp=group[0].timestamp,
        text=checked.code,
        language=OCR_LANGUAGE,
        source="+".join(sources) + f" (consensus x{len(group)})",
        valid=bool(checked.valid),
        issues=checked.issues,
    )


def pipeline(url: str, cfg: Config) -> VideoResult:
    """Run download → transcribe → keyframes → OCR → repair → store for ``url``."""
    logger.info("phase 2/8: downloading %s", url)
    media = download_video(url, cfg)

    logger.info("phase 3/8: transcribing audio")
    transcript = transcribe(media.audio_path, cfg)

    logger.info("phase 4/8: selecting keyframe groups")
    groups = sample_and_group(str(media.video_path), cfg)
    logger.info("phase 4/8: %d stable-run groups selected", len(groups))

    logger.info("phase 5-6/8: OCR + consensus over %d groups", len(groups))
    code_blocks: list[CodeBlock] = []
    # Engines are built once and reused across frames: constructing PaddleOCR
    # per frame would redo graph init (and re-trigger model downloads) each time.
    ocr_engines = [create_engine(name) for name in ("paddle", "tesseract")]
    for group in groups:
        code_blocks.append(_to_code_block_group(group, cfg, ocr_engines))

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
