#!/usr/bin/env bash
# Pending-check orchestrator: waits for connectivity, then runs the real
# Phase 2 download sanity check and the Phase 9 end-to-end run.
set -u
LOG=/tmp/pending-e2e.log
URL="https://youtu.be/aDWDJrACs7s?si=os_qJFReL22zxSex"
ROOT="/c/Users/Deano/Documents/projects/yt-video-watcher-ai"
cd "$ROOT" || exit 1

echo "[$(date +%H:%M:%S)] waiting for youtube.com..." | tee "$LOG"

# 1) Wait for youtube connectivity (max ~2h).
UP=0
for i in $(seq 1 120); do
  if curl -s -o /dev/null --connect-timeout 10 -w "%{http_code}" "https://www.youtube.com" 2>/dev/null | grep -q 200; then
    UP=1; break
  fi
  sleep 60
done

if [ "$UP" != "1" ]; then
  echo "[$(date +%H:%M:%S)] youtube.com never became reachable; aborting e2e." | tee -a "$LOG"
  exit 2
fi
echo "[$(date +%H:%M:%S)] youtube.com reachable." | tee -a "$LOG"

# 2) Install paddlepaddle 2.6.2 from the curl-downloaded wheel if ready.
WHEEL=/tmp/pdldl/paddlepaddle-2.6.2-cp312-cp312-win_amd64.whl
if [ -f "$WHEEL" ] && [ "$(stat -c %s "$WHEEL" 2>/dev/null || echo 0)" -gt 80000000 ]; then
  echo "[$(date +%H:%M:%S)] installing paddlepaddle from wheel + paddleocr..." | tee -a "$LOG"
  .venv/Scripts/python.exe -m pip install --no-input "$WHEEL" "paddleocr==2.7.3" >> "$LOG" 2>&1 \
    && echo "[$(date +%H:%M:%S)] paddle installed." | tee -a "$LOG" \
    || echo "[$(date +%H:%M:%S)] paddle install FAILED (see log); will rely on tesseract fallback." | tee -a "$LOG"
else
  echo "[$(date +%H:%M:%S)] paddle wheel not ready; relying on tesseract fallback." | tee -a "$LOG"
fi

# 3) Real Phase 2 download sanity check.
echo "[$(date +%H:%M:%S)] phase-2 sanity: downloading $URL" | tee -a "$LOG"
PYTHONPATH=src .venv/Scripts/python.exe -c "
import time
from ytextract.config import Config
from ytextract.downloader import download_video
t0 = time.time()
m = download_video('$URL', Config.load())
print('DOWNLOAD_OK', m.video_path, m.audio_path, repr(m.metadata.title), round(time.time()-t0,1), 's')
" >> "$LOG" 2>&1 && echo "[$(date +%H:%M:%S)] phase-2 sanity OK" | tee -a "$LOG" \
  || echo "[$(date +%H:%M:%S)] phase-2 sanity FAILED (see log)" | tee -a "$LOG"

# 4) Real Phase 9 end-to-end run.
echo "[$(date +%H:%M:%S)] phase-9 e2e: ytextract $URL" | tee -a "$LOG"
PYTHONPATH=src .venv/Scripts/python.exe -m ytextract "$URL" >> "$LOG" 2>&1 \
  && echo "[$(date +%H:%M:%S)] E2E_OK" | tee -a "$LOG" \
  || echo "[$(date +%H:%M:%S)] E2E_FAILED (see log)" | tee -a "$LOG"

echo "[$(date +%H:%M:%S)] pending checks finished; full log: $LOG" | tee -a "$LOG"
