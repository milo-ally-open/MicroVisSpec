from __future__ import annotations

import argparse
import csv
import json
from pathlib import Path


def main() -> None:
    parser = argparse.ArgumentParser(description="Compare ONNX and Atlas OM accuracy reports")
    parser.add_argument("--onnx-report", type=Path, required=True)
    parser.add_argument("--om-report", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--max-map-drop", type=float, default=0.02)
    parser.add_argument("--max-class-ap50-drop", type=float, default=0.05)
    args = parser.parse_args()
    onnx = json.loads(args.onnx_report.read_text(encoding="utf-8"))
    om = json.loads(args.om_report.read_text(encoding="utf-8"))
    for key in ("dataset_yaml_sha256", "dataset_manifest_sha256", "split", "images", "image_size", "confidence_threshold", "nms_iou_threshold"):
        if onnx.get(key) != om.get(key):
            raise SystemExit(f"Cannot compare reports: {key} differs ({onnx.get(key)!r} != {om.get(key)!r})")
    onnx_classes, om_classes = onnx["metrics"]["per_class"], om["metrics"]["per_class"]
    if onnx_classes.keys() != om_classes.keys():
        raise SystemExit("Cannot compare reports: class IDs differ")
    if any(onnx_classes[key]["name"] != om_classes[key]["name"] for key in onnx_classes):
        raise SystemExit("Cannot compare reports: class names differ")

    rows, failures = [], []
    for class_id, baseline in onnx_classes.items():
        actual = om_classes[class_id]
        row = {"class_id": class_id, "class_name": baseline["name"], "ground_truth": baseline["ground_truth"]}
        for metric in ("precision", "recall", "f1", "ap50", "map50_95"):
            row[f"onnx_{metric}"] = baseline[metric]
            row[f"om_{metric}"] = actual[metric]
            row[f"delta_{metric}"] = None if baseline[metric] is None or actual[metric] is None else actual[metric] - baseline[metric]
        if baseline["ground_truth"] and baseline["map50_95"] is not None and actual["map50_95"] is not None and actual["map50_95"] < baseline["map50_95"] - args.max_map_drop:
            failures.append(f"class {class_id} ({baseline['name']}) mAP50-95 drop exceeds {args.max_map_drop:.3f}")
        if baseline["ground_truth"] and baseline["ap50"] is not None and actual["ap50"] is not None and actual["ap50"] < baseline["ap50"] - args.max_class_ap50_drop:
            failures.append(f"class {class_id} ({baseline['name']}) AP50 drop exceeds {args.max_class_ap50_drop:.3f}")
        rows.append(row)
    onnx_map, om_map = onnx["metrics"]["map50_95"], om["metrics"]["map50_95"]
    if onnx_map is None or om_map is None:
        failures.append("overall mAP50-95 is unavailable")
    elif om_map < onnx_map - args.max_map_drop:
        failures.append(f"overall mAP50-95 drop exceeds {args.max_map_drop:.3f}")

    report = {
        "onnx_model_sha256": onnx["model_sha256"],
        "om_model_sha256": om["model_sha256"],
        "dataset_yaml_sha256": onnx["dataset_yaml_sha256"],
        "dataset_manifest_sha256": onnx["dataset_manifest_sha256"],
        "split": onnx["split"],
        "images": onnx["images"],
        "onnx_overall": onnx["metrics"],
        "om_overall": om["metrics"],
        "acceptance": {
            "max_map_drop": args.max_map_drop,
            "max_class_ap50_drop": args.max_class_ap50_drop,
            "verdict": "fail" if failures else "pass",
            "failures": failures,
        },
        "per_class": rows,
    }
    args.output.parent.mkdir(parents=True, exist_ok=True)
    json_path = args.output.with_suffix(".json")
    csv_path = args.output.with_suffix(".csv")
    json_path.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    with csv_path.open("w", newline="", encoding="utf-8-sig") as stream:
        writer = csv.DictWriter(stream, fieldnames=list(rows[0]) if rows else ["class_id", "class_name", "ground_truth"])
        writer.writeheader()
        writer.writerows(rows)
    print(f"Wrote {json_path} and {csv_path}; verdict={report['acceptance']['verdict']}")


if __name__ == "__main__":
    main()
