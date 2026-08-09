"""Audio transcription via ``faster-whisper`` (CPU + int8 by default).

Takes a local audio path and returns a timestamped transcript. The heavy
``faster_whisper`` import is deferred until :func:`transcribe` is actually
called so the module stays importable in test contexts without loading
ctranslate2.
"""

from __future__ import annotations

import logging
from dataclasses import dataclass, field
from pathlib import Path

from .config import Config

logger = logging.getLogger("ytextract.transcriber")


class TranscriptionError(RuntimeError):
    """Raised when the Whisper model cannot be loaded or run."""


@dataclass(frozen=True)
class TranscriptSegment:
    """One transcribed utterance with its time span (seconds)."""

    start: float
    end: float
    text: str


@dataclass(frozen=True)
class Transcript:
    """Timestamped transcript plus the detected (or requested) language."""

    language: str | None
    segments: list[TranscriptSegment] = field(default_factory=list)


def transcribe(audio_path: Path, cfg: Config) -> Transcript:
    """Transcribe ``audio_path`` with the configured Whisper model."""
    try:
        # Imported lazily: faster-whisper drags in ctranslate2/av (~1s+ import).
        from faster_whisper import WhisperModel

        model = WhisperModel(
            cfg.whisper_model_size,
            device=cfg.whisper_device,
            compute_type=cfg.whisper_compute_type,
        )
        segments_iter, info = model.transcribe(
            str(audio_path), language=cfg.whisper_language
        )
        segments = [
            TranscriptSegment(start=float(seg.start), end=float(seg.end), text=seg.text)
            for seg in segments_iter
        ]
    except Exception as exc:
        raise TranscriptionError(
            f"whisper transcription failed for {audio_path}: {exc}"
        ) from exc

    language = getattr(info, "language", None)
    logger.info(
        "transcribed %s: %d segments, language=%s",
        audio_path.name,
        len(segments),
        language,
    )
    return Transcript(language=str(language) if language else None, segments=segments)
