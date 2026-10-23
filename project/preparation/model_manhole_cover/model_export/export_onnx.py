"""Export the project checkpoints to fixed-shape ONNX and write provenance."""

from __future__ import annotations

import argparse
import hashlib
import json
import shutil
import sys
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from ultralytics import YOLO

SCRIPT_DIR = Path(__file__).resolve().parent
DATASETS = ("manhole_cover",)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Export trained YOLO models to ONNX.")
    parser.add_argument(
        "--model-name",
        default="yolo11n",
        help="Model name used in checkpoint and output filenames (default: yolo11n).",
    )
    parser.add_argument(
        "--datasets",
        nargs="+",
        choices=DATASETS,
        default=list(DATASETS),
        help="Dataset to export.",
    )
    parser.add_argument(
        "--weights-root",
        type=Path,
        default=SCRIPT_DIR.parent / "model_train" / "train_result",
        help="Directory containing <model-name>_<dataset>_<checkpoint>.pt.",
    )
    parser.add_argument(
        "--checkpoint",
        choices=("best", "last"),
        default="best",
        help="Checkpoint to export (default: best).",
    )
    parser.add_argument("--imgsz", type=int, default=640, help="ONNX input image size.")
    parser.add_argument("--device", default="cpu", help="Export device, e.g. 0 or cpu.")
    parser.add_argument("--opset", type=int, default=17, help="ONNX opset version.")
    parser.add_argument(
        "--output-dir",
        type=Path,
        default=SCRIPT_DIR / "artifacts",
        help="Directory for exported ONNX models and provenance manifest.",
    )
    parser.add_argument(
        "--dynamic",
        action="store_true",
        help="Export dynamic image dimensions and batch size (default: fixed 640x640 input).",
    )
    return parser.parse_args()


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def export_models(args: argparse.Namespace, yolo_class: Any) -> int:
    failures = []
    manifest = {
        "created_utc": datetime.now(timezone.utc).isoformat(),
        "ultralytics_version": getattr(sys.modules.get("ultralytics"), "__version__", "unknown"),
        "model_name": args.model_name,
        "imgsz": args.imgsz,
        "opset": args.opset,
        "dynamic": args.dynamic,
        "device": args.device,
        "models": [],
    }
    args.output_dir.mkdir(parents=True, exist_ok=True)
    for dataset in args.datasets:
        weights = args.weights_root / f"{args.model_name}_{dataset}_{args.checkpoint}.pt"
        if not weights.is_file():
            weights = (
                args.weights_root
                / f"{args.model_name}_{dataset}"
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
                opset=args.opset,
                simplify=False,
            )
            onnx_path = Path(onnx_path).resolve()
            output_path = args.output_dir / f"{args.model_name}_{dataset}_{args.checkpoint}.onnx"
            if onnx_path != output_path.resolve():
                shutil.copy2(onnx_path, output_path)
            manifest["models"].append(
                {
                    "dataset": dataset,
                    "checkpoint": str(weights),
                    "checkpoint_sha256": sha256_file(weights),
                    "onnx": str(output_path.resolve()),
                    "onnx_sha256": sha256_file(output_path),
                }
            )
            print(f"ONNX: {output_path}")
        except Exception as exc:
            message = f"{dataset}: {type(exc).__name__}: {exc}"
            failures.append(message)
            print(f"ERROR {message}", file=sys.stderr)

    if failures:
        print(f"\nExport finished with {len(failures)} failure(s).", file=sys.stderr)
        return 1
    manifest_name = "export_manifest.json" if args.model_name == "yolo11n" else f"{args.model_name}_export_manifest.json"
    manifest_path = args.output_dir / manifest_name
    manifest_path.write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    print(f"\nSuccessfully exported {len(args.datasets)} ONNX model(s).")
    print(f"Manifest: {manifest_path}")
    return 0


def main() -> int:
    args = parse_args()
    args.weights_root = args.weights_root.expanduser().resolve()
    return export_models(args, YOLO)


if __name__ == "__main__":
    raise SystemExit(main())
