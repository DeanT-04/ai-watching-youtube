# Multi-video stress test: consensus/crop generalization (branch `feature/consensus-integrated`)

This document records the real per-region numbers for the stress test (step 2)
and the two additional videos (step 3) of the merge prompt. Everything below
was produced by running the actual pipeline code (`group_stable_runs` +
`_to_code_block_group`) with the real OCR engines (PaddleOCR + tesseract) on
real media — no synthetic mocks. Repro scripts are in `scripts/`:

- `scripts/stress_test.py` — runs all 300 extracted frames of
  `frames/aDWDJrACs7s/` (t_0223..t_0522, the 3:43–8:43 window) through the
  real grouped path with the corrected crop `345,90,1575,760` and the real
  engines. Output: `results/stress/`.
- `scripts/verify_stress.py` — independent tesseract-only re-OCR of every
  group (per-frame raw lines + per-line consensus vote counts).
  Output: `results/stress_verify/`.
- `scripts/inspect_video.py` — per-video frame layout check (gutter position,
  code-pane bounds) to pick the crop for new videos.
- `scripts/run_video.py` — full pipeline (transcribe → group → OCR+consensus →
  save) on an already-downloaded video. Output: `results/<video_id>/groups.json`.

## Step 2 — stress test on the full aDWDJrACs7s frame batch

300 extracted frames, 41 stable-run groups, 129 frames OCR'd (paddle+tesseract
ensemble, corrected crop). All 41 groups completed with zero errors in 23 min.

| grp | frames | t (s) | pipeline lines | tesseract-only consensus lines |
|----:|-------:|------:|---------------:|-------------------------------:|
| 0 | 7 | 223–229 | 19 | 18 |
| 1 | 8 | 230–249 | 17 | 16 |
| 2 | 8 | 250–278 | 24 | 22 |
| 3 | 7 | 279–285 | 15 | 14 |
| 4 | 1 | 286 | 83 | 14 |
| 5 | 1 | 287 | 98 | 21 |
| 6 | 2 | 288–289 | 28 | 25 |
| 7 | 1 | 290 | 54 | 19 |
| 8 | 3 | 291–293 | 24 | 25 |
| 9 | 8 | 294–344 | 18 | 15 |
| 10 | 1 | 345 | 75 | 19 |
| 11 | 5 | 346–350 | 20 | 20 |
| 12 | 5 | 351–355 | 7 | 1 |
| 13 | 2 | 356–357 | 4 | 1 |
| 14 | 2 | 358–359 | 8 | 4 |
| 15 | 1 | 360 | 121 | 1 |
| 16 | 1 | 361 | 129 | 3 |
| 17 | 8 | 362–379 | 10 | 3 |
| 18 | 8 | 380–413 | 23 | 22 |
| 19 | 8 | 414–451 | 26 | 28 |
| 20 | 8 | 452–459 | 25 | 24 |
| 21–35 | 1 each | 460–474 | 2–10 | 0–2 |
| 36 | 5 | 475–479 | 24 | 22 |
| 37 | 3 | 480–482 | 23 | 23 |
| 38 | 2 | 483–484 | 21 | 18 |
| 39 | 1 | 485 | 94 | 24 |
| 40 | 8 | 486–522 | 29 | 29 |

## Step 2 finding that matters: consensus LOSES field names on the real pipeline

The headline claim in `docs/consensus_eval.md` (28/30 struct fields recovered
with clean text) reproduces **only when the consensus input is
tesseract-only** (glued shape: `47 string SymbolName;` as one OCR line). The
real pipeline feeds consensus the **paddle+tesseract ensemble** output, where
PaddleOCR emits word-level boxes (`47`, `string`, `SymbolName;` as three
separate entries). `consensus.pair_line_numbers`'s "separate" shape support
attaches the gutter number to only the **first** following entry, so every
field name after the type becomes "unanchored" and is **dropped** by
`_to_code_block_group` (which deliberately discards unanchored text).

Verified three independent ways:
1. Ensemble diag (`/tmp/ensemble_diag.py`, see also `results/stress/group_40.json`):
   93 OCR entries on frame t_0520 → 29 anchored, **34 unanchored — including
   every struct field name** (`SymbolName;`, `LastMainTFUpdate;`, …).
