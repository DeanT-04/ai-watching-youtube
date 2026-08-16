"""OCR eval harness: score the real pipeline against hand-transcribed fixtures.

Runs the *real* pipeline functions (``cli._to_code_block_group``) on each
fixture's frames (from the same video the fixtures were transcribed from) and
diffs the recovered code against the committed ground truth. Emits per-case
CER/WER/line-recovery/silent-drop plus MQL5 lint signals, and writes a JSON
baseline the regression gate (tests/test_eval_harness.py) checks against.

Usage:
    python scripts/eval_ocr.py                    # real pipeline (paddle+tesseract)
    python scripts/eval_ocr.py --engines tesseract  # fast, deterministic tesseract-only
    python scripts/eval_ocr.py --case struct-symbol-information   # single fixture
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path

import cv2

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "src"))

from ytextract.cli import _to_code_block_group
from ytextract.config import Config
from ytextract.keyframes import Frame
from ytextract.ocr import create_engine

FIXTURES_DIR = Path(__file__).resolve().parent.parent / "tests" / "fixtures" / "ocr_eval"
FRAMES_DIR = Path(__file__).resolve().parent.parent / "frames" / "aDWDJrACs7s"
BASELINE_PATH = Path(__file__).resolve().parent.parent / "docs" / "eval_baseline.json"

WHITESPACE = re.compile(r"\s+")

# MetaEditor wizard chrome: banner rules (//+---+) and boxed title comments
# (//| ... |). These are decorative, OCR as garbage, and their exact dash
# count is not pixel-verifiable, so they are excluded from scoring — the
# harness measures *code* recovery, not comment-chrome reproduction.
_BANNER_LINE = re.compile(r"^//[+|=]")
_BOXED_COMMENT = re.compile(r"^//\|")

# MQL5 keywords + built-in type names, for the near-miss dictionary check.
MQL5_KEYWORDS = {
    "void", "int", "double", "bool", "string", "long", "char", "float",
    "datetime", "color", "enum", "struct", "class", "input", "return",
    "if", "else", "for", "while", "switch", "case", "break", "continue",
    "const", "static", "new", "delete", "true", "false", "this",
    "include", "property", "define", "uchar", "ushort", "uint", "ulong",
}


def _levenshtein(a: str, b: str) -> int:
    """Standard Levenshtein distance (no dependency on jiwer/rapidfuzz)."""
    if a == b:
        return 0
    if not a:
        return len(b)
    if not b:
        return len(a)
    prev = list(range(len(b) + 1))
    for i, ca in enumerate(a, 1):
        cur = [i]
        for j, cb in enumerate(b, 1):
            cur.append(min(prev[j] + 1, cur[j - 1] + 1, prev[j - 1] + (ca != cb)))
        prev = cur
    return prev[-1]


def char_error_rate(ref: str, hyp: str) -> float:
    if not ref:
        return 0.0 if not hyp else 1.0
    return _levenshtein(ref, hyp) / len(ref)


def word_error_rate(ref: str, hyp: str) -> float:
    rw, hw = ref.split(), hyp.split()
    if not rw:
        return 0.0 if not hw else 1.0
    return _levenshtein(rw, hw) / len(rw)


def normalize(text: str) -> str:
    """Collapse whitespace for line matching (not for CER/WER scoring)."""
    return WHITESPACE.sub(" ", text).strip()


def _line_match(gt_line: str, out_lines: list[str]) -> tuple[str | None, float]:
    """Best-matching output line for a ground-truth line, by char error rate.

    Returns (matched_text, best_err). ``gt_line`` is the normalized GT line;
    if the GT line is a substring of an output line, the *matched substring*
    (i.e. the GT line itself) is returned with error 0. Otherwise we pick the
    output line with the lowest CER against the GT line, as long as that's
    below a lenient threshold.
    """
    best, best_err = None, float("inf")
    for ol in out_lines:
        if gt_line and gt_line in ol:
            return gt_line, 0.0
        err = char_error_rate(gt_line, ol)
        if err < best_err:
            best, best_err = ol, err
    # Only treat as a match if at least half the GT characters are right.
    if best is not None and best_err <= 0.5:
        return best, best_err
    return None, 1.0


def score_case(gt_lines: list[str], out_lines: list[str]) -> dict:
    """Compute line-recovery, silent-drop, CER/WER for one fixture.

    ``gt_lines`` / ``out_lines`` are the raw (pre-normalized) lines. Matching
    is done on whitespace-normalized text; CER/WER are computed on the
    normalized matched pairs (for substring matches, the matched substring is
    compared, so a contained GT line scores CER/WER 0).
    """
    gt_norm = [normalize(l) for l in gt_lines if normalize(l)]
    out_norm = [normalize(l) for l in out_lines if normalize(l)]

    matched_pairs: list[tuple[str, str]] = []
    recovered = 0
    silent_drops = 0
    for g in gt_norm:
        m, err = _line_match(g, out_norm)
        if err <= 0.5:
            recovered += 1
            matched_pairs.append((g, m if m is not None else g))
        else:
            silent_drops += 1

    line_recovery = recovered / len(gt_norm) if gt_norm else 0.0

    if matched_pairs:
        cer = sum(char_error_rate(r, h) for r, h in matched_pairs) / len(matched_pairs)
        wer = sum(word_error_rate(r, h) for r, h in matched_pairs) / len(matched_pairs)
    else:
        cer = wer = 1.0

    return {
        "gt_lines": len(gt_norm),
        "out_lines": len(out_norm),
        "recovered_lines": recovered,
        "silent_drops": silent_drops,
        "line_recovery": round(line_recovery, 4),
        "cer": round(cer, 4),
        "wer": round(wer, 4),
    }


# MQL5-specific lint signals -------------------------------------------------

_BRACES = {"{": "}", "(": ")", "[": "]"}


def balanced_delimiters(text: str) -> dict:
    """Report unbalanced {}, (), [] on the recovered text."""
    out = {}
    for open_c, close_c in _BRACES.items():
        opens = text.count(open_c)
        closes = text.count(close_c)
        if opens != closes:
            out[f"{open_c}{close_c}"] = f"{opens} open vs {closes} close"
    return out


_NEAR_MISS = str.maketrans({"0": "o", "1": "l", "5": "s", "8": "b", "3": "e", "@": "a"})


def keyword_near_misses(text: str) -> list[str]:
    """Flag tokens that are an OCR-confusion away from an MQL5 keyword/type.

    E.g. ``boo1`` -> ``bool``, ``atetime`` -> ``datetime``, ``voi d`` -> ``void``.
    Returns human-readable "token -> keyword" strings.
    """
    tokens = re.findall(r"[A-Za-z][A-Za-z0-9_]*", text)
    hits = []
    for tok in tokens:
        folded = tok.lower().translate(_NEAR_MISS)
        if tok.lower() in MQL5_KEYWORDS:
            continue
        for kw in MQL5_KEYWORDS:
            if folded == kw and tok.lower() != kw:
                hits.append(f"{tok} -> {kw}")
    return sorted(set(hits))


def monotonic_line_numbers(text: str) -> bool:
    """True if embedded gutter line numbers are non-decreasing, where present.

    The real pipeline strips gutter numbers from consensus output, so this is
    a no-op signal on consensus text; it matters for the single-frame path.
    """
    nums = [int(m) for m in re.findall(r"^\s*(\d{1,4})\s", text, re.MULTILINE)]
    return nums == sorted(nums)


def run_case(manifest: dict, engines: list) -> dict:
    """Run the real pipeline path on one fixture and score it."""
    case_dir = manifest["_case_dir"]
    gt_text = (case_dir / manifest["ground_truth"]).read_text(encoding="utf-8")
    # Exclude decorative banner/boxed-comment chrome from scoring: it is not
    # code, OCRs as garbage, and its exact dash count isn't verifiable.
    gt_lines = [
        l for l in gt_text.splitlines()
        if not _BANNER_LINE.match(l) and not _BOXED_COMMENT.match(l)
    ]

    cfg = Config.load(
        {
            "YTEXTRACT_CROP": ",".join(str(v) for v in manifest["crop"]),
            "YTEXTRACT_UPSCALE_SCALE": "2",
            "YTEXTRACT_SIMILARITY_THRESHOLD": "0.98",
            "YTEXTRACT_CONSENSUS_MAX_FRAMES_PER_GROUP": "8",
        }
    )

    group = []
    for name in manifest["frames"]:
        img = cv2.imread(str(FRAMES_DIR / name))
        if img is None:
            raise FileNotFoundError(f"missing frame: {FRAMES_DIR / name}")
        t = int(re.search(r"t_(\d+)\.png", name).group(1))
        group.append(Frame(timestamp=float(t), image=img))

    block = _to_code_block_group(group, cfg, engines)
    out_lines = block.text.splitlines()

    scores = score_case(gt_lines, out_lines)
    scores["balanced_delimiters"] = balanced_delimiters(block.text)
    scores["keyword_near_misses"] = keyword_near_misses(block.text)
    scores["monotonic_line_numbers"] = monotonic_line_numbers(block.text)
    scores["pipeline_source"] = block.source
    scores["pipeline_valid"] = block.valid
    return scores


def load_manifests(case_filter: str | None = None) -> list[dict]:
    manifests = []
    for mf in sorted(FIXTURES_DIR.glob("*/manifest.json")):
        m = json.loads(mf.read_text(encoding="utf-8"))
        if case_filter and m.get("case_id", mf.parent.name) != case_filter:
            continue
        m["_case_dir"] = mf.parent
        m.setdefault("case_id", mf.parent.name)
        manifests.append(m)
    return manifests


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--engines", default="paddle,tesseract",
                        help="comma-separated engine list (default: paddle,tesseract)")
    parser.add_argument("--case", default=None, help="run a single fixture case_id")
    parser.add_argument("--baseline", default=str(BASELINE_PATH),
                        help="path to write the JSON baseline")
    args = parser.parse_args(argv)

    engine_names = [e.strip() for e in args.engines.split(",") if e.strip()]
    engines = [create_engine(name) for name in engine_names]

    manifests = load_manifests(args.case)
    if not manifests:
        print(f"no fixtures found", file=sys.stderr)
        return 1

    results = {}
    print(f"{'case':28s} {'cond':10s} {'rec%':>6} {'CER':>6} {'WER':>6} {'drop':>5}")
    print("-" * 66)
    for m in manifests:
        case_id = m["case_id"]
        scores = run_case(m, engines)
        results[case_id] = scores
        print(
            f"{case_id:28s} {m['condition']:10s} "
            f"{scores['line_recovery']*100:5.1f}% "
            f"{scores['cer']:>6.3f} {scores['wer']:>6.3f} "
            f"{scores['silent_drops']:>5}"
        )

    payload = {
        "engines": engine_names,
        "cases": results,
    }
    Path(args.baseline).write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")
    print(f"\nbaseline written to {args.baseline}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
