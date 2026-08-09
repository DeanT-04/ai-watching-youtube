"""Keyframe selection: sample frames at a configurable rate, keep one per
stable run.

OpenCV reads frames from the video; the pure-python :func:`select_keyframes`
groups consecutive frames whose similarity meets ``Config.similarity_threshold``
and keeps the **last** frame of each run, so a code block that stays on screen
for seconds contributes exactly one keyframe to OCR.
"""

from __future__ import annotations

import logging
from dataclasses import dataclass

import cv2
import numpy as np

from .config import Config

logger = logging.getLogger("ytextract.keyframes")


class KeyframeError(RuntimeError):
    """Raised when a video cannot be opened or sampled."""


@dataclass(frozen=True)
class Frame:
    """One sampled frame with its position in the video (seconds)."""

    timestamp: float
    image: np.ndarray


def frame_similarity(a: np.ndarray, b: np.ndarray) -> float:
    """Global structural-similarity-style score in [0, 1] (1.0 = identical).

    Implemented with numpy only (no skimage dependency): mean + variance +
    covariance of the two flattened images with the standard SSIM constants.
    Shape mismatch returns 0.0.
    """
    if a.shape != b.shape:
        return 0.0
    a_f = a.astype(np.float64) / 255.0
    b_f = b.astype(np.float64) / 255.0
    mu_a, mu_b = a_f.mean(), b_f.mean()
    sigma_a, sigma_b = a_f.var(), b_f.var()
    cov = np.mean((a_f - mu_a) * (b_f - mu_b))
    c1, c2 = 0.01**2, 0.03**2
    return float(
        ((2 * mu_a * mu_b + c1) * (2 * cov + c2))
        / ((mu_a**2 + mu_b**2 + c1) * (sigma_a + sigma_b + c2))
    )


def select_keyframes(frames: list[Frame], cfg: Config) -> list[Frame]:
    """Return the last frame of each stable run of similar consecutive frames."""
    if not frames:
        return []
    keyframes: list[Frame] = []
    for i in range(len(frames) - 1):
        if (
            frame_similarity(frames[i].image, frames[i + 1].image)
            < cfg.similarity_threshold
        ):
            keyframes.append(frames[i])
    keyframes.append(frames[-1])
    logger.info(
        "selected %d keyframes from %d sampled frames", len(keyframes), len(frames)
    )
    return keyframes


def sample_frames(video_path: str, sample_rate_fps: float) -> list[Frame]:
    """Open ``video_path`` and sample one frame every ``1/sample_rate_fps`` seconds."""
    cap = cv2.VideoCapture(video_path)
    if not cap.isOpened():
        raise KeyframeError(f"could not open video: {video_path}")
    try:
        fps = float(cap.get(cv2.CAP_PROP_FPS)) or 30.0
        step = max(1, round(fps / sample_rate_fps)) if sample_rate_fps > 0 else 1
        frames: list[Frame] = []
        index = 0
        while True:
            ok, image = cap.read()
            if not ok:
                break
            if index % step == 0:
                frames.append(Frame(timestamp=index / fps, image=image))
            index += 1
    finally:
        cap.release()
    return frames


def sample_and_select(video_path: str, cfg: Config) -> list[Frame]:
    """Convenience: sample at the configured rate, then keep one frame per stable run."""
    sampled = sample_frames(video_path, cfg.sample_rate_fps)
    return select_keyframes(sampled, cfg)
