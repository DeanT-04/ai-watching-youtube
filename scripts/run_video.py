"""Run the extraction pipeline on an already-downloaded video (no re-download).

Mirrors cli.pipeline()'s post-download stages exactly: transcribe (whisper) ->
sample_and_group (keyframes) -> _to_code_block_group per stable run (OCR +
consensus) -> save_result. Uses the corrected crop the consensus work is
designed for (345,90,1575,760) and the documented sample rate (0.05 fps).

Per-group detail (frames, timestamps, source, line count) is also written to
results/<video_id>/groups.json so the PR can report per-region numbers.

Usage: python scripts/run_video.py <video_id>
"""
import json
import sys
import time
from pathlib import Path

sys.path.insert(0, "src")
from ytextract.cli import _to_code_block_group
from ytextract.config import Config
from ytextract.downloader import DownloadedMedia, VideoMetadata, _load_metadata
from ytextract.keyframes import sample_and_group
from ytextract.ocr import create_engine
from ytextract.storage import CodeBlock, VideoResult, save_result
from ytextract.transcriber import transcribe

video_id = sys.argv[1]
outdir = Path("data/raw") / video_id
video_path = next(
    p for p in sorted(outdir.glob(f"{video_id}.*")) if p.suffix in (".mp4", ".mkv", ".webm")
)
audio_path = outdir / f"{video_id}.wav"
metadata = _load_metadata(outdir / f"{video_id}.info.json")
media = DownloadedMedia(
    video_dir=outdir,
    video_path=video_path,
    audio_path=audio_path,
    metadata=metadata,
)

cfg = Config.load(
    {
        "YTEXTRACT_CROP": "345,90,1575,760",  # corrected crop (consensus_eval.md)
        "YTEXTRACT_UPSCALE_SCALE": "2",
        "YTEXTRACT_SIMILARITY_THRESHOLD": "0.98",
        "YTEXTRACT_CONSENSUS_MAX_FRAMES_PER_GROUP": "8",
        "YTEXTRACT_SAMPLE_RATE_FPS": "0.05",  # documented run config (RUN.md)
    }
)

print(f"== {video_id}: {metadata.title}", flush=True)
t0 = time.time()
print("phase 3/8: transcribing", flush=True)
transcript = transcribe(media.audio_path, cfg)
print(f"  transcript: {len(transcript.segments)} segments, lang={transcript.language} ({(time.time()-t0)/60:.1f}min)", flush=True)

print("phase 4/8: sampling + grouping", flush=True)
groups = sample_and_group(str(media.video_path), cfg)
print(f"  {len(groups)} stable-run groups, {sum(len(g) for g in groups)} frames to OCR", flush=True)

print("phase 5-6/8: OCR + consensus", flush=True)
engines = [create_engine("paddle"), create_engine("tesseract")]
code_blocks: list[CodeBlock] = []
group_stats = []
for gi, group in enumerate(groups):
    g0 = time.time()
    ts = [f.timestamp for f in group]
    try:
        block = _to_code_block_group(group, cfg, engines)
        entry = {
            "group": gi, "n_frames": len(group),
            "t_start": ts[0], "t_end": ts[-1],
            "source": block.source, "lines": len(block.text.splitlines()),
            "text": block.text, "error": None,
        }
    except Exception as exc:  # noqa: BLE001
        entry = {
            "group": gi, "n_frames": len(group),
            "t_start": ts[0], "t_end": ts[-1],
            "source": None, "lines": 0, "text": None,
            "error": f"{type(exc).__name__}: {exc}",
        }
    entry["secs"] = round(time.time() - g0, 1)
    group_stats.append(entry)
    code_blocks.append(block)
    print(
        f"  group {gi:02d}: {len(group)} frames t={ts[0]:.0f}-{ts[-1]:.0f} "
        f"{entry['source'] or entry['error']} -> {entry['lines']} lines",
        flush=True,
    )

print("phase 7-8/8: validating + saving", flush=True)
result = VideoResult(
    video_id=video_id,
    title=metadata.title,
    uploader=metadata.uploader,
    webpage_url=metadata.webpage_url,
    transcript_language=transcript.language,
    segments=transcript.segments,
    code_blocks=code_blocks,
)
save_result(result, cfg)
Path(f"results/{video_id}").mkdir(parents=True, exist_ok=True)
Path(f"results/{video_id}/groups.json").write_text(
    json.dumps({"video": video_id, "total_secs": time.time() - t0, "groups": group_stats}, indent=1),
    encoding="utf-8",
)
print(f"DONE {video_id} in {(time.time()-t0)/60:.1f} min", flush=True)
