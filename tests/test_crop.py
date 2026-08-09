"""Tests for crop.py — small synthetic images, real cv2 (no network)."""

import numpy as np
import pytest

from ytextract.config import Config
from ytextract.crop import CropError, crop_region, preprocess, upscale

IMG = np.arange(100, dtype=np.uint8).reshape(10, 10)


def _cfg(**overrides):
    env = {"YTEXTRACT_MAX_WORKERS": "2", **overrides}
    return Config.load(env=env)


def test_crop_region_valid():
    region = IMG[3:7, 2:6]
    assert (crop_region(IMG, (2, 3, 4, 4)) == region).all()


def test_crop_region_negative_origin_raises():
    with pytest.raises(CropError, match="positive"):
        crop_region(IMG, (-1, 0, 4, 4))


def test_crop_region_zero_size_raises():
    with pytest.raises(CropError, match="positive"):
        crop_region(IMG, (0, 0, 0, 4))


def test_crop_region_out_of_bounds_raises():
    with pytest.raises(CropError, match="exceeds"):
        crop_region(IMG, (8, 8, 4, 4))


def test_upscale_scale_one_returns_same_object():
    assert upscale(IMG, 1) is IMG


def test_upscale_lanczos_doubles_dims():
    result = upscale(IMG, 2)
    assert result.shape == (20, 20)


def test_upscale_invalid_scale_raises():
    with pytest.raises(CropError, match="positive"):
        upscale(IMG, 0)


def test_preprocess_no_crop_only_upscales():
    cfg = _cfg()
    result = preprocess(IMG, cfg)
    assert result.shape == (20, 20)


def test_preprocess_with_crop_and_upscale():
    cfg = _cfg(YTEXTRACT_CROP="2,2,4,4")
    result = preprocess(IMG, cfg)
    assert result.shape == (8, 8)  # 4x4 cropped, doubled


def test_preprocess_crop_out_of_bounds_raises():
    cfg = _cfg(YTEXTRACT_CROP="0,0,20,20")
    with pytest.raises(CropError, match="exceeds"):
        preprocess(IMG, cfg)
