"""Cross-frame line-consensus reconstruction.

The rest of the pipeline OCRs every keyframe independently and hands back one
:class:`~ytextract.storage.CodeBlock` per frame. When a code region sits on
screen for several seconds, that produces N noisy, overlapping, *partial*
transcriptions of the same source text — and today nothing merges them, so
whichever lines a single frame happened to miss are gone from the output.

This module fixes that by using the IDE's own line-number gutter as a join
key. Editors like MetaEditor/VS Code print a stable line number next to every
line of code; across a run of frames covering the same region, most lines are
readable in *most* frames even if any single frame drops or garbles some of
them. Anchoring on line number and taking a majority vote per line turns many
partial, error-prone transcriptions into one much more complete and accurate
reconstruction — this is the same idea as multi-frame super-resolution,
applied to text instead of pixels.

Usage is a post-processing step over a contiguous run of same-region
:class:`~ytextract.ocr.base.OcrText` line lists (one list per keyframe):

    >>> reconstruct(frames_lines)
    ReconstructedSource(lines={6: "#property copyright \\"Mr CapFree\\"", ...}, ...)
"""

from __future__ import annotations

import re
from collections import Counter
from collections.abc import Sequence
from dataclasses import dataclass, field

from .ocr.base import OcrText

# A line-number token, standing alone on its own OCR'd line. Some engines
# (PaddleOCR in the observed pipeline output) box the gutter number and the
# code text separately, emitting them as two consecutive entries. Capped at
# 4 digits so we don't mistake a numeric literal in code for a gutter number.
_LINE_NUMBER_ALONE = re.compile(r"^\s*(\d{1,4})\s*$")

# A line-number token glued to the start of the content that follows it on
# the same physical line. This is what plain Tesseract (psm 6, one text line
# per screen row) actually produces -- the gutter number and the code share
# one bounding box, e.g. "46 struct SymbolInformation {". Requires at least
# one non-digit, non-space character after the number+whitespace so we don't
# accidentally eat the start of a numeric code value.
_LINE_NUMBER_GLUED = re.compile(r"^\s*(\d{1,4})\s+(\S.*)$")

# OCR-confusable character classes we normalize before voting so that e.g.
# "boo1" and "bool" are recognized as the *same* candidate line instead of
# splitting the vote between two spellings that should be one.
_CONFUSABLE_TRANSLATION = str.maketrans(
    {
        "1": "l",
        "0": "o",
        "|": "l",
    }
)


@dataclass(frozen=True)
class ReconstructedSource:
    """Consensus text per detected source line number, plus bookkeeping.

    ``lines`` maps a 1-based source line number to the winning text for that
    line. ``unanchored`` holds OCR'd text that never got paired with a line
    number (e.g. a frame that didn't show the gutter) — kept separately so
    callers can decide whether to append it rather than silently drop it.
    ``frame_count`` and ``covered_line_count`` are there purely for eval /
    logging: they tell you how much redundancy actually backed each answer.
    """

    lines: dict[int, str]
    unanchored: list[str] = field(default_factory=list)
    frame_count: int = 0

    def render(self) -> str:
        """Join the consensus lines in line-number order into one text block.

        Gaps (line numbers never observed in any frame) are left out rather
        than padded with blanks, since we have no evidence about them.
        """
        return "\n".join(self.lines[n] for n in sorted(self.lines))

    @property
    def covered_line_count(self) -> int:
        return len(self.lines)


def _normalize_for_voting(text: str) -> str:
    """Fold common OCR confusions so near-identical reads count as one vote."""
    return text.strip().lower().translate(_CONFUSABLE_TRANSLATION)


