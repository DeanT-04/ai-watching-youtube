# ytextract — Architecture, build log & machine profile

Internals that don't belong in the README: how the pipeline is wired, the
build history (phases 0–9), the development-machine profile, configuration
reference, and dependency rationale.

## Pipeline layout

```
src/ytextract/
  cli.py              orchestration: one command runs phases 2–8 with progress logging
  config.py           every tunable, env-driven (YTEXTRACT_*), no hardcoded magic numbers
  logging_setup.py    one logger, idempotent setup
  subprocess_utils.py safe wrapper for every external binary call (timeout, utf-8-safe)
  resource_monitor.py CPU throttle (wait_if_busy) + bounded_map worker cap
  downloader.py       yt-dlp (H.264 preferred, AV1 excluded) + ffmpeg 16 kHz wav + metadata
  transcriber.py      faster-whisper (CPU + int8), lazy import, timestamped segments
  keyframes.py        OpenCV sampling at configurable rate + stable-run grouping (SSIM)
  crop.py             bounds-checked config crop + Lanczos upscale
  ocr/                OcrEngine protocol; paddle_engine (primary), tesseract_engine
                      (secondary, via run_command + TSV), ensemble merge + fallback
  repair.py           ast.parse validation behind a VALIDATORS registry (Python today)
  storage.py          Storage protocol + LocalStorage (transcript.json, code_blocks.json)
tests/                unit tier (100% coverage gate, fully mocked) + integration tier
data/raw/<id>/        downloaded media (gitignored)
data/output/<id>/     results: transcript.json + code_blocks.json (gitignored)
frames/<id>/          sampled frames (tracked; e.g. first 10 min at 1 frame / 5 s)
results/<id>/         tracked copies of pipeline output for review (RUN.md + JSON)
scripts/pending-e2e.sh  re-runs the real-network checks when connectivity returns
```

## Machine profile

Recorded from the Phase 0 system audit (Section 4 of `docs/master-build-prompt.md`):

| Item | Value |
| --- | --- |
| OS | Windows (no WSL); PowerShell 5.1+; git-bash for agent tooling |
| Python | 3.12.4 (`python` on PATH) |
| pip | 24.0 |
| git | 2.52.0.windows.1 |
| ffmpeg | `C:\Users\Deano\scoop\shims\ffmpeg.exe` (scoop) |
| tesseract | `C:\Users\Deano\scoop\shims\tesseract.exe` (scoop) |
| gh | installed, **not authenticated** → local-merge mode, no PRs |
| Logical CPUs | 8 → worker cap defaults to `max(1, 8 - 1) = 7` |
| RAM | 13.9 GB |
| GPU | AMD Radeon(TM) Vega 10 Graphics — present but **not used**; pipeline is CPU-only by design |
| Disk (C:) | 138.6 GB used / 337.5 GB free |

Consequences: default `max_workers` is 7 (never the full core count); Whisper
runs CPU + int8 (`tiny` model by default, override via config); merges are
local `git merge --no-ff` because `gh` is not logged in.

## Configuration reference (env vars, `YTEXTRACT_` prefix, `.env` supported)

| Variable | Default | Meaning |
| --- | --- | --- |
| `MAX_WORKERS` | `max(1, cores-1)` | thread cap for parallel work |
| `CPU_THRESHOLD_PERCENT` / `CPU_POLL_INTERVAL` / `CPU_SLEEP_SECONDS` | 85 / 0.5 / 2.0 | CPU throttle |
| `SAMPLE_RATE_FPS` | 0.5 | keyframe sampling rate |
| `SIMILARITY_THRESHOLD` | 0.98 | stable-run grouping threshold |
| `CROP` | *(none)* | `x,y,width,height` code-pane crop — the main code-accuracy lever |
| `UPSCALE_SCALE` | 2 | Lanczos upscale factor before OCR |
| `WHISPER_MODEL_SIZE` / `WHISPER_DEVICE` / `WHISPER_COMPUTE_TYPE` / `WHISPER_LANGUAGE` | tiny / cpu / int8 / *(auto)* | faster-whisper settings |
| `OCR_CONFIDENCE_THRESHOLD` | 0.5 | min confidence for secondary-engine lines |
| `YTDLP_TIMEOUT` / `FFMPEG_TIMEOUT` | 3600 / 600 | external-tool timeouts (s) |
| `LOG_LEVEL` | INFO | logging level |

## Dependency rationale (why the pins in `requirements.txt` / `pyproject.toml`)

- `paddleocr==2.7.3` + `paddlepaddle==2.6.2` — 2.x line: the engine targets the
  classic `PaddleOCR(...).ocr(img, cls=True)` API.
- `numpy==1.26.4` — paddlepaddle 2.6.2's compiled extensions use the numpy 1.x
  C ABI (numpy 2.x → "module compiled against ABI version …" crash).
- `scipy==1.13.1` — scipy 1.18 references the removed numpy `np.long` under
  numpy 1.26.
- `opencv-python==4.6.0.66` — paddleocr 2.7.3 hard-requires `opencv-python`
  (not headless); keeping a single cv2 provider avoids the namespace-package
  breakage of two `cv2/` installs.
- `yt-dlp`, `faster-whisper`, `psutil`, `python-dotenv` — pinned to installed
  versions; transitive deps are pip-resolved.

