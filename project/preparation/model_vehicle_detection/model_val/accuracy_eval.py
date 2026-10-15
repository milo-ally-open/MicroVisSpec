from __future__ import annotations

import argparse
import csv
import hashlib
import json
from pathlib import Path

import cv2
import numpy as np
import yaml

try:
    import onnxruntime as ort
except ImportError:
    ort = None

try:
    from ais_bench.infer.interface import InferSession
except ImportError:
    InferSession = None


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def dataset_manifest(images: list[Path]) -> str:
    digest = hashlib.sha256()
    for image in images:
        label = label_path(image)
        parts = image.parts
        image_index = max(index for index, part in enumerate(parts) if part == "images")
        digest.update(Path(*parts[image_index + 1:]).as_posix().encode("utf-8"))
        digest.update(bytes.fromhex(sha256(image)))
        digest.update(bytes.fromhex(sha256(label)) if label.is_file() else b"no-label")
    return digest.hexdigest()


def image_paths(data_yaml: Path, split: str) -> tuple[list[Path], dict[int, str]]:
    config = yaml.safe_load(data_yaml.read_text(encoding="utf-8"))
    root = Path(config.get("path", data_yaml.parent))
    if not root.is_absolute():
        root = (data_yaml.parent / root).resolve()
    source = config[split]
    sources = source if isinstance(source, list) else [source]
    files = []
    for item in sources:
        path = Path(item)
        if not path.is_absolute():
            path = (root / path).resolve()
        if path.is_dir():
            files.extend(p for p in path.rglob("*") if p.suffix.lower() in {".jpg", ".jpeg", ".png", ".bmp", ".webp"})
        elif path.is_file():
            files.append(path)
        else:
            raise FileNotFoundError(f"Dataset split path does not exist: {path}")
    names = config.get("names", {})
    names = {int(k): str(v) for k, v in names.items()} if isinstance(names, dict) else dict(enumerate(map(str, names)))
    return sorted(set(files)), names


def label_path(image: Path) -> Path:
    parts = list(image.parts)
    for index in range(len(parts) - 1, -1, -1):
        if parts[index] == "images":
            parts[index] = "labels"
            return Path(*parts[:index + 1]) / f"{image.stem}.txt"
    raise ValueError(f"Image is not under an images directory: {image}")


def read_labels(path: Path, width: int, height: int) -> tuple[np.ndarray, np.ndarray]:
    if not path.is_file():
        return np.empty((0, 4), np.float32), np.empty((0,), np.int32)
    boxes, classes = [], []
    for line_number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        if not line.strip():
            continue
        values = [float(value) for value in line.split()]
        if len(values) != 5:
            raise ValueError(f"Expected a YOLO detection box (5 values) in {path}:{line_number}")
        cls, cx, cy, bw, bh = values
        if cls < 0 or not cls.is_integer() or not all(0 <= value <= 1 for value in (cx, cy, bw, bh)) or bw <= 0 or bh <= 0:
            raise ValueError(f"Invalid normalized detection box in {path}:{line_number}")
        boxes.append([(cx - bw / 2) * width, (cy - bh / 2) * height, (cx + bw / 2) * width, (cy + bh / 2) * height])
        classes.append(int(cls))
    return np.asarray(boxes, np.float32).reshape(-1, 4), np.asarray(classes, np.int32)


def preprocess(image: np.ndarray, size: int) -> tuple[np.ndarray, tuple[float, int, int]]:
    height, width = image.shape[:2]
    scale = min(size / width, size / height)
    resized_w, resized_h = int(np.floor(width * scale + 0.5)), int(np.floor(height * scale + 0.5))
    left, top = round((size - resized_w) / 2 - 0.1), round((size - resized_h) / 2 - 0.1)
    right, bottom = size - resized_w - left, size - resized_h - top
    resized = cv2.resize(image, (resized_w, resized_h), interpolation=cv2.INTER_LINEAR)
    canvas = cv2.copyMakeBorder(resized, top, bottom, left, right, cv2.BORDER_CONSTANT, value=(114, 114, 114))
    tensor = canvas[:, :, ::-1].transpose(2, 0, 1).astype(np.float32) / 255.0
    return np.ascontiguousarray(tensor[None]), (scale, left, top)


