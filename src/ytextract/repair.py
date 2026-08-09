"""Syntax-validated repair for OCR'd code blocks.

Raw OCR text is validated against a per-language validator and failures are
reported with line numbers. New languages are added by registering a
``validator(code: str) -> list[RepairIssue]`` in :data:`VALIDATORS` — callers
only ever call :func:`repair`. Validation *reports* problems; it deliberately
does not rewrite code (repairing OCR artifacts heuristically would introduce
its own errors).
"""

from __future__ import annotations

import ast
from collections.abc import Callable
from dataclasses import dataclass, field

Validator = Callable[[str], list["RepairIssue"]]


@dataclass(frozen=True)
class RepairIssue:
    """A single validation problem; ``line`` is 1-based or None if unknown."""

    line: int | None
    message: str


@dataclass(frozen=True)
class RepairedCode:
    """Validation outcome for one OCR'd code block.

    ``valid`` is True (parses), False (has issues), or None when no validator
    is registered for the language.
    """

    code: str
    language: str
    valid: bool | None
    issues: list[RepairIssue] = field(default_factory=list)


def validate_python(code: str) -> list[RepairIssue]:
    """Validate ``code`` with ``ast.parse``; return any syntax issues."""
    try:
        ast.parse(code)
    except SyntaxError as exc:
        message = exc.msg or "syntax error"
        if exc.lineno is not None:
            if exc.offset:
                message = f"{message} (line {exc.lineno}, column {exc.offset})"
            else:
                message = f"{message} (line {exc.lineno})"
        return [RepairIssue(line=exc.lineno, message=message)]
    except RecursionError:
        return [RepairIssue(line=None, message="code is too deeply nested to parse")]
    return []


VALIDATORS: dict[str, Validator] = {
    "python": validate_python,
}


def register_validator(language: str, validator: Validator) -> None:
    """Register a validator for ``language`` (extension point for new languages)."""
    VALIDATORS[language.lower()] = validator


def repair(code: str, language: str) -> RepairedCode:
    """Validate OCR'd ``code`` for ``language`` and report issues with line numbers."""
    validator = VALIDATORS.get(language.lower())
    if validator is None:
        return RepairedCode(
            code=code,
            language=language,
            valid=None,
            issues=[
                RepairIssue(
                    line=None, message=f"no validator for language {language!r}"
                )
            ],
        )
    issues = validator(code)
    return RepairedCode(code=code, language=language, valid=not issues, issues=issues)
