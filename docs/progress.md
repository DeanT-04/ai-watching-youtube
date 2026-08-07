# PROGRESS

Latest phase status at the top. One entry per phase, most recent first.

## `.ytr` storage-base (`store/`) — done (branch `feature/storage-base`)

- New single-file, queryable deliverable: after every successful `all` run, the
  output tree is packed into **one `.ytr` file per video** (`store/<video_id>.ytr`)
  plus a tiny `store/library.json` index — so an agent can "watch" the whole
  video in **2 commands**: `ytreconstruct store dump <id>` (ordered chunks +
  transcripts) and `ytreconstruct store frame <id> <NNNN> out.png` (pixel-identical
  PNG for OCR/vision). Plus `store query --grep [--range t1,t2]`, `store list`,
  `store verify`.
- **Format** (spec in `docs/storage-format.md`): our own schema on a stdlib zip
  container — `ytr/spec.json` index (metadata, ordered chunk spine, transcripts
  with absolute timestamps, whisper segment provenance, reconstruction/instructions
  seeds) + frames as **WebP lossless** stored byte-exact (zip `Store`, no
  recompression, so quality cannot degrade). Frames decode back to pixel-identical
  PNGs. Zero new dependencies (stdlib only).
- **Frames**: PNG → WebP lossless. Validated on 5 real 4K frames: round trip is
  pixel-identical (raw-RGBA `cmp`) and ~38% smaller (35–43%/frame). Encode is
  ~4.4 s/frame single-threaded (ffmpeg libwebp, no multithreading in this build);
  pack of the real 165-frame video took ~3 min, parallelized via `--jobs`.
- **Real-data validation** (`BL8TfsLk3WM`, 165 chunks, 1217.9 s):
  - `store pack` → `store/BL8TfsLk3WM.ytr` = **177.2 MiB** vs 292.9 MiB output
    tree / 292.7 MiB PNG frames → **-39.5%** in one file.
  - `store verify` OK: 165 chunks, 165 frames, all CRCs + members checked.
  - `store dump --json` cross-checked against `manifest.json`: all 165 chunk
    ids/starts/ends/source_ids identical; **165/165 transcripts byte-identical**
    to the tree's `transcript.txt`.
  - `store frame 0001` → pixel-identical to `output/.../chunks/0001/frame.png`.
  - `store query --grep "take profit" --range 60,120` → correct timestamped hits.
  - `scripts/integration.sh` PASS (auto-pack + verify + dump + list + frame on the
    synthetic video); `go build/vet/gofmt/test` clean.
- **Ergonomics**: `all` auto-packs as stage [6/6] (`--no-store` skips; pack is
  idempotent). `store/` is gitignored (root-anchored), wiped by `scripts/clean.sh`,
  and added to the AGENTS.md never-commit list. Docs updated: README,
  `docs/instructions.md` (2-command workflow), `docs/architecture.md`, AGENTS.md.
- **Not verified here**: OCR spot check (WinRT OCR engine unavailable in this
  shell — it fails on original PNGs too; pixel-identity makes OCR equivalence
  logical), `go test -race` (needs cgo/gcc, absent), non-Windows behavior.

---

## Question log + answer archive (`results/`) (done)

- New persistent Q&A archive at `results/<video_id>/`: `video.yaml` (metadata)
  + one `NNN.yaml` per question (zero-padded counter) holding question, answer,
  process log, evidence paths, status. Gitignored; deliberately not wiped by
  `scripts/clean.sh` (only `work/` + `output/` are).
- Same video URL (any form, `?si=` stripped) reuses the same folder — no
  duplicates. Same or similar question (normalized token-overlap, generous on
  rephrases) → "already answered" + exact YAML path + stored answer printed; no
  new file. Genuinely new question → next `NNN.yaml` written.
- Implemented as the `/ask` agent skill (project scope, `.reasonix/skills/ask/`,
  gitignored); the convention is versioned in `docs/instructions.md` (new
  "Answering questions about a video" section), README, and `docs/architecture.md`.
