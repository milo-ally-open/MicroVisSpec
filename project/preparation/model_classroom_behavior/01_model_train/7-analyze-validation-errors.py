"""Summarize per-class detection errors and render review sheets.

This tool consumes an existing Ultralytics COCO-style predictions.json and a
split text list. Select the split explicitly; do not treat in-sample train
diagnostics as validation or test performance.
"""

from __future__ import annotations

import argparse
import csv
import json
from collections import Counter, defaultdict
from pathlib import Path

from PIL import Image, ImageDraw, ImageFont


CLASS_NAMES = ("hand-raising", "reading", "writing")
COLORS = {"gt": (45, 190, 255), "same": (58, 220, 95), "wrong": (255, 170, 0), "fp": (255, 60, 60)}


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--predictions", type=Path, required=True)
    parser.add_argument("--split-list", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--conf", type=float, default=0.25)
    parser.add_argument("--iou", type=float, default=0.5)
    parser.add_argument("--examples-per-class", type=int, default=16)
    parser.add_argument(
        "--split-name",
        default="validation",
        help="Label for the split being audited (default: validation).",
    )
    return parser.parse_args()


def iou(a: tuple[float, float, float, float], b: tuple[float, float, float, float]) -> float:
    x1, y1 = max(a[0], b[0]), max(a[1], b[1])
    x2, y2 = min(a[2], b[2]), min(a[3], b[3])
    intersection = max(0.0, x2 - x1) * max(0.0, y2 - y1)
    area_a = max(0.0, a[2] - a[0]) * max(0.0, a[3] - a[1])
    area_b = max(0.0, b[2] - b[0]) * max(0.0, b[3] - b[1])
    union = area_a + area_b - intersection
    return intersection / union if union > 0 else 0.0


def load_ground_truth(image_path: Path) -> list[dict[str, object]]:
    label_path = image_path.parent.parent / "labels" / f"{image_path.stem}.txt"
    if not label_path.is_file():
        raise FileNotFoundError(f"Missing label file: {label_path}")
    with Image.open(image_path) as image:
        width, height = image.size
    boxes = []
    for line_number, line in enumerate(label_path.read_text(encoding="utf-8").splitlines(), 1):
        parts = line.split()
        if not parts:
            continue
        class_id = int(parts[0])
        coords = [float(value) for value in parts[1:]]
        if len(coords) == 4:  # normalized xc, yc, w, h
            xc, yc, bw, bh = coords
            box = ((xc - bw / 2) * width, (yc - bh / 2) * height,
                   (xc + bw / 2) * width, (yc + bh / 2) * height)
        elif len(coords) >= 6 and len(coords) % 2 == 0:  # normalized polygon
            xs, ys = coords[::2], coords[1::2]
            box = (min(xs) * width, min(ys) * height,
                   max(xs) * width, max(ys) * height)
        else:
            raise ValueError(f"Unsupported label at {label_path}:{line_number}")
        boxes.append({"class_id": class_id, "box": tuple(float(x) for x in box)})
    return boxes


def write_csv(path: Path, rows: list[dict[str, object]]) -> None:
    if not rows:
        path.write_text("No errors at the selected thresholds.\n", encoding="utf-8")
        return
    with path.open("w", newline="", encoding="utf-8-sig") as stream:
        writer = csv.DictWriter(stream, fieldnames=list(rows[0]))
        writer.writeheader()
        writer.writerows(rows)


def render_sheet(path: Path, records: list[dict[str, object]], image_paths: dict[str, Path],
                 mode: str, conf_threshold: float) -> None:
    if not records:
        return
    columns, tile_w, tile_h, rows = 4, 400, 300, (len(records) + 3) // 4
    sheet = Image.new("RGB", (columns * tile_w, rows * tile_h), "#202124")
    draw_sheet = ImageDraw.Draw(sheet)
    try:
        font = ImageFont.truetype("arial.ttf", 15)
        small_font = ImageFont.truetype("arial.ttf", 13)
    except OSError:
        font = small_font = ImageFont.load_default()

    for index, record in enumerate(records):
        image_path = image_paths[str(record["image_id"])]
        image = Image.open(image_path).convert("RGB")
        orig_w, orig_h = image.size
        image.thumbnail((tile_w, tile_h - 36))
        x0, y0 = (index % columns) * tile_w, (index // columns) * tile_h
        scale_x, scale_y = image.width / orig_w, image.height / orig_h
        sheet.paste(image, (x0, y0 + 36))
        d = ImageDraw.Draw(sheet)

        def rect(box, color, label):
            x1, y1, x2, y2 = box
            scaled = (x0 + x1 * scale_x, y0 + 36 + y1 * scale_y,
                      x0 + x2 * scale_x, y0 + 36 + y2 * scale_y)
            d.rectangle(scaled, outline=color, width=2)
            d.text((scaled[0] + 2, max(y0 + 36, scaled[1] - 16)), label, fill=color, font=small_font,
                   stroke_width=1, stroke_fill="black")

        if mode == "FN":
            rect(record["gt_box"], COLORS["gt"], f"GT {CLASS_NAMES[int(record['class_id'])]}")
            for pred in record.get("related_predictions", []):
                correct = int(pred["class_id"]) == int(record["class_id"])
                rect(pred["box"], COLORS["same"] if correct else COLORS["wrong"],
                     f"{CLASS_NAMES[int(pred['class_id'])]} {float(pred['score']):.2f}")
        else:
            rect(record["pred_box"], COLORS["fp"],
                 f"FP {CLASS_NAMES[int(record['class_id'])]} {float(record['score']):.2f}")
            for gt in record.get("related_ground_truth", []):
                rect(gt["box"], COLORS["gt"], f"GT {CLASS_NAMES[int(gt['class_id'])]}")
        caption = f"{image_path.name[:42]} | {record.get('kind', mode)}"
        d.rectangle((x0, y0, x0 + tile_w, y0 + 34), fill="#111111")
        d.text((x0 + 5, y0 + 8), caption, fill="white", font=font)
    sheet.save(path, quality=92)


def main() -> None:
    args = parse_args()
    prediction_data = json.loads(args.predictions.read_text(encoding="utf-8"))
    image_paths: dict[str, Path] = {}
    for line in args.split_list.read_text(encoding="utf-8-sig").splitlines():
        if line.strip():
            path = Path(line.strip()).resolve()
            image_paths[path.name] = path
    if not image_paths:
        raise ValueError(f"No images listed in {args.split_list}")

    gt_by_image = {name: load_ground_truth(path) for name, path in image_paths.items()}
    pred_by_image: dict[str, list[dict[str, object]]] = defaultdict(list)
    for raw in prediction_data:
        image_id = str(raw["file_name"])
        if image_id not in image_paths:
            continue
        score = float(raw["score"])
        if score < args.conf:
            continue
        x, y, width, height = (float(v) for v in raw["bbox"])
        pred_by_image[image_id].append({
            "class_id": int(raw["category_id"]) - 1,
            "score": score,
            "box": (x, y, x + width, y + height),
        })

    fn_rows: list[dict[str, object]] = []
    fp_rows: list[dict[str, object]] = []
    fn_records: dict[int, list[dict[str, object]]] = defaultdict(list)
    fp_records: dict[int, list[dict[str, object]]] = defaultdict(list)
    counts = {c: Counter() for c in range(len(CLASS_NAMES))}
    cross_class_confusions = Counter()

    for image_id, ground_truth in gt_by_image.items():
        predictions = pred_by_image.get(image_id, [])
        gt_matched: set[int] = set()
        pred_matched: set[int] = set()
        candidates = []
        for pi, pred in enumerate(predictions):
            for gi, gt in enumerate(ground_truth):
                overlap = iou(pred["box"], gt["box"])
                if overlap >= args.iou:
                    candidates.append((float(pred["score"]), overlap, pi, gi))
        for _, _, pi, gi in sorted(candidates, reverse=True):
            if pi in pred_matched or gi in gt_matched:
                continue
            pred, gt = predictions[pi], ground_truth[gi]
            if pred["class_id"] == gt["class_id"]:
                pred_matched.add(pi)
                gt_matched.add(gi)
                counts[int(gt["class_id"])]["TP"] += 1
        # Wrong-class overlaps are separately counted as confusions; they do not
        # count as true positives and remain a false positive plus false negative.
        for pi, pred in enumerate(predictions):
            if pi in pred_matched:
                continue
            pcls = int(pred["class_id"])
            counts[pcls]["FP"] += 1
            overlaps = sorted(((iou(pred["box"], gt["box"]), gi, gt)
                               for gi, gt in enumerate(ground_truth)), reverse=True)
            best_iou, best_gt_index, best_gt = overlaps[0] if overlaps else (0.0, -1, None)
            related_gts = [{"class_id": int(gt["class_id"]), "box": gt["box"]}
                           for gt in ground_truth if iou(pred["box"], gt["box"]) >= args.iou]
            if best_iou < args.iou:
                kind = "background_or_localization"
            elif int(best_gt["class_id"]) != pcls:
                kind = "wrong_class"
                cross_class_confusions[(int(best_gt["class_id"]), pcls)] += 1
            else:
                kind = "duplicate_same_class"
            row = {
                "image_id": image_id, "image_path": str(image_paths[image_id]),
                "class_id": pcls, "class": CLASS_NAMES[pcls], "score": round(float(pred["score"]), 4),
                "kind": kind, "best_gt_class": CLASS_NAMES[int(best_gt["class_id"])] if best_gt else "",
                "best_iou": round(float(best_iou), 4), "pred_box_xyxy": [round(x, 1) for x in pred["box"]],
                "best_gt_box_xyxy": [round(x, 1) for x in best_gt["box"]] if best_gt else [],
            }
            fp_rows.append(row)
            fp_records[pcls].append({**row, "pred_box": pred["box"], "related_ground_truth": related_gts})

        for gi, gt in enumerate(ground_truth):
            if gi in gt_matched:
                continue
            gcls = int(gt["class_id"])
            counts[gcls]["FN"] += 1
            related_preds = [{**pred, "iou": iou(pred["box"], gt["box"])}
                             for pred in predictions if iou(pred["box"], gt["box"]) >= 0.1]
            related_preds.sort(key=lambda p: (p["iou"], p["score"]), reverse=True)
            wrong = [p for p in related_preds if p["class_id"] != gcls and p["iou"] >= args.iou]
            best_same = max((p for p in related_preds if p["class_id"] == gcls),
                            key=lambda p: p["score"], default=None)
            kind = "wrong_class" if wrong else ("low_conf_or_iou" if related_preds else "missed")
            row = {
                "image_id": image_id, "image_path": str(image_paths[image_id]),
                "class_id": gcls, "class": CLASS_NAMES[gcls], "kind": kind,
                "best_same_class_score": round(float(best_same["score"]), 4) if best_same else "",
                "best_same_class_iou": round(float(best_same["iou"]), 4) if best_same else "",
                "wrong_class_candidates": ";".join(
                    f"{CLASS_NAMES[int(p['class_id'])]}:{p['score']:.2f}/IoU{p['iou']:.2f}" for p in wrong
                ),
                "gt_box_xyxy": [round(x, 1) for x in gt["box"]],
            }
            fn_rows.append(row)
            fn_records[gcls].append({**row, "gt_box": gt["box"], "related_predictions": related_preds})

    args.output.mkdir(parents=True, exist_ok=True)
    write_csv(args.output / "false_negatives.csv", fn_rows)
    write_csv(args.output / "false_positives.csv", fp_rows)
    review_rows = [
        {
            "image_path": row["image_path"],
            "image_id": row["image_id"],
            "ground_truth_class": row["best_gt_class"],
            "predicted_class": row["class"],
            "prediction_score": row["score"],
            "box_iou": row["best_iou"],
            "ground_truth_box_xyxy": row["best_gt_box_xyxy"],
            "prediction_box_xyxy": row["pred_box_xyxy"],
            "review_decision": "",
            "confirmed_class": "",
            "review_notes": "",
        }
        for row in fp_rows
        if row["kind"] == "wrong_class"
        and row["class"] in {"reading", "writing"}
        and row["best_gt_class"] in {"reading", "writing"}
        and row["class"] != row["best_gt_class"]
    ]
    review_rows.sort(key=lambda row: (-float(row["prediction_score"]), -float(row["box_iou"])))
    write_csv(args.output / "reading_writing_review.csv", review_rows)
    summary = {
        "split": args.split_name,
        "predictions": str(args.predictions.resolve()),
        "split_list": str(args.split_list.resolve()),
        "images": len(gt_by_image),
        "confidence_threshold": args.conf,
        "iou_threshold": args.iou,
        "matching_note": "Greedy class-aware matching at the specified IoU; FP/FN counts are diagnostic at the selected confidence, not mAP.",
        "per_class": {},
        "cross_class_confusions_gt_to_pred": [
            {"ground_truth": CLASS_NAMES[gt], "predicted": CLASS_NAMES[pred], "count": n}
            for (gt, pred), n in cross_class_confusions.most_common()
        ],
        "totals": {"false_positives": len(fp_rows), "false_negatives": len(fn_rows)},
    }
    for class_id, name in enumerate(CLASS_NAMES):
        c = counts[class_id]
        precision = c["TP"] / (c["TP"] + c["FP"]) if c["TP"] + c["FP"] else 0.0
        recall = c["TP"] / (c["TP"] + c["FN"]) if c["TP"] + c["FN"] else 0.0
        summary["per_class"][name] = {
            "TP": c["TP"], "FP": c["FP"], "FN": c["FN"],
            "precision_at_thresholds": round(precision, 4), "recall_at_thresholds": round(recall, 4),
            "wrong_class_FP": sum(1 for row in fp_rows if row["class_id"] == class_id and row["kind"] == "wrong_class"),
            "missed_due_to_wrong_class": sum(1 for row in fn_rows if row["class_id"] == class_id and row["kind"] == "wrong_class"),
        }
        # Review priority: wrong-class and wholly missed GT first, then lowest
        # same-class confidence / IoU so visually hard cases rise to the top.
        fn_records[class_id].sort(key=lambda r: (
            r["kind"] != "wrong_class", r["kind"] != "missed",
            float(r["best_same_class_score"]) if r["best_same_class_score"] != "" else -1,
        ))
        fp_records[class_id].sort(key=lambda r: (r["kind"] != "wrong_class", -float(r["score"])))
        render_sheet(args.output / f"{name}_false_negatives.jpg",
                     fn_records[class_id][:args.examples_per_class], image_paths, "FN", args.conf)
        render_sheet(args.output / f"{name}_false_positives.jpg",
                     fp_records[class_id][:args.examples_per_class], image_paths, "FP", args.conf)

    (args.output / "summary.json").write_text(json.dumps(summary, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(summary, ensure_ascii=False, indent=2))
    print(f"Audit artifacts: {args.output.resolve()}")


if __name__ == "__main__":
    main()
