import os
from roboflow import Roboflow

# 建议通过环境变量读取，不要把 API Key 写死在代码里
API_KEY = "tkAUEW15WDChi9VonEuf"

rf = Roboflow(api_key=API_KEY)

# =========================
# 1. Warehouse Detection
# =========================
warehouse_project = (
    rf.workspace("warehouse-recognition")
      .project("warehouse-detection-xtbsn")
)

warehouse_dataset = (
    warehouse_project
    .version(1)
    .download(
        model_format="yolov11",
        location="./datasets/warehouse_detection"
    )
)

# =========================
# 2. Vehicle Detection
# =========================
vehicle_project = (
    rf.workspace("jey-mtuyv")
      .project("vehicle-detection-tqbaf-7cck5")
)

vehicle_dataset = (
    vehicle_project
    .version(1)
    .download(
        model_format="yolov11",
        location="./datasets/vehicle_detection"
    )
)

print("两个数据集下载完成。")