def pair_line_numbers(lines: Sequence[OcrText]) -> list[tuple[int | None, OcrText]]:
    """Pair each content line with the gutter number that labels it.

    Handles both observed OCR shapes:

    - **glued** (plain Tesseract, psm 6): number and content share one
      entry, e.g. ``"46 struct SymbolInformation {"`` -> split directly into
      ``(46, "struct SymbolInformation {")``, with the number itself
      stripped from the returned text.
    - **separate** (PaddleOCR-style box detection): a standalone number
      entry ("47") followed by one or more separate content entries (paddle
      word-boxes split ``"string SymbolName;"`` into ``"string"`` and
      ``"SymbolName;"``). The number is attached to all consecutive content
      entries that follow it, until the next number resets it — they are
      joined with a space so a split line still yields one (number, text)
      pair instead of dropping the tail as unanchored.

    A content line before any number is seen (or when the gutter wasn't
    captured at all) is paired with ``None``.
    """
    paired: list[tuple[int | None, OcrText]] = []
    current: int | None = None
    for item in lines:
        glued = _LINE_NUMBER_GLUED.match(item.text)
        if glued:
            current = int(glued.group(1))
            paired.append(
                (current, OcrText(text=glued.group(2), confidence=item.confidence))
            )
            current = None
            continue
        alone = _LINE_NUMBER_ALONE.match(item.text)
        if alone:
            current = int(alone.group(1))
            continue
        if (
            current is not None
            and paired
            and paired[-1][0] == current
        ):
            # Another word-box from the same gutter line (paddle split the
            # line into type + name + ...). Join it onto the previous pair so
            # the whole line votes as one candidate, not as competing texts.
            prev_num, prev_text = paired[-1]
            paired[-1] = (
                prev_num,
                OcrText(
                    text=f"{prev_text.text} {item.text}".strip(),
                    confidence=min(prev_text.confidence, item.confidence),
                ),
            )
            continue
        paired.append((current, item))
    return paired


def _vote(candidates: list[OcrText]) -> str:
    """Pick the winning text for one line number from all frames that saw it.

    Majority vote on the OCR-confusion-normalized text (so "boo1" and "bool"
    vote together); ties and singletons fall back to the highest-confidence
    *original* (non-normalized) reading, since normalization is only a
    voting aid, not something we want to bake into the output.
    """
    if len(candidates) == 1:
        return candidates[0].text
    counts = Counter(_normalize_for_voting(c.text) for c in candidates)
    winning_key, winning_votes = counts.most_common(1)[0]
    tied_keys = [k for k, v in counts.items() if v == winning_votes]
    pool = candidates if len(tied_keys) > 1 else [
        c for c in candidates if _normalize_for_voting(c.text) == winning_key
    ]
    return max(pool, key=lambda c: c.confidence).text


def _drop_isolated_outliers(
    votes: dict[int, list[OcrText]],
) -> dict[int, list[OcrText]]:
    """Drop line numbers that are almost certainly gutter-digit misreads.

    Observed failure mode (real frames, bottom edge of a crop): a blurry or
    partially-clipped row has its gutter number misread as an unrelated
    number, e.g. "74" read as "78", or a single stray digit like "2"/"7"
    read off a half-visible row. These show up as a line number with exactly
    one supporting frame that sits far outside the contiguous cluster every
    other (multi-vote) line number falls in. Numbers with only one vote are
    kept as-is if they're near the trusted cluster -- being a singleton
    isn't itself suspicious, since short runs of frames legitimately produce
    single-vote lines; it's the *combination* of singleton and far-outlier
    that flags a misread.
    """
    trusted = [n for n, c in votes.items() if len(c) > 1]
    if len(trusted) < 2:
        return votes
    lo, hi = min(trusted), max(trusted)
    span = max(hi - lo, 1)
    # A singleton further from the trusted cluster than the cluster's own
    # span is treated as a misread rather than a genuine one-off line.
    margin = span
    return {
        n: c
        for n, c in votes.items()
        if len(c) > 1 or (lo - margin <= n <= hi + margin)
    }


def reconstruct(frames_lines: Sequence[Sequence[OcrText]]) -> ReconstructedSource:
    """Merge OCR line lists from multiple frames of the *same* region.

    ``frames_lines`` is one list of :class:`OcrText` per keyframe, in any
    order (e.g. every keyframe belonging to one stable-scroll run of a file).
    Every occurrence of each gutter line number across every frame becomes
    one vote for that line's text; the winner is stored in the result.
    """
    votes: dict[int, list[OcrText]] = {}
    unanchored: list[str] = []
    for frame_lines in frames_lines:
        for number, item in pair_line_numbers(frame_lines):
            if number is None:
                if item.text.strip():
                    unanchored.append(item.text)
                continue
            votes.setdefault(number, []).append(item)

    votes = _drop_isolated_outliers(votes)
    resolved = {number: _vote(candidates) for number, candidates in votes.items()}
    return ReconstructedSource(
        lines=resolved, unanchored=unanchored, frame_count=len(frames_lines)
    )
