"""Central configuration for ytextract.

Every tunable lives here, read from environment variables (``YTEXTRACT_``
prefix) with optional overrides from a ``.env`` file in the project root.
No other module hardcodes magic numbers; they all read from :class:`Config`.
"""

from __future__ import annotations

import os
from collections.abc import Mapping
from dataclasses import dataclass
from pathlib import Path

from dotenv import load_dotenv

# Repo root: src/ytextract/config.py -> src/ytextract -> src -> root.
PROJECT_ROOT = Path(__file__).resolve().parent.parent.parent


class ConfigError(ValueError):
    """Raised when an environment variable cannot be parsed into its type."""


def default_max_workers() -> int:
    """CPU-count-derived worker cap: max(1, cores - 1), never the full core count."""
    return max(1, (os.cpu_count() or 2) - 1)


def _env_int(name: str, default: int, env: Mapping[str, str]) -> int:
    raw = env.get(name)
    if raw is None or raw.strip() == "":
        return default
    try:
        return int(raw)
    except ValueError as exc:
        raise ConfigError(f"{name} must be an integer, got {raw!r}") from exc


def _env_float(name: str, default: float, env: Mapping[str, str]) -> float:
    raw = env.get(name)
    if raw is None or raw.strip() == "":
        return default
    try:
        return float(raw)
    except ValueError as exc:
        raise ConfigError(f"{name} must be a number, got {raw!r}") from exc


def _env_str(name: str, default: str, env: Mapping[str, str]) -> str:
    raw = env.get(name)
    if raw is None or raw.strip() == "":
        return default
    return raw.strip()


def _env_crop(name: str, env: Mapping[str, str]) -> tuple[int, int, int, int] | None:
    """Parse ``x,y,width,height`` crop coordinates; empty means no crop."""
    raw = env.get(name)
    if raw is None or raw.strip() == "":
        return None
    parts = raw.split(",")
    if len(parts) != 4:
        raise ConfigError(f"{name} must be 'x,y,width,height', got {raw!r}")
    try:
        x, y, w, h = (int(p.strip()) for p in parts)
    except ValueError as exc:
        raise ConfigError(f"{name} must be four integers, got {raw!r}") from exc
    return x, y, w, h


@dataclass(frozen=True)
class Config:
    """Immutable set of all tunables, loaded from environment + .env."""

    max_workers: int
    cpu_threshold_percent: float
    cpu_poll_interval: float
    cpu_sleep_seconds: float
    sample_rate_fps: float
    similarity_threshold: float
    crop: tuple[int, int, int, int] | None
    upscale_scale: int
    whisper_model_size: str
    whisper_device: str
    whisper_compute_type: str
    whisper_language: str | None
    ocr_confidence_threshold: float
    log_level: str
    data_dir: Path
    raw_dir: Path
    output_dir: Path

    @classmethod
    def load(cls, env: Mapping[str, str] | None = None) -> Config:
        """Build a Config from ``os.environ`` (optionally a supplied mapping)."""
        load_dotenv(PROJECT_ROOT / ".env")
        e: Mapping[str, str] = os.environ if env is None else env
        data_dir = Path(_env_str("YTEXTRACT_DATA_DIR", str(PROJECT_ROOT / "data"), e))
        raw_dir = data_dir / "raw"
        output_dir = data_dir / "output"
        return cls(
            max_workers=_env_int("YTEXTRACT_MAX_WORKERS", default_max_workers(), e),
            cpu_threshold_percent=_env_float(
                "YTEXTRACT_CPU_THRESHOLD_PERCENT", 85.0, e
            ),
            cpu_poll_interval=_env_float("YTEXTRACT_CPU_POLL_INTERVAL", 0.5, e),
            cpu_sleep_seconds=_env_float("YTEXTRACT_CPU_SLEEP_SECONDS", 2.0, e),
            sample_rate_fps=_env_float("YTEXTRACT_SAMPLE_RATE_FPS", 0.5, e),
            similarity_threshold=_env_float("YTEXTRACT_SIMILARITY_THRESHOLD", 0.98, e),
            crop=_env_crop("YTEXTRACT_CROP", e),
            upscale_scale=_env_int("YTEXTRACT_UPSCALE_SCALE", 2, e),
            whisper_model_size=_env_str("YTEXTRACT_WHISPER_MODEL_SIZE", "tiny", e),
            whisper_device=_env_str("YTEXTRACT_WHISPER_DEVICE", "cpu", e),
            whisper_compute_type=_env_str("YTEXTRACT_WHISPER_COMPUTE_TYPE", "int8", e),
            whisper_language=_env_str("YTEXTRACT_WHISPER_LANGUAGE", "", e) or None,
            ocr_confidence_threshold=_env_float(
                "YTEXTRACT_OCR_CONFIDENCE_THRESHOLD", 0.5, e
            ),
            log_level=_env_str("YTEXTRACT_LOG_LEVEL", "INFO", e),
            data_dir=data_dir,
            raw_dir=raw_dir,
            output_dir=output_dir,
        )
