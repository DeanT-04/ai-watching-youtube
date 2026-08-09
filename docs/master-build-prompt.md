# Master Build Prompt — YouTube Tutorial Code Extractor

## 0. Role and objective

You are an autonomous coding agent building a local, CPU-only Python tool that takes a
YouTube tutorial URL and produces two things saved to disk: (1) an accurate transcript
of the spoken audio, and (2) accurately extracted, near-verbatim code shown on screen
during the video. No YouTube Data API is used — video/audio access goes through
`yt-dlp` only. This is a personal productivity tool, not a public redistribution service.

Read this entire document before writing any code. It is meant to be the only planning
conversation we have — build autonomously from here using the phase plan in Section 8,
and only pause for the "stop and ask" triggers in Section 2.

## 1. Ground truth about the machine

Do not assume anything about the environment. Before writing setup code, run the system
audit in Section 4 and record the results in `README.md` under a "Machine profile"
section. Every later decision (parallelism, batch size, model size) should reference
these numbers rather than a guess.

Known up front: OS is **Windows, no WSL**. Shell is PowerShell (assume PowerShell 5.1+
unless the audit finds otherwise). CPU-only — no NVIDIA GPU should be assumed available;
verify this rather than trust it blindly.

## 2. Guardrails (deliberately moderate — not maximally strict, not absent)

Proceed autonomously, without asking, for anything that is: reversible, contained
inside the project folder, and doesn't cost money or touch shared system state. This
includes writing/editing/deleting files inside the repo, creating branches, committing,
running tests, installing packages inside the project's virtual environment, and
refactoring your own code.

**Stop and explicitly ask the user before doing any of the following:**

- Installing anything outside the project's virtual environment (system-wide pip
  installs, winget/choco installs, changing PATH, changing PowerShell execution policy)
- Running `git push --force` / `--force-with-lease`, rewriting history, or deleting
  branches on the remote
- Pushing directly to `main` (all work lands on `main` via a merge from a feature
  branch, never a direct commit)
- Deleting or moving any file outside the project directory
- Signing up for, or entering credentials for, any paid API or third-party service
- Downloading content the user doesn't have the right to download (private/paywalled
  videos, DRM circumvention) — this tool only handles publicly accessible YouTube URLs
  the user provides directly
- Expanding scope beyond the phase plan in Section 8 — if a phase seems to be missing
  something important, say so and ask, don't just add it unilaterally

Everything else: use your judgement, make the call, note the decision briefly in the
relevant commit message or README, and keep moving. Don't ask permission for things
like "should I name this variable X or Y."

## 3. Legal / safety baseline

- Only ever fetch videos via the URL the user explicitly provides. Never scrape search
  results, playlists, or channels beyond what's asked.
- Never attempt to bypass age gates, private-video restrictions, or any access control.
- Downloaded video/audio and extracted code stay local. Nothing gets uploaded,
  published, or sent to a third-party API unless the user asks for that later.
- Don't write or call any code whose purpose is credential theft, scraping personal
  data, or anything resembling malware — none of that is needed here and none of it
  should appear even as a "helper utility."

## 4. System & software audit (do this first, before touching git or venv)

Run these in PowerShell and record the results in the README's "Machine profile"
section:

```powershell
python --version
pip --version
git --version
where.exe ffmpeg
where.exe tesseract
(Get-CimInstance Win32_ComputerSystem).NumberOfLogicalProcessors
(Get-CimInstance Win32_ComputerSystem).TotalPhysicalMemory
Get-CimInstance Win32_VideoController | Select-Object Name
Get-PSDrive C | Select-Object Used,Free
where.exe gh
```

If `ffmpeg` or `tesseract` are missing: these are not Python packages, so don't
silently try to install them. Stop, tell the user what's missing and how it's
typically installed on Windows (e.g. `winget install Gyan.FFmpeg`), and wait — don't
install system software without confirmation (see Section 2).

If a GPU is found, note it in the README but keep the default pipeline CPU-only unless
the user asks to use the GPU later — this stays a CPU-only project for now, keep it
simple.

## 5. Virtual environment (Windows-safe)

Create it once:

```powershell
python -m venv .venv
```

Never rely on activating the venv in a script — PowerShell execution policy can
silently block `Activate.ps1` and produce confusing failures. Instead, always call the
venv's interpreter directly, in every command and every script you write:

```powershell
.venv\Scripts\python.exe -m pip install -r requirements.txt
.venv\Scripts\python.exe -m pytest
.venv\Scripts\python.exe -m mypackage
```

This one convention avoids an entire category of "works on my machine, fails on
theirs" Windows problems. Use it everywhere, including any docs you write for the user.

