# Architecture

How `ytreconstruct` is put together and why. The README covers what it does; this file covers how it works and the engineering trade-offs. Details here were verified against the code — nothing is aspirational.

## Pipeline design

The pipeline is a set of small packages connected by JSON contracts on disk — no in-process orchestration beyond `cmd/ytreconstruct`, so each stage can be run, resumed, or debugged on its own.

**Data contract.** `work/<id>/chunks.json` is the spine: a flat, ordered list of chunks with `id`, `start`, `end`, `frame` and `audio` paths. `chunk` writes it; `dedupe` reads it and writes `chunks_deduped.json` in the same shape (merged ranges, `source_ids` recorded); `transcribe` reads it to partition the full-audio transcript; `manifest` prefers the deduped list and falls back to raw when dedupe hasn't run. Everything downstream of `chunk` is a pure transform over this contract.

**Idempotency gates.** Each stage skips when its output already exists (`video.mp4`/`audio.wav`, `chunks.json`, `chunks_deduped.json`, `transcripts/full.json`, `manifest.json`), so `all` resumes from wherever a previous run stopped. Outputs are written atomically (`.part` + rename) so a killed run never fakes completion. The one exception is transcript partitioning — it's milliseconds of work, so it always re-runs whenever `full.json` is present.

**Concurrency.** `all` runs `dedupe` and `transcribe` in parallel goroutines; inside a stage, work is capped at `--jobs` (default half the CPU count). Child processes (`yt-dlp`, `ffmpeg`, `whisper-cli`) run at `BELOW_NORMAL` priority on Windows via `internal/lowprio`, so the machine stays responsive.

**Testability.** Every package takes `exec.Command` through a package-level `command`/`lookPath` indirection, and `internal/testexec` provides a hermetic fake — the whole test suite runs with no network and no real binaries.

## Packages

| Package | Responsibility |
|---|---|
| `internal/download` | yt-dlp wrapper: YouTube URL parsing (`watch`/`shorts`/`embed`/`live`/`youtu.be`), local `--file` mode, audio extraction to 16 kHz mono WAV |
| `internal/chunk` | ffmpeg `scdet` scene detection (streamed stderr parsing), keyframe extraction (`-skip_frame nokey` + seek fallback), pure-Go WAV byte-range slicing |
| `internal/dedupe` | 64-bit dHash of frames (parallel, buffer-fast), run-anchored merging of visually-static consecutive chunks |
| `internal/transcribe` | one `whisper-cli -oj` pass over the full audio track, then segment partitioning into per-chunk transcripts with absolute timestamps |
| `internal/manifest` | assembles the ordered output tree, `manifest.json`, seeded `reconstruction.md`, copied `instructions.md` |
| `internal/lowprio` | `BELOW_NORMAL` priority for child processes (Windows); no-op elsewhere |
| `internal/testexec` | hermetic `exec.Command` fake used by every test suite |

## Performance design

Engineered to be fast and polite to your machine:

- **One whisper process total.** The full audio track is transcribed in a single `whisper-cli` pass (one model load, one inference run), then segments are partitioned into per-chunk transcripts. The old per-chunk approach re-loaded the model 1000+ times.
- **Keyframe extraction.** Representative frames come from the first keyframe at/after each scene cut (`-skip_frame nokey`) — encoders place keyframes at scene cuts, so this is usually the exact scene-start frame, and it avoids decoding forward from the previous keyframe (6× faster on 4K). Falls back to an accurate seek when no keyframe exists after the target.
- **Audio slicing in pure Go.** Chunk audio slices are byte-range copies out of the 16 kHz WAV — zero extra processes.
- **Buffer-fast dHash.** The perceptual hash reads raw pixel buffers instead of per-pixel interface calls (~30× faster on 4K frames) and hashes in parallel.
- **Parallel frame copies** in the manifest stage.
- **Polite CPU usage:** children run at `BELOW_NORMAL` priority on Windows, and the default `--jobs` is half your CPU count — the machine stays usable while the pipeline grinds.
- **Quantized whisper model by default** (`ggml-small-q8_0.bin`): roughly 2× faster than the fp16 build of the same model, with near-identical accuracy.

Validated on a real 20-minute / 4K video: 1731 raw scenes deduped to 165 meaningful chunks, full pipeline in ~23 minutes (details in `PROGRESS.md`).

## Layout

```
cmd/ytreconstruct/      CLI entrypoint (cobra)
internal/download/      yt-dlp + ffmpeg wrapper
internal/chunk/         scene detection → chunk boundaries + chunks.json contract
internal/dedupe/        dHash merging of static chunks
internal/transcribe/    whisper-cli wrapper, absolute-timestamp alignment
internal/manifest/      output tree + manifest.json + reconstruction.md seed
internal/lowprio/       BELOW_NORMAL priority for child processes (Windows)
internal/testexec/      hermetic exec.Command fake for tests
scripts/clean.sh        safe wipe of work/ + output/
scripts/integration.sh  offline end-to-end test
scripts/ocr.ps1         Windows OCR over extracted frames
work/                   scratch (gitignored)
output/                 deliverables (gitignored)
```
