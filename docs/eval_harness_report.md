# OCR eval harness — baseline report

This is the first run of `scripts/eval_ocr.py` (see `NEXT_STEPS.md` Step 3).
It measures the **real pipeline** (`_to_code_block_group` with the real
paddle+tesseract ensemble, and separately tesseract-only for the regression
gate) against 8 hand-transcribed fixtures from `frames/aDWDJrACs7s/`.

## Numbers

**Real pipeline (paddle+tesseract ensemble):**

| case | condition | line-recovery | CER | WER | silent drops |
|---|---|---|---|---|---|
| struct-symbol-information | long-static | **100%** | 0.047 | 0.128 | 0 |
| struct-no-gutter | no-gutter | **100%** | 0.306 | 0.544 | 0 |
| enums-lotsizing | long-static | **100%** | 0.015 | 0.082 | 0 |
| enums-scrolled | scrolling | **100%** | 0.013 | 0.044 | 0 |
| file-header-oninit | long-static | 85.7% | 0.051 | 0.211 | 2 |
| file-header-early | long-static | 80.0% | 0.006 | 0.083 | 2 |
| ctrade-request-type-time | short-dwell | **100%** | 0.002 | 0.075 | 0 |
| ctrade-init-destructor | short-dwell | 91.7% | 0.038 | 0.307 | 2 |

**Tesseract-only (committed baseline for the regression gate):**

| case | line-recovery | CER | WER | silent drops |
|---|---|---|---|---|
| struct-symbol-information | 100% | 0.031 | 0.083 | 0 |
| struct-no-gutter | 90.0% | 0.000 | 0.000 | 3 |
| enums-lotsizing | 100% | 0.086 | 0.191 | 0 |
| enums-scrolled | 100% | 0.006 | 0.030 | 0 |
| file-header-oninit | 85.7% | 0.020 | 0.308 | 2 |
| file-header-early | 80.0% | 0.011 | 0.333 | 2 |
| ctrade-request-type-time | 100% | 0.003 | 0.158 | 0 |
| ctrade-init-destructor | 91.7% | 0.025 | 0.417 | 2 |

## What the numbers show (biggest bottlenecks, in order)

1. **The field-name fix works at scale.** The previously-documented
   `SymbolInformation` struct recovers **100% of lines** through the real
   pipeline now (was 0/26 fields with type+name intact before the fix). The
   4 enums and the scrolled enum view also recover 100%.

2. **The honest worst case is the no-gutter path, and it's tolerable.**
   `struct-no-gutter` recovers 100% of lines through the real pipeline but at
   **CER 0.306 / WER 0.544** — the fallback (no line numbers → single-frame
   reading) gets the lines but mangles the characters. Tesseract-only on the
   same case drops 3 lines (the gutter-less crop also hides 3 struct lines at
   the crop's right edge). This is the number to watch if the fallback is
   ever changed.

3. **The file header is the remaining *completeness* gap.** Both header
   fixtures drop 2 lines (`#property copyright "Mr CapFree"` and
   `#property strict`) because those top-of-file lines' gutter numbers read
   inconsistently across frames, so consensus never anchors them and drops
   them as unanchored. Not a character-accuracy problem (CER stays ~0.01),
   a *silent drop* problem — exactly the failure mode the harness exists to
   catch.

4. **`ctrade-init-destructor` (2-frame short dwell) drops 2 lines** — the
   constructor's initializer-list continuation lines, which only one of the
   two frames ever saw. A thin-redundancy case, consistent with expectation
   for a 2-frame group.

## How to re-run

```bash
.venv/Scripts/python scripts/eval_ocr.py                     # real pipeline -> docs/eval_baseline.json
.venv/Scripts/python scripts/eval_ocr.py --engines tesseract  # fast, deterministic (regression gate)
.venv/Scripts/python -m pytest -m integration -o addopts=""    # regression gate
```

The regression gate (`tests/test_eval_harness.py`) asserts each fixture's
line-recovery does not fall more than 0.1 below the committed
`docs/eval_baseline.json`.
