"""Tests for downloader.py — yt-dlp/ffmpeg calls are fully mocked."""

import json
from pathlib import Path
from unittest import mock

import pytest

from ytextract.config import Config
from ytextract.downloader import (
    DownloadedMedia,
    DownloadError,
    _ffmpeg_audio_cmd,
    _find_video_file,
    _load_metadata,
    _ytdlp_cmd,
    download_video,
    extract_video_id,
)
from ytextract.subprocess_utils import CommandError, CommandResult

CFG = Config.load(env={"YTEXTRACT_MAX_WORKERS": "2"})


@pytest.mark.parametrize(
    "url,expected",
    [
        ("https://youtu.be/aDWDJrACs7s?si=abc", "aDWDJrACs7s"),
        ("https://www.youtube.com/watch?v=dV6-h17m6Ag&t=10s", "dV6-h17m6Ag"),
        ("https://youtube.com/embed/ruk3gkZGNkc", "ruk3gkZGNkc"),
        ("https://www.youtube.com/shorts/ABCDEFGHIJK", "ABCDEFGHIJK"),
        ("https://www.youtube.com/v/abcdefghijk", "abcdefghijk"),
    ],
)
def test_extract_video_id_supported_shapes(url, expected):
    assert extract_video_id(url) == expected


@pytest.mark.parametrize(
    "url",
    [
        "https://example.com/not-youtube",
        "https://youtu.be/too-short",
        "https://www.youtube.com/watch?v=",
        "",
        "not a url",
    ],
)
def test_extract_video_id_invalid_raises(url):
    with pytest.raises(DownloadError, match="could not extract"):
        extract_video_id(url)


def test_ytdlp_cmd_shape():
    argv = _ytdlp_cmd("https://youtu.be/abc123", r"C:\out\abc123.%(ext)s")
    assert argv[0] == "yt-dlp"
    assert "--write-info-json" in argv
    assert "--no-playlist" in argv
    assert argv[argv.index("-o") + 1] == r"C:\out\abc123.%(ext)s"
    assert argv[-1] == "https://youtu.be/abc123"


def test_ffmpeg_audio_cmd_shape():
    argv = _ffmpeg_audio_cmd(Path("v.mp4"), Path("a.wav"))
    assert argv[0] == "ffmpeg"
    assert argv[-1] == "a.wav"
    assert "-ar" in argv and argv[argv.index("-ar") + 1] == "16000"


def test_load_metadata_reads_info_json(tmp_path):
    info = tmp_path / "abc.info.json"
    info.write_text(
        json.dumps(
            {
                "id": "abc",
                "title": "My Title",
                "description": "Desc",
                "uploader": "Uploader",
                "webpage_url": "https://youtu.be/abc",
            }
        ),
        encoding="utf-8",
    )
    meta = _load_metadata(info)
    assert meta.video_id == "abc"
    assert meta.title == "My Title"
    assert meta.description == "Desc"
    assert meta.uploader == "Uploader"
    assert meta.webpage_url == "https://youtu.be/abc"


def test_find_video_file_prefers_mp4(tmp_path):
    (tmp_path / "abc.webm").write_bytes(b"x")
    (tmp_path / "abc.mp4").write_bytes(b"x")
    assert _find_video_file(tmp_path, "abc").name == "abc.mp4"


def test_find_video_file_accepts_mkv(tmp_path):
    (tmp_path / "abc.mkv").write_bytes(b"x")
    assert _find_video_file(tmp_path, "abc").name == "abc.mkv"


def test_find_video_file_missing_raises(tmp_path):
    (tmp_path / "abc.info.json").write_text("{}", encoding="utf-8")
    with pytest.raises(DownloadError, match="no video file"):
        _find_video_file(tmp_path, "abc")


def _write_fake_download(outdir: Path, video_id: str) -> None:
    outdir.mkdir(parents=True, exist_ok=True)
    (outdir / f"{video_id}.mp4").write_bytes(b"video")
    info = {
        "id": video_id,
        "title": "T",
        "description": "D",
        "uploader": "U",
        "webpage_url": f"https://youtu.be/{video_id}",
    }
    (outdir / f"{video_id}.info.json").write_text(json.dumps(info), encoding="utf-8")


def test_download_video_success(tmp_path, no_dotenv):
    cfg = Config.load(
        env={"YTEXTRACT_DATA_DIR": str(tmp_path), "YTEXTRACT_MAX_WORKERS": "2"}
    )
    url = "https://youtu.be/aDWDJrACs7s"
    video_id = "aDWDJrACs7s"
    _write_fake_download(cfg.raw_dir / video_id, video_id)

    with mock.patch("ytextract.downloader.run_command") as run:
        media = download_video(url, cfg)

    assert isinstance(media, DownloadedMedia)
    assert media.video_path == cfg.raw_dir / video_id / f"{video_id}.mp4"
    assert media.audio_path == cfg.raw_dir / video_id / f"{video_id}.wav"
    assert media.metadata.title == "T"
    assert media.video_dir == cfg.raw_dir / video_id
    # yt-dlp call then ffmpeg call, with config timeouts.
    calls = run.call_args_list
    assert calls[0].kwargs["timeout"] == cfg.ytdlp_timeout_seconds
    assert calls[1].kwargs["timeout"] == cfg.ffmpeg_timeout_seconds
    assert calls[0].args[0][0] == "yt-dlp"
    assert calls[1].args[0][0] == "ffmpeg"


def test_download_video_ytdlp_failure_propagates(tmp_path, no_dotenv):
    cfg = Config.load(env={"YTEXTRACT_DATA_DIR": str(tmp_path)})
    with (
        mock.patch(
            "ytextract.downloader.run_command",
            side_effect=CommandError("yt-dlp exploded"),
        ),
        pytest.raises(CommandError, match="yt-dlp exploded"),
    ):
        download_video("https://youtu.be/aDWDJrACs7s", cfg)


def test_download_video_invalid_url(tmp_path, no_dotenv):
    cfg = Config.load(env={"YTEXTRACT_DATA_DIR": str(tmp_path)})
    with pytest.raises(DownloadError, match="could not extract"):
        download_video("https://example.com/nope", cfg)


def test_download_video_audio_extraction_failure(tmp_path, no_dotenv):
    cfg = Config.load(env={"YTEXTRACT_DATA_DIR": str(tmp_path)})
    video_id = "aDWDJrACs7s"
    _write_fake_download(cfg.raw_dir / video_id, video_id)

    def fake_run(argv, **kwargs):
        if argv[0] == "yt-dlp":
            return CommandResult("", "", 0)
        raise CommandError("ffmpeg failed")

    with (
        mock.patch("ytextract.downloader.run_command", side_effect=fake_run),
        pytest.raises(CommandError, match="ffmpeg failed"),
    ):
        download_video("https://youtu.be/aDWDJrACs7s", cfg)
