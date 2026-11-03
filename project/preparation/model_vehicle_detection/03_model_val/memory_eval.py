from __future__ import annotations

import argparse
import json
import resource
import subprocess
from pathlib import Path

import cv2

from accuracy_eval import create_inference, image_paths, preprocess, sha256


def main() -> None:
    parser = argparse.ArgumentParser(description="Measure model package and inference process memory")
    parser.add_argument("--backend", choices=("onnx", "om"), required=True)
    parser.add_argument("--model", type=Path, required=True)
    parser.add_argument("--data", type=Path, required=True)
    parser.add_argument("--split", default="test")
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--device", type=int, default=0)
    parser.add_argument("--imgsz", type=int, default=640)
    parser.add_argument("--limit", type=int, default=1)
    args = parser.parse_args()

    model_path, data_yaml = args.model.resolve(), args.data.resolve()
    images, _ = image_paths(data_yaml, args.split)
    if args.limit is not None:
        images = images[:args.limit]
    if not images:
        raise SystemExit(f"No images found in split {args.split} from {data_yaml}")
    infer, backend_version = create_inference(model_path, args.backend, args.device)
    first_image = cv2.imread(str(images[0]))
    if first_image is None:
        raise ValueError(f"Cannot decode image: {images[0]}")
    batch, _ = preprocess(first_image, args.imgsz)
    outputs = infer(batch)
    process_peak_rss_mib = resource.getrusage(resource.RUSAGE_SELF).ru_maxrss / 1024
    npu_info = ""
    try:
        result = subprocess.run(["npu-smi", "info"], check=False, capture_output=True, text=True, timeout=15)
        npu_info = result.stdout + result.stderr
    except (FileNotFoundError, subprocess.TimeoutExpired):
        npu_info = "npu-smi unavailable; no device-memory reading was collected."

    report = {
        "backend": args.backend,
        "backend_version": backend_version,
        "model": model_path.name,
        "model_sha256": sha256(model_path),
        "model_file_bytes": model_path.stat().st_size,
        "dataset_yaml_sha256": sha256(data_yaml),
        "split": args.split,
        "sample_images": len(images),
        "image_size": args.imgsz,
        "process_peak_rss_mib": process_peak_rss_mib,
        "input_tensor_bytes": int(batch.nbytes),
        "output_tensors": [
            {"shape": list(output.shape), "dtype": str(output.dtype), "bytes": int(output.nbytes)}
            for output in outputs
        ],
        "npu_smi": "captured" if npu_info and not npu_info.startswith("npu-smi unavailable") else "unavailable",
    }
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    args.output.with_suffix(".npu-smi.txt").write_text(npu_info + "\n", encoding="utf-8")
    print(json.dumps(report, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
