"""Stress test: run the real group_stable_runs + _to_code_block_group path
over ALL 300 extracted frames of aDWDJrACs7s (t_0223..t_0522, 3:43-8:43),
using the corrected crop from docs/consensus_eval.md (345,90,1575,760) and
the real OCR engines the pipeline uses (paddle + tesseract).

Output: results/stress/<group>.json with per-frame OCR + consensus result,
plus a summary JSON. Group-by-group so memory stays bounded and one group
failing doesn't kill the run.
"""

import glob
import json
import re
import sys
import time
from pathlib import Path

import cv2

sys.path.insert(0, "src")
from ytextract.cli import _to_code_block_group
from ytextract.config import Config
from ytextract.keyframes import Frame, group_stable_runs
from ytextract.ocr import create_engine

OUT = Path("results/stress")
OUT.mkdir(parents=True, exist_ok=True)

# Corrected crop: x=345 (after sidebar), y=90, w=1920-345=1575, h=850-90=760
cfg = Config.load(
    {
        "YTEXTRACT_CROP": "345,90,1575,760",
        "YTEXTRACT_UPSCALE_SCALE": "2",
        "YTEXTRACT_SIMILARITY_THRESHOLD": "0.98",
        "YTEXTRACT_CONSENSUS_MAX_FRAMES_PER_GROUP": "8",
    }
)

paths = sorted(glob.glob("frames/aDWDJrACs7s/t_*.png"))
frames = []
for p in paths:
    m = re.search(r"t_(\d+)\.png", p)
    frames.append(Frame(timestamp=float(m.group(1)), image=cv2.imread(p)))
print(f"loaded {len(frames)} frames", flush=True)

groups = group_stable_runs(frames, cfg)
print(f"{len(groups)} groups, {sum(len(g) for g in groups)} frames to OCR", flush=True)

engines = [create_engine("paddle"), create_engine("tesseract")]
summary = []
t_start = time.time()
for gi, group in enumerate(groups):
    g_start = time.time()
    ts = [f.timestamp for f in group]
    try:
        block = _to_code_block_group(group, cfg, engines)
        entry = {
            "group": gi,
            "n_frames": len(group),
            "t_start": ts[0],
            "t_end": ts[-1],
            "source": block.source,
            "valid": block.valid,
            "text": block.text,
            "error": None,
        }
    except Exception as exc:  # noqa: BLE001
        entry = {
            "group": gi,
            "n_frames": len(group),
            "t_start": ts[0],
            "t_end": ts[-1],
            "source": None,
            "valid": None,
            "text": None,
            "error": f"{type(exc).__name__}: {exc}",
        }
    entry["secs"] = round(time.time() - g_start, 1)
    summary.append(entry)
    (OUT / f"group_{gi:02d}.json").write_text(
        json.dumps(entry, indent=2), encoding="utf-8"
    )
    n_lines = len(entry["text"].splitlines()) if entry["text"] else 0
    elapsed = time.time() - t_start
    print(
        f"group {gi:02d}: {len(group)} frames t={ts[0]:.0f}-{ts[-1]:.0f} "
        f"{entry['source'] or entry['error']} -> {n_lines} lines "
        f"({entry['secs']}s, total {elapsed/60:.1f}min)",
        flush=True,
    )

(OUT / "summary.json").write_text(
    json.dumps({"video": "aDWDJrACs7s", "groups": summary, "total_secs": time.time() - t_start}, indent=2),
    encoding="utf-8",
)
print(f"DONE in {(time.time()-t_start)/60:.1f} min -> results/stress/", flush=True)
