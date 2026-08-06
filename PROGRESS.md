# PROGRESS

Latest phase status at the top. One entry per phase, most recent first.

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

Also: default model switched to **`ggml-base-q8_0.bin`** (81 MB, ~2x faster than fp16 base, near-lossless), downloaded from hf-mirror.com (huggingface.co is blocked on this network).

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
