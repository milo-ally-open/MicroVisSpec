"""Evaluate trained YOLO models on a held-out dataset split.

By default, evaluate the best vehicle checkpoint on its
held-out test split. Ultralytics writes curves, confusion matrices, and
prediction examples into a uniquely named run directory; this script also
writes per-dataset and combined JSON summaries there.

Run from any directory, for example::

    python 4-validate-model.py --device 0
    python 4-validate-model.py --datasets warehouse_detection --split val
"""

from __future__ import annotations

import argparse
import csv
import hashlib
import json
import sys
from datetime import datetime
from pathlib import Path
from typing import Any

import yaml
from ultralytics import YOLO

SCRIPT_DIR = Path(__file__).resolve().parent
DATASETS = ("vehicle_detection",)
REPORT_COLUMNS = (
    "Dataset",
    "Split",
    "Checkpoint",
    "Class",
    "Images",
    "SourceImages",
    "Instances",
    "Precision",
    "Recall",
    "F1",
    "mAP50",
    "mAP50-95",
    "Inference_ms_per_image",
    "ImageSize",
    "Batch",
    "Device",
    "ConfidenceThreshold",
    "IoUThreshold",
    "CheckpointSHA256",
)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Evaluate trained YOLO detection models and save full reports."
    )
    parser.add_argument(
        "--datasets",
        nargs="+",
        choices=DATASETS,
        default=list(DATASETS),
        help="Datasets to evaluate (default: both).",
    )
    parser.add_argument(
        "--data-root",
        type=Path,
        default=SCRIPT_DIR / "datasets",
        help="Directory containing <dataset-name>/data.yaml.",
    )
    parser.add_argument(
        "--weights-root",
        type=Path,
        default=SCRIPT_DIR / "train_result",
        help="Directory containing yolo11n_<dataset>_<checkpoint>.pt or a training run.",
    )
    parser.add_argument(
        "--checkpoint",
        choices=("best", "last"),
        default="best",
        help="Checkpoint to evaluate (default: best).",
    )
    parser.add_argument(
        "--split",
        choices=("test", "val"),
        default="test",
        help="Dataset split to evaluate; test is the held-out default.",
    )
    parser.add_argument(
        "--project",
        type=Path,
        default=SCRIPT_DIR / "runs" / "validation",
        help="Parent directory for evaluation artifacts.",
    )
    parser.add_argument("--imgsz", type=int, default=640, help="Inference image size.")
    parser.add_argument("--batch", type=int, default=16, help="Validation batch size.")
    parser.add_argument("--device", default="0", help="Inference device, e.g. 0 or cpu.")
    parser.add_argument("--workers", type=int, default=8, help="Dataloader workers.")
    parser.add_argument("--conf", type=float, help="Optional confidence threshold.")
    parser.add_argument("--iou", type=float, default=0.7, help="NMS IoU threshold.")
    parser.add_argument("--max-det", type=int, default=300, help="Maximum detections per image.")
    parser.add_argument(
        "--save-json",
        action=argparse.BooleanOptionalAction,
        default=True,
        help="Save prediction records in JSON format (default: enabled).",
    )
    parser.add_argument(
        "--plots",
        action=argparse.BooleanOptionalAction,
        default=True,
        help="Save metric curves, confusion matrices, and prediction examples.",
    )
    return parser.parse_args()


def resolve_split(data_yaml: Path, split: str) -> tuple[Path, int | None]:
    """Resolve a dataset split and verify that it contains images."""
    data = yaml.safe_load(data_yaml.read_text(encoding="utf-8")) or {}
    split_value = data.get(split)
    if not split_value:
        raise ValueError(f"{data_yaml} does not define the requested '{split}' split.")

    root_value = data.get("path")
    root = Path(root_value).expanduser() if root_value else data_yaml.parent
    if not root.is_absolute():
        root = (data_yaml.parent / root).resolve()
    else:
        root = root.resolve()

    # Ultralytics supports a directory, a text list, or a list of sources.
    sources = split_value if isinstance(split_value, list) else [split_value]
    resolved_sources: list[Path] = []
    image_count = 0
    image_suffixes = {".bmp", ".jpg", ".jpeg", ".png", ".tif", ".tiff", ".webp"}
    for source in sources:
        path = Path(str(source)).expanduser()
        if not path.is_absolute():
            original_source = str(path)
            path = (root / path).resolve()
            # Roboflow YAML commonly uses ../train, ../valid, and ../test
            # while keeping those folders inside the same dataset directory.
            if not path.exists() and original_source.startswith("../"):
                path = (root / original_source[3:]).resolve()
        else:
            path = path.resolve()
        if not path.exists():
            raise FileNotFoundError(f"Dataset split path does not exist: {path}")
        resolved_sources.append(path)
        if path.is_dir():
            image_count += sum(1 for item in path.rglob("*") if item.is_file() and item.suffix.lower() in image_suffixes)
        elif path.is_file() and path.suffix.lower() in image_suffixes:
            image_count += 1
        elif path.is_file():
            entries = [line.strip() for line in path.read_text(encoding="utf-8").splitlines() if line.strip()]
            for entry in entries:
                image_path = Path(entry).expanduser()
                if not image_path.is_absolute():
                    image_path = path.parent / image_path
                image_count += int(image_path.is_file() and image_path.suffix.lower() in image_suffixes)

    if image_count == 0:
        raise ValueError(f"No images found in {split} split for {data_yaml}.")
    return resolved_sources[0], image_count


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def json_safe(value: Any) -> Any:
    """Convert NumPy scalars and other simple values into JSON-safe objects."""
    if hasattr(value, "item"):
        return value.item()
    if isinstance(value, dict):
        return {str(key): json_safe(item) for key, item in value.items()}
    if isinstance(value, (list, tuple)):
        return [json_safe(item) for item in value]
    if isinstance(value, (str, int, float, bool)) or value is None:
        return value
    return str(value)


