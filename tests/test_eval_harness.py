"""Regression gate for the OCR eval harness.

Runs the real scoring path (``scripts/eval_ocr.py``, tesseract-only for
determinism and speed) on every fixture and asserts each fixture's
line-recovery rate does not regress below the committed baseline
(``docs/eval_baseline.json``).

Marked ``integration`` (real tesseract binary + real frames) so it stays out
of the default coverage-gated run, consistent with tests/test_integration.py.
Run with:  .venv\\Scripts\\python.exe -m pytest -m integration -o addopts=""
"""

import json
import sys
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT / "scripts"))

from eval_ocr import load_manifests, run_case  # noqa: E402

BASELINE_PATH = ROOT / "docs" / "eval_baseline.json"

# How far a fixture may fall below its committed baseline before we fail.
# Line-recovery is the completeness metric; 0.1 of tolerance absorbs
# single-line OCR wobble without letting a real regression slip through.
REGRESSION_TOLERANCE = 0.1

pytestmark = pytest.mark.integration


def _load_baseline() -> dict:
    if not BASELINE_PATH.exists():
        pytest.skip(f"no committed baseline at {BASELINE_PATH}")
    data = json.loads(BASELINE_PATH.read_text(encoding="utf-8"))
    return {cid: c for cid, c in data["cases"].items()}


@pytest.fixture(scope="module")
def tesseract_engine():
    from ytextract.ocr import create_engine

    return create_engine("tesseract")


@pytest.mark.parametrize(
    "manifest",
    load_manifests(),
    ids=[m["case_id"] for m in load_manifests()],
)
def test_line_recovery_does_not_regress(manifest, tesseract_engine):
    baseline = _load_baseline()
    case_id = manifest["case_id"]
    if case_id not in baseline:
        pytest.skip(f"{case_id} has no committed baseline entry")
    committed = baseline[case_id]["line_recovery"]

    scores = run_case(manifest, [tesseract_engine])
    recovered = scores["line_recovery"]

    assert recovered >= committed - REGRESSION_TOLERANCE, (
        f"{case_id}: line-recovery regressed from {committed} to {recovered} "
        f"(> {REGRESSION_TOLERANCE} drop). Re-run scripts/eval_ocr.py and, if "
        f"the drop is real and intended, update docs/eval_baseline.json."
    )


def test_baseline_covers_all_fixtures():
    manifests = load_manifests()
    baseline = _load_baseline()
    missing = [m["case_id"] for m in manifests if m["case_id"] not in baseline]
    assert not missing, (
        f"docs/eval_baseline.json is missing entries for: {missing}. "
        f"Run scripts/eval_ocr.py to (re)generate it."
    )
