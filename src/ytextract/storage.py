"""Local storage layer for pipeline output.

One directory per video under ``data/output/<video_id>/`` containing
``transcript.json`` and ``code_blocks.json``. The :class:`Storage` protocol is
the repository-style interface — swapping local files for a real database later
means implementing :class:`Storage` again, not touching callers.
"""

from __future__ import annotations

import json
import logging
from dataclasses import asdict, dataclass, field
from pathlib import Path
from typing import Protocol, runtime_checkable

from .config import Config
from .repair import RepairIssue
from .transcriber import TranscriptSegment

logger = logging.getLogger("ytextract.storage")

_TRANSCRIPT_FILE = "transcript.json"
_CODE_BLOCKS_FILE = "code_blocks.json"


class StorageError(RuntimeError):
    """Raised when saved output is missing or unreadable."""


@dataclass(frozen=True)
class CodeBlock:
    """One OCR'd code region with its on-screen timestamp."""

    timestamp: float
    text: str
    language: str
    source: str
    repaired: bool
    issues: list[RepairIssue] = field(default_factory=list)


@dataclass(frozen=True)
class VideoResult:
    """Everything Phase 9 persists for one video."""

    video_id: str
    title: str
    uploader: str
    webpage_url: str
    transcript_language: str | None
    segments: list[TranscriptSegment]
    code_blocks: list[CodeBlock]


@runtime_checkable
class Storage(Protocol):
    """Repository-style interface for persisting pipeline results."""

    def save_result(self, result: VideoResult) -> Path:
        """Persist ``result`` and return the directory it was written to."""
        ...

    def load_result(self, video_id: str) -> VideoResult:
        """Load a previously saved result for ``video_id``."""
        ...


def _segment_to_dict(seg: TranscriptSegment) -> dict:
    return {"start": seg.start, "end": seg.end, "text": seg.text}


def _segment_from_dict(data: dict) -> TranscriptSegment:
    return TranscriptSegment(
        start=float(data["start"]), end=float(data["end"]), text=str(data["text"])
    )


def _block_to_dict(block: CodeBlock) -> dict:
    return {
        "timestamp": block.timestamp,
        "text": block.text,
        "language": block.language,
        "source": block.source,
        "repaired": block.repaired,
        "issues": [asdict(issue) for issue in block.issues],
    }


def _block_from_dict(data: dict) -> CodeBlock:
    issues = [
        RepairIssue(line=item.get("line"), message=str(item["message"]))
        for item in data.get("issues", [])
    ]
    return CodeBlock(
        timestamp=float(data["timestamp"]),
        text=str(data["text"]),
        language=str(data["language"]),
        source=str(data["source"]),
        repaired=bool(data["repaired"]),
        issues=issues,
    )


class LocalStorage:
    """File-backed :class:`Storage` implementation under ``cfg.output_dir``."""

    def __init__(self, cfg: Config) -> None:
        self._output_dir = cfg.output_dir

    def save_result(self, result: VideoResult) -> Path:
        outdir = self._output_dir / result.video_id
        outdir.mkdir(parents=True, exist_ok=True)

        transcript = {
            "video_id": result.video_id,
            "title": result.title,
            "uploader": result.uploader,
            "webpage_url": result.webpage_url,
            "language": result.transcript_language,
            "segments": [_segment_to_dict(s) for s in result.segments],
        }
        (outdir / _TRANSCRIPT_FILE).write_text(
            json.dumps(transcript, indent=2, ensure_ascii=False), encoding="utf-8"
        )

        code_blocks = {
            "video_id": result.video_id,
            "blocks": [_block_to_dict(b) for b in result.code_blocks],
        }
        (outdir / _CODE_BLOCKS_FILE).write_text(
            json.dumps(code_blocks, indent=2, ensure_ascii=False), encoding="utf-8"
        )
        logger.info("saved result for %s to %s", result.video_id, outdir)
        return outdir

    def load_result(self, video_id: str) -> VideoResult:
        outdir = self._output_dir / video_id
        try:
            transcript = json.loads(
                (outdir / _TRANSCRIPT_FILE).read_text(encoding="utf-8")
            )
            code_blocks = json.loads(
                (outdir / _CODE_BLOCKS_FILE).read_text(encoding="utf-8")
            )
        except (FileNotFoundError, json.JSONDecodeError) as exc:
            raise StorageError(
                f"no readable result for video {video_id!r} in {outdir}"
            ) from exc
        return VideoResult(
            video_id=str(transcript["video_id"]),
            title=str(transcript.get("title", "")),
            uploader=str(transcript.get("uploader", "")),
            webpage_url=str(transcript.get("webpage_url", "")),
            transcript_language=transcript.get("language"),
            segments=[_segment_from_dict(s) for s in transcript["segments"]],
            code_blocks=[_block_from_dict(b) for b in code_blocks["blocks"]],
        )


def save_result(result: VideoResult, cfg: Config) -> Path:
    """Convenience wrapper: persist ``result`` under ``cfg.output_dir``."""
    return LocalStorage(cfg).save_result(result)


def load_result(video_id: str, cfg: Config) -> VideoResult:
    """Convenience wrapper: load ``video_id`` from ``cfg.output_dir``."""
    return LocalStorage(cfg).load_result(video_id)
