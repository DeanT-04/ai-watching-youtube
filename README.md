# ytextract — YouTube Tutorial Code Extractor

A local, CPU-only Python tool that takes a YouTube tutorial URL and produces two
things saved to disk:

1. an accurate **transcript** of the spoken audio, and
2. accurately extracted, **near-verbatim code** shown on screen during the video.

No YouTube Data API is used — video/audio access goes through `yt-dlp` only.
This is a personal productivity tool, not a public redistribution service.
Everything runs offline once the video is downloaded; nothing is uploaded.

Built from the [master build prompt](docs/master-build-prompt.md) in ordered,
individually-merged phases (see Status).

## Machine profile

Recorded from the Phase 0 system audit (Section 4 of the build prompt) on the
development machine:

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

Consequences of the profile:

- Default `max_workers` is 7 (never the full core count).
- Whisper runs CPU + int8 (`tiny` model by default; override via config).
- Merge mode is **local `git merge --no-ff`** because `gh` is not logged in;
  if `gh auth login` is done later, the workflow switches to PRs.

## Setup

```powershell
python -m venv .venv
.venv\Scripts\python.exe -m pip install -r requirements.txt
.venv\Scripts\python.exe -m pip install -r requirements-dev.txt
```

Never activate the venv in scripts (PowerShell execution policy can silently
block `Activate.ps1`); always call the venv interpreter directly:

```powershell
.venv\Scripts\python.exe -m ytextract <youtube-url>
.venv\Scripts\python.exe -m pytest
```

Runtime vs. dev dependencies are split into `requirements.txt` and
`requirements-dev.txt`, both pinned. Configuration lives in `src/ytextract/config.py`
with overrides via `.env` (see `.env.example`); all values are environment-driven,
no hardcoded magic numbers.

## Running the CLI

Phase 9 wires Phases 2–8 into one command; until then there is no end-to-end CLI.

```powershell
.venv\Scripts\python.exe -m ytextract <youtube-url>
```

## Testing

Default run (mocked, no network, no real binaries — this is the coverage gate):

```powershell
.venv\Scripts\python.exe -m pytest --cov=src --cov-report=term-missing --cov-fail-under=100
```

Manual integration tests (real download / real OCR / real Whisper) are marked
`@pytest.mark.integration` and excluded from the default run. Run them explicitly with:

```powershell
.venv\Scripts\python.exe -m pytest -m integration -o addopts=""
```

Sanity checks against real videos use the URLs in `yt-urls.txt` (three
hand-picked public tutorials) — the unit-test tier never touches the network.

## Known limitations

- CPU-only: long videos transcribe slowly; keep the Whisper model small (`tiny`/`base`).
- OCR accuracy on low-resolution / stylized code is imperfect; Phase 7 repair
  validates Python syntax but cannot fix every OCR artifact.
- One video at a time; multi-video batch processing is explicitly future work.
- Windows-only code paths tested; macOS/Linux are not covered.

## Pending checks (blocked by the build-time network outage)

These gates exist in the build plan but could not be completed during the build
session because the machine could not reach github.com / www.youtube.com. They
are not fabricated — each will be re-run when connectivity returns:

- Phase 2 real-download sanity check against a URL from `yt-urls.txt`.
- Phase 9 real end-to-end run (download → transcribe → keyframes → OCR → repair
  → store) against a URL from `yt-urls.txt`, with timing + accuracy noted here.
- Push of the phase branches + main to `origin` (commits are local-only since
  the outage began).

## Status

- **Phase 0 — Scaffolding: done** (skeleton, `.gitignore`, README, pinned
  requirements, venv, `pytest` green).
- **Phase 1 — Foundation utilities: done** (central logging via
  `setup_logging()`, env-driven `Config`, safe subprocess wrapper
  `run_command()` with timeouts + clean errors, CPU throttle `wait_if_busy()`
  and `bounded_map()`; 100% coverage, ruff clean).
- **Phase 2 — Download module: done (unit tier)** — `download_video()` via the
  `run_command` wrapper: yt-dlp merge + `--write-info-json`, ffmpeg 16 kHz mono
  WAV extraction, metadata parsing; 100% coverage with yt-dlp/ffmpeg fully
  mocked. **Real-world sanity check (one URL from `yt-urls.txt`) blocked by a
  network outage during the build (github.com + www.youtube.com both
  unreachable); to be re-run when connectivity returns — see "Pending checks".**
- **Phase 3 — Audio transcription: done (unit tier)** — `transcribe()` wraps
  faster-whisper (CPU + int8, model size / device / compute type / language all
  config-driven); returns timestamped segments; 100% coverage with the Whisper
  call fully faked. Real inference is exercised only via the integration tier /
  Phase 9 e2e run (pending connectivity).
- Phase 4 — Keyframe selection: not started.
- Phase 5 — Crop & preprocessing: not started.
- Phase 6 — OCR ensemble: not started.
- Phase 7 — Syntax-validated repair: not started.
- Phase 8 — Local storage layer: not started.
- Phase 9 — CLI orchestration: not started.
