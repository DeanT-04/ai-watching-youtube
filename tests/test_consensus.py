"""Tests for cross-frame line-consensus reconstruction."""

from ytextract.consensus import pair_line_numbers, reconstruct
from ytextract.ocr.base import OcrText


def _lines(*texts, conf=0.9):
    return [OcrText(text=t, confidence=conf) for t in texts]


def test_pair_line_numbers_attaches_number_to_following_content():
    lines = _lines("47", "string SymbolName;", "48", "datetime LastMainTFUpdate;")
    paired = pair_line_numbers(lines)
    assert paired == [
        (47, OcrText(text="string SymbolName;", confidence=0.9)),
        (48, OcrText(text="datetime LastMainTFUpdate;", confidence=0.9)),
    ]


def test_pair_line_numbers_handles_glued_number_and_content():
    """Real observed Tesseract (psm 6) shape: number and content share one
    OCR'd line, e.g. '46 struct SymbolInformation {'."""
    lines = _lines(
        "46 struct SymbolInformation {",
        "47 string SymbolName;",
        "48 datetime LastMainTFUpdate;",
    )
    paired = pair_line_numbers(lines)
    assert paired == [
        (46, OcrText(text="struct SymbolInformation {", confidence=0.9)),
        (47, OcrText(text="string SymbolName;", confidence=0.9)),
        (48, OcrText(text="datetime LastMainTFUpdate;", confidence=0.9)),
    ]


def test_pair_line_numbers_content_before_any_number_is_unanchored():
    lines = _lines("stray text", "6", "#property copyright \"Mr CapFree\"")
    paired = pair_line_numbers(lines)
    assert paired[0] == (None, OcrText(text="stray text", confidence=0.9))
    assert paired[1][0] == 6


def test_reconstruct_single_frame_passthrough():
    frame = _lines("6", '#property copyright "Mr CapFree"')
    result = reconstruct([frame])
    assert result.lines == {6: '#property copyright "Mr CapFree"'}
    assert result.frame_count == 1


def test_reconstruct_fills_gaps_a_single_frame_dropped():
    """This is the core failure mode observed in the real pipeline run:
    frame A captures lines 6-8 but drops 9-10 (truncation mid-block); frame B
    (a different keyframe of the same still-visible region) captures 8-10.
    Consensus should recover the union, not either frame's partial view.
    """
    frame_a = _lines(
        "6", "#property copyright \"Mr CapFree\"",
        "7", "#property link \"https://www.MrCapFree.com\"",
        "8", "#property version \"1.00\"",
    )
    frame_b = _lines(
        "8", "#property version \"1.00\"",
        "9", "#property strict",
        "10", "#property description \"Waka Waka EA\"",
    )
    result = reconstruct([frame_a, frame_b])
    assert result.covered_line_count == 5
    assert result.lines[9] == "#property strict"
    assert result.lines[10] == '#property description "Waka Waka EA"'
    assert "#property strict" in result.render()


def test_reconstruct_majority_vote_fixes_l_1_confusion():
    """Real observed error: 'bool' misread as 'boo1' by one engine/frame.
    Three independent reads of the same line, two correct one garbled ->
    majority vote (on the OCR-confusable-normalized text) should win.
    """
    frame_a = _lines("51", "bool           CanBuy;")
    frame_b = _lines("51", "boo1           CanBuy;")
    frame_c = _lines("51", "bool           CanBuy;")
    result = reconstruct([frame_a, frame_b, frame_c])
    assert result.lines[51] == "bool           CanBuy;"


def test_reconstruct_tie_breaks_on_confidence():
    frame_a = _lines("1", "x = 1")
    low_conf = OcrText(text="garbled", confidence=0.2)
    frame_b = [OcrText(text="1", confidence=0.9), low_conf]
    result = reconstruct([frame_a, frame_b])
    # Only one real candidate after the number pairs off; low-confidence
    # single reading still wins if it's the only *other* vote, but a real
    # tie (two distinct texts, one vote each) should prefer higher confidence.
    frame_c = _lines("1", "y = 1")
    result2 = reconstruct([frame_a, [OcrText(text="1", confidence=0.9),
                                      OcrText(text="y = 1", confidence=0.99)]])
    assert result2.lines[1] in {"x = 1", "y = 1"}
    assert result2.lines[1] == "y = 1"  # higher confidence wins the tie


def test_reconstruct_no_gutter_numbers_everything_unanchored():
    frame = _lines("no numbers here", "still no numbers")
    result = reconstruct([frame])
    assert result.lines == {}
    assert result.unanchored == ["no numbers here", "still no numbers"]


def test_reconstruct_empty_input():
    result = reconstruct([])
    assert result.lines == {}
    assert result.render() == ""
    assert result.frame_count == 0


def test_reconstruct_drops_isolated_outlier_from_misread_gutter_digit():
    """Real observed failure: a blurry bottom-edge row's gutter number misread
    as an unrelated single digit ("2") while every other nearby line number
    got multiple consistent votes clustered together (46-70). The stray
    singleton far outside that cluster should be dropped, not kept as a
    phantom 'line 2'.
    """
    frames_lines = []
    # lines 46-50 each seen by 3 frames (a trusted, clustered run)
    for _ in range(3):
        frames_lines.append(
            _lines(
                "46 struct SymbolInformation {",
                "47 string SymbolName;",
                "48 datetime LastMainTFUpdate;",
                "49 datetime LastOPOTFUpdate;",
                "50 int UpdateCounter;",
            )
        )
    # one blurry frame also misreads a clipped row as a lone "2"
    frames_lines.append(_lines("2 double LastSellTP;"))
    result = reconstruct(frames_lines)
    assert 2 not in result.lines
    assert result.lines[46] == "struct SymbolInformation {"


def test_reconstruct_keeps_genuine_singleton_near_the_cluster():
    """A line seen by only one frame is legitimate when it's consistent
    with (near) the trusted cluster -- singleton alone isn't the signal,
    being a distant outlier is.
    """
    frames_lines = []
    for _ in range(3):
        frames_lines.append(_lines("46 struct SymbolInformation {", "47 string SymbolName;"))
    frames_lines.append(_lines("48 datetime LastMainTFUpdate;"))  # only 1 frame saw line 48
    result = reconstruct(frames_lines)
    assert result.lines[48] == "datetime LastMainTFUpdate;"


def test_render_orders_by_line_number_not_arrival_order():
    frame_a = _lines("10", "second")
    frame_b = _lines("2", "first")
    result = reconstruct([frame_a, frame_b])
    assert result.render() == "first\nsecond"
