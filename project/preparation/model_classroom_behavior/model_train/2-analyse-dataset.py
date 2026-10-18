import hashlib
import json
from collections import Counter
from pathlib import Path

import yaml


SCRIPT_DIR = Path(__file__).resolve().parent
DATASET_DIR = SCRIPT_DIR / "datasets" / "classroom_behavior"
OUTPUT_DIR = SCRIPT_DIR / "dataset_analysis" / "classroom_behavior"
IMAGE_SUFFIXES = {".jpg", ".jpeg", ".png", ".bmp", ".webp"}


def sha256(path):
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


data_yaml = DATASET_DIR / "data.yaml"
if not data_yaml.is_file():
    raise SystemExit(f"Dataset not found: {data_yaml}. Run 1-download-dataset.py first.")

data = yaml.safe_load(data_yaml.read_text(encoding="utf-8"))
names = data["names"]
class_names = [names[key] for key in sorted(names)] if isinstance(names, dict) else names
class_counts = Counter()
split_counts = {}
image_hashes = {}
issues = []

for split in ("train", "valid", "val", "test"):
    split_dir = DATASET_DIR / split
    if not split_dir.is_dir():
        continue
    images = [path for path in (split_dir / "images").rglob("*") if path.suffix.lower() in IMAGE_SUFFIXES]
    split_counts[split] = {"images": len(images), "objects": 0, "missing_labels": 0}
    for image in images:
        image_hashes.setdefault(sha256(image), []).append(split)
        label = split_dir / "labels" / f"{image.stem}.txt"
        if not label.is_file():
            split_counts[split]["missing_labels"] += 1
            continue
        for line_number, line in enumerate(label.read_text(encoding="utf-8").splitlines(), 1):
            if not line.strip():
                continue
            fields = line.split()
            try:
                class_id = int(fields[0])
                values = [float(value) for value in fields[1:]]
            except (ValueError, IndexError):
                issues.append(f"{label}:{line_number}: invalid label")
                continue
            is_box = len(fields) == 5
            is_polygon = len(fields) >= 7 and (len(fields) - 1) % 2 == 0
            if not (is_box or is_polygon) or not 0 <= class_id < len(class_names):
                issues.append(f"{label}:{line_number}: invalid YOLO box or polygon")
                continue
            if any(value < 0 or value > 1 for value in values):
                issues.append(f"{label}:{line_number}: coordinates must be normalized to [0, 1]")
                continue
            class_counts[class_id] += 1
            split_counts[split]["objects"] += 1

duplicates = [
    {"sha256": digest, "splits": sorted(set(splits))}
    for digest, splits in image_hashes.items()
    if len(set(splits)) > 1
]
report = {
    "dataset": "classroom_behavior",
    "version": 1,
    "classes": [{"id": index, "name": name, "objects": class_counts[index]} for index, name in enumerate(class_names)],
    "splits": split_counts,
    "cross_split_exact_duplicates": duplicates,
    "invalid_labels": issues,
}

OUTPUT_DIR.mkdir(parents=True, exist_ok=True)
report_path = OUTPUT_DIR / "dataset_summary.json"
report_path.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
print(json.dumps(report, ensure_ascii=False, indent=2))
print(f"Summary: {report_path}")
