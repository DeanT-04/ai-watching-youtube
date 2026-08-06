# Build brief: ytreconstruct

Read `AGENTS.md` first if you haven't already — it defines who you are and the hard boundaries you work within. This document is your build plan. You're starting from a genuinely blank project: nothing exists yet except `AGENTS.md`, this file, and `scripts/clean.sh`. Everything else — `go.mod`, source, tests, docs — you're creating from nothing.

Your job is to take this project from blank to built, tested, and reviewed, working through the phases below in order, without needing me to check in after every step. Ask only when you hit a genuine fork in the road as described in AGENTS.md. Otherwise, keep moving, commit as you go, and leave a short note at the top of `docs/progress.md` after each phase so I can see where you are at a glance without reading your whole history.

## The non-negotiables

Everything you build must be:
- **Lightweight** — small binary, minimal dependencies, no bloat "for future flexibility" you haven't been asked for.
- **CPU-friendly** — no GPU assumptions anywhere. If a tool you're wrapping has a GPU mode, don't rely on it.
- **Clean** — a new contributor should be able to read one package and understand it without reading the other four.
- **Expandable, not over-engineered** — leave sensible seams (interfaces where a real alternative implementation is plausible — e.g. swapping the transcription backend), but don't build abstraction layers for hypothetical futures that aren't real requirements yet.

If you ever have to choose between "clever and fast" and "obvious and slightly slower," choose obvious. This tool processes video, which is already slow — the code reading it shouldn't be.

## Target shape of the repo

```
yt-reconstruct/
├── cmd/ytreconstruct/main.go     CLI entrypoint — subcommands: download, chunk, dedupe, transcribe, manifest, all
├── internal/
│   ├── download/download.go      wraps yt-dlp
│   ├── chunk/chunk.go            ffmpeg scene-change detection → chunk boundaries + raw frame/audio split
│   ├── dedupe/dedupe.go          perceptual hash — merges visually-static consecutive chunks
│   ├── transcribe/transcribe.go  wraps local Whisper binary, aligns text to chunk timestamps
│   └── manifest/manifest.go      writes manifest.json + seeds reconstruction.md
├── scripts/clean.sh              already exists — don't break its contract
├── work/                         scratch (gitignored)
├── output/                       deliverable for the downstream reconstruction agent (gitignored)
├── docs/instructions.md          prompt template for the agent that will consume output/
├── go.mod / go.sum
├── README.md
└── AGENTS.md                     already exists
```

## Build order, and why

Build in this order. Each phase depends on the data shape the previous one produces, so don't skip ahead even if it looks tempting.

```
Phase 0  Foundation
   │      go.mod, .gitignore, cobra CLI skeleton with all subcommands stubbed,
   │      README skeleton. Nothing functional yet — just a shape that builds and runs --help.
   ▼
Phase 1  Download
   │      Wrap yt-dlp. Given a URL, produce work/<video_id>/video.mp4 + audio.wav.
   │      Everything downstream needs this, so it comes first and gets the most defensive
   │      error handling (bad URL, private video, missing yt-dlp binary).
   ▼
Phase 2  Chunk
   │      Wrap ffmpeg scene-change detection to get boundary timestamps. For each boundary,
   │      extract one representative frame and the corresponding audio slice into
   │      work/<video_id>/chunks_raw/. This fixes the data shape (frame + audio slice +
   │      timestamp range per chunk) that everything after this depends on — get the
   │      interface right here before parallelizing anything.
   │
   ├──Phase 3  Dedupe ───────────┐
   │      Perceptual-hash the      │  These two are independent of each other — dedupe only
   │      representative frames.   │  needs the frame list, transcribe only needs the audio
   │      Where consecutive        │  list, and neither needs the other's output. Once
   │      frames are visually      │  Phase 2's interface is locked, this is the pair to
   │      identical, merge their   │  hand to subagents in parallel. See "Where to use
   │      time ranges rather than  │  subagents" below.
   │      dropping data — the      │
   │      audio for that static    │
   │      period still matters.    │
   │                                │
   └──Phase 4  Transcribe ───────┘
          Wrap the local Whisper binary per chunk's audio slice. Output transcript.txt
          per chunk, aligned by chunk id.
   ▼
Phase 5  Manifest
   │      Take the deduped chunk list + transcripts, write the final
   │      output/<video_id>/chunks/000N/{frame.png, transcript.txt, meta.json} tree,
   │      write manifest.json listing chunks in strict order, seed an empty
   │      reconstruction.md and copy in instructions.md. This is where dedupe's output
   │      and transcribe's output get reunited — you own this integration, not a subagent.
   ▼
Phase 6  CLI wiring
   │      Wire the `all` subcommand to run phases 1–5 end to end with progress output.
   │      Keep every individual subcommand usable standalone too, so a run can be resumed
   │      from any stage without redoing earlier (expensive) work.
   ▼
Phase 7  Efficiency pass
   │      Profile actual CPU/memory use on a real video. Stream ffmpeg output rather than
   │      buffering full files in memory. Cap concurrency to a sane default tied to
   │      available cores, with a flag to override. This is a pass over existing code,
   │      not new features.
   ▼
Phase 8  Testing
   │      Unit tests per package (exec calls mocked — no real binaries or network in CI).
   │      One integration script that runs the full pipeline against a short, fixed test
   │      video and asserts the manifest and chunk tree come out valid.
   ▼
Phase 9  Docs + final review
          Finish README (install requirements, usage, flags) and instructions.md.
          Do a full self-review pass: no dead code, no TODOs left unresolved without a
          reason, AGENTS.md constraints actually held throughout, clean.sh still
          accurately describes what it deletes.
```

## Where to use subagents

Delegate to subagents only where the work is genuinely independent and you can specify it completely up front. In this project, that's exactly one place: **Phase 3 and Phase 4**, once Phase 2 has locked the chunk data shape. Give each subagent the exact struct/interface it must implement and the test cases it must pass — don't assume it can see your reasoning, only what you write in its task.

Everything else stays with you directly:
- Interface and data-shape decisions (Phase 2, Phase 5) — these ripple through the whole project, so they need one consistent judgment behind them.
- Integration (Phase 5, Phase 6) — reconciling two subagents' output is exactly the kind of cross-cutting judgment call that shouldn't itself be delegated.
- The final test and review pass (Phase 8, Phase 9) — you need to have the full picture in your head to review it honestly.

If a phase turns out to have an independent sub-piece I haven't anticipated, use the same test: can you specify it completely, does it not need to know how any sibling piece is implemented, and are you still the one integrating and reviewing the result? If yes to all three, delegating is fine.

## Definition of done

Don't consider this finished until every one of these is true:

- [ ] `go build ./...` succeeds with zero errors or warnings
- [ ] `go vet ./...` is clean
- [ ] `gofmt -l .` reports no files
- [ ] Every package has tests, and `go test ./...` passes with no network or external binaries required
- [ ] The integration script runs a full pipeline against a test video and produces a valid `manifest.json` and populated `chunks/` tree
- [ ] `README.md` documents install requirements (yt-dlp, ffmpeg, the Whisper binary) and every subcommand
- [ ] Every constraint in `AGENTS.md` was actually held, not just noted — no cloud deps, no GPU assumption, nothing under `work/` or `output/` committed
- [ ] `scripts/clean.sh` still matches the real directory structure you ended up building
- [ ] You can describe, in the final `docs/progress.md` entry, what you tested yourself versus what still needs a human's eyes

Work through this now, starting at Phase 0.
