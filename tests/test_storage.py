"""Tests for storage.py — tmp_path filesystem, no network."""

import json

import pytest

from ytextract.config import Config
from ytextract.repair import RepairIssue
from ytextract.storage import (
    CodeBlock,
    LocalStorage,
    Storage,
    StorageError,
    VideoResult,
    load_result,
    save_result,
)
from ytextract.transcriber import TranscriptSegment

CFG = Config.load(env={"YTEXTRACT_MAX_WORKERS": "2"})


def _result(video_id="abc"):
    return VideoResult(
        video_id=video_id,
        title="My Tutorial",
        uploader="Channel",
        webpage_url=f"https://youtu.be/{video_id}",
        transcript_language="en",
        segments=[
            TranscriptSegment(0.0, 1.5, "hello"),
            TranscriptSegment(1.5, 3.0, "world"),
        ],
        code_blocks=[
            CodeBlock(
                timestamp=2.0,
                text="def main():\n    pass",
                language="python",
                source="paddle+tesseract",
                valid=True,
                issues=[],
            ),
            CodeBlock(
                timestamp=10.0,
                text="x =",
                language="python",
                source="paddle",
                valid=False,
                issues=[RepairIssue(line=1, message="invalid syntax (line 1)")],
            ),
        ],
    )


def test_local_storage_round_trip(tmp_path, no_dotenv):
    cfg = Config.load(
        env={"YTEXTRACT_DATA_DIR": str(tmp_path), "YTEXTRACT_MAX_WORKERS": "2"}
    )
    storage = LocalStorage(cfg)
    outdir = storage.save_result(_result())
    assert outdir == cfg.output_dir / "abc"
    assert (outdir / "transcript.json").exists()
    assert (outdir / "code_blocks.json").exists()

    loaded = storage.load_result("abc")
    assert loaded == _result()


def test_module_level_save_and_load(tmp_path, no_dotenv):
    cfg = Config.load(
        env={"YTEXTRACT_DATA_DIR": str(tmp_path), "YTEXTRACT_MAX_WORKERS": "2"}
    )
    save_result(_result(), cfg)
    assert load_result("abc", cfg) == _result()


def test_save_creates_nested_directories(tmp_path, no_dotenv):
    cfg = Config.load(env={"YTEXTRACT_DATA_DIR": str(tmp_path / "deep" / "nested")})
    outdir = save_result(_result(), cfg)
    assert outdir.is_dir()


def test_save_uses_utf8_for_non_ascii(tmp_path, no_dotenv):
    cfg = Config.load(env={"YTEXTRACT_DATA_DIR": str(tmp_path)})
    result = _result()
    result = VideoResult(**{**result.__dict__, "title": "日本語タイトル"})
    save_result(result, cfg)
    raw = (cfg.output_dir / "abc" / "transcript.json").read_text(encoding="utf-8")
    assert "日本語タイトル" in raw


def test_load_missing_video_raises(tmp_path, no_dotenv):
    cfg = Config.load(env={"YTEXTRACT_DATA_DIR": str(tmp_path)})
    with pytest.raises(StorageError, match="no readable result"):
        load_result("missing", cfg)


def test_load_corrupt_json_raises(tmp_path, no_dotenv):
    cfg = Config.load(env={"YTEXTRACT_DATA_DIR": str(tmp_path)})
    outdir = cfg.output_dir / "abc"
    outdir.mkdir(parents=True)
    (outdir / "transcript.json").write_text("{not json", encoding="utf-8")
    (outdir / "code_blocks.json").write_text("{}", encoding="utf-8")
    with pytest.raises(StorageError, match="no readable result"):
        load_result("abc", cfg)


def test_load_missing_code_blocks_raises(tmp_path, no_dotenv):
    cfg = Config.load(env={"YTEXTRACT_DATA_DIR": str(tmp_path)})
    outdir = cfg.output_dir / "abc"
    outdir.mkdir(parents=True)
    (outdir / "transcript.json").write_text(
        json.dumps({"video_id": "abc", "segments": []}), encoding="utf-8"
    )
    with pytest.raises(StorageError, match="no readable result"):
        load_result("abc", cfg)


def test_load_missing_key_raises(tmp_path, no_dotenv):
    cfg = Config.load(env={"YTEXTRACT_DATA_DIR": str(tmp_path)})
    outdir = cfg.output_dir / "abc"
    outdir.mkdir(parents=True)
    (outdir / "transcript.json").write_text(
        json.dumps({"no": "segments key"}), encoding="utf-8"
    )
    (outdir / "code_blocks.json").write_text("{}", encoding="utf-8")
    with pytest.raises(StorageError, match="no readable result"):
        load_result("abc", cfg)


def test_storage_protocol_is_satisfied():
    assert isinstance(LocalStorage(CFG), Storage)


def test_result_with_no_code_blocks(tmp_path, no_dotenv):
    cfg = Config.load(env={"YTEXTRACT_DATA_DIR": str(tmp_path)})
    result = _result()
    result = VideoResult(
        video_id=result.video_id,
        title=result.title,
        uploader=result.uploader,
        webpage_url=result.webpage_url,
        transcript_language=result.transcript_language,
        segments=result.segments,
        code_blocks=[],
    )
    save_result(result, cfg)
    loaded = load_result("abc", cfg)
    assert loaded.code_blocks == []
