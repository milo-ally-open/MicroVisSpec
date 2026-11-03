"""Create a reproducible near-duplicate-aware classroom dataset split.

Images connected by a 64-bit difference-hash distance <= 4 are kept in one
split. The source dataset is read-only; generated text lists point to its images.
"""

from __future__ import annotations

import argparse
import json
import random
from collections import Counter
from pathlib import Path

import numpy as np
import yaml
from PIL import Image


HERE = Path(__file__).resolve().parent
SOURCE = HERE / "datasets" / "classroom_behavior"
OUTPUT = HERE / "datasets_grouped" / "classroom_behavior"
SPLITS = ("train", "valid", "test")
CLASS_NAMES = ["hand-raising", "reading", "writing"]


def dhash(path: Path) -> int:
    pixels = np.asarray(Image.open(path).convert("L").resize((9, 8)))
    bits = pixels[:, :-1] > pixels[:, 1:]
    value = 0
    for bit in bits.flat:
        value = (value << 1) | int(bit)
    return value


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--source", type=Path, default=SOURCE)
    parser.add_argument("--output", type=Path, default=OUTPUT)
    parser.add_argument("--max-hamming", type=int, default=4)
    parser.add_argument("--seed", type=int, default=42)
    args = parser.parse_args()
    source = args.source.resolve()
    output = args.output.resolve()

    images: list[Path] = []
    for split in SPLITS:
        images.extend(sorted(p.resolve() for p in (source / split / "images").glob("*.*")))
    if not images:
        raise FileNotFoundError(f"No images found under {source}")
    hashes = [dhash(path) for path in images]

    parent = list(range(len(images)))

    def find(index: int) -> int:
        while parent[index] != index:
            parent[index] = parent[parent[index]]
            index = parent[index]
        return index

    edges = 0
    for left in range(len(images)):
        for right in range(left + 1, len(images)):
            if (hashes[left] ^ hashes[right]).bit_count() <= args.max_hamming:
                edges += 1
                root_left, root_right = find(left), find(right)
                if root_left != root_right:
                    parent[root_right] = root_left

    groups = np.asarray([find(i) for i in range(len(images))])
    group_members: dict[int, list[int]] = {}
    for index, group in enumerate(groups):
        group_members.setdefault(int(group), []).append(index)
    shuffled_groups = list(group_members)
    random.Random(args.seed).shuffle(shuffled_groups)
    test_idx_list: list[int] = []
    valid_idx_list: list[int] = []
    train_idx_list: list[int] = []
    target_test = round(len(images) * 0.15)
    target_valid = round(len(images) * 0.15)
    for group in shuffled_groups:
        members = group_members[group]
        if len(test_idx_list) < target_test:
            test_idx_list.extend(members)
        elif len(valid_idx_list) < target_valid:
            valid_idx_list.extend(members)
        else:
            train_idx_list.extend(members)
    train_idx = np.asarray(train_idx_list)
    valid_idx = np.asarray(valid_idx_list)
    test_idx = np.asarray(test_idx_list)
    assigned = {"train": train_idx, "valid": valid_idx, "test": test_idx}

    output.mkdir(parents=True, exist_ok=True)
    split_groups: dict[str, set[int]] = {}
    stats: dict[str, dict[str, object]] = {}
    for split, indices in assigned.items():
        entries = sorted(images[int(i)].as_posix() for i in indices)
        (output / f"{split}.txt").write_text("\n".join(entries) + "\n", encoding="utf-8")
        split_groups[split] = {int(groups[int(i)]) for i in indices}
        class_counts = Counter()
        labeled = 0
        for index in indices:
            image_path = images[int(index)]
            label_path = image_path.parent.parent / "labels" / f"{image_path.stem}.txt"
            if not label_path.is_file():
                continue
            labeled += 1
            for line in label_path.read_text(encoding="utf-8").splitlines():
                if line.strip():
                    class_counts[int(line.split()[0])] += 1
        stats[split] = {
            "images": len(indices),
            "near_duplicate_groups": len(split_groups[split]),
            "labeled_images": labeled,
            "instances_by_class": {
                CLASS_NAMES[c]: class_counts[c] for c in range(len(CLASS_NAMES))
            },
        }

    if any(split_groups[a] & split_groups[b] for a, b in (("train", "valid"), ("train", "test"), ("valid", "test"))):
        raise RuntimeError("A near-duplicate group crossed splits.")

    data = {
        "path": output.as_posix(),
        "train": "train.txt",
        "val": "valid.txt",
        "test": "test.txt",
        "nc": len(CLASS_NAMES),
        "names": CLASS_NAMES,
    }
    (output / "data.yaml").write_text(yaml.safe_dump(data, allow_unicode=True, sort_keys=False), encoding="utf-8")
    group_sizes = Counter(int(groups[i]) for i in range(len(images)))
    report = {
        "source": str(source),
        "output": str(output),
        "seed": args.seed,
        "dhash_max_hamming": args.max_hamming,
        "near_duplicate_edges": edges,
        "near_duplicate_groups": len(group_sizes),
        "groups_with_multiple_images": sum(size > 1 for size in group_sizes.values()),
        "largest_group_size": max(group_sizes.values()),
        "splits": stats,
    }
    (output / "split_report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(report, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
