"""Pack manually annotated target frames into a video-isolated YOLO dataset.

Inputs are an output directory from ``19-prepare-target-classroom-videos.py``,
one YOLO label text file per frame (empty file means a reviewed background),
and an explicit source_video_id-to-split CSV. The tool refuses missing labels,
invalid boxes, duplicate IDs, and exact-image leakage across splits.
"""

from __future__ import annotations

import argparse
import csv
import hashlib
import json
import os
import shutil
from collections import Counter, defaultdict
from pathlib import Path

import yaml


CLASSES = ["hand-raising", "reading", "writing"]
SPLITS = {"train", "valid", "test"}
IMAGE_SUFFIXES = {".bmp", ".jpg", ".jpeg", ".png", ".tif", ".tiff", ".webp"}


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--frames-dir", type=Path, required=True, help="Frame sampler output containing frames_manifest.csv.")
    parser.add_argument("--labels-dir", type=Path, required=True, help="YOLO labels named <frame-stem>.txt; include empty files for reviewed backgrounds.")
    parser.add_argument("--source-splits", type=Path, required=True, help="CSV columns: source_video_id,split (train, valid, or test).")
    parser.add_argument("--output-dir", type=Path, required=True, help="New output directory outside the Git repository.")
    return parser.parse_args()


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def checked_path(root: Path, relative: str) -> Path:
    path = (root / relative).resolve()
    if path != root and root not in path.parents:
        raise ValueError(f"Manifest path escapes its root directory: {relative}")
    return path


def read_splits(path: Path) -> dict[str, str]:
    with path.open("r", newline="", encoding="utf-8-sig") as stream:
        reader = csv.DictReader(stream)
        if not reader.fieldnames or not {"source_video_id", "split"}.issubset(reader.fieldnames):
            raise ValueError("Source split CSV must contain source_video_id and split columns")
        mapping: dict[str, str] = {}
        for line_no, row in enumerate(reader, start=2):
            video_id = (row.get("source_video_id") or "").strip()
            split = (row.get("split") or "").strip().lower()
            if not video_id:
                raise ValueError(f"Blank source_video_id at {path}:{line_no}")
            if split not in SPLITS:
                raise ValueError(f"Invalid split {split!r} at {path}:{line_no}; expected train, valid, or test")
            if video_id in mapping:
                raise ValueError(f"Duplicate source_video_id in split CSV: {video_id}")
            mapping[video_id] = split
    if set(mapping.values()) != SPLITS:
        raise ValueError(f"All three splits must have at least one source video; found {sorted(set(mapping.values()))}")
    return mapping


def validate_label(path: Path) -> tuple[Counter[int], bool]:
    counts: Counter[int] = Counter()
    rows = path.read_text(encoding="utf-8-sig").splitlines()
    for line_no, line in enumerate(rows, start=1):
        fields = line.split()
        if len(fields) != 5:
            raise ValueError(f"Expected 5 YOLO columns at {path}:{line_no}: {line!r}")
        try:
            class_value = float(fields[0])
            values = [float(value) for value in fields[1:]]
        except ValueError as exc:
            raise ValueError(f"Non-numeric YOLO value at {path}:{line_no}") from exc
        if not class_value.is_integer() or int(class_value) not in range(len(CLASSES)):
            raise ValueError(f"Class ID must be 0, 1, or 2 at {path}:{line_no}")
        cx, cy, width, height = values
        if not all(0.0 <= value <= 1.0 for value in values):
            raise ValueError(f"Normalized values must be in [0,1] at {path}:{line_no}")
        if width <= 0 or height <= 0 or cx - width / 2 < 0 or cy - height / 2 < 0 or cx + width / 2 > 1 or cy + height / 2 > 1:
            raise ValueError(f"Degenerate or out-of-frame box at {path}:{line_no}")
        counts[int(class_value)] += 1
    return counts, not any(line.strip() for line in rows)