Keep `requirements.txt` (runtime) and `requirements-dev.txt` (pytest, pytest-cov,
ruff/black) separate, and pin versions.

## 6. CPU / resource safety

Detect the logical core count from the Section 4 audit. Default worker/thread caps to
`max(1, cores - 1)` — never `cores`, never unbounded. Process one video at a time in
v1 — no concurrent video pipelines yet; that's a later optimization, not a foundation
requirement.

For any CPU-heavy loop (OCR over many frames, batch Whisper calls), periodically check
`psutil.cpu_percent(interval=0.5)` and insert a short sleep if usage is pegged above
~85% for a sustained stretch, so the machine stays usable for other work. Make these
numbers config values, not hardcoded magic numbers, so the user can tune them without
touching logic.

## 7. Git / GitHub workflow ("trees" = branches, used deliberately)

- The user already has a GitHub repo connected. First action: `git remote -v` to
  confirm the remote — don't assume its name or URL.
- Never commit directly to `main`. Every phase (or sub-piece of a phase) gets its own
  branch: `phase-0-scaffolding`, `phase-1-foundation`, `phase-2-download`, etc.
- Work in bite-sized commits: one logical unit (a function/module plus its tests) per
  commit, not a giant end-of-day dump. Commit message format:
  `phase-N: short description of what changed and why`.
- Before merging a branch into `main`: all tests must pass, the coverage bar from
  Section 9 must be met, and the README must be updated (Section 10). Only then merge
  (`git merge --no-ff`) and push `main`.
- Push the feature branch to the remote at each checkpoint, not just at the end of a
  phase, so work isn't sitting only-local for hours. Never force-push.
- If the GitHub CLI (`gh`) is available (checked in Section 4), open a PR instead of
  merging locally in silence, so there's a visible record. Mention in the README which
  mode (PR vs local merge) is being used.

## 8. Build order — foundation first

Like framing a house before hanging windows: do not skip ahead. Each phase must be
fully working, tested, committed, and merged before the next phase starts. Each phase
has a "definition of done" below — treat it as a hard gate.

**Phase 0 — Scaffolding**
- `git init` (or confirm existing), verify remote, create `.gitignore` (`.venv/`,
  `__pycache__/`, downloaded media, model caches, `.env`, coverage reports, IDE folders)
- Folder skeleton: `src/<package_name>/`, `tests/`, `README.md`, `requirements.txt`,
  `requirements-dev.txt`, `config.py` or `.env.example`
- README with: project description, machine profile (Section 4), setup instructions,
  status section (starts as "Phase 0 in progress")
- Done when: a fresh clone + venv + `pip install` + `pytest` (even with zero tests)
  all work cleanly on a second check.

**Phase 1 — Foundation utilities**
- Logging setup (one place, used everywhere — no scattered `print()`)
- Config loader (reads `.env`/`config.py`; gives every later module its tunables: max
  workers, similarity threshold, crop coordinates, etc.)
- A safe subprocess wrapper (used for every later call to `yt-dlp`/`ffmpeg`/`tesseract`)
  that enforces timeouts and captures stderr/stdout cleanly, instead of every module
  calling `subprocess.run` directly
- Resource monitor helper wrapping the Section 6 `psutil` check, as a reusable
  function/decorator
- Tests for all of the above at 100% — this is pure logic, no network/OCR involved, so
  there's no excuse not to hit full coverage here

**Phase 2 — Download module**
- Thin wrapper around `yt-dlp` that takes a URL and returns local paths to video,
  audio, and metadata (title, description, uploader)
- Store downloads under `data/raw/<video_id>/`
- Unit tests mock `yt-dlp` entirely — no real network calls in the test suite; cover
  both a successful download and a failed/invalid-URL case
- Done when: given any single public URL, the module produces the three local files
  with correct paths, with tests fully mocked and passing. As a real-world sanity
  check (not part of the coverage-gated suite), run it once against a URL from
  `yt-urls.txt`.

**Phase 3 — Audio transcription**
- `faster-whisper` wrapper, CPU + int8 by default (config-overridable); takes an audio
  path, returns a transcript with timestamps
- Tests use a tiny local fixture audio clip (a few seconds, checked into
  `tests/fixtures/`) or fully mock the whisper call for the unit-test tier; any slower
  "real" test gets marked `@pytest.mark.integration` so it doesn't run by default and
  doesn't block the coverage gate

**Phase 4 — Keyframe selection**
- Frame extraction (OpenCV) at a configurable sample rate
- SSIM or pixel-diff based grouping into "stable runs," keeping the last frame per run
- Pure-logic-testable: feed synthetic frame arrays in tests, no real video needed for
  the unit tier

