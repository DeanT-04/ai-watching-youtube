# ytextract — sample run results: `aDWDJrACs7s`

Pipeline output for the first URL in `yt-urls.txt`:
`https://youtu.be/aDWDJrACs7s` — "Build This $2800 Forex Trading Bot From
Scratch (Full Code), Part 2" (82.5 min, uploader **Mr. CapFree**). The video is
**MQL5** (MetaTrader 5) code shown in the MetaEditor IDE.

These files are tracked copies of the run output so a reviewer can inspect
"where the project is at" without regenerating anything. Raw (gitignored)
copies live in `data/output/aDWDJrACs7s/`.

## Run config

| Setting | Value |
| --- | --- |
| Command | `.venv\Scripts\python.exe -m ytextract <url>` |
| `YTEXTRACT_CROP` | `0,90,1382,380` (MetaEditor code pane, 1080p) |
| `YTEXTRACT_WHISPER_MODEL_SIZE` | `tiny.en` (cached, offline) |
| `YTEXTRACT_WHISPER_LANGUAGE` | `en` |
| `YTEXTRACT_SAMPLE_RATE_FPS` | `0.05` (1 frame / 20 s) |
| Date | 2026-08-10, ~39 min wall |

## Results at a glance

- **Transcript**: 751 segments, language `en` — high quality (clean English,
  e.g. "We're building the trading robot. It's a multi-currency grid
  management system…").
- **Code blocks**: 166 saved (162 non-empty).
  - **104 / 162 (64%) contain real-code tokens** (`#property`, `struct`,
    `void On`, `int `, `string `, `return`, `datetime`, `ArrayResize`, …).
  - **0 parse as Python** (`valid: false` everywhere) — expected and by
    design: the validator is Python-only (`repair.VALIDATORS`), and this video
    is MQL5.
- **The 3:43–8:43 window** (matching `frames/aDWDJrACs7s/t_0223..t_0522.png`)
  contains the readable MQL5 file header and the `SymbolInformation` struct:

  ```
  #property copyright "Mr CapFree"
  #property link   https://www.MrCapFree.com
  #property version "1.00"
  #property strict
  ...
  struct SymbolInformation {
    string SymbolName;
    datetime LastMainTFUpdate;
    datetime LastOPOTFUpdate;
    int    UpdateCounter;
    bool   CanBuy;
    bool   CanSell;
    datetime CurrentBarTime;
    datetime PreviousBarTime;
    double closePrice;
    bool   GridOpened;
  ```

- **Comparison vs. the earlier no-crop run** (same video, same engines):
  the crop raised code-carrying blocks from ~0% (pure editor chrome) to 64%.
  Remaining noise: line numbers glued to code (`N 6 #property`), per-field
  line fragmentation in the struct, `O`/`l`/`1` confusions (`boo1`, `LastoPotFUpdate`),
  and editor toolbar text (`Compile`, `History`) caught at the crop's top edge.

## Files

- `transcript.json` — timestamped speech segments.
- `code_blocks.json` — per keyframe: `timestamp`, `text`, `language`,
  `source` (engine), `valid` (Python-parseable), `issues` (validation
  problems with line numbers).

## How to read `code_blocks.json`

Each block's `timestamp` is the on-screen second; the matching frame is
`frames/aDWDJrACs7s/t_<timestamp>.png`. Blocks are NOT deduplicated across
stable runs — expect repeats while a code region stays on screen.
