from pathlib import Path
from collections import Counter
import math, random, cv2, yaml
import pandas as pd
import matplotlib

matplotlib.use("Agg")
import matplotlib.pyplot as plt

SCRIPT_DIR = Path(__file__).resolve().parent
DATASETS = {"warehouse_detection": SCRIPT_DIR / "datasets" / "warehouse_detection"}
OUT = SCRIPT_DIR / "dataset_analysis"
random.seed(42)

def load_classes(root):
    with open(root / "data.yaml", encoding="utf-8") as f:
        names = yaml.safe_load(f)["names"]
    return {int(k): v for k, v in names.items()} if isinstance(names, dict) else dict(enumerate(names))

def read_label(path):
    if not path.exists():
        return []

    boxes = []
    for line_number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        if not line.strip():
            continue

        fields = line.split()
        is_box = len(fields) == 5
        is_polygon = len(fields) >= 7 and (len(fields) - 1) % 2 == 0
        if not (is_box or is_polygon):
            raise ValueError(
                f"Invalid YOLO label in {path}:{line_number}: expected a 5-value "
                "bounding box or a class ID followed by at least 3 (x, y) points"
            )

        try:
            values = list(map(float, fields))
        except ValueError as exc:
            raise ValueError(
                f"Invalid YOLO label in {path}:{line_number}: values must be numeric"
            ) from exc

        if not all(map(math.isfinite, values)):
            raise ValueError(
                f"Invalid YOLO label in {path}:{line_number}: values must be finite"
            )

        cls = values[0]
        if cls < 0 or not cls.is_integer():
            raise ValueError(
                f"Invalid YOLO label in {path}:{line_number}: class ID must be a "
                "non-negative integer"
            )

        if is_box:
            xc, yc, bw, bh = values[1:]
        else:
            points = list(zip(values[1::2], values[2::2]))
            xs, ys = zip(*points)
            x1, x2 = min(xs), max(xs)
            y1, y2 = min(ys), max(ys)
            xc, yc = (x1 + x2) / 2, (y1 + y2) / 2
            bw, bh = x2 - x1, y2 - y1

        if not all(0 <= value <= 1 for value in (xc, yc, bw, bh)) or bw <= 0 or bh <= 0:
            raise ValueError(
                f"Invalid YOLO label in {path}:{line_number}: box values must be "
                "normalized to [0, 1], with positive width and height"
            )

        boxes.append([cls, xc, yc, bw, bh])

    return boxes

def plot_save(x, y, title, path, rotate=False):
    plt.figure(figsize=(10, 6))
    plt.bar(x, y)
    plt.title(title)
    if rotate:
        plt.xticks(rotation=45, ha="right")
    plt.tight_layout()
    plt.savefig(path, dpi=200)
    plt.close()

def draw_sample(img_path, label_path, names, save_path):
    img = cv2.imread(str(img_path))
    if img is None:
        return

    h, w = img.shape[:2]

    for cls, xc, yc, bw, bh in read_label(label_path):
        cls = int(cls)
        x1, y1 = int((xc-bw/2)*w), int((yc-bh/2)*h)
        x2, y2 = int((xc+bw/2)*w), int((yc+bh/2)*h)

        cv2.rectangle(img, (x1,y1), (x2,y2), (0,255,0), 2)
        cv2.putText(img, names.get(cls, str(cls)), (x1, max(20,y1-5)),
                    cv2.FONT_HERSHEY_SIMPLEX, 0.6, (0,255,0), 2)

    cv2.imwrite(str(save_path), img)

def analyze(name, root):
    names = load_classes(root)
    out = OUT / name
    out.mkdir(parents=True, exist_ok=True)
    (out / "samples").mkdir(exist_ok=True)

    class_count = Counter()
    split_rows, bbox_rows, samples = [], [], []

    for split in ["train", "valid", "test"]:
        base = root / split
        if not base.exists() and split == "valid":
            base = root / "val"
        if not base.exists():
            continue

        images_dir = base / "images"
        if not images_dir.is_dir():
            raise FileNotFoundError(f"Expected image directory: {images_dir}")

        imgs = list(images_dir.glob("*.*"))
        objects, empty = 0, 0

        for img in imgs:
            label = base / "labels" / f"{img.stem}.txt"
            boxes = read_label(label)

            if not boxes:
                empty += 1
            else:
                samples.append((img, label))

            objects += len(boxes)

            for cls, xc, yc, bw, bh in boxes:
                cls = int(cls)
                class_count[cls] += 1
                bbox_rows.append([names.get(cls, str(cls)), bw, bh, bw*bh])

        split_rows.append([base.name, len(imgs), objects, empty])

    class_df = pd.DataFrame(
        [[i, names[i], class_count[i]] for i in names],
        columns=["class_id", "class_name", "count"]
    ).sort_values("count", ascending=False)

    split_df = pd.DataFrame(
        split_rows,
        columns=["split", "images", "objects", "empty_images"]
    )

    bbox_df = pd.DataFrame(
        bbox_rows,
        columns=["class_name", "width", "height", "area"]
    )

    class_df.to_csv(out / "class_statistics.csv", index=False)
    split_df.to_csv(out / "split_statistics.csv", index=False)
    bbox_df.to_csv(out / "bbox_statistics.csv", index=False)

    print(f"\n{name}")
    print(split_df)
    print("\n类别统计:")
    print(class_df)

    plot_save(
        class_df["class_name"], class_df["count"],
        f"{name} Class Distribution",
        out / "01_class_distribution.png", True
    )

    plot_save(
        split_df["split"], split_df["images"],
        f"{name} Dataset Split",
        out / "02_dataset_split.png"
    )

    if not bbox_df.empty:
        plt.figure(figsize=(7,7))
        plt.scatter(bbox_df["width"], bbox_df["height"], s=5, alpha=.2)
        plt.xlabel("BBox Width")
        plt.ylabel("BBox Height")
        plt.title(f"{name} BBox Distribution")
        plt.tight_layout()
        plt.savefig(out / "03_bbox_distribution.png", dpi=200)
        plt.close()

    for i, (img, label) in enumerate(random.sample(samples, min(9, len(samples))), 1):
        draw_sample(img, label, names, out / "samples" / f"{i:02d}_{img.name}")

if __name__ == "__main__":
    OUT.mkdir(exist_ok=True)

    for name, root in DATASETS.items():
        if root.exists():
            analyze(name, root)
        else:
            print(f"数据集不存在: {root}")
