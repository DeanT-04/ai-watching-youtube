"""Entry point so the CLI runs as ``python -m ytextract <url>``."""

from ytextract.cli import (
    main,
)  # pragma: no cover - only runs in subprocess (`python -m ytextract`); smoke-tested in tests/test_cli.py

if (
    __name__ == "__main__"
):  # pragma: no cover - runpy re-execution deadlocks under pytest; smoke-tested via subprocess in tests/test_cli.py
    raise SystemExit(main())
