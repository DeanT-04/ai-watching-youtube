"""Inspect a downloaded video to determine the MetaEditor code-pane crop and
whether gutter line numbers are visible — the generalization check for step 3.

For each frame sampled: OCR the full frame with tesseract (fast) and look
for a column of short integer tokens (gutter line numbers) on the left side
of a code region, plus report the bounding region where code-like text
(#property, struct, int, string, etc.) appears.

Usage: python scripts/inspect_video.py <video_id>
"""
import glob, json, re, sys, time
import cv2
sys.path.insert(0, 'src')
from ytextract.ocr import create_engine
from ytextract.ocr.base import OcrText
from ytextract.ocr.tesseract_engine import parse_tsv
from ytextract.subprocess_utils import run_command
import tempfile
from pathlib import Path

video_id = sys.argv[1]
video_path = sorted(glob.glob(f'data/raw/{video_id}/{video_id}.*')) 
video_path = next(p for p in video_path if p.endswith(('.mp4','.mkv','.webm')))
print('video:', video_path)

cap = cv2.VideoCapture(str(video_path))
fps = float(cap.get(cv2.CAP_PROP_FPS)) or 30.0
n = int(cap.get(cv2.CAP_PROP_FRAME_COUNT))
dur = n / fps
print(f'fps={fps:.2f} frames={n} duration={dur/60:.1f}min')

# sample ~8 frames spread across the video
sample_idx = [int(i * (n - 1) / 7) for i in range(8)]
samples = []
for idx in sample_idx:
    cap.set(cv2.CAP_PROP_POS_FRAMES, idx)
    ok, img = cap.read()
    if ok:
        samples.append((idx / fps, img))
cap.release()
print(f'sampled {len(samples)} frames')

eng = create_engine('tesseract')
report = []
for t, img in samples:
    with tempfile.TemporaryDirectory() as tmp:
        png = Path(tmp) / 'f.png'
        cv2.imwrite(str(png), img)
        r = run_command(['tesseract', str(png), 'stdout', '--psm', '6', 'tsv'], check=False)
    words = []
    for raw in r.stdout.splitlines():
        parts = raw.split('\t')
        if len(parts) != 12 or parts[0] == 'level':
            continue
        try:
            conf = float(parts[10])
            text = parts[11].strip()
            left, top, w, h = int(parts[6]), int(parts[7]), int(parts[8]), int(parts[9])
        except ValueError:
            continue
        if conf > 0 and text:
            words.append({'text': text, 'left': left, 'top': top, 'w': w, 'h': h, 'conf': conf})
    # candidate gutter numbers: short integer tokens in the left half
    gutter = [w for w in words if re.fullmatch(r'\d{1,3}', w['text']) and w['left'] < 600 and w['top'] > 100]
    # code-like lines
    code_kw = re.compile(r'#property|struct|void|int|string|double|bool|datetime|return|enum|class|input|#include|^\d+$')
    code_words = [w for w in words if code_kw.search(w['text'])]
    report.append({
        't': round(t, 1),
        'img_size': list(img.shape[:2]),
        'gutter_numbers': gutter[:20],
        'n_gutter': len(gutter),
        'code_words': code_words[:15],
        'all_words_left': sorted([w for w in words if w['left'] < 200][:25], key=lambda w: w['top'])[:25],
    })
    print(f't={t/60:.1f}min gutter={len(gutter)} code_kw={len(code_words)}', flush=True)

Path(f'results/inspect_{video_id}.json').write_text(json.dumps(report, indent=1), encoding='utf-8')
print('saved results/inspect_' + video_id + '.json')
