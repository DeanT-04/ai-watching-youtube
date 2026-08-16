"""Independent verification of the stress-test consensus output.

For every group: re-OCR each frame with tesseract ONLY (fast, ~2.4s/frame)
using the same corrected crop, run consensus.reconstruct() to get the
per-line vote counts, and save:
  - per-frame raw OCR lines (with gutter numbers)
  - consensus lines dict: {line_no: {"text": ..., "votes": n, "frames": [...]}}
This is an independent cross-check of the paddle+tesseract ensemble run:
it shows how many frames actually supported each consensus line, and lets
a reviewer see the raw OCR that backs the merged text.
"""
import glob, json, re, sys, time
from pathlib import Path
import cv2
sys.path.insert(0, 'src')
from ytextract.config import Config
from ytextract.crop import preprocess
from ytextract.keyframes import Frame, group_stable_runs
from ytextract.ocr import create_engine
from ytextract.ocr.base import OcrText
from ytextract.consensus import reconstruct, pair_line_numbers

OUT = Path('results/stress_verify')
OUT.mkdir(parents=True, exist_ok=True)

cfg = Config.load({
    "YTEXTRACT_CROP": "345,90,1575,760",
    "YTEXTRACT_UPSCALE_SCALE": "2",
    "YTEXTRACT_SIMILARITY_THRESHOLD": "0.98",
    "YTEXTRACT_CONSENSUS_MAX_FRAMES_PER_GROUP": "8",
})

paths = sorted(glob.glob('frames/aDWDJrACs7s/t_*.png'))
frames = [Frame(timestamp=float(re.search(r't_(\d+)\.png', p).group(1)), image=cv2.imread(p)) for p in paths]
groups = group_stable_runs(frames, cfg)
eng = create_engine('tesseract')

report = []
t0 = time.time()
for gi, group in enumerate(groups):
    per_frame = []
    for f in group:
        prepared = preprocess(f.image, cfg)
        lines = eng.ocr(prepared)
        per_frame.append({
            "t": f.timestamp,
            "raw": [(l.text, round(l.confidence, 3)) for l in lines],
        })
    frames_lines = [[OcrText(text=t, confidence=c) for t, c in f["raw"]] for f in per_frame]
    merged = reconstruct(frames_lines)
    # per-line support: how many frames had this line number, and which texts
    support = {}
    for fi, f in enumerate(per_frame):
        for number, item in pair_line_numbers(frames_lines[fi]):
            if number is None:
                continue
            support.setdefault(number, []).append({"frame": fi, "text": item.text})
    entry = {
        "group": gi, "n_frames": len(group), "t_start": group[0].timestamp, "t_end": group[-1].timestamp,
        "consensus_lines": {str(n): {"text": merged.lines[n], "votes": len(support.get(n, []))} for n in sorted(merged.lines)},
        "support": {str(n): support.get(n) for n in sorted(support)},
        "unanchored_count": len(merged.unanchored),
        "per_frame": per_frame,
    }
    report.append(entry)
    (OUT / f"group_{gi:02d}.json").write_text(json.dumps(entry, indent=1, default=str), encoding='utf-8')
    print(f"group {gi:02d}: {len(group)} frames, {len(merged.lines)} consensus lines ({(time.time()-t0)/60:.1f}min)", flush=True)

(OUT / "summary.json").write_text(json.dumps({"groups": report, "total_secs": time.time()-t0}, indent=1, default=str), encoding='utf-8')
print("VERIFY DONE", flush=True)
