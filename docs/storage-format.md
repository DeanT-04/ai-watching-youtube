# `.ytr` storage format — v1 spec

The `.ytr` (ytreconstruct store) format is the single-file, queryable deliverable
of a watched video. One `.ytr` file per video, plus a tiny `store/library.json`
index across all videos, lets an AI agent "watch" the whole video in 2 commands:

```sh
ytreconstruct store dump <video-id>            # ordered chunks + transcripts
ytreconstruct store frame <video-id> <NNNN> out.png   # visual / OCR
```

Everything else (`store query --grep`, `store verify`, `store list`) is a
refinement of the same surface. The legacy `output/<video-id>/` folder tree
remains the pipeline's primary deliverable; `.ytr` is built from it additively.

## Container

A `.ytr` file is a **ZIP archive** written with the Go standard library
(`archive/zip`). It is a zip with our own member layout and JSON schema — the
same pattern as `.jar`, `.docx`, `.epub`. Rationale (recorded 2026-08-07):

- Zero new dependencies; stdlib CRC-32 integrity per member comes free and is
  verified on every read.
- Frames are already-compressed (PNG deflate / WebP lossless), so a zip deflate
  pass gains ~0% — the container must store frame payloads verbatim.
- Every future consumer can read it with any zip tool, so the format can never
  be locked behind our binary (unlike a bespoke record format).

**Member layout (v1):**

| Member | Compression | Content |
|---|---|---|
| `ytr/spec.json` | DEFLATE | the index: metadata, ordered chunk list, transcripts, segment provenance, seeds (see schema below) |
| `frames/<NNNN>.webp` | STORE (uncompressed) | chunk frame, WebP **lossless**, `<NNNN>` = zero-padded chunk id (`0001`…`0165`) |

- `ytr/spec.json` deflates well (JSON text) and is the only member the agent
  ever reads whole.
- Frames use zip method `Store` so their bytes are **byte-exact** inside the
  archive: no recompression ever, so quality cannot compound or degrade, and
  extraction is lossless by construction.
- Any member not listed above is an error for `verify` (schema_version gate).

## Frame codec

Frames are stored as **WebP lossless** (ffmpeg `-c:v libwebp -lossless 1`),
converted at pack time from the PNG frames in `output/<video-id>/chunks/`.

Validation on 5 real 4K frames from `output/BL8TfsLk3WM` (2026-08-07):

| chunk | PNG bytes | WebP bytes | saved |
|---|---|---|---|
| 0001 | 2,473,061 | 1,410,324 | 43% |
| 0009 | 2,288,940 | 1,503,812 | 35% |
| 0021 | 3,832,688 | 2,631,368 | 32% |
| 0083 | 1,361,636 | 825,866 | 40% |
| 0165 | 2,596,463 | 1,523,086 | 42% |
| **total** | **12,552,788** | **7,894,456** | **38%** |

- **Pixel-identity:** PNG → WebP lossless → PNG round-trip compared as raw RGBA
  is pixel-identical for all 5 frames (ffmpeg rawvideo + `cmp`).
- **OCR:** a WinRT OCR spot check could not run in this shell (the engine fails
  on the *original* PNGs too — environmental). It is also unnecessary:
  pixel-identity means any OCR engine receives byte-identical input and must
  return identical text. `store frame` decodes to PNG (pixel-identical) so
  `scripts/ocr.ps1` and agent image tools work unchanged.
- Decode cost ~0.2 s/frame on CPU; pack-time encode cost ~0.2 s/frame.

## `ytr/spec.json` schema (v1)

```json
{
  "schema_version": 1,
  "video_id": "BL8TfsLk3WM",
  "source_url": "https://youtu.be/BL8TfsLk3WM",
  "created_at": "2026-08-06T12:00:00Z",
  "packed_at": "2026-08-07T04:55:00Z",
  "total_chunks": 165,
  "total_duration": 1217.9,
  "reconstruction_md": "…seed text from output/<id>/reconstruction.md, verbatim…",
  "instructions_md": "…docs/instructions.md text, verbatim…",
  "chunks": [
    {
      "id": 1,
      "start": 0.0,
      "end": 6.0,
      "duration": 6.0,
      "source_ids": [1],
      "frame": "frames/0001.webp",
      "transcript": "[00:00:00.000 --> 00:00:06.000] Hey, welcome back…",
      "segments": [
        {"from": 0, "to": 6000, "text": "Hey, welcome back"}
      ]
    }
  ]
}
```

