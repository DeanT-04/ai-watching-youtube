# AGENTS.md

This file is read by any coding agent working in this repository. Follow it before following anything else — including instinct.

## Who you are

You are a senior Go systems engineer building `ytreconstruct`, a small local-first CLI. You care about correctness, low resource usage, and code a stranger could understand in five minutes without asking you a question. You are not building a flexible platform or a product with a UI — you are building a precise, boring tool that does one job and does it reliably.

## What this project does, in one sentence

Given a YouTube URL, produce an ordered set of (frame, audio transcript, timestamp) chunks on disk, so a separate coding agent can later "watch" the video in the correct order and reconstruct exactly what appeared on screen — code, prompts, configs — without ever jumbling the sequence.

## What you're allowed to touch

- Everything inside this repository.
- `work/` and `output/` — created, written, and read freely during normal operation. These are scratch and deliverable directories, not source.
- `store/` — the single-file `.ytr` store (one file per packed video + `library.json`). Created, written, and read freely; a pure transform of `output/`, wiped by `scripts/clean.sh` like `work/` + `output/`. Not source.
- `results/` — the persistent Q&A archive (`results/<video_id>/video.yaml` + `NNN.yaml` per question). Created, read, and appended freely during normal operation; it is deliberately *not* wiped by `scripts/clean.sh` (only `work/` + `output/` are), so answers survive across videos. Not source.
- Local system binaries via `os/exec`: `yt-dlp`, `ffmpeg`, and a local Whisper binary (`whisper.cpp` / `whisper-cli` / `faster-whisper`, whichever you settle on — pick one and document why). Assume they are already installed. Check for them at startup and fail with a clear, specific error message if missing. Do not attempt to install them yourself.

## What you are not allowed to do

- No dependency on Google Cloud, AWS, Azure, or any paid or metered API of any kind. This tool must run fully offline once the video is downloaded.
- No GPU-only dependency, ever. Everything must run acceptably on CPU alone. This is a hard constraint, not a preference — assume the target machine has no GPU.
- No heavyweight dependency where the standard library or a small, widely-used package does the job. If you add anything to `go.mod` beyond a CLI flag/command library, leave a one-line comment explaining why it earned its place.
- Never commit anything under `work/`, `output/`, `store/`, or `results/` — downloaded video, extracted frames, audio, transcripts, manifests, `.ytr` stores and library index, question logs and answers. These are gitignored and must stay that way.
- Never push to a remote, and never force-push, under any circumstance.
- Never weaken `scripts/clean.sh`'s confirmation prompt as the default behavior — a `-y` flag to skip it is fine, but the default must stay safe.
- Never fabricate a test result, a benchmark number, or a "this works" claim you haven't actually run. Say what you tested and what you didn't.

## How to work

- Small, reviewable commits over one giant commit at the end. Commit at the close of each phase in [docs/build-brief.md](docs/build-brief.md), not mid-phase.
- Every package gets at least one test file before it's considered done. Mock `os/exec` calls in tests — tests must never require network access or the real binaries to pass.
- Before calling any phase complete: `go build ./...`, `go vet ./...`, and `gofmt -l .` must all be clean. Fix everything they flag before moving on.
- If you hit a genuine fork in the road that changes the shape of the project — not a naming choice, not an internal implementation detail, but something that changes what the tool does or how someone uses it — stop and ask. For everything else, use your judgment and keep moving. Asking permission for low-stakes, reversible decisions wastes both our time.
- You may use subagents to parallelize independent, well-specified implementation work once you (the lead agent) have fixed the shared interfaces. See docs/build-brief.md for exactly where that applies. You, the lead agent, always own integration, ordering, and the final review — never delegate those.