2. Tesseract-only consensus (`results/stress_verify/group_40.json`) recovers
   **25/26 documented struct fields with type+name intact** (lines 44–73,
   gutter numbers 46–70, 72, 73 — matches the docs' claim).
3. The real pipeline output for group 40 contains **0/26 fields with
   type+name intact** — 25 of its 29 lines are bare type keywords
   (`string`, `datetime`, `int`, …). A 4×-upscale re-OCR of the actual frame
   pixels (`/tmp/pixel_check.py`) proves the field names ARE in the image.

Direction of the regression: group 39 (single frame, same region) keeps the
field names (94 lines incl. `SymbolName;`, `closePrice;`, …) because the
single-frame path joins every OCR line; the multi-frame consensus path
**removes** them. So consensus, as wired, is strictly worse than the old
single-frame path on this video's struct region.

This is not an environment artifact: it is a reproducible interaction between
the ensemble merge (paddle word-boxes) and the separate-shape pairing logic.
The docs' wiring verification explicitly used `create_engine('tesseract')`
(consensus_eval.md "Follow-up" section), not the pipeline's actual engine list
`[paddle, tesseract]`.

### FIXED after this report (see `docs/consensus_eval.md` "Bug found by the
multi-video stress test, fixed after the merge")

`consensus.pair_line_numbers` now keeps the gutter number anchored across
*all* consecutive content entries until the next standalone number, and joins
them into one pair — so paddle word-boxes `("47", "string", "SymbolName;")`
produce `(47, "string SymbolName;")` instead of dropping the tail. Verified
against the real ensemble output on frame t_0520 (`47: string SymbolName;`,
`48: datetime LastMainTFUpdate;`, … recovered) and by the full-suite
regression tests (`test_pair_line_numbers_joins_paddle_word_boxes_under_one_number`
et al.). The tables below were produced *before* this fix; re-run
`scripts/stress_test.py` on the current `main` to see the post-fix numbers
(the new results are in `docs/` — see the 300-frame re-run recorded after
the fix).

## Step 3 — two additional videos (Mr. CapFree Parts 3 and 4)

Both are the same MQL5/MetaEditor series as the first video. Both show gutter
line numbers (Part 3: 16–34 numbers per sampled frame; Part 4: 8–25) and the
same layout (sidebar x≈0–350, gutter x≈363–382, code pane x≈443–1850,
y≈90–850) — so the corrected crop `345,90,1575,760` carries over without
adjustment. Both ran end-to-end with **zero errors** at the documented sample
rate (0.05 fps = 1 frame/20 s):

| video | groups | multi-frame (consensus) | single-frame | OCR frames | total lines | wall |
|-------|-------:|------------------------:|-------------:|-----------:|------------:|-----:|
| dV6-h17m6Ag (Part 3) | 183 | 40 | 143 | 243 | 8380 | 56.3 min |
| ruk3gkZGNkc (Part 4) | 177 | 32 | 145 | 219 | 8806 | 51.6 min |

Per-region numbers for every group are in `results/<video_id>/groups.json`
(frames, t-range, source incl. `consensus xN`, line count, full text).
Spot-checked groups (re-OCR of the actual frame with psm 11 / 3× scale /
extended margin, `scripts` + `/tmp/linecheck.py`):

| video | group | frames | pipeline lines | pipeline lines matching frame OCR |
|-------|------:|-------:|---------------:|----------------------------------:|
| dV6-h17m6Ag | 20 | 2 | 18 | 9/12 checked |
| dV6-h17m6Ag | 97 | 2 | 18 | 12/17 checked |
| dV6-h17m6Ag | 8 | 1 | 129 | 54/76 checked |
| ruk3gkZGNkc | 20 | 1 | 60 | 50/57 checked |
| ruk3gkZGNkc | 97 | 2 | 14 | 11/11 checked |
| ruk3gkZGNkc | 8 | 2 | 16 | 10/14 checked |

(Line-match counting is substring-based on normalized text; unmatched lines
are mostly long lines truncated differently at the crop edge, not
fabrications.)

## Explicit generalization caveats

1. **Consensus rarely engages on real full-video runs.** At the documented
   sample rate (1 frame/20 s), 143/183 groups (Part 3) and 145/177 (Part 4)
   are single-frame — no redundancy for consensus to merge. The 1-fps frame
   batches in `frames/` are much denser than what the pipeline samples by
   default, so the consensus benefit shown in step 2 only materializes when
   the sample rate is raised (or the frame batch is pre-extracted densely).
2. **The ensemble/pairing bug (step 2 finding) applies to the new videos
   too** — every multi-frame group in Parts 3/4 goes through the same
   paddle+tesseract ensemble input, so field names after a type keyword are
   dropped there as well (visible in `results/*/groups.json`: consensus
   groups consistently produce ~15–25 lines vs 45–60 for single-frame groups
   of the same region).
3. **No-gutter / different-theme videos were NOT tested** — all three videos
   are the same uploader, same IDE theme, same window layout, all with a
   visible gutter. The no-consensus fallback path (no line numbers detected)
   is still exercised by the 145+ single-frame groups (they fall back to
   frame-0 reading), but a genuinely different theme/font/zoom was not
   available to test. This is the honest boundary of what the 3-video sample
   can claim.

## Repro

```bash
# full frame batch of aDWDJrACs7s through the real grouped path
.venv/Scripts/python scripts/stress_test.py        # -> results/stress/
.venv/Scripts/python scripts/verify_stress.py       # -> results/stress_verify/
# layout + full run for a new video
.venv/Scripts/python scripts/inspect_video.py <video_id>
.venv/Scripts/python scripts/run_video.py <video_id>  # -> results/<id>/groups.json
```
