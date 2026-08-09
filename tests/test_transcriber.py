"""Tests for transcriber.py — faster_whisper is faked via sys.modules."""

import sys
import types
from pathlib import Path
from unittest import mock

import pytest

from ytextract.config import Config
from ytextract.transcriber import (
    Transcript,
    TranscriptionError,
    TranscriptSegment,
    transcribe,
)

CFG = Config.load(env={"YTEXTRACT_MAX_WORKERS": "2"})


def _fake_segment(start, end, text):
    return types.SimpleNamespace(start=start, end=end, text=text)


def _patch_faster_whisper(whisper_model_cls):
    """Swap the real faster_whisper module for a fake one in sys.modules."""
    fake_module = types.SimpleNamespace(WhisperModel=whisper_model_cls)
    return mock.patch.dict(sys.modules, {"faster_whisper": fake_module})


@pytest.fixture
def fake_whisper():
    """Install a fake faster_whisper; returns the fake module."""
    fake_model = types.SimpleNamespace(
        transcribe=mock.Mock(
            return_value=(
                iter(
                    [
                        _fake_segment(0.0, 1.5, "hello world"),
                        _fake_segment(1.5, 3.0, "second line"),
                    ]
                ),
                types.SimpleNamespace(language="en"),
            )
        )
    )
    fake_module = types.SimpleNamespace(WhisperModel=mock.Mock(return_value=fake_model))
    with _patch_faster_whisper(fake_module.WhisperModel):
        yield fake_module


def test_transcribe_returns_timestamped_segments(fake_whisper):
    result = transcribe(Path("does-not-matter.wav"), CFG)
    assert isinstance(result, Transcript)
    assert result.language == "en"
    assert result.segments == [
        TranscriptSegment(0.0, 1.5, "hello world"),
        TranscriptSegment(1.5, 3.0, "second line"),
    ]
    # Config values forwarded to the model constructor.
    fake_whisper.WhisperModel.assert_called_once_with(
        CFG.whisper_model_size, device="cpu", compute_type="int8"
    )
    # Audio path + language forwarded to transcribe.
    fake_whisper.WhisperModel.return_value.transcribe.assert_called_once_with(
        "does-not-matter.wav", language=None
    )


def test_transcribe_forwards_language(fake_whisper):
    cfg = Config.load(
        env={"YTEXTRACT_WHISPER_LANGUAGE": "ja", "YTEXTRACT_MAX_WORKERS": "2"}
    )
    transcribe(Path("a.wav"), cfg)
    fake_whisper.WhisperModel.return_value.transcribe.assert_called_once_with(
        "a.wav", language="ja"
    )


def test_transcribe_model_load_failure_raises():
    failing_cls = mock.Mock(side_effect=RuntimeError("model download failed"))
    with (
        _patch_faster_whisper(failing_cls),
        pytest.raises(TranscriptionError, match="model download failed"),
    ):
        transcribe(Path("a.wav"), CFG)


def test_transcribe_run_failure_raises():
    fake_model = types.SimpleNamespace(
        transcribe=mock.Mock(side_effect=RuntimeError("decode error"))
    )
    with (
        _patch_faster_whisper(mock.Mock(return_value=fake_model)),
        pytest.raises(TranscriptionError, match="decode error"),
    ):
        transcribe(Path("a.wav"), CFG)


def test_transcribe_missing_language_info(fake_whisper):
    fake_whisper.WhisperModel.return_value.transcribe.return_value = (
        iter([]),
        types.SimpleNamespace(language=None),
    )
    result = transcribe(Path("a.wav"), CFG)
    assert result.language is None
    assert result.segments == []
