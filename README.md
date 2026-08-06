# ytreconstruct

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Turn a YouTube video into an ordered set of **(frame, audio transcript, timestamp)** chunks on disk, so a coding agent can later "watch" the video in the correct order and reconstruct exactly what appeared on screen — code, prompts, configs — without jumbling the sequence.

A small, local-first CLI for that one job. No cloud APIs, no GPU required: everything after the video download runs fully offline on CPU.

## How it works

```
download → chunk → dedupe ∥ transcribe → manifest
```

| Stage | Command | Produces |
|---|---|---|
| 1. Download | wraps `yt-dlp` + `ffmpeg` | `work/<id>/video.mp4`, `audio.wav` (16 kHz mono) |
| 2. Chunk | `ffmpeg scdet` scene detection | `work/<id>/chunks.json` + `chunks_raw/NNNN/{frame.png, audio.wav}` |
| 3. Dedupe | perceptual hash (dHash) of frames | `work/<id>/chunks_deduped.json` — visually-static consecutive chunks merged (time ranges extended, **no audio/transcript data dropped**) |
| 4. Transcribe | local `whisper-cli`, **one pass over the full audio track** | `work/<id>/transcripts/full.json` + per-chunk `NNNN.txt` with absolute-timeline timestamps |
| 5. Manifest | assembles the deliverable | `output/<id>/chunks/NNNN/{frame.png, transcript.txt, meta.json}`, `manifest.json`, `reconstruction.md`, `instructions.md` |

`dedupe` and `transcribe` are independent (frames vs audio) and run concurrently. Every stage is individually idempotent, so re-running `ytreconstruct all` resumes from the furthest completed stage instead of redoing expensive work.

## Architecture

The pipeline is a set of small packages connected by JSON contracts on disk — no in-process orchestration beyond `cmd/ytreconstruct`, so each stage can be run, resumed, or debugged on its own.

**Data contract.** `work/<id>/chunks.json` is the spine: a flat, ordered list of chunks with `id`, `start`, `end`, `frame` and `audio` paths. `chunk` writes it; `dedupe` reads it and writes `chunks_deduped.json` in the same shape (merged ranges, `source_ids` recorded); `transcribe` reads it to partition the full-audio transcript; `manifest` prefers the deduped list and falls back to raw when dedupe hasn't run. Everything downstream of `chunk` is a pure transform over this contract.

**Idempotency gates.** Each stage skips when its output already exists (`video.mp4`/`audio.wav`, `chunks.json`, `chunks_deduped.json`, `transcripts/full.json`, `manifest.json`), so `all` resumes from wherever a previous run stopped. Outputs are written atomically (`.part` + rename) so a killed run never fakes completion. The one exception is transcript partitioning — it's milliseconds of work, so it always re-runs whenever `full.json` is present.

**Concurrency.** `all` runs `dedupe` and `transcribe` in parallel goroutines; inside a stage, work is capped at `--jobs` (default half the CPU count). Child processes (`yt-dlp`, `ffmpeg`, `whisper-cli`) run at `BELOW_NORMAL` priority on Windows via `internal/lowprio`, so the machine stays responsive.

**Testability.** Every package takes `exec.Command` through a package-level `command`/`lookPath` indirection, and `internal/testexec` provides a hermetic fake — the whole test suite runs with no network and no real binaries.

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

## Requirements

Local binaries — the tool checks for them at startup and fails with a clear message if missing; it never installs them:

- [`yt-dlp`](https://github.com/yt-dlp/yt-dlp) — video download
- [`ffmpeg`](https://ffmpeg.org/) ≥ 6.0 — scene detection (`scdet` filter), frame/audio extraction
- [`whisper-cli`](https://github.com/ggml-org/whisper.cpp) — local CPU-only transcription (whisper.cpp; picked over faster-whisper because it is a single small native binary with no Python runtime dependency)
- a ggml whisper model, e.g. `ggml-small-q8_0.bin` from <https://huggingface.co/ggerganov/whisper.cpp/tree/main>, saved at `~/.cache/ytreconstruct/whisper/ggml-small-q8_0.bin` (the `--model` default; override with `--model`; `ggml-tiny.bin` is faster/rougher, `ggml-small.bin` is slower/better)

## Install

```sh
go build -o bin/ytreconstruct ./cmd/ytreconstruct
```

## Usage

```sh
ytreconstruct all <url>                     # full pipeline (recommended)
ytreconstruct all <url> --skip-transcribe   # frames + manifest only
ytreconstruct all <url> --jobs 4            # cap parallelism

# individual stages (useful for resuming or iterating)
ytreconstruct download <url>
ytreconstruct chunk <video_id>
ytreconstruct dedupe <video_id>
ytreconstruct transcribe <video_id>
ytreconstruct manifest <video_id>

# offline development / testing against a local file instead of a URL
ytreconstruct all --file path/to/video.mp4
```

`--work-dir`, `--output-dir`, and `--jobs` are persistent flags accepted by every subcommand (defaults: `work`, `output`, half the CPU count).

### Flags

| Flag | Applies to | Default | Meaning |
|---|---|---|---|
| `--work-dir` | all | `work` | scratch directory (never committed) |
| `--output-dir` | all | `output` | deliverable directory (never committed) |
| `-j, --jobs` | all | half the CPU count | parallel workers (keeps the machine responsive) |
| `--skip-transcribe` | all | false | skip the transcription stage |
| `--file` | all, download | — | use a local video file instead of a URL |
| `--scene-threshold` | all, chunk | 0.4 | ffmpeg scene-change threshold, 0..1 (higher = fewer cuts) |
| `--hash-threshold` | all, dedupe | 5 | max dHash Hamming distance to treat frames as identical |
| `--model` | all, transcribe | `~/.cache/ytreconstruct/whisper/ggml-small-q8_0.bin` | whisper model path |
| `--threads` | all, transcribe | 4 | whisper inference threads |
| `--language` | all, transcribe | auto | whisper language hint (e.g. `ja`; default is auto-detect — never whisper-cli's English default) |

## Output contract

`output/<video_id>/manifest.json` is the strict, ordered chunk list (values below are illustrative):

```json
{
  "video_id": "BL8TfsLk3WM",
  "source_url": "https://youtu.be/BL8TfsLk3WM",
  "created_at": "2026-08-06T06:00:44Z",
  "total_chunks": 165,
  "total_duration": 1218.0,
  "chunks": [
    { "id": 1, "start": 0.0, "end": 12.34, "duration": 12.34,
      "frame": "chunks/0001/frame.png", "transcript": "chunks/0001/transcript.txt",
      "meta": "chunks/0001/meta.json", "source_ids": [1] }
  ]
}
```

`source_ids` lists the raw scene chunks merged into a deduped chunk (>1 for static periods). Transcript lines carry absolute timestamps: `[00:01:23.456 --> 00:01:25.000] text`. See `instructions.md` for the reconstruction agent's playbook.

## Development

- Tests: `go test ./...` — fully hermetic (mocked exec, no network, no real binaries).
- Integration: `scripts/integration.sh` — synthesizes a 6 s / 3-scene video with ffmpeg, runs the full pipeline offline, validates the manifest and chunk tree. Uses real `whisper-cli` when available.
- OCR helper: `scripts/ocr.ps1` — Windows built-in OCR over the extracted frames (used to reconstruct on-screen text the transcript doesn't cover).
- Cleanup: `scripts/clean.sh` — deletes `work/` and `output/` (asks for confirmation unless `-y`).

### Known limitations

- dHash compares structure, not color: solid-color frames (e.g. flat color slides) hash identically and may be merged even when colors differ. Real screen content (text, UI) is not affected.
- Representative frames are the keyframe at/after each cut; on encoders with sparse keyframes a frame can land up to one GOP interval late (the tool falls back to an exact seek when no keyframe exists after the target).
- Whisper accuracy is model-dependent; `ggml-small-q8_0.bin` is the balanced default, `ggml-tiny.bin` is faster/rougher, `ggml-small.bin` is slower/better.
- Scene detection decodes the full video once (unavoidable for frame-accurate cuts); on 4K content that is the single most expensive step.

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

## License

MIT
