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
    import torch
    from ultralytics.utils.metrics import ap_per_class, box_iou
except ImportError:
    torch = None
    ap_per_class = None
    box_iou = None

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
            original_source = str(path).replace("\\", "/")
            path = (root / path).resolve()
            if not path.exists() and original_source.startswith("../"):
                path = (root / Path(*original_source[3:].split("/"))).resolve()
        if path.is_dir():
            files.extend(p for p in path.rglob("*") if p.suffix.lower() in {".jpg", ".jpeg", ".png", ".bmp", ".webp"})
        elif path.is_file() and path.suffix.lower() == ".txt":
            for line in path.read_text(encoding="utf-8-sig").splitlines():
                entry = line.strip()
                if not entry:
                    continue
                image = Path(entry)
                if not image.is_absolute():
                    image = (root / image).resolve()
                if image.suffix.lower() not in {".jpg", ".jpeg", ".png", ".bmp", ".webp"}:
                    raise ValueError(f"Unsupported image in split list {path}: {entry}")
                if not image.is_file():
                    raise FileNotFoundError(f"Image from split list does not exist: {image}")
                files.append(image)
        elif path.is_file():
            files.append(path)
        else:
            raise FileNotFoundError(f"Dataset split path does not exist: {path}")
    names = config.get("names", {})
    names = {int(k): str(v) for k, v in names.items()} if isinstance(names, dict) else dict(enumerate(map(str, names)))
    return sorted(set(files)), names


def read_image(path: Path) -> np.ndarray | None:
    """Decode from bytes so OpenCV works with non-ASCII Windows paths."""
    image_bytes = np.fromfile(path, dtype=np.uint8)
    return cv2.imdecode(image_bytes, cv2.IMREAD_COLOR)


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
        is_box = len(values) == 5
        is_polygon = len(values) >= 7 and (len(values) - 1) % 2 == 0
        if not (is_box or is_polygon):
            raise ValueError(f"Expected a YOLO box or polygon in {path}:{line_number}")
        cls = values[0]
        if cls < 0 or not cls.is_integer():
            raise ValueError(f"Invalid class ID in {path}:{line_number}")
        if is_box:
            cx, cy, bw, bh = values[1:]
        else:
            points = np.asarray(values[1:], dtype=np.float32).reshape(-1, 2)
            if not np.isfinite(points).all() or (points < 0).any() or (points > 1).any():
                raise ValueError(f"Invalid normalized polygon in {path}:{line_number}")
            x1, y1 = points.min(axis=0)
            x2, y2 = points.max(axis=0)
            cx, cy, bw, bh = (x1 + x2) / 2, (y1 + y2) / 2, x2 - x1, y2 - y1
        if not np.isfinite([cx, cy, bw, bh]).all() or not all(0 <= value <= 1 for value in (cx, cy, bw, bh)) or bw <= 0 or bh <= 0:
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