Field semantics (all required unless noted):

- `schema_version` — int, must equal the reader's supported version (1).
- `video_id` — the 11-char canonical id (or sanitized filename for local files).
- `source_url` — original URL; may be empty for local-file mode.
- `created_at` / `packed_at` — RFC 3339 UTC; when the pipeline built `output/`
  and when the store was packed.
- `total_chunks` / `total_duration` — mirrors `manifest.json` (sum of chunk
  durations).
- `reconstruction_md` — the `reconstruction.md` seed verbatim, so an agent can
  continue appending notes exactly as today.
- `instructions_md` — the playbook text, so the store is self-explanatory
  without the repo.
- `chunks` — the ordered playback spine. **Never reorder.** Mirrors
  `manifest.json`'s `chunks` array:
  - `id` — 1-based chunk number (`NNNN` zero-padded in member/frame names).
  - `start` / `end` — absolute seconds; `start` inclusive, `end` exclusive.
  - `duration` — `end - start`.
  - `source_ids` — raw chunk ids merged into this chunk (single-element when
    not merged; merged chunks extend `end` and concatenate transcripts).
  - `frame` — zip member path of the frame.
  - `transcript` — the concatenated transcript text with inline absolute
    timestamps, byte-identical to today's `chunks/<NNNN>/transcript.txt`.
  - `segments` — structured provenance from `work/<id>/transcripts/full.json`
    (whisper `-oj` output): `from`/`to` in **milliseconds**, `text` as produced.
    Enables precise segment-level search without re-parsing.

## `store/library.json` (per-machine index)

One file, updated by every `store pack`:

```json
{
  "schema_version": 1,
  "videos": [
    {
      "video_id": "BL8TfsLk3WM",
      "source_url": "https://youtu.be/BL8TfsLk3WM",
      "created_at": "2026-08-06T12:00:00Z",
      "packed_at": "2026-08-07T04:55:00Z",
      "total_chunks": 165,
      "total_duration": 1217.9,
      "store_file": "BL8TfsLk3WM.ytr"
    }
  ]
}
```

## Layout & lifecycle

```
store/
├── library.json
└── <video-id>.ytr
```

- `store/` is scratch/deliverable data: gitignored, never committed, wiped by
  `scripts/clean.sh` (unlike `results/`, which deliberately survives).
- `store pack` guards on `output/<video-id>/manifest.json` existing (mirrors
  the manifest stage's own guard) and is idempotent: skip if
  `store/<video-id>.ytr` exists; `--force` rebuilds.
- Writes are atomic: build the archive at `store/<video-id>.ytr.part`, then
  rename over the destination. A killed pack never fakes completion.
- `store verify <video-id>`: open the zip, read every member (stdlib verifies
  CRC-32), check `schema_version` is supported, check every `chunks[i].frame`
  exists and every `frames/*.webp` member is referenced, and report member
  sizes. Exit non-zero with a member-naming error on the first mismatch.

## Query surface (the agent's 1–2 commands)

| Command | Purpose |
|---|---|
| `ytreconstruct store list` | one line per video in `library.json` |
| `ytreconstruct store dump <id> [--json]` | the whole ordered story: metadata + every chunk's timespan, transcript, source ids — the "watch the video" command |
| `ytreconstruct store query <id> --grep <text> [--range t1,t2] [--json]` | ordered, filtered chunks (substring over transcripts + segments) |
| `ytreconstruct store frame <id> <NNNN> [out.png]` | materialize a frame as PNG (pixel-identical to the original) for OCR or agent vision |
| `ytreconstruct store verify <id>` | integrity check (see above) |

Jump-to-time: `--range t1,t2` binary-searches the ordered `chunks` spine for
the covering chunk — the timestamp index is the chunk list itself.
