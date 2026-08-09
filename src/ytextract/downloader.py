"""Download module: thin wrapper around ``yt-dlp`` + ``ffmpeg``.

Takes a public YouTube URL and produces, under ``data/raw/<video_id>/``:

- the video file (``<video_id>.mp4``, merged best video + audio)
- the audio track (``<video_id>.wav``, 16 kHz mono PCM for Whisper)
- the ``<video_id>.info.json`` metadata file written by ``yt-dlp``

All external calls go through the Phase 1 :func:`run_command` wrapper, so the
unit-test tier mocks one function instead of the real network.
"""

from __future__ import annotations

import json
import re
from dataclasses import dataclass
from pathlib import Path

from .config import Config
from .subprocess_utils import run_command

# 11-char YouTube video id: [A-Za-z0-9_-]
_ID_PATTERN = r"[A-Za-z0-9_-]{11}"
_URL_RE = re.compile(
    rf"(?:youtu\.be/{_ID_PATTERN}|"
    rf"youtube\.com/(?:watch\?v=|embed/|shorts/|v/){_ID_PATTERN})"
)

# Prefer <=1080p video + audio, merged; falls back to whatever single file exists.
_YTDLP_FORMAT = "bv*[height<=1080]+ba/b"


class DownloadError(RuntimeError):
    """Raised when a video cannot be downloaded or its outputs cannot be found."""


@dataclass(frozen=True)
class VideoMetadata:
    """Title/description/uploader as reported by yt-dlp's info JSON."""

    video_id: str
    title: str
    description: str
    uploader: str
    webpage_url: str


@dataclass(frozen=True)
class DownloadedMedia:
    """Local paths + metadata for one downloaded video."""

    video_dir: Path
    video_path: Path
    audio_path: Path
    metadata: VideoMetadata


def extract_video_id(url: str) -> str:
    """Return the 11-char YouTube video id from a supported URL shape."""
    match = _URL_RE.search(url)
    if match is None:
        raise DownloadError(f"could not extract a YouTube video id from: {url}")
    # The id is the last 11 chars of the matched span.
    return match.group(0)[-11:]


def _ytdlp_cmd(url: str, output_template: str) -> list[str]:
    """Build the yt-dlp argv for a single merged video + info JSON."""
    return [
        "yt-dlp",
        "-f",
        _YTDLP_FORMAT,
        "--merge-output-format",
        "mp4",
        "--write-info-json",
        "--no-playlist",
        "-o",
        output_template,
        url,
    ]


def _ffmpeg_audio_cmd(video_path: Path, audio_path: Path) -> list[str]:
    """Build the ffmpeg argv for 16 kHz mono PCM audio extraction."""
    return [
        "ffmpeg",
        "-y",
        "-i",
        str(video_path),
        "-vn",
        "-ac",
        "1",
        "-ar",
        "16000",
        "-c:a",
        "pcm_s16le",
        str(audio_path),
    ]


def _load_metadata(info_json: Path) -> VideoMetadata:
    """Read title/description/uploader from yt-dlp's ``*.info.json``."""
    data = json.loads(info_json.read_text(encoding="utf-8"))
    return VideoMetadata(
        video_id=str(data["id"]),
        title=str(data.get("title", "")),
        description=str(data.get("description", "")),
        uploader=str(data.get("uploader", "")),
        webpage_url=str(data.get("webpage_url", "")),
    )


def _find_video_file(outdir: Path, video_id: str) -> Path:
    """Locate the merged video file, preferring .mp4."""
    candidates = sorted(outdir.glob(f"{video_id}.*"))
    for path in candidates:
        if path.name.endswith((".mp4", ".mkv", ".webm")):
            return path
    raise DownloadError(
        f"yt-dlp finished but no video file was found in {outdir} "
        f"(expected {video_id}.mp4)"
    )


def download_video(url: str, cfg: Config) -> DownloadedMedia:
    """Download ``url`` into ``cfg.raw_dir/<video_id>/`` and return paths + metadata."""
    video_id = extract_video_id(url)
    outdir = cfg.raw_dir / video_id
    outdir.mkdir(parents=True, exist_ok=True)

    run_command(
        _ytdlp_cmd(url, str(outdir / f"{video_id}.%(ext)s")),
        timeout=cfg.ytdlp_timeout_seconds,
    )

    video_path = _find_video_file(outdir, video_id)
    audio_path = outdir / f"{video_id}.wav"
    run_command(
        _ffmpeg_audio_cmd(video_path, audio_path),
        timeout=cfg.ffmpeg_timeout_seconds,
    )

    metadata = _load_metadata(outdir / f"{video_id}.info.json")
    return DownloadedMedia(
        video_dir=outdir,
        video_path=video_path,
        audio_path=audio_path,
        metadata=metadata,
    )
