#!/usr/bin/env bash
#
# integration.sh — end-to-end test of the full ytreconstruct pipeline,
# fully offline: it synthesizes a 6-second, 3-scene test video with ffmpeg
# (no network, no YouTube) and runs `all` against it, then asserts the
# manifest and chunk tree come out valid.
#
# If whisper-cli + a model are available the transcription stage runs for
# real; otherwise the run falls back to --skip-transcribe and the script
# reports that transcripts were not exercised.
#
# Usage: scripts/integration.sh
# Exit 0 = pipeline produced a valid deliverable, exit 1 = something failed.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP="$(mktemp -d)"
# The Go binary is a native Windows exe: give it real Windows paths, not
# git-bash/MSYS paths (cygpath converts; MSYS auto-conversion is unreliable).
WIN_TMP="$(cygpath -w "$TMP" 2>/dev/null || printf '%s' "$TMP")"
trap 'rm -rf -- "$TMP"' EXIT

fail() { printf 'integration: FAIL: %s\n' "$1" >&2; exit 1; }
pass() { printf 'integration: ok: %s\n' "$1"; }

for bin in ffmpeg ffprobe; do
  command -v "$bin" >/dev/null 2>&1 || fail "required binary '$bin' not found in PATH"
done

# --- 0. build the binary ---------------------------------------------------
printf 'integration: building ytreconstruct...\n'
( cd "$ROOT" && go build -o "$TMP/ytreconstruct" ./cmd/ytreconstruct )

# --- 1. synthesize a 3-scene test video ------------------------------------
printf 'integration: synthesizing 6s / 3-scene test video...\n'
ffmpeg -y -loglevel error \
  -f lavfi -i "smptebars=duration=2:size=320x240:rate=10" \
  -f lavfi -i "smptehdbars=duration=2:size=320x240:rate=10" \
  -f lavfi -i "rgbtestsrc=duration=2:size=320x240:rate=10" \
  -f lavfi -i "sine=frequency=440:duration=6" \
  -filter_complex "[0:v][1:v][2:v]concat=n=3:v=1:a=0[v]" \
  -map "[v]" -map 3:a \
  -c:v libx264 -preset ultrafast -c:a pcm_s16le -t 6 \
  "$TMP/test.mp4"

# --- 2. decide whether transcription runs for real --------------------------
WHISPER_MODEL="${WHISPER_MODEL:-$(cygpath -w "$HOME/.cache/ytreconstruct/whisper/ggml-base.bin" 2>/dev/null || printf '%s' "$HOME/.cache/ytreconstruct/whisper/ggml-base.bin")}"
SKIP=""
if command -v whisper-cli >/dev/null 2>&1 && [ -f "$WHISPER_MODEL" ]; then
  pass "whisper-cli + model found ($WHISPER_MODEL) — transcription will run"
else
  SKIP="--skip-transcribe"
  pass "whisper-cli/model not found — running with --skip-transcribe (transcripts not exercised)"
fi

# --- 3. run the full pipeline ------------------------------------------------
printf 'integration: running ytreconstruct all --file ...\n'
"$TMP/ytreconstruct" all --file "$WIN_TMP/test.mp4" \
  --work-dir "$WIN_TMP/work" --output-dir "$WIN_TMP/output" \
  --jobs 2 --model "$WHISPER_MODEL" $SKIP \
  || fail "pipeline run exited nonzero"

OUT="$WIN_TMP/output/test"
[ -f "$OUT/manifest.json" ] || fail "manifest.json missing"
[ -f "$OUT/reconstruction.md" ] || fail "reconstruction.md missing"

# --- 4. validate manifest.json -----------------------------------------------
python - "$OUT/manifest.json" <<'PYEOF' || fail "manifest validation script failed"
import json, os, sys

path = sys.argv[1]
with open(path, encoding="utf-8") as f:
    m = json.load(f)

errors = []
if m.get("total_chunks") != 3:
    errors.append(f"total_chunks = {m.get('total_chunks')}, want 3")
if abs(m.get("total_duration", 0) - 6.0) > 0.01:
    errors.append(f"total_duration = {m.get('total_duration')}, want 6.0")
chunks = m.get("chunks", [])
if len(chunks) != 3:
    errors.append(f"chunks length = {len(chunks)}, want 3")
starts = [c.get("start") for c in chunks]
if starts != sorted(starts):
    errors.append(f"chunks not in ascending start order: {starts}")
if starts != [0.0, 2.0, 4.0]:
    errors.append(f"chunk starts = {starts}, want [0.0, 2.0, 4.0]")
for c in chunks:
    for key in ("frame", "transcript", "meta"):
        rel = c.get(key)
        if not rel or not os.path.isfile(os.path.join(os.path.dirname(path), rel)):
            errors.append(f"chunk {c.get('id')}: missing {key} -> {rel}")
    if len(c.get("source_ids", [])) != 1:
        errors.append(f"chunk {c.get('id')}: source_ids = {c.get('source_ids')}, want single source")
if errors:
    sys.stderr.write("\n".join(errors) + "\n")
    sys.exit(1)
print("integration: ok: manifest valid: 3 ordered chunks, 6.0s, all files present")
PYEOF

# --- 5. optional: transcripts exercised? --------------------------------------
if [ -z "$SKIP" ]; then
  for n in 0001 0002 0003; do
    [ -s "$OUT/chunks/$n/transcript.txt" ] \
      || fail "chunk $n transcript.txt missing/empty after real transcription"
  done
  pass "transcripts present and non-empty for all 3 chunks"
fi

pass "full pipeline produced a valid deliverable"
printf 'integration: PASS\n'
