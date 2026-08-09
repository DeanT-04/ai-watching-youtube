"""Trivial smoke test so the Phase 0 gate (fresh clone + venv + pip install + pytest) is green."""


def test_package_imports() -> None:
    import ytextract  # noqa: F401

    assert ytextract.__version__
