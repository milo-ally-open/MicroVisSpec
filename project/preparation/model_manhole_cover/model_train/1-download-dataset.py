import os
from pathlib import Path

import yaml
from roboflow import Roboflow


SCRIPT_DIR = Path(__file__).resolve().parent
DATASET_DIR = SCRIPT_DIR / "datasets" / "manhole_cover"
WORKSPACE = "roboflow-universe-projects"
PROJECT = "manhole-cover-detection"
VERSION = 1
EXPECTED_CLASSES = {"0"}
DATASET_LABEL = "roboflow-universe-projects/manhole-cover-detection v1"

api_key = os.environ.get("ROBOFLOW_API_KEY")
if not api_key:
    raise SystemExit("Set ROBOFLOW_API_KEY before downloading the dataset.")

project = Roboflow(api_key=api_key).workspace(WORKSPACE).project(PROJECT)
dataset = project.version(VERSION).download(
    model_format="yolov11",
    location=str(DATASET_DIR),
)

data_yaml = Path(dataset.location) / "data.yaml"
data = yaml.safe_load(data_yaml.read_text(encoding="utf-8"))
names = data["names"]
class_names = [names[key] for key in sorted(names)] if isinstance(names, dict) else names
normalized = {name.lower().replace("_", "-").replace(" ", "-") for name in class_names}
if normalized != EXPECTED_CLASSES:
    raise SystemExit(f"Unexpected dataset classes: {class_names}")

classes_file = SCRIPT_DIR.parent / "model_deployment" / "classes.txt"
classes_file.parent.mkdir(parents=True, exist_ok=True)
classes_file.write_text("\n".join(class_names) + "\n", encoding="utf-8")

print(f"Downloaded manhole_cover ({DATASET_LABEL}) to {dataset.location}")
print(f"Class order: {class_names}")
