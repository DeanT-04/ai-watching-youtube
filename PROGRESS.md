# PROGRESS

Latest phase status at the top. One entry per phase, most recent first.

## Phase 0 — Foundation (done)

- Wiped the previous (failed) git history entirely; repo re-initialized with a single fresh commit.
- `go.mod` (`ytreconstruct`, cobra v1.10.2), `.gitignore` (work/, output/, models never committed),
  `scripts/clean.sh` (confirmation prompt preserved, `-y` to skip).
- CLI skeleton: `cmd/ytreconstruct` with subcommands `download`, `chunk`, `dedupe`, `transcribe`,
  `manifest`, `all`; internal package stubs with the locked `Run(Options) error` interfaces and the
  `chunks.json` data-shape contract (`internal/chunk.ChunkList`).

### Machine setup notes (for the record)

- `ffmpeg` was present as a broken empty scoop install — repaired via `scoop uninstall` + `scoop install ffmpeg` (9.0, works).
- `whisper-cpp` installed via scoop → `whisper-cli` 1.9.2.
- Model: `ggml-base.bin` (multilingual, CPU-friendly) downloading to `~/.cache/ytreconstruct/whisper/` from hf-mirror.com (huggingface.co is unreachable from this network; YouTube is also currently unreachable — final live test is pending that).
