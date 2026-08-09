"""Integration tests: real binaries (ffmpeg-free: cv2 video + real tesseract +
cached whisper model) on synthetic local media. Excluded from the default
coverage-gated run; execute with:

    .venv\\Scripts\\python.exe -m pytest -m integration -o addopts=""
"""

import wave
from pathlib import Path

import cv2
import numpy as np
import pytest

from ytextract.config import Config
from ytextract.keyframes import sample_and_select
from ytextract.ocr import run_ocr
from ytextract.repair import repair
from ytextract.storage import CodeBlock, VideoResult, save_result
from ytextract.transcriber import transcribe

pytestmark = pytest.mark.integration


def _render_code_frame(lines, height=480, width=640):
    """BGR frame with white code text on a dark background (cv2/Hershey font)."""
    img = np.full((height, width, 3), 20, dtype=np.uint8)
    y = 60
    for line in lines:
        cv2.putText(
            img, line, (20, y), cv2.FONT_HERSHEY_SIMPLEX, 1.0, (255, 255, 255), 2
        )
        y += 50
    return img


def _write_silent_wav(path: Path, seconds=1.0, rate=16000) -> None:
    with wave.open(str(path), "wb") as wav:
        wav.setnchannels(1)
        wav.setsampwidth(2)
        wav.setframerate(rate)
        wav.writeframes(b"\x00\x00" * int(rate * seconds))


@pytest.fixture
def local_media(tmp_path):
    """10s 10fps synthetic video: 5s of one code block, 5s of another."""
    video = tmp_path / "synth.mp4"
    writer = cv2.VideoWriter(
        str(video), cv2.VideoWriter_fourcc(*"mp4v"), 10, (640, 480)
    )
    assert writer.isOpened(), "cv2 VideoWriter could not open mp4v codec"
    try:
        for i in range(100):
            lines = (
                ["def main():", "print('hello')"]
                if i < 50
                else ["x = 1 + 2", "print(x)"]
            )
            writer.write(_render_code_frame(lines))
    finally:
        writer.release()
    audio = tmp_path / "silence.wav"
    _write_silent_wav(audio)
    return video, audio


def test_real_ocr_reads_rendered_code():
    """Real tesseract against a synthetic fixture image."""
    image = _render_code_frame(["def main():", "print('hi')"])
    result = run_ocr(image, Config.load(env={}), engines=("tesseract",))
    joined = " ".join(line.text for line in result.lines).lower()
    assert "def main" in joined
    assert "print" in joined


def test_local_video_pipeline(tmp_path, local_media, no_dotenv):
    """Phases 3–8 against synthetic local media (real cv2, tesseract, whisper).

    Whisper uses the cached ``tiny.en`` model (offline). Paddle is not used
    here (its model download needs the network); the ensemble's engine
    selection is exercised as configured.
    """
    video, audio = local_media
    cfg = Config.load(
        env={
            "YTEXTRACT_DATA_DIR": str(tmp_path),
            "YTEXTRACT_MAX_WORKERS": "2",
            "YTEXTRACT_WHISPER_MODEL_SIZE": "tiny.en",
            "YTEXTRACT_WHISPER_LANGUAGE": "en",
            "YTEXTRACT_SAMPLE_RATE_FPS": "2.0",
        }
    )

    transcript = transcribe(audio, cfg)
    assert isinstance(transcript.segments, list)

    frames = sample_and_select(str(video), cfg)
    assert len(frames) >= 2  # two distinct code blocks → ≥2 stable runs

    code_blocks: list[CodeBlock] = []
    for frame in frames:
        ocr = run_ocr(frame.image, cfg, engines=("tesseract",))
        text = "\n".join(line.text for line in ocr.lines)
        checked = repair(text, "python")
        code_blocks.append(
            CodeBlock(
                timestamp=frame.timestamp,
                text=checked.code,
                language="python",
                source=ocr.engine,
                valid=bool(checked.valid),
                issues=checked.issues,
            )
        )
    assert any("def main" in b.text for b in code_blocks)

    result = VideoResult(
        video_id="integration-synth",
        title="Synthetic Integration Video",
        uploader="local",
        webpage_url="local",
        transcript_language=transcript.language,
        segments=transcript.segments,
        code_blocks=code_blocks,
    )
    outdir = save_result(result, cfg)
    assert (outdir / "transcript.json").exists()
    assert (outdir / "code_blocks.json").exists()
