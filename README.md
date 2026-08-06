# ytreconstruct

Turn a YouTube video into an ordered set of **(frame, audio transcript, timestamp)** chunks on disk, so a separate coding agent can later "watch" the video in the correct order and reconstruct exactly what appeared on screen — code, prompts, configs — without jumbling the sequence.

> Status: under construction. See `PROGRESS.md` for where the build stands.

## Requirements

Local binaries, checked at startup by the tool (it never installs them):

- [`yt-dlp`](https://github.com/yt-dlp/yt-dlp) — video download
- [`ffmpeg`](https://ffmpeg.org/) — scene detection, frame + audio extraction
- [`whisper-cli`](https://github.com/ggml-org/whisper.cpp) — local, CPU-only speech transcription
- a ggml whisper model (e.g. `ggml-base.bin`) — see below

Everything runs fully offline once the video is downloaded. No cloud APIs, no GPU required.

## Usage

```
ytreconstruct all <url>                # full pipeline
ytreconstruct download <url>           # phase 1: fetch video + audio
ytreconstruct chunk <video_id>         # phase 2: scene chunks
ytreconstruct dedupe <video_id>        # phase 3: merge static periods
ytreconstruct transcribe <video_id>    # phase 4: local whisper
ytreconstruct manifest <video_id>      # phase 5: ordered output tree
```

(Full flag reference lands with the completed build — see `PROGRESS.md`.)
