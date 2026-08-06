# ytreconstruct

Turn a YouTube video into an ordered set of **(frame, audio transcript, timestamp)** chunks on disk, so a separate coding agent can later "watch" the video in the correct order and reconstruct exactly what appeared on screen — code, prompts, configs — without jumbling the sequence.

A small, local-first CLI. No cloud APIs, no GPU required: everything after the video download runs fully offline.

## How it works

```
download → chunk → dedupe ∥ transcribe → manifest
```

| Stage | Command | Produces |
|---|---|---|
| 1. Download | wraps `yt-dlp` + `ffmpeg` | `work/<id>/video.mp4`, `audio.wav` (16 kHz mono) |
| 2. Chunk | `ffmpeg scdet` scene detection | `work/<id>/chunks.json` + `chunks_raw/NNNN/{frame.png, audio.wav}` |
| 3. Dedupe | perceptual hash (dHash) of frames | `work/<id>/chunks_deduped.json` — visually-static consecutive chunks merged (time ranges extended, **no audio/transcript data dropped**) |
| 4. Transcribe | local `whisper-cli` per chunk | `work/<id>/transcripts/NNNN.txt` with absolute-timeline timestamps |
| 5. Manifest | assembles the deliverable | `output/<id>/chunks/NNNN/{frame.png, transcript.txt, meta.json}`, `manifest.json`, `reconstruction.md`, `instructions.md` |

`dedupe` and `transcribe` are independent (frames vs audio) and run concurrently. Every stage is individually idempotent, so re-running `ytreconstruct all` resumes from the furthest completed stage.

## Requirements

Local binaries — the tool checks for them at startup and fails with a clear message if missing; it never installs them:

- [`yt-dlp`](https://github.com/yt-dlp/yt-dlp) — video download
- [`ffmpeg`](https://ffmpeg.org/) ≥ 6.0 — scene detection (`scdet` filter), frame/audio extraction
- [`whisper-cli`](https://github.com/ggml-org/whisper.cpp) — local CPU-only transcription (whisper.cpp; picked over faster-whisper because it is a single small native binary with no Python runtime dependency)
- a ggml whisper model, e.g. `ggml-base.bin` from <https://huggingface.co/ggerganov/whisper.cpp/tree/main>, saved at `~/.cache/ytreconstruct/whisper/ggml-base.bin` (the `--model` default; override with `--model`)

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

Every subcommand accepts `--work-dir` (default `work`) and `--output-dir` (default `output`); `all`, `chunk` and `transcribe` also accept `--jobs` (default: CPU count).

### Flags

| Flag | Applies to | Default | Meaning |
|---|---|---|---|
| `--work-dir` | all | `work` | scratch directory (never committed) |
| `--output-dir` | all | `output` | deliverable directory (never committed) |
| `-j, --jobs` | all, chunk, transcribe | CPU count | parallel workers |
| `--file` | all, download | — | use a local video file instead of a URL |
| `--skip-transcribe` | all | false | skip the transcription stage |
| `--scene-threshold` | all, chunk | 0.4 | ffmpeg scene-change threshold, 0..1 (higher = fewer cuts) |
| `--hash-threshold` | all, dedupe | 5 | max dHash Hamming distance to treat frames as identical |
| `--model` | all, transcribe | `~/.cache/ytreconstruct/whisper/ggml-base.bin` | whisper model path |
| `--threads` | all, transcribe | 4 | whisper inference threads per process |
| `--language` | all, transcribe | auto | whisper language hint (e.g. `en`) |

## Output contract

`output/<video_id>/manifest.json` is the strict, ordered chunk list:

```json
{
  "video_id": "BL8TfsLk3WM",
  "source_url": "https://youtu.be/BL8TfsLk3WM",
  "total_chunks": 42,
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
- Cleanup: `scripts/clean.sh` — deletes `work/` and `output/` (asks for confirmation unless `-y`).

### Known limitations

- dHash compares structure, not color: solid-color frames (e.g. flat color slides) hash identically and may be merged even when colors differ. Real screen content (text, UI) is not affected.
- Representative frames use fast seek (`-ss` before `-i`); on encoders with sparse keyframes a frame may land a few seconds off the exact cut.
- Whisper accuracy is model-dependent; `ggml-base.bin` is a balanced default, `ggml-tiny.en.bin` is faster/rougher, `ggml-small.bin` is slower/better.

## Layout

```
cmd/ytreconstruct/      CLI entrypoint (cobra)
internal/download/      yt-dlp + ffmpeg wrapper
internal/chunk/         scene detection → chunk boundaries + chunks.json contract
internal/dedupe/        dHash merging of static chunks
internal/transcribe/    whisper-cli wrapper, absolute-timestamp alignment
internal/manifest/      output tree + manifest.json + reconstruction.md seed
internal/testexec/      hermetic exec.Command fake for tests
scripts/clean.sh        safe wipe of work/ + output/
scripts/integration.sh  offline end-to-end test
work/                   scratch (gitignored)
output/                 deliverables (gitignored)
```