def main() -> None:
    args = parse_args()
    frames_dir = args.frames_dir.expanduser().resolve()
    labels_dir = args.labels_dir.expanduser().resolve()
    split_csv = args.source_splits.expanduser().resolve()
    output_dir = args.output_dir.expanduser().resolve()
    if not frames_dir.is_dir():
        raise FileNotFoundError(f"Frame directory does not exist: {frames_dir}")
    if not labels_dir.is_dir():
        raise FileNotFoundError(f"Label directory does not exist: {labels_dir}")
    if not split_csv.is_file():
        raise FileNotFoundError(f"Source split CSV does not exist: {split_csv}")
    manifest_path = frames_dir / "frames_manifest.csv"
    if not manifest_path.is_file():
        raise FileNotFoundError(f"Missing frame manifest: {manifest_path}")
    if output_dir.exists() and any(output_dir.iterdir()):
        raise FileExistsError(f"Output directory must be new or empty: {output_dir}")
    project_root = Path(__file__).resolve().parents[1]
    if output_dir == project_root or project_root in output_dir.parents:
        raise ValueError(f"Target classroom data must stay outside the Git repository: {output_dir}")
    for source_root in (frames_dir, labels_dir):
        if output_dir == source_root or source_root in output_dir.parents or output_dir in source_root.parents:
            raise ValueError("Output directory cannot contain or be contained by the frame or label source directory")

    video_splits = read_splits(split_csv)
    records: list[dict[str, str | Path | Counter[int] | bool]] = []
    seen_frame_names: set[str] = set()
    seen_label_stems: set[str] = set()
    observed_video_hashes: dict[str, str] = {}
    with manifest_path.open("r", newline="", encoding="utf-8-sig") as stream:
        reader = csv.DictReader(stream)
        required = {"source_video_id", "source_sha256", "frame_path"}
        if not reader.fieldnames or not required.issubset(reader.fieldnames):
            raise ValueError(f"Frame manifest must contain columns: {sorted(required)}")
        for line_no, row in enumerate(reader, start=2):
            video_id = (row.get("source_video_id") or "").strip()
            video_hash = (row.get("source_sha256") or "").strip().lower()
            frame_rel = (row.get("frame_path") or "").strip()
            if video_id not in video_splits:
                raise ValueError(f"No split assigned to source_video_id={video_id!r} at {manifest_path}:{line_no}")
            if len(video_hash) != 64 or video_id != video_hash[:16]:
                raise ValueError(f"Video ID/hash mismatch at {manifest_path}:{line_no}")
            previous_hash = observed_video_hashes.setdefault(video_id, video_hash)
            if previous_hash != video_hash:
                raise ValueError(f"One source_video_id maps to multiple video hashes: {video_id}")
            frame_path = checked_path(frames_dir, frame_rel)
            if not frame_path.is_file() or frame_path.suffix.lower() not in IMAGE_SUFFIXES:
                raise FileNotFoundError(f"Missing or unsupported frame at {manifest_path}:{line_no}: {frame_path}")
            if frame_path.name in seen_frame_names:
                raise ValueError(f"Frame basename collision; use unique frame names: {frame_path.name}")
            seen_frame_names.add(frame_path.name)
            if frame_path.stem in seen_label_stems:
                raise ValueError(f"Frame stems must be unique for matching YOLO labels: {frame_path.stem}")
            seen_label_stems.add(frame_path.stem)
            label_path = labels_dir / f"{frame_path.stem}.txt"
            if not label_path.is_file():
                raise FileNotFoundError(f"Missing label file (create an empty file for a reviewed background): {label_path}")
            counts, is_background = validate_label(label_path)
            records.append(
                {
                    "source_video_id": video_id,
                    "source_sha256": (row.get("source_sha256") or "").strip(),
                    "frame_path": frame_path,
                    "label_path": label_path,
                    "split": video_splits[video_id],
                    "counts": counts,
                    "background": is_background,
                }
            )
    if not records:
        raise ValueError("Frame manifest contains no frames")
    missing_video_records = sorted(set(video_splits) - set(observed_video_hashes))
    if missing_video_records:
        raise ValueError(f"Split CSV contains videos with no sampled frames: {missing_video_records[:5]}")

    expected_labels = {f"{Path(str(item['frame_path'])).stem}.txt" for item in records}
    orphan_labels = sorted(path.name for path in labels_dir.glob("*.txt") if path.name not in expected_labels)
    if orphan_labels:
        raise ValueError(f"Found {len(orphan_labels)} labels without a matching manifest frame; sample: {orphan_labels[:5]}")

    # Exact duplicate frame files must never cross a video-level split.
    content_splits: dict[str, tuple[str, str]] = {}
    exact_duplicate_count = 0
    for item in records:
        frame_path = Path(str(item["frame_path"]))
        digest = sha256_file(frame_path)
        current = (str(item["split"]), str(item["source_video_id"]))
        previous = content_splits.get(digest)
        if previous and previous[0] != current[0]:
            raise ValueError(f"Exact duplicate frames cross splits: {previous[1]} ({previous[0]}) and {current[1]} ({current[0]})")
        if previous:
            exact_duplicate_count += 1
        else:
            content_splits[digest] = current

    counts_by_split: dict[str, Counter[int]] = defaultdict(Counter)
    backgrounds_by_split: Counter[str] = Counter()
    videos_by_split: dict[str, set[str]] = defaultdict(set)
    for item in records:
        split = str(item["split"])
        counts_by_split[split].update(item["counts"])  # type: ignore[arg-type]
        backgrounds_by_split[split] += int(bool(item["background"]))
        videos_by_split[split].add(str(item["source_video_id"]))
    for split in sorted(SPLITS):
        absent = [CLASSES[class_id] for class_id in range(len(CLASSES)) if counts_by_split[split][class_id] == 0]
        if absent:
            raise ValueError(f"Split {split!r} has no labeled boxes for classes: {absent}")

    output_dir.mkdir(parents=True, exist_ok=True)
    for item in records:
        split = str(item["split"])
        video_id = str(item["source_video_id"])
        frame_path = Path(str(item["frame_path"]))
        label_path = Path(str(item["label_path"]))
        image_dest = output_dir / "images" / split / frame_path.name
        label_dest = output_dir / "labels" / split / f"{frame_path.stem}.txt"
        image_dest.parent.mkdir(parents=True, exist_ok=True)
        label_dest.parent.mkdir(parents=True, exist_ok=True)
        try:
            os.link(frame_path, image_dest)
        except OSError:
            shutil.copy2(frame_path, image_dest)
        shutil.copy2(label_path, label_dest)

    data_yaml = {
        "path": output_dir.as_posix(),
        "train": "images/train",
        "val": "images/valid",
        "test": "images/test",
        "nc": len(CLASSES),
        "names": CLASSES,
    }
    (output_dir / "data.yaml").write_text(yaml.safe_dump(data_yaml, allow_unicode=True, sort_keys=False), encoding="utf-8")
    shutil.copy2(manifest_path, output_dir / "frames_manifest.csv")
    shutil.copy2(split_csv, output_dir / "source_video_splits.csv")
    report = {
        "frames_dir": str(frames_dir),
        "labels_dir": str(labels_dir),
        "frame_count": len(records),
        "source_video_count": len(video_splits),
        "exact_duplicate_frames_within_split": exact_duplicate_count,
        "splits": {
            split: {
                "source_video_count": len(videos_by_split[split]),
                "frame_count": sum(str(item["split"]) == split for item in records),
                "reviewed_background_count": backgrounds_by_split[split],
                "box_count_by_class": {CLASSES[class_id]: counts_by_split[split][class_id] for class_id in range(len(CLASSES))},
            }
            for split in sorted(SPLITS)
        },
        "class_names": CLASSES,
        "split_policy": "source_video_id; each source video belongs to exactly one split",
    }
    (output_dir / "preparation_report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(report, ensure_ascii=False, indent=2))
    print(f"Dataset YAML: {output_dir / 'data.yaml'}")


if __name__ == "__main__":
    main()