def report_rows(report: dict[str, Any]) -> list[dict[str, Any]]:
    """Create one overall row and one row per class for CSV and terminal output."""
    overall = report["overall_metrics"]
    precision = overall.get("metrics/precision(B)")
    recall = overall.get("metrics/recall(B)")
    f1 = (2 * precision * recall / (precision + recall)) if precision is not None and recall is not None and precision + recall else None
    per_class = report["per_class_metrics"]
    valid_images = report.get("evaluated_images", report["split_images"])
    shared = {
        "Dataset": report["dataset"],
        "Split": report["split"],
        "Checkpoint": report["checkpoint"],
        "SourceImages": report["split_images"],
        "Inference_ms_per_image": report["speed_ms_per_image"].get("inference"),
        "ImageSize": report["imgsz"],
        "Batch": report["batch"],
        "Device": report["device"],
        "ConfidenceThreshold": report["confidence_threshold"],
        "IoUThreshold": report["iou_threshold"],
        "CheckpointSHA256": report["checkpoint_sha256"],
    }
    rows = [
        {
            **shared,
            "Class": "all",
            "Images": valid_images,
            "Instances": sum(int(row["Instances"]) for row in per_class),
            "Precision": precision,
            "Recall": recall,
            "F1": f1,
            "mAP50": overall.get("metrics/mAP50(B)"),
            "mAP50-95": overall.get("metrics/mAP50-95(B)"),
        }
    ]
    for class_row in per_class:
        rows.append(
            {
                **shared,
                "Class": class_row["Class"],
                "Images": class_row["Images"],
                "Instances": class_row["Instances"],
                "Precision": class_row["Box-P"],
                "Recall": class_row["Box-R"],
                "F1": class_row["Box-F1"],
                "mAP50": class_row["mAP50"],
                "mAP50-95": class_row["mAP50-95"],
            }
        )
    return rows


def write_report_csv(path: Path, rows: list[dict[str, Any]]) -> None:
    """Write a spreadsheet-friendly UTF-8 CSV report."""
    with path.open("w", encoding="utf-8-sig", newline="") as stream:
        writer = csv.DictWriter(stream, fieldnames=REPORT_COLUMNS, extrasaction="ignore")
        writer.writeheader()
        for row in rows:
            writer.writerow(
                {
                    key: f"{value:.6f}" if isinstance(value, float) else value
                    for key, value in row.items()
                }
            )


def print_report_table(rows: list[dict[str, Any]]) -> None:
    """Print all overall and per-class report rows in a readable terminal table."""
    columns = ("Dataset", "Class", "Images", "Instances", "Precision", "Recall", "F1", "mAP50", "mAP50-95")
    display_rows = []
    for row in rows:
        display_rows.append(
            [
                str(row.get("Dataset", "")),
                str(row.get("Class", "")),
                str(row.get("Images", "")),
                str(row.get("Instances", "")),
                *(f"{row[key]:.6f}" if isinstance(row.get(key), (int, float)) else "" for key in columns[4:]),
            ]
        )
    widths = [max(len(column), *(len(row[index]) for row in display_rows)) for index, column in enumerate(columns)]
    header = " | ".join(column.ljust(widths[index]) for index, column in enumerate(columns))
    divider = "-+-".join("-" * width for width in widths)
    print("\nComplete evaluation report (overall and per class):")
    print(header)
    print(divider)
    for row in display_rows:
        print(" | ".join(value.ljust(widths[index]) for index, value in enumerate(row)))