- Verified: skill mechanics (video-ID extraction, counter, similarity) tested
  against real inputs; all three workflow paths exercised against the real
  archive; archive seeded with the existing full-prompt answer as `001.yaml`
  plus a fresh take-profit answer as `002.yaml`; `go build/vet/gofmt/test` and
  `scripts/integration.sh` still green (no Go changes).

---

## Repo refactor + clean end-to-end re-run (done)

- Repo restructured to industry standard: root-level docs moved to `docs/`
  (`architecture.md`, `build-brief.md`, `instructions.md`, `progress.md`);
  added `LICENSE` (MIT), `CONTRIBUTING.md`, `.github/workflows/ci.yml`
  (hermetic build/vet/gofmt/test on Linux + Windows); `manifest` stage now
  copies `docs/instructions.md` (legacy root fallback + test).
- Universal `/setup` agent skill (global) installs all dependencies
  (Go, yt-dlp, ffmpeg, whisper-cli, model) — idempotent, cross-platform.
- **Clean end-to-end run** on the same URL after wiping `work/` + `output/`:
  identical results — 1731 raw scenes → 165 chunks, 1217.9 s, 186 whisper
  segments, zero missing files, `instructions.md` copied from its new `docs/`
  home. Full prompt re-OCR'd from fresh frames (chunks 240–302) and
  cross-checked against the fresh transcript; reconstruction written to
  `output/BL8TfsLk3WM/prompt_reconstructed.md`.

---

## The true test — full prompt extracted from the video (done)

Reconstructed the complete prompt the creator pasted into Claude (opus 4.8) from the on-screen
frames (chunks 240–287, t≈129–431s) using the built-in Windows OCR engine (scripts/ocr.ps1),
cross-checked against the spoken transcript. Result saved to `output/BL8TfsLk3WM/prompt_reconstructed.md`.
The prompt has 5 blocks: (1) MQL5 expert persona, (2) time-based range-breakout strategy
(03:00–06:00 range, buy/sell stops, SL at opposite side, risk-based lotsize, 18:00 close),
(3) adaptive take-profit = average of the last X maximum-favorable-excursion points with
simulated first X trades, (4) "ask before you code", (5) file path placeholder. Claude's
clarifying questions are also visible in the frames (t≈445s+). The workflow works end-to-end:
frames + transcripts were sufficient to reconstruct verbatim on-screen text the creator never
fully read aloud.

Also: base whisper models removed; `ggml-small-q8_0.bin` is now the only installed model and the
new default (verified A/B earlier: 14m07s vs 4m23s, clearly better transcripts).

---

# PROGRESS

Latest phase status at the top. One entry per phase, most recent first.

## Model A/B — base q8_0 vs small q8_0 (done, on the real video)

Timed on the same 1218 s audio, same settings (default 4 threads), single-pass:

| Model | Wall time | Segments | Words | Verdict |
|---|---|---|---|---|
| `ggml-base-q8_0` (default) | **4m23s** | 183 | 3432 | readable, meaning-accurate; stutters/fillers ("should should should we only need one"), mis-heard opener ("Here's the scenario I can make" vs actual "Hey, welcome back") |
| `ggml-small-q8_0` | **14m07s** (3.2x slower) | 186 | 3405 | fixes every visible base artifact: clean opener, no stutter duplication, cleaner function words |

No ground truth available for a true WER number; the comparison is qualitative side-by-side on matched segments. Base remains the speed default; small is one `--model` flag away (`~/.cache/ytreconstruct/whisper/ggml-small-q8_0.bin`, 264 MB — downloaded, verified). Deliverable rebuilt with small transcripts.

---

## Phase 12 — Real run on https://youtu.be/BL8TfsLk3WM (done)

**Full pipeline wall time: 22m42s** (1218 s / 4K / 1731 raw chunks), down from 60+ minutes and never-finishing before the overhaul. Breakdown:

