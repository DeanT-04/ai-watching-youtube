"""Tests for config.py — pure env parsing, no I/O."""

from pathlib import Path
from unittest import mock

import pytest

from ytextract.config import Config, ConfigError, default_max_workers


@pytest.fixture
def no_dotenv():
    with mock.patch("ytextract.config.load_dotenv"):
        yield


def test_defaults(no_dotenv):
    with mock.patch("ytextract.config.os.cpu_count", return_value=8):
        cfg = Config.load(env={})
    assert cfg.max_workers == 7  # max(1, cores - 1)
    assert cfg.cpu_threshold_percent == 85.0
    assert cfg.cpu_poll_interval == 0.5
    assert cfg.cpu_sleep_seconds == 2.0
    assert cfg.sample_rate_fps == 0.5
    assert cfg.similarity_threshold == 0.98
    assert cfg.crop is None
    assert cfg.upscale_scale == 2
    assert cfg.whisper_model_size == "tiny"
    assert cfg.whisper_device == "cpu"
    assert cfg.whisper_compute_type == "int8"
    assert cfg.whisper_language is None
    assert cfg.ocr_confidence_threshold == 0.5
    assert cfg.log_level == "INFO"
    assert cfg.data_dir.name == "data"
    assert cfg.raw_dir.name == "raw"
    assert cfg.output_dir.name == "output"


def test_env_overrides(no_dotenv):
    cfg = Config.load(
        env={
            "YTEXTRACT_MAX_WORKERS": "3",
            "YTEXTRACT_CPU_THRESHOLD_PERCENT": "90.5",
            "YTEXTRACT_CPU_POLL_INTERVAL": "0.2",
            "YTEXTRACT_CPU_SLEEP_SECONDS": "1.5",
            "YTEXTRACT_SAMPLE_RATE_FPS": "1.0",
            "YTEXTRACT_SIMILARITY_THRESHOLD": "0.95",
            "YTEXTRACT_CROP": "10,20,30,40",
            "YTEXTRACT_UPSCALE_SCALE": "4",
            "YTEXTRACT_WHISPER_MODEL_SIZE": "base",
            "YTEXTRACT_WHISPER_DEVICE": "cpu",
            "YTEXTRACT_WHISPER_COMPUTE_TYPE": "float32",
            "YTEXTRACT_WHISPER_LANGUAGE": "en",
            "YTEXTRACT_OCR_CONFIDENCE_THRESHOLD": "0.7",
            "YTEXTRACT_LOG_LEVEL": "DEBUG",
            "YTEXTRACT_DATA_DIR": "C:/tmp/data",
        }
    )
    assert cfg.max_workers == 3
    assert cfg.cpu_threshold_percent == 90.5
    assert cfg.cpu_poll_interval == 0.2
    assert cfg.cpu_sleep_seconds == 1.5
    assert cfg.sample_rate_fps == 1.0
    assert cfg.similarity_threshold == 0.95
    assert cfg.crop == (10, 20, 30, 40)
    assert cfg.upscale_scale == 4
    assert cfg.whisper_model_size == "base"
    assert cfg.whisper_language == "en"
    assert cfg.ocr_confidence_threshold == 0.7
    assert cfg.log_level == "DEBUG"
    assert cfg.data_dir == Path("C:/tmp/data")
    assert cfg.raw_dir == Path("C:/tmp/data/raw")
    assert cfg.output_dir == Path("C:/tmp/data/output")


def test_empty_strings_fall_back_to_defaults(no_dotenv):
    cfg = Config.load(env={"YTEXTRACT_MAX_WORKERS": "", "YTEXTRACT_LOG_LEVEL": ""})
    assert cfg.max_workers == default_max_workers()
    assert cfg.log_level == "INFO"


def test_uses_os_environ_when_env_is_none(no_dotenv):
    with mock.patch.dict(
        "ytextract.config.os.environ",
        {"YTEXTRACT_LOG_LEVEL": "WARNING", "YTEXTRACT_MAX_WORKERS": "2"},
    ):
        cfg = Config.load()
    assert cfg.log_level == "WARNING"
    assert cfg.max_workers == 2


@pytest.mark.parametrize(
    "name,value",
    [("YTEXTRACT_MAX_WORKERS", "abc"), ("YTEXTRACT_UPSCALE_SCALE", "2.5")],
)
def test_invalid_int_raises(no_dotenv, name, value):
    with pytest.raises(ConfigError, match="must be an integer"):
        Config.load(env={name: value})


def test_invalid_float_raises(no_dotenv):
    with pytest.raises(ConfigError, match="must be a number"):
        Config.load(env={"YTEXTRACT_CPU_THRESHOLD_PERCENT": "high"})


@pytest.mark.parametrize("value", ["1,2,3", "1,2,3,4,5", "a,b,c,d"])
def test_invalid_crop_raises(no_dotenv, value):
    with pytest.raises(ConfigError, match="CROP"):
        Config.load(env={"YTEXTRACT_CROP": value})


def test_default_max_workers(no_dotenv):
    with mock.patch("ytextract.config.os.cpu_count", return_value=1):
        assert default_max_workers() == 1
    with mock.patch("ytextract.config.os.cpu_count", return_value=None):
        assert default_max_workers() == 1