def decode(output: np.ndarray, classes: int, size: int, original: tuple[int, int], pad: tuple[float, int, int], conf: float, iou: float) -> np.ndarray:
    values = np.asarray(output)
    while values.ndim > 2:
        values = values[0]
    if values.shape[0] < values.shape[1]:
        values = values.T
    if values.shape[1] < 4 + classes:
        raise ValueError(f"Unexpected detector output shape {values.shape}; expected 4+{classes} columns")
    scores = values[:, 4:4 + classes]
    ids = scores.argmax(axis=1)
    confidence = scores[np.arange(len(scores)), ids]
    keep = confidence >= conf
    boxes, confidence, ids = values[keep, :4], confidence[keep], ids[keep]
    if not len(boxes):
        return np.empty((0, 6), np.float32)
    x1 = boxes[:, 0] - boxes[:, 2] / 2
    y1 = boxes[:, 1] - boxes[:, 3] / 2
    x2 = boxes[:, 0] + boxes[:, 2] / 2
    y2 = boxes[:, 1] + boxes[:, 3] / 2
    scale, left, top = pad
    h, w = original
    x1, x2 = np.clip((x1 - left) / scale, 0, w), np.clip((x2 - left) / scale, 0, w)
    y1, y2 = np.clip((y1 - top) / scale, 0, h), np.clip((y2 - top) / scale, 0, h)
    detections = []
    for cls in np.unique(ids):
        indices = np.flatnonzero(ids == cls)
        indices = indices[np.argsort(confidence[indices])[::-1]]
        selected = []
        while len(indices):
            current = indices[0]
            selected.append(current)
            rest = indices[1:]
            if not len(rest):
                break
            xx1 = np.maximum(x1[current], x1[rest])
            yy1 = np.maximum(y1[current], y1[rest])
            xx2 = np.minimum(x2[current], x2[rest])
            yy2 = np.minimum(y2[current], y2[rest])
            inter = np.maximum(0, xx2 - xx1) * np.maximum(0, yy2 - yy1)
            area0 = (x2[current] - x1[current]) * (y2[current] - y1[current])
            area1 = (x2[rest] - x1[rest]) * (y2[rest] - y1[rest])
            overlap = inter / np.maximum(area0 + area1 - inter, 1e-9)
            indices = rest[overlap <= iou]
        for index in selected:
            detections.append([x1[index], y1[index], x2[index], y2[index], confidence[index], cls])
    return np.asarray(detections, np.float32).reshape(-1, 6)


def iou_one(box: np.ndarray, others: np.ndarray) -> np.ndarray:
    if not len(others):
        return np.zeros(0, np.float32)
    inter = np.maximum(0, np.minimum(box[2:], others[:, 2:]) - np.maximum(box[:2], others[:, :2])).prod(axis=1)
    area0 = np.maximum(0, box[2:] - box[:2]).prod()
    area1 = np.maximum(0, others[:, 2:] - others[:, :2]).prod(axis=1)
    return inter / np.maximum(area0 + area1 - inter, 1e-9)


def average_precision(tp: np.ndarray, fp: np.ndarray, positives: int) -> tuple[float | None, float, float]:
    if not positives:
        return None, 0.0, 0.0
    if not len(tp):
        return 0.0, 0.0, 0.0
    tp, fp = np.cumsum(tp), np.cumsum(fp)
    recall, precision = tp / positives, tp / np.maximum(tp + fp, 1e-9)
    mrec = np.concatenate(([0.0], recall, [1.0]))
    mpre = np.concatenate(([1.0], precision, [0.0]))
    mpre = np.maximum.accumulate(mpre[::-1])[::-1]
    points = np.linspace(0, 1, 101)
    indices = np.minimum(np.searchsorted(mrec, points, side="left"), len(mrec) - 1)
    ap = np.mean(mpre[indices])
    return float(ap), float(precision[-1]), float(recall[-1])


def evaluate_metrics(records: list[dict], class_names: dict[int, str]) -> dict:
    class_ids = sorted(set(class_names) | {int(c) for item in records for c in item["gt_classes"]} | {int(d[5]) for item in records for d in item["detections"]})
    rows = {}
    thresholds = np.arange(0.5, 0.951, 0.05)
    for cls in class_ids:
        ground_truth = {i: np.asarray(item["gt_boxes"], np.float32).reshape(-1, 4)[np.asarray(item["gt_classes"]) == cls] for i, item in enumerate(records)}
        positives = sum(len(x) for x in ground_truth.values())
        predictions = [(i, d) for i, item in enumerate(records) for d in item["detections"] if int(d[5]) == cls]
        predictions.sort(key=lambda item: item[1][4], reverse=True)
        ap_values = []
        for threshold in thresholds:
            matched = {i: set() for i in range(len(records))}
            tp, fp = [], []
            for image_index, det in predictions:
                candidates = ground_truth[image_index]
                overlaps = iou_one(det[:4], candidates)
                order = np.argsort(overlaps)[::-1]
                match = next((int(j) for j in order if overlaps[j] >= threshold and int(j) not in matched[image_index]), None)
                tp.append(float(match is not None))
                fp.append(float(match is None))
                if match is not None:
                    matched[image_index].add(match)
            ap, _, _ = average_precision(np.asarray(tp), np.asarray(fp), positives)
            if ap is not None:
                ap_values.append(ap)
        fixed_tp = fixed_fp = 0
        for image_index, item in enumerate(records):
            gt = ground_truth[image_index]
            matched = set()
            for det in sorted((d for d in item["detections"] if int(d[5]) == cls and d[4] >= item["conf"]), key=lambda d: d[4], reverse=True):
                overlaps = iou_one(det[:4], gt)
                order = np.argsort(overlaps)[::-1]
                match = next((int(j) for j in order if overlaps[j] >= item["iou"] and int(j) not in matched), None)
                if match is None:
                    fixed_fp += 1
                else:
                    fixed_tp += 1
                    matched.add(match)
        fixed_fn = positives - fixed_tp
        precision = fixed_tp / max(fixed_tp + fixed_fp, 1)
        recall = fixed_tp / max(fixed_tp + fixed_fn, 1)
        rows[str(cls)] = {
            "name": class_names.get(cls, str(cls)), "ground_truth": positives,
            "detections_at_conf": fixed_tp + fixed_fp, "precision": precision,
            "recall": recall, "f1": 2 * precision * recall / max(precision + recall, 1e-12),
            "ap50": ap_values[0] if ap_values else None,
            "map50_95": float(np.mean(ap_values)) if ap_values else None,
        }
    present = [row for row in rows.values() if row["ground_truth"] > 0]
    tp = sum(row["precision"] * row["detections_at_conf"] for row in rows.values())
    fp = sum(row["detections_at_conf"] for row in rows.values()) - tp
    gt = sum(row["ground_truth"] for row in present)
    micro_p, micro_r = tp / max(tp + fp, 1), tp / max(gt, 1)
    return {"operating_point": {"precision": micro_p, "recall": micro_r, "f1": 2 * micro_p * micro_r / max(micro_p + micro_r, 1e-12)},
            "macro_ap50": float(np.mean([x["ap50"] for x in present])) if present else None,
            "map50_95": float(np.mean([x["map50_95"] for x in present])) if present else None,
            "per_class": rows}


