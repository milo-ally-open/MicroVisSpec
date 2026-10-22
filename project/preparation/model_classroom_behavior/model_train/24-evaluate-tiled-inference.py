"""Evaluate overlapping-tile inference on a standard YOLO detection split."""

from __future__ import annotations

import argparse
import json
import time
from pathlib import Path

import numpy as np
import torch
import yaml
from PIL import Image
from torchvision.ops import batched_nms
from ultralytics import YOLO
from ultralytics.utils.metrics import DetMetrics, box_iou
from ultralytics.utils.ops import xywh2xyxy


N_IOU = 10


def parse_args():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--weights", type=Path, required=True)
    parser.add_argument("--data-yaml", type=Path, required=True)
    parser.add_argument("--split", choices=("val", "test"), default="test")
    parser.add_argument("--imgsz", type=int, default=768, help="Model inference size for each crop")
    parser.add_argument("--tile-size", type=int, default=768, help="Crop side in source pixels; 0 means one full-frame crop")
    parser.add_argument("--overlap", type=float, default=0.25)
    parser.add_argument("--batch", type=int, default=4)
    parser.add_argument("--device", default="0")
    parser.add_argument("--augment", action=argparse.BooleanOptionalAction, default=True)
    parser.add_argument("--iou", type=float, default=0.5, help="Per-crop and merged NMS IoU")
    parser.add_argument("--conf", type=float, default=0.001)
    parser.add_argument("--max-det", type=int, default=300)
    parser.add_argument("--project", type=Path, required=True)
    parser.add_argument("--name", required=True)
    return parser.parse_args()


def resolve_images(data_yaml: Path, split: str) -> tuple[list[Path], dict[int, str]]:
    config = yaml.safe_load(data_yaml.read_text(encoding="utf-8"))
    root = Path(config["path"])
    if not root.is_absolute():
        root = (data_yaml.parent / root).resolve()
    listing = Path(config[split])
    if not listing.is_absolute():
        listing = root / listing
    if listing.suffix.lower() == ".txt":
        entries = [line.strip() for line in listing.read_text(encoding="utf-8-sig").splitlines() if line.strip()]
        images = [Path(entry).resolve() if Path(entry).is_absolute() else (root / entry).resolve() for entry in entries]
    else:
        images = sorted(p.resolve() for p in listing.glob("*.*") if p.is_file())
    missing = [str(path) for path in images if not path.is_file()]
    if missing:
        raise FileNotFoundError(f"Missing split images: {missing[:5]}")
    raw_names = config["names"]
    names = {int(key): value for key, value in raw_names.items()} if isinstance(raw_names, dict) else dict(enumerate(raw_names))
    return images, names


def tile_starts(length: int, tile_size: int, overlap: float) -> list[int]:
    if length <= tile_size:
        return [0]
    step = max(1, int(tile_size * (1 - overlap)))
    starts = list(range(0, length - tile_size + 1, step))
    last = length - tile_size
    if starts[-1] != last:
        starts.append(last)
    return starts


def make_crops(image: Image.Image, tile_size: int, overlap: float):
    width, height = image.size
    if tile_size == 0:
        return [(image, 0, 0)]
    crops = []
    for top in tile_starts(height, tile_size, overlap):
        for left in tile_starts(width, tile_size, overlap):
            right, bottom = min(left + tile_size, width), min(top + tile_size, height)
            crops.append((image.crop((left, top, right, bottom)), left, top))
    return crops


def read_targets(image_path: Path, width: int, height: int, class_count: int):
    label_path = image_path.parent.parent.parent / "labels" / image_path.parent.name / f"{image_path.stem}.txt"
    if not label_path.is_file():
        return np.empty((0, 5), dtype=np.float32)
    rows = []
    for line in label_path.read_text(encoding="utf-8-sig").splitlines():
        fields = line.split()
        if len(fields) != 5:
            continue
        cls, cx, cy, bw, bh = map(float, fields)
        if not cls.is_integer() or not 0 <= int(cls) < class_count:
            continue
        box = xywh2xyxy(torch.tensor([[cx * width, cy * height, bw * width, bh * height]], dtype=torch.float32))[0].numpy()
        rows.append([int(cls), *box.tolist()])
    return np.asarray(rows, dtype=np.float32).reshape(-1, 5)


def match_predictions(pred: torch.Tensor, target: torch.Tensor) -> np.ndarray:
    correct = np.zeros((len(pred), N_IOU), dtype=bool)
    if len(pred) == 0 or len(target) == 0:
        return correct
    overlaps = box_iou(target[:, 1:5], pred[:, :4]).cpu().numpy()
    class_match = target[:, 0:1].cpu().numpy() == pred[:, 5].cpu().numpy()[None, :]
    overlaps *= class_match
    for index, threshold in enumerate(np.linspace(0.5, 0.95, N_IOU)):
        matches = np.array(np.nonzero(overlaps >= threshold)).T
        if matches.shape[0] > 1:
            matches = matches[overlaps[matches[:, 0], matches[:, 1]].argsort()[::-1]]
            matches = matches[np.unique(matches[:, 1], return_index=True)[1]]
            matches = matches[np.unique(matches[:, 0], return_index=True)[1]]
        if len(matches):
            correct[matches[:, 1], index] = True
    return correct