def evaluate_one(
    *,
    dataset_name: str,
    data_yaml: Path,
    weights: Path,
    split: str,
    split_images: int,
    output_dir: Path,
    args: argparse.Namespace,
    yolo_class: Any,
) -> dict[str, Any]:
    model = yolo_class(str(weights))
    metrics = model.val(
        data=str(data_yaml),
        split=split,
        imgsz=args.imgsz,
        batch=args.batch,
        device=args.device,
        workers=args.workers,
        conf=args.conf,
        iou=args.iou,
        max_det=args.max_det,
        plots=args.plots,
        save_json=args.save_json,
        project=str(output_dir.parent),
        name=output_dir.name,
        exist_ok=False,
        verbose=True,
    )

    # Ultralytics creates the requested directory, or increments its name if a
    # previous run already occupies it.
    actual_output_dir = output_dir
    if not actual_output_dir.exists():
        candidates = sorted(
            output_dir.parent.glob(f"{output_dir.name}*"),
            key=lambda item: item.stat().st_mtime_ns,
        )
        if candidates:
            actual_output_dir = candidates[-1]

    metric_keys = (
        "metrics/precision(B)",
        "metrics/recall(B)",
        "metrics/mAP50(B)",
        "metrics/mAP50-95(B)",
    )
    overall = {key: float(metrics.results_dict[key]) for key in metric_keys if key in metrics.results_dict}
    per_class = json_safe(metrics.summary(normalize=True, decimals=6))
    evaluated_images = len(getattr(metrics.box, "image_metrics", {})) or split_images
    result = {
        "dataset": dataset_name,
        "split": split,
        "split_images": split_images,
        "evaluated_images": evaluated_images,
        "data_yaml": str(data_yaml),
        "checkpoint": str(weights),
        "checkpoint_sha256": sha256_file(weights),
        "imgsz": args.imgsz,
        "batch": args.batch,
        "device": args.device,
        "confidence_threshold": args.conf,
        "iou_threshold": args.iou,
        "overall_metrics": overall,
        "per_class_metrics": per_class,
        "speed_ms_per_image": json_safe(metrics.speed),
        "artifacts_dir": str(actual_output_dir),
    }
    (actual_output_dir / "evaluation.json").write_text(
        json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
    )
    write_report_csv(actual_output_dir / "report.csv", report_rows(result))
    return result


def main() -> int:
    args = parse_args()
    args.data_root = args.data_root.expanduser().resolve()
    args.weights_root = args.weights_root.expanduser().resolve()
    args.project = args.project.expanduser().resolve()

    # Resolve all inputs before starting evaluation so a missing second model
    # cannot leave the requested two-model evaluation half-done.
    inputs = []
    for dataset_name in args.datasets:
        data_yaml = args.data_root / dataset_name / "data.yaml"
        weights = args.weights_root / f"yolo11n_{dataset_name}_{args.checkpoint}.pt"
        if not weights.is_file():
            weights = args.weights_root / f"yolo11n_{dataset_name}" / "weights" / f"{args.checkpoint}.pt"
        if not data_yaml.is_file():
            raise FileNotFoundError(f"Dataset YAML not found: {data_yaml}")
        if not weights.is_file():
            raise FileNotFoundError(f"Model checkpoint not found: {weights}")
        split_path, split_images = resolve_split(data_yaml, args.split)
        print(f"Preflight {dataset_name}: {args.split} images={split_images} ({split_path})")
        inputs.append((dataset_name, data_yaml, weights, split_images))

    args.project.mkdir(parents=True, exist_ok=True)
    timestamp = datetime.now().strftime("%Y%m%d_%H%M%S")
    reports = []
    failures = []
    for dataset_name, data_yaml, weights, split_images in inputs:
        run_name = f"{dataset_name}_{args.split}_{timestamp}"
        output_dir = args.project / run_name
        try:
            print(f"\n=== Evaluating {dataset_name} on {args.split} ===")
            print(f"Checkpoint: {weights}")
            reports.append(
                evaluate_one(
                    dataset_name=dataset_name,
                    data_yaml=data_yaml,
                    weights=weights,
                    split=args.split,
                    split_images=split_images,
                    output_dir=output_dir,
                    args=args,
                    yolo_class=YOLO,
                )
            )
        except Exception as exc:
            failures.append({"dataset": dataset_name, "error": f"{type(exc).__name__}: {exc}"})
            print(f"Evaluation failed for {dataset_name}: {type(exc).__name__}: {exc}", file=sys.stderr)

    summary = {
        "created_at": datetime.now().astimezone().isoformat(timespec="seconds"),
        "split": args.split,
        "checkpoint": args.checkpoint,
        "reports": reports,
        "failures": failures,
    }
    summary_path = args.project / f"evaluation_summary_{timestamp}.json"
    summary_path.write_text(json.dumps(summary, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    all_rows = [row for report in reports for row in report_rows(report)]
    csv_path = args.project / f"evaluation_report_{timestamp}.csv"
    write_report_csv(csv_path, all_rows)
    print(f"\nSummary: {summary_path}")
    print(f"CSV report: {csv_path}")
    for report in reports:
        print(f"Dataset CSV: {report['artifacts_dir']}/report.csv")
    for report in reports:
        metrics = report["overall_metrics"]
        print(
            f"{report['dataset']}: P={metrics.get('metrics/precision(B)', float('nan')):.4f} "
            f"R={metrics.get('metrics/recall(B)', float('nan')):.4f} "
            f"mAP50={metrics.get('metrics/mAP50(B)', float('nan')):.4f} "
            f"mAP50-95={metrics.get('metrics/mAP50-95(B)', float('nan')):.4f} "
            f"artifacts={report['artifacts_dir']}"
        )
    if all_rows:
        print_report_table(all_rows)
    return 1 if failures else 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (FileNotFoundError, ValueError, RuntimeError) as exc:
        raise SystemExit(f"Error: {exc}") from exc
