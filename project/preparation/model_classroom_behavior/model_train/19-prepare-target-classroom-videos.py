"""Sample target-classroom videos into frames while preserving video identity.

Run this script from the model directory and pass local directories with
``--input-dir`` and ``--output-dir``. It does not assign frame-level
train/validation/test splits. Keep every frame from one source video in the
same split; see ``TARGET_DOMAIN_DATA.md``.
"""

from __future__ import annotations

import argparse
import csv
import hashlib
import json
from datetime import datetime, timezone
from pathlib import Path

import cv2


VIDEO_SUFFIXES = {".avi", ".m4v", ".mkv", ".mov", ".mp4", ".webm", ".wmv"}
MANIFEST_FIELDS = (
    "source_video_id",
    "source_filename",
    "source_sha256",
    "frame_index",
    "timestamp_ms",
    "width",
    "height",
    "frame_path",
)


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input-dir", type=Path, required=True, help="Directory of source videos, searched recursively.")
    parser.add_argument("--output-dir", type=Path, required=True, help="New output directory outside the project repository.")
    parser.add_argument("--interval-seconds", type=float, default=2.0, help="Approximate spacing between sampled frames (default: 2 seconds).")
    parser.add_argument("--jpeg-quality", type=int, default=95, help="JPEG quality from 1 to 100 (default: 95).")
    return parser.parse_args()


def main() -> None:
    args = parse_args()
    input_dir = args.input_dir.expanduser().resolve()
    output_dir = args.output_dir.expanduser().resolve()
    if not input_dir.is_dir():
        raise FileNotFoundError(f"Input directory does not exist: {input_dir}")
    if input_dir == output_dir or input_dir in output_dir.parents:
        raise ValueError("Output directory must not be inside the source-video directory.")
    if not (0.1 <= args.interval_seconds <= 3600):
        raise ValueError("--interval-seconds must be between 0.1 and 3600.")
    if not (1 <= args.jpeg_quality <= 100):
        raise ValueError("--jpeg-quality must be between 1 and 100.")
    if output_dir.exists() and any(output_dir.iterdir()):
        raise FileExistsError(f"Output directory is not empty; choose a new path: {output_dir}")

    videos = sorted(path for path in input_dir.rglob("*") if path.is_file() and path.suffix.lower() in VIDEO_SUFFIXES)
    if not videos:
        raise FileNotFoundError(f"No supported video files found under {input_dir}")

    output_dir.mkdir(parents=True, exist_ok=True)
    manifest_path = output_dir / "frames_manifest.csv"
    video_summaries: list[dict[str, object]] = []
    sampled_count = 0
    with manifest_path.open("w", newline="", encoding="utf-8-sig") as manifest_file:
        writer = csv.DictWriter(manifest_file, fieldnames=MANIFEST_FIELDS)
        writer.writeheader()
        manifest_file.flush()

        for video_path in videos:
            video_hash = sha256_file(video_path)
            video_id = video_hash[:16]
            capture = cv2.VideoCapture(str(video_path))
            if not capture.isOpened():
                capture.release()
                raise ValueError(f"Cannot open video: {video_path}")

            fps = float(capture.get(cv2.CAP_PROP_FPS))
            if fps <= 0:
                capture.release()
                raise ValueError(f"Video reports an invalid frame rate: {video_path}")
            frame_count = int(capture.get(cv2.CAP_PROP_FRAME_COUNT))
            stride = max(1, round(fps * args.interval_seconds))
            video_output = output_dir / video_id
            video_output.mkdir(parents=True, exist_ok=True)
            sampled_video_count = 0
            frame_index = 0

            while True:
                ok, frame = capture.read()
                if not ok:
                    break
                if frame_index % stride == 0:
                    timestamp_ms = float(capture.get(cv2.CAP_PROP_POS_MSEC))
                    height, width = frame.shape[:2]
                    frame_name = f"{video_id}_{frame_index:09d}_{timestamp_ms:012.1f}ms.jpg"
                    frame_path = video_output / frame_name
                    encoded_ok, encoded = cv2.imencode(
                        ".jpg", frame, [cv2.IMWRITE_JPEG_QUALITY, args.jpeg_quality]
                    )
                    if not encoded_ok:
                        capture.release()
                        raise ValueError(f"Could not encode sampled frame: {video_path} @ {timestamp_ms:.1f} ms")
                    encoded.tofile(str(frame_path))
                    writer.writerow(
                        {
                            "source_video_id": video_id,
                            "source_filename": video_path.name,
                            "source_sha256": video_hash,
                            "frame_index": frame_index,
                            "timestamp_ms": f"{timestamp_ms:.1f}",
                            "width": width,
                            "height": height,
                            "frame_path": str(frame_path.relative_to(output_dir)),
                        }
                    )
                    sampled_video_count += 1
                    sampled_count += 1
                frame_index += 1

            capture.release()
            if frame_index == 0:
                raise ValueError(f"Video contained no readable frames: {video_path}")
            video_summaries.append(
                {
                    "source_video_id": video_id,
                    "source_filename": video_path.name,
                    "source_sha256": video_hash,
                    "fps": fps,
                    "reported_frame_count": frame_count,
                    "decoded_frame_count": frame_index,
                    "sampled_frame_count": sampled_video_count,
                    "interval_seconds": args.interval_seconds,
                }
            )
            manifest_file.flush()
            print(f"{video_path.name}: {sampled_video_count} frames, source_video_id={video_id}", flush=True)

    summary = {
        "created_utc": datetime.now(timezone.utc).isoformat(),
        "input_dir": str(input_dir),
        "output_dir": str(output_dir),
        "interval_seconds": args.interval_seconds,
        "jpeg_quality": args.jpeg_quality,
        "video_count": len(video_summaries),
        "sampled_frame_count": sampled_count,
        "split_policy": "not assigned; split by source_video_id after annotation",
        "videos": video_summaries,
    }
    (output_dir / "sampling_summary.json").write_text(
        json.dumps(summary, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
    )
    print(f"Saved {sampled_count} frames from {len(video_summaries)} videos to {output_dir}")
    print(f"Manifest: {manifest_path}")


if __name__ == "__main__":
    main()
