"""Crop & preprocessing before OCR.

The crop region is a config value (``Config.crop`` = ``x,y,width,height``),
never hardcoded. Upscaling uses Lanczos interpolation so small terminal-font
code becomes easier for OCR engines to read.
"""

from __future__ import annotations

import cv2
import numpy as np

from .config import Config

CropRegion = tuple[int, int, int, int]


class CropError(ValueError):
    """Raised when a configured crop region lies outside the image."""


def crop_region(image: np.ndarray, region: CropRegion) -> np.ndarray:
    """Return the ``(x, y, width, height)`` sub-region of ``image``.

    Validates that the region is fully inside the image so a misconfigured
    crop fails loudly instead of silently producing a wrong-sized frame.
    """
    height, width = image.shape[:2]
    x, y, w, h = region
    if x < 0 or y < 0 or w <= 0 or h <= 0:
        raise CropError(f"crop region must be positive and inside the image: {region}")
    if x + w > width or y + h > height:
        raise CropError(f"crop region {region} exceeds image size {width}x{height}")
    return image[y : y + h, x : x + w]


def upscale(image: np.ndarray, scale: int) -> np.ndarray:
    """Upscale ``image`` by ``scale`` using Lanczos interpolation."""
    if scale <= 0:
        raise CropError(f"upscale scale must be positive, got {scale}")
    if scale == 1:
        return image
    height, width = image.shape[:2]
    return cv2.resize(
        image,
        (width * scale, height * scale),
        interpolation=cv2.INTER_LANCZOS4,
    )


def preprocess(image: np.ndarray, cfg: Config) -> np.ndarray:
    """Apply the configured crop (if any) then Lanczos upscale."""
    result = image
    crop: CropRegion | None = cfg.crop
    if crop is not None:
        result = crop_region(result, crop)
    return upscale(result, cfg.upscale_scale)