def preprocess_nv21(image: np.ndarray, size: int) -> tuple[np.ndarray, tuple[float, int, int]]:
    """Build the static-AIPP input contract: packed Y followed by interleaved VU."""
    if size <= 0 or size % 2:
        raise ValueError("NV21 input size must be a positive even number")
    height, width = image.shape[:2]
    scale = min(size / width, size / height)
    resized_w = int(np.floor(width * scale + 0.5))
    resized_h = int(np.floor(height * scale + 0.5))
    left = round((size - resized_w) / 2 - 0.1)
    top = round((size - resized_h) / 2 - 0.1)
    right, bottom = size - resized_w - left, size - resized_h - top
    resized = cv2.resize(image, (resized_w, resized_h), interpolation=cv2.INTER_LINEAR)
    canvas = cv2.copyMakeBorder(resized, top, bottom, left, right, cv2.BORDER_CONSTANT, value=(114, 114, 114))
    i420 = cv2.cvtColor(canvas, cv2.COLOR_BGR2YUV_I420).reshape(-1)
    y_bytes = size * size
    chroma_bytes = y_bytes // 4
    y_plane = i420[:y_bytes]
    u_plane = i420[y_bytes:y_bytes + chroma_bytes].reshape(size // 2, size // 2)
    v_plane = i420[y_bytes + chroma_bytes:y_bytes + 2 * chroma_bytes].reshape(size // 2, size // 2)
    vu_plane = np.empty((size // 2, size), dtype=np.uint8)
    vu_plane[:, 0::2] = v_plane
    vu_plane[:, 1::2] = u_plane
    nv21 = np.concatenate((y_plane, vu_plane.reshape(-1)))
    return np.ascontiguousarray(nv21.reshape(1, -1)), (scale, left, top)


def preprocess_for_backend(image: np.ndarray, size: int, backend: str) -> tuple[np.ndarray, tuple[float, int, int]]:
    if backend == "om":
        return preprocess_nv21(image, size)
    return preprocess(image, size)


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
            # Match Ultralytics ordering: perform NMS in network coordinates, then
            # remove letterbox padding and rescale without clipping before AP matching.
            # Keep predictions beyond the image edge for AP matching. Ultralytics
            # evaluates unclipped network-space boxes; clipping before IoU changes
            # edge-object matches. The affine inverse preserves IoU without clipping.
            source_x1 = (x1[index] - left) / scale
            source_y1 = (y1[index] - top) / scale
            source_x2 = (x2[index] - left) / scale
            source_y2 = (y2[index] - top) / scale
            detections.append([source_x1, source_y1, source_x2, source_y2, confidence[index], cls])
    detections = np.asarray(detections, np.float32).reshape(-1, 6)
    if len(detections) > 300:
        detections = detections[np.argsort(detections[:, 4])[::-1][:300]]
    elif len(detections):
        detections = detections[np.argsort(detections[:, 4])[::-1]]
    return detections


def iou_one(box: np.ndarray, others: np.ndarray) -> np.ndarray:
    box = np.asarray(box, np.float32)
    others = np.asarray(others, np.float32).reshape(-1, 4)
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
    mrec = np.concatenate(([0.0], recall, [recall[-1]], [1.0]))
    mpre = np.concatenate(([1.0], precision, [0.0], [0.0]))
    mpre = np.maximum.accumulate(mpre[::-1])[::-1]
    points = np.linspace(0, 1, 101)
    trapezoid_fn = getattr(np, "trapezoid", None) or getattr(np, "trapz")
    ap = trapezoid_fn(np.interp(points, mrec, mpre), points)
    return float(ap), float(precision[-1]), float(recall[-1])


def match_predictions_ultralytics(overlaps: np.ndarray, threshold: float) -> set[int]:
    """Return matched prediction indices using Ultralytics' greedy IoU ordering."""
    matches = np.array(np.nonzero(overlaps >= threshold)).T
    if len(matches) > 1:
        matches = matches[np.argsort(overlaps[matches[:, 0], matches[:, 1]])[::-1]]
        matches = matches[np.unique(matches[:, 1], return_index=True)[1]]
        matches = matches[np.unique(matches[:, 0], return_index=True)[1]]
    return set(matches[:, 1].astype(int)) if len(matches) else set()


def evaluate_metrics(records: list[dict], class_names: dict[int, str]) -> dict:
    if ap_per_class is not None and box_iou is not None and torch is not None:
        return evaluate_metrics_ultralytics(records, class_names)

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
            assignment: dict[int, set[int]] = {}
            for image_index in range(len(records)):
                image_predictions = [det for idx, det in predictions if idx == image_index]
                overlaps = np.zeros((len(ground_truth[image_index]), len(image_predictions)), np.float32)
                for det_index, det in enumerate(image_predictions):
                    overlaps[:, det_index] = iou_one(det[:4], ground_truth[image_index])
                assignment[image_index] = match_predictions_ultralytics(overlaps, threshold)
            per_image_seen = {image_index: 0 for image_index in range(len(records))}
            tp, fp = [], []
            for image_index, _ in predictions:
                local_index = per_image_seen[image_index]
                is_match = local_index in assignment[image_index]
                tp.append(float(is_match))
                fp.append(float(not is_match))
                per_image_seen[image_index] += 1
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


def evaluate_metrics_ultralytics(records: list[dict], class_names: dict[int, str]) -> dict:
    """Use Ultralytics' native box matching and AP accumulator for report parity."""
    iouv = torch.linspace(0.5, 0.95, 10)
    stats = []
    for item in records:
        detections = np.asarray(item["detections"], np.float32).reshape(-1, 6)
        gt_boxes = np.asarray(item["gt_boxes"], np.float32).reshape(-1, 4)
        gt_classes = np.asarray(item["gt_classes"], np.float32).reshape(-1)
        pred_boxes = detections[:, :4]
        pred_conf = detections[:, 4]
        pred_classes = detections[:, 5]
        correct = np.zeros((len(detections), len(iouv)), dtype=bool)
        if len(gt_boxes) and len(detections):
            overlaps = box_iou(torch.from_numpy(gt_boxes), torch.from_numpy(pred_boxes)).numpy()
            overlaps *= gt_classes[:, None] == pred_classes[None, :]
            for threshold_index, threshold in enumerate(iouv.numpy()):
                matches = np.array(np.nonzero(overlaps >= threshold)).T
                if len(matches) > 1:
                    matches = matches[np.argsort(overlaps[matches[:, 0], matches[:, 1]])[::-1]]
                    matches = matches[np.unique(matches[:, 1], return_index=True)[1]]
                    matches = matches[np.unique(matches[:, 0], return_index=True)[1]]
                if len(matches):
                    correct[matches[:, 1].astype(int), threshold_index] = True
        stats.append((correct, pred_conf, pred_classes, gt_classes))

    tp = np.concatenate([item[0] for item in stats], axis=0)
    confidence = np.concatenate([item[1] for item in stats], axis=0)
    prediction_classes = np.concatenate([item[2] for item in stats], axis=0)
    target_classes = np.concatenate([item[3] for item in stats], axis=0)
    _, _, precision, recall, f1, ap, unique_classes, *_ = ap_per_class(
        tp, confidence, prediction_classes, target_classes, names=class_names
    )
    rows = {}
    for index, cls in enumerate(unique_classes):
        gt_count = int(np.count_nonzero(target_classes == cls))
        tp_count = int(round(float(recall[index]) * gt_count))
        fp_count = int(round(tp_count / max(float(precision[index]), 1e-16) - tp_count))
        rows[str(int(cls))] = {
            "name": class_names.get(int(cls), str(int(cls))),
            "ground_truth": gt_count,
            "detections_at_conf": tp_count + fp_count,
            "precision": float(precision[index]),
            "recall": float(recall[index]),
            "f1": float(f1[index]),
            "ap50": float(ap[index, 0]),
            "map50_95": float(ap[index].mean()),
        }
    present = list(rows.values())
    macro_precision = float(np.mean([row["precision"] for row in present])) if present else 0.0
    macro_recall = float(np.mean([row["recall"] for row in present])) if present else 0.0
    macro_f1 = float(np.mean([row["f1"] for row in present])) if present else 0.0
    return {
        "operating_point": {
            "selection": "Ultralytics smoothed max-F1 confidence per class",
            "precision": macro_precision,
            "recall": macro_recall,
            "f1": macro_f1,
        },
        "macro_ap50": float(np.mean([row["ap50"] for row in present])) if present else None,
        "map50_95": float(np.mean([row["map50_95"] for row in present])) if present else None,
        "per_class": rows,
    }


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
        image = read_image(image_path)
        if image is None:
            raise ValueError(f"Cannot decode image: {image_path}")
        batch, pad = preprocess_for_backend(image, args.imgsz, args.backend)
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
        "metric_implementation": "Ultralytics ap_per_class / box_iou" if ap_per_class is not None else "local fallback",
        "preprocess": (
            "BGR decode; 114 letterbox; NV21 YUV420SP_U8 (Y then interleaved VU); static AIPP converts to normalized RGB"
            if args.backend == "om"
            else "BGR decode; RGB CHW float32 /255; 114 letterbox; fixed square input"
        ),
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
