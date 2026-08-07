#!/usr/bin/env bash
#
# clean.sh — wipe everything ytreconstruct produced: scratch (work/) and
# deliverables (output/). Source code is never touched.
#
# Safe by default: asks for confirmation unless -y is passed.
#
# Usage:
#   scripts/clean.sh          # prompt before deleting
#   scripts/clean.sh -y       # skip the prompt
#
# If you change what the tool writes, update the dirs below to match.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIRS=(work output store)

ask() {
  printf '%s\n' "This will permanently delete:"
  for d in "${DIRS[@]}"; do
    [ -e "$ROOT/$d" ] && printf '  - %s/\n' "$ROOT/$d"
  done
  printf '%s ' "Continue? [y/N] "
  read -r answer
  case "$answer" in
    y|Y|yes|YES) return 0 ;;
    *) return 1 ;;
  esac
}

[ $# -eq 1 ] && [ "$1" = "-y" ] && FORCE=1 || FORCE=0

deleted=0
for d in "${DIRS[@]}"; do
  if [ -e "$ROOT/$d" ]; then
    if [ "$FORCE" -eq 0 ] && ! ask; then
      printf '%s\n' "Aborted — nothing was deleted."
      exit 0
    fi
    rm -rf -- "$ROOT/$d"
    printf '%s\n' "Deleted $ROOT/$d/"
    deleted=1
  fi
done

[ "$deleted" -eq 0 ] && printf '%s\n' "Nothing to clean — work/ and output/ are already absent."
exit 0