## Build status (phases 0–9, all complete)

- **Phase 0 — Scaffolding: done.** `.gitignore`, package skeleton, README,
  pinned requirements, venv, `pytest` green.
- **Phase 1 — Foundation utilities: done.** `setup_logging()`, env-driven
  `Config`, `run_command()` wrapper (timeouts, clean errors, utf-8-safe),
  `wait_if_busy()` / `bounded_map()`. 100% coverage.
- **Phase 2 — Download module: done.** `download_video()` via `run_command`:
  yt-dlp merge (H.264 preferred, AV1 excluded — OpenCV can't decode AV1) +
  `--write-info-json`, ffmpeg 16 kHz mono wav, metadata parsing. 100% coverage;
  real-download sanity check passed.
- **Phase 3 — Audio transcription: done.** `transcribe()` wraps faster-whisper
  (CPU + int8, config-driven); timestamped segments. 100% coverage; real
  inference exercised in the integration tier and the real e2e.
- **Phase 4 — Keyframe selection: done.** `sample_frames()` + `select_keyframes()`
  (SSIM-style grouping, last frame per stable run). 100% coverage.
- **Phase 5 — Crop & preprocessing: done.** `crop_region()` (bounds-checked) +
  `upscale()` (Lanczos). 100% coverage.
- **Phase 6 — OCR ensemble: done.** PaddleOCR (primary) + Tesseract (secondary,
  `run_command` + TSV parsing) behind `OcrEngine`; documented merge policy;
  fallback-on-engine-failure; engines built once per run. 100% coverage.
- **Phase 7 — Syntax-validated repair: done.** `repair()` via `ast.parse`
  behind `VALIDATORS` registry; line/column reporting. 100% coverage.
- **Phase 8 — Local storage layer: done.** `Storage` protocol + `LocalStorage`;
  round-trip, corrupt/missing-file, UTF-8 cases. 100% coverage.
- **Phase 9 — CLI orchestration: done.** `ytextract <url>` / `python -m ytextract`;
  fully-mocked e2e wiring test; subprocess smoke tests; integration tier
  (real tesseract OCR + real cached-whisper pipeline on synthetic media).

Verification gates: `pytest --cov=src --cov-report=term-missing
--cov-fail-under=100` (132 tests, 100%), `ruff check src tests`, `ruff format
--check src tests`.

## Real-network checks (executed 2026-08-10 after connectivity returned)

Both gates from the build plan ran against the first URL in `yt-urls.txt`
(`https://youtu.be/aDWDJrACs7s` — "Build This $2800 Forex Trading Bot From
Scratch (Full Code), Part 2", 82.5 min, uploader Mr. CapFree):

- **Phase 2 real-download sanity check: PASSED** (~105 s). Produced
  `data/raw/aDWDJrACs7s/{aDWDJrACs7s.mp4, aDWDJrACs7s.wav, aDWDJrACs7s.info.json}`
  with correct title/uploader metadata. One real bug found and fixed here: the
  default format string picked an AV1 stream (OpenCV cannot decode it) — the
  downloader now prefers H.264 (`vcodec^=avc1`) and excludes AV1.
- **Phase 9 real end-to-end run: PASSED** (~57 min total wall; config: Whisper
  `tiny.en` cached model, sample rate 0.05 fps). Download ~3 min → transcribe
  751 segments / `en` ~9 min → 166 keyframes from 248 sampled frames ~13 min →
  OCR (PaddleOCR+Tesseract ensemble) over all 166 keyframes ~32 min → validate
  → save to `data/output/aDWDJrACs7s/`.
- **Crop-enabled re-run (2026-08-10, same URL, `YTEXTRACT_CROP=0,90,1382,380`,
  ~39 min):** code-carrying blocks went from ~0% to **64% (104/162)** —
  the MQL5 file header (`#property copyright "Mr CapFree"` …) and the
  `SymbolInformation` struct are readable in the 3:43–8:43 window. Remaining
  noise: glued line numbers, per-field fragmentation, `O/l/1` confusions.
  Tracked copies for review: `results/aDWDJrACs7s/` (+ matching frames in
  `frames/aDWDJrACs7s/`). 0/162 blocks parse as Python — the validator is
  Python-only by design and this video is MQL5.
- **Rough accuracy impression (honest):** transcript quality is high (clean,
  coherent English). Code extraction with the *default* (no crop) is poor —
  0/165 non-empty blocks parse as Python, because (a) the video is MQL5
  (C-like), which the Python-only validator flags by design, and (b) full-frame
  OCR captures UI chrome rather than the editor's small code text. With a crop
  region over the code pane (Phase 5 config), tesseract read genuine code lines
  (`return(INIT_SUCCEEDED);`, `void OnDeinit(const int reason) {`, `void
  OnTick() {`) at 67–96% confidence — the configurable crop is the intended
  accuracy lever.

## Pending checks

- Re-running the real checks for the other URLs in `yt-urls.txt`:
  `bash scripts/pending-e2e.sh` (waits for youtube.com, then downloads and
  runs the full pipeline, logging to the console). The first build-session
  attempt was blocked by a youtube.com outage (18:13–20:34 local); the checks
  were completed manually once connectivity returned (see above).
