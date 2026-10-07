from pathlib import Path
from collections import Counter
import random, cv2, yaml
import pandas as pd
import matplotlib.pyplot as plt

DATASETS = {
    "warehouse_detection": Path("./datasets/warehouse_detection"),
    "vehicle_detection": Path("./datasets/vehicle_detection"),
}
OUT = Path("./dataset_analysis")
random.seed(42)

def load_classes(root):
    with open(root / "data.yaml", encoding="utf-8") as f:
        names = yaml.safe_load(f)["names"]
    return {int(k): v for k, v in names.items()} if isinstance(names, dict) else dict(enumerate(names))

def read_label(path):
    if not path.exists():
        return []
    return [list(map(float, x.split()[:5])) for x in path.read_text().splitlines() if x.strip()]

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

        imgs = list((base / "images").glob("*.*"))
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

        split_rows.append([split, len(imgs), objects, empty])

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