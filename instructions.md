# Instructions for the reconstruction agent

You have been given a directory produced by `ytreconstruct` from a YouTube
video. Your job: **watch the video in order** and reconstruct exactly what
appeared on screen — code, prompts, configs, URLs, UI text — without ever
jumbling the sequence.

## What you have

```
output/<video_id>/
├── manifest.json        ← read this first: the strict, ordered chunk list
├── reconstruction.md    ← your working log; append notes under each chunk
├── instructions.md      ← this file
└── chunks/
    ├── 0001/
    │   ├── frame.png        ← one frame from the scene
    │   ├── transcript.txt   ← audio transcript, absolute timestamps
    │   └── meta.json        ← id, start, end, duration, source chunks
    ├── 0002/ ...
```

`manifest.json` lists every chunk **in strict playback order**. Each entry
gives the chunk's time range (`start` → `end`, absolute seconds on the video
timeline), its files, and `source_ids` — the raw scene chunks that were
merged into it (merged chunks have multiple sources; their transcript is the
concatenation of the source transcripts in order).

## Rules

1. **Never reorder.** Process chunks in the order they appear in
   `manifest.json` — 0001, 0002, 0003, … No skipping ahead, no reordering,
   even if a transcript or frame looks redundant.
2. **Transcribe faithfully.** Copy code, prompts, commands, URLs, and config
   values **verbatim** from the frame. Do not paraphrase code. If the frame
   is blurry or the text unreadable, say so rather than guessing.
3. **Frames are the source of truth for visuals**; transcripts are the
   source of truth for speech. Use both — the transcript may describe
   something not visible in the frame (and vice versa).
4. **Timestamps are absolute** on the video timeline (`00:01:23.456` =
   83.456 s). Use them to locate context in the source video if needed.
5. **Append your reconstruction notes** under each chunk heading in
   `reconstruction.md`. Never edit earlier entries or the manifest.
6. If a chunk is a merged static period (`source_ids` has >1 entry), expect
   the same visual content for the whole range — the audio transcript is
   what changes there.
7. When you finish, `reconstruction.md` must read like a linear, timestamped
   retelling of the video with every on-screen artifact captured.