def create_inference(model_path: Path, backend: str, device: int):
    if backend == "onnx":
        if ort is None:
            raise SystemExit("ONNX Runtime is required for --backend onnx")
        session = ort.InferenceSession(str(model_path), providers=["CPUExecutionProvider"])
        input_name = session.get_inputs()[0].name
        return lambda batch: session.run(None, {input_name: batch}), ort.__version__
    if InferSession is None:
        raise SystemExit("aclruntime via ais_bench is required for --backend om")
    session = InferSession(device, str(model_path))
    return lambda batch: session.infer([batch]), "aclruntime via ais_bench InferSession"


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--backend", choices=("onnx", "om"), required=True)
    parser.add_argument("--model", type=Path, required=True)
    parser.add_argument("--data", type=Path, required=True)
    parser.add_argument("--split", default="test")
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--device", type=int, default=0)
    parser.add_argument("--imgsz", type=int, default=640)
    parser.add_argument("--conf", type=float, default=0.001)
    parser.add_argument("--iou", type=float, default=0.45)
    parser.add_argument("--warmup", type=int, default=5)
    parser.add_argument("--limit", type=int)
    args = parser.parse_args()
    data_yaml, model_path = args.data.resolve(), args.model.resolve()
    images, class_names = image_paths(data_yaml, args.split)
    if args.limit:
        images = images[:args.limit]
    if not images:
        raise SystemExit(f"No images found in split {args.split} from {data_yaml}")
    infer, backend_version = create_inference(model_path, args.backend, args.device)
    records = []
    for image_path in images:
        image = cv2.imread(str(image_path))
        if image is None:
            raise ValueError(f"Cannot decode image: {image_path}")
        batch, pad = preprocess(image, args.imgsz)
        if not records:
            for _ in range(args.warmup):
                infer(batch)
        outputs = infer(batch)
        detections = decode(outputs[0], len(class_names), args.imgsz, image.shape[:2], pad, min(args.conf, 0.001), args.iou)
        gt_boxes, gt_classes = read_labels(label_path(image_path), image.shape[1], image.shape[0])
        records.append({"image": image_path.name, "detections": detections.tolist(), "gt_boxes": gt_boxes.tolist(), "gt_classes": gt_classes.tolist(), "conf": args.conf, "iou": args.iou})
    if not any(item["gt_classes"] for item in records):
        raise SystemExit(f"No ground-truth labels were found for split {args.split}; refusing to report accuracy metrics")
    report = {
        "backend": args.backend, "backend_version": backend_version,
        "model": model_path.name, "model_sha256": sha256(model_path),
        "dataset_yaml": str(data_yaml), "dataset_yaml_sha256": sha256(data_yaml),
        "dataset_manifest_sha256": dataset_manifest(images),
        "split": args.split, "images": len(images), "image_size": args.imgsz,
        "confidence_threshold": args.conf, "nms_iou_threshold": args.iou,
        "preprocess": "BGR decode; RGB CHW float32 /255; 114 letterbox; fixed square input",
        "metrics": evaluate_metrics(records, class_names),
    }
    args.output.mkdir(parents=True, exist_ok=True)
    (args.output / f"{args.backend}_report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    with (args.output / f"{args.backend}_per_class.csv").open("w", newline="", encoding="utf-8-sig") as stream:
        writer = csv.DictWriter(stream, fieldnames=("class_id", "name", "ground_truth", "detections_at_conf", "precision", "recall", "f1", "ap50", "map50_95"))
        writer.writeheader()
        for class_id, row in report["metrics"]["per_class"].items():
            writer.writerow({"class_id": class_id, **row})
    print(json.dumps({k: report[k] for k in ("backend", "images", "metrics")}, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
