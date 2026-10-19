from __future__ import annotations

import argparse
import json
import time
from pathlib import Path

import numpy as np

from accuracy_eval import create_inference, decode, image_paths, preprocess_for_backend, read_image, sha256


def summarize(values: list[float]) -> dict[str, float]:
    return {
        "mean": float(np.mean(values)),
        "p50": float(np.percentile(values, 50)),
        "p95": float(np.percentile(values, 95)),
        "p99": float(np.percentile(values, 99)),
    }


def main() -> None:
    parser = argparse.ArgumentParser(description="Measure model preprocessing, inference, and postprocessing latency")
    parser.add_argument("--backend", choices=("onnx", "om"), required=True)
    parser.add_argument("--model", type=Path, required=True)
    parser.add_argument("--data", type=Path, required=True)
    parser.add_argument("--split", default="test")
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--device", type=int, default=0)
    parser.add_argument("--imgsz", type=int, default=640)
    parser.add_argument("--conf", type=float, default=0.25)
    parser.add_argument("--iou", type=float, default=0.45)
    parser.add_argument("--warmup", type=int, default=5)
    parser.add_argument("--limit", type=int)
    args = parser.parse_args()

    data_yaml, model_path = args.data.resolve(), args.model.resolve()
    images, class_names = image_paths(data_yaml, args.split)
    if args.limit is not None:
        images = images[:args.limit]
    if not images:
        raise SystemExit(f"No images found in split {args.split} from {data_yaml}")
    infer, backend_version = create_inference(model_path, args.backend, args.device)
    preprocess_ms, inference_ms, postprocess_ms = [], [], []
    detection_count = 0
    for image_index, image_path in enumerate(images):
        image = read_image(image_path)
        if image is None:
            raise ValueError(f"Cannot decode image: {image_path}")
        started = time.perf_counter()
        batch, pad = preprocess_for_backend(image, args.imgsz, args.backend)
        preprocess_ms.append((time.perf_counter() - started) * 1000)
        if image_index == 0:
            for _ in range(args.warmup):
                infer(batch)
        started = time.perf_counter()
        outputs = infer(batch)
        inference_ms.append((time.perf_counter() - started) * 1000)
        started = time.perf_counter()
        detections = decode(outputs[0], len(class_names), args.imgsz, image.shape[:2], pad, args.conf, args.iou)
        detection_count += len(detections)
        postprocess_ms.append((time.perf_counter() - started) * 1000)

    total_ms = [pre + inf + post for pre, inf, post in zip(preprocess_ms, inference_ms, postprocess_ms)]
    report = {
        "backend": args.backend,
        "backend_version": backend_version,
        "model": model_path.name,
        "model_sha256": sha256(model_path),
        "dataset_yaml_sha256": sha256(data_yaml),
        "split": args.split,
        "images": len(images),
        "image_size": args.imgsz,
        "input_format": "nv21_yuv420sp_u8_static_aipp" if args.backend == "om" else "rgb_nchw_float32",
        "confidence_threshold": args.conf,
        "nms_iou_threshold": args.iou,
        "warmup_iterations": args.warmup,
        "detections": detection_count,
        "latency_ms": {
            "preprocess": summarize(preprocess_ms),
            "inference": summarize(inference_ms),
            "postprocess": summarize(postprocess_ms),
            "pipeline_sum": summarize(total_ms),
        },
        "pipeline_fps": 1000 / float(np.mean(total_ms)),
    }
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(report, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
