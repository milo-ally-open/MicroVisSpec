"""Build human-review contact sheets for reading/writing cross-class errors."""

from __future__ import annotations

import argparse
import csv
import json
import math
from pathlib import Path

from PIL import Image, ImageDraw, ImageFont


SCRIPT_DIR = Path(__file__).resolve().parent
INPUT_CSV = SCRIPT_DIR / "runs_grouped" / "validation" / "val_error_audit" / "reading_writing_review.csv"
DEFAULT_OUTPUT = SCRIPT_DIR / "dataset_analysis" / "classroom_behavior" / "reading_writing_review_sheets"
TILE_W, TILE_H = 320, 280
COLS, ROWS = 4, 4
GT_COLOR = (0, 205, 255)
PRED_COLOR = (255, 74, 74)


def parse_box(raw: str) -> tuple[float, float, float, float]:
    values = json.loads(raw)
    if len(values) != 4:
        raise ValueError(f"Expected 4 box coordinates, got {raw}")
    return tuple(float(value) for value in values)


def expand_box(box: tuple[float, float, float, float], width: int, height: int, scale: float = 1.45) -> tuple[int, int, int, int]:
    x1, y1, x2, y2 = box
    cx, cy = (x1 + x2) / 2, (y1 + y2) / 2
    half_w, half_h = (x2 - x1) * scale / 2, (y2 - y1) * scale / 2
    return (max(0, int(cx - half_w)), max(0, int(cy - half_h)),
            min(width, int(cx + half_w)), min(height, int(cy + half_h)))


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", type=Path, default=INPUT_CSV)
    parser.add_argument("--output", type=Path, default=DEFAULT_OUTPUT)
    args = parser.parse_args()
    if args.output.exists():
        raise FileExistsError(f"Refusing to overwrite review sheets: {args.output}")
    args.output.mkdir(parents=True)

    with args.input.open(encoding="utf-8-sig", newline="") as stream:
        rows = list(csv.DictReader(stream))
    rows.sort(key=lambda row: (row["ground_truth_class"], -float(row["prediction_score"]), -float(row["box_iou"]), row["image_id"]))

    font = ImageFont.load_default(size=15)
    title_font = ImageFont.load_default(size=18)
    records = []
    page_size = COLS * ROWS
    page_count = math.ceil(len(rows) / page_size)
    for page_index in range(page_count):
        page = rows[page_index * page_size:(page_index + 1) * page_size]
        sheet = Image.new("RGB", (COLS * TILE_W, ROWS * TILE_H), (32, 34, 38))
        draw = ImageDraw.Draw(sheet)
        for position, row in enumerate(page):
            image_path = Path(row["image_path"])
            with Image.open(image_path) as source:
                image = source.convert("RGB")
            gt_box = parse_box(row["ground_truth_box_xyxy"])
            pred_box = parse_box(row["prediction_box_xyxy"])
            crop_box = expand_box((min(gt_box[0], pred_box[0]), min(gt_box[1], pred_box[1]),
                                   max(gt_box[2], pred_box[2]), max(gt_box[3], pred_box[3])), *image.size, scale=1.55)
            crop = image.crop(crop_box)
            crop.thumbnail((TILE_W - 12, TILE_H - 62), Image.Resampling.LANCZOS)

            x = (position % COLS) * TILE_W
            y = (position // COLS) * TILE_H
            frame = Image.new("RGB", (TILE_W, TILE_H), (24, 25, 28))
            frame_draw = ImageDraw.Draw(frame)
            paste_x = (TILE_W - crop.width) // 2
            paste_y = 34 + (TILE_H - 62 - crop.height) // 2
            frame.paste(crop, (paste_x, paste_y))

            crop_x1, crop_y1, crop_x2, crop_y2 = crop_box
            scale_x, scale_y = crop.width / (crop_x2 - crop_x1), crop.height / (crop_y2 - crop_y1)
            for box, color in ((gt_box, GT_COLOR), (pred_box, PRED_COLOR)):
                bx1 = paste_x + int((box[0] - crop_x1) * scale_x)
                by1 = paste_y + int((box[1] - crop_y1) * scale_y)
                bx2 = paste_x + int((box[2] - crop_x1) * scale_x)
                by2 = paste_y + int((box[3] - crop_y1) * scale_y)
                frame_draw.rectangle((bx1, by1, bx2, by2), outline=color, width=3)

            title = f"{page_index * page_size + position + 1:03d}  GT {row['ground_truth_class']}  >  Pred {row['predicted_class']}"
            detail = f"conf={float(row['prediction_score']):.3f}  IoU={float(row['box_iou']):.3f}"
            frame_draw.text((6, 5), title, fill=(245, 245, 245), font=font)
            frame_draw.text((6, TILE_H - 24), detail, fill=(210, 210, 210), font=font)
            sheet.paste(frame, (x, y))
            records.append({"index": page_index * page_size + position + 1,
                            "page": page_index + 1, "image_id": row["image_id"],
                            "ground_truth_class": row["ground_truth_class"],
                            "predicted_class": row["predicted_class"],
                            "prediction_score": row["prediction_score"], "box_iou": row["box_iou"]})

        sheet_path = args.output / f"review_{page_index + 1:02d}.jpg"
        sheet.save(sheet_path, quality=92, optimize=True)
        print(f"Saved {sheet_path}")

    (args.output / "index.csv").write_text(
        "index,page,image_id,ground_truth_class,predicted_class,prediction_score,box_iou\n" +
        "\n".join(
            f"{item['index']},{item['page']},{item['image_id']},{item['ground_truth_class']},{item['predicted_class']},{item['prediction_score']},{item['box_iou']}"
            for item in records
        ) + "\n", encoding="utf-8-sig")
    (args.output / "README.md").write_text(
        "# Reading / writing cross-class review sheets\n\n"
        f"Contains {len(rows)} candidate errors across {page_count} image sheets. Each tile is a crop around the ground-truth and predicted boxes. "
        "Cyan box = annotation; red box = model prediction. These are review candidates only; no labels were changed. "
        "Use the index CSV to map a tile back to the full image path and fill review decisions in the source review CSV.\n",
        encoding="utf-8")
    print(f"Built {page_count} sheets for {len(rows)} candidates in {args.output}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
