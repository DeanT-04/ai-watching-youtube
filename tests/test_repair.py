"""Tests for repair.py — the accuracy-critical module, malformed inputs included."""

from unittest import mock

from ytextract.repair import (
    VALIDATORS,
    RepairedCode,
    RepairIssue,
    register_validator,
    repair,
    validate_python,
)

GOOD = "def main():\n    print('hello')\n\nmain()\n"


def test_valid_python_has_no_issues():
    result = repair(GOOD, "python")
    assert result.valid is True
    assert result.issues == []
    assert result.code == GOOD
    assert result.language == "python"


def test_python_with_trailing_whitespace_and_newlines():
    messy = "x = 1   \n\n\n\n"
    result = repair(messy, "python")
    assert result.valid is True


def test_python_comment_only_is_valid():
    assert repair("# just a comment\n", "python").valid is True


def test_python_empty_is_valid():
    assert repair("", "python").valid is True


def test_syntax_error_reports_line_number():
    bad = "def broken(:\n    pass\n"
    result = repair(bad, "python")
    assert result.valid is False
    assert result.issues == [
        RepairIssue(line=1, message="invalid syntax (line 1, column 12)")
    ]


def test_syntax_error_without_offset(monkeypatch):
    err = SyntaxError("boom")
    err.lineno = 7
    err.offset = None
    monkeypatch.setattr("ytextract.repair.ast.parse", mock.Mock(side_effect=err))
    result = repair("anything", "python")
    assert result.issues == [RepairIssue(line=7, message="boom (line 7)")]


def test_error_on_later_line():
    bad = "x = 1\ny = 2\nif True print('nope')\n"
    result = repair(bad, "python")
    assert result.valid is False
    assert result.issues[0].line == 3


def test_indentation_error_is_reported():
    bad = "def f():\nprint('no indent')\n"
    result = repair(bad, "python")
    assert result.valid is False
    assert result.issues[0].line == 2
    assert "indent" in result.issues[0].message.lower()


def test_deeply_nested_code_reports_without_crashing(monkeypatch):
    # CPython 3.12's parser raises SyntaxError for deep nesting, so the
    # RecursionError handler is defensive; exercise it directly.
    monkeypatch.setattr(
        "ytextract.repair.ast.parse", mock.Mock(side_effect=RecursionError())
    )
    result = repair("x", "python")
    assert result.valid is False
    assert "nested" in result.issues[0].message


def test_unknown_language_is_not_validated():
    result = repair("SELECT 1;", "sql")
    assert result.valid is None
    assert len(result.issues) == 1
    assert "no validator" in result.issues[0].message


def test_language_matching_is_case_insensitive():
    assert repair(GOOD, "Python").valid is True
    assert repair(GOOD, "PYTHON").valid is True


def test_register_validator_adds_language():
    def fake_validator(code):
        if "bad" in code:
            return [RepairIssue(line=1, message="contains 'bad'")]
        return []

    register_validator("fake", fake_validator)
    try:
        assert "fake" in VALIDATORS
        assert repair("bad stuff", "fake").valid is False
        assert repair("good stuff", "fake").valid is True
    finally:
        del VALIDATORS["fake"]


def test_validate_python_directly():
    assert validate_python(GOOD) == []
    assert validate_python("x =\n") != []


def test_repaired_code_dataclass_fields():
    result = repair(GOOD, "python")
    assert isinstance(result, RepairedCode)
    assert isinstance(result.issues, list)
