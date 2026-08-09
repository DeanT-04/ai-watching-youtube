"""OCR ensemble package: engine interface + Paddle/Tesseract engines + merge."""

from .base import OcrEngine, OcrError, OcrResult, OcrText
from .ensemble import create_engine, merge_results, run_ocr
from .paddle_engine import PaddleEngine
from .tesseract_engine import TesseractEngine, parse_tsv

__all__ = [
    "OcrEngine",
    "OcrError",
    "OcrResult",
    "OcrText",
    "PaddleEngine",
    "TesseractEngine",
    "create_engine",
    "merge_results",
    "parse_tsv",
    "run_ocr",
]
