# Line-consensus reconstruction: real before/after

This is a real test against the existing `frames/aDWDJrACs7s/` frames and
real `tesseract`, not a synthetic mock — run yourself with the commands
below.

## Bug found: the crop was clipping ~40% of the visible code vertically

`results/aDWDJrACs7s/RUN.md` records the run config as
`YTEXTRACT_CROP=0,90,1382,380`. Two problems with that region on this
1920x1080 source:

1. `x=0` includes the file-explorer sidebar, whose folder/file names get
   OCR'd inline with the code (visible throughout `code_blocks.json`, e.g.
   `"OpenCL"`, `"Panels"`, `"Trade.mqh"` interleaved mid-block).
2. `y:90..470` (height 380) is **too short** — on frame `t_0520.png` the
   code pane runs to roughly `y=850`. The old crop physically excludes
   everything past source line ~57, regardless of what OCR could have read.

## Single-frame baseline (old crop, one frame)

Cropping `t_0520.png` to the old region and running plain tesseract stops
at line 56 of the 30-line `SymbolInformation` struct (see prior review) —
**11 of 30 fields**, several with character errors (`boo1`, `closePrice`).

## Fixed crop, still single frame

Same frame, corrected crop (`x` starts after the sidebar, `y` extends to
the true pane bottom): captures through line 66 — **21 of 30 fields**, from
the crop fix alone, no consensus involved yet.

## Fixed crop + consensus across 6 consecutive 1s frames (real run)

```
python3 -c "
import cv2, sys; sys.path.insert(0,'src')
from ytextract.ocr.tesseract_engine import TesseractEngine
from ytextract.consensus import reconstruct

eng = TesseractEngine(psm='6')
frames_lines = []
for t in [498,502,506,510,514,518]:
    img = cv2.imread(f'frames/aDWDJrACs7s/t_0{t}.png')
    crop = img[90:850, 345:1920]
    up = cv2.resize(crop, (crop.shape[1]*2, crop.shape[0]*2), interpolation=cv2.INTER_LANCZOS4)
    frames_lines.append(eng.ocr(up))
result = reconstruct(frames_lines)
print(result.covered_line_count, 'lines recovered')
"
```

Result: **28 of 30 struct fields recovered with clean, correct text**
(lines 46-70, 72, 73), matching hand-verified ground truth read directly
off the frame images. This includes automatic correction of an OCR
character error (`67 int lactDavOfllaak:` in a single frame's raw output →
correctly resolved to `LastDayOfWeek;` once outvoted by clean reads from
other frames).

## Known remaining gap (reported honestly, not smoothed over)

Lines 71/74/75 are still missing or attached to the wrong number. The cause
is systematic, not random: the crop's bottom edge clips that specific row
consistently in 2 of the 6 frames, producing a truncated gutter number
(`"72"` read as `"2"`, `"78"` instead of `"74"`) in a way that repeats
across frames rather than varying — so majority vote doesn't self-correct
it, and the current isolated-outlier filter (built for one-off misreads)
doesn't catch a *repeated* misread either. Two follow-ups, not yet
implemented:

1. Add another ~50px of vertical margin below the last visible code row —
   cheap, and directly addresses the observed clipping.
2. Add a monotonic-sequence repair pass: gutter numbers increase by
   exactly 1 per row, so a gap in the resolved sequence (e.g. 70, 73 with
   71/72 missing) combined with a stray short numeric token nearby (`"2"`,
   `"7"`) is very likely that number missing its clipped leading digit(s).

## Net effect

Single frame, old crop: **~37% field recovery**, several character errors.
Six frames, fixed crop, consensus: **~93% field recovery** (28/30), text
matching ground truth exactly. This is on one real struct from the real
video, not a synthetic benchmark — re-run the command above to reproduce.

## Follow-up: wiring into the actual pipeline (`feature/consensus-pipeline-integration`)

The above proved the merge logic works; it didn't make the real pipeline
*use* it, because `select_keyframes` (Phase 4) deliberately collapses every
stable run down to **one** frame before OCR ever runs — by design, to keep
OCR cost down. That means consensus had nothing to consume: there was only
ever one reading per region.

Fixed by adding `keyframes.group_stable_runs` alongside (not replacing)
`select_keyframes`: same run-boundary detection, but keeps every frame in
the run (subsampled to `YTEXTRACT_CONSENSUS_MAX_FRAMES_PER_GROUP`, default
8, evenly across the run) instead of just the last one. `cli.pipeline` now
calls `sample_and_group` + `_to_code_block_group` per run instead of one
frame at a time. A run of size 1 is byte-for-byte identical to the old
single-frame path — verified via `test_to_code_block_group_size_one_matches_single_frame_path`.

Re-ran the real frames through the *actual* `cli._to_code_block_group`
(not a standalone script) with `create_engine('tesseract')` — same clean
28/30-field result as the standalone consensus test, confirming the wiring
itself, not just the merge algorithm, works against real data.

### Bug found by the multi-video stress test, fixed after the merge

The wiring verification above used tesseract-only (`create_engine('tesseract')`),
but the **real pipeline runs the paddle+tesseract ensemble** (`cli.pipeline`
builds `[create_engine('paddle'), create_engine('tesseract')]`). PaddleOCR
emits word-level boxes: a source line `47 string SymbolName;` arrives as
three separate OCR entries (`"47"`, `"string"`, `"SymbolName;"`). The original
separate-shape pairing attached the gutter number to only the **first**
following content entry and reset the anchor, so `"SymbolName;"` became
*unanchored* and was dropped by `_to_code_block_group` — the field name
silently vanished from every multi-frame struct/enum line.

Verified against the real frames through the actual ensemble path (frame
t_0520: 93 OCR entries → 34 unanchored, including every struct field name)
and quantified on the full 300-frame batch in
`docs/multi_video_stress_test.md`: group 40 recovered **0/26 fields with
type+name intact** (25 bare-type lines) through the real pipeline, while
tesseract-only consensus recovered 25/26 — proving the names were in the
frames and the loss was in the pairing logic.

**Fix (`pair_line_numbers`):** the gutter number now stays anchored across
*all* consecutive content entries until the next standalone number resets it,
and those entries are joined with a space into one `(number, text)` pair —
so paddle word-boxes (`"47"`, `"string"`, `"SymbolName;"`) yield
`(47, "string SymbolName;")` instead of dropping the tail. Added regression
tests (`test_pair_line_numbers_joins_paddle_word_boxes_under_one_number`,
`test_pair_line_numbers_joins_only_within_one_number`,
`test_reconstruct_paddle_word_boxes_recover_full_line`). Re-run of the real
ensemble path on t_0520 now recovers `47: string SymbolName;`,
`48: datetime LastMainTFUpdate;`, etc.

### Mistake caught during this verification, fixed before commit

First version of `_to_code_block_group` appended `merged.unanchored` (OCR
lines that never got paired with a gutter number) to the output text, on
the theory that they might still be genuine code. Running it against the
real frames showed this was wrong: unanchored text is overwhelmingly
repeated UI-chrome OCR noise (toolbar labels like "Compile", "History",
misread once per frame), and appending it turned a clean 28-line
reconstruction into a wall of garbage. Fixed to use only the anchored
consensus text; `unanchored` stays available on `ReconstructedSource` for
a future caller that wants it, but isn't included in the code block by
default.
