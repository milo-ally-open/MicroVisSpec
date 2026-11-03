import os
from pathlib import Path

from roboflow import Roboflow

API_KEY = os.environ.get("ROBOFLOW_API_KEY")
if not API_KEY:
    raise SystemExit("Set ROBOFLOW_API_KEY in the environment before downloading datasets.")

rf = Roboflow(api_key=API_KEY)
DATASET_ROOT = Path(__file__).resolve().parent / "datasets"

project = rf.workspace("warehouse-recognition").project("warehouse-detection-xtbsn")
dataset = project.version(1).download(
    model_format="yolov11",
    location=str(DATASET_ROOT / "warehouse_detection"),
)
print(f"warehouse_detection v1 downloaded to {dataset.location}")
