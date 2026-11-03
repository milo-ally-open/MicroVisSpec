"""Prepare novel target-class images from classroom-4sjvo for fine-tuning."""

from __future__ import annotations

import csv
import argparse
import json
import shutil
from collections import Counter
from pathlib import Path

import yaml


TARGET_NAMES = ["hand-raising", "reading", "writing"]
SOURCE_TO_TARGET = {0: 0, 2: 1, 4: 2}


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--aux-root", type=Path, required=True, help="Audited Roboflow source dataset directory.")
    parser.add_argument("--audit-root", type=Path, required=True, help="New output directory for prepared classroom4 data.")
    parser.add_argument("--scb5-root", type=Path, required=True, help="Audited SCB5 directory containing train_merged.txt.")
    parser.add_argument("--baseline-root", type=Path, required=True, help="Grouped baseline dataset directory containing valid.txt and test.txt.")
    return parser.parse_args()


def main() -> None:
    args = parse_args()
    aux_root = args.aux_root.resolve()
    audit_root = args.audit_root.resolve()
    dataset_root = audit_root / "classroom_behavior"
    scb5_root = args.scb5_root.resolve()
    baseline_root = args.baseline_root.resolve()

    if audit_root.exists():
        raise FileExistsError(f"Refusing to overwrite prepared data: {audit_root}")

    match_path = aux_root / "review" / "all_source_near_duplicates.csv"
    with match_path.open(encoding="utf-8-sig", newline="") as stream:
        excluded = {Path(row["candidate_image"]).resolve() for row in csv.DictReader(stream)}

    source_images = sorted((aux_root / "train" / "images").glob("*.*"))
    image_dir = audit_root / "train" / "images"
    label_dir = audit_root / "train" / "labels"
    image_dir.mkdir(parents=True)
    label_dir.mkdir(parents=True)

    kept_images = []
    target_instances = Counter()
    excluded_near_duplicate = 0
    excluded_without_target = 0
    for image in source_images:
        if image.resolve() in excluded:
            excluded_near_duplicate += 1
            continue
        source_label = aux_root / "train" / "labels" / f"{image.stem}.txt"
        if not source_label.is_file():
            raise FileNotFoundError(f"Missing label file for {image}")
        mapped_lines = []
        for line_number, line in enumerate(source_label.read_text(encoding="utf-8-sig").splitlines(), 1):
            fields = line.split()
            if len(fields) != 5:
                raise ValueError(f"Malformed annotation {source_label}:{line_number}")
            source_id = int(float(fields[0]))
            if source_id not in SOURCE_TO_TARGET:
                continue
            target_id = SOURCE_TO_TARGET[source_id]
            mapped_lines.append(str(target_id) + " " + " ".join(fields[1:]))
            target_instances[TARGET_NAMES[target_id]] += 1
        if not mapped_lines:
            excluded_without_target += 1
            continue
        destination_image = image_dir / image.name
        shutil.copy2(image, destination_image)
        destination_label = label_dir / f"{image.stem}.txt"
        destination_label.write_text("\n".join(mapped_lines) + "\n", encoding="utf-8")
        kept_images.append(destination_image.resolve())

    existing_train = [
        Path(line.strip()).resolve()
        for line in (scb5_root / "train_merged.txt").read_text(encoding="utf-8-sig").splitlines()
        if line.strip()
    ]
    valid_images = [
        Path(line.strip()).resolve()
        for line in (baseline_root / "valid.txt").read_text(encoding="utf-8-sig").splitlines()
        if line.strip()
    ]
    test_images = [
        Path(line.strip()).resolve()
        for line in (baseline_root / "test.txt").read_text(encoding="utf-8-sig").splitlines()
        if line.strip()
    ]

    dataset_root.mkdir(parents=True)
    train_images = existing_train + kept_images
    for filename, paths in (("train.txt", train_images), ("valid.txt", valid_images), ("test.txt", test_images)):
        (dataset_root / filename).write_text("\n".join(path.as_posix() for path in paths) + "\n", encoding="utf-8")
    data_yaml = {
        "path": dataset_root.as_posix(),
        "train": "train.txt",
        "val": "valid.txt",
        "test": "test.txt",
        "nc": len(TARGET_NAMES),
        "names": TARGET_NAMES,
    }
    (dataset_root / "data.yaml").write_text(yaml.safe_dump(data_yaml, allow_unicode=True, sort_keys=False), encoding="utf-8")

    report = {
        "source_url": "https://universe.roboflow.com/studentbehavior-37daj/classroom-4sjvo/dataset/3",
        "source_license": "CC BY 4.0",
        "source_class_names": ["hand-raising", "leaning over the table", "reading", "using phone", "writing"],
        "target_class_names": TARGET_NAMES,
        "source_to_target_class_ids": SOURCE_TO_TARGET,
        "source_train_images": len(source_images),
        "near_duplicate_images_excluded": excluded_near_duplicate,
        "images_without_target_class_excluded": excluded_without_target,
        "prepared_novel_images": len(kept_images),
        "prepared_target_instances": {name: target_instances[name] for name in TARGET_NAMES},
        "existing_scb5_merged_train_images": len(existing_train),
        "merged_train_images": len(train_images),
        "validation_images": len(valid_images),
        "test_images": len(test_images),
        "test_split_used_for_model_selection": False,
        "prepared_root": str(audit_root),
        "data_yaml": str(dataset_root / "data.yaml"),
    }
    (audit_root / "preparation_report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(report, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