- download: skipped (resumed from the earlier run's video.mp4/audio.wav)
- chunk (detection + 1731 keyframe frames + Go audio slices): ~12 min, of which scene detection (full 4K decode) is ~5.5 min
- dedupe (fast dHash, parallel): under a minute — **1731 raw chunks → 165 meaningful chunks** (keyframe-sharing runs merged; max 72 sources in one run)
- transcribe (single whisper pass, q8_0 model, `-l auto`): ~5 min — 183 segments
- manifest (parallel builds): ~3 min

**Deliverable verified:** `output/BL8TfsLk3WM/manifest.json` — 165 ordered chunks, total_duration 1217.9 s, all frame/transcript/meta files present, reconstruction.md + instructions.md seeded. Transcripts carry correct absolute timestamps (e.g. `[00:00:07.120 --> 00:00:13.040] take profit machine learning algorithm...`). 86/165 chunks contain speech; the rest are music/ambient passages with no whisper segments.

**Bug found by the real run:** whisper.cpp's `-oj` JSON reports `offsets` in **milliseconds**, not seconds — the first version dropped 182/183 segments (they fell outside every chunk range) and rendered a bogus `01:50:40` timestamp. Fixed with a /1000 conversion + regression test; transcribe now always re-partitions from `full.json` (the whisper pass is what resume skips).

**Machine politeness verified:** child processes (ffmpeg, whisper-cli, yt-dlp) confirmed running at BELOW_NORMAL priority (PriorityClass 16384 = 0x4000) on Windows; default `--jobs` = half the cores. The machine stays usable while the pipeline grinds.

## Next step

None — project complete. Remaining caveats for a human: `go test -race` needs cgo/gcc (absent here); non-Windows paths untested; whisper accuracy is base-model quality (swap `--model` for small/medium if better transcripts are needed).

---

## Phase 11 — Performance overhaul (done, benchmarked)

The first real run (1218 s, 4K, 1731 chunks) exposed three bottlenecks; all fixed:

| Bottleneck | Before | After | How |
|---|---|---|---|
| Transcription | 1731 whisper-cli model loads, 30+ min, not finished | **one pass: 6m42s for the whole video** (fp16; ~3.5 min with the q8_0 default) | Single `whisper-cli -oj` run over the full `audio.wav`, then segments partitioned into per-chunk transcripts (`transcripts/full.json` + `NNNN.txt`). Also fixed a latent bug: whisper-cli's own default language is `en` — we now always pass `-l auto` unless overridden. |
| Frame extraction | 2.5 s/chunk (decode forward from keyframe) | **0.41 s/chunk** | `-skip_frame nokey`: grab the first keyframe at/after each cut (encoders keyframe at cuts), with a plain-seek fallback for sparse-keyframe tails. |
| Scene detection | 5m38s full 4K decode | 5m30s (floor: detection needs every frame decoded; scale filters don't help — decode happens first) | Documented as the unavoidable single most expensive step. |
| Audio slices | 1731 ffmpeg processes | **~5 s total, zero processes** | Pure-Go byte-range slicing of the 16 kHz WAV (header-parsed; falls back to ffmpeg for non-standard WAVs). |
| Dedupe dHash | 60+ min CPU (per-pixel `At().RGBA()` on 4K) | **~1 min** | Raw pixel-buffer box-average (NRGBA/RGBA/Gray fast paths, ~30x) + parallel hashing (`--jobs`). |
| Manifest | sequential 3 GB frame copies | parallel with `--jobs` | Worker pool over chunk builds. |
| Machine politeness | default jobs = all cores, normal priority | **half-core default + BELOW_NORMAL priority** on Windows (children via `lowprio.Command`) | The user's machine stays usable while the pipeline runs. |

Also: default model switched to **`ggml-small-q8_0.bin`** (81 MB, ~2x faster than fp16 base, near-lossless), downloaded from hf-mirror.com (huggingface.co is blocked on this network).

**Verified:** all 7 test suites pass (rewritten for the new transcribe/chunk internals), `scripts/integration.sh` PASS with the optimized pipeline, `go build/vet/gofmt` clean. **Not verified here:** `go test -race` (needs cgo/gcc, absent on this Windows box).

## Next step

Re-run `https://youtu.be/BL8TfsLk3WM` end-to-end with the optimized pipeline (Phase 12), then final review.

---

## Phases 1–9 — built, tested, reviewable

Everything below is committed and green: `go build ./...`, `go vet ./...`, `gofmt -l .` clean; `go test ./...` passes for all 7 packages (hermetic — mocked exec, no network/binaries).

- **Phase 1 Foundation** — go.mod (cobra, the one allowed dep), .gitignore, `scripts/clean.sh`, CLI skeleton, locked package interfaces + `chunks.json` contract.
- **Phase 2 Download** — yt-dlp/ffmpeg wrapper, YouTube URL parsing (watch/shorts/embed/live/youtu.be), local `--file` mode, idempotent resume, URL/`--file` validation.
- **Phase 3 Chunk** — ffmpeg `scdet` scene detection (streamed stderr parsing), ffprobe duration, parallel frame+audio extraction (jobs-capped), `chunks.json` data shape.
- **Phase 4+5 Dedupe + Transcribe** — built by two parallel subagents against the locked interface, then lead-reviewed and verified: dedupe = pure-Go 9×8 dHash + run-anchored merge (never drifts, never drops audio); transcribe = whisper-cli `-oj` per chunk with absolute-timestamp alignment (`transcripts/NNNN.txt` + raw JSON provenance).
- **Phase 6 Manifest** — output tree (`chunks/NNNN/{frame.png, transcript.txt, meta.json}`), `manifest.json`, seeded `reconstruction.md`, `instructions.md` copy; merged-chunk transcripts concatenated from `source_ids` in order; falls back to raw chunks when dedupe hasn't run.
- **Phase 7 `all` orchestration** — download → chunk → (dedupe ∥ transcribe) → manifest with progress, resume-from-any-stage, `--skip-transcribe`, `--file` mode.
- **Phase 8 Efficiency** — streaming everywhere (scene-detection stderr piped, no full-file buffering), concurrency capped at `--jobs` (default CPU count) in chunk + transcribe.
- **Phase 9 Testing** — per-package unit tests (7 suites) + CLI tests + `scripts/integration.sh`: synthesizes a 6 s / 3-scene video with ffmpeg, runs the full pipeline offline (real whisper-cli when present), validates manifest order/totals/files. **Result: PASS** (ran on this machine with real ffmpeg + real whisper).

### Bugs found and fixed during the build

- Stale-flag-capture bug: every subcommand read persistent flags (`--work-dir`, `--output-dir`, `--jobs`, `--skip-transcribe`) at command-construction time via a by-value parameter, so the parsed values were silently ignored. Fixed by reading flags at RunE time (`cmd.Flags().GetX`); regression test added.
- Manifest transcript merge inserted blank lines between source transcripts; fixed with newline-aware joining.
- dHash note: solid-color frames hash identically (structural hash, not color) — documented in README as a known limitation.

### Machine setup (this machine)

- ffmpeg 9.0 (repaired a broken empty scoop install), whisper-cli 1.9.2 (scoop), `ggml-base.bin` (148 MB, multilingual) at `~/.cache/ytreconstruct/whisper/` — fetched from hf-mirror.com because huggingface.co is blocked on this network. YouTube was also unreachable during part of the build (now confirmed reachable again — 200 in ~2 s).

### What I tested vs. what still needs eyes

**Tested by me (real runs, this machine):** full offline pipeline end-to-end via `scripts/integration.sh` (PASS, including real whisper transcription); `go test ./...` (all pass); `go build/vet/gofmt` clean; CLI help; `--file` mode; resume behavior (stages skip when outputs exist); yt-dlp metadata fetch for the target video (1218 s ≈ 20 min, works).

**Not tested yet:** the real YouTube run against `https://youtu.be/BL8TfsLk3WM` (pending — see next step); `go test -race` (requires cgo/gcc, not present on this Windows machine); anything on non-Windows (paths/flags were written portably but only Windows was exercised).

## Next step

Real end-to-end test: `ytreconstruct all https://youtu.be/BL8TfsLk3WM` → verify manifest + chunk tree. Then final review sign-off.