def main():
    args = parse_args()
    if not args.weights.is_file() or not args.data_yaml.is_file():
        raise FileNotFoundError("Weights and dataset YAML must exist.")
    if args.tile_size < 0 or not 0 <= args.overlap < 1:
        raise ValueError("tile-size must be >= 0 and overlap must be in [0, 1).")
    images, names = resolve_images(args.data_yaml, args.split)
    model = YOLO(str(args.weights))
    if model.names != names:
        raise ValueError(f"Model classes {model.names} do not match data classes {names}.")

    metrics = DetMetrics(names=names)
    all_predictions = []
    tile_counts = []
    started = time.perf_counter()
    for index, image_path in enumerate(images, 1):
        with Image.open(image_path) as source:
            image = source.convert("RGB")
        width, height = image.size
        crops = make_crops(image, args.tile_size, args.overlap)
        tile_counts.append(len(crops))
        tile_outputs = []
        for batch_start in range(0, len(crops), args.batch):
            batch = crops[batch_start : batch_start + args.batch]
            results = model.predict(
                source=[item[0] for item in batch],
                imgsz=args.imgsz,
                batch=len(batch),
                device=args.device,
                conf=args.conf,
                iou=args.iou,
                max_det=args.max_det,
                augment=args.augment,
                verbose=False,
                save=False,
            )
            for result, (_, offset_x, offset_y) in zip(results, batch):
                if result.boxes is None or len(result.boxes) == 0:
                    continue
                boxes = result.boxes.xyxy.detach().cpu().clone()
                boxes[:, [0, 2]] += offset_x
                boxes[:, [1, 3]] += offset_y
                boxes[:, [0, 2]].clamp_(0, width)
                boxes[:, [1, 3]].clamp_(0, height)
                tile_outputs.append(torch.cat((boxes, result.boxes.conf.detach().cpu()[:, None], result.boxes.cls.detach().cpu()[:, None]), dim=1))

        if tile_outputs and len(crops) == 1:
            # YOLO.predict already ran NMS for this single full-frame crop.
            prediction = torch.cat(tile_outputs, dim=0)
        elif tile_outputs:
            candidates = torch.cat(tile_outputs, dim=0)
            keep = batched_nms(candidates[:, :4], candidates[:, 4], candidates[:, 5].long(), args.iou)[: args.max_det]
            prediction = candidates[keep]
        else:
            prediction = torch.empty((0, 6), dtype=torch.float32)

        target_np = read_targets(image_path, width, height, len(names))
        target = torch.from_numpy(target_np)
        correct = match_predictions(prediction, target)
        target_cls = target_np[:, 0].astype(np.float32) if len(target_np) else np.empty(0, dtype=np.float32)
        pred_cls = prediction[:, 5].numpy().astype(np.float32) if len(prediction) else np.empty(0, dtype=np.float32)
        conf = prediction[:, 4].numpy() if len(prediction) else np.empty(0, dtype=np.float32)
        metrics.update_stats({
            "tp": correct,
            "conf": conf,
            "pred_cls": pred_cls,
            "target_cls": target_cls,
            "target_img": np.unique(target_cls),
            "im_name": image_path.name,
        })
        all_predictions.extend({
            "image": str(image_path),
            "class_id": int(row[5]),
            "confidence": float(row[4]),
            "bbox_xyxy": [round(float(v), 3) for v in row[:4]],
        } for row in prediction.numpy())
        if index % 25 == 0 or index == len(images):
            print(f"{index}/{len(images)} images; mean tiles/image={np.mean(tile_counts):.2f}", flush=True)

    output = args.project.resolve() / args.name
    if output.exists():
        raise FileExistsError(f"Refusing to overwrite: {output}")
    output.mkdir(parents=True)
    metrics.process(save_dir=output, plot=False)
    report = {
        "weights": str(args.weights.resolve()),
        "data_yaml": str(args.data_yaml.resolve()),
        "split": args.split,
        "images": len(images),
        "imgsz": args.imgsz,
        "tile_size_source_pixels": args.tile_size,
        "overlap": args.overlap,
        "mean_tiles_per_image": float(np.mean(tile_counts)),
        "tta": args.augment,
        "nms_iou": args.iou,
        "conf": args.conf,
        "overall": dict(zip(("precision", "recall", "mAP50", "mAP50_95"), map(float, metrics.mean_results()))),
        "per_class": {names[i]: dict(zip(("precision", "recall", "mAP50", "mAP50_95"), map(float, metrics.class_result(i)))) for i in range(len(names))},
        "speed_seconds_total": time.perf_counter() - started,
        "predictions_count": len(all_predictions),
    }
    (output / "tiled_evaluation.json").write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    (output / "predictions.json").write_text(json.dumps(all_predictions, ensure_ascii=False) + "\n", encoding="utf-8")
    print(json.dumps(report, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
