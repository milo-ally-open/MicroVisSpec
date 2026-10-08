"""Export the trained YOLO detection models to ONNX."""

from __future__ import annotations

import argparse
import sys
from pathlib import Path
from typing import Any

SCRIPT_DIR = Path(__file__).resolve().parent
DATASETS = ("warehouse_detection", "vehicle_detection")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Export trained YOLO models to ONNX.")
    parser.add_argument(
        "--datasets",
        nargs="+",
        choices=DATASETS,
        default=list(DATASETS),
        help="Datasets to export (default: both).",
    )
    parser.add_argument(
        "--weights-root",
        type=Path,
        default=SCRIPT_DIR / "runs" / "train",
        help="Directory containing yolo11n_<dataset>/weights/<checkpoint>.pt.",
    )
    parser.add_argument(
        "--checkpoint",
        choices=("best", "last"),
        default="best",
        help="Checkpoint to export (default: best).",
    )
    parser.add_argument("--imgsz", type=int, default=640, help="ONNX input image size.")
    parser.add_argument("--device", default="0", help="Export device, e.g. 0 or cpu.")
    parser.add_argument(
        "--dynamic",
        action="store_true",
        help="Export dynamic image dimensions and batch size (default: fixed 640x640 input).",
    )
    return parser.parse_args()


def export_models(args: argparse.Namespace, yolo_class: Any) -> int:
    failures = []
    for dataset in args.datasets:
        weights = (
            args.weights_root
            / f"yolo11n_{dataset}"
            / "weights"
            / f"{args.checkpoint}.pt"
        )
        if not weights.is_file():
            failures.append(f"{dataset}: checkpoint not found: {weights}")
            print(f"ERROR {failures[-1]}", file=sys.stderr)
            continue

        try:
            print(f"\n=== Exporting {dataset} ===")
            print(f"Checkpoint: {weights}")
            model = yolo_class(str(weights))
            onnx_path = model.export(
                format="onnx",
                imgsz=args.imgsz,
                device=args.device,
                dynamic=args.dynamic,
            )
            print(f"ONNX: {onnx_path}")
        except Exception as exc:
            message = f"{dataset}: {type(exc).__name__}: {exc}"
            failures.append(message)
            print(f"ERROR {message}", file=sys.stderr)

    if failures:
        print(f"\nExport finished with {len(failures)} failure(s).", file=sys.stderr)
        return 1
    print(f"\nSuccessfully exported {len(args.datasets)} ONNX model(s).")
    return 0


def main() -> int:
    args = parse_args()
    args.weights_root = args.weights_root.expanduser().resolve()

    try:
        from ultralytics import YOLO
    except ImportError as exc:
        raise SystemExit(
            "Ultralytics is required. Activate the Ultralytics conda environment first."
        ) from exc

    return export_models(args, YOLO)


if __name__ == "__main__":
    raise SystemExit(main())