**Phase 5 — Crop & preprocessing**
- Configurable crop region (coordinates in config, not hardcoded) + Lanczos upscale
  before OCR
- Tests with small synthetic/fixture images

**Phase 6 — OCR ensemble**
- PaddleOCR primary, Tesseract secondary (via the Phase 1 subprocess wrapper), both
  behind a common interface so a third engine can be added later without touching
  callers
- Tests mock both engines' outputs and verify the ensemble/merge logic, not the
  engines themselves

**Phase 7 — Syntax-validated repair**
- Take raw OCR text for a code block, attempt to parse it with the appropriate
  language validator (start with Python's `ast.parse`, structured so more languages
  can be added later), flag/report failures with line numbers
- This is the accuracy-critical module — give it the most thorough tests of any phase,
  including deliberately malformed inputs

**Phase 8 — Local storage layer**
- Save final output (transcript + code blocks with timestamps) as structured local
  files (one JSON or Markdown file per video under `data/output/<video_id>/`)
- Build this behind a small repository-style interface (`save_result()`,
  `load_result()`) so swapping local files for a real database later means writing a
  new implementation of that interface, not rewriting every caller — don't build the
  database now, that's explicitly future work

**Phase 9 — CLI orchestration**
- One command that takes a URL and runs Phases 2–8 in order, with clear progress
  logging
- End-to-end test using mocks for every external call (download, whisper, OCR) so the
  full pipeline's wiring is verified without needing real network/OCR time
- Once that passes, run one real end-to-end pass against a URL from `yt-urls.txt` as
  the actual proof the pipeline works, and note the result (timing, rough accuracy
  impression) in the README's status section

**Explicitly future work — do not build yet, just leave the interface open for it:**
- Vector database / embeddings for RAG retrieval
- Multi-video batch processing / concurrency
- Any cloud/API-based OCR or LLM-based repair step

## 9. Testing & coverage

Target: 100% coverage on all first-party code in `src/`, achieved honestly, not by
gaming it:

- Every module that touches the outside world (network, subprocess, filesystem beyond
  the project, OCR/whisper libraries) must wrap that call in a thin, separately
  testable function — tests mock that function, not the real network/OCR call. This is
  what makes 100% achievable without a flaky, slow test suite.
- A small number of true integration tests (real download, real OCR on a tiny fixture)
  are fine to have, but mark them `@pytest.mark.integration`, exclude them from the
  coverage-gated default run, and document how to run them manually in the README.
- The project root already contains `yt-urls.txt` — three real YouTube URLs the user
  has picked out ahead of time. These are for the manual/integration tier only, not
  for the mocked unit-test tier. Once a phase is ready for a real, non-mocked sanity
  check (e.g. confirming Phase 2's downloader against an actual video, or a full
  Phase 9 end-to-end run), read a URL from that file rather than asking the user for
  one or inventing a placeholder. Don't commit any large files these test runs produce
  (raw video/audio, model caches) — `.gitignore` from Phase 0 already excludes
  `data/raw/` and `data/output/` for this reason.
- Coverage command:
  `.venv\Scripts\python.exe -m pytest --cov=src --cov-report=term-missing --cov-fail-under=100`
- Genuinely untestable defensive code may use `# pragma: no cover`, but every use needs
  a one-line comment explaining why — this should be rare, not a pattern.

## 10. README — keep it current, always

After every commit that changes behavior, update the README's "Status" section: which
phase is done, what's in progress, what's next. This is the single source of truth for
anyone — including a future agent session — picking this project back up; it should
never require reading the whole codebase to figure out where things stand. Also keep
current: setup instructions, how to run the CLI, the machine profile from Section 4,
and a short "known limitations" list.

## 11. Code style

- Split by responsibility: one module = one job (downloader, transcriber, keyframe
  selector, cropper, OCR engine, repairer, storage, CLI). No file should be doing three
  unrelated things — if a file starts sprawling, that's a signal to split it.
- Comment only where the *why* isn't obvious from the code itself — not on every line,
  not restating what a line already says. A one-line docstring on every public
  function/class explaining its purpose is good; narrating obvious logic line-by-line
  is noise.
- Type hints on function signatures throughout — this is CPU-bound Python gluing
  several libraries together, and type hints catch interface mistakes cheaply.

## 12. Working order, always

Start simple, get each phase working end-to-end in its crudest form, then improve.
E.g. Phase 6's first working version can be PaddleOCR alone with no ensemble voting —
get the pipe fully connected first, then come back and add the Tesseract cross-check
and multi-frame voting as a follow-up commit once the foundation is proven. Don't
gold-plate an early phase before the whole pipeline runs end to end once. Foundation →
plumbing → accuracy refinement, in that order.
