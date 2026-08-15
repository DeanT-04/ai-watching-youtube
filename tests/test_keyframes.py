"""Tests for keyframes.py — cv2.VideoCapture is faked; selection is pure logic."""

from unittest import mock

import cv2
import numpy as np
import pytest

from ytextract.config import Config
from ytextract.keyframes import (
    Frame,
    KeyframeError,
    frame_similarity,
    group_stable_runs,
    sample_and_group,
    sample_and_select,
    sample_frames,
    select_keyframes,
)

CFG = Config.load(env={"YTEXTRACT_MAX_WORKERS": "2"})

BLACK = np.full((4, 4), 128, dtype=np.uint8)


class FakeCap:
    """Minimal stand-in for cv2.VideoCapture."""

    def __init__(self, frames, fps=30.0, opened=True):
        self._frames = list(frames)
        self._fps = fps
        self.opened = opened
        self._i = 0
        self.released = False

    def isOpened(self):
        return self.opened

    def get(self, prop):
        if prop == cv2.CAP_PROP_FPS:
            return self._fps
        return 0.0

    def read(self):
        if self._i < len(self._frames):
            frame = self._frames[self._i]
            self._i += 1
            return True, frame
        return False, None

    def release(self):
        self.released = True


def _patch_capture(fake_cap):
    return mock.patch("ytextract.keyframes.cv2.VideoCapture", return_value=fake_cap)


def test_sample_frames_uses_fps_and_step():
    frames = [BLACK.copy() for _ in range(60)]  # 60 frames @ 30fps = 2s
    fake = FakeCap(frames, fps=30.0)
    with _patch_capture(fake):
        sampled = sample_frames("video.mp4", sample_rate_fps=1.0)
    # step = 30 → indices 0 and 30 → timestamps 0.0 and 1.0
    assert [f.timestamp for f in sampled] == [0.0, 1.0]
    assert fake.released is True


def test_sample_frames_zero_fps_falls_back_to_30():
    frames = [BLACK.copy() for _ in range(60)]
    fake = FakeCap(frames, fps=0.0)
    with _patch_capture(fake):
        sampled = sample_frames("video.mp4", sample_rate_fps=1.0)
    assert [f.timestamp for f in sampled] == [0.0, 1.0]


def test_sample_frames_zero_rate_samples_every_frame():
    frames = [BLACK.copy() for _ in range(5)]
    fake = FakeCap(frames, fps=30.0)
    with _patch_capture(fake):
        sampled = sample_frames("video.mp4", sample_rate_fps=0.0)
    assert len(sampled) == 5


def test_sample_frames_not_opened_raises():
    fake = FakeCap([], opened=False)
    with (
        _patch_capture(fake),
        pytest.raises(KeyframeError, match="could not open"),
    ):
        sample_frames("video.mp4", 1.0)


def test_frame_similarity_identical_is_one():
    assert frame_similarity(BLACK, BLACK.copy()) == 1.0


def test_frame_similarity_shape_mismatch_is_zero():
    assert frame_similarity(BLACK, np.full((8, 8), 128, dtype=np.uint8)) == 0.0


def test_frame_similarity_different_content_is_low():
    other = np.full((4, 4), 200, dtype=np.uint8)
    sim = frame_similarity(BLACK, other)
    assert sim < CFG.similarity_threshold


def test_frame_similarity_near_identical_is_high():
    noisy = BLACK.copy()
    noisy[0, 0] = 127
    assert frame_similarity(BLACK, noisy) > 0.98


def test_select_keyframes_empty():
    assert select_keyframes([], CFG) == []


def test_select_keyframes_identical_run_keeps_one():
    frames = [Frame(ts, BLACK.copy()) for ts in range(10)]
    result = select_keyframes(frames, CFG)
    assert [f.timestamp for f in result] == [9.0]


def test_select_keyframes_keeps_last_frame_of_each_run():
    frames = [
        Frame(0.0, np.full((4, 4), 10, dtype=np.uint8)),
        Frame(1.0, np.full((4, 4), 10, dtype=np.uint8)),
        Frame(2.0, np.full((4, 4), 200, dtype=np.uint8)),
        Frame(3.0, np.full((4, 4), 200, dtype=np.uint8)),
        Frame(4.0, np.full((4, 4), 250, dtype=np.uint8)),
    ]
    result = select_keyframes(frames, CFG)
    assert [f.timestamp for f in result] == [1.0, 3.0, 4.0]


def test_sample_and_select_combines():
    frames = [Frame(ts, BLACK.copy()) for ts in range(10)]
    with mock.patch("ytextract.keyframes.sample_frames", return_value=frames) as sample:
        result = sample_and_select("video.mp4", CFG)
    sample.assert_called_once_with("video.mp4", CFG.sample_rate_fps)
    assert [f.timestamp for f in result] == [9.0]


def test_group_stable_runs_empty():
    assert group_stable_runs([], CFG) == []


def test_group_stable_runs_keeps_every_frame_of_one_run():
    """This is the whole point of the function: unlike select_keyframes,
    a stable run's frames are NOT collapsed to one -- they're preserved so
    consensus reconstruction has multiple reads to merge across."""
    frames = [Frame(float(ts), BLACK.copy()) for ts in range(5)]
    groups = group_stable_runs(frames, CFG)
    assert len(groups) == 1
    assert [f.timestamp for f in groups[0]] == [0.0, 1.0, 2.0, 3.0, 4.0]


def test_group_stable_runs_splits_on_scene_change():
    frames = [
        Frame(0.0, np.full((4, 4), 10, dtype=np.uint8)),
        Frame(1.0, np.full((4, 4), 10, dtype=np.uint8)),
        Frame(2.0, np.full((4, 4), 200, dtype=np.uint8)),
        Frame(3.0, np.full((4, 4), 200, dtype=np.uint8)),
        Frame(4.0, np.full((4, 4), 250, dtype=np.uint8)),
    ]
    groups = group_stable_runs(frames, CFG)
    assert [[f.timestamp for f in g] for g in groups] == [
        [0.0, 1.0],
        [2.0, 3.0],
        [4.0],
    ]


def test_group_stable_runs_subsamples_long_runs():
    cfg = Config.load(
        env={
            "YTEXTRACT_MAX_WORKERS": "2",
            "YTEXTRACT_CONSENSUS_MAX_FRAMES_PER_GROUP": "3",
        }
    )
    frames = [Frame(float(ts), BLACK.copy()) for ts in range(20)]
    groups = group_stable_runs(frames, cfg)
    assert len(groups) == 1
    assert len(groups[0]) == 3
    # Keeps the ends of the run, not an arbitrary prefix.
    assert groups[0][0].timestamp == 0.0
    assert groups[0][-1].timestamp == 19.0


def test_group_stable_runs_default_cap_is_eight():
    frames = [Frame(float(ts), BLACK.copy()) for ts in range(100)]
    groups = group_stable_runs(frames, CFG)
    assert len(groups[0]) == 8


def test_sample_and_group_combines():
    frames = [Frame(ts, BLACK.copy()) for ts in range(10)]
    with mock.patch("ytextract.keyframes.sample_frames", return_value=frames) as sample:
        result = sample_and_group("video.mp4", CFG)
    sample.assert_called_once_with("video.mp4", CFG.sample_rate_fps)
    assert len(result) == 1
    # Default cap is 8, so a run of 10 is subsampled -- but the ends are kept.
    assert len(result[0]) == 8
    assert result[0][0].timestamp == 0
    assert result[0][-1].timestamp == 9
