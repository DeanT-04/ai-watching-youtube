# OCR eval fixtures — ground truth provenance

These fixtures are the ground truth for `scripts/eval_ocr.py` (see
`NEXT_STEPS.md` Step 3). Every `ground_truth.mqh` here was transcribed from
the actual video frames — **not** from a synthetic source — using the method
below, so each line is re-checkable against `frames/aDWDJrACs7s/t_*.png`.

## How ground truth was derived

The frames are 1080p screenshots of the MetaEditor IDE showing MQL5 source.
No literal "read the pixels by eye" was possible in the authoring environment
(no image display); instead every line was verified three independent ways and
only kept when they agreed:

1. **8-frame tesseract-only consensus** (`results/stress_verify/`, per-frame
   raw OCR + per-line gutter-number vote counts) — this is the same
   tesseract-only path that was independently verified against the
   already-hand-verified `SymbolInformation` struct (25/26 fields exact) in
   the prior session's manual review.
2. **High-resolution re-OCR of the exact pixel rows** (3x–10x Lanczos
   upscale, psm 6/7) on 3–6 different frames per ambiguous token.
3. **MQL5 domain knowledge** (keyword/type dictionary, brace matching, enum
   `= N` convention) to resolve OCR confusions that the pixels could not —
   e.g. `@` → `0` (the value digit), `}3` → `};`, `boo1` → `bool`.

Ambiguities that could not be resolved confidently are noted in the fixture's
`manifest.json` (`note` field) rather than silently guessed.

### Confusions resolved by pixel evidence

| Frame text read | Resolution | Evidence |
|---|---|---|
| `double ClosePrice;` vs `closePrice` | **`ClosePrice`** | 6 frames, 4x+ upscale, 94% confidence (RUN.md's `closePrice` was a transcription slip) |
| `#property version "1.606"` vs `"1.00"` | **`"1.00"`** | 3x upscale reads `"1.00"` on 4 frames at 78-82% confidence; RUN.md documents `"1.00"`; the 2x "1.606" is scale-dependent OCR noise |
| `FixedLots = @` / `AllowBuySell = @` / `CloseStopTradingFor24h = @` / `TheAccount = @` | **`= 0`** | `@` glyph is a single digit (10px, vs `2,` at 17px); `(@.25%` in a comment = `(0.25%`; MQL5 enum convention (last member `= 0` without comma) |

## Fixture inventory

| case_id | condition | frames | what it covers |
|---|---|---|---|
| `struct-symbol-information` | long-static | t_0486..t_0522 | `SymbolInformation` struct (28 lines) — the documented hand-verified region |
| `struct-no-gutter` | no-gutter | t_0486..t_0522 | same struct, crop excludes the gutter column → no line numbers → tests the no-consensus fallback |
| `enums-lotsizing` | long-static | t_0414..t_0451 | 4 enums (`LotSizingEnum`, `AllowBuySellEnum`, `eMaxDrawdownAction`, `eDrawdownCalculation`) |
| `enums-scrolled` | scrolling | t_0452..t_0459 | same enums scrolled ~3 lines down (line 20 at top) |
| `file-header-oninit` | long-static | t_0294..t_0344 | file header + includes + globals + `OnInit`/`OnDeinit` |
| `file-header-early` | long-static | t_0223..t_0229 | earlier file state: header + empty `OnInit`/`OnDeinit`/`OnTick` |
| `ctrade-request-type-time` | short-dwell | t_0291..t_0293 | `CTrade` library methods (3 frames) |
| `ctrade-init-destructor` | short-dwell | t_0288..t_0289 | `CTrade` ctor/dtor/`Request` (2 frames) |

## Re-checking a line

```bash
# zoom into an exact pixel row on a frame (e.g. the struct field row)
.venv/Scripts/python -c "
import cv2, tempfile, sys; from pathlib import Path
sys.path.insert(0, 'src')
from ytextract.subprocess_utils import run_command
from ytextract.ocr.tesseract_engine import parse_tsv
img = cv2.imread('frames/aDWDJrACs7s/t_0520.png')
crop = img[395:435, 380:950]  # y-range, x-range of the line in question
up = cv2.resize(crop, (crop.shape[1]*6, crop.shape[0]*6), interpolation=cv2.INTER_LANCZOS4)
with tempfile.TemporaryDirectory() as tmp:
    p = Path(tmp)/'f.png'; cv2.imwrite(str(p), up)
    r = run_command(['tesseract', str(p), 'stdout', '--psm', '6', 'tsv'])
print([l.text for l in parse_tsv(r.stdout)])
"
```

## Known scoring exclusions

`scripts/eval_ocr.py` excludes MetaEditor wizard chrome from scoring: banner
rules (`//+---…`) and boxed title comments (`//| … |`). These are decorative,
OCR as garbage, and their exact dash count is not pixel-verifiable — the
harness measures *code* recovery, not comment-chrome reproduction.
